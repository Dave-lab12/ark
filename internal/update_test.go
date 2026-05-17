package internal

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestUpdateAssetName(t *testing.T) {
	got, err := updateAssetName("0.0.1", "darwin", "arm64")
	if err != nil {
		t.Fatal(err)
	}
	if got != "ark_0.0.1_darwin_arm64.tar.gz" {
		t.Fatalf("asset name = %q", got)
	}
}

func TestExtractArkBinary(t *testing.T) {
	var archive bytes.Buffer
	gz := gzip.NewWriter(&archive)
	tw := tar.NewWriter(gz)
	body := []byte("binary")
	if err := tw.WriteHeader(&tar.Header{
		Name: "ark",
		Mode: 0o755,
		Size: int64(len(body)),
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write(body); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}

	got, perm, err := extractArkBinary(bytes.NewReader(archive.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, body) {
		t.Fatalf("binary = %q, want %q", got, body)
	}
	if perm != 0o755 {
		t.Fatalf("perm = %v, want 0755", perm)
	}
}

func TestEnsureUpdateTargetWritableRefusesReadOnlyDir(t *testing.T) {
	// Root bypasses unix permission bits, so this test only makes sense for
	// unprivileged users.
	if os.Geteuid() == 0 {
		t.Skip("running as root; permission bits are bypassed")
	}
	dir := t.TempDir()
	target := filepath.Join(dir, "ark")
	if err := os.WriteFile(target, []byte("stub"), 0o755); err != nil {
		t.Fatalf("seed target: %v", err)
	}
	if err := os.Chmod(dir, 0o555); err != nil {
		t.Fatalf("chmod readonly: %v", err)
	}
	// Restore writability so t.TempDir's cleanup can remove the dir.
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })

	err := ensureUpdateTargetWritable(target)
	if err == nil {
		t.Fatal("expected refusal for read-only directory, got nil")
	}
	if !strings.Contains(err.Error(), "package manager") {
		t.Fatalf("error did not mention package manager: %v", err)
	}
}

func TestEnsureUpdateTargetWritableAcceptsWritableDir(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "ark")
	if err := os.WriteFile(target, []byte("stub"), 0o755); err != nil {
		t.Fatalf("seed target: %v", err)
	}
	if err := ensureUpdateTargetWritable(target); err != nil {
		t.Fatalf("expected writable dir to be accepted, got %v", err)
	}
}

func TestPackageManagerHint(t *testing.T) {
	cases := map[string]string{
		"/opt/homebrew/bin/ark":               "brew upgrade ark",
		"/usr/local/Cellar/ark/0.1.0/bin/ark": "brew upgrade ark",
		"/opt/local/bin/ark":                  "sudo port upgrade ark",
		"/usr/bin/ark":                        "apt upgrade ark",
		"/home/user/.ark/bin/ark":             "",
	}
	for path, want := range cases {
		got := packageManagerHint(path)
		if want == "" {
			if got != "" {
				t.Errorf("packageManagerHint(%q) = %q, want empty", path, got)
			}
			continue
		}
		if !strings.Contains(got, want) {
			t.Errorf("packageManagerHint(%q) = %q, want substring %q", path, got, want)
		}
	}
}
