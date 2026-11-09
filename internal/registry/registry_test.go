package registry

import (
	"archive/tar"
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestExtractUKIWritesRawArtifact(t *testing.T) {
	var archive bytes.Buffer
	writer := tar.NewWriter(&archive)
	artifact := []byte("kern-elf-image")
	if err := writer.WriteHeader(&tar.Header{Name: "image.uki", Mode: 0600, Size: int64(len(artifact)), Typeflag: tar.TypeReg}); err != nil {
		t.Fatal(err)
	}
	if _, err := writer.Write(artifact); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}

	destination := filepath.Join(t.TempDir(), "image.uki")
	if err := extractUKI(&archive, destination); err != nil {
		t.Fatal(err)
	}
	actual, err := os.ReadFile(destination)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(actual, artifact) {
		t.Fatalf("artifact contents = %q, want raw bytes %q", actual, artifact)
	}
}

func TestExtractUKIIgnoresUnsafeEntryNames(t *testing.T) {
	var archive bytes.Buffer
	writer := tar.NewWriter(&archive)
	unsafe := []byte("wrong-file")
	if err := writer.WriteHeader(&tar.Header{Name: "../../image.uki", Mode: 0600, Size: int64(len(unsafe)), Typeflag: tar.TypeReg}); err != nil {
		t.Fatal(err)
	}
	if _, err := writer.Write(unsafe); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := extractUKI(&archive, filepath.Join(t.TempDir(), "image.uki")); err == nil {
		t.Fatal("expected missing artifact error for unsafe archive path")
	}
}

func TestPushImageInvalidReference(t *testing.T) {
	err := PushImage("/tmp/test.uki", "invalid!!reference", Credentials{})
	if err == nil {
		t.Error("should return error for invalid reference")
	}
}

func TestPullImageInvalidReference(t *testing.T) {
	err := PullImage("invalid!!reference", "/tmp/test", Credentials{})
	if err == nil {
		t.Error("should return error for invalid reference")
	}
}

func TestPullImageNonExistent(t *testing.T) {
	err := PullImage("ghcr.io/nonexistent/nonexistent:latest", "/tmp/test", Credentials{})
	if err == nil {
		t.Error("should return error for nonexistent image")
	}
}
