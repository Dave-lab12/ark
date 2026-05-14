package internal

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"golang.org/x/term"
)

type App struct {
	paths    Paths
	config   Config
	registry *Registry
	images   *ImageStore
	in       io.Reader
	out      io.Writer
	errOut   io.Writer
	stdin    *os.File
	stdout   *os.File
}

type InitOptions struct {
	Runtime       string
	SSHEnabled    bool
	DockerEnabled bool
	Enter         bool
	AssumeYes     bool
}

func NewApp(in io.Reader, out, errOut io.Writer) (*App, error) {
	paths, err := DefaultPaths()
	if err != nil {
		return nil, err
	}
	if err := paths.EnsureControlPlane(); err != nil {
		return nil, err
	}
	if err := EnsureDefaultConfig(paths); err != nil {
		return nil, err
	}
	config, err := LoadConfig(paths)
	if err != nil {
		return nil, err
	}
	if err := EnsureImageSource(config); err != nil {
		return nil, err
	}
	projectRoot, err := config.ProjectRootPath()
	if err != nil {
		return nil, err
	}
	paths.ProjectRoot = projectRoot
	app := &App{
		paths:    paths,
		config:   config,
		registry: NewRegistry(paths),
		images:   NewImageStore(paths),
		in:       in,
		out:      out,
		errOut:   errOut,
	}
	if file, ok := in.(*os.File); ok {
		app.stdin = file
	}
	if file, ok := out.(*os.File); ok {
		app.stdout = file
	}
	return app, nil
}

func (a *App) Prepare(ctx context.Context) error {
	if err := a.paths.EnsureControlPlane(); err != nil {
		return err
	}
	if err := EnsureDefaultConfig(a.paths); err != nil {
		return err
	}
	if err := EnsureImageSource(a.config); err != nil {
		return err
	}
	if err := a.registry.EnsureDefault(ctx); err != nil {
		return err
	}
	if err := a.images.EnsureDefault(ctx); err != nil {
		return err
	}
	if _, err := LoadConfig(a.paths); err != nil {
		return err
	}
	if _, err := a.registry.Load(ctx); err != nil {
		return err
	}
	if _, err := a.images.Load(ctx); err != nil {
		return err
	}
	return nil
}

func (a *App) InitProject(ctx context.Context, name string, opts InitOptions) error {
	if err := ValidateProjectName(name); err != nil {
		return err
	}
	rt, selectedRuntime, err := ResolveRuntime(ctx, opts.Runtime)
	if err != nil {
		return err
	}
	if err := rt.Available(ctx); err != nil {
		return err
	}
	state, err := a.registry.Load(ctx)
	if err != nil {
		return err
	}
	if _, exists := state.Projects[name]; exists {
		return fmt.Errorf("project %q already exists", name)
	}

	projectPath, err := a.paths.ProjectPath(name)
	if err != nil {
		return err
	}
	nonEmpty, err := DirExistsNonEmpty(projectPath)
	if err != nil {
		return err
	}
	if nonEmpty && !opts.AssumeYes {
		ok, err := a.confirm(fmt.Sprintf("Directory %s already exists and is not empty. Use it for this project?", projectPath))
		if err != nil {
			return err
		}
		if !ok {
			return errors.New("init canceled")
		}
	}
	if err := os.MkdirAll(projectPath, 0o755); err != nil {
		return fmt.Errorf("create project directory %s: %w", projectPath, err)
	}

	imageInfo, err := a.ensureBaseImage(ctx, rt, selectedRuntime)
	if err != nil {
		return err
	}
	project, err := NewProject(name, selectedRuntime, projectPath, a.config.Image.Tag, imageInfo.Fingerprint, opts.SSHEnabled, opts.DockerEnabled)
	if err != nil {
		return err
	}

	if err := a.prepareProjectMounts(project); err != nil {
		return err
	}
	volumeNames := projectCreateVolumeNames(project)
	createdVolumes := make([]string, 0, len(volumeNames))
	for _, volumeName := range volumeNames {
		if err := rt.CreateVolume(ctx, volumeName); err != nil {
			a.cleanupVolumes(ctx, rt, createdVolumes)
			return err
		}
		createdVolumes = append(createdVolumes, volumeName)
	}

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
		a.cleanupVolumes(ctx, rt, createdVolumes)
		return err
	}

	if err := a.registry.Update(ctx, func(state *State) error {
		if _, exists := state.Projects[name]; exists {
			return fmt.Errorf("project %q was created concurrently", name)
		}
		state.Projects[name] = project
		return nil
	}); err != nil {
		if rmErr := rt.Remove(ctx, project.ContainerName, true); rmErr != nil && !errors.Is(rmErr, ErrNotFound) {
			slog.Warn("cleanup container after init failure", "container", project.ContainerName, "error", rmErr)
		}
		a.cleanupVolumes(ctx, rt, createdVolumes)
		return err
	}

	fmt.Fprintf(a.out, "Created project %s at %s using %s\n", project.Name, project.Path, project.Runtime)
	if opts.Enter && a.isInteractive() {
		return a.RunProject(ctx, project.Name, nil)
	}
	fmt.Fprintf(a.out, "Enter it with: ark %s\n", project.Name)
	return nil
}

func (a *App) StartProject(ctx context.Context, name string, enter bool) error {
	project, rt, err := a.projectRuntime(ctx, name)
	if err != nil {
		return err
	}
	if err := a.warnProjectImageStale(ctx, project); err != nil {
		return err
	}
	if err := a.ensureStarted(ctx, rt, project); err != nil {
		return err
	}
	if err := a.touchProject(ctx, name); err != nil {
		return err
	}
	if enter {
		return a.execProject(ctx, rt, project, nil)
	}
	fmt.Fprintf(a.out, "Started %s\n", name)
	return nil
}

func (a *App) StopProject(ctx context.Context, name string) error {
	project, rt, err := a.projectRuntime(ctx, name)
	if err != nil {
		return err
	}
	if err := rt.Stop(ctx, project.ContainerName, 10); err != nil {
		return err
	}
	fmt.Fprintf(a.out, "Stopped %s\n", name)
	return nil
}

func (a *App) RemoveProject(ctx context.Context, name string, force bool) error {
	project, rt, err := a.projectRuntime(ctx, name)
	if err != nil {
		return err
	}
	if !force {
		ok, err := a.confirm(fmt.Sprintf("Remove project %s container and volumes? The project directory will remain at %s.", name, project.Path))
		if err != nil {
			return err
		}
		if !ok {
			return errors.New("remove canceled")
		}
	}
	if err := rt.Remove(ctx, project.ContainerName, true); err != nil && !errors.Is(err, ErrNotFound) {
		return err
	}
	for _, volumeName := range projectVolumeNames(project) {
		if err := rt.RemoveVolume(ctx, volumeName); err != nil {
			return err
		}
	}
	if err := a.registry.Update(ctx, func(state *State) error {
		delete(state.Projects, name)
		return nil
	}); err != nil {
		return err
	}
	fmt.Fprintf(a.out, "Removed %s\n", name)
	return nil
}

func (a *App) ListProjects(ctx context.Context) error {
	state, err := a.registry.Load(ctx)
	if err != nil {
		return err
	}
	if len(state.Projects) == 0 {
		fmt.Fprintln(a.out, "No ark projects yet.")
		return nil
	}
	fmt.Fprintf(a.out, "%-18s %-8s %-12s %s\n", "NAME", "RUNTIME", "STATUS", "PATH")
	names := make([]string, 0, len(state.Projects))
	for name := range state.Projects {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		project := state.Projects[name]
		status := "unknown"
		if rt, err := RuntimeByName(project.Runtime); err == nil {
			if container, err := rt.Inspect(ctx, project.ContainerName); err == nil {
				status = container.Status
			}
		}
		fmt.Fprintf(a.out, "%-18s %-8s %-12s %s\n", project.Name, project.Runtime, status, project.Path)
	}
	return nil
}

func (a *App) RunProject(ctx context.Context, name string, cmd []string) error {
	project, rt, err := a.projectRuntime(ctx, name)
	if err != nil {
		return err
	}
	if err := a.warnProjectImageStale(ctx, project); err != nil {
		return err
	}
	if err := a.ensureStarted(ctx, rt, project); err != nil {
		return err
	}
	if err := a.touchProject(ctx, name); err != nil {
		return err
	}
	return a.execProject(ctx, rt, project, cmd)
}

func (a *App) RunDefault(ctx context.Context) error {
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("get current directory: %w", err)
	}
	project, ok, err := a.registry.ProjectForPath(ctx, cwd)
	if err != nil {
		return err
	}
	if ok {
		return a.RunProject(ctx, project.Name, nil)
	}
	fmt.Fprintln(a.out, "ark creates isolated development containers per project.")
	fmt.Fprintln(a.out, "")
	fmt.Fprintln(a.out, "Try:")
	fmt.Fprintln(a.out, "  ark init app --runtime docker -y")
	fmt.Fprintln(a.out, "  ark app echo hello")
	fmt.Fprintln(a.out, "")
	fmt.Fprintln(a.out, "If you are inside a registered project directory, plain `ark` enters it.")
	return nil
}

func (a *App) projectRuntime(ctx context.Context, name string) (Project, Runtime, error) {
	project, err := a.registry.Project(ctx, name)
	if err != nil {
		return Project{}, nil, err
	}
	rt, err := RuntimeByName(project.Runtime)
	if err != nil {
		return Project{}, nil, err
	}
	if err := rt.Available(ctx); err != nil {
		return Project{}, nil, err
	}
	return project, rt, nil
}

func (a *App) ensureStarted(ctx context.Context, rt Runtime, project Project) error {
	container, err := rt.Inspect(ctx, project.ContainerName)
	if err != nil {
		return err
	}
	if container.Running {
		return nil
	}
	return rt.Start(ctx, project.ContainerName)
}

func (a *App) execProject(ctx context.Context, rt Runtime, project Project, cmd []string) error {
	extraEnv := []string{}
	if project.SSHEnabled && a.config.Git.Enabled {
		broker, err := a.startGitBroker(ctx, project)
		if err != nil {
			return err
		}
		extraEnv = broker.Environment()
		defer func() {
			if err := broker.Close(); err != nil {
				slog.Warn("close Git broker", "project", project.Name, "error", err)
			}
		}()
	}
	tty := len(cmd) == 0 && a.isInteractive()
	if len(cmd) == 0 {
		cmd = []string{a.config.Container.Shell}
	}
	stdin := a.in
	if !tty && a.stdin != nil && term.IsTerminal(int(a.stdin.Fd())) {
		stdin = nil
	}
	return rt.Exec(ctx, project.ContainerName, ExecSpec{
		Cmd:     cmd,
		Env:     append(ProjectEnv(project, a.config), extraEnv...),
		Workdir: a.config.Container.Workdir,
		User:    a.config.Container.User,
		TTY:     tty,
		Stdin:   stdin,
		Stdout:  a.out,
		Stderr:  a.errOut,
	})
}

func (a *App) startGitBroker(ctx context.Context, project Project) (*GitBroker, error) {
	if err := a.prepareProjectMounts(project); err != nil {
		return nil, err
	}
	socketPath := filepath.Join(a.paths.ProjectSocketDir(project), filepath.Base(a.config.Git.BrokerSocket))
	return StartGitBroker(ctx, socketPath, a.config.Git.Hosts, a.errOut)
}

func (a *App) touchProject(ctx context.Context, name string) error {
	return a.registry.Update(ctx, func(state *State) error {
		project, ok := state.Projects[name]
		if !ok {
			return fmt.Errorf("project %q: %w", name, ErrNotFound)
		}
		project.LastUsedAt = time.Now().UTC()
		state.Projects[name] = project
		return nil
	})
}

func (a *App) cleanupVolumes(ctx context.Context, rt Runtime, volumeNames []string) {
	for i := len(volumeNames) - 1; i >= 0; i-- {
		if err := rt.RemoveVolume(ctx, volumeNames[i]); err != nil {
			slog.Warn("cleanup volume after init failure", "volume", volumeNames[i], "error", err)
		}
	}
}

func (a *App) confirm(prompt string) (bool, error) {
	fmt.Fprintf(a.out, "%s [y/N] ", prompt)
	reader := bufio.NewReader(a.in)
	answer, err := reader.ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return false, fmt.Errorf("read confirmation: %w", err)
	}
	answer = strings.ToLower(strings.TrimSpace(answer))
	return answer == "y" || answer == "yes", nil
}

func (a *App) isInteractive() bool {
	return a.stdin != nil && a.stdout != nil && term.IsTerminal(int(a.stdin.Fd())) && term.IsTerminal(int(a.stdout.Fd()))
}
