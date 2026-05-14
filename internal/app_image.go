package internal

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/spf13/cobra"
)

type imageStatus struct {
	Runtime     string
	Tag         string
	Expected    string
	Built       string
	Status      string
	ImageExists bool
}

func (a *App) imageCommand() *cobra.Command {
	var runtimeName string
	cmd := &cobra.Command{
		Use:   "image",
		Short: "Manage Ark base images",
	}
	cmd.PersistentFlags().StringVar(&runtimeName, "runtime", a.config.Runtime, "runtime: auto, apple, or docker")
	cmd.AddCommand(&cobra.Command{
		Use:   "status",
		Short: "Show Ark base image status",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.ImageStatus(cmd.Context(), runtimeName)
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "rebuild",
		Short: "Rebuild the reusable Ark base image",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.ImageRebuild(cmd.Context(), runtimeName)
		},
	})
	return cmd
}

func (a *App) rebuildCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "rebuild <name>",
		Short: "Recreate one project container from the current base image",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.RebuildProject(cmd.Context(), args[0])
		},
	}
}

func (a *App) ImageStatus(ctx context.Context, requestedRuntime string) error {
	status, err := a.resolveImageStatus(ctx, requestedRuntime)
	if err != nil {
		return err
	}
	fmt.Fprintf(a.out, "Runtime: %s\n", status.Runtime)
	fmt.Fprintf(a.out, "Image:   %s\n", status.Tag)
	fmt.Fprintf(a.out, "Status:  %s\n\n", status.Status)
	fmt.Fprintf(a.out, "Expected: %s\n", status.Expected)
	if status.Built == "" {
		fmt.Fprintln(a.out, "Built:    <none>")
	} else {
		fmt.Fprintf(a.out, "Built:    %s\n", status.Built)
	}
	if status.Status != "current" {
		fmt.Fprintln(a.out, "")
		fmt.Fprintln(a.out, "Run: ark image rebuild")
	}
	return nil
}

func (a *App) ImageRebuild(ctx context.Context, requestedRuntime string) error {
	rt, selectedRuntime, err := ResolveRuntime(ctx, requestedRuntime)
	if err != nil {
		return err
	}
	if err := rt.Available(ctx); err != nil {
		return err
	}
	fingerprint, err := a.expectedImageFingerprint(selectedRuntime)
	if err != nil {
		return err
	}
	if err := BuildBaseImage(ctx, rt, a.config, a.out, a.errOut); err != nil {
		return err
	}
	if err := a.recordBuiltImage(ctx, fingerprint); err != nil {
		return err
	}
	fmt.Fprintf(a.out, "Updated image metadata for %s: %s\n", selectedRuntime, fingerprint)
	return nil
}

func (a *App) RebuildProject(ctx context.Context, name string) error {
	project, rt, err := a.projectRuntime(ctx, name)
	if err != nil {
		return err
	}
	if err := a.prepareProjectMounts(project); err != nil {
		return err
	}
	imageInfo, err := a.ensureBaseImage(ctx, rt, project.Runtime)
	if err != nil {
		return err
	}
	if err := rt.Stop(ctx, project.ContainerName, 10); err != nil && !errors.Is(err, ErrNotFound) {
		return err
	}
	if err := rt.Remove(ctx, project.ContainerName, true); err != nil && !errors.Is(err, ErrNotFound) {
		return err
	}
	for _, volumeName := range projectCreateVolumeNames(project) {
		if err := rt.CreateVolume(ctx, volumeName); err != nil {
			return err
		}
	}
	project.Image = a.config.Image.Tag
	project.ImageFingerprint = imageInfo.Fingerprint
	if _, err := rt.Create(ctx, CreateSpec{
		Name:          project.ContainerName,
		Image:         project.Image,
		ProjectName:   project.Name,
		ProjectID:     project.ID,
		ProjectPath:   project.Path,
		Workdir:       a.config.Container.Workdir,
		Env:           ProjectEnv(project, a.config),
		Mounts:        a.projectMounts(project),
		DockerEnabled: project.DockerEnabled,
		Privileged:    a.config.Container.Privileged,
		Network:       true,
	}); err != nil {
		return err
	}
	if err := a.registry.Update(ctx, func(state *State) error {
		if _, ok := state.Projects[name]; !ok {
			return fmt.Errorf("project %q: %w", name, ErrNotFound)
		}
		project.LastUsedAt = time.Now().UTC()
		state.Projects[name] = project
		return nil
	}); err != nil {
		return err
	}
	fmt.Fprintf(a.out, "Rebuilt project %s from %s\n", name, project.Image)
	return nil
}

func (a *App) ensureBaseImage(ctx context.Context, rt Runtime, runtimeName string) (ImageInfo, error) {
	status, err := a.resolveImageStatusForRuntime(ctx, rt, runtimeName)
	if err != nil {
		return ImageInfo{}, err
	}
	switch status.Status {
	case "current":
		return ImageInfo{
			Tag:         status.Tag,
			Fingerprint: status.Built,
		}, nil
	case "missing":
		if !a.config.Image.AutoBuild {
			return ImageInfo{}, fmt.Errorf("image %s is missing; run ark image rebuild", status.Tag)
		}
		fmt.Fprintf(a.out, "Image %s is missing; building it now\n", status.Tag)
		if err := BuildBaseImage(ctx, rt, a.config, a.out, a.errOut); err != nil {
			return ImageInfo{}, err
		}
		if err := a.recordBuiltImage(ctx, status.Expected); err != nil {
			return ImageInfo{}, err
		}
		return ImageInfo{Tag: status.Tag, Fingerprint: status.Expected}, nil
	case "stale":
		if a.config.Image.AutoRebuild {
			fmt.Fprintf(a.out, "Image %s is stale; rebuilding it now\n", status.Tag)
			if err := BuildBaseImage(ctx, rt, a.config, a.out, a.errOut); err != nil {
				return ImageInfo{}, err
			}
			if err := a.recordBuiltImage(ctx, status.Expected); err != nil {
				return ImageInfo{}, err
			}
			return ImageInfo{Tag: status.Tag, Fingerprint: status.Expected}, nil
		}
		fmt.Fprintf(a.errOut, "Ark base image %s is stale.\nRun: ark image rebuild\n", status.Tag)
		return ImageInfo{Tag: status.Tag, Fingerprint: status.Built}, nil
	default:
		return ImageInfo{}, fmt.Errorf("unknown image status %q", status.Status)
	}
}

func (a *App) resolveImageStatus(ctx context.Context, requestedRuntime string) (imageStatus, error) {
	rt, selectedRuntime, err := ResolveRuntime(ctx, requestedRuntime)
	if err != nil {
		return imageStatus{}, err
	}
	if err := rt.Available(ctx); err != nil {
		return imageStatus{}, err
	}
	return a.resolveImageStatusForRuntime(ctx, rt, selectedRuntime)
}

func (a *App) resolveImageStatusForRuntime(ctx context.Context, rt Runtime, runtimeName string) (imageStatus, error) {
	expected, err := a.expectedImageFingerprint(runtimeName)
	if err != nil {
		return imageStatus{}, err
	}
	tag := a.config.Image.Tag
	exists, err := rt.ImageExists(ctx, tag)
	if err != nil {
		return imageStatus{}, err
	}
	state, err := a.images.Load(ctx)
	if err != nil {
		return imageStatus{}, err
	}
	built := ""
	if state.Image.Tag == tag {
		built = state.Image.Fingerprint
	}
	status := "stale"
	if !exists {
		status = "missing"
	} else if built == expected {
		status = "current"
	}
	return imageStatus{
		Runtime:     runtimeName,
		Tag:         tag,
		Expected:    expected,
		Built:       built,
		Status:      status,
		ImageExists: exists,
	}, nil
}

func (a *App) expectedImageFingerprint(runtimeName string) (string, error) {
	source, err := a.config.ImageSourcePath()
	if err != nil {
		return "", err
	}
	return computeImageFingerprint(runtimeName, ArkVersion, source)
}

func (a *App) recordBuiltImage(ctx context.Context, fingerprint string) error {
	return a.images.Update(ctx, func(state *ImageState) error {
		state.Image = ImageInfo{
			Tag:         a.config.Image.Tag,
			Fingerprint: fingerprint,
			BuiltAt:     time.Now().UTC(),
		}
		return nil
	})
}

func (a *App) warnProjectImageStale(ctx context.Context, project Project) error {
	expected, err := a.expectedImageFingerprint(project.Runtime)
	if err != nil {
		return err
	}
	if project.ImageFingerprint == "" || project.ImageFingerprint == expected {
		return nil
	}
	fmt.Fprintf(a.errOut, "Project %s was created with an older Ark base image.\nRun: ark rebuild %s\n", project.Name, project.Name)
	return nil
}

func (a *App) prepareProjectMounts(project Project) error {
	return ensureProjectControlPlane(a.paths, project)
}

func (a *App) projectMounts(project Project) []MountSpec {
	return appendProjectControlPlaneMounts(ProjectMounts(project), a.paths, project)
}
