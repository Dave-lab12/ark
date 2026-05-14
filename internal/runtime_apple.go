package internal

import (
	"context"
	"fmt"
	"os/exec"
)

type AppleRuntime struct{}

func NewAppleRuntime() *AppleRuntime {
	return &AppleRuntime{}
}

func (r *AppleRuntime) Name() string {
	return RuntimeApple
}

func (r *AppleRuntime) Available(ctx context.Context) error {
	if _, err := exec.LookPath("container"); err != nil {
		return fmt.Errorf("Apple container CLI is not available: %w", err)
	}
	return nil
}

func (r *AppleRuntime) ImageExists(ctx context.Context, tag string) (bool, error) {
	return false, fmt.Errorf("Apple runtime image inspect: %w", ErrUnsupported)
}

func (r *AppleRuntime) BuildImage(ctx context.Context, spec BuildImageSpec) error {
	return fmt.Errorf("Apple runtime image build: %w", ErrUnsupported)
}

func (r *AppleRuntime) Create(ctx context.Context, spec CreateSpec) (string, error) {
	return "", fmt.Errorf("Apple runtime create: %w", ErrUnsupported)
}

func (r *AppleRuntime) Start(ctx context.Context, containerName string) error {
	return fmt.Errorf("Apple runtime start: %w", ErrUnsupported)
}

func (r *AppleRuntime) Stop(ctx context.Context, containerName string, timeoutSeconds int) error {
	return fmt.Errorf("Apple runtime stop: %w", ErrUnsupported)
}

func (r *AppleRuntime) Remove(ctx context.Context, containerName string, force bool) error {
	return fmt.Errorf("Apple runtime remove: %w", ErrUnsupported)
}

func (r *AppleRuntime) Exec(ctx context.Context, containerName string, spec ExecSpec) error {
	return fmt.Errorf("Apple runtime exec: %w", ErrUnsupported)
}

func (r *AppleRuntime) Inspect(ctx context.Context, containerName string) (*Container, error) {
	return nil, fmt.Errorf("Apple runtime inspect: %w", ErrUnsupported)
}

func (r *AppleRuntime) List(ctx context.Context) ([]Container, error) {
	return nil, fmt.Errorf("Apple runtime list: %w", ErrUnsupported)
}

func (r *AppleRuntime) CreateVolume(ctx context.Context, name string) error {
	return fmt.Errorf("Apple runtime create volume: %w", ErrUnsupported)
}

func (r *AppleRuntime) RemoveVolume(ctx context.Context, name string) error {
	return fmt.Errorf("Apple runtime remove volume: %w", ErrUnsupported)
}
