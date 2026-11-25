package start

import (
	"archive/tar"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"

	mobyclient "github.com/moby/moby/client"
)

func (r *dockerRuntime) exportFiles(ctx context.Context, svc *ServiceContext, id string) error {
	if len(svc.Spec.Docker.ExportFiles) == 0 {
		return nil
	}
	docker, err := r.client()
	if err != nil {
		return err
	}
	data := templateData(svc, fmt.Sprintf("%s:%d", svc.serviceHost(), hostPort(svc)), "")
	for name, tmpl := range svc.Spec.Docker.ExportFiles {
		if !filepath.IsLocal(name) || filepath.Base(name) != name || name == "." {
			return fmt.Errorf("docker export filename %q must be a filename under the service run directory", name)
		}
		source, err := render("docker.export_files."+name, tmpl, data)
		if err != nil {
			return err
		}
		if !path.IsAbs(source) {
			return fmt.Errorf("docker export source %q must be absolute", source)
		}
		result, err := docker.CopyFromContainer(ctx, id, mobyclient.CopyFromContainerOptions{SourcePath: source})
		if err != nil {
			return fmt.Errorf("export %s from container: %w", source, err)
		}
		err = writeDockerExport(svc.RunDir, name, result.Content)
		if err := errors.Join(err, result.Content.Close()); err != nil {
			return fmt.Errorf("export %s from container: %w", source, err)
		}
	}
	return nil
}

func writeDockerExport(dir, name string, content io.Reader) error {
	archive := tar.NewReader(content)
	header, err := archive.Next()
	if err != nil {
		return err
	}
	if header.Typeflag != tar.TypeReg || header.Size > 1<<20 {
		return fmt.Errorf("expected a regular file of at most 1 MiB, got type %q and size %d", header.Typeflag, header.Size)
	}
	file, err := os.CreateTemp(dir, ".docker-export-*")
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(file.Name()) }()
	_, err = io.Copy(file, archive)
	if err := errors.Join(err, file.Close()); err != nil {
		return err
	}
	return os.Rename(file.Name(), filepath.Join(dir, name))
}
