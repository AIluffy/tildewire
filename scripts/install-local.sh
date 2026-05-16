#!/usr/bin/env sh
set -eu

bin_dir="${1:-${TILDEWIRE_BIN_DIR:-$HOME/.local/bin}}"
canonical="$bin_dir/tildewire"
short="$bin_dir/tw"

mkdir -p "$bin_dir"
go build -o "$canonical" .
printf 'installed tildewire: %s\n' "$canonical"

if existing="$(command -v tw 2>/dev/null)"; then
	if [ "$existing" != "$short" ]; then
		printf 'skipped tw: command already exists at %s\n' "$existing"
		printf 'use tildewire, or choose a private shell alias such as twire or tdw\n'
		exit 0
	fi
fi

if [ -e "$short" ] || [ -L "$short" ]; then
	if [ -L "$short" ]; then
		link_target="$(readlink "$short" || true)"
		if [ "$link_target" = "$canonical" ] || [ "$link_target" = "tildewire" ]; then
			ln -sf "$canonical" "$short"
			printf 'installed tw: %s -> %s\n' "$short" "$canonical"
			exit 0
		fi
	fi
	printf 'skipped tw: path already exists at %s\n' "$short"
	printf 'use tildewire, or choose a private shell alias such as twire or tdw\n'
	exit 0
fi

ln -s "$canonical" "$short"
printf 'installed tw: %s -> %s\n' "$short" "$canonical"
