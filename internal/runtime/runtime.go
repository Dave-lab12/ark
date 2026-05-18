package runtime

import (
	"context"
	"fmt"

	"github.com/Dave-lab12/ark/internal/core"
)

type Runtime interface {
	Name() string
	Available(ctx context.Context) error
	ImageExists(ctx context.Context, tag string) (bool, error)
	BuildImage(ctx context.Context, spec BuildImageSpec) error

	Create(ctx context.Context, spec CreateSpec) (string, error)
	Start(ctx context.Context, containerName string) error
	Stop(ctx context.Context, containerName string, timeoutSeconds int) error
	Remove(ctx context.Context, containerName string, force bool) error

	Exec(ctx context.Context, containerName string, spec ExecSpec) error
	Inspect(ctx context.Context, containerName string) (*Container, error)
	List(ctx context.Context) ([]Container, error)

	CreateVolume(ctx context.Context, name string) error
	RemoveVolume(ctx context.Context, name string) error
}

func ResolveRuntime(ctx context.Context, requested string) (Runtime, string, error) {
	if requested == "" {
		requested = RuntimeAuto
	}
	switch requested {
	case core.RuntimeDocker:
		rt, err := NewDockerRuntime()
		return rt, core.RuntimeDocker, err
	case core.RuntimeApple:
		return NewAppleRuntime(), core.RuntimeApple, nil
	case core.RuntimeAuto:
		return resolveAutoRuntime(ctx)
	default:
		return nil, "", fmt.Errorf("unknown runtime %q", requested)
	}
}

func RuntimeByName(name string) (Runtime, error) {
	switch name {
	case core.RuntimeDocker:
		return NewDockerRuntime()
	case core.RuntimeApple:
		return NewAppleRuntime(), nil
	default:
		return nil, fmt.Errorf("unknown stored runtime %q", name)
	}
}

func resolveAutoRuntime(ctx context.Context) (Runtime, string, error) {
	// The Apple backend is intentionally a stub in this MVP. Keep auto on Docker
	// until the Apple runtime is implemented so `ark init` remains usable.
	rt, err := NewDockerRuntime()
	if err != nil {
		return nil, "", err
	}
	if err := rt.Available(ctx); err != nil {
		return nil, "", fmt.Errorf("auto runtime could not find Docker: %w", err)
	}
	return rt, core.RuntimeDocker, nil
}
