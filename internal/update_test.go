package internal

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
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
