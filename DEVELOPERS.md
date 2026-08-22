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
| regenerate goldens | `go test ./cmd/jjf/ ./internal/schema/ ./internal/sml/ ./internal/export/xlsx/ ./internal/export/dot/ ./internal/importer/postgres/ -update` |
| coverage | `go test -covermode=atomic -coverprofile=/tmp/c.out ./... && go tool cover -func=/tmp/c.out \| tail -1` |
| vet | `go vet ./...` |
| format check | `test -z "$(gofmt -l .)" \|\| gofmt -d .` |
| format | `gofmt -w .` |
| staticcheck | `staticcheck ./...` |
| lint install.sh | `shellcheck --shell=sh install.sh` |
| regenerate the pg_dump fixtures | `sh internal/importer/postgres/testdata/generate.sh` |
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
- Only the six packages that own goldens define the `-update` flag, so
  `go test ./... -update` fails with `flag provided but not defined` in the rest.
  List the packages as the table above does
- `internal/importer/postgres/testdata/dump/pg<major>/*.sql` is real
  `pg_dump --schema-only` output, one directory per PostgreSQL major, regenerated
  from `testdata/source/*.sql` by
  `internal/importer/postgres/testdata/generate.sh`. The importer supports
  **pg_dump 13 to 18** and every major in that range has a capture committed;
  `TestImportAgreesAcrossPgDumpMajors` holds all of them to the same goldens, which
  are built from pg16. The script starts a throwaway PostgreSQL cluster under
  `/tmp` for each major it finds under `/usr/lib/postgresql/*/bin` — `PGBIN`
  narrows it to one — and needs root or the `postgres` user, because the server
  refuses to run as root. The majors beside the distribution's own come from the
  PGDG repository at apt.postgresql.org; the script header carries the commands.
  The clusters are deleted afterwards and never committed. A regenerated dump
  always differs in the `\restrict` / `\unrestrict` line: pg_dump puts a random
  token there
- `internal/importer/postgres/testdata/dump/synthetic/*.sql` is hand-written, says
  so in its own headers, and is not produced by the script. It covers dump shapes
  no installed pg_dump writes any more — unqualified names, `WITH (oids = false)` —
  and one deliberately broken file
- `go.mod` pins the toolchain in two directives that do different jobs. `go 1.26`
  is the floor: a go command older than that refuses to build, which is where the
  enforcement lives. `toolchain go1.26.7` is the exact release the go command
  fetches and runs when the local one is older, downloaded as a module and
  verified through the checksum database like any other. Set them with
  `go mod edit -go=1.26 -toolchain=go1.26.7` rather than by hand; plain
  `go get go@1.26` resolves to the newest patch and collapses both into a single
  `go 1.26.7` line, which would make the flake assertion below demand that exact
  patch from nixpkgs
- `GOTOOLCHAIN=local` ignores the toolchain directive and runs whatever is
  installed, so a build under it is pinned by the `go` line alone. That is the
  case in the nix dev shell, which pins `GOTOOLCHAIN=local` so no go command
  downloads a toolchain beside the one nix provides: a nix build uses nixpkgs'
  Go, pinned by `flake.lock`, and `flake.nix` asserts only that it satisfies the
  `go` directive. Its `staticcheck` is the same 2026.1 release CI pins
- A binary from `nix build` has no VCS metadata to stamp, so it reports
  `v<manifest version>+nix.<rev>` rather than a tag. Release archives keep coming
  from the release workflow
- The documentation site is `docs/`, built with MkDocs Material and deployed by
  `.github/workflows/pages.yml`; the Pages source is "GitHub Actions" and not a
  branch. `mkdocs.yml` carries the page map by hand, because every generator that
  derives a sidebar automatically wants per-page front matter and that renders as
  a raw YAML table on github.com. `pages_test.go` keeps the map and `docs/` in
  agreement both ways, and checks the absolute github.com links the site forces on
  any document that has to point outside `docs/`
- A relative link that leaves `docs/` cannot be served by the site. Write those as
  `https://github.com/shutx-net/jumping-json-flush/blob/main/<path>`; the build
  runs with `--strict` and fails on anything else
- To work on the site: `pip install mkdocs-material==9.7.7`, then `mkdocs serve`
  for a live preview or `mkdocs build --strict` for the check the workflow runs
- `install.sh` is POSIX sh, not bash, so lint it with `--shell=sh`. Its
  interpreter, its target list, the archive names it builds and the release tag
  it accepts are all pinned by `install_test.go` against
  `.github/workflows/release.yml`, and its options against `docs/install.md`.
  shellcheck comes from the nix shell and from the CI runner; the devcontainer
  image does not carry it
- `design/` holds decision records for work that is not adopted: what an
  implementation would commit the project to, and the choices it would be bound
  by. It is deliberately outside `docs/`, which is the published site — a page
  there describing a feature the tool does not have would mislead its readers,
  and `mkdocs build --strict` rejects a file that is not in the `nav` map anyway
