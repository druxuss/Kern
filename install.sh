#!/usr/bin/env bash
set -Eeuo pipefail

# One-command installer for mainstream x86_64 Linux distributions and WSL2.
# Run as the target user; sudo is used only for packages and /usr/local.

readonly RELEASE_URL="https://github.com/druxuss/Kern/releases/download/kern-nightly"
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
if [[ ! -r /etc/os-release ]]; then
    fail "cannot identify this Linux distribution (/etc/os-release is missing)"
fi
# shellcheck disable=SC1091
source /etc/os-release
if [[ "$(uname -m)" != "x86_64" ]]; then
    fail "this release targets x86_64; detected $(uname -m)"
fi

sudo -v
printf 'Installing runtime packages...\n'
common_packages=(ca-certificates curl binutils cpio tar findutils skopeo buildah fuse-overlayfs crun slirp4netns)
distro_like=" ${ID_LIKE:-} "
if command -v apt-get >/dev/null 2>&1; then
    sudo apt-get -qq update
    if [[ "${ID}" == "ubuntu" || "$distro_like" == *" ubuntu "* ]] \
        && ! apt-cache show buildah >/dev/null 2>&1; then
        sudo apt-get -qq install -y software-properties-common
        sudo add-apt-repository -y universe >/dev/null
    fi
    sudo env DEBIAN_FRONTEND=noninteractive apt-get -qq install -y \
        "${common_packages[@]}" qemu-system-x86 uidmap iproute2 bridge-utils uml-utilities dnsmasq-base xz-utils
elif command -v dnf >/dev/null 2>&1 || command -v yum >/dev/null 2>&1; then
    if [[ "${ID}" == "fedora" || ( "$distro_like" == *" fedora "* && "$distro_like" != *" rhel "* ) ]]; then
        qemu_package="qemu-system-x86-core"
    else
        qemu_package="qemu-kvm"
    fi
    if command -v dnf >/dev/null 2>&1; then
        sudo dnf -q install -y "${common_packages[@]}" "$qemu_package" shadow-utils iproute bridge-utils dnsmasq xz
    else
        sudo yum -q install -y "${common_packages[@]}" "$qemu_package" shadow-utils iproute bridge-utils dnsmasq xz
    fi
elif command -v pacman >/dev/null 2>&1; then
    sudo pacman -S --needed --noconfirm "${common_packages[@]}" qemu-system-x86 shadow iproute2 bridge-utils dnsmasq xz
elif command -v zypper >/dev/null 2>&1; then
    sudo zypper --quiet --non-interactive install --no-recommends \
        "${common_packages[@]}" qemu-x86 shadow iproute2 bridge-utils dnsmasq xz
else
    fail "unsupported Linux package manager; supported managers are apt, dnf/yum, pacman, and zypper"
fi

printf 'Preparing rootless container storage...\n'
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
    fuse_overlay_path="$(command -v fuse-overlayfs)"
    cat >"$storage_config" <<EOF
[storage]
driver = "overlay"

[storage.options]
mount_program = "${fuse_overlay_path}"
EOF
    created_storage_config=1
fi

tmp_dir=""
smoke_dir=""
cleanup() {
    [[ -z "$tmp_dir" ]] || rm -rf "$tmp_dir"
    [[ -z "$smoke_dir" ]] || rm -rf "$smoke_dir"
}
trap cleanup EXIT

printf 'Downloading prebuilt kern bundle...\n'
tmp_dir="$(mktemp -d)"
archive="kern-linux-amd64.tar.gz"
cache_buster="v=$(date +%s)"
if ! curl --fail --location --silent --show-error --retry 2 --retry-all-errors \
    --connect-timeout 20 --max-time 300 \
    "${RELEASE_URL}/${archive}?${cache_buster}" -o "${tmp_dir}/${archive}"; then
    fail "could not download the prebuilt bundle; check the network or wait for the kern-nightly CI release"
fi
if ! curl --fail --location --silent --show-error --retry 2 --retry-all-errors \
    --connect-timeout 20 --max-time 60 \
    "${RELEASE_URL}/${archive}.sha256?${cache_buster}" -o "${tmp_dir}/${archive}.sha256"; then
    fail "could not download the bundle checksum"
fi
(
    cd "$tmp_dir"
    sha256sum --check --status "${archive}.sha256"
) || fail "prebuilt bundle checksum verification failed"
if tar -tzf "${tmp_dir}/${archive}" | grep -Eq '(^/|(^|/)\.\.(/|$))'; then
    fail "prebuilt bundle contains an unsafe path"
fi
mkdir -p "${tmp_dir}/bundle"
tar -xzf "${tmp_dir}/${archive}" -C "${tmp_dir}/bundle"
[[ -x "${tmp_dir}/bundle/kern" ]] || fail "prebuilt bundle is missing kern"
compgen -G "${tmp_dir}/bundle/profiles/*.toml" >/dev/null \
    || fail "prebuilt bundle is missing runtime profiles"
sudo install -m 0755 "${tmp_dir}/bundle/kern" "$INSTALL_DIR/kern"
sudo install -d -m 0755 "$INSTALL_DIR/profiles"
sudo install -m 0644 "${tmp_dir}/bundle"/profiles/*.toml "$INSTALL_DIR/profiles/"

printf 'Checking runtime tools...\n'
for tool in qemu-system-x86_64 skopeo buildah cpio objcopy xz; do
    command -v "$tool" >/dev/null 2>&1 || fail "required command not found after install: $tool"
done
smoke_dir="$(mktemp -d)"
smoke_image="kern-installer-smoke-$$"
printf 'smoke test\n' >"$smoke_dir/marker"
printf 'FROM scratch\nCOPY marker /marker\n' >"$smoke_dir/Containerfile"
if ! buildah info >/dev/null 2>&1 || ! buildah build --pull=false -t "$smoke_image" -f "$smoke_dir/Containerfile" "$smoke_dir" >/dev/null 2>&1; then
    if [[ "$created_storage_config" -ne 1 ]]; then
        fail "Buildah cannot initialize or build a local image; existing storage configuration was preserved"
    fi
    printf 'Overlay storage unavailable; retrying with VFS...\n'
    cat >"$storage_config" <<'EOF'
[storage]
driver = "vfs"
EOF
    buildah info >/dev/null 2>&1 || fail "rootless Buildah cannot initialize even with VFS storage"
    buildah build --pull=false -t "$smoke_image" -f "$smoke_dir/Containerfile" "$smoke_dir" >/dev/null 2>&1 \
        || fail "Buildah cannot build a local image as this user"
fi
buildah rmi "$smoke_image" >/dev/null 2>&1
rm -rf "$smoke_dir"
smoke_dir=""
printf 'Runtime: QEMU %s; ' "$(qemu-system-x86_64 --version | head -n1 | sed 's/^QEMU emulator version //; s/ .*//')"
if [[ -c /dev/kvm && -r /dev/kvm && -w /dev/kvm ]]; then
    printf 'KVM acceleration available\n'
else
    printf 'using QEMU emulation (KVM unavailable)\n'
fi
kern --help >/dev/null || fail "kern failed its startup check"

printf 'kern installed. Run: kern init, kern build, kern run\n'
