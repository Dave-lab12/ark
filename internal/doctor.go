package internal

import (
	"context"
	"fmt"
)

func (a *App) Doctor(ctx context.Context) error {
	fmt.Fprintln(a.out, "Ark doctor")

	docker, err := NewDockerRuntime()
	if err != nil {
		fmt.Fprintf(a.out, "docker: error (%v)\n", err)
	} else if err := docker.Available(ctx); err != nil {
		fmt.Fprintf(a.out, "docker: unavailable (%v)\n", err)
	} else {
		fmt.Fprintln(a.out, "docker: ok")
	}

	apple := NewAppleRuntime()
	if err := apple.Available(ctx); err != nil {
		fmt.Fprintln(a.out, "apple: unavailable")
	} else {
		fmt.Fprintln(a.out, "apple: CLI present, backend not implemented in this MVP")
	}

	fmt.Fprintf(a.out, "state: %s\n", a.paths.StateFile)
	fmt.Fprintf(a.out, "projects: %s\n", a.paths.ProjectRoot)
	return nil
}
