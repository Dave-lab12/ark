package project

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/Dave-lab12/ark/internal/paths"
	"github.com/gofrs/flock"
)

type Registry struct {
	paths Paths
	lock  *flock.Flock
}

func NewRegistry(paths Paths) *Registry {
	return &Registry{
		paths: paths,
		lock:  flock.New(paths.LockFile),
	}
}

func (r *Registry) Load(ctx context.Context) (*State, error) {
	var state *State
	err := r.withLock(ctx, func() error {
		var err error
		state, err = r.loadUnlocked()
		return err
	})
	if err != nil {
		return nil, err
	}
	return state, nil
}

func (r *Registry) Update(ctx context.Context, fn func(*State) error) error {
	return r.withLock(ctx, func() error {
		state, err := r.loadUnlocked()
		if err != nil {
			return err
		}
		if err := fn(state); err != nil {
			return err
		}
		return r.writeUnlocked(state)
	})
}

func (r *Registry) EnsureDefault(ctx context.Context) error {
	return r.withLock(ctx, func() error {
		if _, err := os.Stat(r.paths.StateFile); err == nil {
			return nil
		} else if !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("stat state file %s: %w", r.paths.StateFile, err)
		}
		return r.writeUnlocked(&State{
			Version:  StateVersion,
			Projects: map[string]Project{},
		})
	})
}

func (r *Registry) withLock(ctx context.Context, fn func() error) error {
	if err := r.paths.Ensure(); err != nil {
		return err
	}
	locked, err := r.lock.TryLockContext(ctx, 100*time.Millisecond)
	if err != nil {
		return fmt.Errorf("lock registry: %w", err)
	}
	if !locked {
		return fmt.Errorf("lock registry: %w", ctx.Err())
	}
	defer func() {
		if err := r.lock.Unlock(); err != nil {
			slog.Warn("unlock registry", "error", err)
		}
	}()
	return fn()
}

func (r *Registry) loadUnlocked() (*State, error) {
	data, err := os.ReadFile(r.paths.StateFile)
	if errors.Is(err, os.ErrNotExist) {
		return &State{Version: StateVersion, Projects: map[string]Project{}}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read state file %s: %w", r.paths.StateFile, err)
	}
	var state State
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, fmt.Errorf("parse state file %s: %w", r.paths.StateFile, err)
	}
	if state.Version == 0 {
		state.Version = StateVersion
	}
	if state.Version != StateVersion {
		return nil, fmt.Errorf("unsupported state version %d", state.Version)
	}
	if state.Projects == nil {
		state.Projects = map[string]Project{}
	}
	return &state, nil
}

func (r *Registry) writeUnlocked(state *State) error {
	state.Version = StateVersion
	if state.Projects == nil {
		state.Projects = map[string]Project{}
	}
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("encode state: %w", err)
	}
	data = append(data, '\n')
	return paths.AtomicWriteFile(r.paths.StateFile, data, 0o600)
}

func (r *Registry) Project(ctx context.Context, name string) (Project, error) {
	state, err := r.Load(ctx)
	if err != nil {
		return Project{}, err
	}
	project, ok := state.Projects[name]
	if !ok {
		return Project{}, fmt.Errorf("project %q: %w", name, ErrNotFound)
	}
	return project, nil
}

func (r *Registry) ProjectForPath(ctx context.Context, path string) (Project, bool, error) {
	state, err := r.Load(ctx)
	if err != nil {
		return Project{}, false, err
	}
	var best Project
	bestLen := -1
	for _, project := range state.Projects {
		if IsInsidePath(path, project.Path) && len(project.Path) > bestLen {
			best = project
			bestLen = len(project.Path)
		}
	}
	if bestLen == -1 {
		return Project{}, false, nil
	}
	return best, true, nil
}
