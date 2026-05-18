package internal

import (
	"bytes"
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestRunProjectPortChangeRunningRecreatesWithConfirmation(t *testing.T) {
	ctx := context.Background()
	port3000 := mustPortMapping(t, "3000")
	rt := &fakePortRuntime{
		inspectResults: []*Container{
			{Running: true, Status: "running"},
			{Running: false, Status: "created"},
		},
	}
	app, out, _ := newPortTestApp(t, "y\n", testProject(t, nil, false), rt)

	if err := app.RunProject(ctx, "app", nil, PortOptions{Specs: []string{"3000"}}); err != nil {
		t.Fatalf("RunProject: %v", err)
	}

	wantCalls := []string{"Inspect", "Stop", "Remove", "Create", "Inspect", "Start"}
	if !reflect.DeepEqual(rt.calls, wantCalls) {
		t.Fatalf("calls mismatch:\n got: %v\nwant: %v", rt.calls, wantCalls)
	}
	if len(rt.createSpecs) != 1 || !PortMappingsEqual(rt.createSpecs[0].Ports, []PortMapping{port3000}) {
		t.Fatalf("Create ports mismatch: %#v", rt.createSpecs)
	}
	project := mustRegistryProject(t, app, "app")
	if !project.AutoRecreateOnPortChange {
		t.Fatalf("AutoRecreateOnPortChange was not persisted")
	}
	if !PortMappingsEqual(project.Ports, []PortMapping{port3000}) {
		t.Fatalf("registry ports mismatch: %#v", project.Ports)
	}
	if !strings.Contains(out.String(), "Recreating app for port change") {
		t.Fatalf("missing recreate output: %q", out.String())
	}
}

func TestRunProjectPortChangeStoppedRecreatesWithoutConfirmation(t *testing.T) {
	ctx := context.Background()
	rt := &fakePortRuntime{
		inspectResults: []*Container{
			{Running: false, Status: "exited"},
			{Running: false, Status: "created"},
		},
	}
	app, out, _ := newPortTestApp(t, "", testProject(t, nil, false), rt)

	if err := app.RunProject(ctx, "app", nil, PortOptions{Specs: []string{"3000"}}); err != nil {
		t.Fatalf("RunProject: %v", err)
	}

	wantCalls := []string{"Inspect", "Remove", "Create", "Inspect", "Start"}
	if !reflect.DeepEqual(rt.calls, wantCalls) {
		t.Fatalf("calls mismatch:\n got: %v\nwant: %v", rt.calls, wantCalls)
	}
	if strings.Contains(out.String(), "Continue?") {
		t.Fatalf("unexpected confirmation prompt: %q", out.String())
	}
}

func TestRunProjectPortChangeNoopDoesNotRecreate(t *testing.T) {
	ctx := context.Background()
	port3000 := mustPortMapping(t, "3000")
	rt := &fakePortRuntime{
		inspectResults: []*Container{{Running: true, Status: "running"}},
	}
	app, _, _ := newPortTestApp(t, "", testProject(t, []PortMapping{port3000}, false), rt)

	if err := app.RunProject(ctx, "app", nil, PortOptions{Specs: []string{"3000"}}); err != nil {
		t.Fatalf("RunProject: %v", err)
	}

	wantCalls := []string{"Inspect"}
	if !reflect.DeepEqual(rt.calls, wantCalls) {
		t.Fatalf("calls mismatch:\n got: %v\nwant: %v", rt.calls, wantCalls)
	}
	if len(rt.createSpecs) != 0 {
		t.Fatalf("unexpected recreate: %#v", rt.createSpecs)
	}
}

func TestRunProjectNoPortsClearsAndRecreates(t *testing.T) {
	ctx := context.Background()
	port3000 := mustPortMapping(t, "3000")
	rt := &fakePortRuntime{
		inspectResults: []*Container{
			{Running: false, Status: "exited"},
			{Running: false, Status: "created"},
		},
	}
	app, _, _ := newPortTestApp(t, "", testProject(t, []PortMapping{port3000}, false), rt)

	if err := app.RunProject(ctx, "app", nil, PortOptions{Clear: true}); err != nil {
		t.Fatalf("RunProject: %v", err)
	}

	wantCalls := []string{"Inspect", "Remove", "Create", "Inspect", "Start"}
	if !reflect.DeepEqual(rt.calls, wantCalls) {
		t.Fatalf("calls mismatch:\n got: %v\nwant: %v", rt.calls, wantCalls)
	}
	if len(rt.createSpecs) != 1 || len(rt.createSpecs[0].Ports) != 0 {
		t.Fatalf("Create ports were not cleared: %#v", rt.createSpecs)
	}
	project := mustRegistryProject(t, app, "app")
	if len(project.Ports) != 0 {
		t.Fatalf("registry ports were not cleared: %#v", project.Ports)
	}
}

func TestRunProjectPortsListModeDoesNotTouchRuntime(t *testing.T) {
	ctx := context.Background()
	port3000 := mustPortMapping(t, "3000")
	rt := &fakePortRuntime{}
	app, out, _ := newPortTestApp(t, "", testProject(t, []PortMapping{port3000}, false), rt)

	if err := app.RunProject(ctx, "app", nil, PortOptions{List: true}); err != nil {
		t.Fatalf("RunProject: %v", err)
	}

	if len(rt.calls) != 0 {
		t.Fatalf("runtime should not be touched in list mode, got %v", rt.calls)
	}
	if got := out.String(); !strings.Contains(got, "app ports:") || !strings.Contains(got, "  3000") {
		t.Fatalf("unexpected list output: %q", got)
	}
}

func TestRunProjectPortChangeRecreatesVolumes(t *testing.T) {
	ctx := context.Background()
	rt := &fakePortRuntime{
		inspectResults: []*Container{
			{Running: false, Status: "exited"},
			{Running: false, Status: "created"},
		},
	}
	app, _, _ := newPortTestApp(t, "", testProject(t, nil, false), rt)

	if err := app.RunProject(ctx, "app", nil, PortOptions{Specs: []string{"3000"}}); err != nil {
		t.Fatalf("RunProject: %v", err)
	}

	wantVolumes := []string{"ark-test-home", "ark-test-cache"}
	if !reflect.DeepEqual(rt.createdVolumes, wantVolumes) {
		t.Fatalf("createdVolumes mismatch:\n got: %v\nwant: %v", rt.createdVolumes, wantVolumes)
	}
}

func TestRunProjectPortChangeSkipsRememberedConfirmation(t *testing.T) {
	ctx := context.Background()
	rt := &fakePortRuntime{
		inspectResults: []*Container{
			{Running: true, Status: "running"},
			{Running: false, Status: "created"},
		},
	}
	app, out, _ := newPortTestApp(t, "", testProject(t, nil, true), rt)

	if err := app.RunProject(ctx, "app", nil, PortOptions{Specs: []string{"3000"}}); err != nil {
		t.Fatalf("RunProject: %v", err)
	}

	wantCalls := []string{"Inspect", "Stop", "Remove", "Create", "Inspect", "Start"}
	if !reflect.DeepEqual(rt.calls, wantCalls) {
		t.Fatalf("calls mismatch:\n got: %v\nwant: %v", rt.calls, wantCalls)
	}
	if strings.Contains(out.String(), "Continue?") {
		t.Fatalf("unexpected confirmation prompt: %q", out.String())
	}
}

func TestRunProjectInvalidPortSpecDoesNotTouchRuntime(t *testing.T) {
	ctx := context.Background()
	rt := &fakePortRuntime{}
	app, _, _ := newPortTestApp(t, "", testProject(t, nil, false), rt)

	err := app.RunProject(ctx, "app", nil, PortOptions{Specs: []string{"3000-3010"}})
	if err == nil {
		t.Fatalf("expected error")
	}
	if !strings.Contains(err.Error(), portRangeErrorText) {
		t.Fatalf("error = %v, want range error", err)
	}
	if len(rt.calls) != 0 {
		t.Fatalf("runtime should not be touched for invalid spec, got %v", rt.calls)
	}
}

func TestListProjectsIncludesPortsColumn(t *testing.T) {
	ctx := context.Background()
	port3000 := mustPortMapping(t, "3000")
	rt := &fakePortRuntime{
		inspectResults: []*Container{{Running: true, Status: "running"}},
	}
	app, out, _ := newPortTestApp(t, "", testProject(t, []PortMapping{port3000}, false), rt)

	if err := app.ListProjects(ctx); err != nil {
		t.Fatalf("ListProjects: %v", err)
	}

	got := out.String()
	if !strings.Contains(got, "PORTS") || !strings.Contains(got, "3000") {
		t.Fatalf("list output missing ports column:\n%s", got)
	}
}

func TestParsePortOptionsFromArgsStripsPortFlags(t *testing.T) {
	cmdArgs, ports, err := parsePortOptionsFromArgs([]string{"--port", "3000", "echo", "hi", "--port=-3001"})
	if err != nil {
		t.Fatalf("parsePortOptionsFromArgs: %v", err)
	}
	if !reflect.DeepEqual(cmdArgs, []string{"echo", "hi"}) {
		t.Fatalf("cmd args = %v", cmdArgs)
	}
	if !ports.Specified || !reflect.DeepEqual(ports.Specs, []string{"3000", "-3001"}) {
		t.Fatalf("ports = %#v", ports)
	}
}

type fakePortRuntime struct {
	inspectResults []*Container
	createSpecs    []CreateSpec
	calls          []string
	createdVolumes []string
	imageExists    bool
	imageExistsSet bool
	imageExistsTag []string
	buildImageSpec []BuildImageSpec
}

func (f *fakePortRuntime) Name() string {
	return RuntimeDocker
}

func (f *fakePortRuntime) Available(context.Context) error {
	return nil
}

func (f *fakePortRuntime) ImageExists(_ context.Context, tag string) (bool, error) {
	f.imageExistsTag = append(f.imageExistsTag, tag)
	if f.imageExistsSet {
		return f.imageExists, nil
	}
	return true, nil
}

func (f *fakePortRuntime) BuildImage(_ context.Context, spec BuildImageSpec) error {
	f.buildImageSpec = append(f.buildImageSpec, spec)
	return nil
}

func (f *fakePortRuntime) Create(_ context.Context, spec CreateSpec) (string, error) {
	f.calls = append(f.calls, "Create")
	f.createSpecs = append(f.createSpecs, spec)
	return "container-id", nil
}

func (f *fakePortRuntime) Start(context.Context, string) error {
	f.calls = append(f.calls, "Start")
	return nil
}

func (f *fakePortRuntime) Stop(context.Context, string, int) error {
	f.calls = append(f.calls, "Stop")
	return nil
}

func (f *fakePortRuntime) Remove(context.Context, string, bool) error {
	f.calls = append(f.calls, "Remove")
	return nil
}

func (f *fakePortRuntime) Exec(context.Context, string, ExecSpec) error {
	f.calls = append(f.calls, "Exec")
	return nil
}

func (f *fakePortRuntime) Inspect(context.Context, string) (*Container, error) {
	f.calls = append(f.calls, "Inspect")
	if len(f.inspectResults) == 0 {
		return &Container{Running: true, Status: "running"}, nil
	}
	result := f.inspectResults[0]
	f.inspectResults = f.inspectResults[1:]
	return result, nil
}

func (f *fakePortRuntime) List(context.Context) ([]Container, error) {
	return nil, nil
}

func (f *fakePortRuntime) CreateVolume(_ context.Context, name string) error {
	f.createdVolumes = append(f.createdVolumes, name)
	return nil
}

func (f *fakePortRuntime) RemoveVolume(context.Context, string) error {
	return nil
}

func newPortTestApp(t *testing.T, input string, project Project, rt *fakePortRuntime) (*App, *bytes.Buffer, *bytes.Buffer) {
	t.Helper()
	ctx := context.Background()
	root := t.TempDir()
	paths := portTestPaths(root)
	config := DefaultConfig()
	config.Runtime = RuntimeDocker
	config.ProjectRoot = paths.ProjectRoot
	config.Image.Source = paths.ImageDir
	config.Init.Enter = false
	config.Git.Enabled = false
	if err := EnsureImageSource(config); err != nil {
		t.Fatalf("EnsureImageSource: %v", err)
	}
	registry := NewRegistry(paths)
	if err := registry.EnsureDefault(ctx); err != nil {
		t.Fatalf("EnsureDefault: %v", err)
	}
	if err := registry.Update(ctx, func(state *State) error {
		state.Projects[project.Name] = project
		return nil
	}); err != nil {
		t.Fatalf("seed registry: %v", err)
	}

	var out bytes.Buffer
	var errOut bytes.Buffer
	app := &App{
		paths:    paths,
		config:   config,
		registry: registry,
		images:   NewImageStore(paths),
		runtimeByName: func(name string) (Runtime, error) {
			if name != RuntimeDocker {
				return nil, errors.New("unexpected runtime: " + name)
			}
			return rt, nil
		},
		in:     strings.NewReader(input),
		out:    &out,
		errOut: &errOut,
	}
	return app, &out, &errOut
}

func testProject(t *testing.T, ports []PortMapping, autoRecreate bool) Project {
	t.Helper()
	now := time.Date(2026, 5, 17, 12, 0, 0, 0, time.UTC)
	return Project{
		ID:                       "01ARZ3NDEKTSV4RRFFQ69G5FAV",
		Name:                     "app",
		Runtime:                  RuntimeDocker,
		Path:                     filepath.Join(t.TempDir(), "app"),
		ContainerName:            "ark-test-container",
		Image:                    DefaultImageTag,
		Volumes:                  Volumes{Home: "ark-test-home", Cache: "ark-test-cache"},
		Ports:                    ports,
		AutoRecreateOnPortChange: autoRecreate,
		CreatedAt:                now,
		LastUsedAt:               now,
	}
}

func portTestPaths(root string) Paths {
	arkHome := filepath.Join(root, ".ark")
	stateFile := filepath.Join(arkHome, "state.json")
	return Paths{
		ArkHome:          arkHome,
		ImageDir:         filepath.Join(arkHome, "image"),
		ImageStateFile:   filepath.Join(arkHome, "image", "state.json"),
		SocketsDir:       filepath.Join(arkHome, "sockets"),
		CacheDir:         filepath.Join(arkHome, "cache"),
		LogsDir:          filepath.Join(arkHome, "logs"),
		DevcontainersDir: filepath.Join(arkHome, "devcontainers"),
		ConfigFile:       filepath.Join(arkHome, "config.toml"),
		StateFile:        stateFile,
		LockFile:         stateFile + ".lock",
		ProjectRoot:      filepath.Join(root, "projects"),
	}
}

func mustPortMapping(t *testing.T, spec string) PortMapping {
	t.Helper()
	ports, err := ParsePortList([]string{spec})
	if err != nil {
		t.Fatalf("ParsePortList(%q): %v", spec, err)
	}
	if len(ports) != 1 {
		t.Fatalf("ParsePortList(%q) returned %d ports", spec, len(ports))
	}
	return ports[0]
}

func mustRegistryProject(t *testing.T, app *App, name string) Project {
	t.Helper()
	project, err := app.registry.Project(context.Background(), name)
	if err != nil {
		t.Fatalf("registry.Project(%q): %v", name, err)
	}
	return project
}

var _ Runtime = (*fakePortRuntime)(nil)
