#!/bin/sh
# Installs spot-go from its GitHub releases:
#
#   curl -fsSL https://raw.githubusercontent.com/franciscocpg/spot-go/main/install.sh | sh
#
# To pick the version or the folder, pass them after `sh -s --`:
#
#   curl -fsSL https://raw.githubusercontent.com/franciscocpg/spot-go/main/install.sh | sh -s -- --version 1.2.3 --dir /usr/local/bin
#
# SPOT_GO_VERSION and SPOT_GO_INSTALL_DIR set them too.

set -eu

repo=franciscocpg/spot-go
version=${SPOT_GO_VERSION:-latest}
dir=${SPOT_GO_INSTALL_DIR:-$HOME/.local/bin}

usage() {
	cat <<EOF
Installs spot-go from https://github.com/$repo/releases.

Usage: install.sh [--version VERSION] [--dir DIR]

  --version VERSION  the version to install, such as 1.2.3 (default: the latest)
  --dir DIR          the folder to install spot-go to (default: ~/.local/bin)
EOF
}

fail() {
	echo "install.sh: $*" >&2
	exit 1
}

while [ $# -gt 0 ]; do
	case $1 in
	--version)
		[ $# -ge 2 ] || fail "--version needs a version"
		version=$2
		shift 2
		;;
	--dir)
		[ $# -ge 2 ] || fail "--dir needs a folder"
		dir=$2
		shift 2
		;;
	-h | --help)
		usage
		exit 0
		;;
	*)
		usage >&2
		exit 2
		;;
	esac
done

# platform prints the platform of the release asset to install, such as
# linux_amd64: the platforms Soloist, which plays the audio, has a build for.
platform() {
	os=$(uname -s)
	arch=$(uname -m)
	case $os in
	Darwin)
		# A shell running under Rosetta reports x86_64 on Apple Silicon.
		if [ "$(sysctl -n hw.optional.arm64 2>/dev/null || true)" = 1 ]; then
			arch=arm64
		fi
		[ "$arch" = arm64 ] || fail "spot-go only runs on Apple Silicon Macs: Soloist has no build for Intel Macs"
		echo darwin_arm64
		;;
	Linux)
		case $arch in
		x86_64 | amd64) echo linux_amd64 ;;
		aarch64 | arm64)
			# A 32-bit system on a 64-bit kernel needs the 32-bit build.
			if [ "$(getconf LONG_BIT 2>/dev/null || echo 64)" = 32 ]; then
				echo linux_armv7
			else
				echo linux_arm64
			fi
			;;
		armv7* | armv8l) echo linux_armv7 ;;
		*) fail "spot-go doesn't run on $arch: Soloist has no build for it" ;;
		esac
		;;
	*) fail "spot-go doesn't run on $os: Soloist has no build for it" ;;
	esac
}

# download downloads the URL $1 to the file $2.
download() {
	if command -v curl >/dev/null 2>&1; then
		curl -fsSL -o "$2" "$1"
	elif command -v wget >/dev/null 2>&1; then
		wget -q -O "$2" "$1"
	else
		fail "curl or wget is needed to download spot-go"
	fi
}

# sha256 prints the SHA-256 of the file $1.
sha256() {
	if command -v sha256sum >/dev/null 2>&1; then
		sha256sum "$1" | cut -d ' ' -f 1
	elif command -v shasum >/dev/null 2>&1; then
		shasum -a 256 "$1" | cut -d ' ' -f 1
	else
		fail "sha256sum or shasum is needed to check the download"
	fi
}

p=$(platform) || exit 1
asset=spot-go_$p.tar.gz
case $version in
latest) base=https://github.com/$repo/releases/latest/download ;;
*) base=https://github.com/$repo/releases/download/v${version#v} ;;
esac

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

echo "Downloading $base/$asset"
download "$base/$asset" "$tmp/$asset" || fail "fail to download $base/$asset"
download "$base/checksums.txt" "$tmp/checksums.txt" || fail "fail to download $base/checksums.txt"

want=$(awk -v f="$asset" '$2 == f { print $1 }' "$tmp/checksums.txt")
[ -n "$want" ] || fail "checksums.txt has no checksum for $asset"
got=$(sha256 "$tmp/$asset")
[ "$got" = "$want" ] || fail "the SHA-256 of $asset is $got, want $want"

tar -xzf "$tmp/$asset" -C "$tmp" spot-go
mkdir -p "$dir"
# Into the folder and then renamed, so a spot-go that still runs, such as
# spot-go in the background, keeps running from the file it started from.
cp "$tmp/spot-go" "$dir/.spot-go.new"
chmod 755 "$dir/.spot-go.new"
mv -f "$dir/.spot-go.new" "$dir/spot-go"

echo "Installed $("$dir/spot-go" --version) to $dir/spot-go"
case :$PATH: in
*:"$dir":*) ;;
*) echo "$dir isn't in your PATH. Add it, or run $dir/spot-go." ;;
esac
