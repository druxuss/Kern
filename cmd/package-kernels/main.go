package main

import (
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/druxuss/Kern/internal/build"
)

func main() {
	if len(os.Args) < 3 {
		fmt.Fprintln(os.Stderr, "usage: package-kernels OUTPUT_DIR RUNTIME...")
		os.Exit(2)
	}
	if err := packageKernels(os.Args[1], os.Args[2:]); err != nil {
		fmt.Fprintln(os.Stderr, "package-kernels:", err)
		os.Exit(1)
	}
}

func packageKernels(outputDir string, runtimes []string) error {
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return err
	}
	for _, runtime := range runtimes {
		tempDir, err := os.MkdirTemp("", "kern-package-kernel-")
		if err != nil {
			return err
		}
		kernelPath, downloadErr := build.DownloadKernel(runtime, tempDir)
		if downloadErr != nil {
			os.RemoveAll(tempDir)
			return fmt.Errorf("fetching %s kernel: %w", runtime, downloadErr)
		}
		imageDigest, err := os.ReadFile(filepath.Join(tempDir, "image.digest"))
		if err != nil {
			os.RemoveAll(tempDir)
			return fmt.Errorf("reading %s image digest: %w", runtime, err)
		}
		kernel, err := os.Open(kernelPath)
		if err != nil {
			os.RemoveAll(tempDir)
			return err
		}
		var magic [4]byte
		_, readErr := io.ReadFull(kernel, magic[:])
		closeErr := kernel.Close()
		if readErr != nil {
			os.RemoveAll(tempDir)
			return fmt.Errorf("reading %s kernel header: %w", runtime, readErr)
		}
		if closeErr != nil {
			os.RemoveAll(tempDir)
			return closeErr
		}
		if string(magic[:]) != "\x7fELF" {
			os.RemoveAll(tempDir)
			return fmt.Errorf("%s kernel does not have an ELF header", runtime)
		}

		assetName := fmt.Sprintf("kern-kernel-%s-x86_64.xz", runtime)
		assetPath := filepath.Join(outputDir, assetName)
		asset, err := os.Create(assetPath)
		if err != nil {
			os.RemoveAll(tempDir)
			return err
		}
		compress := exec.Command("xz", "-T0", "-1", "-c", kernelPath)
		compress.Stdout = asset
		compress.Stderr = os.Stderr
		compressErr := compress.Run()
		closeErr = asset.Close()
		if compressErr != nil {
			os.RemoveAll(tempDir)
			return fmt.Errorf("compressing %s kernel: %w", runtime, compressErr)
		}
		if closeErr != nil {
			os.RemoveAll(tempDir)
			return closeErr
		}
		compressed, err := os.Open(assetPath)
		if err != nil {
			os.RemoveAll(tempDir)
			return err
		}
		hash := sha256.New()
		_, hashErr := io.Copy(hash, compressed)
		closeErr = compressed.Close()
		if hashErr != nil {
			os.RemoveAll(tempDir)
			return hashErr
		}
		if closeErr != nil {
			os.RemoveAll(tempDir)
			return closeErr
		}
		if err := os.WriteFile(assetPath+".sha256", []byte(fmt.Sprintf("%x  %s\n", hash.Sum(nil), assetName)), 0644); err != nil {
			os.RemoveAll(tempDir)
			return err
		}
		if err := os.WriteFile(assetPath+".image-digest", imageDigest, 0644); err != nil {
			os.RemoveAll(tempDir)
			return err
		}
		os.RemoveAll(tempDir)
		fmt.Printf("Packaged %s kernel: %s\n", runtime, assetPath)
	}
	return nil
}
