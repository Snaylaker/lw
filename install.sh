#!/bin/sh
# lw installer — https://github.com/snaylaker/lw
#
# Downloads a prebuilt lw binary, verifies it against the release checksums file,
# and installs it into ~/.local/bin (override with --dir / LW_INSTALL_DIR / PREFIX).
# When no prebuilt asset matches the platform it falls back to `go build` from a
# shallow clone. It never needs root, and it never writes outside the install
# directory and a temporary directory it removes on exit.
#
#   curl -fsSL https://raw.githubusercontent.com/snaylaker/lw/main/install.sh | sh
#   curl -fsSL .../install.sh | sh -s -- --version v0.1.0 --dir /opt/tools/bin

set -eu

MODULE="github.com/snaylaker/lw"
REPO="${LW_REPO:-${MODULE#github.com/}}"
VERSION="${LW_VERSION:-}"
INSTALL_DIR="${LW_INSTALL_DIR:-}"
FROM_SOURCE="${LW_FROM_SOURCE:-0}"
WORK=""

log() { printf '%s\n' "$*"; }
warn() { printf 'warning: %s\n' "$*" >&2; }

die() {
	printf 'error: %s\n' "$1" >&2
	if [ "$#" -gt 1 ]; then
		printf 'next: %s\n' "$2" >&2
	fi
	exit 1
}

die_usage() {
	printf 'error: %s\n' "$1" >&2
	usage >&2
	exit 2
}

usage() {
	cat <<'EOF'
lw installer

Usage: install.sh [options]

Options:
  --version <tag>       release to install (e.g. v0.1.0); default: latest release
  --dir <path>          directory to install into; default: ~/.local/bin
  --build-from-source   skip the prebuilt asset and build with `go build`
  --help                print this message

Environment:
  LW_VERSION        same as --version
  LW_INSTALL_DIR    same as --dir
  PREFIX            install into $PREFIX/bin
  LW_FROM_SOURCE=1  same as --build-from-source
  LW_REPO           owner/name of the GitHub repository (default: snaylaker/lw)
EOF
}

cleanup() {
	if [ -n "$WORK" ] && [ -d "$WORK" ]; then
		rm -rf "$WORK"
	fi
}
trap cleanup EXIT
trap 'cleanup; exit 130' INT
trap 'cleanup; exit 143' HUP TERM

have() { command -v "$1" >/dev/null 2>&1; }

# --- arguments ---------------------------------------------------------------

while [ "$#" -gt 0 ]; do
	case "$1" in
	--version)
		[ "$#" -ge 2 ] || die_usage "--version needs a value"
		VERSION="$2"
		shift 2
		;;
	--version=*)
		VERSION="${1#--version=}"
		shift
		;;
	--dir)
		[ "$#" -ge 2 ] || die_usage "--dir needs a value"
		INSTALL_DIR="$2"
		shift 2
		;;
	--dir=*)
		INSTALL_DIR="${1#--dir=}"
		shift
		;;
	--build-from-source)
		FROM_SOURCE=1
		shift
		;;
	--help | -h)
		usage
		exit 0
		;;
	*)
		die_usage "unknown option: $1"
		;;
	esac
done

if [ -z "$INSTALL_DIR" ]; then
	if [ -n "${PREFIX:-}" ]; then
		INSTALL_DIR="$PREFIX/bin"
	else
		INSTALL_DIR="${HOME:-.}/.local/bin"
	fi
fi

# --- platform ----------------------------------------------------------------

OS="unknown"
case "$(uname -s 2>/dev/null || echo unknown)" in
Darwin) OS="darwin" ;;
Linux) OS="linux" ;;
MINGW* | MSYS* | CYGWIN* | Windows_NT) OS="windows" ;;
esac

ARCH="unknown"
case "$(uname -m 2>/dev/null || echo unknown)" in
x86_64 | amd64) ARCH="amd64" ;;
arm64 | aarch64) ARCH="arm64" ;;
esac

BIN="lw"
if [ "$OS" = "windows" ]; then
	BIN="lw.exe"
fi

# Prebuilt assets are published for darwin and linux on amd64 and arm64 only.
PREBUILT=1
if [ "$OS" != "darwin" ] && [ "$OS" != "linux" ]; then
	PREBUILT=0
fi
if [ "$ARCH" = "unknown" ]; then
	PREBUILT=0
fi
if [ "$FROM_SOURCE" = "1" ]; then
	PREBUILT=0
fi

# --- download helpers --------------------------------------------------------

DOWNLOADER=""
if have curl; then
	DOWNLOADER="curl"
elif have wget; then
	DOWNLOADER="wget"
fi

# fetch <url> <destination-file>
fetch() {
	case "$DOWNLOADER" in
	curl) curl -fsSL --proto '=https' --tlsv1.2 -o "$2" "$1" ;;
	wget) wget -q -O "$2" "$1" ;;
	*) return 1 ;;
	esac
}

# fetch_stdout <url>
fetch_stdout() {
	case "$DOWNLOADER" in
	curl) curl -fsSL --proto '=https' --tlsv1.2 "$1" ;;
	wget) wget -q -O - "$1" ;;
	*) return 1 ;;
	esac
}

# sha256_of <file>
sha256_of() {
	if have sha256sum; then
		sha256sum "$1" | awk '{print $1}'
	elif have shasum; then
		shasum -a 256 "$1" | awk '{print $1}'
	elif have openssl; then
		openssl dgst -sha256 "$1" | awk '{print $NF}'
	else
		return 1
	fi
}

# --- resolve the version -----------------------------------------------------

resolve_latest_tag() {
	fetch_stdout "https://api.github.com/repos/$REPO/releases/latest" 2>/dev/null |
		sed -n 's/.*"tag_name"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' |
		head -n 1
}

TAG=""
if [ "$PREBUILT" = "1" ]; then
	if [ -z "$DOWNLOADER" ]; then
		die "no curl or wget found" "install curl or wget, or re-run with --build-from-source"
	fi
	if [ -n "$VERSION" ]; then
		TAG="$VERSION"
	else
		TAG="$(resolve_latest_tag || true)"
		if [ -z "$TAG" ]; then
			warn "could not resolve the latest release of $REPO; building from source instead"
			PREBUILT=0
		fi
	fi
else
	TAG="$VERSION"
fi

# --- announce ----------------------------------------------------------------

log "lw installer"
log "  repository:  https://github.com/$REPO"
if [ -n "$TAG" ]; then
	log "  version:     $TAG"
else
	log "  version:     default branch (source build)"
fi
log "  platform:    $OS/$ARCH"
log "  install to:  $INSTALL_DIR/$BIN"
if [ "$PREBUILT" = "1" ]; then
	NOV="${TAG#v}"
	ASSET="lw_${NOV}_${OS}_${ARCH}.tar.gz"
	BASE="https://github.com/$REPO/releases/download/$TAG"
	log "  method:      download $ASSET, verify against checksums.txt"
else
	if [ "$FROM_SOURCE" = "1" ]; then
		log "  method:      build from source (--build-from-source)"
	else
		log "  method:      build from source (no prebuilt asset for $OS/$ARCH)"
	fi
fi
log ""

# --- install directory -------------------------------------------------------

if [ ! -d "$INSTALL_DIR" ]; then
	mkdir -p "$INSTALL_DIR" ||
		die "cannot create $INSTALL_DIR" "pick a writable directory with --dir <path>"
	log "created $INSTALL_DIR"
fi
if [ ! -w "$INSTALL_DIR" ]; then
	die "$INSTALL_DIR is not writable" "pick a writable directory with --dir <path>; this installer never uses sudo"
fi

WORK="$(mktemp -d 2>/dev/null || mktemp -d -t lw-install)" ||
	die "cannot create a temporary directory" "check that TMPDIR points somewhere writable"

# --- fetch a prebuilt binary -------------------------------------------------

download_prebuilt() {
	log "downloading $BASE/$ASSET"
	if ! fetch "$BASE/$ASSET" "$WORK/$ASSET"; then
		return 1
	fi

	log "downloading $BASE/checksums.txt"
	if ! fetch "$BASE/checksums.txt" "$WORK/checksums.txt"; then
		die "release $TAG has no checksums.txt" "an unverifiable download is never installed; re-run with --build-from-source"
	fi

	EXPECTED="$(awk -v want="$ASSET" '$2 == want || $2 == "*" want {print $1; exit}' "$WORK/checksums.txt")"
	if [ -z "$EXPECTED" ]; then
		die "checksums.txt for $TAG has no entry for $ASSET" "an unverifiable download is never installed; re-run with --build-from-source"
	fi

	ACTUAL="$(sha256_of "$WORK/$ASSET")" ||
		die "no sha256sum, shasum or openssl found" "install one of them, or re-run with --build-from-source"

	if [ "$EXPECTED" != "$ACTUAL" ]; then
		die "checksum mismatch for $ASSET (expected $EXPECTED, got $ACTUAL)" "the download is corrupt or tampered with; nothing was installed"
	fi
	log "checksum ok: $ACTUAL"

	tar -xzf "$WORK/$ASSET" -C "$WORK" ||
		die "cannot extract $ASSET" "re-run with --build-from-source"

	if [ ! -f "$WORK/$BIN" ]; then
		FOUND="$(find "$WORK" -type f -name "$BIN" -print 2>/dev/null | head -n 1)"
		[ -n "$FOUND" ] || die "$ASSET does not contain a $BIN binary" "re-run with --build-from-source"
		mv "$FOUND" "$WORK/$BIN"
	fi
	return 0
}

# --- build from source -------------------------------------------------------

build_from_source() {
	have git || die "git is required to build from source" "install git, then re-run"
	have go || die "no Go toolchain found and no prebuilt binary for $OS/$ARCH" "install Go from https://go.dev/dl/, then re-run"

	if [ -n "$TAG" ]; then
		log "cloning https://github.com/$REPO at $TAG"
		git clone --quiet --depth 1 --branch "$TAG" "https://github.com/$REPO" "$WORK/src" ||
			die "cannot clone $REPO at $TAG" "check that the tag exists, or drop --version to build the default branch"
	else
		log "cloning https://github.com/$REPO"
		git clone --quiet --depth 1 "https://github.com/$REPO" "$WORK/src" ||
			die "cannot clone $REPO" "check your network access to github.com"
	fi

	log "building ./cmd/lw with go build"
	# Stamped, so a source build reports the version the installer announced
	# rather than the "dev" default of internal/cli.Version.
	(cd "$WORK/src" && go build -trimpath \
		-ldflags "-s -w -X github.com/snaylaker/lw/internal/cli.Version=${TAG:-dev}" \
		-o "$WORK/$BIN" ./cmd/lw) ||
		die "go build failed" "run go build ./cmd/lw inside a clone of $REPO to see why"
	return 0
}

if [ "$PREBUILT" = "1" ]; then
	if ! download_prebuilt; then
		warn "no prebuilt asset $ASSET in release $TAG; building from source instead"
		build_from_source
	fi
else
	build_from_source
fi

# --- install -----------------------------------------------------------------

STAGED="$INSTALL_DIR/.lw-install.$$"
cp "$WORK/$BIN" "$STAGED" || die "cannot write to $INSTALL_DIR" "pick a writable directory with --dir <path>"
chmod 0755 "$STAGED"
mv -f "$STAGED" "$INSTALL_DIR/$BIN" || {
	rm -f "$STAGED"
	die "cannot install into $INSTALL_DIR" "pick a writable directory with --dir <path>"
}

log ""
log "installed $INSTALL_DIR/$BIN"
if [ -x "$INSTALL_DIR/$BIN" ]; then
	"$INSTALL_DIR/$BIN" --version || true
fi

case ":${PATH:-}:" in
*":$INSTALL_DIR:"*) ;;
*)
	log ""
	warn "$INSTALL_DIR is not on your PATH"
	log "add it to your shell profile:"
	log "    export PATH=\"$INSTALL_DIR:\$PATH\""
	;;
esac

log ""
log "next: run 'lw' to connect a Read-only Linear personal API key."
log "      Onboarding prefers the system credential store and asks before using an"
log "      owner-only file. You can instead set LINEAR_API_KEY or credentialCommand."
log "      Run 'lw doctor' to inspect the active credential source and environment."
