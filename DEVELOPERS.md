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
| regenerate goldens | `go test ./cmd/jjf/ ./internal/schema/ ./internal/sml/ ./internal/export/xlsx/ ./internal/export/dot/ ./internal/export/ddl/ ./internal/importer/postgres/ -update` |
| coverage | `go test -covermode=atomic -coverprofile=/tmp/c.out ./... && go tool cover -func=/tmp/c.out \| tail -1` |
| vet | `go vet ./...` |
| format check | `test -z "$(gofmt -l .)" \|\| gofmt -d .` |
| format | `gofmt -w .` |
| staticcheck | `staticcheck ./...` |
| lint install.sh | `shellcheck --shell=sh install.sh` |
| regenerate the pg_dump fixtures | `sh internal/importer/postgres/testdata/generate.sh` |
| regenerate one major | `PGBIN=/usr/lib/postgresql/17/bin sh internal/importer/postgres/testdata/generate.sh` |
| run the DDL round trip | `PGBIN=/usr/lib/postgresql/17/bin sh internal/export/ddl/testdata/roundtrip.sh` |
| round trip one document | `PGBIN=/usr/lib/postgresql/17/bin OUTDIR=/tmp/rt sh internal/export/ddl/testdata/roundtrip.sh edge.json` |
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
- Only the seven packages that own goldens define the `-update` flag, so
  `go test ./... -update` fails with `flag provided but not defined` in the rest.
  List the packages as the table above does
- `jjf export ddl` is the only exporter that refuses its input. A document that
  contradicts itself still makes a useful workbook and a useful diagram, so `xlsx`
  and `dot` render it; SQL a database rejects is worth nothing, so `ddl` writes
  nothing and exits **2**, not 4 — 4 has to keep meaning that the environment
  stopped the write, which is all `writeFileAtomically` produces. The asymmetry is
  pinned by `TestOnlyDDLRefusesFindings`, so deleting the format table's `accept`
  field has to argue with a test
- The DDL round trip — document → `jjf export ddl` → live PostgreSQL → `pg_dump`
  → `jjf import` → document — runs in the `verify` leg of
  `.github/workflows/pg-fixtures.yml`, one PostgreSQL major per leg, driven by
  `internal/export/ddl/testdata/roundtrip.sh`. Golden files prove only that the
  generator emits what it emitted; the database is the real oracle. The script
  starts a throwaway cluster of its own — `generate.sh` stops its server between
  majors and at the end, so there is never one left to reuse — and takes
  `full.json`, `edge.json` and `minimal.json` twice around, two empty databases
  per document. `edge.json` is applied after
  `internal/export/ddl/testdata/edge.prelude.sql`: the document names a
  user-defined type the design format has no way to define, and the documented
  remedy is that the type exists in the target database, not that the generator
  learns to emit `CREATE TYPE`. The comparison is at the document level, never at
  the SQL level — `pg_dump` writes a random token into its `\restrict` lines —
  and the gate is that the **second** pass equals the first, not that the first
  equals the input. A hand-written quoted literal picks up an explicit cast on the
  first pass (`'now'` becomes `'now'::text`) and is stable from the second
  onwards; `DEFAULT NULL` disappears, because PostgreSQL treats it as no default;
  `NO ACTION` disappears, because it is PostgreSQL's own default. That
  input-against-first-pass difference is written into the job summary on purpose
  and gated on purpose *not*: every line of it belongs to PostgreSQL rather than
  to jjf, so pinning it would turn a PostgreSQL release into a jjf CI failure with
  no jjf change to make
- `roundtrip.sh` needs `jq`, which the nix shell and the CI runner both have and
  the devcontainer image does not — it has no PostgreSQL either, so the round trip
  does not run there at all. It keeps `OUTDIR` and deletes only the cluster, so
  the diffs and the intermediate SQL survive a failure; and it writes nothing into
  the checkout, which the workflow gates on with the same `git status --porcelain`
  shape the gofmt check uses
- `internal/importer/postgres/testdata/dump/pg<major>/*.sql` is real
  `pg_dump --schema-only` output, one directory per PostgreSQL major, regenerated
  from `testdata/source/*.sql` by
  `internal/importer/postgres/testdata/generate.sh`. The importer supports
  **pg_dump 13 to 18** and every major in that range has a capture committed;
  `TestImportAgreesAcrossPgDumpMajors` holds all of them to the same goldens, which
  are built from pg16. The script starts a throwaway PostgreSQL cluster under
  `/tmp` for each major it finds under `/usr/lib/postgresql/*/bin` — `PGBIN`
  narrows it to one. The server refuses to run as root, so as root the script
  runs every server command through `su postgres`, which then has to be able to
  read `testdata/source/` and traverse the path above it; as any ordinary user it
  runs them directly and the question never comes up, which is what CI does. The
  majors beside the distribution's own come from the PGDG repository at
  apt.postgresql.org; the script header carries the commands.
  The clusters are deleted afterwards and never committed. Four lines of a
  regenerated dump never match the committed one: the `\restrict` /
  `\unrestrict` lines, where pg_dump puts a random token, and the two
  `-- Dumped ... version` banners, which move with every PostgreSQL minor release
  and with the packaging. Nothing else does — everything below those four lines
  is the schema
- `.github/workflows/pg-fixtures.yml` is where everything about jjf that needs a
  real PostgreSQL server lives: the other half of that script, and the DDL round
  trip above. Nothing in `go test` starts a database, so nothing would notice a
  `pg_dump` that stopped writing what was captured; this workflow installs one
  PostgreSQL major per matrix leg, runs `generate.sh` unchanged against a live
  server, and holds the regenerated dumps to the committed goldens with the
  importer's own test suite. It runs weekly, on pull requests that touch
  `internal/importer/postgres/**` or `internal/export/ddl/**`, and on demand with
  `gh workflow run pg-fixtures.yml`. It never commits what it regenerates: the
  dumps leave the runner as the `dump-pg<major>` artifact and the round trip's
  working files as `roundtrip-pg<major>`, and nothing else. Four things turn it
  red, and the job summary says which. A dump no longer imports to its golden — a
  real divergence, and the summary carries the diff of the document, not of the
  SQL. Or the dump text changed in something other than those four lines —
  `pg_dump` writes something new and the committed capture is stale. Both are
  fixed by regenerating locally and committing the result, re-running the package
  with `-update` only when the goldens should move with it; the pg16 leg is the
  one that can also fail on `edge.warnings.txt`, because those warnings carry line
  numbers and the goldens are built from pg16. Or the second pass of the DDL round
  trip is not the first, or the generated DDL did not apply at all — read the
  `roundtrip-pg<major>` artifact, which holds both passes' DDL, both dumps, both
  documents, the warnings and the diffs; the failure is either the generator and
  the importer disagreeing with each other or PostgreSQL reshaping something one
  of them then reads differently, and both of those are real. That round trip is
  guarded on the PostgreSQL install and not on the regeneration, so it still
  answers when `generate.sh` fails, and the command table above reproduces it
  locally. Or `upstream majors` failed, the job that notices a PostgreSQL major
  newer than anything captured here — it writes the steps it wants into its own
  log and summary, and stays red every week until the repository says something
  about that major either way. Do not make any of these jobs a required status
  check: they are path filtered, and a required check that never runs blocks the
  merge for ever. GitHub also disables a scheduled workflow after 60 days without
  a commit to the repository, so a long quiet stretch ends with this one silently
  switched off
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
  a raw YAML table on github.com. `internal/repo/pages_test.go` keeps the map and
  `docs/` in agreement both ways, and checks the absolute github.com links the site
  forces on any document that has to point outside `docs/`
- A relative link that leaves `docs/` cannot be served by the site. Write those as
  `https://github.com/shutx-net/jumping-json-flush/blob/main/<path>`; the build
  runs with `--strict` and fails on anything else
- To work on the site: `pip install mkdocs-material==9.7.7`, then `mkdocs serve`
  for a live preview or `mkdocs build --strict` for the check the workflow runs
- `install.sh` is POSIX sh, not bash, so lint it with `--shell=sh`. Its
  interpreter, its target list, the archive names it builds and the release tag
  it accepts are all pinned by `internal/repo/install_test.go` against
  `.github/workflows/release.yml`, and its options against `docs/install.md`.
  shellcheck comes from the nix shell and from the CI runner; the devcontainer
  image does not carry it
- `design/` holds decision records: the choices an implementation is bound by,
  and what they commit the project to, settled before the code exists so that
  they are decided deliberately rather than discovered. It is deliberately outside `docs/`, which is the published site — a page
  there describing a feature the tool does not have would mislead its readers,
  and `mkdocs build --strict` rejects a file that is not in the `nav` map anyway
