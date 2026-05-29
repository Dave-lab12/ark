package internal

import (
	"bytes"
	"context"
	"errors"
	"os"
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

func TestStartProjectStartsBeforeDynamicPortsAndEnter(t *testing.T) {
	ctx := context.Background()
	project := testProject(t, []PortMapping{mustPortMapping(t, "0:3000")}, false)
	rt := &fakePortRuntime{
		inspectResults: []*Container{
			{Running: false, Status: "exited"},
			{
				Running: true,
				Status:  "running",
				Ports:   []PortMapping{mustPortMapping(t, "49152:3000")},
			},
		},
	}
	app, out, _ := newPortTestApp(t, "", project, rt)

	if err := app.StartProject(ctx, project.Name, true, PortOptions{}); err != nil {
		t.Fatalf("StartProject: %v", err)
	}

	wantCalls := []string{"Inspect", "Start", "Inspect", "Exec"}
	if !reflect.DeepEqual(rt.calls, wantCalls) {
		t.Fatalf("calls mismatch:\n got: %v\nwant: %v", rt.calls, wantCalls)
	}
	if got := out.String(); !strings.Contains(got, "Forwarded:") || !strings.Contains(got, "49152:3000") {
		t.Fatalf("dynamic ports were not printed after start:\n%s", got)
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

func TestRunProjectMemoryChangeRecreatesAndPersists(t *testing.T) {
	ctx := context.Background()
	rt := &fakePortRuntime{
		inspectResults: []*Container{
			{Running: false, Status: "exited"},
			{Running: false, Status: "created"},
		},
	}
	app, _, _ := newPortTestApp(t, "", testProject(t, nil, false), rt)

	err := app.RunProjectWithOptions(ctx, "app", nil, ProjectOptions{
		Memory: MemoryOptions{Limit: "4g"},
	})
	if err != nil {
		t.Fatalf("RunProjectWithOptions: %v", err)
	}

	wantCalls := []string{"Inspect", "Remove", "Create", "Inspect", "Start"}
	if !reflect.DeepEqual(rt.calls, wantCalls) {
		t.Fatalf("calls mismatch:\n got: %v\nwant: %v", rt.calls, wantCalls)
	}
	if len(rt.createSpecs) != 1 || rt.createSpecs[0].Memory != "4g" {
		t.Fatalf("Create memory mismatch: %#v", rt.createSpecs)
	}
	project := mustRegistryProject(t, app, "app")
	if project.Memory != "4g" {
		t.Fatalf("registry memory = %q, want 4g", project.Memory)
	}
}

func TestRunProjectPortAndMemoryChangeRecreatesOnce(t *testing.T) {
	ctx := context.Background()
	port3000 := mustPortMapping(t, "3000")
	rt := &fakePortRuntime{
		inspectResults: []*Container{
			{Running: false, Status: "exited"},
			{Running: false, Status: "created"},
		},
	}
	app, _, _ := newPortTestApp(t, "", testProject(t, nil, false), rt)

	err := app.RunProjectWithOptions(ctx, "app", nil, ProjectOptions{
		Ports:  PortOptions{Specs: []string{"3000"}},
		Memory: MemoryOptions{Limit: "4g"},
	})
	if err != nil {
		t.Fatalf("RunProjectWithOptions: %v", err)
	}

	if len(rt.createSpecs) != 1 {
		t.Fatalf("expected one recreate, got %#v", rt.createSpecs)
	}
	if !PortMappingsEqual(rt.createSpecs[0].Ports, []PortMapping{port3000}) || rt.createSpecs[0].Memory != "4g" {
		t.Fatalf("Create spec mismatch: %#v", rt.createSpecs[0])
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

func TestCreateProjectContainerIncludesReadOnlyConfigMounts(t *testing.T) {
	ctx := context.Background()
	rt := &fakePortRuntime{}
	app, _, _ := newPortTestApp(t, "", testProject(t, nil, false), rt)

	configDir := filepath.Join(t.TempDir(), "app")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	configFile := filepath.Join(t.TempDir(), ".gitconfig")
	if err := os.WriteFile(configFile, []byte("[user]\n\tname = yab\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	app.config.Mounts.ReadOnly = []ReadOnlyMountConfig{
		{Source: configDir, Target: "/home/dev/.config/app"},
		{Source: configFile, Target: "/home/dev/.gitconfig"},
	}

	if err := app.createProjectContainer(ctx, rt, mustRegistryProject(t, app, "app")); err != nil {
		t.Fatalf("createProjectContainer: %v", err)
	}
	if len(rt.createSpecs) != 1 {
		t.Fatalf("Create called %d times", len(rt.createSpecs))
	}

	got := rt.createSpecs[0].Mounts
	if !hasMount(got, MountSpec{
		Type:     MountTypeBind,
		Source:   configDir,
		Target:   "/home/dev/.config/app",
		ReadOnly: true,
	}) {
		t.Fatalf("missing read-only directory mount: %#v", got)
	}
	if !hasMount(got, MountSpec{
		Type:     MountTypeBind,
		Source:   configFile,
		Target:   "/home/dev/.gitconfig",
		ReadOnly: true,
	}) {
		t.Fatalf("missing read-only file mount: %#v", got)
	}
}

func TestCreateProjectContainerRejectsInvalidConfigMount(t *testing.T) {
	ctx := context.Background()
	rt := &fakePortRuntime{}
	app, _, _ := newPortTestApp(t, "", testProject(t, nil, false), rt)

	app.config.Mounts.ReadOnly = []ReadOnlyMountConfig{
		{Source: t.TempDir(), Target: "/home/dev/.cache/app"},
	}

	err := app.createProjectContainer(ctx, rt, mustRegistryProject(t, app, "app"))
	if err == nil {
		t.Fatalf("expected error")
	}
	if !strings.Contains(err.Error(), "mounts.readonly[0]") {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(rt.createSpecs) != 0 {
		t.Fatalf("runtime should not receive Create on invalid mount config: %#v", rt.createSpecs)
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

func TestRunProjectInvalidMemoryDoesNotTouchRuntime(t *testing.T) {
	ctx := context.Background()
	rt := &fakePortRuntime{}
	app, _, _ := newPortTestApp(t, "", testProject(t, nil, false), rt)

	err := app.RunProjectWithOptions(ctx, "app", nil, ProjectOptions{
		Memory: MemoryOptions{Limit: "nope"},
	})
	if err == nil {
		t.Fatalf("expected error")
	}
	if !strings.Contains(err.Error(), "invalid memory limit") {
		t.Fatalf("error = %v, want invalid memory limit", err)
	}
	if len(rt.calls) != 0 {
		t.Fatalf("runtime should not be touched for invalid memory, got %v", rt.calls)
	}
}

func TestListProjectsIncludesPortsColumn(t *testing.T) {
	ctx := context.Background()
	port3000 := mustPortMapping(t, "3000")
	project := testProject(t, []PortMapping{port3000}, false)
	project.Memory = "4g"
	rt := &fakePortRuntime{
		inspectResults: []*Container{{Running: true, Status: "running"}},
		networkGroups:  []NetworkGroup{{Name: "mindplex", NetworkName: "ark-mindplex", Containers: []string{"ark-test-container"}}},
	}
	app, out, _ := newPortTestApp(t, "", project, rt)

	if err := app.ListProjects(ctx); err != nil {
		t.Fatalf("ListProjects: %v", err)
	}

	got := out.String()
	for _, want := range []string{"PORTS", "GROUPS", "LIMIT", "3000", "mindplex", "4g"} {
		if !strings.Contains(got, want) {
			t.Fatalf("list output missing %q:\n%s", want, got)
		}
	}
	for _, notWant := range []string{"CPU", "RAM", "12.5%", "128MiB"} {
		if strings.Contains(got, notWant) {
			t.Fatalf("list output unexpectedly contains %q:\n%s", notWant, got)
		}
	}
	wantCalls := []string{"ListNetworkGroups", "Inspect"}
	if !reflect.DeepEqual(rt.calls, wantCalls) {
		t.Fatalf("calls mismatch:\n got: %v\nwant: %v", rt.calls, wantCalls)
	}
}

func TestListProjectsSkipsStatsForStoppedContainers(t *testing.T) {
	ctx := context.Background()
	rt := &fakePortRuntime{
		inspectResults: []*Container{{Running: false, Status: "exited"}},
	}
	app, out, _ := newPortTestApp(t, "", testProject(t, nil, false), rt)

	if err := app.ListProjects(ctx); err != nil {
		t.Fatalf("ListProjects: %v", err)
	}

	if strings.Contains(out.String(), "0.0%") {
		t.Fatalf("stopped container should not show live stats:\n%s", out.String())
	}
	wantCalls := []string{"ListNetworkGroups", "Inspect"}
	if !reflect.DeepEqual(rt.calls, wantCalls) {
		t.Fatalf("calls mismatch:\n got: %v\nwant: %v", rt.calls, wantCalls)
	}
}

func TestListProjectStatsShowsLiveUsage(t *testing.T) {
	ctx := context.Background()
	project := testProject(t, nil, false)
	project.Memory = "4g"
	rt := &fakePortRuntime{
		inspectResults: []*Container{{Running: true, Status: "running"}},
		statsResults:   []*ResourceStats{{CPUPercent: 12.5, MemoryUsage: 128 * 1024 * 1024}},
	}
	app, out, _ := newPortTestApp(t, "", project, rt)

	if err := app.ListProjectStats(ctx, nil); err != nil {
		t.Fatalf("ListProjectStats: %v", err)
	}

	got := out.String()
	for _, want := range []string{"NAME", "STATUS", "CPU", "RAM", "LIMIT", "app", "running", "12.5%", "128MiB", "4g"} {
		if !strings.Contains(got, want) {
			t.Fatalf("stats output missing %q:\n%s", want, got)
		}
	}
	wantCalls := []string{"Inspect", "Stats"}
	if !reflect.DeepEqual(rt.calls, wantCalls) {
		t.Fatalf("calls mismatch:\n got: %v\nwant: %v", rt.calls, wantCalls)
	}
}

func TestListProjectStatsSkipsStatsForStoppedContainers(t *testing.T) {
	ctx := context.Background()
	rt := &fakePortRuntime{
		inspectResults: []*Container{{Running: false, Status: "exited"}},
	}
	app, out, _ := newPortTestApp(t, "", testProject(t, nil, false), rt)

	if err := app.ListProjectStats(ctx, []string{"app"}); err != nil {
		t.Fatalf("ListProjectStats: %v", err)
	}

	if strings.Contains(out.String(), "0.0%") {
		t.Fatalf("stopped container should not show live stats:\n%s", out.String())
	}
	wantCalls := []string{"Inspect"}
	if !reflect.DeepEqual(rt.calls, wantCalls) {
		t.Fatalf("calls mismatch:\n got: %v\nwant: %v", rt.calls, wantCalls)
	}
}

func TestListProjectsIncludesPortsColumnLegacy(t *testing.T) {
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

func TestParseProjectOptionsFromArgsStripsMemoryFlags(t *testing.T) {
	cmdArgs, opts, err := parseProjectOptionsFromArgs([]string{"--memory", "4g", "npm", "run", "dev", "--port=3000"})
	if err != nil {
		t.Fatalf("parseProjectOptionsFromArgs: %v", err)
	}
	if !reflect.DeepEqual(cmdArgs, []string{"npm", "run", "dev"}) {
		t.Fatalf("cmd args = %v", cmdArgs)
	}
	if opts.Memory.Limit != "4g" || !opts.Memory.Specified {
		t.Fatalf("memory = %#v", opts.Memory)
	}
	if !opts.Ports.Specified || !reflect.DeepEqual(opts.Ports.Specs, []string{"3000"}) {
		t.Fatalf("ports = %#v", opts.Ports)
	}
}

func TestCreateNetworkGroup(t *testing.T) {
	ctx := context.Background()
	rt := &fakePortRuntime{}
	app, out, _ := newPortTestApp(t, "", testProject(t, nil, false), rt)

	if err := app.CreateNetworkGroup(ctx, "mindplex"); err != nil {
		t.Fatalf("CreateNetworkGroup: %v", err)
	}

	if !reflect.DeepEqual(rt.networkCreates, []string{"ark-mindplex"}) {
		t.Fatalf("networkCreates = %v", rt.networkCreates)
	}
	if !strings.Contains(out.String(), "Created network group mindplex") {
		t.Fatalf("unexpected output: %q", out.String())
	}
}

func TestListNetworkGroups(t *testing.T) {
	ctx := context.Background()
	rt := &fakePortRuntime{
		networkGroups: []NetworkGroup{
			{Name: "mindplex", NetworkName: "ark-mindplex", Containers: []string{"ark-test-container"}},
		},
	}
	app, out, _ := newPortTestApp(t, "", testProject(t, nil, false), rt)

	if err := app.ListNetworkGroups(ctx); err != nil {
		t.Fatalf("ListNetworkGroups: %v", err)
	}

	got := out.String()
	for _, want := range []string{"GROUP", "NETWORK", "PROJECTS", "mindplex", "ark-mindplex", "app"} {
		if !strings.Contains(got, want) {
			t.Fatalf("network list missing %q:\n%s", want, got)
		}
	}
	if !reflect.DeepEqual(rt.calls, []string{"ListNetworkGroups"}) {
		t.Fatalf("calls = %v", rt.calls)
	}
}

func TestAddProjectsToNetworkGroup(t *testing.T) {
	ctx := context.Background()
	rt := &fakePortRuntime{}
	app, out, _ := newPortTestApp(t, "", testProject(t, nil, false), rt)

	if err := app.AddProjectsToNetworkGroup(ctx, "mindplex", []string{"app"}); err != nil {
		t.Fatalf("AddProjectsToNetworkGroup: %v", err)
	}

	if !reflect.DeepEqual(rt.networkCreates, []string{"ark-mindplex"}) {
		t.Fatalf("networkCreates = %v", rt.networkCreates)
	}
	if len(rt.networkConnects) != 1 {
		t.Fatalf("networkConnects = %#v", rt.networkConnects)
	}
	connect := rt.networkConnects[0]
	if connect.NetworkName != "ark-mindplex" || connect.ContainerName != "ark-test-container" || !reflect.DeepEqual(connect.Aliases, []string{"app"}) {
		t.Fatalf("connect spec = %#v", connect)
	}
	if !strings.Contains(out.String(), "Added app to network group mindplex") {
		t.Fatalf("unexpected output: %q", out.String())
	}
}

func TestRemoveProjectsFromNetworkGroup(t *testing.T) {
	ctx := context.Background()
	rt := &fakePortRuntime{}
	app, out, _ := newPortTestApp(t, "", testProject(t, nil, false), rt)

	if err := app.RemoveProjectsFromNetworkGroup(ctx, "mindplex", []string{"app"}); err != nil {
		t.Fatalf("RemoveProjectsFromNetworkGroup: %v", err)
	}

	if !reflect.DeepEqual(rt.networkDisconnects, []string{"ark-mindplex/ark-test-container"}) {
		t.Fatalf("networkDisconnects = %v", rt.networkDisconnects)
	}
	if !strings.Contains(out.String(), "Removed app from network group mindplex") {
		t.Fatalf("unexpected output: %q", out.String())
	}
}

func TestNetworkGroupRejectsInvalidName(t *testing.T) {
	if _, err := arkNetworkName("mind plex"); err == nil {
		t.Fatalf("expected invalid network group name")
	}
}

type fakePortRuntime struct {
	inspectResults     []*Container
	statsResults       []*ResourceStats
	createSpecs        []CreateSpec
	calls              []string
	createdVolumes     []string
	networkCreates     []string
	networkConnects    []NetworkConnectSpec
	networkDisconnects []string
	networkGroups      []NetworkGroup
	imageExists        bool
	imageExistsSet     bool
	imageExistsTag     []string
	buildImageSpec     []BuildImageSpec
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

func (f *fakePortRuntime) Stats(context.Context, string) (*ResourceStats, error) {
	f.calls = append(f.calls, "Stats")
	if len(f.statsResults) == 0 {
		return &ResourceStats{}, nil
	}
	result := f.statsResults[0]
	f.statsResults = f.statsResults[1:]
	return result, nil
}

func (f *fakePortRuntime) List(context.Context) ([]Container, error) {
	return nil, nil
}

func (f *fakePortRuntime) EnsureNetwork(_ context.Context, name string) error {
	f.calls = append(f.calls, "EnsureNetwork")
	f.networkCreates = append(f.networkCreates, name)
	return nil
}

func (f *fakePortRuntime) ConnectNetwork(_ context.Context, spec NetworkConnectSpec) error {
	f.calls = append(f.calls, "ConnectNetwork")
	f.networkConnects = append(f.networkConnects, spec)
	return nil
}

func (f *fakePortRuntime) DisconnectNetwork(_ context.Context, networkName, containerName string) error {
	f.calls = append(f.calls, "DisconnectNetwork")
	f.networkDisconnects = append(f.networkDisconnects, networkName+"/"+containerName)
	return nil
}

func (f *fakePortRuntime) ListNetworkGroups(context.Context) ([]NetworkGroup, error) {
	f.calls = append(f.calls, "ListNetworkGroups")
	return f.networkGroups, nil
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

func hasMount(mounts []MountSpec, want MountSpec) bool {
	for _, mount := range mounts {
		if reflect.DeepEqual(mount, want) {
			return true
		}
	}
	return false
}

var _ Runtime = (*fakePortRuntime)(nil)
