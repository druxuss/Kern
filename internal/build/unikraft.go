package build

import (
	"archive/tar"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/remote"
)

var runtimeOSMap = map[string]string{
	"node":   "unikraft.org/node:21",
	"python": "unikraft.org/python:3.12",
	"go":     "unikraft.org/base:latest",
	"rust":   "unikraft.org/base:latest",
	"bun":    "unikraft.org/bun:1.1",
}

const kernelImageAnnotation = "org.unikraft.kernel.image"
const kernelReleaseURL = "https://github.com/druxuss/Kern/releases/download/kern-nightly"

func DownloadKernel(runtime, destDir string) (string, error) {
	return downloadKernel(runtime, destDir)
}

func downloadKernel(runtime, destDir string) (string, error) {
	image, ok := runtimeOSMap[runtime]
	if !ok {
		return "", fmt.Errorf("runtime not supported: %q", runtime)
	}
	ref, err := name.ParseReference(image)
	if err != nil {
		return "", fmt.Errorf("invalid Unikraft image %q: %w", image, err)
	}
	remoteImage, err := remote.Image(ref, remote.WithPlatform(v1.Platform{OS: "fc", Architecture: "x86_64"}))
	if err != nil {
		return "", fmt.Errorf("reading Unikraft image manifest for %s: %w", runtime, err)
	}
	manifest, err := remoteImage.Manifest()
	if err != nil {
		return "", fmt.Errorf("reading Unikraft image layers for %s: %w", runtime, err)
	}
	imageDigest, err := remoteImage.Digest()
	if err != nil {
		return "", fmt.Errorf("reading Unikraft image digest for %s: %w", runtime, err)
	}

	cacheDir, err := os.UserCacheDir()
	if err != nil {
		cacheDir = os.TempDir()
	}
	kernelCacheDir := filepath.Join(cacheDir, "kern", "kernels", runtime)
	cachedKernel := filepath.Join(kernelCacheDir, "kernel")
	cachedDigest := filepath.Join(kernelCacheDir, "image.digest")
	destPath := filepath.Join(destDir, "kernel")
	if err := os.MkdirAll(destDir, 0755); err != nil {
		return "", err
	}

	if _, err := os.Stat(cachedKernel); err == nil {
		cacheDigest, digestErr := os.ReadFile(cachedDigest)
		// Adopt a legacy cache once; older Kern releases did not store its source digest.
		if (digestErr == nil && strings.TrimSpace(string(cacheDigest)) == imageDigest.String()) || os.IsNotExist(digestErr) {
			if err := copyFile(cachedKernel, destPath); err != nil {
				return "", err
			}
			if os.IsNotExist(digestErr) {
				_ = os.WriteFile(cachedDigest, []byte(imageDigest.String()+"\n"), 0644)
			}
			if err := writeImageDigest(destDir, imageDigest.String()); err != nil {
				return "", err
			}
			fmt.Printf("→ Using cached kernel for %s\n", runtime)
			return destPath, nil
		}
	}

	kernelLayer, err := findKernelLayer(manifest)
	if err != nil {
		return "", fmt.Errorf("finding kernel layer for %s: %w", runtime, err)
	}

	available, err := downloadPrebuiltKernel(runtime, imageDigest.String(), destPath)
	if err != nil {
		return "", fmt.Errorf("downloading the prebuilt kernel asset: %w", err)
	}
	if available {
		cacheKernel(destPath, kernelCacheDir, cachedKernel, cachedDigest, imageDigest.String())
		if err := writeImageDigest(destDir, imageDigest.String()); err != nil {
			return "", err
		}
		return destPath, nil
	}

	layer, err := remoteImage.LayerByDigest(kernelLayer.Digest)
	if err != nil {
		return "", fmt.Errorf("opening kernel layer for %s: %w", runtime, err)
	}
	content, err := layer.Uncompressed()
	if err != nil {
		return "", fmt.Errorf("downloading kernel layer for %s: %w", runtime, err)
	}
	defer content.Close()

	fmt.Printf("→ Downloading %s kernel layer (%.1f MiB; debug symbols skipped)...\n", runtime, float64(kernelLayer.Size)/(1024*1024))
	if err := extractKernelLayer(content, kernelLayer.Annotations[kernelImageAnnotation], destPath, kernelLayer.Size); err != nil {
		return "", fmt.Errorf("extracting kernel layer for %s: %w", runtime, err)
	}
	if err := writeImageDigest(destDir, imageDigest.String()); err != nil {
		return "", err
	}
	cacheKernel(destPath, kernelCacheDir, cachedKernel, cachedDigest, imageDigest.String())
	return destPath, nil
}

func downloadPrebuiltKernel(runtime, imageDigest, outputPath string) (bool, error) {
	client := &http.Client{Timeout: time.Hour}
	return downloadPrebuiltKernelFrom(client, kernelReleaseURL, runtime, imageDigest, outputPath)
}

func downloadPrebuiltKernelFrom(client *http.Client, releaseURL, runtime, imageDigest, outputPath string) (bool, error) {
	asset := "kern-kernel-" + runtime + "-x86_64.xz"
	prefix := strings.TrimRight(releaseURL, "/") + "/" + asset
	sourceDigest, found, err := readKernelAsset(client, prefix+".image-digest")
	if err != nil || !found || strings.TrimSpace(string(sourceDigest)) != imageDigest {
		return false, err
	}
	checksum, found, err := readKernelAsset(client, prefix+".sha256")
	if err != nil || !found {
		return false, err
	}
	fields := strings.Fields(string(checksum))
	if len(fields) == 0 || len(fields[0]) != sha256.Size*2 {
		return false, fmt.Errorf("invalid prebuilt kernel checksum")
	}
	expected, err := hex.DecodeString(fields[0])
	if err != nil {
		return false, fmt.Errorf("invalid prebuilt kernel checksum: %w", err)
	}

	response, err := client.Get(prefix)
	if err != nil {
		return false, err
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusNotFound {
		return false, nil
	}
	if response.StatusCode != http.StatusOK {
		return false, fmt.Errorf("kernel asset returned HTTP %s", response.Status)
	}
	archive, err := os.CreateTemp(filepath.Dir(outputPath), ".kern-kernel-*.xz")
	if err != nil {
		return false, err
	}
	archivePath := archive.Name()
	defer os.Remove(archivePath)
	hash := sha256.New()
	progress := &kernelLayerProgress{reader: io.TeeReader(response.Body, hash), total: response.ContentLength, next: 5 * 1024 * 1024}
	_, copyErr := io.Copy(archive, progress)
	closeErr := archive.Close()
	if copyErr != nil {
		return false, copyErr
	}
	if closeErr != nil {
		return false, closeErr
	}
	if !bytes.Equal(hash.Sum(nil), expected) {
		return false, fmt.Errorf("prebuilt kernel checksum mismatch")
	}
	fmt.Println()

	output, err := os.CreateTemp(filepath.Dir(outputPath), ".kern-kernel-*")
	if err != nil {
		return false, err
	}
	tempPath := output.Name()
	defer func() {
		output.Close()
		os.Remove(tempPath)
	}()
	decompress := exec.Command("xz", "-dc", archivePath)
	decompress.Stdout = output
	var stderr bytes.Buffer
	decompress.Stderr = &stderr
	if err := decompress.Run(); err != nil {
		return false, fmt.Errorf("decompressing prebuilt kernel: %w: %s", err, stderr.String())
	}
	if err := output.Chmod(0755); err != nil {
		return false, err
	}
	if err := output.Close(); err != nil {
		return false, err
	}
	if err := os.Rename(tempPath, outputPath); err != nil {
		return false, err
	}
	fmt.Println("✓ Prebuilt kernel downloaded")
	return true, nil
}

func readKernelAsset(client *http.Client, url string) ([]byte, bool, error) {
	response, err := client.Get(url)
	if err != nil {
		return nil, false, err
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusNotFound {
		return nil, false, nil
	}
	if response.StatusCode != http.StatusOK {
		return nil, false, fmt.Errorf("kernel metadata returned HTTP %s", response.Status)
	}
	content, err := io.ReadAll(io.LimitReader(response.Body, 64*1024))
	return content, true, err
}

func writeImageDigest(destDir, digest string) error {
	return os.WriteFile(filepath.Join(destDir, "image.digest"), []byte(digest+"\n"), 0644)
}

func cacheKernel(source, cacheDir, cachedKernel, cachedDigest, imageDigest string) {
	if err := os.MkdirAll(cacheDir, 0755); err != nil {
		return
	}
	if err := copyFile(source, cachedKernel); err != nil {
		return
	}
	if err := os.WriteFile(cachedDigest, []byte(imageDigest+"\n"), 0644); err != nil {
		return
	}
	fmt.Printf("✓ Kernel cached at %s\n", cacheDir)
}

func findKernelLayer(manifest *v1.Manifest) (v1.Descriptor, error) {
	for _, layer := range manifest.Layers {
		if layer.Annotations[kernelImageAnnotation] != "" {
			return layer, nil
		}
	}
	return v1.Descriptor{}, fmt.Errorf("image has no layer annotated with %q", kernelImageAnnotation)
}

func extractKernelLayer(content io.Reader, imagePath, outputPath string, totalSize int64) error {
	if imagePath == "" {
		return fmt.Errorf("kernel layer has no image path annotation")
	}
	for _, segment := range strings.Split(strings.ReplaceAll(imagePath, "\\", "/"), "/") {
		if segment == ".." {
			return fmt.Errorf("unsafe kernel image path %q", imagePath)
		}
	}
	target := strings.TrimPrefix(path.Clean(imagePath), "/")
	if target == "." || target == ".." || strings.HasPrefix(target, "../") {
		return fmt.Errorf("unsafe kernel image path %q", imagePath)
	}
	output, err := os.CreateTemp(filepath.Dir(outputPath), ".kern-kernel-*")
	if err != nil {
		return err
	}
	tempPath := output.Name()
	keepOutput := false
	defer func() {
		_ = output.Close()
		if !keepOutput {
			_ = os.Remove(tempPath)
		}
	}()

	progress := &kernelLayerProgress{reader: content, total: totalSize, next: 5 * 1024 * 1024}
	archive := tar.NewReader(progress)
	found := false
	for {
		header, err := archive.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		member := strings.TrimPrefix(path.Clean(header.Name), "./")
		if member != target {
			continue
		}
		if found {
			return fmt.Errorf("kernel path %q occurs more than once", imagePath)
		}
		if header.Typeflag != tar.TypeReg && header.Typeflag != tar.TypeRegA {
			return fmt.Errorf("kernel path %q is not a regular file", imagePath)
		}
		if _, err := io.CopyN(output, archive, header.Size); err != nil {
			return err
		}
		found = true
	}
	fmt.Printf("✓ Kernel layer downloaded (%.1f MiB)\n", float64(progress.read)/(1024*1024))
	if !found {
		return fmt.Errorf("kernel file %q not found in its OCI layer", imagePath)
	}
	if err := output.Chmod(0755); err != nil {
		return err
	}
	if err := output.Close(); err != nil {
		return err
	}
	if err := os.Rename(tempPath, outputPath); err != nil {
		return err
	}
	keepOutput = true
	return nil
}

type kernelLayerProgress struct {
	reader io.Reader
	total  int64
	read   int64
	next   int64
}

func (p *kernelLayerProgress) Read(buffer []byte) (int, error) {
	n, err := p.reader.Read(buffer)
	p.read += int64(n)
	if p.read >= p.next {
		if p.total > 0 {
			fmt.Printf("\r  Downloaded %.1f/%.1f MiB", float64(p.read)/(1024*1024), float64(p.total)/(1024*1024))
		} else {
			fmt.Printf("\r  Downloaded %.1f MiB", float64(p.read)/(1024*1024))
		}
		p.next = ((p.read / (5 * 1024 * 1024)) + 1) * (5 * 1024 * 1024)
	}
	return n, err
}

func buildRootfs(projectPath, runtime string) (string, error) {
	fmt.Println("→ Building rootfs with buildah...")

	imageName := "kern-rootfs-" + runtime

	// buildah build
	buildCmd := exec.Command("buildah", "build",
		"--layers",
		"-t", imageName,
		"-f", filepath.Join(projectPath, "Dockerfile"),
		projectPath,
	)
	buildCmd.Stdout = os.Stdout
	buildCmd.Stderr = os.Stderr
	if err := buildCmd.Run(); err != nil {
		return "", fmt.Errorf("Buildah failed to build the rootfs (Kern does not require Docker): %w", err)
	}

	// Exportar imagen a tar OCI
	tarPath := filepath.Join(projectPath, ".unikraft", "rootfs.tar")
	pushCmd := exec.Command("buildah", "push",
		"localhost/"+imageName,
		"oci-archive:"+tarPath,
	)
	pushCmd.Stdout = os.Stdout
	pushCmd.Stderr = os.Stderr
	if err := pushCmd.Run(); err != nil {
		return "", fmt.Errorf("error exporting rootfs: %w", err)
	}

	// Extraer la capa del OCI tar
	rootfsDir := filepath.Join(projectPath, ".unikraft", "rootfs")
	if err := extractOCILayer(tarPath, rootfsDir); err != nil {
		return "", fmt.Errorf("error extracting rootfs: %w", err)
	}

	// Limpiar
	os.Remove(tarPath)
	exec.Command("buildah", "rmi", "localhost/"+imageName).Run()

	return rootfsDir, nil
}

func extractOCILayer(tarPath, rootfsDir string) error {
	if err := os.RemoveAll(rootfsDir); err != nil {
		return err
	}
	if err := os.MkdirAll(rootfsDir, 0755); err != nil {
		return err
	}

	// Leer el index.json del tar OCI
	indexCmd := exec.Command("tar", "xOf", tarPath, "index.json")
	indexData, err := indexCmd.Output()
	if err != nil {
		return fmt.Errorf("error reading OCI index: %w", err)
	}

	var index struct {
		Manifests []struct {
			Digest string `json:"digest"`
		} `json:"manifests"`
	}
	if err := json.Unmarshal(indexData, &index); err != nil {
		return fmt.Errorf("error parsing OCI index: %w", err)
	}

	if len(index.Manifests) == 0 {
		return fmt.Errorf("no manifests found in OCI image")
	}

	manifestDigest := strings.TrimPrefix(index.Manifests[0].Digest, "sha256:")
	manifestPath := "blobs/sha256/" + manifestDigest

	manifestCmd := exec.Command("tar", "xOf", tarPath, manifestPath)
	manifestData, err := manifestCmd.Output()
	if err != nil {
		return fmt.Errorf("error reading manifest: %w", err)
	}

	var manifest struct {
		Layers []struct {
			Digest string `json:"digest"`
		} `json:"layers"`
	}
	if err := json.Unmarshal(manifestData, &manifest); err != nil {
		return fmt.Errorf("error parsing manifest: %w", err)
	}

	if len(manifest.Layers) == 0 {
		return fmt.Errorf("no layers found in OCI image")
	}

	// OCI layers are deltas; apply every layer in manifest order to reconstruct
	// the final root filesystem rather than extracting only the base layer.
	for i, layerDesc := range manifest.Layers {
		layerDigest := strings.TrimPrefix(layerDesc.Digest, "sha256:")
		if decoded, err := hex.DecodeString(layerDigest); err != nil || len(decoded) != sha256.Size {
			return fmt.Errorf("invalid digest for OCI layer %d", i)
		}
		layerPath := "blobs/sha256/" + layerDigest

		extractCmd := exec.Command("tar", "xOf", tarPath, layerPath)
		untarCmd := exec.Command("tar", "-xz", "-C", rootfsDir, "--no-same-owner", "--no-same-permissions")
		layer, err := extractCmd.StdoutPipe()
		if err != nil {
			return fmt.Errorf("error opening OCI layer %d: %w", i, err)
		}
		untarCmd.Stdin = layer
		untarCmd.Stderr = os.Stderr
		if err := untarCmd.Start(); err != nil {
			return fmt.Errorf("error starting extraction of OCI layer %d: %w", i, err)
		}
		if err := extractCmd.Run(); err != nil {
			_ = untarCmd.Process.Kill()
			_ = untarCmd.Wait()
			return fmt.Errorf("error reading OCI layer %d: %w", i, err)
		}
		if err := untarCmd.Wait(); err != nil {
			return fmt.Errorf("error extracting OCI layer %d: %w", i, err)
		}
	}

	return nil
}

func generateInitramfs(rootfsDir, outputPath string) error {
	fmt.Println("→ Generating initramfs CPIO...")

	output, err := os.Create(outputPath)
	if err != nil {
		return fmt.Errorf("error creating initramfs: %w", err)
	}
	defer output.Close()

	findCmd := exec.Command("find", ".")
	findCmd.Dir = rootfsDir
	cpioCmd := exec.Command("cpio", "-o", "-H", "newc")
	cpioCmd.Dir = rootfsDir
	files, err := findCmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("error listing rootfs: %w", err)
	}
	cpioCmd.Stdin = files
	cpioCmd.Stdout = output
	cpioCmd.Stderr = os.Stderr
	if err := cpioCmd.Start(); err != nil {
		return fmt.Errorf("error starting cpio: %w", err)
	}
	if err := findCmd.Run(); err != nil {
		_ = cpioCmd.Process.Kill()
		_ = cpioCmd.Wait()
		return fmt.Errorf("error listing rootfs: %w", err)
	}
	if err := cpioCmd.Wait(); err != nil {
		return fmt.Errorf("error generating initramfs: %w", err)
	}

	return nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0755)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	return os.Chmod(dst, 0755)
}
