package image

import (
	"bytes"
	"context"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"

	"github.com/Dave-lab12/ark/internal/paths"
)

//go:embed assets/Containerfile assets/ark-entrypoint assets/ark-ssh assets/ark-zshrc assets/p10k.zsh
var embeddedBaseImageFS embed.FS

var fingerprintFiles = []string{
	"Containerfile",
	"ark-entrypoint",
	"ark-ssh",
	"ark-zshrc",
	"p10k.zsh",
}

type embeddedImageAsset struct {
	Name string
	Path string
	Perm fs.FileMode
}

var embeddedImageAssets = []embeddedImageAsset{
	{Name: "Containerfile", Path: "assets/Containerfile", Perm: 0o644},
	{Name: "ark-entrypoint", Path: "assets/ark-entrypoint", Perm: 0o755},
	{Name: "ark-ssh", Path: "assets/ark-ssh", Perm: 0o755},
	{Name: "ark-zshrc", Path: "assets/ark-zshrc", Perm: 0o644},
	{Name: "p10k.zsh", Path: "assets/p10k.zsh", Perm: 0o644},
}

func BuildBaseImage(ctx context.Context, rt Runtime, config Config, out, errOut io.Writer) error {
	spec, err := config.BuildImageSpec(out, errOut)
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "Building %s from %s\n", spec.Tag, spec.ContextDir)
	return rt.BuildImage(ctx, spec)
}

func ComputeImageFingerprint(runtimeName string, arkVersion string, baseDir string) (string, error) {
	return computeImageFingerprint(runtimeName, arkVersion, baseDir)
}

func computeImageFingerprint(runtimeName string, arkVersion string, sourceDir string) (string, error) {
	hash := sha256.New()
	writeHashRecord(hash, "ark-version", arkVersion)
	writeHashRecord(hash, "runtime", runtimeName)

	files := append([]string(nil), fingerprintFiles...)
	sort.Strings(files)
	for _, name := range files {
		path := filepath.Join(sourceDir, name)
		data, err := os.ReadFile(path)
		if err != nil {
			return "", fmt.Errorf("read image fingerprint file %s: %w", path, err)
		}
		writeHashRecord(hash, filepath.ToSlash(filepath.Join("~/.ark/image", name)), string(data))
	}
	return "sha256:" + hex.EncodeToString(hash.Sum(nil)), nil
}

func writeHashRecord(w io.Writer, key, value string) {
	fmt.Fprintf(w, "%d:%s\n%d:%s\n", len(key), key, len(value), value)
}

func EnsureImageSource(config Config) error {
	source, err := config.ImageSourcePath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(source, 0o700); err != nil {
		return fmt.Errorf("create image source directory %s: %w", source, err)
	}
	overwrite := config.UsesDefaultImageSource(source)
	for _, asset := range embeddedImageAssets {
		dst := filepath.Join(source, asset.Name)
		data, err := readEmbeddedImageAsset(asset)
		if err != nil {
			return err
		}
		if overwrite {
			current, err := os.ReadFile(dst)
			if err == nil && bytes.Equal(current, data) {
				if chmodErr := os.Chmod(dst, asset.Perm); chmodErr != nil {
					return fmt.Errorf("chmod image source file %s: %w", dst, chmodErr)
				}
				continue
			}
			if err != nil && !errors.Is(err, os.ErrNotExist) {
				return fmt.Errorf("read image source file %s: %w", dst, err)
			}
		} else {
			if _, err := os.Stat(dst); err == nil {
				continue
			} else if !errors.Is(err, os.ErrNotExist) {
				return fmt.Errorf("stat image source file %s: %w", dst, err)
			}
		}
		if err := paths.AtomicWriteFile(dst, data, asset.Perm); err != nil {
			return err
		}
	}
	return nil
}

func readEmbeddedImageAsset(asset embeddedImageAsset) ([]byte, error) {
	data, err := embeddedBaseImageFS.ReadFile(asset.Path)
	if err != nil {
		return nil, fmt.Errorf("read embedded image source %s: %w", asset.Path, err)
	}
	return data, nil
}
