package build

import (
	"archive/tar"
	"bytes"
	"crypto/sha256"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/google/go-containerregistry/pkg/v1"
)

func TestFindKernelLayerSkipsDebugLayer(t *testing.T) {
	manifest := &v1.Manifest{Layers: []v1.Descriptor{
		{Annotations: map[string]string{"org.unikraft.kernel.imagedbg": "/unikraft/bin/kernel.dbg"}},
		{Annotations: map[string]string{kernelImageAnnotation: "/unikraft/bin/kernel"}},
	}}

	layer, err := findKernelLayer(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if got := layer.Annotations[kernelImageAnnotation]; got != "/unikraft/bin/kernel" {
		t.Fatalf("selected layer %q, want production kernel layer", got)
	}
}

func TestKernelCacheIsPlatformSpecific(t *testing.T) {
	cache := kernelCachePath(t.TempDir(), "node")
	if filepath.Base(cache) != "qemu" || filepath.Base(filepath.Dir(cache)) != "node" {
		t.Fatalf("kernel cache path %q does not isolate runtime and platform", cache)
	}
}

func TestFindKernelLayerRejectsImagesWithoutKernel(t *testing.T) {
	_, err := findKernelLayer(&v1.Manifest{Layers: []v1.Descriptor{{
		Annotations: map[string]string{"org.unikraft.kernel.imagedbg": "/unikraft/bin/kernel.dbg"},
	}}})
	if err == nil {
		t.Fatal("expected an error when the manifest has no kernel layer")
	}
}

func TestExtractKernelLayerWritesOnlyAnnotatedKernel(t *testing.T) {
	want := []byte("ELF kernel fixture")
	var archive bytes.Buffer
	w := tar.NewWriter(&archive)
	if err := w.WriteHeader(&tar.Header{Name: "unikraft/bin/kernel", Mode: 0644, Size: int64(len(want)), Typeflag: tar.TypeReg}); err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write(want); err != nil {
		t.Fatal(err)
	}
	debug := []byte("debug symbols must not be selected")
	if err := w.WriteHeader(&tar.Header{Name: "unikraft/bin/kernel.dbg", Mode: 0644, Size: int64(len(debug)), Typeflag: tar.TypeReg}); err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write(debug); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}

	output := filepath.Join(t.TempDir(), "kernel")
	if err := extractKernelLayer(bytes.NewReader(archive.Bytes()), "/unikraft/bin/kernel", output, int64(archive.Len())); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("extracted %q, want %q", got, want)
	}
}

func TestExtractKernelLayerRejectsUnsafePath(t *testing.T) {
	err := extractKernelLayer(io.NopCloser(bytes.NewReader(nil)), "/../../etc/passwd", filepath.Join(t.TempDir(), "kernel"), 0)
	if err == nil {
		t.Fatal("expected an error for an unsafe kernel path")
	}
}

func TestDownloadPrebuiltKernelVerifiesAndUnpacksAsset(t *testing.T) {
	xz, err := exec.LookPath("xz")
	if err != nil {
		t.Skip("xz is not installed")
	}
	want := []byte("prebuilt kernel fixture")
	compress := exec.Command(xz, "-1", "-c")
	compress.Stdin = bytes.NewReader(want)
	compressed, err := compress.Output()
	if err != nil {
		t.Fatal(err)
	}
	checksum := sha256.Sum256(compressed)
	const digest = "sha256:fixture"
	assetName := "kern-kernel-node-x86_64.xz"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/" + assetName + ".image-digest":
			fmt.Fprintln(w, digest)
		case "/" + assetName + ".sha256":
			fmt.Fprintf(w, "%x  %s\n", checksum, assetName)
		case "/" + assetName:
			_, _ = w.Write(compressed)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	output := filepath.Join(t.TempDir(), "kernel")
	found, err := downloadPrebuiltKernelFrom(server.Client(), server.URL, "node", digest, output)
	if err != nil {
		t.Fatal(err)
	}
	if !found {
		t.Fatal("expected the matching release kernel asset to be used")
	}
	got, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("kernel = %q, want %q", got, want)
	}
}
