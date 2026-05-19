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
	nvimDir := filepath.Join(root, "nvim")
	if err := os.MkdirAll(nvimDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	tmuxFile := filepath.Join(root, ".tmux.conf")
	if err := os.WriteFile(tmuxFile, []byte("set -g mouse on\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	config := DefaultConfig()
	config.Mounts.Neovim = nvimDir
	config.Mounts.Tmux = tmuxFile

	got, err := config.ReadOnlyConfigMounts()
	if err != nil {
		t.Fatalf("ReadOnlyConfigMounts: %v", err)
	}

	want := []core.MountSpec{
		{
			Type:     core.MountTypeBind,
			Source:   nvimDir,
			Target:   "/home/dev/.config/nvim",
			ReadOnly: true,
		},
		{
			Type:     core.MountTypeBind,
			Source:   tmuxFile,
			Target:   "/home/dev/.tmux.conf",
			ReadOnly: true,
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ReadOnlyConfigMounts mismatch:\n got: %#v\nwant: %#v", got, want)
	}
}

func TestReadOnlyConfigMountsRejectInvalidSourceShape(t *testing.T) {
	root := t.TempDir()
	wrongDir := filepath.Join(root, "dotfiles")
	if err := os.MkdirAll(wrongDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	config := DefaultConfig()
	config.Mounts.Neovim = wrongDir

	_, err := config.ReadOnlyConfigMounts()
	if err == nil {
		t.Fatalf("expected error")
	}
	if got := err.Error(); got == "" || !containsAll(got, []string{"mounts.neovim", "must end with one of"}) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestReadOnlyConfigMountsRejectWrongTmuxType(t *testing.T) {
	root := t.TempDir()
	tmuxDir := filepath.Join(root, "tmux.conf")
	if err := os.MkdirAll(tmuxDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	config := DefaultConfig()
	config.Mounts.Tmux = tmuxDir

	_, err := config.ReadOnlyConfigMounts()
	if err == nil {
		t.Fatalf("expected error")
	}
	if got := err.Error(); got == "" || !containsAll(got, []string{"mounts.tmux", "regular file"}) {
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
