package internal

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDevcontainerWriteInProjectPermissionsExcludeAndImageInfo(t *testing.T) {
	ctx := context.Background()
	rt := &fakePortRuntime{}
	project := testProject(t, nil, false)
	project.ImageFingerprint = "stale-project-fingerprint"
	app, _, _ := newPortTestApp(t, "", project, rt)
	expectedFingerprint := seedCurrentImage(t, ctx, app, project.Runtime)

	infoDir := filepath.Join(project.Path, ".git", "info")
	if err := os.MkdirAll(infoDir, 0o755); err != nil {
		t.Fatal(err)
	}
	excludePath := filepath.Join(infoDir, "exclude")
	if err := os.WriteFile(excludePath, []byte("# local excludes\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := app.DevcontainerWrite(ctx, project.Name, true); err != nil {
		t.Fatalf("DevcontainerWrite: %v", err)
	}

	target := filepath.Join(project.Path, ".devcontainer", "devcontainer.json")
	assertPathPerm(t, filepath.Dir(target), 0o755)
	assertPathPerm(t, target, 0o644)

	got := readDevcontainerJSONFile(t, target)
	if got["image"] != app.config.Image.Tag {
		t.Fatalf("image = %v, want %s", got["image"], app.config.Image.Tag)
	}
	if fingerprint := arkCustomizationString(t, got, "image_fingerprint"); fingerprint != expectedFingerprint {
		t.Fatalf("image_fingerprint = %q, want %q", fingerprint, expectedFingerprint)
	}
	data, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "stale-project-fingerprint") {
		t.Fatalf("generated devcontainer used stale project fingerprint:\n%s", data)
	}
	if len(rt.imageExistsTag) != 1 || rt.imageExistsTag[0] != app.config.Image.Tag {
		t.Fatalf("ImageExists calls = %v, want [%s]", rt.imageExistsTag, app.config.Image.Tag)
	}

	exclude, err := os.ReadFile(excludePath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(exclude), ".devcontainer/devcontainer.json") {
		t.Fatalf("exclude was not updated:\n%s", exclude)
	}
}

func TestDevcontainerWriteArkOwnedPermissions(t *testing.T) {
	ctx := context.Background()
	rt := &fakePortRuntime{}
	project := testProject(t, nil, false)
	app, _, _ := newPortTestApp(t, "", project, rt)
	seedCurrentImage(t, ctx, app, project.Runtime)

	if err := app.DevcontainerWrite(ctx, project.Name, false); err != nil {
		t.Fatalf("DevcontainerWrite: %v", err)
	}

	target := app.paths.ArkOwnedDevcontainerPath(project)
	assertPathPerm(t, filepath.Dir(target), 0o700)
	assertPathPerm(t, target, 0o600)
}

func assertPathPerm(t *testing.T, path string, want os.FileMode) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat %s: %v", path, err)
	}
	if got := info.Mode().Perm(); got != want {
		t.Fatalf("%s mode = %03o, want %03o", path, got, want)
	}
}

func arkCustomizationString(t *testing.T, got map[string]any, key string) string {
	t.Helper()
	customizations, ok := got["customizations"].(map[string]any)
	if !ok {
		t.Fatalf("customizations = %#v, want object", got["customizations"])
	}
	ark, ok := customizations["ark"].(map[string]any)
	if !ok {
		t.Fatalf("customizations.ark = %#v, want object", customizations["ark"])
	}
	value, ok := ark[key].(string)
	if !ok {
		t.Fatalf("customizations.ark.%s = %#v, want string", key, ark[key])
	}
	return value
}
