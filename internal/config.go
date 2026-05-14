package internal

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
)

type Config struct {
	Image ImageConfig `toml:"image"`
}

type ImageConfig struct {
	Name             string            `toml:"name"`
	BuildContext     string            `toml:"build_context"`
	Containerfile    string            `toml:"containerfile"`
	Base             string            `toml:"base"`
	ExtraAptPackages []string          `toml:"extra_apt_packages"`
	SkipBuild        bool              `toml:"skip_build"`
	BuildArgs        map[string]string `toml:"build_args"`
}

func DefaultConfig() Config {
	return Config{
		Image: ImageConfig{
			Name:          DefaultImageTag,
			Containerfile: "Containerfile",
			Base:          DefaultBaseImage,
			BuildArgs:     map[string]string{},
		},
	}
}

func LoadConfig(paths Paths) (Config, error) {
	config := DefaultConfig()
	data, err := os.ReadFile(paths.ConfigFile)
	if errors.Is(err, os.ErrNotExist) {
		return config, nil
	}
	if err != nil {
		return Config{}, fmt.Errorf("read config file %s: %w", paths.ConfigFile, err)
	}
	if _, err := toml.Decode(string(data), &config); err != nil {
		return Config{}, fmt.Errorf("parse config file %s: %w", paths.ConfigFile, err)
	}
	if err := config.normalize(); err != nil {
		return Config{}, err
	}
	return config, nil
}

func (c *Config) normalize() error {
	if c.Image.Name == "" {
		c.Image.Name = DefaultImageTag
	}
	if c.Image.Containerfile == "" {
		c.Image.Containerfile = "Containerfile"
	}
	if c.Image.Base == "" {
		c.Image.Base = DefaultBaseImage
	}
	if c.Image.BuildArgs == nil {
		c.Image.BuildArgs = map[string]string{}
	}
	if strings.TrimSpace(c.Image.Name) == "" {
		return errors.New("config image.name cannot be empty")
	}
	if strings.TrimSpace(c.Image.Containerfile) == "" {
		return errors.New("config image.containerfile cannot be empty")
	}
	return nil
}

func (c Config) BuildImageSpec(out, errOut io.Writer) (BuildImageSpec, error) {
	contextDir, err := c.Image.ContextDir()
	if err != nil {
		return BuildImageSpec{}, err
	}
	buildArgs := map[string]string{}
	for key, value := range c.Image.BuildArgs {
		buildArgs[key] = value
	}
	buildArgs["ARK_BASE_IMAGE"] = c.Image.Base
	if len(c.Image.ExtraAptPackages) > 0 {
		buildArgs["ARK_EXTRA_APT_PACKAGES"] = strings.Join(c.Image.ExtraAptPackages, " ")
	}
	return BuildImageSpec{
		ContextDir: contextDir,
		Dockerfile: c.Image.Containerfile,
		Tag:        c.Image.Name,
		BuildArgs:  buildArgs,
		Out:        out,
		Err:        errOut,
	}, nil
}

func (c ImageConfig) ContextDir() (string, error) {
	if c.BuildContext == "" {
		return FindBaseImageContext()
	}
	path, err := expandPath(c.BuildContext)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(path)
	if err != nil {
		return "", fmt.Errorf("stat image.build_context %s: %w", path, err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("image.build_context is not a directory: %s", path)
	}
	return path, nil
}

func WriteDefaultConfig(paths Paths, force bool) error {
	if err := paths.EnsureConfigDir(); err != nil {
		return err
	}
	if !force {
		if _, err := os.Stat(paths.ConfigFile); err == nil {
			return fmt.Errorf("config file already exists: %s", paths.ConfigFile)
		} else if !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("stat config file %s: %w", paths.ConfigFile, err)
		}
	}
	return os.WriteFile(paths.ConfigFile, []byte(defaultConfigTOML), 0o600)
}

func expandPath(path string) (string, error) {
	if path == "" {
		return "", nil
	}
	if strings.HasPrefix(path, "~/") || path == "~" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("expand %s: %w", path, err)
		}
		if path == "~" {
			path = home
		} else {
			path = filepath.Join(home, strings.TrimPrefix(path, "~/"))
		}
	}
	return filepath.Abs(path)
}

const defaultConfigTOML = `# Ark config lives at ~/.config/ark/config.toml.
# Existing projects keep the image stored in state.json; this affects new projects.

[image]
# Image tag Ark builds and stores for new projects.
name = "ark-base:dev"

# Leave empty to use Ark's built-in images/base directory.
# Set this to a directory containing your own Containerfile for full control.
build_context = ""

# Containerfile path relative to build_context.
containerfile = "Containerfile"

# Parent image for Ark's built-in Containerfile.
base = "debian:bookworm-slim"

# Extra apt packages appended to the built-in image install step.
extra_apt_packages = []

# Set true to use image.name as an existing image and skip Ark's build step.
skip_build = false

[image.build_args]
# Additional Docker build args for custom Containerfiles.
`
