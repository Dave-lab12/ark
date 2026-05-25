package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/BurntSushi/toml"
	"github.com/Dave-lab12/ark/internal/core"
)

func TestDefaultConfigTOMLMatchesDefaultConfig(t *testing.T) {
	var got Config
	if _, err := toml.Decode(defaultConfigTOML, &got); err != nil {
		t.Fatalf("decode defaultConfigTOML: %v", err)
	}
	if err := got.normalize(); err != nil {
		t.Fatalf("normalize parsed defaultConfigTOML: %v", err)
	}

	want := DefaultConfig()
	if err := want.normalize(); err != nil {
		t.Fatalf("normalize DefaultConfig: %v", err)
	}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("defaultConfigTOML drifted from DefaultConfig:\n got: %#v\nwant: %#v", got, want)
	}
}

func TestReadOnlyConfigMounts(t *testing.T) {
	root := t.TempDir()
	configDir := filepath.Join(root, "app")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	configFile := filepath.Join(root, ".gitconfig")
	if err := os.WriteFile(configFile, []byte("[user]\n\tname = yab\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	config := DefaultConfig()
	config.Mounts.ReadOnly = []ReadOnlyMountConfig{
		{Source: configDir, Target: "/home/dev/.config/app"},
		{Source: configFile, Target: "/home/dev/.gitconfig"},
	}

	got, err := config.ReadOnlyConfigMounts()
	if err != nil {
		t.Fatalf("ReadOnlyConfigMounts: %v", err)
	}

	want := []core.MountSpec{
		{
			Type:     core.MountTypeBind,
			Source:   configDir,
			Target:   "/home/dev/.config/app",
			ReadOnly: true,
		},
		{
			Type:     core.MountTypeBind,
			Source:   configFile,
			Target:   "/home/dev/.gitconfig",
			ReadOnly: true,
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ReadOnlyConfigMounts mismatch:\n got: %#v\nwant: %#v", got, want)
	}
}

func TestReadOnlyConfigMountsInfersTargetFromHomeRelativeSource(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", root)

	configDir := filepath.Join(root, ".config", "app")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	configFile := filepath.Join(root, ".gitconfig")
	if err := os.WriteFile(configFile, []byte("[user]\n\tname = yab\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	config := DefaultConfig()
	config.Mounts.ReadOnly = []ReadOnlyMountConfig{
		{Source: ".config/app"},
		{Source: ".gitconfig"},
	}

	got, err := config.ReadOnlyConfigMounts()
	if err != nil {
		t.Fatalf("ReadOnlyConfigMounts: %v", err)
	}

	want := []core.MountSpec{
		{
			Type:     core.MountTypeBind,
			Source:   configDir,
			Target:   "/home/dev/.config/app",
			ReadOnly: true,
		},
		{
			Type:     core.MountTypeBind,
			Source:   configFile,
			Target:   "/home/dev/.gitconfig",
			ReadOnly: true,
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ReadOnlyConfigMounts mismatch:\n got: %#v\nwant: %#v", got, want)
	}
}

func TestReadOnlyConfigMountsRejectReservedTarget(t *testing.T) {
	root := t.TempDir()
	cacheDir := filepath.Join(root, "cache")
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	config := DefaultConfig()
	config.Mounts.ReadOnly = []ReadOnlyMountConfig{
		{Source: cacheDir, Target: "/home/dev/.cache/nvim"},
	}

	_, err := config.ReadOnlyConfigMounts()
	if err == nil {
		t.Fatalf("expected error")
	}
	if got := err.Error(); got == "" || !containsAll(got, []string{"mounts.readonly[0]", "overlaps Ark-managed path /home/dev/.cache"}) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestReadOnlyConfigMountsRejectInvalidSourceType(t *testing.T) {
	root := t.TempDir()
	socketPath := filepath.Join(root, "socket")
	if err := os.Symlink(filepath.Join(root, "missing"), socketPath); err != nil {
		t.Fatalf("Symlink: %v", err)
	}

	config := DefaultConfig()
	config.Mounts.ReadOnly = []ReadOnlyMountConfig{
		{Source: socketPath, Target: "/home/dev/.config/socket"},
	}

	_, err := config.ReadOnlyConfigMounts()
	if err == nil {
		t.Fatalf("expected error")
	}
	if got := err.Error(); got == "" || !containsAll(got, []string{"mounts.readonly[0]", "does not exist"}) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestReadOnlyConfigMountsRejectDuplicateTarget(t *testing.T) {
	root := t.TempDir()
	firstFile := filepath.Join(root, "first")
	if err := os.WriteFile(firstFile, []byte("one"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	secondFile := filepath.Join(root, "second")
	if err := os.WriteFile(secondFile, []byte("two"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	config := DefaultConfig()
	config.Mounts.ReadOnly = []ReadOnlyMountConfig{
		{Source: firstFile, Target: "/home/dev/.gitconfig"},
		{Source: secondFile, Target: "/home/dev/.gitconfig"},
	}

	_, err := config.ReadOnlyConfigMounts()
	if err == nil {
		t.Fatalf("expected error")
	}
	if got := err.Error(); got == "" || !containsAll(got, []string{"mounts.readonly[1]", "already used by mounts.readonly[0]"}) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestReadOnlyConfigMountsRejectMissingSource(t *testing.T) {
	config := DefaultConfig()
	config.Mounts.ReadOnly = []ReadOnlyMountConfig{
		{Target: ".gitconfig"},
	}

	_, err := config.ReadOnlyConfigMounts()
	if err == nil {
		t.Fatalf("expected error")
	}
	if got := err.Error(); got == "" || !containsAll(got, []string{"mounts.readonly[0]", "source is empty"}) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestReadOnlyConfigMountsRejectImplicitTargetOutsideHome(t *testing.T) {
	root := t.TempDir()
	otherRoot := t.TempDir()
	t.Setenv("HOME", root)

	filePath := filepath.Join(otherRoot, "gitconfig")
	if err := os.WriteFile(filePath, []byte("name = yab\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	config := DefaultConfig()
	config.Mounts.ReadOnly = []ReadOnlyMountConfig{
		{Source: filePath},
	}

	_, err := config.ReadOnlyConfigMounts()
	if err == nil {
		t.Fatalf("expected error")
	}
	if got := err.Error(); got == "" || !containsAll(got, []string{"mounts.readonly[0]", "cannot infer a container target", "set target explicitly"}) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestReadOnlyConfigMountsAllowsRelativeExplicitTarget(t *testing.T) {
	root := t.TempDir()
	filePath := filepath.Join(root, "gitconfig")
	if err := os.WriteFile(filePath, []byte("name = yab\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	config := DefaultConfig()
	config.Mounts.ReadOnly = []ReadOnlyMountConfig{
		{Source: filePath, Target: ".gitconfig"},
	}

	got, err := config.ReadOnlyConfigMounts()
	if err != nil {
		t.Fatalf("ReadOnlyConfigMounts: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("ReadOnlyConfigMounts length = %d, want 1", len(got))
	}
	if got[0].Target != "/home/dev/.gitconfig" {
		t.Fatalf("Target = %q, want %q", got[0].Target, "/home/dev/.gitconfig")
	}
}

func TestReadOnlyConfigMountsRejectRelativeSourceEscapingHome(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", root)

	config := DefaultConfig()
	config.Mounts.ReadOnly = []ReadOnlyMountConfig{
		{Source: "../outside"},
	}

	_, err := config.ReadOnlyConfigMounts()
	if err == nil {
		t.Fatalf("expected error")
	}
	if got := err.Error(); got == "" || !containsAll(got, []string{"mounts.readonly[0]", "escapes your home directory"}) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func containsAll(s string, fragments []string) bool {
	for _, fragment := range fragments {
		if !strings.Contains(s, fragment) {
			return false
		}
	}
	return true
}
