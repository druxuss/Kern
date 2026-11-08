
# kern
<img width="80%" alt="image" src="https://github.com/user-attachments/assets/9aaba24d-f8ad-4505-aed8-64921f6ad8ae" />

A CLI tool to build, run and distribute applications as unikernels using Unikraft.

kern eliminates the operating system from the application lifecycle. Instead of packaging your app inside a container with a full Linux OS, kern compiles it into a unikernel — a single binary that contains exactly what your app needs and nothing else.

## Why kern?

| | Docker | kern |
|---|---|---|
| Boot time | 800ms - 2s | 5 - 12ms |
| Image size | 150MB - 800MB | 20 - 110MB |
| Memory usage | 50 - 150MB | 8 - 40MB |
| Attack surface | ~400 syscalls | ~50 syscalls |

## Requirements

- x86_64 Ubuntu or Debian, or Ubuntu 22.04 in WSL2 on Windows
- A user account with `sudo` access and an internet connection
- KVM is optional; QEMU uses software emulation when hardware acceleration is unavailable

## Installation

Run this same command from a Bash terminal on Linux or from the Ubuntu terminal in WSL2:

```bash
curl -fsSL https://raw.githubusercontent.com/druxuss/Kern/fix/improve/install.sh | bash
```

On Windows, if Ubuntu 22.04 is not installed in WSL2 yet, run this once from an Administrator PowerShell:

```powershell
wsl --install -d Ubuntu-22.04
```

Restart if Windows asks, finish creating the Linux user, open Ubuntu, and run the same Bash command above. Kern and its Linux dependencies are installed inside WSL. The installer fetches a prebuilt Kern binary and configures QEMU, Buildah, Skopeo, and the other host tools; Go, Docker, and application language toolchains are not required on the host.

### Initialize a project

In an existing application directory containing a supported runtime manifest (`package.json`, `go.mod`, `Cargo.toml`, or `requirements.txt`), initialize the Kernfile and confirm the prompt:

```bash
cd path/to/my-app
kern init
```

Detects the runtime automatically (Node.js, Go, Rust, Python) and creates a `Kernfile`.

### Build the unikernel

```bash
kern build
```

Downloads the Unikraft kernel via skopeo, builds the rootfs with buildah, generates the initramfs, packages it as a `.uki` image, signs it with ed25519 and generates a SBOM. No Docker daemon or kraft required.

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

Shows image size, UKI sections, dependencies, runtime info and security status.

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

Shows status, memory (via QMP balloon device) and vCPUs — without needing shell access inside the unikernel.

### Verify image integrity

```bash
kern verify
```

Verifies the ed25519 signature of the built image.

### Debug

```bash
kern debug my-app
```

Shows QMP status, VMM logs and full process info for debugging crashes.

### Push and pull images

```bash
kern push ghcr.io/YOUR_USER/my-app:1.0.0 -u YOUR_USER -p YOUR_TOKEN
kern pull ghcr.io/YOUR_USER/my-app:1.0.0 -u YOUR_USER -p YOUR_TOKEN
```

### Manage secrets

```bash
kern secret set db_url "postgres://user:pass@host/db"
kern secret get db_url
```

Reference secrets in the Kernfile:

```toml
[env]
DATABASE_URL = "secret:db_url"
```

## Multi-service applications

kern supports multi-service applications with automatic networking between services.

```yaml
# kern-compose.yml
version: "1.0"

services:
  api:
    build: ./api
    ports:
      - "3000:3000"

  worker:
    build: ./worker
    ports:
      - "4000:4000"
    depends_on:
      - api
```

```bash
kern compose up -d       # start all services
kern compose ps          # list services
kern compose logs        # show logs
kern compose restart api # restart a service
kern compose down        # stop and remove all services
```

Services communicate with each other by name (`http://worker:4000`) via a virtual bridge network with automatic DHCP and DNS resolution.

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
    steps:
      - uses: actions/checkout@v4

      - name: Build unikernel
        uses: Kern-Unikernel/action@v1
        with:
          push: ghcr.io/${{ github.repository }}:${{ github.sha }}
          registry-user: ${{ github.actor }}
          registry-token: ${{ secrets.GITHUB_TOKEN }}
```

See [Kern-Unikernel/action](https://github.com/Kern-Unikernel/action) for full documentation.

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

| Runtime | Min memory | Boot time | Status |
|---|---|---|---|
| Go 1.22 | 128Mi | ~8ms | ✅ Stable |
| Node.js 21 | 512Mi | ~10ms | ✅ Stable |
| Rust (latest) | 128Mi | ~8ms | ✅ Stable |
| Python 3.12 | 384Mi | ~11ms | ✅ Stable |
| Bun 1.1 | 256Mi | ~10ms | 🚧 Coming soon |

## Security

Every `kern build` automatically:
- Downloads the Unikraft kernel from the official OCI registry
- Builds the rootfs with buildah (no Docker daemon)
- Packages the unikernel as a `.uki` (Unified Kernel Image)
- Signs the image with ed25519 — stored in `.unikernel/image.uki.sig`
- Generates a SBOM (SPDX-2.3) — stored in `.unikernel/image.sbom`
- Keys are stored in `~/.config/kern/keys/`

Verify any image before running:

```bash
kern verify
```

## Observability

kern provides real-time observability without shell access inside the unikernel:

- `kern metrics` — memory (via QMP balloon), vCPUs and status
- `kern logs` — VM serial logs in real-time
- `kern debug` — full process info, VMM logs and QMP status
- `kern bench` — response time, throughput and latency

## Build pipeline

```
kern build
  → skopeo       — downloads Unikraft kernel from unikraft.org OCI registry
  → buildah      — builds app rootfs from Dockerfile (no Docker daemon)
  → cpio         — generates initramfs
  → objcopy      — combines kernel + initramfs + cmdline into .uki binary
  → ed25519      — signs the image
  → SPDX-2.3     — generates SBOM
```

Kernels are cached in `~/.cache/kern/kernels/` after the first build.

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
- [x] kern build (kraft-free: skopeo + buildah + cpio)
- [x] kern run / kern run -d
- [x] kern run --port / --env
- [x] kern ps / stop / rm
- [x] kern logs / kern logs -f
- [x] kern metrics (memory via QMP, vCPUs, status)
- [x] kern debug
- [x] kern inspect (UKI sections, dependencies, security)
- [x] kern verify (ed25519 signature)
- [x] kern bench (response time, throughput, latency)
- [x] kern push / pull (ghcr.io)
- [x] kern secret set / get (AES-256-GCM)
- [x] kern compose (multi-service with bridge networking)
- [x] GitHub Action (Kern-Unikernel/action@v1)
- [x] Node.js 21 runtime
- [x] Go 1.22 runtime
- [x] Rust runtime
- [x] Python 3.12 runtime
- [ ] Bun 1.1 runtime
- [ ] macOS support (HVF)
- [ ] Java 17 runtime
