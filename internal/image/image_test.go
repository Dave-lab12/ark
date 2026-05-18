package image

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestEnsureImageSourceWritesEmbeddedAssets(t *testing.T) {
	config := DefaultConfig()
	config.Image.Source = filepath.Join(t.TempDir(), "image")

	if err := EnsureImageSource(config); err != nil {
		t.Fatal(err)
	}

	for _, asset := range embeddedImageAssets {
		want, err := readEmbeddedImageAsset(asset)
		if err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(config.Image.Source, asset.Name)
		got, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got, want) {
			t.Fatalf("%s was not written from embedded asset", asset.Name)
		}
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != asset.Perm {
			t.Fatalf("%s mode = %v, want %v", asset.Name, info.Mode().Perm(), asset.Perm)
		}
	}
}

func TestEnsureImageSourceRefreshesDefaultImageSource(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	config := DefaultConfig()
	source, err := config.ImageSourcePath()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(source, 0o700); err != nil {
		t.Fatal(err)
	}
	containerfile := filepath.Join(source, "Containerfile")
	if err := os.WriteFile(containerfile, []byte("local edit"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := EnsureImageSource(config); err != nil {
		t.Fatal(err)
	}

	got, err := os.ReadFile(containerfile)
	if err != nil {
		t.Fatal(err)
	}
	want, err := readEmbeddedImageAsset(embeddedImageAssets[0])
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatal("default image source was not refreshed from embedded asset")
	}
}

func TestEnsureImageSourcePreservesCustomImageSource(t *testing.T) {
	config := DefaultConfig()
	config.Image.Source = filepath.Join(t.TempDir(), "custom-image")
	if err := os.MkdirAll(config.Image.Source, 0o700); err != nil {
		t.Fatal(err)
	}
	containerfile := filepath.Join(config.Image.Source, "Containerfile")
	if err := os.WriteFile(containerfile, []byte("custom"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := EnsureImageSource(config); err != nil {
		t.Fatal(err)
	}

	got, err := os.ReadFile(containerfile)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "custom" {
		t.Fatalf("custom image source was overwritten: %q", string(got))
	}
}
