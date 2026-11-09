
# kern
<img width="80%" alt="image" src="https://github.com/user-attachments/assets/9aaba24d-f8ad-4505-aed8-64921f6ad8ae" />

A CLI tool to build, run and distribute applications as unikernels using Unikraft.

kern aims to package supported applications as single-purpose unikernels using Unikraft instead of a general-purpose container operating system. Runtime compatibility and the resulting image contents depend on the selected runtime and application.

Kern currently targets x86_64 Linux guests on QEMU. It is an early-stage project; performance and security depend on the application, runtime profile, host, and configuration. The repository does not yet contain reproducible comparative benchmarks, so no Docker performance figures are claimed here.

## Requirements

- x86_64 Linux (the installer has package-manager paths for Ubuntu/Debian, Fedora/RHEL-compatible, Arch Linux, and openSUSE; only Ubuntu 22.04 has been exercised end to end in this workflow)
- Windows with WSL2 and an Ubuntu distribution
- A user account with `sudo` access and an internet connection
- KVM is optional; QEMU uses software emulation when hardware acceleration is unavailable

## Installation

Run this same command from a Bash terminal on Linux or from an Ubuntu terminal in WSL2:

```bash
curl -fsSL https://raw.githubusercontent.com/druxuss/Kern/main/install.sh | bash
```

On Windows, if Ubuntu is not installed in WSL2 yet, run this once from an Administrator PowerShell:

```powershell
wsl --install -d Ubuntu
```

Restart if Windows asks, finish creating the Linux user, open Ubuntu, and run the same Bash command above. Kern and its Linux dependencies are installed inside WSL. The installer fetches a prebuilt Kern binary and configures QEMU, Buildah, and required host tools; Go, Docker, and application language toolchains are not required on the host. The installer needs sudo and internet access.

### Initialize a project

In an existing application directory containing a supported runtime manifest (`package.json`, `go.mod`, `Cargo.toml`, `requirements.txt`, or `bun.lockb`), initialize the Kernfile:

```bash
cd path/to/my-app
kern init
```

Detects the runtime automatically (Node.js, Go, Rust, Python, or Bun), asks you to confirm, and creates a `Kernfile`. For Rust, it reads the first Cargo binary target (or the package name when no `[[bin]]` target is declared) for the generated app name. Review the entry point, ports, and resource settings before building.

### Build the unikernel

```bash
kern build
```

Downloads and caches the runtime's QEMU kernel, builds the application root filesystem with Buildah, creates a CPIO initramfs, then writes a signed ELF artifact at `.unikernel/image.uki` with embedded metadata sections and an SPDX 2.3 SBOM. The `.uki` extension is Kern's artifact name; it is not a standard UEFI Unified Kernel Image. `kern run` boots the QEMU kernel and initramfs separately. No Docker daemon or Kraft CLI is used by the normal build path.

### Run locally

```bash
kern run          # foreground
kern run -d       # background (detached)
kern run -d --port 8080:8080   # override port
kern run -d --env NODE_ENV=dev # set environment variable
```

### Inspect the image

```bash
kern inspect
```

Shows artifact/kernel/initramfs sizes, embedded ELF sections, runtime resources, signature status, and the SBOM package list. It does not currently enumerate every application dependency.

### Benchmark

```bash
kern run -d
kern bench
```

Shows response time, requests/sec and avg latency.

### Manage running unikernels

```bash
kern ps              # list running unikernels
kern logs my-app     # show logs
kern logs -f my-app  # follow logs in real-time
kern stop my-app     # stop a unikernel
kern rm my-app       # remove a stopped unikernel
```

### Real-time metrics

```bash
kern metrics my-app       # show current metrics
kern metrics my-app -w    # watch metrics in real-time
```

Shows QEMU status, configured RAM, and vCPU information via QMP. Configured RAM is not a measurement of guest memory actually in use.

### Verify image integrity

```bash
kern verify
```

Verifies the local `.unikernel/image.uki` against the local public key in `~/.config/kern/keys/`. This does not establish trust in images received from another machine; registry push/pull does not currently transport or verify signing keys/signatures.

### Debug

```bash
kern debug my-app
```

Shows QMP status, the QEMU process information available to the current user, and paths to the serial and QEMU log files. Guest serial output is available only if the guest writes to the configured serial console.

### Push and pull images

```bash
kern push ghcr.io/YOUR_USER/my-app:1.0.0 -u YOUR_USER -p YOUR_TOKEN
kern pull ghcr.io/YOUR_USER/my-app:1.0.0 -u YOUR_USER -p YOUR_TOKEN
```

The registry stores the raw Kern artifact (`image.uki`) as a single OCI layer; its detached signature is not included. `kern pull` extracts that artifact to `.unikernel/image.uki`; it does not download the matching QEMU kernel/initramfs, so the pulled artifact cannot currently be launched with `kern run` on a clean project. Registry credentials passed with `-p` may be visible in process listings and shell history; use a short-lived token and avoid shared hosts.

### Manage secrets

```bash
kern secret set db_url --stdin
kern secret get db_url
```

Enter the value through standard input (for example, `printf '%s' "$DB_URL" | kern secret set db_url --stdin`) to avoid placing it directly in shell history. Secrets are encrypted locally with AES-256-GCM using a random key stored beside the ciphertext under `~/.config/kern/secrets/`. This protects files at rest from casual disclosure, not from root or another process running as the same user. Resolved environment values are passed to QEMU and can be visible to privileged host users.

Reference secrets in the Kernfile:

```toml
[env]
DATABASE_URL = "secret:db_url"
```

## Experimental multi-service command

`kern compose` exists in the CLI, but its multi-service lifecycle, networking, logging, and cleanup have not been validated end to end. It is not a supported deployment workflow; use individual `kern build` and `kern run` commands for the validated single-service workflow.

## GitHub Action

Build and push unikernels automatically in CI/CD:

```yaml
name: Build unikernel

on:
  push:
    branches: [main]

jobs:
  build:
    runs-on: ubuntu-latest
    permissions:
      contents: read
      packages: write
    steps:
      - uses: actions/checkout@v4

      - name: Build unikernel
        uses: druxuss/Kern/kern-action@main
        with:
          push: ghcr.io/${{ github.repository }}:${{ github.sha }}
          registry-user: ${{ github.actor }}
          registry-token: ${{ secrets.GITHUB_TOKEN }}
```

For GitHub Container Registry pushes, grant the workflow `packages: write` permission. The action should be pinned to a reviewed commit or release tag for reproducible CI; `@main` follows ongoing changes.

See [kern-action](https://github.com/druxuss/Kern/blob/main/kern-action/action.yml) for full documentation.

## The Kernfile

```toml
kernfile = "1.0"

[app]
name    = "my-api"
version = "1.0.0"
runtime = "node"      # node | go | rust | python
entry   = "index.js"

[network]
ports = [3000]

[resources]
memory = "512mb"
cpu    = 1

[env]
NODE_ENV = "production"
DATABASE_URL = "secret:db_url"
```

## Supported runtimes

| Runtime | Default memory | Build recipe |
|---|---|---|---|
| Go | 128 MiB | Implemented; verify app-specific builds |
| Node.js 21 | 512 MiB | Implemented; Node.js 21 is end of life |
| Rust | 64 MiB | Implemented; `kern init` uses the first Cargo binary target |
| Python 3.12 | 384 MiB | Implemented; verify native dependencies in your app |
| Bun | 512 MiB | Experimental; not covered by end-to-end validation |

These are configuration defaults, not measured minimums or boot-time guarantees. Node.js 21 is end of life and is listed because the currently published Unikraft kernel profile targets it; do not treat it as a maintained production runtime.

## Security

Every `kern build` automatically:
- Uses a cached QEMU kernel, or downloads it from the official Unikraft OCI registry when needed
- Builds the rootfs with buildah (no Docker daemon)
- Packages the unikernel as a Kern ELF artifact (`.unikernel/image.uki`; not a UEFI Unified Kernel Image)
- Signs the image with ed25519 — stored in `.unikernel/image.uki.sig`
- Generates a minimal SPDX-2.3 SBOM with the application and runtime/kernel components — stored in `.unikernel/image.sbom`; this is not a full file-level inventory
- Keys are stored in `~/.config/kern/keys/`

Verify a locally built artifact:

```bash
kern verify
```

## Observability

kern provides real-time observability without shell access inside the unikernel:

- `kern metrics` — QEMU status, configured RAM, and vCPU information
- `kern logs` — captured QEMU serial output (guest output depends on serial-console support)
- `kern debug` — QMP status, process details, and log file locations
- `kern bench` — HTTP response time, throughput, and average latency against a running app; it does not measure boot time or compare against Docker

## Build pipeline

```
kern build
  → Go OCI client — downloads Unikraft kernel from unikraft.org OCI registry when no cached release artifact is available
  → buildah      — builds app rootfs from Dockerfile (no Docker daemon)
  → cpio         — generates initramfs
  → objcopy      — adds initramfs + cmdline sections to a Kern ELF artifact
  → ed25519      — signs the image
  → SPDX-2.3     — generates SBOM
```

Kern downloads a compressed kernel artifact for the selected runtime and caches it in `~/.cache/kern/kernels/`. If that release artifact is unavailable, it fetches only the kernel layer from Unikraft and skips the separate debug-symbol layer.

## Project structure

```
kern/
├── cmd/                  ← CLI entry point
├── internal/
│   ├── build/            ← Build engine + .uki generation
│   ├── cli/              ← CLI commands
│   ├── compose/          ← Multi-service compose
│   ├── instances/        ← Instance state management
│   ├── kernfile/         ← Kernfile parser
│   ├── network/          ← Bridge/TAP networking for compose
│   ├── packager/         ← Image signing and SBOM
│   ├── qmp/              ← QEMU Machine Protocol client
│   ├── registry/         ← OCI registry client (ghcr.io)
│   ├── runner/           ← Direct QEMU runner
│   └── secrets/          ← Secret store (AES-256-GCM)
└── profiles/             ← Runtime profiles (TOML)
```

## Status

- [x] kern init
- [x] kern build (QEMU kernel + Buildah + cpio; no Docker daemon or Kraft CLI)
- [x] kern run / kern run -d
- [x] kern run --port / --env
- [x] kern ps / stop / rm
- [x] kern logs / kern logs -f
- [x] kern metrics (configured RAM, vCPUs, QMP status)
- [x] kern debug
- [x] kern inspect (ELF sections, SBOM package list, signature status)
- [x] kern verify (ed25519 signature)
- [x] kern bench (response time, throughput, latency)
- [x] kern push / pull (OCI artifact exchange; pulled artifact is not directly runnable)
- [x] kern secret set / get (local AES-256-GCM store)
- [ ] kern compose (experimental; not end-to-end validated)
- [x] GitHub Action (druxuss/Kern/kern-action@main)
- [x] Node.js 21 build recipe (runtime is end of life)
- [x] Go build recipe
- [x] Rust build recipe
- [x] Python 3.12 build recipe
- [ ] Bun build recipe (experimental)
- [ ] macOS support (HVF)
- [ ] Java 17 runtime
