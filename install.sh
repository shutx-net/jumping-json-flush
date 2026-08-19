#!/bin/sh
#
# install.sh - download a released jjf binary and put it somewhere on PATH.
#
#   curl -fsSL https://raw.githubusercontent.com/shutx-net/jumping-json-flush/main/install.sh | sh
#
# This is POSIX sh, not bash: it runs under sh, dash, bash and busybox ash, so an
# alpine image that ships no bash can pipe it straight into `sh`.
#
# It is served from the default branch and never from a release URL, the same rule
# .claude-plugin/marketplace.json follows: a URL carrying a tag would pin whoever
# copied the one liner to that release forever.
#
# Everything is defined as a function and main runs on the last line, so a
# download that is cut off part way through executes nothing at all.
#
# Nothing here needs root and sudo is never invoked. Where the binary lands is
# decided by pick_install_dir below and printed before the script exits.

set -eu

# The repository the release assets come from. It is also the Go module path
# without its github.com/ prefix, which is what makes the `go install` hint at the
# bottom of an unsupported-platform error correct.
REPO='shutx-net/jumping-json-flush'

# The command being installed.
BINARY='jjf'

# The targets .github/workflows/release.yml publishes. A platform that is not on
# this list has no archive to download, so it is refused before anything is
# fetched rather than through a 404 halfway in.
TARGETS='linux/amd64 linux/arm64 windows/amd64 darwin/amd64 darwin/arm64'

# The shape of a release tag, the same gate the release workflow applies before it
# publishes under one. A tag also goes straight into a download URL, so this is
# what keeps a path out of it.
TAG_PATTERN='^v[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.]+)?$'

# Progress goes to standard error, so that standard output stays free and the
# messages still appear when the script itself arrived through a pipe.
say() {
  printf '==> %s\n' "$*" >&2
}

# Errors carry the script name, matching how jjf itself prefixes its own.
die() {
  printf 'install.sh: %s\n' "$*" >&2
  exit 1
}

# A misuse of the command line, kept apart from a failure of the install: exit 2
# is what jjf returns for invalid input too.
usage_error() {
  printf 'install.sh: %s\n' "$*" >&2
  usage
  exit 2
}

have() {
  command -v "$1" >/dev/null 2>&1
}

usage() {
  cat >&2 <<EOF
Install the ${BINARY} binary from a GitHub release.

  curl -fsSL https://raw.githubusercontent.com/${REPO}/main/install.sh | sh

Options:
  --version <tag>   release to install, such as v0.1.0 (default: the latest release)
  --dir <path>      directory to install into (default: /usr/local/bin when it is
                    writable, otherwise \$HOME/.local/bin)
  -h, --help        print this message and exit

Environment:
  JJF_VERSION       same as --version
  JJF_INSTALL_DIR   same as --dir

Options reach a piped script through -s --:

  curl -fsSL https://raw.githubusercontent.com/${REPO}/main/install.sh | sh -s -- --version v0.1.0
EOF
}

# The two paths that have to be cleaned up, held globally so that the trap can
# reach them. staged is a file inside the install directory and only exists for
# the moment between copying the binary there and renaming it into place.
work_dir=''
staged=''

cleanup() {
  if [ -n "$work_dir" ]; then
    rm -rf "$work_dir"
  fi
  if [ -n "$staged" ]; then
    rm -f "$staged"
  fi
}

# downloader is the tool found at startup: curl, or wget where curl is absent.
downloader=''

fetch_to_file() {
  if [ "$downloader" = curl ]; then
    curl -fsSL --proto '=https' --tlsv1.2 --retry 3 -o "$2" "$1"
  else
    wget -q -O "$2" "$1"
  fi
}

fetch_to_stdout() {
  if [ "$downloader" = curl ]; then
    curl -fsSL --proto '=https' --tlsv1.2 --retry 3 "$1"
  else
    wget -q -O - "$1"
  fi
}

# sha256_of prints the digest of one file, using whichever of the three usual
# tools is present. openssl is read from standard input so that the file name
# cannot end up in the part of the line being stripped; its label is SHA256 in
# OpenSSL 1.1 and SHA2-256 in 3.x, and neither is matched on.
sha256_of() {
  if have sha256sum; then
    sha256sum "$1" | cut -d' ' -f1
  elif have shasum; then
    shasum -a 256 "$1" | cut -d' ' -f1
  elif have openssl; then
    openssl dgst -sha256 < "$1" | sed 's/^.*= *//'
  else
    return 1
  fi
}

# parse_args reads the command line over the two environment variables, which
# main has already applied.
parse_args() {
  while [ "$#" -gt 0 ]; do
    case "$1" in
      --version)
        [ "$#" -ge 2 ] || usage_error '--version needs a tag'
        version="$2"
        shift 2
        ;;
      --version=*)
        version="${1#--version=}"
        shift
        ;;
      --dir)
        [ "$#" -ge 2 ] || usage_error '--dir needs a path'
        install_dir="$2"
        shift 2
        ;;
      --dir=*)
        install_dir="${1#--dir=}"
        shift
        ;;
      -h | --help)
        usage
        exit 0
        ;;
      *)
        usage_error "unknown option: $1"
        ;;
    esac
  done
}

# detect_target sets os and arch to the pair naming a published archive, and
# refuses anything else.
detect_target() {
  kernel="$(uname -s)"
  case "$kernel" in
    Linux) os=linux ;;
    Darwin) os=darwin ;;
    MINGW* | MSYS* | CYGWIN* | Windows_NT) os=windows ;;
    *) die "unsupported operating system: ${kernel}" ;;
  esac

  machine="$(uname -m)"
  case "$machine" in
    x86_64 | amd64) arch=amd64 ;;
    aarch64 | arm64) arch=arm64 ;;
    *) die "unsupported CPU architecture: ${machine}" ;;
  esac

  # The list is padded on both sides so that a plain substring match cannot
  # succeed on half an entry.
  case " ${TARGETS} " in
    *" ${os}/${arch} "*) ;;
    *)
      die "no release is published for ${os}/${arch} (published targets: ${TARGETS}). Build it yourself with: go install github.com/${REPO}/cmd/${BINARY}@latest"
      ;;
  esac
}

# latest_release prints the tag of the newest release that is not a pre-release,
# which is what GitHub's own "latest" means.
latest_release() {
  api="https://api.github.com/repos/${REPO}/releases/latest"
  body="$(fetch_to_stdout "$api")" || die "cannot read ${api}. The repository may have no published release yet, or this address may be over GitHub's unauthenticated limit of 60 API requests an hour; pass --version <tag> to skip the lookup"

  # tag_name is a top level key of the release object and GitHub emits it before
  # the free text body, so the first match is the tag and not something quoted
  # inside the release notes.
  tag="$(printf '%s\n' "$body" | sed -n 's/.*"tag_name"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' | sed -n '1p')"
  [ -n "$tag" ] || die "no tag_name in the response from ${api}"
  printf '%s\n' "$tag"
}

# pick_install_dir prints where the binary goes when no directory was asked for.
#
# sudo is deliberately never invoked. A password prompt appearing in the middle of
# `curl ... | sh` is exactly the surprise a piped script must not spring, and it
# cannot even be answered where no terminal is attached. Anyone who wants the
# binary in a root owned directory can say so, either by running the script as
# root or by passing --dir.
pick_install_dir() {
  if [ "$(id -u)" = 0 ] || [ -w /usr/local/bin ]; then
    printf '%s\n' /usr/local/bin
  else
    [ -n "${HOME-}" ] || die 'HOME is not set, so there is no default directory to install into; pass --dir <path>'
    printf '%s\n' "${HOME}/.local/bin"
  fi
}

main() {
  version="${JJF_VERSION-}"
  install_dir="${JJF_INSTALL_DIR-}"
  parse_args "$@"

  if have curl; then
    downloader=curl
  elif have wget; then
    downloader=wget
  else
    die 'neither curl nor wget was found, and one of them is needed to download the release'
  fi

  detect_target
  say "target: ${os}/${arch}"

  # Resolved before the download, so that an unusable destination is reported
  # without spending a few megabytes first. It is created further down, once
  # there is something to put in it.
  if [ -z "$install_dir" ]; then
    install_dir="$(pick_install_dir)"
  fi
  if [ -e "$install_dir" ] && { [ ! -d "$install_dir" ] || [ ! -w "$install_dir" ]; }; then
    die "${install_dir} is not a writable directory; pass --dir <path>, or run the script as root"
  fi

  if [ -z "$version" ]; then
    version="$(latest_release)"
    say "latest release: ${version}"
  fi
  printf '%s\n' "$version" | grep -Eq "$TAG_PATTERN" ||
    die "\"${version}\" is not a release tag; want v1.2.3 or v1.2.3-rc.1"

  # The archive is named after the target and unpacks into a directory of the
  # same name, which is where the binary is looked for further down.
  stem="${BINARY}_${version}_${os}_${arch}"
  if [ "$os" = windows ]; then
    archive="${stem}.zip"
    program="${BINARY}.exe"
  else
    archive="${stem}.tar.gz"
    program="${BINARY}"
  fi
  base_url="https://github.com/${REPO}/releases/download/${version}"

  trap cleanup EXIT
  trap 'exit 130' INT
  trap 'exit 143' TERM
  work_dir="$(mktemp -d "${TMPDIR:-/tmp}/jjf-install.XXXXXXXX")" ||
    die 'cannot create a temporary directory'

  say "downloading ${archive}"
  fetch_to_file "${base_url}/${archive}" "${work_dir}/${archive}" ||
    die "cannot download ${base_url}/${archive}"
  fetch_to_file "${base_url}/checksums.txt" "${work_dir}/checksums.txt" ||
    die "cannot download ${base_url}/checksums.txt"

  # The digest is compared here rather than handed to `sha256sum --check`, whose
  # --ignore-missing is a GNU extension that busybox and macOS do not have. The
  # second field of a checksums.txt line is the file name, optionally with the
  # binary-mode asterisk in front of it.
  expected="$(awk -v want="$archive" '$2 == want || $2 == "*" want { print $1; exit }' "${work_dir}/checksums.txt")"
  [ -n "$expected" ] || die "checksums.txt of ${version} does not list ${archive}"
  actual="$(sha256_of "${work_dir}/${archive}")" ||
    die 'no sha256 tool was found (sha256sum, shasum or openssl); install one, or download the release by hand'
  [ "$expected" = "$actual" ] ||
    die "checksum mismatch for ${archive}: got ${actual}, want ${expected}"
  say "sha256 verified"

  if [ "$os" = windows ]; then
    have unzip || die "unzip is needed to unpack ${archive}"
    unzip -q "${work_dir}/${archive}" -d "$work_dir" || die "cannot unpack ${archive}"
  else
    tar -xzf "${work_dir}/${archive}" -C "$work_dir" || die "cannot unpack ${archive}"
  fi
  unpacked="${work_dir}/${stem}/${program}"
  [ -f "$unpacked" ] || die "${archive} does not contain ${stem}/${program}"

  mkdir -p "$install_dir" || die "cannot create ${install_dir}"

  # Staged inside the destination directory and renamed into place, so that the
  # install is atomic and never leaves a half written binary behind. It also
  # replaces a copy that is currently running, which writing over the file in
  # place would not.
  staged="${install_dir}/.${BINARY}.install.$$"
  cp "$unpacked" "$staged" || die "cannot write into ${install_dir}"
  chmod 0755 "$staged"
  mv -f "$staged" "${install_dir}/${program}" || die "cannot install into ${install_dir}"
  staged=''

  installed="${install_dir}/${program}"
  say "installed ${installed}"

  "$installed" version >&2 || die "${installed} was installed but does not run"

  case ":${PATH-}:" in
    *":${install_dir}:"*) ;;
    *)
      say "${install_dir} is not on your PATH. Add it with:"
      # The $PATH inside the format string is the literal text the reader is meant
      # to copy, so it must not expand here.
      # shellcheck disable=SC2016
      printf '\n    export PATH="%s:$PATH"\n\n' "$install_dir" >&2
      ;;
  esac
}

main "$@"
