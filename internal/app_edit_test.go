package internal

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEditProjectLaunchesEditorWithRemoteAuthority(t *testing.T) {
	ctx := context.Background()
	rt := &fakePortRuntime{
		inspectResults: []*Container{{Running: true, Status: "running"}},
	}
	project := testProject(t, nil, false)
	app, out, _ := newPortTestApp(t, "", project, rt)
	editor := fakeEditorBinary(t, "code")
	app.config.Editor.Default = editor
	app.config.Container.Workdir = "/work"

	var got struct{ binary, remote, folder string }
	prevLaunch := launchEditor
	t.Cleanup(func() { launchEditor = prevLaunch })
	launchEditor = func(binary, remote, folder string) error {
		got.binary = binary
		got.remote = remote
		got.folder = folder
		return nil
	}

	if err := app.EditProject(ctx, project.Name, EditOptions{}); err != nil {
		t.Fatalf("EditProject: %v", err)
	}

	if got.binary != editor {
		t.Fatalf("binary = %q, want %q", got.binary, editor)
	}
	wantRemote := BuildRemoteAuthority(project.ContainerName)
	if got.remote != wantRemote {
		t.Fatalf("remote = %q, want %q", got.remote, wantRemote)
	}
	if got.folder != "/work" {
		t.Fatalf("folder = %q, want /work", got.folder)
	}
	if !strings.Contains(out.String(), "Opening") {
		t.Fatalf("missing announce line: %q", out.String())
	}
}

func TestEditProjectStartsStoppedContainer(t *testing.T) {
	ctx := context.Background()
	rt := &fakePortRuntime{
		inspectResults: []*Container{
			{Running: false, Status: "exited"},
			{Running: true, Status: "running"},
		},
	}
	project := testProject(t, nil, false)
	app, _, _ := newPortTestApp(t, "", project, rt)
	app.config.Editor.Default = fakeEditorBinary(t, "code")

	prevLaunch := launchEditor
	t.Cleanup(func() { launchEditor = prevLaunch })
	launchEditor = func(string, string, string) error { return nil }

	if err := app.EditProject(ctx, project.Name, EditOptions{}); err != nil {
		t.Fatalf("EditProject: %v", err)
	}

	foundStart := false
	for _, call := range rt.calls {
		if call == "Start" {
			foundStart = true
			break
		}
	}
	if !foundStart {
		t.Fatalf("expected Start call for stopped container, got %v", rt.calls)
	}
}

func TestEditProjectAppliesPortChangeBeforeLaunch(t *testing.T) {
	ctx := context.Background()
	rt := &fakePortRuntime{
		inspectResults: []*Container{
			{Running: false, Status: "exited"},
			{Running: false, Status: "created"},
			{Running: true, Status: "running"},
		},
	}
	project := testProject(t, nil, false)
	app, _, _ := newPortTestApp(t, "", project, rt)
	app.config.Editor.Default = fakeEditorBinary(t, "code")

	prevLaunch := launchEditor
	t.Cleanup(func() { launchEditor = prevLaunch })
	launchEditor = func(string, string, string) error { return nil }

	opts := EditOptions{Ports: PortOptions{Specs: []string{"3000"}}}
	if err := app.EditProject(ctx, project.Name, opts); err != nil {
		t.Fatalf("EditProject: %v", err)
	}

	var portCallSeen bool
	for _, call := range rt.calls {
		if call == "Create" {
			portCallSeen = true
			break
		}
	}
	if !portCallSeen {
		t.Fatalf("expected Create call from port change, got %v", rt.calls)
	}
	final := mustRegistryProject(t, app, project.Name)
	if len(final.Ports) != 1 || final.Ports[0].ContainerPort != "3000" {
		t.Fatalf("ports not persisted: %#v", final.Ports)
	}
}

func TestEditProjectHonorsFolderFlag(t *testing.T) {
	ctx := context.Background()
	rt := &fakePortRuntime{
		inspectResults: []*Container{{Running: true, Status: "running"}},
	}
	project := testProject(t, nil, false)
	app, out, _ := newPortTestApp(t, "", project, rt)
	app.config.Editor.Default = fakeEditorBinary(t, "code")
	app.config.Container.Workdir = "/work"

	var got struct{ remote, folder string }
	prevLaunch := launchEditor
	t.Cleanup(func() { launchEditor = prevLaunch })
	launchEditor = func(_, remote, folder string) error {
		got.remote = remote
		got.folder = folder
		return nil
	}

	if err := app.EditProject(ctx, project.Name, EditOptions{Folder: "mindplex_backend"}); err != nil {
		t.Fatalf("EditProject: %v", err)
	}
	if got.folder != "/work/mindplex_backend" {
		t.Fatalf("folder = %q, want /work/mindplex_backend", got.folder)
	}
	if !strings.HasPrefix(got.remote, "attached-container+") {
		t.Fatalf("remote = %q, want attached-container+...", got.remote)
	}
	if !strings.Contains(out.String(), "mindplex_backend") {
		t.Fatalf("announce line missing folder: %q", out.String())
	}
}

func TestEditProjectNativeModeWritesInProject(t *testing.T) {
	ctx := context.Background()
	rt := &fakePortRuntime{}
	project := testProject(t, nil, false)
	app, _, _ := newPortTestApp(t, "", project, rt)
	app.config.Editor.Default = fakeEditorBinary(t, "zed")

	prevLaunch := launchEditorPath
	t.Cleanup(func() { launchEditorPath = prevLaunch })
	var launchedPath string
	launchEditorPath = func(_, path string) error {
		launchedPath = path
		return nil
	}

	if err := app.EditProject(ctx, project.Name, EditOptions{}); err != nil {
		t.Fatalf("EditProject: %v", err)
	}

	devcontainerPath := filepath.Join(project.Path, ".devcontainer", "devcontainer.json")
	data, err := os.ReadFile(devcontainerPath)
	if err != nil {
		t.Fatalf("read generated devcontainer: %v", err)
	}
	if !strings.Contains(string(data), `"generated": true`) {
		t.Fatalf("missing ark marker:\n%s", data)
	}
	if !strings.Contains(string(data), `"overrideCommand": false`) {
		t.Fatalf("missing overrideCommand: false:\n%s", data)
	}
	if launchedPath != project.Path {
		t.Fatalf("editor opened at %q, want %q", launchedPath, project.Path)
	}
}

func TestEditProjectNativeModeRefusesToOverwriteNonArkFile(t *testing.T) {
	ctx := context.Background()
	rt := &fakePortRuntime{}
	project := testProject(t, nil, false)
	app, _, _ := newPortTestApp(t, "", project, rt)
	app.config.Editor.Default = fakeEditorBinary(t, "zed")

	devcontainerPath := filepath.Join(project.Path, ".devcontainer", "devcontainer.json")
	if err := os.MkdirAll(filepath.Dir(devcontainerPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(devcontainerPath, []byte(`{"name":"user's own"}`), 0o644); err != nil {
		t.Fatal(err)
	}

	err := app.EditProject(ctx, project.Name, EditOptions{})
	if err == nil {
		t.Fatal("expected refusal")
	}
	if !strings.Contains(err.Error(), "refusing to overwrite") {
		t.Fatalf("error didn't mention refusal: %v", err)
	}
}

func TestEditProjectNativeModeOverwritesArkGeneratedFile(t *testing.T) {
	ctx := context.Background()
	rt := &fakePortRuntime{}
	project := testProject(t, nil, false)
	app, _, _ := newPortTestApp(t, "", project, rt)
	app.config.Editor.Default = fakeEditorBinary(t, "zed")

	prevLaunch := launchEditorPath
	t.Cleanup(func() { launchEditorPath = prevLaunch })
	launchEditorPath = func(string, string) error { return nil }

	devcontainerPath := filepath.Join(project.Path, ".devcontainer", "devcontainer.json")
	if err := os.MkdirAll(filepath.Dir(devcontainerPath), 0o755); err != nil {
		t.Fatal(err)
	}
	seed := []byte(`{"name":"old","customizations":{"ark":{"generated":true}}}`)
	if err := os.WriteFile(devcontainerPath, seed, 0o644); err != nil {
		t.Fatal(err)
	}

	if err := app.EditProject(ctx, project.Name, EditOptions{}); err != nil {
		t.Fatalf("EditProject: %v", err)
	}

	data, err := os.ReadFile(devcontainerPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), `"name": "old"`) {
		t.Fatalf("ark-generated file was not overwritten:\n%s", data)
	}
}

func TestEditProjectNativeModeFolderMapping(t *testing.T) {
	ctx := context.Background()
	rt := &fakePortRuntime{}
	project := testProject(t, nil, false)
	app, _, _ := newPortTestApp(t, "", project, rt)
	app.config.Editor.Default = fakeEditorBinary(t, "zed")
	app.config.Container.Workdir = "/work"

	prevLaunch := launchEditorPath
	t.Cleanup(func() { launchEditorPath = prevLaunch })
	var launchedPath string
	launchEditorPath = func(_, path string) error {
		launchedPath = path
		return nil
	}

	if err := app.EditProject(ctx, project.Name, EditOptions{Folder: "packages/api"}); err != nil {
		t.Fatalf("EditProject: %v", err)
	}

	want := filepath.Join(project.Path, "packages", "api")
	if launchedPath != want {
		t.Fatalf("launched %q, want %q", launchedPath, want)
	}
}

func TestEditProjectNativeModeFolderOutsideWorkdir(t *testing.T) {
	ctx := context.Background()
	rt := &fakePortRuntime{}
	project := testProject(t, nil, false)
	app, _, _ := newPortTestApp(t, "", project, rt)
	app.config.Editor.Default = fakeEditorBinary(t, "zed")
	app.config.Container.Workdir = "/work"

	err := app.EditProject(ctx, project.Name, EditOptions{Folder: "/home/dev"})
	if err == nil {
		t.Fatal("expected error for folder outside workdir")
	}
	if !strings.Contains(err.Error(), "outside the container workdir") {
		t.Fatalf("error didn't mention workdir: %v", err)
	}
}

func TestAddToGitLocalExclude(t *testing.T) {
	dir := t.TempDir()
	info := filepath.Join(dir, ".git", "info")
	if err := os.MkdirAll(info, 0o755); err != nil {
		t.Fatal(err)
	}
	excludePath := filepath.Join(info, "exclude")
	if err := os.WriteFile(excludePath, []byte("# user content\n*.swp\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := addToGitLocalExclude(dir, ".devcontainer/devcontainer.json"); err != nil {
		t.Fatalf("addToGitLocalExclude: %v", err)
	}

	data, err := os.ReadFile(excludePath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), ".devcontainer/devcontainer.json") {
		t.Fatalf("entry not added:\n%s", data)
	}

	// Second call should be a no-op.
	before := string(data)
	if err := addToGitLocalExclude(dir, ".devcontainer/devcontainer.json"); err != nil {
		t.Fatal(err)
	}
	data, _ = os.ReadFile(excludePath)
	if string(data) != before {
		t.Fatalf("second call modified the file:\nbefore:\n%s\nafter:\n%s", before, data)
	}
}

func TestAddToGitLocalExcludeNoRepo(t *testing.T) {
	dir := t.TempDir()
	if err := addToGitLocalExclude(dir, "anything"); err != nil {
		t.Fatalf("expected silent no-op, got %v", err)
	}
}

func fakeEditorBinary(t *testing.T, name string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("write fake editor: %v", err)
	}
	return path
}
