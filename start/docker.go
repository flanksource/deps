package start

import (
	"context"
	"fmt"
	"io"
	"time"

	cerrdefs "github.com/containerd/errdefs"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/client"
	"github.com/docker/go-connections/nat"
	"github.com/flanksource/deps/start/state"
)

const serviceLabel = "flanksource.deps/service"

// dockerRuntime runs the service as a container via the docker SDK, using an
// idempotent named container deps-start-<service>.
type dockerRuntime struct {
	docker *client.Client
}

func (r *dockerRuntime) Kind() RuntimeKind { return RuntimeDocker }

func (r *dockerRuntime) client() (*client.Client, error) {
	if r.docker != nil {
		return r.docker, nil
	}
	c, err := newDockerClient()
	if err != nil {
		return nil, fmt.Errorf("failed to create docker client: %w", err)
	}
	r.docker = c
	return c, nil
}

func containerName(service string) string { return "deps-start-" + service }

func (r *dockerRuntime) Start(ctx context.Context, svc *ServiceContext) (*state.State, error) {
	spec := svc.Spec.Docker
	if err := checkPlatform(spec.Platforms, svc.OS, svc.Arch); err != nil {
		return nil, err
	}
	docker, err := r.client()
	if err != nil {
		return nil, err
	}
	if svc.Version == "" {
		svc.Version = "latest"
	}
	svc.Host = daemonHost(docker)
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
		alive: func() bool {
			info, err := docker.ContainerInspect(ctx, id)
			return err == nil && info.State != nil && info.State.Running
		},
		output: func() string { return r.containerLogs(ctx, id) },
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

// ensureContainer returns the id of the named service container, reusing a
// running one, starting a stopped one, or pulling+creating a new one.
func (r *dockerRuntime) ensureContainer(ctx context.Context, svc *ServiceContext, data map[string]any, name string) (string, bool, error) {
	docker, err := r.client()
	if err != nil {
		return "", false, err
	}
	if info, err := docker.ContainerInspect(ctx, name); err == nil {
		if info.State != nil && info.State.Running {
			return info.ID, true, nil
		}
		if err := docker.ContainerStart(ctx, info.ID, container.StartOptions{}); err != nil {
			return "", false, fmt.Errorf("failed to start existing container %s: %w", name, err)
		}
		return info.ID, false, nil
	}

	config, hostConfig, err := r.containerConfig(svc, data, name)
	if err != nil {
		return "", false, err
	}
	reader, err := docker.ImagePull(ctx, config.Image, image.PullOptions{})
	if err != nil {
		return "", false, fmt.Errorf("failed to pull %s: %w", config.Image, err)
	}
	_, _ = io.Copy(io.Discard, reader)
	_ = reader.Close()

	created, err := docker.ContainerCreate(ctx, config, hostConfig, nil, nil, name)
	if err != nil {
		return "", false, fmt.Errorf("failed to create container %s: %w", name, err)
	}
	if err := docker.ContainerStart(ctx, created.ID, container.StartOptions{}); err != nil {
		return "", false, fmt.Errorf("failed to start container %s: %w", name, err)
	}
	return created.ID, false, nil
}

func (r *dockerRuntime) containerConfig(svc *ServiceContext, data map[string]any, name string) (*container.Config, *container.HostConfig, error) {
	spec := svc.Spec.Docker
	imageRef, err := render("docker.image", spec.Image, data)
	if err != nil {
		return nil, nil, err
	}

	var env []string
	for k, v := range spec.Env {
		rendered, err := render("docker.env."+k, v, data)
		if err != nil {
			return nil, nil, err
		}
		env = append(env, k+"="+rendered)
	}
	for k, v := range svc.Opts.Env {
		env = append(env, k+"="+v)
	}

	var cmd []string
	for i, c := range append(append([]string{}, spec.Command...), spec.Args...) {
		rendered, err := render(fmt.Sprintf("docker.cmd[%d]", i), c, data)
		if err != nil {
			return nil, nil, err
		}
		cmd = append(cmd, rendered)
	}

	exposed := nat.PortSet{}
	bindings := nat.PortMap{}
	// bind loopback-only for a local daemon; a remote daemon must expose on
	// all interfaces to be reachable from here
	bindIP := "127.0.0.1"
	if svc.Host != "" && svc.Host != "localhost" {
		bindIP = ""
	}
	primary, _ := svc.Spec.PrimaryPort()
	for _, p := range svc.Spec.Ports {
		proto := p.Protocol
		if proto == "" {
			proto = "tcp"
		}
		port, err := nat.NewPort(proto, fmt.Sprint(p.Port))
		if err != nil {
			return nil, nil, err
		}
		hostPort := p.Port
		if p.Name == primary.Name && svc.Opts.Port != 0 {
			hostPort = svc.Opts.Port
		}
		exposed[port] = struct{}{}
		bindings[port] = []nat.PortBinding{{HostIP: bindIP, HostPort: fmt.Sprint(hostPort)}}
	}

	// a named volume (not a host bind) so data survives on remote daemons too
	var binds []string
	if spec.DataPath != "" {
		binds = append(binds, name+"-data:"+spec.DataPath)
	}
	for i, v := range spec.Volumes {
		rendered, err := render(fmt.Sprintf("docker.volumes[%d]", i), v, data)
		if err != nil {
			return nil, nil, err
		}
		binds = append(binds, rendered)
	}

	config := &container.Config{
		Image:        imageRef,
		Env:          env,
		Cmd:          cmd,
		ExposedPorts: exposed,
		Labels:       map[string]string{serviceLabel: svc.Name},
	}
	hostConfig := &container.HostConfig{
		PortBindings:  bindings,
		Binds:         binds,
		RestartPolicy: container.RestartPolicy{Name: container.RestartPolicyUnlessStopped},
	}
	return config, hostConfig, nil
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
	exec, err := docker.ContainerExecCreate(ctx, id, container.ExecOptions{Cmd: rendered})
	if err != nil {
		return false
	}
	if err := docker.ContainerExecStart(ctx, exec.ID, container.ExecStartOptions{}); err != nil {
		return false
	}
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		info, err := docker.ContainerExecInspect(ctx, exec.ID)
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

func (r *dockerRuntime) containerLogs(ctx context.Context, id string) string {
	docker, err := r.client()
	if err != nil {
		return ""
	}
	reader, err := docker.ContainerLogs(ctx, id, container.LogsOptions{ShowStdout: true, ShowStderr: true, Tail: "50"})
	if err != nil {
		return ""
	}
	defer func() { _ = reader.Close() }()
	data, _ := io.ReadAll(reader)
	return string(data)
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
	return docker.ContainerStop(ctx, st.ContainerID, container.StopOptions{Timeout: &timeout})
}

func (r *dockerRuntime) Status(ctx context.Context, st *state.State) (state.Status, error) {
	docker, err := r.client()
	if err != nil {
		return state.StatusUnknown, err
	}
	info, err := docker.ContainerInspect(ctx, st.ContainerID)
	if err != nil {
		if cerrdefs.IsNotFound(err) {
			return state.StatusStopped, nil
		}
		return state.StatusUnknown, err
	}
	if info.State != nil && info.State.Running {
		return state.StatusRunning, nil
	}
	return state.StatusStopped, nil
}
