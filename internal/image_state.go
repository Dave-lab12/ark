package internal

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/gofrs/flock"
)

type ImageState struct {
	Version int       `json:"version"`
	Image   ImageInfo `json:"image"`
}

type ImageInfo struct {
	Tag         string    `json:"tag"`
	Fingerprint string    `json:"fingerprint"`
	BuiltAt     time.Time `json:"built_at"`
}

type ImageStore struct {
	paths Paths
	lock  *flock.Flock
}

func NewImageStore(paths Paths) *ImageStore {
	return &ImageStore{
		paths: paths,
		lock:  flock.New(paths.ImageStateFile + ".lock"),
	}
}

func (s *ImageStore) EnsureDefault(ctx context.Context) error {
	return s.withLock(ctx, func() error {
		if _, err := os.Stat(s.paths.ImageStateFile); err == nil {
			return nil
		} else if !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("stat image state file %s: %w", s.paths.ImageStateFile, err)
		}
		return s.writeUnlocked(&ImageState{
			Version: StateVersion,
			Image:   ImageInfo{},
		})
	})
}

func (s *ImageStore) Load(ctx context.Context) (*ImageState, error) {
	var state *ImageState
	err := s.withLock(ctx, func() error {
		var err error
		state, err = s.loadUnlocked()
		return err
	})
	if err != nil {
		return nil, err
	}
	return state, nil
}

func (s *ImageStore) Update(ctx context.Context, fn func(*ImageState) error) error {
	return s.withLock(ctx, func() error {
		state, err := s.loadUnlocked()
		if err != nil {
			return err
		}
		if err := fn(state); err != nil {
			return err
		}
		return s.writeUnlocked(state)
	})
}

func (s *ImageStore) withLock(ctx context.Context, fn func() error) error {
	if err := s.paths.EnsureControlPlane(); err != nil {
		return err
	}
	locked, err := s.lock.TryLockContext(ctx, 100*time.Millisecond)
	if err != nil {
		return fmt.Errorf("lock image state: %w", err)
	}
	if !locked {
		return fmt.Errorf("lock image state: %w", ctx.Err())
	}
	defer func() {
		if err := s.lock.Unlock(); err != nil {
			slog.Warn("unlock image state", "error", err)
		}
	}()
	return fn()
}

func (s *ImageStore) loadUnlocked() (*ImageState, error) {
	data, err := os.ReadFile(s.paths.ImageStateFile)
	if errors.Is(err, os.ErrNotExist) {
		return &ImageState{Version: StateVersion, Image: ImageInfo{}}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read image state file %s: %w", s.paths.ImageStateFile, err)
	}
	var state ImageState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, fmt.Errorf("parse image state file %s: %w", s.paths.ImageStateFile, err)
	}
	if state.Version == 0 {
		state.Version = StateVersion
	}
	if state.Version != StateVersion {
		return nil, fmt.Errorf("unsupported image state version %d", state.Version)
	}
	return &state, nil
}

func (s *ImageStore) writeUnlocked(state *ImageState) error {
	state.Version = StateVersion
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("encode image state: %w", err)
	}
	data = append(data, '\n')
	return atomicWriteFile(s.paths.ImageStateFile, data, 0o600)
}
