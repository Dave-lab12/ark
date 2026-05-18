package image

import (
	"github.com/Dave-lab12/ark/internal/config"
	"github.com/Dave-lab12/ark/internal/core"
	"github.com/Dave-lab12/ark/internal/paths"
	"github.com/Dave-lab12/ark/internal/runtime"
)

type Config = config.Config
type Paths = paths.Paths
type Runtime = runtime.Runtime

const StateVersion = core.StateVersion

func DefaultConfig() Config {
	return config.DefaultConfig()
}
