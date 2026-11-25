package start

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strconv"

	cerrdefs "github.com/containerd/errdefs"
	mobyclient "github.com/moby/moby/client"

	"github.com/flanksource/deps/pkg/version"
	"github.com/flanksource/deps/start/state"
)

func (r *dockerRuntime) DesiredConfig(ctx context.Context, svc *ServiceContext) (*state.EffectiveConfig, error) {
	spec := svc.Spec.Docker
	if spec == nil {
		return nil, fmt.Errorf("service %s has no docker runtime", svc.Name)
	}
	host, err := r.daemonHost(ctx)
	if err != nil {
		return nil, err
	}
	svc.Host = host
	if spec.DataPath == "" {
		if svc.Opts.VolumeMode != "" {
			return nil, fmt.Errorf("service %s does not define a docker data volume", svc.Name)
		}
	} else if svc.Opts.VolumeMode == "" {
		return nil, fmt.Errorf("docker volume mode was not resolved for %s", svc.Name)
	}
	switch {
	case spec.ResolveVersion && svc.Tag != "":
	case spec.ResolveVersion:
		if svc.Version, err = resolveServiceVersion(ctx, svc, svc.Version); err != nil {
			return nil, err
		}
	case svc.Version == "" && spec.DefaultVersion != "":
		svc.Version = spec.DefaultVersion
	// A concrete version is already a valid image tag; the package manager
	// tracks binary releases (e.g. Maven for postgres), not registry tags.
	case version.LooksLikeExactVersion(svc.Version) || version.IsPartialVersion(svc.Version):
	default:
		if svc.Version, err = resolveServiceVersion(ctx, svc, svc.Version); err != nil {
			return nil, err
		}
	}
	data := templateData(svc, fmt.Sprintf("%s:%d", svc.serviceHost(), hostPort(svc)), "")
	image, err := render("docker.image", spec.Image, data)
	if err != nil {
		return nil, err
	}
	if spec.Dockerfile != "" {
		if !version.LooksLikeExactVersion(svc.Version) {
			return nil, fmt.Errorf("building the %s image requires an exact version, got %q", svc.Name, svc.Version)
		}
		image += fmt.Sprintf(":%x", sha256.Sum256([]byte(svc.Version+"\x00"+spec.Dockerfile)))
	}
	config := &state.EffectiveConfig{
		Runtime: string(RuntimeDocker), Version: svc.Version, Image: image,
		Parameters: cloneStrings(svc.Opts.Parameters), Ports: configuredPorts(svc),
		Bind: dockerBindAddress(svc),
	}
	for path, content := range spec.Files {
		rendered, err := render("docker.files."+path, content, data)
		if err != nil {
			return nil, err
		}
		if rendered == "" {
			continue
		}
		if config.Files == nil {
			config.Files = map[string]string{}
		}
		config.Files[path] = fileDigest(rendered)
	}
	if spec.DataPath != "" {
		config.Volume = &state.Volume{Mode: string(svc.Opts.VolumeMode), Target: spec.DataPath}
		switch svc.Opts.VolumeMode {
		case VolumeHost:
			config.Volume.Source = svc.DataDir
		case VolumePersistent:
			config.Volume.Source = containerName(svc.Name, svc.Opts.InstanceID) + "-data"
		case VolumeEphemeral:
		default:
			return nil, fmt.Errorf("unsupported docker volume mode %q", svc.Opts.VolumeMode)
		}
	}
	return config, nil
}

func (r *dockerRuntime) InspectConfig(ctx context.Context, svc *ServiceContext, prior *state.State) (*state.EffectiveConfig, error) {
	docker, err := r.client()
	if err != nil {
		return nil, err
	}
	id := prior.ContainerID
	if id == "" {
		id = containerName(svc.Name, svc.Opts.InstanceID)
	}
	res, err := docker.ContainerInspect(ctx, id, mobyclient.ContainerInspectOptions{})
	if cerrdefs.IsNotFound(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to inspect docker config for %s: %w", svc.Name, err)
	}
	var config *state.EffectiveConfig
	if res.Container.Config != nil {
		if encoded := res.Container.Config.Labels[managedConfigLabel]; encoded != "" {
			config, err = decodeManagedConfig(encoded)
			if err != nil {
				return nil, err
			}
		}
	}
	if config == nil {
		config = &state.EffectiveConfig{Runtime: string(RuntimeDocker), Parameters: map[string]string{}}
	}
	if prior.StartOptions != nil && len(config.Parameters) == 0 {
		config.Parameters = cloneStrings(prior.StartOptions.Parameters)
	}
	managedPorts := config.Ports
	config.Ports = map[string]int{}
	if res.Container.Config != nil {
		config.Image = res.Container.Config.Image
	}
	ports := configuredPorts(svc)
	for _, port := range svc.Spec.Ports {
		containerPort := port.Port
		if svc.Spec.Docker.MatchHostPorts {
			containerPort = ports[port.Name]
			if managedPort := managedPorts[port.Name]; managedPort != 0 {
				containerPort = managedPort
			}
		}
		key := fmt.Sprintf("%d/%s", containerPort, protocol(port.Protocol))
		for exposed, bindings := range res.Container.HostConfig.PortBindings {
			if exposed.String() != key || len(bindings) == 0 {
				continue
			}
			value, parseErr := strconv.Atoi(bindings[0].HostPort)
			if parseErr != nil {
				return nil, fmt.Errorf("invalid live docker port %q for %s: %w", bindings[0].HostPort, svc.Name, parseErr)
			}
			config.Ports[port.Name] = value
			if bindings[0].HostIP.IsValid() {
				config.Bind = bindings[0].HostIP.String()
			} else {
				config.Bind = "0.0.0.0"
			}
		}
	}
	if svc.Spec.Docker.DataPath != "" {
		config.Volume = nil
		for _, mount := range res.Container.Mounts {
			if mount.Destination != svc.Spec.Docker.DataPath {
				continue
			}
			config.Volume = &state.Volume{Target: mount.Destination, Source: mount.Source}
			switch string(mount.Type) {
			case "bind":
				config.Volume.Mode = string(VolumeHost)
			case "volume":
				config.Volume.Mode, config.Volume.Source = string(VolumePersistent), mount.Name
			case "tmpfs":
				config.Volume.Mode, config.Volume.Source = string(VolumeEphemeral), ""
			default:
				return nil, fmt.Errorf("unsupported live docker mount type %q for %s", mount.Type, svc.Name)
			}
		}
	}
	return config, nil
}

// fileDigest is a change-detection fingerprint of rendered file content, which
// may embed credentials. It is an HMAC rather than a bare hash: it is never
// used to store or verify passwords.
func fileDigest(content string) string {
	mac := hmac.New(sha256.New, []byte("deps-start-file-digest"))
	mac.Write([]byte(content))
	return hex.EncodeToString(mac.Sum(nil))
}

func dockerBindAddress(svc *ServiceContext) string {
	if svc.Opts.BindAddress != "" {
		return svc.Opts.BindAddress
	}
	if svc.Host == "" || svc.Host == "localhost" {
		return "127.0.0.1"
	}
	return "0.0.0.0"
}
