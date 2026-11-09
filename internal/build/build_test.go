package build

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestGenerateDockerfileRefreshesLegacyNodeTemplate(t *testing.T) {
	project := t.TempDir()
	if err := os.WriteFile(filepath.Join(project, "Dockerfile"), []byte(legacyNodeDockerfile("index.js")), 0644); err != nil {
		t.Fatal(err)
	}
	if err := generateDockerfile("node", "index.js", project); err != nil {
		t.Fatal(err)
	}

	dockerfile, err := os.ReadFile(filepath.Join(project, "Dockerfile"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"COPY package*.json ./",
		"npm install --omit=dev --no-audit --no-fund && mkdir -p node_modules",
	} {
		if !strings.Contains(string(dockerfile), want) {
			t.Errorf("generated Dockerfile missing %q", want)
		}
	}

	ignore, err := os.ReadFile(filepath.Join(project, ".dockerignore"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(ignore), "node_modules") {
		t.Fatal("generated .dockerignore must exclude host node_modules")
	}
}

func TestGenerateDockerfilePreservesCustomNodeDockerfile(t *testing.T) {
	project := t.TempDir()
	want := []byte("FROM custom-node-image\n")
	if err := os.WriteFile(filepath.Join(project, "Dockerfile"), want, 0644); err != nil {
		t.Fatal(err)
	}
	if err := generateDockerfile("node", "index.js", project); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(project, "Dockerfile"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(want) {
		t.Fatalf("custom Dockerfile changed: got %q, want %q", got, want)
	}
}

func TestGenerateInitramfsReadsFilesFromRootfsDirectory(t *testing.T) {
	if _, err := exec.LookPath("cpio"); err != nil {
		t.Skip("cpio is not installed")
	}
	if _, err := exec.LookPath("find"); err != nil {
		t.Skip("find is not installed")
	}

	rootfs := t.TempDir()
	if err := os.MkdirAll(filepath.Join(rootfs, "etc"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(rootfs, "etc", "os-release"), []byte("ID=test\n"), 0644); err != nil {
		t.Fatal(err)
	}
	initramfs := filepath.Join(t.TempDir(), "initramfs.cpio")
	if err := generateInitramfs(rootfs, initramfs); err != nil {
		t.Fatal(err)
	}

	archive, err := os.Open(initramfs)
	if err != nil {
		t.Fatal(err)
	}
	defer archive.Close()
	listCmd := exec.Command("cpio", "-it")
	listCmd.Stdin = archive
	listing, err := listCmd.Output()
	if err != nil {
		t.Fatalf("listing generated initramfs: %v", err)
	}
	if !strings.Contains(string(listing), "etc/os-release") {
		t.Fatalf("generated initramfs does not contain etc/os-release: %s", listing)
	}
}

func TestExtractOCIArchiveAppliesAllLayers(t *testing.T) {
	if _, err := exec.LookPath("tar"); err != nil {
		t.Skip("tar is not installed")
	}

	makeLayer := func(name, contents string) []byte {
		t.Helper()
		var data bytes.Buffer
		gz := gzip.NewWriter(&data)
		layer := tar.NewWriter(gz)
		if err := layer.WriteHeader(&tar.Header{Name: name, Mode: 0644, Size: int64(len(contents))}); err != nil {
			t.Fatal(err)
		}
		if _, err := layer.Write([]byte(contents)); err != nil {
			t.Fatal(err)
		}
		if err := layer.Close(); err != nil {
			t.Fatal(err)
		}
		if err := gz.Close(); err != nil {
			t.Fatal(err)
		}
		return data.Bytes()
	}
	layers := [][]byte{
		makeLayer("etc/os-release", "ID=test\n"),
		makeLayer("usr/src/index.js", "console.log('ok')\n"),
	}
	layerDigests := make([]string, len(layers))
	for i, layer := range layers {
		sum := sha256.Sum256(layer)
		layerDigests[i] = hex.EncodeToString(sum[:])
	}

	type descriptor struct {
		MediaType string `json:"mediaType"`
		Digest    string `json:"digest"`
		Size      int    `json:"size"`
	}
	manifest := struct {
		SchemaVersion int          `json:"schemaVersion"`
		MediaType     string       `json:"mediaType"`
		Layers        []descriptor `json:"layers"`
	}{SchemaVersion: 2, MediaType: "application/vnd.oci.image.manifest.v1+json"}
	for i, layer := range layers {
		manifest.Layers = append(manifest.Layers, descriptor{
			MediaType: "application/vnd.oci.image.layer.v1.tar+gzip",
			Digest:    "sha256:" + layerDigests[i],
			Size:      len(layer),
		})
	}
	manifestData, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	manifestSum := sha256.Sum256(manifestData)
	manifestDigest := hex.EncodeToString(manifestSum[:])
	indexData, err := json.Marshal(struct {
		SchemaVersion int          `json:"schemaVersion"`
		Manifests     []descriptor `json:"manifests"`
	}{SchemaVersion: 2, Manifests: []descriptor{{
		MediaType: "application/vnd.oci.image.manifest.v1+json",
		Digest:    "sha256:" + manifestDigest,
		Size:      len(manifestData),
	}}})
	if err != nil {
		t.Fatal(err)
	}

	archivePath := filepath.Join(t.TempDir(), "rootfs.oci.tar")
	archiveFile, err := os.Create(archivePath)
	if err != nil {
		t.Fatal(err)
	}
	archive := tar.NewWriter(archiveFile)
	writeEntry := func(name string, data []byte) {
		t.Helper()
		if err := archive.WriteHeader(&tar.Header{Name: name, Mode: 0644, Size: int64(len(data))}); err != nil {
			t.Fatal(err)
		}
		if _, err := archive.Write(data); err != nil {
			t.Fatal(err)
		}
	}
	writeEntry("index.json", indexData)
	writeEntry("blobs/sha256/"+manifestDigest, manifestData)
	for i, layer := range layers {
		writeEntry("blobs/sha256/"+layerDigests[i], layer)
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
	if err := archiveFile.Close(); err != nil {
		t.Fatal(err)
	}

	rootfs := filepath.Join(t.TempDir(), "rootfs")
	if err := extractOCILayer(archivePath, rootfs); err != nil {
		t.Fatal(err)
	}
	for path, want := range map[string]string{
		"etc/os-release":   "ID=test\n",
		"usr/src/index.js": "console.log('ok')\n",
	} {
		got, err := os.ReadFile(filepath.Join(rootfs, filepath.FromSlash(path)))
		if err != nil {
			t.Fatalf("read extracted %s: %v", path, err)
		}
		if string(got) != want {
			t.Errorf("%s = %q, want %q", path, got, want)
		}
	}
}
