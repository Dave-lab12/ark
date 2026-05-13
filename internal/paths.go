package internal

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/adrg/xdg"
)

type Paths struct {
	DataDir     string
	StateFile   string
	LockFile    string
	ProjectRoot string
}

func DefaultPaths() (Paths, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return Paths{}, fmt.Errorf("find home directory: %w", err)
	}

	dataDir := filepath.Join(dataHome(home), "ark")
	stateFile := filepath.Join(dataDir, "state.json")
	return Paths{
		DataDir:     dataDir,
		StateFile:   stateFile,
		LockFile:    stateFile + ".lock",
		ProjectRoot: filepath.Join(home, "ark"),
	}, nil
}

func dataHome(home string) string {
	if env := os.Getenv("XDG_DATA_HOME"); env != "" {
		return env
	}
	if strings.Contains(xdg.DataHome, ".local/share") {
		return xdg.DataHome
	}
	return filepath.Join(home, ".local", "share")
}

func (p Paths) Ensure() error {
	if err := os.MkdirAll(p.DataDir, 0o700); err != nil {
		return fmt.Errorf("create data directory %s: %w", p.DataDir, err)
	}
	if err := os.MkdirAll(p.ProjectRoot, 0o755); err != nil {
		return fmt.Errorf("create project root %s: %w", p.ProjectRoot, err)
	}
	return nil
}

func (p Paths) ProjectPath(name string) (string, error) {
	if err := ValidateProjectName(name); err != nil {
		return "", err
	}
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
