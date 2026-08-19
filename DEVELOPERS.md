# Developing jjf

[README](README.md) · [README (日本語)](README.ja.md)

Go is not expected on the host. Two environments provide it, and the command
table below is written for both.

## devcontainer

The container image carries Go. Drive it with the
[devcontainer CLI](https://github.com/devcontainers/cli).

```sh
devcontainer up   --workspace-folder .
devcontainer exec --workspace-folder . bash -lc '<CMD>'
```

## nix flake

```sh
nix develop            # enter the shell, then run <CMD> inside it
nix develop -c <CMD>   # run one command and leave
```

The shell provides the Go of the locked nixpkgs, which `flake.nix` asserts still
satisfies the `go` directive of `go.mod`, plus gopls, staticcheck, gh and jq.
`nix build` produces `./result/bin/jjf` and runs `go test ./...` as its check
phase, which is what `nix flake check` verifies in CI. `.envrc` is there for
direnv.

## Commands

| Goal | `<CMD>` |
| --- | --- |
| build | `go build -o /tmp/jjf ./cmd/jjf` |
| run | `go run ./cmd/jjf validate examples/db-design.example.json` |
| test | `go test ./...` |
| test (race) | `CGO_ENABLED=1 go test -race ./...` |
| regenerate goldens | `go test ./cmd/jjf/ ./internal/schema/ ./internal/sml/ ./internal/export/xlsx/ -update` |
| coverage | `go test -covermode=atomic -coverprofile=/tmp/c.out ./... && go tool cover -func=/tmp/c.out \| tail -1` |
| vet | `go vet ./...` |
| format check | `test -z "$(gofmt -l .)" \|\| gofmt -d .` |
| format | `gofmt -w .` |
| staticcheck | `staticcheck ./...` |
| lint install.sh | `shellcheck --shell=sh install.sh` |
| cross-build check | `for t in linux/amd64 linux/arm64 windows/amd64 darwin/amd64 darwin/arm64; do CGO_ENABLED=0 GOOS=${t%/*} GOARCH=${t#*/} go build -trimpath -ldflags "-s -w" -o /dev/null ./cmd/jjf \|\| echo "FAIL $t"; done` |

## Things to watch out for

- `gofmt -l` exits 0 even when it lists unformatted files. A gate must always use
  `test -z "$(gofmt -l .)"`
- `go test -race` requires CGO. Never pin `CGO_ENABLED=0` in the environment
- CI runs staticcheck as `go run honnef.co/go/tools/cmd/staticcheck@2026.1 ./...`
  and keeps it out of `go.mod` (`go get -tool` adds indirect dependencies and
  raises the `go` directive)
- `go run ./cmd/jjf ...` hides `jjf`'s own exit code. Use a built binary
  whenever an exit code is what you are checking
- Only the four packages that own goldens define the `-update` flag, so
  `go test ./... -update` fails with `flag provided but not defined` in the rest.
  List the packages as the table above does
- The nix dev shell pins `GOTOOLCHAIN=local`, so no go command downloads a
  toolchain beside the one nix provides. Its `staticcheck` is the same 2026.1
  release CI pins
- A binary from `nix build` has no VCS metadata to stamp, so it reports
  `v<manifest version>+nix.<rev>` rather than a tag. Release archives keep coming
  from the release workflow
- The documentation site is GitHub Pages, built by GitHub itself from the default
  branch with no workflow: `_config.yml` is the entire configuration. Two rules
  decide what is served and only one of them is written down — Jekyll also skips
  everything whose name starts with a dot, which is why a link into `.github/` or
  `.claude-plugin/` has to be an absolute URL and not a relative path.
  `pages_test.go` enforces both rules against every published document. To
  reproduce a build locally: `gem install jekyll jekyll-optional-front-matter
  jekyll-readme-index jekyll-titles-from-headings jekyll-relative-links
  jekyll-default-layout jekyll-theme-primer`, declare those under `plugins:` in a
  throwaway copy of the config (GitHub injects them), and run jekyll with
  `PAGES_REPO_NWO=shutx-net/jumping-json-flush`
- `install.sh` is POSIX sh, not bash, so lint it with `--shell=sh`. Its
  interpreter, its target list, the archive names it builds and the release tag
  it accepts are all pinned by `install_test.go` against
  `.github/workflows/release.yml`, and its options against `docs/install.md`.
  shellcheck comes from the nix shell and from the CI runner; the devcontainer
  image does not carry it
