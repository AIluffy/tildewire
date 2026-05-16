#!/usr/bin/env sh
set -eu

repo="${TILDEWIRE_REPO:-AIluffy/tildewire}"
version="${TILDEWIRE_VERSION:-latest}"
bin_dir="${TILDEWIRE_BIN_DIR:-$HOME/.local/bin}"
github_url="${GITHUB_URL:-https://github.com}"
github_api_url="${GITHUB_API_URL:-https://api.github.com}"

fail() {
	printf 'tildewire install: %s\n' "$*" >&2
	exit 1
}

download() {
	url="$1"
	output="$2"
	if command -v curl >/dev/null 2>&1; then
		curl -fsSL "$url" -o "$output"
	elif command -v wget >/dev/null 2>&1; then
		wget -q "$url" -O "$output"
	else
		fail "missing curl or wget"
	fi
}

detect_os() {
	case "$(uname -s)" in
	Darwin)
		printf 'darwin\n'
		;;
	Linux)
		printf 'linux\n'
		;;
	*)
		fail "unsupported operating system: $(uname -s)"
		;;
	esac
}

detect_arch() {
	case "$(uname -m)" in
	x86_64 | amd64)
		printf 'amd64\n'
		;;
	arm64 | aarch64)
		printf 'arm64\n'
		;;
	*)
		fail "unsupported architecture: $(uname -m)"
		;;
	esac
}

resolve_tag() {
	if [ "$version" != "latest" ]; then
		case "$version" in
		v*)
			printf '%s\n' "$version"
			;;
		*)
			printf 'v%s\n' "$version"
			;;
		esac
		return 0
	fi

	release_json="$tmpdir/latest.json"
	download "$github_api_url/repos/$repo/releases/latest" "$release_json"
	tag="$(sed -n 's/.*"tag_name"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' "$release_json" | head -n 1)"
	[ -n "$tag" ] || fail "could not resolve latest release for $repo"
	printf '%s\n' "$tag"
}

verify_checksum() {
	checksums_file="$1"
	asset_name="$2"
	asset_path="$3"

	expected="$(awk -v file="$asset_name" '$2 == file { print $1 }' "$checksums_file" | head -n 1)"
	[ -n "$expected" ] || fail "checksum not found for $asset_name"

	if command -v sha256sum >/dev/null 2>&1; then
		actual="$(sha256sum "$asset_path" | awk '{ print $1 }')"
	elif command -v shasum >/dev/null 2>&1; then
		actual="$(shasum -a 256 "$asset_path" | awk '{ print $1 }')"
	else
		printf 'tildewire install: warning: missing sha256sum or shasum; skipping checksum verification\n' >&2
		return 0
	fi

	[ "$expected" = "$actual" ] || fail "checksum mismatch for $asset_name"
}

install_binary() {
	source_path="$1"
	target_path="$2"

	mkdir -p "$(dirname "$target_path")"
	if command -v install >/dev/null 2>&1; then
		install -m 0755 "$source_path" "$target_path"
	else
		cp "$source_path" "$target_path"
		chmod 0755 "$target_path"
	fi
}

install_short_alias() {
	canonical="$bin_dir/tildewire"
	short="$bin_dir/tw"

	if existing="$(command -v tw 2>/dev/null)"; then
		if [ "$existing" != "$short" ]; then
			printf 'skipped tw: command already exists at %s\n' "$existing"
			printf 'use tildewire, or choose a private shell alias such as twire or tdw\n'
			return 0
		fi
	fi

	if [ -e "$short" ] || [ -L "$short" ]; then
		if [ -L "$short" ]; then
			link_target="$(readlink "$short" || true)"
			if [ "$link_target" = "$canonical" ] || [ "$link_target" = "tildewire" ]; then
				ln -sf "$canonical" "$short"
				printf 'installed tw: %s -> %s\n' "$short" "$canonical"
				return 0
			fi
		fi
		printf 'skipped tw: path already exists at %s\n' "$short"
		printf 'use tildewire, or choose a private shell alias such as twire or tdw\n'
		return 0
	fi

	ln -s "$canonical" "$short"
	printf 'installed tw: %s -> %s\n' "$short" "$canonical"
}

tmpdir="$(mktemp -d)"
trap 'rm -rf "$tmpdir"' EXIT INT TERM

os="$(detect_os)"
arch="$(detect_arch)"
tag="$(resolve_tag)"
asset_version="${tag#v}"
archive="tildewire_${asset_version}_${os}_${arch}.tar.gz"
archive_url="$github_url/$repo/releases/download/$tag/$archive"
checksums_url="$github_url/$repo/releases/download/$tag/checksums.txt"

download "$archive_url" "$tmpdir/$archive"
download "$checksums_url" "$tmpdir/checksums.txt"
verify_checksum "$tmpdir/checksums.txt" "$archive" "$tmpdir/$archive"

tar -xzf "$tmpdir/$archive" -C "$tmpdir" tildewire
install_binary "$tmpdir/tildewire" "$bin_dir/tildewire"
printf 'installed tildewire: %s\n' "$bin_dir/tildewire"
install_short_alias

case ":$PATH:" in
*":$bin_dir:"*) ;;
*) printf 'note: add %s to PATH to run tildewire from any shell\n' "$bin_dir" ;;
esac
