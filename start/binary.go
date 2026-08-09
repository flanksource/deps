package start

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/flanksource/clicky/exec"
	"github.com/flanksource/deps"
	"github.com/flanksource/deps/pkg/types"
	"github.com/flanksource/deps/start/state"
)

// binaryRuntime installs the service artifact via deps and runs it under a
// clicky supervised process.
type binaryRuntime struct {
	mu   sync.Mutex
	sup  *exec.SupervisedProcess
	proc *exec.Process
}

func (r *binaryRuntime) Kind() RuntimeKind { return RuntimeBinary }

func (r *binaryRuntime) Reconcile(ctx context.Context, svc *ServiceContext, prior *state.State) (*state.State, error) {
	if processAlive(prior.PID) {
		if err := r.Stop(ctx, prior); err != nil {
			return nil, fmt.Errorf("failed to stop binary for reconciliation: %w", err)
		}
	}
	return r.Start(ctx, svc)
}

func (r *binaryRuntime) Start(ctx context.Context, svc *ServiceContext) (*state.State, error) {
	spec := svc.Spec.Binary
	if err := checkPlatform(spec.Platforms, svc.OS, svc.Arch); err != nil {
		return nil, err
	}

	if prior, err := state.Load(svc.Opts.StateDir, svc.Name); err == nil && processAlive(prior.PID) {
		return prior, nil
	}

	if err := installServicePackages(ctx, svc, spec.Package, spec.Requires...); err != nil {
		return nil, err
	}

	data := templateData(svc, fmt.Sprintf("localhost:%d", hostPort(svc)), "")
	if err := writeRunFiles(svc, data); err != nil {
		return nil, err
	}
	if err := runInitSteps(svc, data); err != nil {
		return nil, err
	}

	command, args, env, err := renderCommand(svc, data)
	if err != nil {
		return nil, err
	}

	logf, err := os.OpenFile(svc.LogFile, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, err
	}

	started := make(chan *exec.Process, 1)
	// record where the log ends before launching, so a stdout probe only
	// matches this start's output
	since := logOffsetOf(svc.LogFile)

	out := serviceLogWriter(svc, logf)
	proc := exec.NewExec(command, args...).WithProcessGroup().WithEnv(env).WithCwd(svc.RunDir).Stream(out, out)
	sup := proc.Supervise(exec.SuperviseOptions{
		RestartPolicy: exec.RestartNo,
		StopGrace:     10 * time.Second,
		DetectPorts:   true,
		OnStarted: func(p *exec.Process) {
			select {
			case started <- p:
			default:
			}
		},
	})
	sup.Start()

	select {
	case p := <-started:
		r.mu.Lock()
		r.sup, r.proc = sup, p
		r.mu.Unlock()
	case <-time.After(10 * time.Second):
		sup.Stop()
		return nil, fmt.Errorf("%s did not start within 10s", command)
	case <-ctx.Done():
		sup.Stop()
		return nil, ctx.Err()
	}

	if err := awaitHealthy(ctx, svc, spec.Health, r.Watch(ctx, svc, nil, since)); err != nil {
		sup.Stop()
		return nil, err
	}
	go persistMetrics(svc.Opts.StateDir, svc.Name, sup)

	ports := map[string]int{}
	for _, p := range svc.Spec.Ports {
		ports[p.Name] = p.Port
	}
	if primary, ok := svc.Spec.PrimaryPort(); ok && svc.Opts.Port != 0 {
		ports[primary.Name] = svc.Opts.Port
	}
	return &state.State{PID: sup.Pid(), SupervisorPID: os.Getpid(), Ports: ports}, nil
}

// persistMetrics runs in the supervising process, copying the supervised
// process's resource samples (CPU, RSS, open files, detected ports) into the
// state file until the service exits. Readers (status/list) run in other
// processes and can only see what is persisted here.
func persistMetrics(stateDir, name string, sup *exec.SupervisedProcess) {
	// let the post-start state save land before the first update
	time.Sleep(3 * time.Second)
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for ; ; <-ticker.C {
		switch sup.Status() {
		case exec.StatusStopped, exec.StatusExited, exec.StatusCrashed:
			return
		}
		st, err := state.Load(stateDir, name)
		if err != nil || st.SupervisorPID != os.Getpid() {
			return // state was removed or belongs to another supervisor
		}
		// the pid changes across in-place restarts
		st.PID = sup.Pid()
		res := sup.Resources()
		st.Resources = &state.Resources{
			CPUPercent: res.CPUPercent,
			RSSBytes:   res.RSSBytes,
			PeakRSS:    sup.Peak().RSSBytes,
			OpenFiles:  res.OpenFiles,
			Ports:      sup.Ports(),
			Restarts:   sup.Restarts(),
			SampledAt:  time.Now(),
		}
		_ = st.Save(stateDir)
	}
}

// Watch observes the supervised process for readiness. Output always comes
// from the log file rather than the supervisor's in-memory buffer: it is the
// only view a restart driven from another process has, and the offset keeps
// an in-place restart from matching the output of the run it replaced.
func (r *binaryRuntime) Watch(ctx context.Context, svc *ServiceContext, st *state.State, since logOffset) *processWatch {
	watch := &processWatch{output: since.read}
	r.mu.Lock()
	sup := r.sup
	r.mu.Unlock()
	if sup != nil {
		watch.alive = func() bool {
			switch sup.Status() {
			case exec.StatusCrashed, exec.StatusExited, exec.StatusStopped:
				return false
			}
			return true
		}
		watch.ports = sup.Ports
		return watch
	}
	if st != nil {
		watch.alive = func() bool { return st.PID == 0 || processAlive(st.PID) }
	}
	return watch
}

// versionAny tells the installer to use whatever is already installed and to
// resolve a version only when nothing is.
const versionAny = "any"

// installServicePackages ensures the service's artifact (and any extra
// required packages) are present and fills AppDir/BinDir/Version from the
// primary package. pkgName defaults to the service's registry key.
//
// Starting a service is not an upgrade: without Opts.Update an installed
// artifact is used as-is, so start stays offline and fast. A version supplied
// on this invocation is always honoured; one replayed from persisted start
// options is not a reason to go back to the network.
func installServicePackages(ctx context.Context, svc *ServiceContext, pkgName string, extra ...string) error {
	if pkgName == "" {
		pkgName = svc.Name
	}
	version := svc.Opts.Version
	if version == "" {
		version = "latest"
	}
	if !svc.Opts.Update && !svc.Opts.supplied.version {
		version = versionAny
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	appRoot := filepath.Join(home, ".deps", "opt")
	opts := []deps.InstallOption{
		deps.WithBinDir(filepath.Join(home, ".deps", "bin")),
		deps.WithAppDir(appRoot),
	}
	result, err := deps.InstallWithContext(ctx, pkgName, version, opts...)
	if err != nil {
		return fmt.Errorf("failed to install %s: %w", pkgName, err)
	}
	svc.BinDir = result.BinDir
	// "any" is a sentinel, not a version: keep whatever the state already
	// recorded rather than reporting it as the running version
	if resolved := result.Version.Version; resolved != "" && resolved != versionAny {
		svc.Version = resolved
	}
	// the installer reports AppDir only when it actually installs, so an
	// already-installed directory package has to be located on disk
	svc.AppDir = result.AppDir
	if svc.AppDir == "" && result.Package.Mode == "directory" {
		svc.AppDir = existingAppDir(appRoot, result.Package, svc.Version)
		if svc.AppDir == "" {
			return fmt.Errorf("%s reports no install directory under %s; reinstall it with --update", pkgName, appRoot)
		}
	}

	for _, name := range extra {
		required := versionAny
		if svc.Opts.Update {
			required = "latest"
		}
		if _, err := deps.InstallWithContext(ctx, name, required, opts...); err != nil {
			return fmt.Errorf("failed to install required package %s: %w", name, err)
		}
	}
	return nil
}

// existingAppDir locates an installed directory-mode package, covering both
// versioned and unversioned folder layouts.
func existingAppDir(root string, pkg types.Package, version string) string {
	for _, folder := range []string{pkg.FolderName(version), pkg.FolderName("")} {
		dir := filepath.Join(root, folder)
		if info, err := os.Stat(dir); err == nil && info.IsDir() {
			return dir
		}
	}
	return ""
}

// renderCommand resolves the executable path, args and env from the spec.
// Relative commands resolve against the installed AppDir.
func renderCommand(svc *ServiceContext, data map[string]any) (string, []string, map[string]string, error) {
	spec := svc.Spec.Binary
	command, err := render("binary.command", spec.Command, data)
	if err != nil {
		return "", nil, nil, err
	}
	if !filepath.IsAbs(command) && strings.Contains(command, "/") {
		command = filepath.Join(svc.AppDir, command)
	}

	args, err := renderArguments("binary.args", spec.Args, data)
	if err != nil {
		return "", nil, nil, err
	}

	env := map[string]string{}
	for k, v := range spec.Env {
		rendered, err := render("binary.env."+k, v, data)
		if err != nil {
			return "", nil, nil, err
		}
		env[k] = rendered
	}
	for k, v := range svc.Opts.Env {
		env[k] = v
	}
	return command, args, env, nil
}

// writeRunFiles renders spec files (configs) into the run dir and the
// password file used by init steps.
func writeRunFiles(svc *ServiceContext, data map[string]any) error {
	pwfile, _ := data["passwordFile"].(string)
	if err := os.WriteFile(pwfile, []byte(svc.Password), 0o600); err != nil {
		return err
	}
	for name, content := range svc.Spec.Binary.Files {
		rendered, err := render("binary.files."+name, content, data)
		if err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(svc.RunDir, name), []byte(rendered), 0o644); err != nil {
			return err
		}
	}
	return nil
}

// runInitSteps executes one-time setup commands (e.g. initdb) with the
// installed AppDir as working directory.
func runInitSteps(svc *ServiceContext, data map[string]any) error {
	for i, step := range svc.Spec.Binary.Init {
		if step.Creates != "" {
			creates, err := render(fmt.Sprintf("binary.init[%d].creates", i), step.Creates, data)
			if err != nil {
				return err
			}
			if _, err := os.Stat(creates); err == nil {
				continue
			}
		}
		command, err := render(fmt.Sprintf("binary.init[%d].command", i), step.Command, data)
		if err != nil {
			return err
		}
		proc := exec.NewExec(command).WithCwd(svc.AppDir).Run()
		if result := proc.Result(); !result.IsOk() {
			return fmt.Errorf("init step %q failed (exit %d): %s", command, result.ExitCode, tail(result.Stdout+"\n"+result.Stderr, 20))
		}
	}
	return nil
}

// Restart restarts the service in place, keeping the supervisor alive: via
// SupervisedProcess.Restart when this process supervises it, otherwise by
// signalling the supervising process (which traps SIGHUP and restarts).
func (r *binaryRuntime) Restart(ctx context.Context, stateDir string, st *state.State) error {
	r.mu.Lock()
	sup := r.sup
	r.mu.Unlock()
	if sup != nil {
		return r.restartInProcess(ctx, stateDir, st, sup)
	}

	if st.SupervisorPID <= 0 || !processAlive(st.SupervisorPID) {
		return fmt.Errorf("no supervisor running for %s", st.Name)
	}
	oldPID := st.PID
	if err := signalSupervisor(st.SupervisorPID); err != nil {
		return err
	}
	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		fresh, err := state.Load(stateDir, st.Name)
		if err == nil && fresh.PID != 0 && fresh.PID != oldPID && processAlive(fresh.PID) {
			*st = *fresh
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(500 * time.Millisecond):
		}
	}
	return fmt.Errorf("supervisor did not restart %s within 60s", st.Name)
}

func (r *binaryRuntime) restartInProcess(ctx context.Context, stateDir string, st *state.State, sup *exec.SupervisedProcess) error {
	oldPID := sup.Pid()
	sup.Restart()
	deadline := time.Now().Add(60 * time.Second)
	for sup.Pid() == oldPID || !sup.IsRunning() {
		if time.Now().After(deadline) {
			return fmt.Errorf("%s did not come back up within 60s (status %s)", st.Name, sup.Status())
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(200 * time.Millisecond):
		}
	}

	st.PID = sup.Pid()
	st.StartedAt = time.Now()
	// Ready stays false until the caller's readiness probes pass; a process
	// that came back up is not yet a service that accepts connections.
	if fresh, err := state.Load(stateDir, st.Name); err == nil {
		fresh.PID = st.PID
		fresh.StartedAt = st.StartedAt
		fresh.Ready = false
		return fresh.Save(stateDir)
	}
	return nil
}

func (r *binaryRuntime) Stop(ctx context.Context, st *state.State) error {
	r.mu.Lock()
	sup := r.sup
	r.mu.Unlock()
	if sup != nil {
		sup.Stop()
		return nil
	}
	return killProcessGroup(st.PID, 10*time.Second)
}

func (r *binaryRuntime) Status(ctx context.Context, st *state.State) (state.Status, error) {
	if processAlive(st.PID) {
		return state.StatusRunning, nil
	}
	return state.StatusStopped, nil
}

// Wait blocks until the supervised process exits or ctx is cancelled.
func (r *binaryRuntime) Wait(ctx context.Context) error {
	r.mu.Lock()
	sup := r.sup
	r.mu.Unlock()
	if sup == nil {
		return nil
	}
	done := make(chan struct{})
	go func() {
		sup.Wait()
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		sup.Stop()
		return ctx.Err()
	}
}

func checkPlatform(patterns []string, os, arch string) error {
	if len(patterns) == 0 {
		return nil
	}
	platform := os + "-" + arch
	for _, p := range patterns {
		if matched, _ := filepath.Match(p, platform); matched {
			return nil
		}
	}
	return fmt.Errorf("binary runtime not supported on %s (supported: %s)", platform, strings.Join(patterns, ", "))
}
