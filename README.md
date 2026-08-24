# Jumpin' Json Flush

[日本語](README.ja.md)

[![CI](https://github.com/shutx-net/jumping-json-flush/actions/workflows/ci.yml/badge.svg?branch=main)](https://github.com/shutx-net/jumping-json-flush/actions/workflows/ci.yml)
[![Go](https://img.shields.io/github/go-mod/go-version/shutx-net/jumping-json-flush)](go.mod)

**Jumpin' Json Flush** (`jjf`) is a CLI tool that keeps database design
information in structured JSON as the single source of truth and turns it into
design documents people can read: an Excel workbook, a Graphviz ER diagram, and
a PostgreSQL DDL script.

- **JSON is the only source of truth.** A generated `.xlsx`, `.dot` or `.sql` is a
  derived artifact and is never treated as authoritative data
- **Deterministic output.** The same input always produces a byte-identical
  `.xlsx`, `.dot` and `.sql`
- **A single binary.** No CGO, no runtime dependencies. It runs as it is on
  musl/alpine
- **Built for AI agents.** Structural validation through JSON Schema and an Agent
  Skill let an agent edit the design JSON safely. The skill is distributed as a
  Claude Code plugin (`/plugin install jjf@jjf-tools`) and, following the Agent
  Skills specification, installs into Codex and GitHub Copilot as well

```sh
jjf import postgres schema.sql -o db-design.json
jjf validate db-design.json
jjf export xlsx db-design.json -o db-design.xlsx
jjf export dot db-design.json -o er.dot
jjf export ddl db-design.json -o schema.sql
```

## Installation

```sh
curl -fsSL https://raw.githubusercontent.com/shutx-net/jumping-json-flush/main/install.sh | sh
```

The script picks the archive for your OS and CPU, **verifies its sha256 against
the release's `checksums.txt`**, and installs into `/usr/local/bin` when that is
writable and `$HOME/.local/bin` otherwise. It never calls `sudo`, and it prints
where the binary went.

Three other ways:

- `go install github.com/shutx-net/jumping-json-flush/cmd/jjf@latest`
- `nix profile add github:shutx-net/jumping-json-flush`
- the archives at
  [Releases](https://github.com/shutx-net/jumping-json-flush/releases), for
  `linux/amd64`, `linux/arm64`, `windows/amd64`, `darwin/amd64` and
  `darwin/arm64`, each with a `checksums.txt`

Pinning a version, choosing the directory, Windows, CI, verifying a download by
hand and uninstalling are all in [`docs/install.md`](docs/install.md)
([日本語](docs/install.ja.md)).

## Usage

```sh
# build a design document from a PostgreSQL schema dump
pg_dump --schema-only mydb > schema.sql
jjf import postgres schema.sql -o db-design.json

# check a design document against the built-in JSON Schema
jjf validate db-design.json

# turn it into an Excel design document
jjf export xlsx db-design.json -o db-design.xlsx

# turn it into a Graphviz ER diagram
jjf export dot db-design.json -o er.dot

# turn it into a PostgreSQL DDL script
jjf export ddl db-design.json -o schema.sql
```

An import reads a `pg_dump --schema-only` **file** — `jjf` never connects to a
database — and validates what it produced before writing it, so an import cannot
leave behind a document that `jjf validate` would reject. Anything the design
format cannot hold, such as a `CHECK` constraint or a partial index, is reported
on standard error with the line it was on, and the surrounding table is still
imported.

Validation reports **every violation at once**, each pointing at its location with
a JSON Pointer, and touches no network: the schema is embedded in the binary. An
export validates first, so a document that fails produces no output file at all,
not even a single byte. **The same input always produces a byte-identical
`.xlsx`**, which is what makes comparing artifact hashes in CI worth doing.

`ddl` goes one step further and refuses a document `jjf validate` would only warn
about, along with the PostgreSQL-specific mistakes that command deliberately does
not report. A document that contradicts itself still makes a useful workbook and
a useful diagram, so `xlsx` and `dot` render it; SQL a database rejects is worth
nothing, so `ddl` writes nothing and exits 2.

`jjf validate` then checks the document **against itself**: that the columns named
by its keys and indexes exist, that every foreign key names a table this document
defines, matches it column for column and targets columns that table constrains
to be unique, that no primary key column is declared nullable, that one table
never uses the same column or constraint name twice, and that no column declares
a default that is empty or does not read as a SQL expression. Those findings are warnings
on standard error and leave the exit code successful, so a document that passes
today keeps passing; `-strict` turns them into a failure.

Every command and its options, the rules for `-o`, and the exit codes a
pipeline reads — 2 for bad input, 3 for a schema violation — are in
[`docs/usage.md`](docs/usage.md) ([日本語](docs/usage.ja.md)). Code 3 means JSON
Schema conformance and nothing else: a referential finding is not a schema
violation, so `validate -strict` reports one as 2.

## The database design JSON

```json
{
  "formatVersion": "1.0",
  "database": { "name": "ec_shop", "logicalName": "ECサイト", "dbms": "PostgreSQL" },
  "tables": [
    {
      "name": "users",
      "logicalName": "会員",
      "columns": [
        { "name": "id", "logicalName": "会員ID", "type": "BIGINT", "nullable": false },
        { "name": "email", "logicalName": "メールアドレス", "type": "VARCHAR",
          "length": 255, "nullable": false }
      ],
      "primaryKey": { "name": "pk_users", "columns": ["id"] }
    }
  ]
}
```

Physical names stay ASCII and Japanese belongs in `logicalName`. Unknown
properties are rejected on every object. A complete example lives in
[`examples/db-design.example.json`](examples/db-design.example.json), and the
formal definition of the structure in
[`schema/db-design.schema.json`](schema/db-design.schema.json).

Every field, every rule, and the three sheets of the generated workbook are in
[`docs/db-design-format.md`](docs/db-design-format.md)
([日本語](docs/db-design-format.ja.md)).

## Using it from an AI agent

`skills/db-design/` ships the design conventions and the working rules for an AI
agent in [Agent Skills](https://code.claude.com/docs/en/skills.md) form: the hard
rules, the change-then-validate workflow, the permitted enum values, a table
mapping real error messages to their fixes, and editing recipes. It installs into
Claude Code as a plugin, with **no git, no npm and no Node**.

```text
/plugin marketplace add https://raw.githubusercontent.com/shutx-net/jumping-json-flush/main/.claude-plugin/marketplace.json
/plugin install jjf@jjf-tools
```

Invoke it with `/jjf:db-design`. It is not tied to Claude Code: the skill follows
the [Agent Skills](https://agentskills.io) specification and uses nothing outside
it, so `gh skill install shutx-net/jumping-json-flush db-design --agent codex`
puts the same directory where Codex, GitHub Copilot or another conforming host
looks for it. Those routes, installing without the plugin, and the release
procedure are in [`skills/README.md`](skills/README.md). The skill is written in
English; the same material in Japanese, for a person to read rather than an agent
to execute, is [`docs/db-design-guide.ja.md`](docs/db-design-guide.ja.md).

## Wiring it into CI

Detect changes to `db-design.json`, validate them, and keep the `.xlsx` as an
artifact — never as a commit, because it is a derived artifact. Ready-to-use
examples: [`examples/ci/github-actions.yml`](examples/ci/github-actions.yml) and
[`examples/ci/gitlab-ci.yml`](examples/ci/gitlab-ci.yml).

## Dependencies

**None.** `go.mod` has no `require` block at all: everything `jjf` does, it does
on the Go standard library.

That includes JSON Schema validation. `internal/schema` carries a validator
written for one schema rather than a general implementation of the
specification: the keywords `schema/db-design.schema.json` actually uses, and no
others. A keyword it does not implement cannot be added to the schema unnoticed,
because the schema is decoded into Go types that refuse an unknown key and `jjf`
fails to start; and the schema itself is held to the JSON Schema Draft 2020-12
meta-schema by CI, with a tool run from its module path so that it never becomes
a dependency either.

The Excel output is written by hand on top of `archive/zip` and `encoding/xml`;
no third-party Excel library is involved. `go version -m jjf` will confirm that
the binary records no dependencies at all.

## Documentation

Published as a site as well: **<https://shutx-net.github.io/jumping-json-flush/>**

| | |
| --- | --- |
| [`docs/install.md`](docs/install.md) | installing, pinning a version, verifying a download |
| [`docs/usage.md`](docs/usage.md) | the commands, their options and the exit codes |
| [`docs/db-design-format.md`](docs/db-design-format.md) | every field of the design JSON, and the workbook it produces |
| [`docs/db-design-guide.ja.md`](docs/db-design-guide.ja.md) | the same conventions as the skill, in Japanese, for a person to read |
| [`skills/README.md`](skills/README.md) | the Agent Skill and how it is distributed |
| [DEVELOPERS.md](https://github.com/shutx-net/jumping-json-flush/blob/main/DEVELOPERS.md) | setting up an environment and the command table |

Each of those has a Japanese counterpart beside it, except DEVELOPERS.md, which is
English only and is linked by URL rather than by path because release archives
ship the READMEs and nothing else.

## Versioning

The tool's version and the database design format's version are **independent**.

- `jjf` itself follows Semantic Versioning (tags such as `v0.1.0`)
- A design document carries a `formatVersion` (`MAJOR.MINOR`), currently `1.0`
- `formatVersion` is raised **only when the format itself changes incompatibly**
- Feeding in an unsupported major version exits with code 2 and a dedicated
  message (`unsupported formatVersion "2.0"; this jjf supports 1.x - please upgrade jjf`)

## Out of scope

Connecting to a running database, Mermaid output, Markdown output, judging a
database design (normalization, index strategy, type choice), migration
management, converting Excel back into JSON, editing the Excel directly, a GUI,
and customising the Excel template are all out of scope.

A PostgreSQL DDL script is generated with `jjf export ddl`; see
[`docs/usage.md`](docs/usage.md#export). It creates a schema from nothing, and
the decisions behind its format are recorded in
[`design/ddl-export.md`](design/ddl-export.md). Applying a design to a database
that already has one stays out of scope, because that needs to know the state
that database is in.

An entity relationship diagram is generated as Graphviz DOT source (`jjf export
dot`); see [`docs/usage.md`](docs/usage.md#export). Rendering it to an image is
not: that is the reader's own `dot`, which is what keeps `jjf` a single binary
with no runtime dependencies.

Importing a schema **from a `pg_dump --schema-only` file** is supported, for
PostgreSQL only; see [`docs/usage.md`](docs/usage.md#import). Reading the schema
out of a live server is not.

## License

[MIT](LICENSE)
