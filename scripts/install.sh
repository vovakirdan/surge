#!/bin/sh
set -eu

repo="${SURGE_REPO:-vovakirdan/surge}"
version="${SURGE_VERSION:-latest}"
install_dir="${SURGE_INSTALL_DIR:-$HOME/.surge}"

say() {
	printf '%s\n' "$*"
}

need() {
	if ! command -v "$1" >/dev/null 2>&1; then
		say "error: $1 is required" >&2
		exit 1
	fi
}

download() {
	url="$1"
	out="$2"
	if command -v curl >/dev/null 2>&1; then
		curl -fL --retry 5 --retry-delay 2 --retry-connrefused \
			--connect-timeout 20 --speed-limit 1024 --speed-time 180 \
			"$url" -o "$out"
	elif command -v wget >/dev/null 2>&1; then
		wget -O "$out" "$url"
	else
		say "error: curl or wget is required" >&2
		exit 1
	fi
}

os="$(uname -s)"
arch="$(uname -m)"
case "$os:$arch" in
	Linux:x86_64|Linux:amd64)
		asset="surge-linux-x86_64.tar.gz"
		;;
	Darwin:x86_64|Darwin:amd64)
		asset="surge-darwin-x86_64.tar.gz"
		;;
	Darwin:arm64|Darwin:aarch64)
		asset="surge-darwin-arm64.tar.gz"
		;;
	*)
		say "error: unsupported platform: $os $arch" >&2
		say "supported release installers: Linux x86_64, macOS x86_64, macOS arm64" >&2
		exit 1
		;;
esac

need tar
need mktemp

if [ "${SURGE_DOWNLOAD_BASE_URL:-}" ]; then
	base_url="${SURGE_DOWNLOAD_BASE_URL%/}"
elif [ "$version" = "latest" ]; then
	base_url="https://github.com/$repo/releases/latest/download"
else
	case "$version" in
		v*) tag="$version" ;;
		*) tag="v$version" ;;
	esac
	base_url="https://github.com/$repo/releases/download/$tag"
fi

tmpdir="$(mktemp -d)"
trap 'rm -rf "$tmpdir"' EXIT INT TERM

say "downloading Surge from $base_url"
download "$base_url/$asset" "$tmpdir/$asset"
download "$base_url/SHA256SUMS" "$tmpdir/SHA256SUMS"

if command -v sha256sum >/dev/null 2>&1; then
	(cd "$tmpdir" && grep " $asset\$" SHA256SUMS > SHA256SUMS.one && sha256sum -c SHA256SUMS.one)
elif command -v shasum >/dev/null 2>&1; then
	(cd "$tmpdir" && grep " $asset\$" SHA256SUMS > SHA256SUMS.one && shasum -a 256 -c SHA256SUMS.one)
else
	say "warning: sha256sum/shasum not found; skipping checksum verification" >&2
fi

stage="$tmpdir/stage"
mkdir -p "$stage"
tar -xzf "$tmpdir/$asset" -C "$stage"

if [ ! -x "$stage/bin/surge" ] || [ ! -f "$stage/share/surge/core/intrinsics.sg" ]; then
	say "error: release archive has an unexpected layout" >&2
	exit 1
fi

mkdir -p "$install_dir/bin" "$install_dir/share"
new_bin="$install_dir/bin/surge.new.$$"
new_share="$install_dir/share/surge.new.$$"
rm -f "$new_bin"
rm -rf "$new_share"
cp "$stage/bin/surge" "$new_bin"
cp -R "$stage/share/surge" "$new_share"
chmod 755 "$new_bin"
mv "$new_bin" "$install_dir/bin/surge"
rm -rf "$install_dir/share/surge"
mv "$new_share" "$install_dir/share/surge"

say "installed Surge to $install_dir"
"$install_dir/bin/surge" version --full

case ":$PATH:" in
	*":$install_dir/bin:"*) ;;
	*)
		say ""
		say "add Surge to PATH:"
		say "  export PATH=\"$install_dir/bin:\$PATH\""
		;;
esac

say ""
say "LLVM backend still needs clang, llvm, and lld from your system packages."
say "To update later, run this installer again."
