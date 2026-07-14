package start

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/netip"
	"sort"
	"strings"
	"time"

	cerrdefs "github.com/containerd/errdefs"
	sdkclient "github.com/docker/go-sdk/client"
	sdkcontainer "github.com/docker/go-sdk/container"
	"github.com/moby/moby/api/pkg/stdcopy"
	mobycontainer "github.com/moby/moby/api/types/container"
	mobynetwork "github.com/moby/moby/api/types/network"
	mobyclient "github.com/moby/moby/client"

	"github.com/flanksource/deps/start/state"
)

const serviceLabel = "flanksource.deps/service"
const managedConfigLabel = "flanksource.deps/config"

// dockerRuntime runs the service as a container via the docker go-sdk, using
// an idempotent named container deps-start-<service>. The sdk client resolves
// DOCKER_HOST and the docker CLI's current context natively.
type dockerRuntime struct {
	docker sdkclient.SDKClient
}

func (r *dockerRuntime) Kind() RuntimeKind { return RuntimeDocker }

func (r *dockerRuntime) client() (sdkclient.SDKClient, error) {
	if r.docker != nil {
		return r.docker, nil
	}
	cli, err := sdkclient.New(context.Background())
	if err != nil {
		return nil, fmt.Errorf("failed to create docker client: %w", err)
	}
	r.docker = cli
	return cli, nil
}

// daemonHost returns the hostname services on the daemon are reachable at:
// the sdk resolves it to "localhost" for unix/npipe sockets and to the
// endpoint hostname for tcp daemons.
func (r *dockerRuntime) daemonHost(ctx context.Context) string {
	cli, err := r.client()
	if err != nil {
		return "localhost"
	}
	host, err := cli.DaemonHostWithContext(ctx)
	if err != nil || host == "" {
		return "localhost"
	}
	return host
}

func containerName(service string) string { return "deps-start-" + service }

func (r *dockerRuntime) Reconcile(ctx context.Context, svc *ServiceContext, prior *state.State) (*state.State, error) {
	docker, err := r.client()
	if err != nil {
		return nil, err
	}
	container := prior.ContainerID
	if container == "" {
		container = containerName(svc.Name)
	}
	if _, err := docker.ContainerRemove(ctx, container, mobyclient.ContainerRemoveOptions{Force: true}); err != nil && !cerrdefs.IsNotFound(err) {
		return nil, fmt.Errorf("failed to remove container %s for reconciliation: %w", container, err)
	}
	return r.Start(ctx, svc)
}

func (r *dockerRuntime) Start(ctx context.Context, svc *ServiceContext) (*state.State, error) {
	spec := svc.Spec.Docker
	if err := checkPlatform(spec.Platforms, svc.OS, svc.Arch); err != nil {
		return nil, err
	}
	if _, err := r.client(); err != nil {
		return nil, err
	}
	if _, err := r.DesiredConfig(ctx, svc); err != nil {
		return nil, err
	}
	svc.Host = r.daemonHost(ctx)
	data := templateData(svc, fmt.Sprintf("%s:%d", svc.serviceHost(), hostPort(svc)), "")

	name := containerName(svc.Name)
	id, running, err := r.ensureContainer(ctx, svc, data, name)
	if err != nil {
		return nil, err
	}

	if running {
		if prior, err := state.Load(svc.Opts.StateDir, svc.Name); err == nil && prior.ContainerID == id {
			return prior, nil
		}
	}

	watch := &processWatch{
		alive:  func() bool { return r.isRunning(ctx, id) },
		output: func() string { return r.containerLogTail(ctx, id) },
		execProbe: func(ctx context.Context, cmd []string) bool {
			return r.execProbe(ctx, id, cmd, data)
		},
	}
	if err := awaitHealthy(ctx, svc, spec.Health, watch); err != nil {
		return nil, fmt.Errorf("container %s: %w", name, err)
	}

	ports := map[string]int{}
	for _, p := range svc.Spec.Ports {
		ports[p.Name] = p.Port
	}
	if primary, ok := svc.Spec.PrimaryPort(); ok && svc.Opts.Port != 0 {
		ports[primary.Name] = svc.Opts.Port
	}
	return &state.State{ContainerID: id, Ports: ports}, nil
}

func (r *dockerRuntime) isRunning(ctx context.Context, id string) bool {
	docker, err := r.client()
	if err != nil {
		return false
	}
	res, err := docker.ContainerInspect(ctx, id, mobyclient.ContainerInspectOptions{})
	return err == nil && res.Container.State != nil && res.Container.State.Running
}

// ensureContainer returns the id of the named service container, reusing a
// running one, starting a stopped one, or creating a new one via the sdk.
func (r *dockerRuntime) ensureContainer(ctx context.Context, svc *ServiceContext, data map[string]any, name string) (string, bool, error) {
	docker, err := r.client()
	if err != nil {
		return "", false, err
	}
	existing, err := docker.FindContainerByName(ctx, name)
	if err == nil && existing != nil {
		desired, configErr := r.DesiredConfig(ctx, svc)
		if configErr != nil {
			return "", false, configErr
		}
		encoded, configErr := encodeManagedConfig(desired)
		if configErr != nil {
			return "", false, configErr
		}
		if existing.Labels[managedConfigLabel] != encoded {
			if _, removeErr := docker.ContainerRemove(ctx, existing.ID, mobyclient.ContainerRemoveOptions{Force: true}); removeErr != nil {
				return "", false, fmt.Errorf("failed to replace unmanaged container %s: %w", name, removeErr)
			}
			existing = nil
		}
	}
	switch {
	case err == nil && existing != nil:
		if existing.State == mobycontainer.StateRunning {
			return existing.ID, true, nil
		}
		if _, err := docker.ContainerStart(ctx, existing.ID, mobyclient.ContainerStartOptions{}); err != nil {
			return "", false, fmt.Errorf("failed to start existing container %s: %w", name, err)
		}
		return existing.ID, false, nil
	case err != nil && !cerrdefs.IsNotFound(err):
		return "", false, err
	}

	opts, err := r.runOptions(svc, data, name)
	if err != nil {
		return "", false, err
	}
	ctr, err := sdkcontainer.Run(ctx, append(opts, sdkcontainer.WithClient(docker))...)
	if err != nil {
		return "", false, fmt.Errorf("failed to run container %s: %w", name, err)
	}
	return ctr.ID(), false, nil
}

// runOptions renders the docker spec into sdk run options.
func (r *dockerRuntime) runOptions(svc *ServiceContext, data map[string]any, name string) ([]sdkcontainer.ContainerCustomizer, error) {
	spec := svc.Spec.Docker
	desired, err := r.DesiredConfig(context.Background(), svc)
	if err != nil {
		return nil, err
	}
	imageRef := desired.Image
	managedConfig, err := encodeManagedConfig(desired)
	if err != nil {
		return nil, err
	}

	env, err := renderDockerEnvironment(svc, data)
	if err != nil {
		return nil, err
	}

	cmd, err := renderArguments("docker.cmd", append(append([]string{}, spec.Command...), spec.Args...), data)
	if err != nil {
		return nil, err
	}

	// bind loopback-only for a local daemon unless an explicit bind address
	// is set; a remote daemon must expose on all interfaces to be reachable
	var bindIP netip.Addr
	bind := svc.Opts.BindAddress
	if bind == "" && (svc.Host == "" || svc.Host == "localhost") {
		bind = "127.0.0.1"
	}
	if bind != "" && bind != "0.0.0.0" {
		if bindIP, err = netip.ParseAddr(bind); err != nil {
			return nil, fmt.Errorf("invalid bind address %q: %w", bind, err)
		}
	}

	var exposed []string
	bindings := mobynetwork.PortMap{}
	primary, _ := svc.Spec.PrimaryPort()
	for _, p := range svc.Spec.Ports {
		proto := p.Protocol
		if proto == "" {
			proto = "tcp"
		}
		port, err := mobynetwork.ParsePort(fmt.Sprintf("%d/%s", p.Port, proto))
		if err != nil {
			return nil, err
		}
		hostPort := p.Port
		if p.Name == primary.Name && svc.Opts.Port != 0 {
			hostPort = svc.Opts.Port
		}
		exposed = append(exposed, port.String())
		bindings[port] = []mobynetwork.PortBinding{{HostIP: bindIP, HostPort: fmt.Sprint(hostPort)}}
	}

	var binds []string
	tmpfs := map[string]string{}
	if desired.Volume != nil {
		switch VolumeMode(desired.Volume.Mode) {
		case VolumeHost, VolumePersistent:
			binds = append(binds, desired.Volume.Source+":"+desired.Volume.Target)
		case VolumeEphemeral:
			tmpfs[desired.Volume.Target] = "rw"
		}
	}
	for i, v := range spec.Volumes {
		rendered, err := render(fmt.Sprintf("docker.volumes[%d]", i), v, data)
		if err != nil {
			return nil, err
		}
		binds = append(binds, rendered)
	}

	opts := []sdkcontainer.ContainerCustomizer{
		sdkcontainer.WithImage(imageRef),
		sdkcontainer.WithName(name),
		sdkcontainer.WithEnv(env),
		sdkcontainer.WithLabels(map[string]string{serviceLabel: svc.Name, managedConfigLabel: managedConfig}),
		sdkcontainer.WithExposedPorts(exposed...),
		sdkcontainer.WithHostConfigModifier(func(hc *mobycontainer.HostConfig) {
			hc.PortBindings = bindings
			hc.Binds = binds
			hc.Tmpfs = tmpfs
			hc.Privileged = spec.Privileged
			hc.RestartPolicy = mobycontainer.RestartPolicy{Name: mobycontainer.RestartPolicyUnlessStopped}
		}),
	}
	if len(cmd) > 0 {
		opts = append(opts, sdkcontainer.WithCmd(cmd...))
	}
	return opts, nil
}

func protocol(value string) string {
	if value == "" {
		return "tcp"
	}
	return value
}

func renderDockerEnvironment(svc *ServiceContext, data map[string]any) (map[string]string, error) {
	env := map[string]string{}
	for name, value := range svc.Spec.Docker.Env {
		rendered, err := render("docker.env."+name, value, data)
		if err != nil {
			return nil, err
		}
		env[name] = rendered
	}
	for name, value := range svc.Opts.Env {
		env[name] = value
	}
	return env, nil
}

// execProbe runs a health command inside the container, rendering each
// argument, and reports success on exit code 0.
func (r *dockerRuntime) execProbe(ctx context.Context, id string, cmd []string, data map[string]any) bool {
	docker, err := r.client()
	if err != nil {
		return false
	}
	rendered := make([]string, len(cmd))
	for i, c := range cmd {
		if rendered[i], err = render(fmt.Sprintf("health.exec[%d]", i), c, data); err != nil {
			return false
		}
	}
	created, err := docker.ExecCreate(ctx, id, mobyclient.ExecCreateOptions{Cmd: rendered})
	if err != nil {
		return false
	}
	if _, err := docker.ExecStart(ctx, created.ID, mobyclient.ExecStartOptions{Detach: true}); err != nil {
		return false
	}
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		info, err := docker.ExecInspect(ctx, created.ID, mobyclient.ExecInspectOptions{})
		if err != nil {
			return false
		}
		if !info.Running {
			return info.ExitCode == 0
		}
		time.Sleep(200 * time.Millisecond)
	}
	return false
}

// Logs streams the container's demultiplexed logs; with follow it blocks
// until ctx is cancelled.
func (r *dockerRuntime) Logs(ctx context.Context, st *state.State, follow bool, w io.Writer) error {
	docker, err := r.client()
	if err != nil {
		return err
	}
	if st.ContainerID == "" {
		return fmt.Errorf("no container recorded for %s", st.Name)
	}
	reader, err := docker.ContainerLogs(ctx, st.ContainerID, mobyclient.ContainerLogsOptions{
		ShowStdout: true, ShowStderr: true, Follow: follow, Tail: "500",
	})
	if err != nil {
		return err
	}
	defer func() { _ = reader.Close() }()

	if res, err := docker.ContainerInspect(ctx, st.ContainerID, mobyclient.ContainerInspectOptions{}); err == nil &&
		res.Container.Config != nil && res.Container.Config.Tty {
		_, err = io.Copy(w, reader)
		return err
	}
	if _, err := stdcopy.StdCopy(w, w, reader); err != nil && ctx.Err() == nil {
		return err
	}
	return nil
}

// containerLogTail returns recent demultiplexed log output for error reports.
func (r *dockerRuntime) containerLogTail(ctx context.Context, id string) string {
	docker, err := r.client()
	if err != nil {
		return ""
	}
	reader, err := docker.ContainerLogs(ctx, id, mobyclient.ContainerLogsOptions{
		ShowStdout: true, ShowStderr: true, Tail: "50",
	})
	if err != nil {
		return ""
	}
	defer func() { _ = reader.Close() }()
	var buf strings.Builder
	_, _ = stdcopy.StdCopy(&buf, &buf, reader)
	return buf.String()
}

// Restart restarts the container in place via the docker API.
func (r *dockerRuntime) Restart(ctx context.Context, stateDir string, st *state.State) error {
	docker, err := r.client()
	if err != nil {
		return err
	}
	if st.ContainerID == "" {
		return fmt.Errorf("no container recorded for %s", st.Name)
	}
	timeout := 30
	if _, err := docker.ContainerRestart(ctx, st.ContainerID, mobyclient.ContainerRestartOptions{Timeout: &timeout}); err != nil {
		return fmt.Errorf("failed to restart container: %w", err)
	}
	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		if r.isRunning(ctx, st.ContainerID) {
			st.StartedAt = time.Now()
			if fresh, err := state.Load(stateDir, st.Name); err == nil {
				fresh.StartedAt = st.StartedAt
				_ = fresh.Save(stateDir)
			}
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(500 * time.Millisecond):
		}
	}
	return fmt.Errorf("container %s did not come back up within 60s", st.Name)
}

// Metrics samples the container's CPU and memory via the stats API.
func (r *dockerRuntime) Metrics(ctx context.Context, st *state.State) (*state.Resources, error) {
	docker, err := r.client()
	if err != nil {
		return nil, err
	}
	if st.ContainerID == "" {
		return nil, fmt.Errorf("no container recorded")
	}
	// IncludePreviousSample populates PreCPUStats so the CPU delta works
	res, err := docker.ContainerStats(ctx, st.ContainerID, mobyclient.ContainerStatsOptions{IncludePreviousSample: true})
	if err != nil {
		return nil, err
	}
	defer func() { _ = res.Body.Close() }()
	var stats mobycontainer.StatsResponse
	if err := json.NewDecoder(res.Body).Decode(&stats); err != nil {
		return nil, err
	}

	// same formula as docker stats: delta over the pre-sample, scaled to cpus
	var cpu float64
	cpuDelta := float64(stats.CPUStats.CPUUsage.TotalUsage - stats.PreCPUStats.CPUUsage.TotalUsage)
	sysDelta := float64(stats.CPUStats.SystemUsage - stats.PreCPUStats.SystemUsage)
	if cpuDelta > 0 && sysDelta > 0 {
		cpus := float64(stats.CPUStats.OnlineCPUs)
		if cpus == 0 {
			cpus = float64(len(stats.CPUStats.CPUUsage.PercpuUsage))
		}
		cpu = cpuDelta / sysDelta * cpus * 100
	}
	// docker CLI convention: usage minus the reclaimable page cache
	mem := stats.MemoryStats.Usage
	if cache, ok := stats.MemoryStats.Stats["inactive_file"]; ok && cache < mem {
		mem -= cache
	}

	var ports []int
	for _, p := range st.Ports {
		ports = append(ports, p)
	}
	sort.Ints(ports)
	return &state.Resources{
		CPUPercent: cpu,
		RSSBytes:   mem,
		PeakRSS:    stats.MemoryStats.MaxUsage,
		Ports:      ports,
		SampledAt:  time.Now(),
	}, nil
}

func (r *dockerRuntime) Stop(ctx context.Context, st *state.State) error {
	docker, err := r.client()
	if err != nil {
		return err
	}
	if st.ContainerID == "" {
		return fmt.Errorf("no container recorded for %s", st.Name)
	}
	timeout := 30
	_, err = docker.ContainerStop(ctx, st.ContainerID, mobyclient.ContainerStopOptions{Timeout: &timeout})
	return err
}

func (r *dockerRuntime) Status(ctx context.Context, st *state.State) (state.Status, error) {
	docker, err := r.client()
	if err != nil {
		return state.StatusUnknown, err
	}
	res, err := docker.ContainerInspect(ctx, st.ContainerID, mobyclient.ContainerInspectOptions{})
	if err != nil {
		if cerrdefs.IsNotFound(err) {
			return state.StatusStopped, nil
		}
		return state.StatusUnknown, err
	}
	if res.Container.State != nil && res.Container.State.Running {
		return state.StatusRunning, nil
	}
	return state.StatusStopped, nil
}
