package internal

import (
	"archive/tar"
	"context"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
)

func BuildBaseImage(ctx context.Context, rt Runtime, out, errOut io.Writer) error {
	contextDir, err := FindBaseImageContext()
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "Building %s from %s\n", DefaultImageTag, contextDir)
	return rt.BuildImage(ctx, BuildImageSpec{
		ContextDir: contextDir,
		Tag:        DefaultImageTag,
		Out:        out,
		Err:        errOut,
	})
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
