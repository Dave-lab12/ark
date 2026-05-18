package core

import (
	"errors"
	"fmt"
)

var (
	ErrNotFound    = errors.New("not found")
	ErrUnsupported = errors.New("unsupported")
)

type ExitError struct {
	Code int
}

func (e *ExitError) Error() string {
	return fmt.Sprintf("process exited with code %d", e.Code)
}

func ExitCode(err error) (int, bool) {
	var exitErr *ExitError
	if errors.As(err, &exitErr) {
		return exitErr.Code, true
	}
	return 0, false
}
