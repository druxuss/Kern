package build

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/druxuss/Kern/internal/kernfile"
	"github.com/druxuss/Kern/internal/packager"
)

func Build(config *kernfile.KernfileConfig, projectPath string) error {
	fmt.Println("→ Generating Dockerfile...")
	if err := generateDockerfile(config.App.Runtime, config.App.Entry, projectPath); err != nil {
		return fmt.Errorf("error generating Dockerfile: %w", err)
	}

	fmt.Println("→ Generating Kraftfile...")
	kraftfilePath, err := generateKraftfile(config, projectPath)
	if err != nil {
		return fmt.Errorf("error generating Kraftfile: %w", err)
	}

	fmt.Println("→ Compiling unikernel (kraft-free)...")
	unikraftDir := filepath.Join(projectPath, ".unikraft", "build")
	if err := os.MkdirAll(unikraftDir, 0755); err != nil {
		return fmt.Errorf("error creating build directory: %w", err)
	}

	kernelPath, err := downloadKernel(config.App.Runtime, unikraftDir)
	if err != nil {
		return fmt.Errorf("error downloading Unikraft kernel: %w", err)
	}
	fmt.Printf("✓ Kernel downloaded at %s\n", kernelPath)

	rootfsDir, err := buildRootfs(projectPath, config.App.Runtime)
	if err != nil {
		return fmt.Errorf("error building rootfs: %w", err)
	}

	initrdPath := filepath.Join(unikraftDir, "initramfs-x86_64.cpio")
	if err := generateInitramfs(rootfsDir, initrdPath); err != nil {
		return fmt.Errorf("error generating initramfs: %w", err)
	}
	fmt.Println("✓ Initramfs generated")
	os.RemoveAll(rootfsDir)

	ukiPath := filepath.Join(projectPath, ".unikernel", "image.uki")
	prevPath := filepath.Join(projectPath, ".unikernel", "image.uki.prev")
	unikernelDir := filepath.Dir(ukiPath)
	if err := os.MkdirAll(unikernelDir, 0755); err != nil {
		return fmt.Errorf("error creating image directory: %w", err)
	}
	if _, err := os.Stat(ukiPath); err == nil {
		if data, err := os.ReadFile(ukiPath); err == nil {
			os.WriteFile(prevPath, data, 0755)
		}
	}

	fmt.Println("→ Generating .uki image...")
	if err := generateUKI(config, projectPath); err != nil {
		return fmt.Errorf("error generating .uki: %w", err)
	}
	fmt.Println("✓ .uki image generated")

	fmt.Println("→ Signing image and generating SBOM...")
	configDir, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("error getting home directory: %w", err)
	}
	configDir = filepath.Join(configDir, ".config", "kern")

	_, privKey, err := packager.GenerateKeyPair(configDir)
	if err != nil {
		return fmt.Errorf("error generating key pair: %w", err)
	}

	imagePath := filepath.Join(unikernelDir, "image.uki")

	if _, err := os.Stat(imagePath); err == nil {
		if err := packager.SignImage(imagePath, privKey); err != nil {
			return fmt.Errorf("error signing image: %w", err)
		}
		fmt.Println("✓ Image signed")
	}

	sbom := packager.GenerateSBOM(config.App.Name, config.App.Version, config.App.Runtime,
		[]packager.Component{
			{Name: "unikraft", Version: "0.21.0"},
			{Name: config.App.Runtime, Version: "runtime"},
		})

	sbomPath := filepath.Join(unikernelDir, "image.sbom")
	if err := packager.SaveSBOM(sbom, sbomPath); err != nil {
		return fmt.Errorf("error saving SBOM: %w", err)
	}

	fmt.Println("✓ SBOM generated")
	fmt.Printf("✓ Build completed — Kraftfile generated at %s\n", kraftfilePath)
	return nil
}

func generateDockerfile(runtime, entry, projectPath string) error {
	dockerfilePath := filepath.Join(projectPath, "Dockerfile")
	generated := false

	var content string

	switch runtime {
	case "node":
		content = "FROM node:21-alpine AS build\n" +
			"WORKDIR /usr/src\n" +
			"COPY package*.json ./\n" +
			"RUN npm install --omit=dev --no-audit --no-fund && mkdir -p node_modules\n" +
			"COPY . /usr/src/\n\n" +
			"FROM scratch\n" +
			"COPY --from=build /etc/os-release /etc/os-release\n" +
			"COPY --from=build /usr/src/node_modules /usr/src/node_modules\n" +
			"COPY --from=build /usr/src/" + entry + " /usr/src/" + entry + "\n"
	case "python":
		content = "FROM docker.io/library/python:3.12-slim AS build\n" +
			"WORKDIR /src\n" +
			"COPY requirements.txt* ./\n" +
			"RUN if [ -f requirements.txt ]; then pip install -r requirements.txt; fi\n" +
			"COPY . .\n\n" +
			"FROM scratch\n" +
			"COPY --from=build /usr/local/lib/libpython3.12.so.1.0 /usr/local/lib/libpython3.12.so.1.0\n" +
			"COPY --from=build /usr/local/bin/python3.12 /usr/local/bin/python3.12\n" +
			"COPY --from=build /lib/x86_64-linux-gnu/libc.so.6 /lib/x86_64-linux-gnu/libc.so.6\n" +
			"COPY --from=build /lib/x86_64-linux-gnu/libz.so.1 /lib/x86_64-linux-gnu/libz.so.1\n" +
			"COPY --from=build /lib64/ld-linux-x86-64.so.2 /lib64/ld-linux-x86-64.so.2\n" +
			"COPY --from=build /usr/local/lib/python3.12/http /usr/local/lib/python3.12/http\n" +
			"COPY --from=build /usr/local/lib/python3.12/urllib /usr/local/lib/python3.12/urllib\n" +
			"COPY --from=build /usr/local/lib/python3.12/email /usr/local/lib/python3.12/email\n" +
			"COPY --from=build /usr/local/lib/python3.12/encodings /usr/local/lib/python3.12/encodings\n" +
			"COPY --from=build /usr/local/lib/python3.12/*.py /usr/local/lib/python3.12/\n" +
			"COPY --from=build /src /src\n"
	case "go":
		content = "FROM docker.io/library/golang:1.22-bookworm AS build\n" +
			"WORKDIR /src\n" +
			"COPY . .\n" +
			"RUN CGO_ENABLED=1 go build \\\n" +
			"      -buildmode=pie \\\n" +
			"      -ldflags \"-linkmode external -extldflags '-static-pie'\" \\\n" +
			"      -tags netgo \\\n" +
			"      -o /app .\n\n" +
			"FROM scratch\n" +
			"COPY --from=build /app /usr/src/app\n" +
			"COPY --from=build /lib/x86_64-linux-gnu/libc.so.6 /lib/x86_64-linux-gnu/\n" +
			"COPY --from=build /lib64/ld-linux-x86-64.so.2 /lib64/\n"
	case "bun":
		content = "FROM oven/bun:1.1.20-debian AS bun\n" +
			"RUN mv /usr/local/bin/bun /usr/bin/bun\n" +
			"WORKDIR /usr/src\n" +
			"COPY . .\n" +
			"RUN bun install --production\n\n" +
			"FROM scratch\n" +
			"COPY --from=bun /usr/bin/bun /usr/bin/bun\n" +
			"COPY --from=bun /lib/x86_64-linux-gnu/libc.so.6 /lib/x86_64-linux-gnu/libc.so.6\n" +
			"COPY --from=bun /lib/x86_64-linux-gnu/libm.so.6 /lib/x86_64-linux-gnu/libm.so.6\n" +
			"COPY --from=bun /lib64/ld-linux-x86-64.so.2 /lib64/ld-linux-x86-64.so.2\n" +
			"COPY --from=bun /usr/src /usr/src\n"
	case "rust":
		content = "FROM rust:latest AS build\n" +
			"WORKDIR /src\n" +
			"RUN rustup target add x86_64-unknown-linux-musl\n" +
			"RUN apt-get update && apt-get install -y musl-tools\n" +
			"COPY . .\n" +
			"RUN cargo build --release --target x86_64-unknown-linux-musl\n\n" +
			"FROM scratch\n" +
			"COPY --from=build /src/target/x86_64-unknown-linux-musl/release/app /usr/src/app\n"
	default:
		return fmt.Errorf("runtime not supported: %q", runtime)
	}

	// Refresh Dockerfiles generated by older Kern versions while preserving
	// user-authored Dockerfiles.
	if existing, err := os.ReadFile(dockerfilePath); err == nil {
		if string(existing) == content || (runtime == "node" && string(existing) == legacyNodeDockerfile(entry)) {
			// Existing template is current, or is a known generated template.
			generated = true
		} else {
			return nil
		}
	} else if !os.IsNotExist(err) {
		return err
	} else {
		generated = true
	}

	if err := os.WriteFile(dockerfilePath, []byte(content), 0644); err != nil {
		return err
	}
	if runtime == "node" && generated {
		return ignoreNodeModules(projectPath)
	}
	return nil
}

func ignoreNodeModules(projectPath string) error {
	path := filepath.Join(projectPath, ".dockerignore")
	data, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.TrimSpace(line) == "node_modules" {
			return nil
		}
	}
	if len(data) > 0 && data[len(data)-1] != '\n' {
		data = append(data, '\n')
	}
	data = append(data, []byte("node_modules\n")...)
	return os.WriteFile(path, data, 0644)
}

func legacyNodeDockerfile(entry string) string {
	return "FROM node:21-alpine AS build\n" +
		"WORKDIR /usr/src\n" +
		"COPY . /usr/src/\n" +
		"RUN npm install\n\n" +
		"FROM scratch\n" +
		"COPY --from=build /etc/os-release /etc/os-release\n" +
		"COPY --from=build /usr/src/node_modules /usr/src/node_modules\n" +
		"COPY --from=build /usr/src/" + entry + " /usr/src/" + entry + "\n"
}

func generateKraftfile(config *kernfile.KernfileConfig, projectPath string) (string, error) {
	profile, err := kernfile.LoadProfile(config.App.Runtime)
	if err != nil {
		return "", err
	}

	runtimeImage := map[string]string{
		"node":   "node:21",
		"python": "python:3.12",
		"go":     "base:latest",
		"bun":    "bun:1.1",
		"rust":   "base:latest",
	}[config.App.Runtime]

	cmdMap := map[string]string{
		"node":   `"/usr/bin/node", "/usr/src/` + config.App.Entry + `"`,
		"python": `"/usr/local/bin/python3.12", "/src/` + config.App.Entry + `"`,
		"go":     `"/usr/src/app"`,
		"bun":    `"/usr/bin/bun", "run", "/usr/src/` + config.App.Entry + `"`,
		"rust":   `"/usr/src/app"`,
	}
	appCmd := cmdMap[config.App.Runtime]

	content := fmt.Sprintf("spec: v0.6\nruntime: %s\nrootfs: ./Dockerfile\ncmd: [%s]\nresources:\n  memory: %s\n  cpu: %d\n",
		runtimeImage, appCmd, profile.Memory, profile.CPU)

	kraftfilePath := filepath.Join(projectPath, "Kraftfile")
	if err := os.WriteFile(kraftfilePath, []byte(content), 0644); err != nil {
		return "", err
	}
	return kraftfilePath, nil
}

func generateUKI(config *kernfile.KernfileConfig, projectPath string) error {
	initrdPath := filepath.Join(projectPath, ".unikraft", "build", "initramfs-x86_64.cpio")
	ukiPath := filepath.Join(projectPath, ".unikernel", "image.uki")

	if _, err := os.Stat(initrdPath); err != nil {
		return fmt.Errorf("initrd not found — run 'kern build' first")
	}

	kernelPath := filepath.Join(projectPath, ".unikraft", "build", "kernel")
	if _, err := os.Stat(kernelPath); err != nil {
		return fmt.Errorf("kernel not found — run 'kern build' first")
	}

	cmdlineMap := map[string]string{
		"node":   "/usr/bin/node /usr/src/" + config.App.Entry,
		"python": "/usr/local/bin/python3.12 /src/" + config.App.Entry,
		"go":     "/usr/src/app",
		"bun":    "/usr/bin/bun run /usr/src/" + config.App.Entry,
		"rust":   "/usr/src/app",
	}
	cmdline := fmt.Sprintf(`vfs.fstab=[ "initrd0:/:extract:::" ] -- %s`, cmdlineMap[config.App.Runtime])

	cmdlinePath := filepath.Join(projectPath, ".unikernel", "cmdline.txt")
	if err := os.WriteFile(cmdlinePath, []byte(cmdline), 0644); err != nil {
		return fmt.Errorf("error writing cmdline: %w", err)
	}
	defer os.Remove(cmdlinePath)

	cmd := exec.Command("objcopy",
		"--add-section", ".initrd="+initrdPath,
		"--set-section-flags", ".initrd=readonly,data",
		"--add-section", ".cmdline="+cmdlinePath,
		"--set-section-flags", ".cmdline=readonly,data",
		kernelPath,
		ukiPath,
	)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("error generating .uki: %w", err)
	}

	return nil
}
