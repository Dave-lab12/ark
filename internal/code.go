package internal

import (
	"context"
	"errors"
)

func (a *App) Code(ctx context.Context, name string) error {
	_, err := a.registry.Project(ctx, name)
	if err != nil {
		return err
	}
	return errors.New("ark code is not implemented in the Docker lifecycle MVP")
}
