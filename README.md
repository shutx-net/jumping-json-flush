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
# check a design document against the built-in JSON Schema
jjf validate db-design.json

# turn it into an Excel design document
jjf export xlsx db-design.json -o db-design.xlsx
```

Validation reports **every violation at once**, each pointing at its location with
a JSON Pointer, and touches no network: the schema is embedded in the binary. An
export validates first, so a document that fails produces no output file at all,
not even a single byte. **The same input always produces a byte-identical
`.xlsx`**, which is what makes comparing artifact hashes in CI worth doing.

The two commands and their options, the rules for `-o`, and the exit codes a
pipeline reads — 2 for bad input, 3 for a schema violation — are in
[`docs/usage.md`](docs/usage.md) ([日本語](docs/usage.ja.md)).

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

Invoke it with `/jjf:db-design`. Installing it without the plugin and the release
procedure are in [`skills/README.md`](skills/README.md). The skill is written in
English; the same material in Japanese, for a person to read rather than an agent
to execute, is [`docs/db-design-guide.ja.md`](docs/db-design-guide.ja.md).

## Wiring it into CI

Detect changes to `db-design.json`, validate them, and keep the `.xlsx` as an
artifact — never as a commit, because it is a derived artifact. Ready-to-use
examples: [`examples/ci/github-actions.yml`](examples/ci/github-actions.yml) and
[`examples/ci/gitlab-ci.yml`](examples/ci/gitlab-ci.yml).

## Dependencies

**One direct dependency, plus one indirect dependency that cannot be avoided.**

| Module | Kind | Purpose |
| --- | --- | --- |
| `github.com/santhosh-tekuri/jsonschema/v6` | direct | JSON Schema Draft 2020-12 validation |
| `golang.org/x/text` | indirect | unavoidable: `jsonschema/v6` exposes it in its public API |

Those two are also the only dependencies recorded in the binary, which
`go version -m jjf` will confirm. The Excel output is written by hand on top of
`archive/zip` and `encoding/xml`; no third-party Excel library is involved.

## Documentation

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

Connecting to a database or importing a schema from an existing one, DDL
generation, ER diagram / Mermaid output, Markdown output, semantic consistency
validation, migration management, converting Excel back into JSON, editing the
Excel directly, a GUI, and customising the Excel template are all out of scope.

## License

[MIT](LICENSE)
