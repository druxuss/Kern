#!/usr/bin/env bash
set -Eeuo pipefail

KERN_BRANCH="${KERN_BRANCH:-main}"
script_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"

if [[ -f "${script_dir}/install.sh" ]]; then
    exec bash "${script_dir}/install.sh"
fi

printf 'Installing kern into this WSL distribution from branch %s\n' "$KERN_BRANCH"
exec bash <(curl --fail --location --silent --show-error \
    "https://raw.githubusercontent.com/druxuss/Kern/${KERN_BRANCH}/install.sh")
