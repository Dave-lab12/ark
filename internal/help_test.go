package internal

import (
	"bytes"
	"context"
	"fmt"
	"strconv"
	"strings"
	"testing"
)

func TestRootHelpDisablesColorWhenNotInteractive(t *testing.T) {
	var out bytes.Buffer
	var errOut bytes.Buffer
	app := &App{out: &out, errOut: &errOut}
	cmd := app.rootCommand(context.Background())
	cmd.SetArgs([]string{"--help"})
	if err := cmd.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("ExecuteContext: %v", err)
	}
	if strings.Contains(out.String(), "\x1b[") {
		t.Fatalf("non-interactive help should not contain ANSI escapes:\n%q", out.String())
	}
}

func TestRootHelpShowsCommandGroups(t *testing.T) {
	var out bytes.Buffer
	var errOut bytes.Buffer
	app := &App{out: &out, errOut: &errOut}
	cmd := app.rootCommand(context.Background())
	cmd.SetArgs([]string{"--help"})

	if err := cmd.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("ExecuteContext: %v", err)
	}

	got := out.String()
	for _, want := range []string{
		"PROJECTS",
		"ARK",
		"INSIDE A PROJECT",
		"init        create a project",
		"edit        open a project in your editor",
		"rebuild     recreate from the current base image",
		"config      show or edit ark config",
		"  path      print config file path",
		"image       manage the base image",
		"  status    show base image status",
		"  rebuild   rebuild the reusable base image",
		"devcontainer  manage devcontainer.json generation",
		"doctor      check local setup",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("help output missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "temp") {
		t.Fatalf("hidden temp command appeared in help:\n%s", got)
	}
}

func TestImageHelpShowsImageSubcommands(t *testing.T) {
	var out bytes.Buffer
	var errOut bytes.Buffer
	app := &App{out: &out, errOut: &errOut}
	cmd := app.rootCommand(context.Background())
	cmd.SetArgs([]string{"image", "--help"})

	if err := cmd.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("ExecuteContext: %v", err)
	}

	got := out.String()
	for _, want := range []string{
		"manage the base image",
		"USAGE",
		"ark image <command>",
		"COMMANDS",
		"status      show base image status",
		"rebuild     rebuild the reusable base image",
		"EXAMPLES",
		"ark image status",
		"ark image rebuild",
		"FLAGS",
		"--runtime",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("image help missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "INSIDE A PROJECT") {
		t.Fatalf("image help should not show project help:\n%s", got)
	}
}

func TestProjectHelpRegisteredProject(t *testing.T) {
	ctx := context.Background()
	port3000 := mustPortMapping(t, "3000")
	project := testProject(t, []PortMapping{port3000}, false)
	rt := &fakePortRuntime{
		inspectResults: []*Container{{Running: true, Status: "running"}},
	}
	app, out, _ := newHelpExecuteApp(t, &project, rt)

	if err := app.Execute(ctx, []string{"app", "--help"}); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	got := out.String()
	for _, want := range []string{
		"ark app",
		"project \"app\"",
		"Status:    running",
		"Ports:     3000",
		"USAGE",
		"ark app --port 3000",
		"ark app --port -3000",
		"ark app --ports",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("project help missing %q:\n%s", want, got)
		}
	}
}

func TestProjectHelpUnregisteredProject(t *testing.T) {
	ctx := context.Background()
	rt := &fakePortRuntime{}
	app, out, _ := newHelpExecuteApp(t, nil, rt)

	if err := app.Execute(ctx, []string{"missing", "--help"}); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	got := out.String()
	for _, want := range []string{
		"project \"missing\" is not registered",
		"Create it with:",
		"ark init missing",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("unregistered help missing %q:\n%s", want, got)
		}
	}
	if len(rt.calls) != 0 {
		t.Fatalf("runtime should not be touched for unregistered help, got %v", rt.calls)
	}
}

func newHelpExecuteApp(t *testing.T, project *Project, rt *fakePortRuntime) (*App, *bytes.Buffer, *bytes.Buffer) {
	t.Helper()
	ctx := context.Background()
	paths := portTestPaths(t.TempDir())
	writeHelpConfig(t, paths)
	registry := NewRegistry(paths)
	if err := registry.EnsureDefault(ctx); err != nil {
		t.Fatalf("EnsureDefault: %v", err)
	}
	if project != nil {
		if err := registry.Update(ctx, func(state *State) error {
			state.Projects[project.Name] = *project
			return nil
		}); err != nil {
			t.Fatalf("seed registry: %v", err)
		}
	}

	var out bytes.Buffer
	var errOut bytes.Buffer
	app := &App{
		paths:  paths,
		in:     strings.NewReader(""),
		out:    &out,
		errOut: &errOut,
		runtimeByName: func(name string) (Runtime, error) {
			if name != RuntimeDocker {
				return nil, fmt.Errorf("unexpected runtime %q", name)
			}
			return rt, nil
		},
	}
	return app, &out, &errOut
}

func writeHelpConfig(t *testing.T, paths Paths) {
	t.Helper()
	content := fmt.Sprintf(`version = 1
runtime = %s
project_root = %s

[image]
tag = %s
source = %s
`,
		strconv.Quote(RuntimeDocker),
		strconv.Quote(paths.ProjectRoot),
		strconv.Quote(DefaultImageTag),
		strconv.Quote(paths.ImageDir),
	)
	if err := atomicWriteFile(paths.ConfigFile, []byte(content), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
}
