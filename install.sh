#!/usr/bin/env bash
set -Eeuo pipefail

# One-command installer for a clean Ubuntu/Debian x86_64 host.
# Run as the target user, not with sudo; sudo is used only for system packages
# and files under /usr/local.

readonly GO_VERSION="1.27.3"
readonly GO_MIN_VERSION="1.26.3"
readonly GO_SHA256="65efbc80fdc1fa2cd2631cad5b481777a68e8725f34e89f50fc6ac714e379b40"
readonly REPOSITORY="https://github.com/druxuss/Kern.git"
readonly REPOSITORY_REF="${KERN_BRANCH:-fix/improve}"
readonly INSTALL_DIR="/usr/local/bin"

fail() {
    printf 'kern installer: ERROR: %s\n' "$*" >&2
    exit 1
}

if [[ "${EUID}" -eq 0 ]]; then
    fail "run this command as your normal user; the installer will call sudo when needed"
fi
if ! command -v sudo >/dev/null 2>&1; then
    fail "sudo is required to install system packages"
fi
if ! command -v apt-get >/dev/null 2>&1 || [[ ! -r /etc/os-release ]]; then
    fail "supported hosts are Ubuntu/Debian x86_64"
fi
# shellcheck disable=SC1091
source /etc/os-release
if [[ "${ID:-}" != "ubuntu" && "${ID:-}" != "debian" ]]; then
    fail "supported hosts are Ubuntu/Debian; detected ${PRETTY_NAME:-unknown}"
fi
if [[ "$(uname -m)" != "x86_64" ]]; then
    fail "the current build and QEMU pipeline targets x86_64; detected $(uname -m)"
fi

sudo -v
printf '\n==> Installing host dependencies\n'
sudo apt-get update
sudo apt-get install -y software-properties-common
if [[ "${ID}" == "ubuntu" ]]; then
    sudo add-apt-repository -y universe
fi
sudo apt-get update
sudo apt-get install -y \
    ca-certificates curl git build-essential binutils cpio tar findutils \
    qemu-system-x86 cpu-checker skopeo buildah uidmap fuse-overlayfs crun slirp4netns \
    iproute2 bridge-utils uml-utilities dnsmasq-base

printf '\n==> Preparing rootless Buildah\n'
ensure_subid_range() {
    local file="$1" option="$2" uid
    uid="$(id -u "$USER")"
    if [[ ! -e "$file" ]]; then
        sudo install -m 0644 /dev/null "$file"
    fi
    if awk -F: -v user="$USER" -v uid="$uid" '$1 == user || $1 == uid { found = 1 } END { exit !found }' "$file"; then
        return
    fi
    local first last
    first="$(awk -F: 'BEGIN { max = 99999 } $2 ~ /^[0-9]+$/ && $3 ~ /^[0-9]+$/ { end = $2 + $3 - 1; if (end > max) max = end } END { print (int(max / 65536) + 1) * 65536 }' "$file")"
    last=$((first + 65535))
    (( last <= 4294967294 )) || fail "no free subordinate ID range remains in $file"
    sudo usermod "$option" "${first}-${last}" "$USER"
}
ensure_subid_range /etc/subuid --add-subuids
ensure_subid_range /etc/subgid --add-subgids
if [[ -c /dev/kvm ]] && getent group kvm >/dev/null && ! id -nG "$USER" | tr ' ' '\n' | grep -qx kvm; then
    sudo usermod -aG kvm "$USER"
    printf 'Added %s to group kvm; a new login session is needed for KVM acceleration.\n' "$USER"
fi
mkdir -p "$HOME/.config/containers"
storage_config="$HOME/.config/containers/storage.conf"
created_storage_config=0
if [[ ! -e "$storage_config" ]]; then
    cat >"$storage_config" <<'EOF'
[storage]
driver = "overlay"

[storage.options]
mount_program = "/usr/bin/fuse-overlayfs"
EOF
    created_storage_config=1
fi

printf '\n==> Checking Go toolchain\n'
tmp_dir=""
work_dir=""
smoke_dir=""
cleanup() {
    [[ -z "$tmp_dir" ]] || rm -rf "$tmp_dir"
    [[ -z "$work_dir" ]] || rm -rf "$work_dir"
    [[ -z "$smoke_dir" ]] || rm -rf "$smoke_dir"
}
trap cleanup EXIT
current_go=""
go_bin=""
if [[ "${ID}" == "ubuntu" ]] && apt-cache show golang-1.26-go >/dev/null 2>&1; then
    # Ubuntu 22.04 publishes Go 1.26 in Universe/security updates. Using the
    # signed distro package avoids downloading the much larger upstream tarball.
    sudo apt-get install -y golang-1.26-go
    go_bin="/usr/lib/go-1.26/bin"
else
    if [[ -x /usr/local/go/bin/go ]]; then
        current_go="$(/usr/local/go/bin/go version | awk '{print $3}' | sed 's/^go//')"
    fi
    if [[ -z "$current_go" || "$(printf '%s\n' "$GO_VERSION" "$current_go" | sort -V | head -n1)" != "$GO_VERSION" ]]; then
        tmp_dir="$(mktemp -d)"
        archive="go${GO_VERSION}.linux-amd64.tar.gz"
        if ! curl --fail --location --show-error --retry 2 --retry-all-errors \
            --connect-timeout 20 --max-time 100 --progress-bar \
            "https://go.dev/dl/${archive}" -o "${tmp_dir}/${archive}"; then
            fail "could not download Go ${GO_VERSION} from go.dev after retries (about 5 minutes); check outbound HTTPS/DNS and rerun the installer"
        fi
        printf '%s  %s\n' "$GO_SHA256" "${tmp_dir}/${archive}" | sha256sum --check --status \
            || fail "Go archive checksum verification failed"

        # Go's official installer requires replacing, not overlaying, an old tree.
        if [[ -d /usr/local/go ]]; then
            backup="/usr/local/go.kern-backup.$(date +%Y%m%d%H%M%S)"
            sudo mv /usr/local/go "$backup"
            printf 'Previous Go installation moved to %s\n' "$backup"
        fi
        sudo tar -C /usr/local -xzf "${tmp_dir}/${archive}"
    fi
    go_bin="/usr/local/go/bin"
fi
current_go="$("${go_bin}/go" version | awk '{print $3}' | sed 's/^go//')"
if [[ "$(printf '%s\n' "$GO_MIN_VERSION" "$current_go" | sort -V | head -n1)" != "$GO_MIN_VERSION" ]]; then
    fail "Go ${current_go} is too old; need at least ${GO_MIN_VERSION}"
fi

# /usr/local/bin is already on Ubuntu's default PATH, including the shell that
# launched this installer, so kern build can invoke Go immediately.
sudo ln -sfn "${go_bin}/go" /usr/local/bin/go
sudo ln -sfn "${go_bin}/gofmt" /usr/local/bin/gofmt
export PATH="${go_bin}:${PATH}"
printf 'Using %s\n' "$(go version)"

printf '\n==> Building and installing kern (%s)\n' "$REPOSITORY_REF"
work_dir="$(mktemp -d)"
git clone --depth 1 --branch "$REPOSITORY_REF" "$REPOSITORY" "$work_dir/source"
(
    cd "$work_dir/source"
    # The pipe separator makes Go try the next proxy after network errors,
    # while normal go.sum / checksum-database verification remains enabled.
    GOPROXY="${KERN_GOPROXY:-https://proxy.golang.org|https://goproxy.io|direct}" \
        go build -trimpath -o "$work_dir/kern" ./cmd/
)
sudo install -m 0755 "$work_dir/kern" "$INSTALL_DIR/kern"
sudo install -d -m 0755 "$INSTALL_DIR/profiles"
sudo install -m 0644 "$work_dir/source"/profiles/*.toml "$INSTALL_DIR/profiles/"

printf '\n==> Verifying installed tools\n'
for tool in go qemu-system-x86_64 skopeo buildah cpio objcopy; do
    command -v "$tool" >/dev/null 2>&1 || fail "required command not found after install: $tool"
done
smoke_dir="$(mktemp -d)"
smoke_image="kern-installer-smoke-$$"
printf 'smoke test\n' >"$smoke_dir/marker"
printf 'FROM scratch\nCOPY marker /marker\n' >"$smoke_dir/Containerfile"
if ! buildah info >/dev/null || ! buildah build --pull=false -t "$smoke_image" -f "$smoke_dir/Containerfile" "$smoke_dir"; then
    if [[ "$created_storage_config" -ne 1 ]]; then
        fail "Buildah cannot initialize or build a local image; existing storage configuration was preserved"
    fi
    printf 'Buildah overlay failed; retrying with the portable VFS storage driver.\n'
    cat >"$storage_config" <<'EOF'
[storage]
driver = "vfs"
EOF
    buildah info >/dev/null || fail "rootless Buildah cannot initialize even with VFS storage"
    buildah build --pull=false -t "$smoke_image" -f "$smoke_dir/Containerfile" "$smoke_dir" \
        || fail "Buildah cannot build a local image as this user"
fi
buildah rmi "$smoke_image" >/dev/null
rm -rf "$smoke_dir"
smoke_dir=""
printf 'QEMU: %s\n' "$(qemu-system-x86_64 --version | head -n1)"
printf 'KVM: '
if kvm-ok >/dev/null 2>&1; then
    printf 'available (hardware acceleration)\n'
else
    printf 'not available; kern will use QEMU TCG emulation\n'
fi
kern --help >/dev/null || fail "kern failed its startup check"

printf '\nInstallation complete. Run kern build and kern run from an app directory.\n'
