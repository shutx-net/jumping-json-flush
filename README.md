# Jumpin' Json Flush

[日本語](README.ja.md)

[![CI](https://github.com/shutx-net/jumping-json-flush/actions/workflows/ci.yml/badge.svg?branch=main)](https://github.com/shutx-net/jumping-json-flush/actions/workflows/ci.yml)
[![Go](https://img.shields.io/github/go-mod/go-version/shutx-net/jumping-json-flush)](go.mod)

`jjf` keeps a database design in one JSON file and generates the rest: an Excel
design document, an ER diagram (an SVG it draws itself, or Graphviz DOT source),
and a PostgreSQL DDL script. The JSON is the source of truth — every generated
file is a build artifact, regenerated rather than edited.

```sh
jjf import postgres schema.sql -o db-design.json   # build it from a pg_dump file
jjf validate db-design.json                        # check it
jjf export xlsx db-design.json -o db-design.xlsx   # Excel design document
jjf export dot  db-design.json -o er.dot           # Graphviz ER diagram
jjf export svg  db-design.json -o er.svg           # ER diagram, drawn by jjf
jjf export ddl  db-design.json -o schema.sql       # PostgreSQL DDL script
```

- **No dependencies.** `go.mod` has no `require` block at all — the JSON Schema
  validator and the Excel writer are both written on the standard library. One
  static binary, no CGO, no runtime; it runs as it is on musl/alpine
- **Deterministic.** The same input always produces byte-identical output, which
  is what makes comparing artifact hashes in CI worth doing
- **Built for AI agents.** Schema validation plus an Agent Skill let an agent
  edit the design safely

## Installation

```sh
curl -fsSL https://raw.githubusercontent.com/shutx-net/jumping-json-flush/main/install.sh | sh
```

It picks the archive for your OS and CPU, verifies its sha256 against the
release's `checksums.txt`, and installs into `/usr/local/bin` when that is
writable and `$HOME/.local/bin` otherwise. It never calls `sudo`.

Three other ways: `go install github.com/shutx-net/jumping-json-flush/cmd/jjf@latest`,
`nix profile add github:shutx-net/jumping-json-flush`, and the archives at
[Releases](https://github.com/shutx-net/jumping-json-flush/releases) for
`linux/amd64`, `linux/arm64`, `windows/amd64`, `darwin/amd64` and `darwin/arm64`.

Pinning a version, Windows, CI and uninstalling are in
[`docs/install.md`](docs/install.md) ([日本語](docs/install.ja.md)).

## What each command does

- **`import`** reads a `pg_dump --schema-only` **file** — `jjf` never connects to
  a database — and validates what it built before writing it. What the format
  cannot hold, such as a `CHECK` constraint, is reported with its line number
- **`validate`** reports **every violation at once**, each with a JSON Pointer,
  against the schema embedded in the binary. It then checks the document against
  itself — a foreign key with no target, a nullable primary key column, a default
  that is not a SQL expression — as warnings; `-strict` makes them a failure
- **`export`** validates first, so a failing document produces no output at all,
  not even a single byte. `ddl` alone also refuses a document that contradicts
  itself, because SQL a database rejects is worth nothing

Exit code 2 means bad input; 3 means a JSON Schema violation and nothing else.
Every command and its options are in [`docs/usage.md`](docs/usage.md)
([日本語](docs/usage.ja.md)).

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
properties are rejected on every object.

Every field and every rule is in
[`docs/db-design-format.md`](docs/db-design-format.md)
([日本語](docs/db-design-format.ja.md)). A complete example lives in
[`examples/db-design.example.json`](examples/db-design.example.json), and the
formal definition in [`schema/db-design.schema.json`](schema/db-design.schema.json).

## Using it from an AI agent

`skills/db-design/` ships the design conventions as an
[Agent Skill](https://code.claude.com/docs/en/skills.md). It installs into Claude
Code as a plugin, with **no git, no npm and no Node**:

```text
/plugin marketplace add https://raw.githubusercontent.com/shutx-net/jumping-json-flush/main/.claude-plugin/marketplace.json
/plugin install jjf@jjf-tools
```

Invoke it with `/jjf:db-design`. The skill follows the
[Agent Skills](https://agentskills.io) specification, so
`gh skill install shutx-net/jumping-json-flush db-design --agent codex` puts the
same directory where Codex or GitHub Copilot looks for it. The other routes and
the release procedure are in [`skills/README.md`](skills/README.md).

## Wiring it into CI

Validate `db-design.json` when it changes, and keep the `.xlsx` as an artifact —
never as a commit, because it is a derived artifact. Ready-to-use examples:
[`examples/ci/github-actions.yml`](examples/ci/github-actions.yml) and
[`examples/ci/gitlab-ci.yml`](examples/ci/gitlab-ci.yml).

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

Each has a Japanese counterpart beside it, except DEVELOPERS.md, which is English
only and is linked by URL because release archives ship the READMEs and nothing
else.

## Versioning

The tool's version and the design format's version are **independent**. `jjf`
follows Semantic Versioning; a document carries a `formatVersion`
(`MAJOR.MINOR`, currently `1.0`) that rises only when the format itself changes
incompatibly. An unsupported major exits 2 and says to upgrade `jjf`.

## Out of scope

Connecting to a running database, migrations and schema diffs, judging a design
(normalization, index strategy, type choice), Mermaid and Markdown output,
rendering the `.dot` to an image, converting Excel back to JSON, editing the
Excel directly, a GUI, and customising the Excel template.

The generated DDL creates a schema from nothing. Applying a design to a database
that already has one needs to know the state that database is in, which is a
different tool; the decisions behind the format are recorded in
[`design/ddl-export.md`](design/ddl-export.md).

## License

[MIT](LICENSE)
