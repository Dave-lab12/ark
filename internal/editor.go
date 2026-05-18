package internal

import (
	"encoding/hex"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"runtime"
	"strings"
)

// ResolveEditorBinary looks up the editor binary by name, checking PATH
// first and then a small set of macOS Application bundle locations.
// Returns the resolved absolute path on success.
//
// The Application-bundle fallback is macOS-only and intentionally short:
// users with non-standard installs can put the binary on PATH or pass
// --editor with a different name. We don't try to be exhaustive.
func ResolveEditorBinary(name string) (string, error) {
	if name == "" {
		return "", fmt.Errorf("editor name is empty")
	}
	if path, err := exec.LookPath(name); err == nil {
		return path, nil
	}
	if runtime.GOOS == "darwin" {
		if path := lookupMacOSAppBinary(name); path != "" {
			return path, nil
		}
	}
	return "", fmt.Errorf("editor %q not found on PATH%s", name, macOSHint(name))
}

// lookupMacOSAppBinary checks well-known /Applications paths for the named
// editor's CLI helper. Returns the empty string if nothing matches.
func lookupMacOSAppBinary(name string) string {
	candidates := macOSAppBinaryCandidates(name)
	for _, candidate := range candidates {
		if info, err := stat(candidate); err == nil && !info.IsDir() {
			return candidate
		}
	}
	return ""
}

// macOSAppBinaryCandidates returns candidate absolute paths inside
// /Applications for the named editor. The list covers the VS Code family
// as installed by the official installers. Order matters: most common
// first.
func macOSAppBinaryCandidates(name string) []string {
	appName := macOSAppName(name)
	if appName == "" {
		return nil
	}
	return []string{
		filepath.Join("/Applications", appName+".app", "Contents", "Resources", "app", "bin", name),
	}
}

// macOSAppName maps a CLI binary name to its .app bundle name. Empty
// string means "no known mapping" — the function bails before trying any
// filesystem lookups.
func macOSAppName(binary string) string {
	switch binary {
	case "code":
		return "Visual Studio Code"
	case "code-insiders":
		return "Visual Studio Code - Insiders"
	case "cursor":
		return "Cursor"
	case "windsurf":
		return "Windsurf"
	case "codium":
		return "VSCodium"
	case "void":
		return "Void"
	}
	return ""
}

func macOSHint(name string) string {
	if runtime.GOOS != "darwin" {
		return ""
	}
	if macOSAppName(name) == "" {
		return ""
	}
	return ` (if you have it installed, run "Shell Command: Install '` + name + `' command in PATH" from the editor's command palette)`
}

type EditorMode int

const (
	// EditorModeAttach uses ark's existing flow: ark manages the
	// container directly, editor attaches via --remote
	// attached-container+<hex>. Reserved for editors known to handle
	// that URI scheme reliably.
	EditorModeAttach EditorMode = iota

	// EditorModeDevcontainerNative writes a .devcontainer/devcontainer.json
	// into the project and opens the editor at the project root. The
	// editor's own devcontainer flow drives container creation. Used
	// for editors where attach-by-URI is unreliable or unsupported.
	EditorModeDevcontainerNative
)

// EditorModeFor returns the dispatch mode for an editor binary name.
// Only editors verified to work in attach mode are listed; everything
// else uses native mode. Promote a new editor to attach mode only after
// hand-testing that --remote attached-container+<hex> actually attaches.
func EditorModeFor(name string) EditorMode {
	switch normalizeEditorName(name) {
	case "code", "code-insiders", "cursor":
		return EditorModeAttach
	default:
		return EditorModeDevcontainerNative
	}
}

// normalizeEditorName strips path components, extensions, and case
// so the dispatcher works whether the user passes "code", "Code.exe",
// or "/Applications/Visual Studio Code.app/Contents/Resources/app/bin/code".
func normalizeEditorName(name string) string {
	name = strings.ToLower(strings.TrimSpace(name))
	name = filepath.Base(name)
	return strings.TrimSuffix(name, ".exe")
}

// BuildRemoteAuthority returns the value for the editor's --remote flag.
// This is used only for editors verified to support VS Code's
// attached-container resolver, currently VS Code, code-insiders, and Cursor.
func BuildRemoteAuthority(containerName string) string {
	return "attached-container+" + hex.EncodeToString([]byte(containerName))
}

// resolveContainerFolder normalizes a user-provided folder. Absolute paths
// are returned as-is. Relative paths are joined with the workdir (so
// "--folder mindplex_backend" means "/work/mindplex_backend" when workdir
// is "/work"). Path traversal is not blocked here — the container is the
// trust boundary, not this validation.
func resolveContainerFolder(folder, workdir string) string {
	if folder == "" {
		return workdir
	}
	if strings.HasPrefix(folder, "/") {
		return folder
	}
	return workdir + "/" + folder
}

// resolveNativeWorkspaceFolder resolves --folder to the container path that
// editor-owned devcontainers should open, while keeping the host editor launch
// rooted at the project directory.
func resolveNativeWorkspaceFolder(folder, workdir string) (string, error) {
	workdir = path.Clean(strings.TrimSpace(workdir))
	if !path.IsAbs(workdir) {
		return "", fmt.Errorf("container workdir %q must be absolute", workdir)
	}
	if strings.TrimSpace(folder) == "" {
		return workdir, nil
	}

	candidate := strings.TrimSpace(folder)
	if path.IsAbs(candidate) {
		candidate = path.Clean(candidate)
	} else {
		candidate = path.Clean(path.Join(workdir, candidate))
	}
	if candidate != workdir && !strings.HasPrefix(candidate, strings.TrimRight(workdir, "/")+"/") {
		return "", fmt.Errorf("--folder %q is outside the container workdir %q; native-mode editors can only open paths inside the project", folder, workdir)
	}
	return candidate, nil
}

// stat is a thin wrapper kept indirected so tests can stub the filesystem
// lookup without touching the global os package.
var stat = osStat

func osStat(path string) (fs.FileInfo, error) {
	return os.Stat(path)
}

// launchEditor is the function used to start the editor process. It's a
// var so tests can stub it without spawning a real GUI app.
var launchEditor = launchEditorImpl

func launchEditorImpl(binary, remote, folder string) error {
	cmd := exec.Command(binary, "--remote", remote, folder)
	cmd.Stdin = nil
	cmd.Stdout = nil
	cmd.Stderr = nil
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("launch %s: %w", binary, err)
	}
	return cmd.Process.Release()
}

// launchEditorPath opens the editor at a given host path, with no remote
// authority. Used by devcontainer-native mode, where the editor finds
// the in-project devcontainer.json itself and drives the container
// connection.
var launchEditorPath = launchEditorPathImpl

func launchEditorPathImpl(binary, path string) error {
	cmd := exec.Command(binary, path)
	cmd.Stdin = nil
	cmd.Stdout = nil
	cmd.Stderr = nil
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("launch %s: %w", binary, err)
	}
	return cmd.Process.Release()
}
