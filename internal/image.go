package internal

import (
	"archive/tar"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
)

var fingerprintFiles = []string{
	"Containerfile",
	"ark-entrypoint",
	"ark-ssh",
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
	defaultSource, err := FindBaseImageContext()
	if err != nil {
		return err
	}
	for _, name := range fingerprintFiles {
		dst := filepath.Join(source, name)
		if _, err := os.Stat(dst); err == nil {
			continue
		} else if !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("stat image source file %s: %w", dst, err)
		}
		src := filepath.Join(defaultSource, name)
		data, err := os.ReadFile(src)
		if err != nil {
			return fmt.Errorf("read default image source %s: %w", src, err)
		}
		info, err := os.Stat(src)
		if err != nil {
			return fmt.Errorf("stat default image source %s: %w", src, err)
		}
		perm := info.Mode().Perm()
		if perm == 0 {
			perm = 0o600
		}
		if err := atomicWriteFile(dst, data, perm); err != nil {
			return err
		}
	}
	return nil
}

func FindBaseImageContext() (string, error) {
	cwd, err := os.Getwd()
	if err == nil {
		if path, ok := findImageContextUpward(cwd); ok {
			return path, nil
		}
	}

	exe, err := os.Executable()
	if err == nil {
		if path, ok := findImageContextUpward(filepath.Dir(exe)); ok {
			return path, nil
		}
	}

	return "", fmt.Errorf("find images/base/Containerfile: run ark from the source tree for this MVP")
}

func findImageContextUpward(start string) (string, bool) {
	dir := start
	for {
		candidate := filepath.Join(dir, "images", "base")
		if _, err := os.Stat(filepath.Join(candidate, "Containerfile")); err == nil {
			return candidate, true
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", false
		}
		dir = parent
	}
}

func tarDirectory(root string) (io.ReadCloser, error) {
	info, err := os.Stat(root)
	if err != nil {
		return nil, fmt.Errorf("stat build context %s: %w", root, err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("build context is not a directory: %s", root)
	}

	pr, pw := io.Pipe()
	go func() {
		tw := tar.NewWriter(pw)
		err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if path == root {
				return nil
			}
			info, err := entry.Info()
			if err != nil {
				return err
			}
			header, err := tar.FileInfoHeader(info, "")
			if err != nil {
				return err
			}
			rel, err := filepath.Rel(root, path)
			if err != nil {
				return err
			}
			header.Name = filepath.ToSlash(rel)
			if err := tw.WriteHeader(header); err != nil {
				return err
			}
			if entry.IsDir() {
				return nil
			}
			file, err := os.Open(path)
			if err != nil {
				return err
			}
			defer file.Close()
			_, err = io.Copy(tw, file)
			return err
		})
		closeErr := tw.Close()
		if err == nil {
			err = closeErr
		}
		_ = pw.CloseWithError(err)
	}()
	return pr, nil
}
