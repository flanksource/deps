package start

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/flanksource/clicky/exec"
	"github.com/flanksource/deps/start/state"
)

// commandRuntime manages services whose lifecycle is driven by a CLI that
// runs to completion (kind create/delete cluster). The rendered stop/status
// commands are persisted in state so later invocations can run them without
// re-rendering.
type commandRuntime struct{}

func (r *commandRuntime) Kind() RuntimeKind { return RuntimeCommand }

func (r *commandRuntime) Reconcile(ctx context.Context, svc *ServiceContext, prior *state.State) (*state.State, error) {
	status, err := r.Status(ctx, prior)
	if err != nil {
		return nil, fmt.Errorf("failed to inspect command service for reconciliation: %w", err)
	}
	if status != state.StatusStopped {
		if err := r.Stop(ctx, prior); err != nil {
			return nil, fmt.Errorf("failed to stop command service for reconciliation: %w", err)
		}
	}
	return r.Start(ctx, svc)
}

func (r *commandRuntime) Start(ctx context.Context, svc *ServiceContext) (*state.State, error) {
	spec := svc.Spec.Command
	if err := checkPlatform(spec.Platforms, svc.OS, svc.Arch); err != nil {
		return nil, err
	}
	if prior, err := state.Load(svc.Opts.StateDir, svc.Name); err == nil && prior.Ready {
		if status, err := r.Status(ctx, prior); err == nil && status == state.StatusRunning {
			return prior, nil
		}
	}

	if err := installServicePackages(ctx, svc, spec.Package); err != nil {
		return nil, err
	}

	data := templateData(svc, fmt.Sprintf("%s:%d", svc.serviceHost(), hostPort(svc)), "")
	env := map[string]string{}
	for k, v := range spec.Env {
		rendered, err := render("command.env."+k, v, data)
		if err != nil {
			return nil, err
		}
		env[k] = rendered
	}
	for name, content := range spec.Files {
		rendered, err := render("command.files."+name, content, data)
		if err != nil {
			return nil, err
		}
		if err := os.WriteFile(filepath.Join(svc.RunDir, name), []byte(rendered), 0o644); err != nil {
			return nil, err
		}
	}

	commands := map[string]string{}
	for key, tmpl := range map[string]string{"start": spec.Start, "stop": spec.Stop, "status": spec.Status} {
		if tmpl == "" {
			continue
		}
		rendered, err := render("command."+key, tmpl, data)
		if err != nil {
			return nil, err
		}
		commands[key] = rendered
	}

	logf, err := os.OpenFile(svc.LogFile, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, err
	}
	defer func() { _ = logf.Close() }()

	out := serviceLogWriter(svc, logf)
	proc := exec.NewExec(commands["start"]).WithEnv(env).WithCwd(svc.RunDir).
		WithTimeout(svc.Opts.WaitTimeout).Stream(out, out).Run()
	if result := proc.Result(); !result.IsOk() {
		return nil, fmt.Errorf("%q failed (exit %d):\n%s", commands["start"], result.ExitCode, tail(result.Stdout+"\n"+result.Stderr, 25))
	}

	if err := awaitHealthy(ctx, svc, spec.Health, nil); err != nil {
		return nil, err
	}
	return &state.State{Commands: commands}, nil
}

func (r *commandRuntime) Stop(ctx context.Context, st *state.State) error {
	stop := st.Commands["stop"]
	if stop == "" {
		return fmt.Errorf("no stop command recorded for %s", st.Name)
	}
	proc := exec.NewExec(stop).WithTimeout(5 * time.Minute).Run()
	if result := proc.Result(); !result.IsOk() {
		return fmt.Errorf("%q failed (exit %d): %s", stop, result.ExitCode, tail(result.Stdout+"\n"+result.Stderr, 10))
	}
	return nil
}

func (r *commandRuntime) Status(ctx context.Context, st *state.State) (state.Status, error) {
	status := st.Commands["status"]
	if status == "" {
		return state.StatusUnknown, nil
	}
	if exec.NewExec(status).WithTimeout(time.Minute).Run().Result().IsOk() {
		return state.StatusRunning, nil
	}
	return state.StatusStopped, nil
}
