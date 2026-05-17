#!/usr/bin/env sh
set -eu

bin_dir="${1:-${TILDEWIRE_BIN_DIR:-$HOME/.local/bin}}"
canonical="$bin_dir/tildewire"

mkdir -p "$bin_dir"
: "${CGO_ENABLED:=0}"
export CGO_ENABLED
go build -trimpath -ldflags "-s -w" -o "$canonical" .
printf 'installed tildewire: %s\n' "$canonical"
