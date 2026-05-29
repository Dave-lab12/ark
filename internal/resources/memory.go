package resources

import (
	"fmt"
	"strings"

	"github.com/docker/go-units"
)

func NormalizeMemoryLimit(spec string) (string, error) {
	limit := strings.TrimSpace(spec)
	if limit == "" {
		return "", fmt.Errorf("memory limit cannot be empty")
	}
	bytes, err := units.RAMInBytes(limit)
	if err != nil {
		return "", fmt.Errorf("invalid memory limit %q: %w", spec, err)
	}
	if bytes <= 0 {
		return "", fmt.Errorf("memory limit must be greater than zero")
	}
	return limit, nil
}

func ParseMemoryLimitBytes(spec string) (int64, error) {
	limit, err := NormalizeMemoryLimit(spec)
	if err != nil {
		return 0, err
	}
	return units.RAMInBytes(limit)
}

func FormatBytes(bytes uint64) string {
	return units.BytesSize(float64(bytes))
}
