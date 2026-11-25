package start

import (
	"archive/tar"
	"bytes"
	"context"
	"fmt"
	"io"
	"strings"

	cerrdefs "github.com/containerd/errdefs"
	mobyclient "github.com/moby/moby/client"
	"github.com/moby/moby/client/pkg/jsonmessage"
)

func ensureDockerImage(ctx context.Context, docker mobyclient.APIClient, svc *ServiceContext, image string) error {
	if _, err := docker.ImageInspect(ctx, image); err == nil {
		return nil
	} else if !cerrdefs.IsNotFound(err) {
		return fmt.Errorf("inspect image %s: %w", image, err)
	}

	var buildContext bytes.Buffer
	tarball := tar.NewWriter(&buildContext)
	dockerfile := svc.Spec.Docker.Dockerfile
	if err := tarball.WriteHeader(&tar.Header{Name: "Dockerfile", Mode: 0o644, Size: int64(len(dockerfile))}); err != nil {
		return err
	}
	if _, err := io.WriteString(tarball, dockerfile); err != nil {
		return err
	}
	if err := tarball.Close(); err != nil {
		return err
	}

	version := strings.TrimPrefix(svc.Version, "v")
	build, err := docker.ImageBuild(ctx, &buildContext, mobyclient.ImageBuildOptions{
		Tags: []string{image}, Remove: true, ForceRemove: true,
		BuildArgs: map[string]*string{"DEPS_SERVICE_VERSION": &version},
	})
	if err != nil {
		return fmt.Errorf("build image %s: %w", image, err)
	}
	defer func() { _ = build.Body.Close() }()
	w := svc.Opts.LogWriter
	if w == nil {
		w = io.Discard
	}
	if err := jsonmessage.DisplayJSONMessagesStream(build.Body, w, 0, false, nil); err != nil {
		return fmt.Errorf("build image %s: %w", image, err)
	}
	if _, err := docker.ImageInspect(ctx, image); err != nil {
		return fmt.Errorf("inspect built image %s: %w", image, err)
	}
	return nil
}
