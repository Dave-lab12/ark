package internal

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
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
	paths         Paths
	config        Config
	registry      *Registry
	images        *ImageStore
	reserved      map[string]struct{}
	runtimeByName func(string) (Runtime, error)
	in            io.Reader
	out           io.Writer
	errOut        io.Writer
	stdin         *os.File
	stdout        *os.File
}

type InitOptions struct {
	Runtime       string
	SSHEnabled    bool
	DockerEnabled bool
	Enter         bool
	Ports         PortOptions
}

type PortOptions struct {
	Specified bool
	Specs     []string
	Clear     bool
	List      bool
}

type EditOptions struct {
	EditorOverride string
	Folder         string
	Ports          PortOptions
}

func normalizePortOptions(ports PortOptions) (PortOptions, error) {
	ports.Specified = ports.Specified || len(ports.Specs) > 0 || ports.Clear || ports.List
	if len(ports.Specs) > 0 && ports.Clear {
		return ports, errors.New("--port and --no-ports are mutually exclusive")
	}
	if ports.List && (len(ports.Specs) > 0 || ports.Clear) {
		return ports, errors.New("--ports lists current ports and cannot be combined with --port or --no-ports")
	}
	return ports, nil
}

func truncateColumn(s string, width int) string {
	if len(s) <= width {
		return s
	}
	if width <= 3 {
		return s[:width]
	}
	return s[:width-3] + "..."
}

// NewApp is intentionally cheap: it resolves paths and wires I/O streams,
// but does no disk I/O and loads no config. All filesystem touches happen
// in Prepare so tests can construct an App without a real ~/.ark layout.
func NewApp(in io.Reader, out, errOut io.Writer) (*App, error) {
	paths, err := DefaultPaths()
	if err != nil {
		return nil, err
	}
	app := &App{
		paths:         paths,
		runtimeByName: RuntimeByName,
		in:            in,
		out:           out,
		errOut:        errOut,
	}
	if file, ok := in.(*os.File); ok {
		app.stdin = file
	}
	if file, ok := out.(*os.File); ok {
		app.stdout = file
	}
	return app, nil
}

// Prepare materializes everything NewApp deferred: control-plane dirs,
// default config + image source files, the loaded config, and the
// registry/image stores that depend on the final ProjectRoot.
func (a *App) Prepare(ctx context.Context) error {
	if err := a.paths.EnsureControlPlane(); err != nil {
		return err
	}
	if err := EnsureDefaultConfig(a.paths); err != nil {
		return err
	}
	config, err := LoadConfig(a.paths)
	if err != nil {
		return err
	}
	a.config = config
	if err := EnsureImageSource(a.config); err != nil {
		return err
	}
	projectRoot, err := a.config.ProjectRootPath()
	if err != nil {
		return err
	}
	a.paths.ProjectRoot = projectRoot

	// Registry/ImageStore copy Paths by value, so they must be constructed
	// after ProjectRoot is finalized.
	a.registry = NewRegistry(a.paths)
	a.images = NewImageStore(a.paths)
	if err := a.registry.EnsureDefault(ctx); err != nil {
		return err
	}
	if err := a.images.EnsureDefault(ctx); err != nil {
		return err
	}
	return nil
}

func (a *App) InitProject(ctx context.Context, name string, opts InitOptions) error {
	ports, err := normalizePortOptions(opts.Ports)
	if err != nil {
		return err
	}
	var initialPorts []PortMapping
	if ports.Specified && !ports.Clear && len(ports.Specs) > 0 {
		initialPorts, err = ParsePortChange(ports.Specs, nil)
		if err != nil {
			return err
		}
	}
	if err := ValidateProjectName(name, a.reserved); err != nil {
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
	project.Ports = initialPorts

	if err := ensureProjectControlPlane(a.paths, project); err != nil {
		return err
	}
	volumeNames := projectVolumeNames(project)
	createdVolumes := make([]string, 0, len(volumeNames))
	for _, volumeName := range volumeNames {
		if err := rt.CreateVolume(ctx, volumeName); err != nil {
			a.cleanupVolumes(ctx, rt, createdVolumes)
			return err
		}
		createdVolumes = append(createdVolumes, volumeName)
	}

	if err := a.createProjectContainer(ctx, rt, project); err != nil {
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
	if ports.List {
		a.printProjectPorts(project)
	}
	if opts.Enter && a.isInteractive() {
		return a.RunProject(ctx, project.Name, nil, PortOptions{})
	}
	fmt.Fprintf(a.out, "Enter it with: ark %s\n", project.Name)
	return nil
}

func (a *App) StartProject(ctx context.Context, name string, enter bool, ports PortOptions) error {
	ports, err := normalizePortOptions(ports)
	if err != nil {
		return err
	}
	project, err := a.registry.Project(ctx, name)
	if err != nil {
		return err
	}
	if ports.List {
		a.printProjectPorts(project)
		return nil
	}
	rt, err := a.runtimeForProject(ctx, project)
	if err != nil {
		return err
	}
	project, err = a.applyRequestedPorts(ctx, rt, project, ports)
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
	if err := a.printDynamicPortsIfAny(ctx, rt, project); err != nil {
		fmt.Fprintf(a.errOut, "ark: could not read live ports: %v\n", err)
	}
	if enter {
		return a.execProject(ctx, rt, project, nil)
	}
	fmt.Fprintf(a.out, "Started %s\n", name)
	return nil
}

func (a *App) EditProject(ctx context.Context, name string, opts EditOptions) error {
	ports, err := normalizePortOptions(opts.Ports)
	if err != nil {
		return err
	}
	if ports.List {
		return errors.New("--ports cannot be combined with ark edit; use ark <name> --ports")
	}

	project, err := a.registry.Project(ctx, name)
	if err != nil {
		return err
	}

	rt, err := a.runtimeForProject(ctx, project)
	if err != nil {
		return err
	}

	project, err = a.applyRequestedPorts(ctx, rt, project, ports)
	if err != nil {
		return err
	}

	if err := a.touchProject(ctx, name); err != nil {
		return err
	}

	editorName := opts.EditorOverride
	if editorName == "" {
		editorName = a.config.Editor.Default
	}
	if editorName == "" {
		return errors.New("no editor configured; set [editor].default in ~/.ark/config.toml or pass --editor")
	}

	binary, err := ResolveEditorBinary(editorName)
	if err != nil {
		return err
	}

	switch EditorModeFor(editorName) {
	case EditorModeAttach:
		return a.editProjectAttach(ctx, rt, project, binary, opts)
	case EditorModeDevcontainerNative:
		return a.editProjectNative(ctx, rt, project, binary, opts)
	default:
		return fmt.Errorf("internal error: unknown editor mode for %q", editorName)
	}
}

func (a *App) editProjectAttach(ctx context.Context, rt Runtime, project Project, binary string, opts EditOptions) error {
	if err := a.ensureStarted(ctx, rt, project); err != nil {
		return err
	}

	folder := resolveContainerFolder(opts.Folder, a.config.Container.Workdir)
	remote := BuildRemoteAuthority(project.ContainerName)

	if err := launchEditor(binary, remote, folder); err != nil {
		return err
	}

	if folder != a.config.Container.Workdir {
		fmt.Fprintf(a.out, "Opening %s (%s) in %s...\n", project.Name, folder, filepath.Base(binary))
	} else {
		fmt.Fprintf(a.out, "Opening %s in %s...\n", project.Name, filepath.Base(binary))
	}
	if err := a.printDynamicPortsIfAny(ctx, rt, project); err != nil {
		fmt.Fprintf(a.errOut, "ark: could not read live ports: %v\n", err)
	}
	return nil
}

func (a *App) editProjectNative(ctx context.Context, rt Runtime, project Project, binary string, opts EditOptions) error {
	workspaceFolder, err := resolveNativeWorkspaceFolder(opts.Folder, a.config.Container.Workdir)
	if err != nil {
		return err
	}

	devcontainerPath := filepath.Join(project.Path, ".devcontainer", "devcontainer.json")

	if err := assertSafeToWriteDevcontainer(devcontainerPath); err != nil {
		return err
	}

	imageInfo, err := a.ensureBaseImage(ctx, rt, project.Runtime)
	if err != nil {
		return err
	}

	if err := mkdirAllWithMode(filepath.Dir(devcontainerPath), 0o755); err != nil {
		return fmt.Errorf("create .devcontainer directory: %w", err)
	}

	data, err := BuildDevcontainer(project, a.config, DevcontainerRenderOptions{
		ImageTag:         imageInfo.Tag,
		ImageFingerprint: imageInfo.Fingerprint,
		ArkVersion:       ArkVersion,
		WorkspaceFolder:  workspaceFolder,
	})
	if err != nil {
		return err
	}
	if err := atomicWriteFile(devcontainerPath, data, 0o644); err != nil {
		return err
	}

	if err := addToGitLocalExclude(project.Path, ".devcontainer/devcontainer.json"); err != nil {
		// Non-fatal: the user may not be in a Git repo, or the exclude
		// file may not be writable. Surface the warning, continue.
		fmt.Fprintf(a.errOut, "ark: could not update .git/info/exclude: %v\n", err)
	}

	if err := launchEditorPath(binary, project.Path); err != nil {
		return err
	}
	fmt.Fprintf(a.out, "Opening %s in %s...\n", project.Name, filepath.Base(binary))
	fmt.Fprintln(a.out, `(click "Reopen in Container" if the editor prompts)`)
	return nil
}

func (a *App) DevcontainerWrite(ctx context.Context, name string, inProject bool) error {
	project, rt, err := a.projectRuntime(ctx, name)
	if err != nil {
		return err
	}

	var target string
	if inProject {
		target = filepath.Join(project.Path, ".devcontainer", "devcontainer.json")
		if err := assertSafeToWriteDevcontainer(target); err != nil {
			return err
		}
	} else {
		target = a.paths.ArkOwnedDevcontainerPath(project)
	}

	imageInfo, err := a.ensureBaseImage(ctx, rt, project.Runtime)
	if err != nil {
		return err
	}

	dirPerm := os.FileMode(0o700)
	filePerm := os.FileMode(0o600)
	if inProject {
		dirPerm = 0o755
		filePerm = 0o644
	}
	if err := mkdirAllWithMode(filepath.Dir(target), dirPerm); err != nil {
		return fmt.Errorf("create directory: %w", err)
	}

	data, err := BuildDevcontainer(project, a.config, DevcontainerRenderOptions{
		ImageTag:         imageInfo.Tag,
		ImageFingerprint: imageInfo.Fingerprint,
		ArkVersion:       ArkVersion,
	})
	if err != nil {
		return err
	}
	if err := atomicWriteFile(target, data, filePerm); err != nil {
		return err
	}

	if inProject {
		if err := addToGitLocalExclude(project.Path, ".devcontainer/devcontainer.json"); err != nil {
			fmt.Fprintf(a.errOut, "ark: could not update .git/info/exclude: %v\n", err)
		}
	}

	fmt.Fprintf(a.out, "Wrote %s\n", target)
	return nil
}

// assertSafeToWriteDevcontainer refuses to overwrite an existing
// devcontainer.json unless ark previously generated it. The marker
// lives in customizations.ark.generated.
func assertSafeToWriteDevcontainer(path string) error {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read existing devcontainer.json: %w", err)
	}
	var parsed struct {
		Customizations struct {
			Ark struct {
				Generated bool `json:"generated"`
			} `json:"ark"`
		} `json:"customizations"`
	}
	if jsonErr := json.Unmarshal(data, &parsed); jsonErr == nil && parsed.Customizations.Ark.Generated {
		return nil
	}
	return fmt.Errorf(
		"refusing to overwrite existing %s\n"+
			"ark didn't create this file. Move or rename it, then try again.",
		path,
	)
}

// addToGitLocalExclude appends an entry to <repo>/.git/info/exclude if
// the file exists and the entry isn't already present. This is the local
// per-clone ignore mechanism, distinct from the repo's tracked .gitignore
// — appropriate for machine-generated files like ark's devcontainer.
func addToGitLocalExclude(projectPath, entry string) error {
	excludePath := filepath.Join(projectPath, ".git", "info", "exclude")
	info, err := os.Stat(excludePath)
	if errors.Is(err, os.ErrNotExist) {
		// No git repo (or no info/exclude). Skip silently.
		return nil
	}
	if err != nil {
		return fmt.Errorf("stat %s: %w", excludePath, err)
	}
	if info.IsDir() {
		return fmt.Errorf("%s is a directory", excludePath)
	}

	data, err := os.ReadFile(excludePath)
	if err != nil {
		return fmt.Errorf("read %s: %w", excludePath, err)
	}
	if entryAlreadyPresent(data, entry) {
		return nil
	}

	// Append with a leading newline if needed.
	var buf bytes.Buffer
	buf.Write(data)
	if len(data) > 0 && data[len(data)-1] != '\n' {
		buf.WriteByte('\n')
	}
	buf.WriteString(entry)
	buf.WriteByte('\n')
	return atomicWriteFile(excludePath, buf.Bytes(), 0o644)
}

func entryAlreadyPresent(data []byte, entry string) bool {
	for _, line := range bytes.Split(data, []byte("\n")) {
		if string(bytes.TrimSpace(line)) == entry {
			return true
		}
	}
	return false
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
	fmt.Fprintf(a.out, "%-18s %-8s %-12s %-18s %s\n", "NAME", "RUNTIME", "STATUS", "PORTS", "PATH")
	names := make([]string, 0, len(state.Projects))
	for name := range state.Projects {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		project := state.Projects[name]
		status := "unknown"
		runtimeByName := a.runtimeByName
		if runtimeByName == nil {
			runtimeByName = RuntimeByName
		}
		if rt, err := runtimeByName(project.Runtime); err == nil {
			if container, err := rt.Inspect(ctx, project.ContainerName); err == nil {
				status = container.Status
			}
		}
		ports := FormatPortList(project.Ports)
		if ports == "" {
			ports = "—"
		}
		fmt.Fprintf(a.out, "%-18s %-8s %-12s %-18s %s\n", project.Name, project.Runtime, status, truncateColumn(ports, 18), project.Path)
	}
	return nil
}

func (a *App) RunProject(ctx context.Context, name string, cmd []string, ports PortOptions) error {
	ports, err := normalizePortOptions(ports)
	if err != nil {
		return err
	}
	project, err := a.registry.Project(ctx, name)
	if err != nil {
		return err
	}
	if ports.List {
		a.printProjectPorts(project)
		return nil
	}
	rt, err := a.runtimeForProject(ctx, project)
	if err != nil {
		return err
	}
	project, err = a.applyRequestedPorts(ctx, rt, project, ports)
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
	if err := a.printDynamicPortsIfAny(ctx, rt, project); err != nil {
		fmt.Fprintf(a.errOut, "ark: could not read live ports: %v\n", err)
	}
	if len(cmd) == 0 && !a.isInteractive() {
		return nil
	}
	return a.execProject(ctx, rt, project, cmd)
}

func (a *App) RunDefault(ctx context.Context, ports PortOptions) error {
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("get current directory: %w", err)
	}
	project, ok, err := a.registry.ProjectForPath(ctx, cwd)
	if err != nil {
		return err
	}
	if ok {
		return a.RunProject(ctx, project.Name, nil, ports)
	}
	fmt.Fprintln(a.out, "ark creates isolated development containers per project.")
	fmt.Fprintln(a.out, "")
	fmt.Fprintln(a.out, "Try:")
	fmt.Fprintln(a.out, "  ark init app --runtime docker")
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
	rt, err := a.runtimeForProject(ctx, project)
	if err != nil {
		return Project{}, nil, err
	}
	return project, rt, nil
}

func (a *App) runtimeForProject(ctx context.Context, project Project) (Runtime, error) {
	runtimeByName := a.runtimeByName
	if runtimeByName == nil {
		runtimeByName = RuntimeByName
	}
	rt, err := runtimeByName(project.Runtime)
	if err != nil {
		return nil, err
	}
	if err := rt.Available(ctx); err != nil {
		return nil, err
	}
	return rt, nil
}

func (a *App) createProjectContainer(ctx context.Context, rt Runtime, project Project) error {
	_, err := rt.Create(ctx, CreateSpec{
		Name:          project.ContainerName,
		Image:         project.Image,
		ProjectName:   project.Name,
		ProjectID:     project.ID,
		ProjectPath:   project.Path,
		Workdir:       a.config.Container.Workdir,
		Env:           ProjectEnv(project, a.config),
		Mounts:        a.projectMounts(project),
		Ports:         project.Ports,
		DockerEnabled: project.DockerEnabled,
		Privileged:    a.config.Container.Privileged,
		Network:       true,
	})
	return err
}

// applyRequestedPorts parses ports.Specs against the project's current ports,
// applies the change via applyPortChange if anything differs, and returns the
// (possibly updated) project. It is a no-op when ports.Specified is false.
func (a *App) applyRequestedPorts(ctx context.Context, rt Runtime, project Project, ports PortOptions) (Project, error) {
	if !ports.Specified {
		return project, nil
	}
	var desired []PortMapping
	if !ports.Clear {
		var err error
		desired, err = ParsePortChange(ports.Specs, project.Ports)
		if err != nil {
			return project, err
		}
	}
	if PortMappingsEqual(project.Ports, desired) {
		return project, nil
	}
	return a.applyPortChange(ctx, rt, project, desired)
}

func (a *App) applyPortChange(ctx context.Context, rt Runtime, project Project, desired []PortMapping) (Project, error) {
	container, err := rt.Inspect(ctx, project.ContainerName)
	if err != nil && !errors.Is(err, ErrNotFound) {
		return project, err
	}

	if container != nil && container.Running {
		if !project.AutoRecreateOnPortChange {
			ok, err := a.confirm(fmt.Sprintf(
				"%s is running. Changing ports recreates the container.\n"+
					"Files in /work and your home volume are preserved. Processes running inside will stop.\n"+
					"Continue? (remembered for this project)",
				project.Name,
			))
			if err != nil {
				return project, err
			}
			if !ok {
				return project, errors.New("port change canceled")
			}
			project.AutoRecreateOnPortChange = true
		}
		fmt.Fprintf(a.out, "Recreating %s for port change...\n", project.Name)
		if err := rt.Stop(ctx, project.ContainerName, 10); err != nil && !errors.Is(err, ErrNotFound) {
			return project, err
		}
		if err := rt.Remove(ctx, project.ContainerName, true); err != nil && !errors.Is(err, ErrNotFound) {
			return project, err
		}
	} else if container != nil {
		if err := rt.Remove(ctx, project.ContainerName, true); err != nil && !errors.Is(err, ErrNotFound) {
			return project, err
		}
	}

	project.Ports = desired

	for _, volumeName := range projectVolumeNames(project) {
		if err := rt.CreateVolume(ctx, volumeName); err != nil {
			return project, err
		}
	}

	if err := a.createProjectContainer(ctx, rt, project); err != nil {
		return project, err
	}
	if err := a.registry.Update(ctx, func(state *State) error {
		if _, ok := state.Projects[project.Name]; !ok {
			return fmt.Errorf("project %q: %w", project.Name, ErrNotFound)
		}
		state.Projects[project.Name] = project
		return nil
	}); err != nil {
		return project, err
	}
	return project, nil
}

func (a *App) printProjectPorts(p Project) {
	if len(p.Ports) == 0 {
		fmt.Fprintf(a.out, "%s has no ports configured.\n", p.Name)
		return
	}
	fmt.Fprintf(a.out, "%s ports:\n", p.Name)
	for _, port := range p.Ports {
		fmt.Fprintf(a.out, "  %s\n", FormatPortMapping(port))
	}
}

func (a *App) printDynamicPortsIfAny(ctx context.Context, rt Runtime, p Project) error {
	hasDynamic := false
	for _, port := range p.Ports {
		if port.HostPort == "0" {
			hasDynamic = true
			break
		}
	}
	if !hasDynamic {
		return nil
	}
	container, err := rt.Inspect(ctx, p.ContainerName)
	if err != nil {
		return err
	}
	fmt.Fprintln(a.out, "Forwarded:")
	for _, port := range container.Ports {
		fmt.Fprintf(a.out, "  %s\n", FormatPortMapping(port))
	}
	return nil
}

func (a *App) printProjectHelp(ctx context.Context, name string) error {
	project, err := a.registry.Project(ctx, name)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			fmt.Fprintf(a.out, "ark %s \u2014 project %q is not registered.\n\n", name, name)
			fmt.Fprintln(a.out, "Create it with:")
			fmt.Fprintf(a.out, "  ark init %s\n", name)
			return nil
		}
		return err
	}

	ports := FormatPortList(project.Ports)
	if ports == "" {
		ports = "\u2014"
	}
	fmt.Fprintf(a.out, "ark %s \u2014 enter or run in project %q\n\n", name, name)
	fmt.Fprintln(a.out, "CURRENT")
	fmt.Fprintf(a.out, "  Status:    %s\n", a.projectHelpStatus(ctx, project))
	fmt.Fprintf(a.out, "  Path:      %s\n", project.Path)
	fmt.Fprintf(a.out, "  Ports:     %s\n\n", ports)
	fmt.Fprintln(a.out, "USAGE")
	fmt.Fprintf(a.out, "  ark %s                    enter the project shell\n", name)
	fmt.Fprintf(a.out, "  ark %s <cmd...>           run a command in the project\n", name)
	fmt.Fprintf(a.out, "  ark %s --port 3000        add a port\n", name)
	fmt.Fprintf(a.out, "  ark %s --port -3000       remove a port\n", name)
	fmt.Fprintf(a.out, "  ark %s --port =3000       replace all ports\n", name)
	fmt.Fprintf(a.out, "  ark %s --ports            show this project's ports\n", name)
	fmt.Fprintf(a.out, "  ark %s --no-ports         clear all ports\n\n", name)
	fmt.Fprintln(a.out, "See \"ark --help\" for project management commands.")
	return nil
}

func (a *App) projectHelpStatus(ctx context.Context, project Project) string {
	runtimeByName := a.runtimeByName
	if runtimeByName == nil {
		runtimeByName = RuntimeByName
	}
	rt, err := runtimeByName(project.Runtime)
	if err != nil {
		return "not found"
	}
	if err := rt.Available(ctx); err != nil {
		return "not found"
	}
	container, err := rt.Inspect(ctx, project.ContainerName)
	if errors.Is(err, ErrNotFound) {
		return "not found"
	}
	if err != nil {
		return "not found"
	}
	if container.Running {
		return "running"
	}
	return "stopped"
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
	if err := ensureProjectControlPlane(a.paths, project); err != nil {
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
