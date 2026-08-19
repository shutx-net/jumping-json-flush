# Jumpin' Json Flush

[日本語](README.ja.md)

**Jumpin' Json Flush** (`jjf`) is a CLI tool that keeps database design
information in structured JSON as the single source of truth and turns it into an
Excel design document people can read.

- **JSON is the only source of truth.** A generated `.xlsx` is a derived artifact
  and is never treated as authoritative data
- **Deterministic output.** The same input always produces a byte-identical `.xlsx`
- **A single binary.** No CGO, no runtime dependencies. It runs as it is on
  musl/alpine
- **Built for AI agents.** Structural validation through JSON Schema and an Agent
  Skill let an agent edit the design JSON safely. The skill is distributed as a
  Claude Code plugin (`/plugin install jjf@jjf-tools`)

```sh
jjf validate db-design.json
jjf export xlsx db-design.json -o db-design.xlsx
```

## Installation

### Release binaries

Grab the archive for your OS and CPU from
[Releases](https://github.com/shutx-net/jumping-json-flush/releases). Five targets are
published: `linux/amd64`, `linux/arm64`, `windows/amd64`, `darwin/amd64` and
`darwin/arm64`.

```sh
VERSION=v0.1.0
curl -sSfL -o jjf.tar.gz \
  "https://github.com/shutx-net/jumping-json-flush/releases/download/${VERSION}/jjf_${VERSION}_linux_amd64.tar.gz"
tar xzf jjf.tar.gz
sudo install -m 0755 "jjf_${VERSION}_linux_amd64/jjf" /usr/local/bin/jjf
jjf version
```

Every release ships a `checksums.txt` (sha256).

```sh
curl -sSfL -O "https://github.com/shutx-net/jumping-json-flush/releases/download/${VERSION}/checksums.txt"
sha256sum --check --ignore-missing checksums.txt
```

### go install

```sh
go install github.com/shutx-net/jumping-json-flush/cmd/jjf@latest
```

Pin a version by naming its tag, as in `@v0.1.0`. Go 1.24 or later is required.

## Usage

### validate

```sh
jjf validate db-design.json
```

Checks a database design JSON against the built-in JSON Schema (Draft 2020-12).
**Every violation is reported at once**, each one pointing at its location with a
JSON Pointer.

```text
db-design.json: does not conform to the jjf database design schema
  /database/dbms                   value must be one of 'PostgreSQL', 'MySQL', 'MariaDB', 'SQLite', 'Oracle', 'SQLServer'
  /tables/0                        missing property 'logicalName'
  /tables/0/columns/0              missing property 'nullable'
  /tables/0/columns/1/logicalName  minLength: got 0, want 1
  /tables/0/columns/1/name         '9bad' does not match pattern '^[A-Za-z_][A-Za-z0-9_]*$'
  /tables/0/columns/1/nullable     got string, want boolean
  /tables/0/indexes/0              missing property 'name'

7 error(s). See schema/db-design.schema.json.
```

Validation touches no network. The schema is embedded in the binary, so a
`$schema` written in the document never causes a fetch.

### export

```sh
jjf export xlsx db-design.json -o db-design.xlsx
```

- The input is always validated first. **A document that fails validation
  produces no output file at all, not even a single byte**
- Leave `-o` out and the output goes **next to the input, with the extension
  replaced by `.xlsx`** (`docs/db-design.json` → `docs/db-design.xlsx`)
- `-o -` writes to standard output, but it is **refused when standard output is a
  terminal** (a binary would only garble the screen). A pipe or a redirect is fine
- The workbook is written to a temporary file and renamed into place, so a failure
  part way through never leaves a corrupt file behind
- `xlsx` is the only format Phase 1 supports

```sh
# into a pipe
jjf export xlsx db-design.json -o - | sha256sum

# writing straight to the terminal is refused (exit code 2)
jjf export xlsx db-design.json -o -
# jjf: refusing to write a workbook to the terminal; redirect standard output or pass -o <file>
```

#### Byte-for-byte determinism

**The same input always produces a byte-identical `.xlsx`.** No generation
timestamp is embedded, the ZIP timestamps are fixed, and nothing depends on Go's
map iteration order.

```sh
jjf export xlsx db-design.json -o a.xlsx
jjf export xlsx db-design.json -o b.xlsx
sha256sum a.xlsx b.xlsx   # the two hashes are identical
```

That makes it possible to compare artifact hashes in CI, and to treat "the design
document changed although the JSON did not" as the anomaly it is.

### version

```sh
jjf version
# jjf v0.1.0
# built with go1.24.0 for linux/amd64
```

A release binary reports its tag name; one installed with `go install` reports the
module version Go recorded.

### Exit codes

| Code | Meaning | Typical cause |
| --- | --- | --- |
| 0 | success | — |
| 1 | general error | an internal error that fits none of the other categories |
| 2 | invalid input | wrong arguments, missing file, JSON syntax error, unsupported `formatVersion`, unknown output format, `-o -` pointed at a terminal |
| 3 | schema validation error | a JSON Schema violation |
| 4 | output generation error | the destination cannot be written, the directory does not exist |

What matters in CI is being able to **tell 3 from 2**. A 3 is a problem with the
contents of the design JSON; a 2 is a problem with how the tool was called, where
the file is, or which version of `jjf` is installed.

Success messages go to standard output; errors and usage go to standard error.

## The database design JSON format

A complete example lives in
[`examples/db-design.example.json`](examples/db-design.example.json), and the
formal definition of the structure in
[`schema/db-design.schema.json`](schema/db-design.schema.json).

```json
{
  "$schema": "https://raw.githubusercontent.com/shutx-net/jumping-json-flush/main/schema/db-design.schema.json",
  "formatVersion": "1.0",
  "database": {
    "name": "ec_shop",
    "logicalName": "ECサイト",
    "dbms": "PostgreSQL"
  },
  "tables": [
    {
      "name": "users",
      "logicalName": "会員",
      "columns": [
        {
          "name": "id",
          "logicalName": "会員ID",
          "type": "BIGINT",
          "nullable": false,
          "autoIncrement": true
        },
        {
          "name": "email",
          "logicalName": "メールアドレス",
          "type": "VARCHAR",
          "length": 255,
          "nullable": false
        }
      ],
      "primaryKey": { "name": "pk_users", "columns": ["id"] },
      "uniqueKeys": [{ "name": "uq_users_email", "columns": ["email"] }]
    }
  ]
}
```

The essentials:

| Item | Rule |
| --- | --- |
| Encoding | UTF-8 (a BOM is accepted). LF line endings are recommended |
| Required at the root | `formatVersion`, `database`, `tables` |
| Required per table | `name`, `logicalName`, `columns` |
| Required per column | `name`, `logicalName`, `type`, `nullable` |
| Unknown properties | **rejected** on every object (`additionalProperties: false`) |
| Physical names | `^[A-Za-z_][A-Za-z0-9_]*$`, at most 128 characters. Japanese belongs in `logicalName` |
| Type names | Parameters may not be inlined as in `VARCHAR(30)`. Split them into `type: "VARCHAR"` plus `length: 30` |
| Defaults | `default` is a string only. A SQL literal includes its quotes (`"'pending'"`). No DEFAULT clause means the key is simply absent |
| Enums | `dbms` has 6 values; `onUpdate` / `onDelete` have 5 (`CASCADE`, `RESTRICT`, `SET NULL`, `SET DEFAULT`, `NO ACTION`) |

Writing `$schema` at the root gives you completion and warnings in editors such as
VS Code. `jjf` itself never reads the value.

**Validation today is structural only.** Semantic consistency is **not** checked:
whether a foreign key's target exists, whether table or column names are
duplicated, whether the columns named by a primary key or an index exist.

### What the generated workbook contains

| Sheet | Contents |
| --- | --- |
| 表紙 (cover) | Database name, logical name, DBMS, table count, format version, description |
| テーブル一覧 (table list) | Physical name, logical name, description, column count and sheet name of every table |
| テーブル定義 (table definition) | One sheet per table: the column definitions, then a block each for the primary key, unique keys, foreign keys and indexes |

Notation:

- In the `NULL` and `自動採番` (auto increment) columns, `○` means yes and an empty
  cell means no
- The `長さ` (size) column holds one of `length`, `precision` or
  `precision,scale`. It stays empty for a type that declares no size
- Sheet names are truncated to 31 characters (Excel's limit), and a collision gets
  a `(2)`, `(3)`, … suffix. The table list sheet prints **the name actually
  allocated**, so truncation and numbering are visible
- Layout and colours are fixed inside `jjf` and cannot be controlled from the
  JSON

## Using it from an AI agent

`skills/db-design/` ships the database design conventions and the working rules
for an AI agent in [Agent Skills](https://code.claude.com/docs/en/skills.md)
form. The skill itself is written in English, but its trigger keywords include
Japanese, so a request written in Japanese loads it too.

Install it into Claude Code as a
[plugin](https://code.claude.com/docs/en/plugins-reference.md). **You need no git,
no npm and no Node**: the zip archive is fetched over HTTPS (the `archive` source
requires Claude Code v2.1.224 or later).

```text
/plugin marketplace add https://raw.githubusercontent.com/shutx-net/jumping-json-flush/main/.claude-plugin/marketplace.json
/plugin install jjf@jjf-tools
```

Once installed, invoke it with `/jjf:db-design`. The marketplace catalog and
the plugin manifest live in
[`.claude-plugin/`](.claude-plugin/marketplace.json), and the archive is published
with each release as `jjf-plugin-<tag>.zip`.

For installing without the plugin — copying the directory into `.claude/skills/` —
and for the release procedure, see [`skills/README.md`](skills/README.md). What
the skill covers:

- Hard rules such as never editing the Excel directly, and making every change
  against the JSON
- The workflow: change, run `jjf validate`, and only call it done once that
  succeeds
- A quick reference of the required properties and every permitted enum value
- The type names recommended per DBMS
- A table mapping the real validation error messages to their fixes
- Editing recipes for adding a table, changing a column, adding a foreign key and
  so on
- The notation of the generated Excel, and what the JSON cannot control

Because the skill is written in English, a human-facing document carrying **the
same content in Japanese** for reading and reviewing is available at
[`docs/db-design-guide.ja.md`](docs/db-design-guide.ja.md). It gathers the
material above into a single guide that reads front to back. What the agent reads
is the English skill, and the English skill wins whenever the two disagree.

## Wiring it into CI

The recommended setup detects changes to `db-design.json`, validates them, fails
the pipeline when validation fails, and keeps the `.xlsx` as an artifact when it
succeeds. Do not commit the generated `.xlsx` — it is a derived artifact, so put
it in `.gitignore`.

Ready-to-use workflow examples:

- GitHub Actions: [`examples/ci/github-actions.yml`](examples/ci/github-actions.yml)
- GitLab CI: [`examples/ci/gitlab-ci.yml`](examples/ci/gitlab-ci.yml)

## Dependencies

**One direct dependency, plus one indirect dependency that cannot be avoided.**

| Module | Kind | Purpose |
| --- | --- | --- |
| `github.com/santhosh-tekuri/jsonschema/v6` | direct | JSON Schema Draft 2020-12 validation |
| `golang.org/x/text` | indirect | unavoidable, because `jsonschema/v6` exposes `ErrorKind.LocalizedString(*message.Printer)` in its public API |

Those two are also the only dependencies recorded in the final binary, which
`go version -m jjf` will confirm. The Excel output is written by hand on top of
`archive/zip` and `encoding/xml`; no third-party Excel library is involved. There
are no runtime dependencies, and the binary is distributed statically linked with
`CGO_ENABLED=0`.

## Development

Go is provided inside the container only. Run it through the
[devcontainer CLI](https://github.com/devcontainers/cli).

```sh
devcontainer up   --workspace-folder .
devcontainer exec --workspace-folder . bash -lc '<CMD>'
```

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
| cross-build check | `for t in linux/amd64 linux/arm64 windows/amd64 darwin/amd64 darwin/arm64; do CGO_ENABLED=0 GOOS=${t%/*} GOARCH=${t#*/} go build -trimpath -ldflags "-s -w" -o /dev/null ./cmd/jjf \|\| echo "FAIL $t"; done` |

Things to watch out for:

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

### Repository layout

```text
cmd/jjf/               CLI (subcommand dispatch, argument parsing, exit codes)
internal/exitcode/     exit codes and error wrapping
internal/model/        Go types matching the design JSON one to one, and decoding
internal/schema/       compiling the embedded schema and formatting its errors
internal/sml/          generic SpreadsheetML / OPC writer (knows nothing about database design)
internal/export/xlsx/  the design document renderer (sole owner of the layout)
schema/                [authoritative] the design JSON Schema and its go:embed declaration
skills/db-design/      [authoritative] the Agent Skill for AI agents
.claude-plugin/        the plugin manifest and the marketplace catalog
examples/              a sample design JSON and CI workflow examples
docs/                  human-facing Japanese documentation (the database design guide)
```

## Versioning

The tool's version and the database design format's version are **independent**.

- `jjf` itself follows Semantic Versioning (tags such as `v0.1.0`)
- A design document carries a `formatVersion` (`MAJOR.MINOR`), currently `1.0`
- `formatVersion` is raised **only when the format itself changes incompatibly**
- Feeding in an unsupported major version exits with code 2 and a dedicated
  message (`unsupported formatVersion "2.0"; this jjf supports 1.x - please upgrade jjf`)

## Out of scope

Connecting to a database or importing a schema from an existing one, DDL
generation, ER diagram / Mermaid output, Markdown output, semantic consistency
validation, migration management, converting Excel back into JSON, editing the
Excel directly, a GUI, and customising the Excel template are all out of scope.

## License

[MIT](LICENSE)
