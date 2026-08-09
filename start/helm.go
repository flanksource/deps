package start

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	osexec "os/exec"
	"path/filepath"
	"sort"
	"time"

	"github.com/flanksource/clicky/exec"
	"github.com/flanksource/deps"
	"github.com/flanksource/deps/start/state"
)

// helmRuntime installs the service as a helm release via the helm CLI
// (auto-installed through the deps registry).
type helmRuntime struct {
	helmPath string
}

func (r *helmRuntime) Kind() RuntimeKind { return RuntimeHelm }

func (r *helmRuntime) DesiredConfig(_ context.Context, svc *ServiceContext) (*state.EffectiveConfig, error) {
	spec := svc.Spec.Helm
	if spec == nil {
		return nil, fmt.Errorf("service %s has no helm runtime", svc.Name)
	}
	release, _, err := helmRelease(svc)
	if err != nil {
		return nil, err
	}
	config := &state.EffectiveConfig{
		Runtime: string(RuntimeHelm), Version: svc.Opts.Version, Chart: spec.Chart,
		Parameters: cloneStrings(svc.Opts.Parameters), Ports: configuredPorts(svc),
		Namespace: svc.Opts.Namespace,
	}
	if spec.ChartVersion != "" {
		config.Chart += "@" + spec.ChartVersion
	}
	if spec.Volume == nil {
		if svc.Opts.VolumeMode != "" {
			return nil, fmt.Errorf("service %s does not define a helm data volume", svc.Name)
		}
		return config, nil
	}
	mode, ok := spec.Volume.Modes[string(svc.Opts.VolumeMode)]
	if !ok {
		return nil, fmt.Errorf("service %s helm runtime does not support %s volume mode", svc.Name, svc.Opts.VolumeMode)
	}
	_ = mode
	config.Volume = &state.Volume{Mode: string(svc.Opts.VolumeMode), Target: spec.Volume.MountPath}
	switch svc.Opts.VolumeMode {
	case VolumeHost:
		config.Volume.Source = svc.DataDir
	case VolumePersistent:
		config.Volume.Source = release + "-data"
	case VolumeEphemeral:
	default:
		return nil, fmt.Errorf("unsupported helm volume mode %q", svc.Opts.VolumeMode)
	}
	return config, nil
}

func (r *helmRuntime) InspectConfig(ctx context.Context, svc *ServiceContext, prior *state.State) (*state.EffectiveConfig, error) {
	if prior.HelmRelease == "" {
		return nil, nil
	}
	helm, err := r.ensureHelm(ctx)
	if err != nil {
		return nil, err
	}
	proc := exec.NewExec(helm, "status", prior.HelmRelease, "--namespace", prior.Namespace, "-o", "json").
		WithTimeout(30 * time.Second).Run()
	result := proc.Result()
	if !result.IsOk() {
		return nil, nil
	}
	var status struct {
		Info struct {
			Description string `json:"description"`
		} `json:"info"`
	}
	if err := json.Unmarshal([]byte(result.Stdout), &status); err != nil {
		return nil, fmt.Errorf("failed to parse helm live config for %s: %w", svc.Name, err)
	}
	return decodeManagedConfig(status.Info.Description)
}

func (r *helmRuntime) ensureHelm(ctx context.Context) (string, error) {
	if r.helmPath != "" {
		return r.helmPath, nil
	}
	if path, err := osexec.LookPath("helm"); err == nil {
		r.helmPath = path
		return path, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	binDir := filepath.Join(home, ".deps", "bin")
	if _, err := deps.InstallWithContext(ctx, "helm", "latest", deps.WithBinDir(binDir)); err != nil {
		return "", fmt.Errorf("failed to install helm: %w", err)
	}
	r.helmPath = filepath.Join(binDir, "helm")
	return r.helmPath, nil
}

func (r *helmRuntime) Start(ctx context.Context, svc *ServiceContext) (*state.State, error) {
	helm, err := r.ensureHelm(ctx)
	if err != nil {
		return nil, err
	}

	release, data, err := helmRelease(svc)
	if err != nil {
		return nil, err
	}
	args, err := buildHelmArgs(svc, data, release)
	if err != nil {
		return nil, err
	}

	logf, err := os.OpenFile(svc.LogFile, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, err
	}
	defer func() { _ = logf.Close() }()

	out := serviceLogWriter(svc, logf)
	proc := exec.NewExec(helm, args...).Stream(out, out).Run()
	if result := proc.Result(); !result.IsOk() {
		return nil, fmt.Errorf("helm upgrade --install %s failed (exit %d):\n%s", release, result.ExitCode, tail(result.Stdout+"\n"+result.Stderr, 25))
	}

	primary, _ := svc.Spec.PrimaryPort()
	return &state.State{
		HelmRelease: release,
		Namespace:   svc.Opts.Namespace,
		Ports:       map[string]int{primary.Name: hostPort(svc)},
	}, nil
}

func (r *helmRuntime) Reconcile(ctx context.Context, svc *ServiceContext, _ *state.State) (*state.State, error) {
	return r.Start(ctx, svc)
}

// helmRelease renders the release name and returns the template data with
// {{.release}} populated.
func helmRelease(svc *ServiceContext) (string, map[string]any, error) {
	tmpl := svc.Spec.Helm.ReleaseName
	if tmpl == "" {
		tmpl = "deps-{{.name}}"
	}
	release, err := render("helm.release_name", tmpl, templateData(svc, "", ""))
	if err != nil {
		return "", nil, err
	}
	return release, templateData(svc, "", release), nil
}

// buildHelmArgs renders values/set pairs and assembles the helm upgrade
// --install argument list. Rendered values are written to the run dir.
func buildHelmArgs(svc *ServiceContext, data map[string]any, release string) ([]string, error) {
	spec := svc.Spec.Helm
	if spec.Volume != nil && svc.Opts.VolumeMode == "" {
		svc.Opts.VolumeMode = VolumePersistent
	}
	args := []string{"upgrade", "--install", release, spec.Chart,
		"--namespace", svc.Opts.Namespace, "--create-namespace",
		"--wait", "--timeout", svc.Opts.WaitTimeout.String()}
	if spec.Repo != "" {
		args = append(args, "--repo", spec.Repo)
	}
	if spec.ChartVersion != "" {
		args = append(args, "--version", spec.ChartVersion)
	}
	if spec.Values != "" {
		values, err := render("helm.values", spec.Values, data)
		if err != nil {
			return nil, err
		}
		valuesFile := filepath.Join(svc.RunDir, "values.yaml")
		if err := os.WriteFile(valuesFile, []byte(values), 0o600); err != nil {
			return nil, err
		}
		args = append(args, "-f", valuesFile)
	}
	desired, err := (&helmRuntime{}).DesiredConfig(context.Background(), svc)
	if err != nil {
		return nil, err
	}
	description, err := encodeManagedConfig(desired)
	if err != nil {
		return nil, err
	}
	args = append(args, "--description", description)

	args, err = appendHelmSet(args, "helm.set", spec.Set, data)
	if err != nil {
		return nil, err
	}
	if svc.Opts.Port != 0 {
		if len(spec.PortSet) == 0 {
			return nil, fmt.Errorf("helm chart %s does not define a primary port override", spec.Chart)
		}
		args, err = appendHelmSet(args, "helm.port_set", spec.PortSet, data)
		if err != nil {
			return nil, err
		}
	}
	if spec.Volume != nil {
		mode, ok := spec.Volume.Modes[string(svc.Opts.VolumeMode)]
		if !ok {
			return nil, fmt.Errorf("helm chart %s does not support %s volume mode", spec.Chart, svc.Opts.VolumeMode)
		}
		args, err = appendHelmSet(args, "helm.volume."+string(svc.Opts.VolumeMode), mode.Set, data)
		if err != nil {
			return nil, err
		}
	}
	return args, nil
}

func appendHelmSet(args []string, source string, values map[string]string, data map[string]any) ([]string, error) {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		rendered, err := render(source+"."+key, values[key], data)
		if err != nil {
			return nil, err
		}
		if rendered == "" {
			continue
		}
		args = append(args, "--set", key+"="+rendered)
	}
	return args, nil
}

func (r *helmRuntime) Stop(ctx context.Context, st *state.State) error {
	helm, err := r.ensureHelm(ctx)
	if err != nil {
		return err
	}
	proc := exec.NewExec(helm, "uninstall", st.HelmRelease, "--namespace", st.Namespace, "--wait").
		WithTimeout(2 * time.Minute).Run()
	if result := proc.Result(); !result.IsOk() {
		return fmt.Errorf("helm uninstall %s failed (exit %d): %s", st.HelmRelease, result.ExitCode, tail(result.Stderr, 10))
	}
	return nil
}

func (r *helmRuntime) Status(ctx context.Context, st *state.State) (state.Status, error) {
	helm, err := r.ensureHelm(ctx)
	if err != nil {
		return state.StatusUnknown, err
	}
	proc := exec.NewExec(helm, "status", st.HelmRelease, "--namespace", st.Namespace, "-o", "json").
		WithTimeout(30 * time.Second).Run()
	result := proc.Result()
	if !result.IsOk() {
		return state.StatusStopped, nil
	}
	var status struct {
		Info struct {
			Status string `json:"status"`
		} `json:"info"`
	}
	if err := json.Unmarshal([]byte(result.Stdout), &status); err != nil {
		return state.StatusUnknown, fmt.Errorf("failed to parse helm status: %w", err)
	}
	if status.Info.Status == "deployed" {
		return state.StatusRunning, nil
	}
	return state.StatusStopped, nil
}
