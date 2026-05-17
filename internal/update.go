package internal

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

const (
	defaultUpdateRepository = "Dave-lab12/ark"
	maxUpdateBinarySize     = 128 << 20
)

type githubRelease struct {
	TagName string `json:"tag_name"`
}

func (a *App) updateCommand() *cobra.Command {
	var force bool
	cmd := &cobra.Command{
		Use:   "update",
		Short: "Update Ark to the latest released binary",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.Update(cmd.Context(), force)
		},
	}
	cmd.Flags().BoolVar(&force, "force", false, "reinstall even if the current version matches latest")
	return cmd
}

func (a *App) Update(ctx context.Context, force bool) error {
	target, err := resolveUpdateTarget(a.paths.ArkHome)
	if err != nil {
		return err
	}
	// Bail early if we can't write where the running binary lives. Avoids
	// downloading a release tarball just to fail at install time.
	if err := ensureUpdateTargetWritable(target); err != nil {
		return err
	}

	client := &http.Client{Timeout: 5 * time.Minute}
	release, err := latestGitHubRelease(ctx, client, defaultUpdateRepository)
	if err != nil {
		return err
	}
	version := strings.TrimPrefix(release.TagName, "v")
	if version == "" {
		return fmt.Errorf("latest release has empty tag")
	}
	if !force && ArkVersion != "dev" && ArkVersion == version {
		fmt.Fprintf(a.out, "Ark %s is already current\n", ArkVersion)
		return nil
	}

	assetName, err := updateAssetName(version, runtime.GOOS, runtime.GOARCH)
	if err != nil {
		return err
	}
	url := fmt.Sprintf("https://github.com/%s/releases/download/%s/%s", defaultUpdateRepository, release.TagName, assetName)
	fmt.Fprintf(a.out, "Downloading Ark %s for %s/%s\n", version, runtime.GOOS, runtime.GOARCH)
	binary, perm, err := downloadArkBinary(ctx, client, url)
	if err != nil {
		return err
	}

	if err := installArkBinary(target, binary, perm); err != nil {
		return err
	}
	fmt.Fprintf(a.out, "Installed Ark %s to %s\n", version, target)
	return nil
}

// resolveUpdateTarget returns the on-disk path of the running ark binary,
// following symlinks. If we can't determine it (e.g. /proc unavailable on
// the host), fall back to the ark-managed install path.
func resolveUpdateTarget(arkHome string) (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return filepath.Join(arkHome, "bin", "ark"), nil
	}
	resolved, err := filepath.EvalSymlinks(exe)
	if err != nil {
		// A dangling symlink is unusual but not fatal — use the raw path.
		return exe, nil
	}
	return resolved, nil
}

// ensureUpdateTargetWritable verifies we can replace the file at target.
// We test by trying to create a temp file in the parent directory, since
// rename-over-busy is what installArkBinary ultimately does. A permission
// error here almost always means the binary was installed by a package
// manager, so we surface a clearer message than the raw EACCES.
func ensureUpdateTargetWritable(target string) error {
	dir := filepath.Dir(target)
	probe, err := os.CreateTemp(dir, ".ark-write-probe-*")
	if err != nil {
		if errors.Is(err, fs.ErrPermission) {
			return notWritableError(target)
		}
		return fmt.Errorf("check %s for writability: %w", dir, err)
	}
	name := probe.Name()
	_ = probe.Close()
	_ = os.Remove(name)
	return nil
}

func notWritableError(target string) error {
	msg := fmt.Sprintf("ark update cannot write to %s: this binary appears to be installed by a package manager, which ark will not overwrite.", target)
	if hint := packageManagerHint(target); hint != "" {
		msg += "\n" + hint
	}
	return errors.New(msg)
}

// packageManagerHint is best-effort: if the install path looks like a
// recognizable package-manager prefix, suggest the matching upgrade command.
// Empty string means "no specific guidance" — the generic error still applies.
func packageManagerHint(target string) string {
	switch {
	case strings.HasPrefix(target, "/opt/homebrew/"),
		strings.HasPrefix(target, "/usr/local/Cellar/"),
		strings.Contains(target, "/Cellar/"):
		return "Try: brew upgrade ark"
	case strings.HasPrefix(target, "/opt/local/"):
		return "Try: sudo port upgrade ark"
	case strings.HasPrefix(target, "/usr/bin/"), strings.HasPrefix(target, "/usr/sbin/"):
		return "Try your distro's package manager (e.g. apt upgrade ark)."
	}
	return ""
}

func latestGitHubRelease(ctx context.Context, client *http.Client, repository string) (githubRelease, error) {
	url := fmt.Sprintf("https://api.github.com/repos/%s/releases/latest", repository)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return githubRelease{}, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "ark/"+ArkVersion)
	resp, err := client.Do(req)
	if err != nil {
		return githubRelease{}, fmt.Errorf("fetch latest release: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return githubRelease{}, fmt.Errorf("fetch latest release: %s", resp.Status)
	}
	var release githubRelease
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return githubRelease{}, fmt.Errorf("decode latest release: %w", err)
	}
	if strings.TrimSpace(release.TagName) == "" {
		return githubRelease{}, errors.New("latest release response did not include tag_name")
	}
	return release, nil
}

func updateAssetName(version, goos, goarch string) (string, error) {
	switch goos {
	case "darwin", "linux":
	default:
		return "", fmt.Errorf("ark update does not have a release asset for %s/%s", goos, goarch)
	}
	switch goarch {
	case "amd64", "arm64":
	default:
		return "", fmt.Errorf("ark update does not have a release asset for %s/%s", goos, goarch)
	}
	return fmt.Sprintf("ark_%s_%s_%s.tar.gz", version, goos, goarch), nil
}

func downloadArkBinary(ctx context.Context, client *http.Client, url string) ([]byte, os.FileMode, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("User-Agent", "ark/"+ArkVersion)
	resp, err := client.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("download release asset: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, 0, fmt.Errorf("download release asset: %s", resp.Status)
	}
	return extractArkBinary(resp.Body)
}

func extractArkBinary(r io.Reader) ([]byte, os.FileMode, error) {
	gz, err := gzip.NewReader(r)
	if err != nil {
		return nil, 0, fmt.Errorf("open release archive: %w", err)
	}
	defer gz.Close()

	tr := tar.NewReader(gz)
	for {
		header, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, 0, fmt.Errorf("read release archive: %w", err)
		}
		if header.Typeflag != tar.TypeReg || path.Base(header.Name) != "ark" {
			continue
		}
		data, err := io.ReadAll(io.LimitReader(tr, maxUpdateBinarySize+1))
		if err != nil {
			return nil, 0, fmt.Errorf("read ark binary from release archive: %w", err)
		}
		if len(data) > maxUpdateBinarySize {
			return nil, 0, fmt.Errorf("release binary is larger than %d bytes", maxUpdateBinarySize)
		}
		perm := header.FileInfo().Mode().Perm()
		if perm == 0 {
			perm = 0o755
		}
		return data, perm, nil
	}
	return nil, 0, errors.New("release archive did not contain an ark binary")
}

func installArkBinary(target string, binary []byte, perm os.FileMode) error {
	if len(binary) == 0 {
		return errors.New("refusing to install empty ark binary")
	}
	dir := filepath.Dir(target)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create install directory %s: %w", dir, err)
	}
	tmp, err := os.CreateTemp(dir, ".ark-update-*")
	if err != nil {
		return fmt.Errorf("create temporary update file: %w", err)
	}
	tmpName := tmp.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tmpName)
		}
	}()
	if _, err := tmp.Write(binary); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write temporary update file: %w", err)
	}
	if err := tmp.Chmod(perm | 0o111); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("chmod temporary update file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temporary update file: %w", err)
	}
	if err := os.Rename(tmpName, target); err != nil {
		return fmt.Errorf("install ark binary to %s: %w", target, err)
	}
	cleanup = false
	return nil
}
