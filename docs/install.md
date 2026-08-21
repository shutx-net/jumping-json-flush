# Installing jjf

[README](https://github.com/shutx-net/jumping-json-flush/blob/main/README.md) · [日本語](install.ja.md)

`jjf` is one statically linked binary. There is no runtime to install beside it,
no CGO and no glibc, so a musl or alpine image runs it as it is.

| Method | Use it when |
| --- | --- |
| `install.sh` | you are setting up a workstation or an image and have curl or wget |
| release archives | you are in CI, or you want a URL that cannot change under you |
| `go install` | a Go toolchain is already there |
| nix | you are on nix |

## install.sh

```sh
curl -fsSL https://raw.githubusercontent.com/shutx-net/jumping-json-flush/main/install.sh | sh
```

The script is POSIX sh rather than bash, so `| bash` does exactly the same thing
and an alpine image that ships no bash runs it unchanged.

What it does, in order:

1. works out the target from `uname`, and refuses a platform that has no published
   archive instead of finding out through a 404
2. asks the GitHub API for the latest release tag, unless one was given
3. downloads the archive and the release's `checksums.txt`
4. **compares the sha256 and stops if the two disagree**, before anything is
   unpacked
5. unpacks into a temporary directory, which is removed however the script ends
6. copies the binary into the install directory under a temporary name and renames
   it into place, so the install is atomic and can replace a copy that is running
7. runs `jjf version`, and prints where the binary went

### Options

| Option | Environment | Default |
| --- | --- | --- |
| `--version <tag>` | `JJF_VERSION` | the latest release |
| `--dir <path>` | `JJF_INSTALL_DIR` | see [Where the binary goes](#where-the-binary-goes) |
| `--help` | | |

A piped script takes its options after `-s --`:

```sh
curl -fsSL https://raw.githubusercontent.com/shutx-net/jumping-json-flush/main/install.sh |
  sh -s -- --version v0.1.0 --dir ~/bin
```

The environment variables are the same thing said in front of the shell, which is
sometimes easier to read:

```sh
curl -fsSL https://raw.githubusercontent.com/shutx-net/jumping-json-flush/main/install.sh |
  JJF_VERSION=v0.1.0 JJF_INSTALL_DIR=~/bin sh
```

A tag that is not a release version is refused. Nothing else is ever interpolated
into a download URL.

### Where the binary goes

| Condition | Directory |
| --- | --- |
| `--dir` or `JJF_INSTALL_DIR` was given | that directory |
| running as root, or `/usr/local/bin` is writable | `/usr/local/bin` |
| otherwise | `$HOME/.local/bin` |

**`sudo` is never invoked.** A password prompt in the middle of a piped script is
a surprise, and it cannot be answered at all where no terminal is attached. To put
the binary somewhere only root can write, run the whole script as root:

```sh
curl -fsSL https://raw.githubusercontent.com/shutx-net/jumping-json-flush/main/install.sh | sudo sh
```

The directory it chose is printed at the end, together with the line to add if it
is not on your `PATH`.

### What has to be there already

- curl, or wget
- tar, or unzip on Windows
- one of `sha256sum`, `shasum` or `openssl`. **Without one of the three the script
  refuses to install**, because it has no way to check what it downloaded

### Platforms

Five targets are published: `linux/amd64`, `linux/arm64`, `windows/amd64`,
`darwin/amd64` and `darwin/arm64`. Linux, macOS and WSL are covered directly.

On Windows the script runs only under Git Bash or Cygwin, and needs `unzip`, which
Git for Windows does not include. Without it, take the zip by hand as described
below.

### In CI

Pin the version, so that a pipeline does not change what it installs on its own:

```sh
curl -fsSL https://raw.githubusercontent.com/shutx-net/jumping-json-flush/main/install.sh |
  sh -s -- --version v0.1.0
```

That is what
[`examples/ci/github-actions.yml`](https://github.com/shutx-net/jumping-json-flush/blob/main/examples/ci/github-actions.yml) and
[`examples/ci/gitlab-ci.yml`](https://github.com/shutx-net/jumping-json-flush/blob/main/examples/ci/gitlab-ci.yml) do.

Even pinned, the script itself is served from the default branch and can be
changed there. A pipeline that wants a URL nobody can move should take the
[release archive](#release-archives) instead, and check its sha256 against the
release's `checksums.txt` itself.

### On piping a script into a shell

It is a fair thing to be wary of. What this one does about it:

- every statement at the top level is a constant or a function definition, and
  `main "$@"` is the last line of the file, so a connection cut off part way
  through runs nothing at all
- the archive's sha256 is checked against the release's `checksums.txt` before it
  is unpacked, and a mismatch installs nothing
- it writes exactly one file, at a path it prints
- it never calls `sudo`, and it never edits a shell profile
- reading it first costs nothing:
  `curl -fsSL https://raw.githubusercontent.com/shutx-net/jumping-json-flush/main/install.sh | less`

## Release archives

Every release publishes the five archives and a `checksums.txt` covering all of
them, at
[Releases](https://github.com/shutx-net/jumping-json-flush/releases). Each archive
unpacks into a directory named after itself, holding the binary, `LICENSE` and
both READMEs.

```sh
VERSION=v0.1.0
ARCHIVE="jjf_${VERSION}_linux_amd64"
BASE="https://github.com/shutx-net/jumping-json-flush/releases/download/${VERSION}"

curl -sSfL -O "${BASE}/${ARCHIVE}.tar.gz"
curl -sSfL -O "${BASE}/checksums.txt"
# checksums.txt covers all five archives, so the other four are expected to be
# missing here.
sha256sum --check --ignore-missing checksums.txt

tar xzf "${ARCHIVE}.tar.gz"
sudo install -m 0755 "${ARCHIVE}/jjf" /usr/local/bin/jjf
jjf version
```

`--ignore-missing` is a GNU extension. Where it is absent, busybox and macOS
included, keep the one line that matters instead:

```sh
grep "  ${ARCHIVE}.tar.gz$" checksums.txt > archive.sha256
sha256sum -c archive.sha256      # macOS: shasum -a 256 -c archive.sha256
```

## go install

```sh
go install github.com/shutx-net/jumping-json-flush/cmd/jjf@latest
```

Name a tag, as in `@v0.1.0`, to pin a version. Go 1.26 or later is required; an
older Go fetches the toolchain `go.mod` names, unless `GOTOOLCHAIN=local` forbids
it. The version such a binary reports is the module version Go recorded, not a
release tag.

## nix

```sh
nix profile add github:shutx-net/jumping-json-flush
```

`nix run github:shutx-net/jumping-json-flush -- validate db-design.json` runs it
without installing anything. A binary built by nix reports
`v<version>+nix.<rev>`, because a nix build has no VCS metadata to stamp a tag
from.

## Upgrading

Run the same command again. `install.sh` replaces the binary where it already is,
and `go install` and `nix` do their usual thing.

## Uninstalling

Delete the binary. Nothing else was written: no configuration, no cache, no shell
profile was touched.

```sh
rm "$(command -v jjf)"
```
