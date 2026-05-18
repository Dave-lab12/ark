package editor

import (
	"encoding/hex"
	"io/fs"
	"os"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestBuildRemoteAuthority(t *testing.T) {
	containerName := "ark-test"
	got := BuildRemoteAuthority(containerName)
	const prefix = "attached-container+"
	if !strings.HasPrefix(got, prefix) {
		t.Fatalf("missing prefix: %q", got)
	}
	decoded, err := hex.DecodeString(strings.TrimPrefix(got, prefix))
	if err != nil {
		t.Fatalf("hex decode: %v", err)
	}
	if string(decoded) != containerName {
		t.Fatalf("decoded = %q, want %q", string(decoded), containerName)
	}
}

func TestEditorModeFor(t *testing.T) {
	cases := map[string]EditorMode{
		"code":          EditorModeAttach,
		"code-insiders": EditorModeAttach,
		"cursor":        EditorModeAttach,
		"zed":           EditorModeDevcontainerNative,
		"windsurf":      EditorModeDevcontainerNative,
		"antigravity":   EditorModeDevcontainerNative,
		"nvim":          EditorModeDevcontainerNative,
		"":              EditorModeDevcontainerNative,
		// Path normalization: full path resolves to base binary.
		"/Applications/Cursor.app/Contents/Resources/app/bin/cursor": EditorModeAttach,
		"Code.exe": EditorModeAttach,
		"CURSOR":   EditorModeAttach,
	}
	for name, want := range cases {
		if got := EditorModeFor(name); got != want {
			t.Errorf("EditorModeFor(%q) = %v, want %v", name, got, want)
		}
	}
}

func TestResolveEditorBinaryFindsOnPATH(t *testing.T) {
	got, err := ResolveEditorBinary("go")
	if err != nil {
		t.Fatalf("ResolveEditorBinary(go): %v", err)
	}
	if got == "" {
		t.Fatal("expected non-empty path")
	}
}

func TestResolveEditorBinaryNotFoundProducesUsefulError(t *testing.T) {
	_, err := ResolveEditorBinary("definitely-not-a-real-editor-binary-xyz")
	if err == nil {
		t.Fatal("expected error for missing binary")
	}
	if !strings.Contains(err.Error(), "definitely-not-a-real-editor-binary-xyz") {
		t.Fatalf("error did not mention binary name: %v", err)
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Fatalf("error did not mention 'not found': %v", err)
	}
}

func TestResolveEditorBinaryFallsBackToAppBundle(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("macOS-only fallback")
	}
	fakePath := "/Applications/Cursor.app/Contents/Resources/app/bin/cursor"
	prev := stat
	t.Cleanup(func() { stat = prev })
	stat = func(path string) (fs.FileInfo, error) {
		if path == fakePath {
			return fakeFileInfo{name: "cursor"}, nil
		}
		return nil, os.ErrNotExist
	}

	if _, err := os.Stat("/usr/local/bin/cursor"); err == nil {
		t.Skip("cursor is on PATH on this machine; can't exercise fallback")
	}
	got, err := ResolveEditorBinary("cursor")
	if err != nil {
		t.Fatalf("ResolveEditorBinary(cursor): %v", err)
	}
	if got != fakePath {
		t.Fatalf("got %q, want %q", got, fakePath)
	}
}

type fakeFileInfo struct{ name string }

func (f fakeFileInfo) Name() string       { return f.name }
func (f fakeFileInfo) Size() int64        { return 0 }
func (f fakeFileInfo) Mode() fs.FileMode  { return 0o755 }
func (f fakeFileInfo) ModTime() time.Time { return time.Time{} }
func (f fakeFileInfo) IsDir() bool        { return false }
func (f fakeFileInfo) Sys() any           { return nil }
