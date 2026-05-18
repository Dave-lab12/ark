package internal

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type Paths struct {
	ArkHome          string
	ImageDir         string
	ImageStateFile   string
	SocketsDir       string
	CacheDir         string
	LogsDir          string
	DevcontainersDir string
	ConfigFile       string
	StateFile        string
	LockFile         string
	ProjectRoot      string
}

func DefaultPaths() (Paths, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return Paths{}, fmt.Errorf("find home directory: %w", err)
	}

	arkHome := filepath.Join(home, ".ark")
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
		ProjectRoot:      filepath.Join(home, "ark"),
	}, nil
}

func (p Paths) Ensure() error {
	if err := p.EnsureControlPlane(); err != nil {
		return err
	}
	if err := os.MkdirAll(p.ProjectRoot, 0o755); err != nil {
		return fmt.Errorf("create project root %s: %w", p.ProjectRoot, err)
	}
	return nil
}

func (p Paths) EnsureControlPlane() error {
	dirs := []string{
		p.ArkHome,
		p.ImageDir,
		p.SocketsDir,
		p.CacheDir,
		p.LogsDir,
		p.DevcontainersDir,
	}
	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return fmt.Errorf("create Ark directory %s: %w", dir, err)
		}
	}
	return nil
}

func (p Paths) EnsureConfigDir() error {
	if err := os.MkdirAll(filepath.Dir(p.ConfigFile), 0o700); err != nil {
		return fmt.Errorf("create config directory %s: %w", filepath.Dir(p.ConfigFile), err)
	}
	return nil
}

// ArkOwnedDevcontainerPath returns the path to the ark-owned devcontainer
// file for a project. Used by `ark devcontainer write` for inspection.
// It is NOT used during `ark edit` — native mode writes into the project,
// attach mode doesn't write a devcontainer file at all.
func (p Paths) ArkOwnedDevcontainerPath(project Project) string {
	return filepath.Join(p.DevcontainersDir, project.ID, "devcontainer.json")
}

func (p Paths) ProjectSocketDir(project Project) string {
	if project.ID == "" {
		return filepath.Join(p.SocketsDir, project.Name)
	}
	return filepath.Join(p.SocketsDir, project.ID)
}

func (p Paths) ProjectPath(name string) (string, error) {
	// Callers are responsible for ValidateProjectName; the path-escape check
	// below still catches anything dangerous regardless.
	root, err := filepath.Abs(p.ProjectRoot)
	if err != nil {
		return "", fmt.Errorf("resolve project root: %w", err)
	}
	path, err := filepath.Abs(filepath.Join(root, name))
	if err != nil {
		return "", fmt.Errorf("resolve project path: %w", err)
	}
	if path != root && !strings.HasPrefix(path, root+string(os.PathSeparator)) {
		return "", fmt.Errorf("project path escapes project root: %s", path)
	}
	return path, nil
}

func IsInsidePath(path, root string) bool {
	pathAbs, err := filepath.Abs(path)
	if err != nil {
		return false
	}
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return false
	}
	rel, err := filepath.Rel(rootAbs, pathAbs)
	if err != nil {
		return false
	}
	return rel == "." || (!strings.HasPrefix(rel, ".."+string(os.PathSeparator)) && rel != "..")
}

func DirExistsNonEmpty(path string) (bool, error) {
	entries, err := os.ReadDir(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("read directory %s: %w", path, err)
	}
	return len(entries) > 0, nil
}

func atomicWriteFile(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create directory %s: %w", dir, err)
	}

	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+"-*.tmp")
	if err != nil {
		return fmt.Errorf("create temporary file for %s: %w", path, err)
	}
	tmpName := tmp.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tmpName)
		}
	}()

	if err := tmp.Chmod(perm); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("chmod temporary file for %s: %w", path, err)
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write temporary file for %s: %w", path, err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("sync temporary file for %s: %w", path, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temporary file for %s: %w", path, err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("replace %s: %w", path, err)
	}
	cleanup = false
	return nil
}
