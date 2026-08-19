---
name: db-design
description: Edits and validates jjf database design JSON, then regenerates the Excel design document from it. The JSON is the single source of truth — never edit the generated .xlsx; change the JSON and export again. Covers table definitions, column definitions, data types, nullability, defaults, primary keys, foreign keys, indexes, JSON Schema validation with jjf validate, and Excel export with jjf export xlsx. Use when a repository holds a db-design.json, when asked to add or change a table, column, index, primary key or foreign key, when asked to update a database design document or DB schema, or when jjf validate fails. 日本語の依頼でも使う - DB設計、データベース設計書、テーブル定義、カラム定義、外部キー、インデックス、スキーマ検証、Excel出力。
license: MIT
compatibility: Requires the jjf CLI on PATH. Uses only portable Agent Skills frontmatter fields, so it also works with claude.ai skill upload and the Anthropic Agent SDK.
allowed-tools:
  - Read
  - Bash(jjf validate:*)
  - Bash(jjf export:*)
metadata:
  project: jjf
  language: en
  jjfFormatVersion: "1.0"
---

# jjf Database Design Skill

## Overview

`jjf` keeps a database design in a JSON document — called `db-design.json`
below — and renders it as an Excel workbook. The JSON is the single source of
truth. The `.xlsx` is a derived artifact: it is rebuilt from scratch on every
export and is byte-identical for identical input.

Every design change is therefore a JSON change. "Update the Excel design
document" means edit the JSON and run `jjf export xlsx` again. A generated
workbook is never an input — never read one to recover a design, and when a
workbook and the JSON disagree, the JSON is right.

This file is not the structure specification. Structure is defined by
`schema/db-design.schema.json` (JSON Schema Draft 2020-12), embedded in the
`jjf` binary. What follows is the procedure: what to do, in what order, and
how to recover when validation fails.

## When to use this skill

- The repository holds a `db-design.json` (or `*.jjf.json`, `docs/db-design.json`, ...)
- Add a table; add, change or remove a column; change a type; add an index or a key
- Update the database design document, the DB schema, or the Excel design document
- `jjf validate` failed and the document has to be fixed
- Author a new jjf database design document from scratch

## Authoritative sources

| Source | Authority |
| --- | --- |
| `schema/db-design.schema.json` | **Structure.** Required properties, types, enums and patterns are decided here and nowhere else |
| This skill and its references | **Procedure.** Conventions, editing order, failure recovery |
| The existing `db-design.json` | **Current state.** Match its naming, granularity and ordering |
| The user's request | **Intent.** Ask instead of guessing when it is ambiguous |

A generated `.xlsx` is **not** a source. Never read one as input.

## Hard rules

1. Never edit an `.xlsx`. Change the JSON and export again.
2. Never add a property the schema does not define. Every object is `additionalProperties: false`,
   so `comment`, `engine` and the like fail validation. Put such information in `description`.
3. Never write the same property twice in one object. Duplicate JSON keys pass
   both `jjf` and JSON Schema silently and the last one wins, so re-read every
   object you edited.
4. Never put parameters in `type`. Write `"type": "VARCHAR"` with `"length": 30`,
   never `"VARCHAR(30)"` — the pattern rejects parentheses.
5. Write SQL string literals in `default` with their quotes included, as in
   `"default": "'pending'"`. Omit the `default` key entirely when the column has
   no DEFAULT clause; never write `"default": ""` to mean "no default".
6. Never change `formatVersion` to work around an error.
7. Run `jjf validate` after every edit, and report done only once it passes.
8. Change only what was asked. No drive-by reformatting or reordering.

## Workflow

1. Settle the request: which tables, columns and constraints. Ask when unclear.
2. Read the existing `db-design.json` and adopt its conventions.
3. Read [references/fields.md](references/fields.md) for the allowed values of
   every field you are about to touch.
4. Edit the JSON. [references/recipes.md](references/recipes.md) has a worked
   example for each kind of edit.
5. Run `jjf validate <input.json>`.
6. **If validation fails, return to step 4.** One run reports every violation at once, so fix all
   of them before running again. Never report done on a document that still fails.
7. Run `jjf export xlsx <input.json> -o <output.xlsx>` when a refreshed workbook is wanted.
   Export validates first, so a failing document produces no output at all.
8. Report what changed in the JSON, and note that the `.xlsx` still needs
   regenerating if you did not regenerate it.

## Document structure at a glance

```text
db-design.json
├─ $schema         optional  editor hint only; jjf ignores the value
├─ formatVersion   required  "1.0" (a MAJOR.MINOR string)
├─ database        required
│   ├─ name        required  physical database name
│   ├─ logicalName optional  human readable name, any language
│   ├─ description optional
│   └─ dbms        optional  enum, 6 values
└─ tables          required  at least 1
    └─ [n]
        ├─ name        required  physical table name
        ├─ logicalName required  human readable table name
        ├─ description optional
        ├─ columns     required  at least 1
        │   └─ [n]
        │       ├─ name          required  physical column name
        │       ├─ logicalName   required  human readable column name
        │       ├─ type          required  type name without parameters
        │       ├─ nullable      required  boolean, never omitted
        │       ├─ description   optional
        │       ├─ length        optional  integer >= 1
        │       ├─ precision     optional  integer >= 1
        │       ├─ scale         optional  integer >= 0, requires precision
        │       ├─ default       optional  string, <= 255 chars
        │       └─ autoIncrement optional  boolean, default false
        ├─ primaryKey  optional  { name?, columns[] }
        ├─ uniqueKeys  optional  [ { name?, columns[] } ]
        ├─ foreignKeys optional  [ { name?, columns[], references{ table, columns[] }, onUpdate?, onDelete? } ]
        └─ indexes     optional  [ { name, columns[], unique? } ]
```

| Object | Required properties |
| --- | --- |
| root | `formatVersion`, `database`, `tables` |
| `database` | `name` |
| `tables[]` | `name`, `logicalName`, `columns` |
| `columns[]` | `name`, `logicalName`, `type`, `nullable` |
| `primaryKey` | `columns` |
| `uniqueKeys[]` | `columns` |
| `foreignKeys[]` | `columns`, `references` |
| `references` | `table`, `columns` |
| `indexes[]` | `name`, `columns` |

`tables` and `columns` need at least one entry, as does every list of column
names (`primaryKey.columns`, `indexes[].columns`, ...), which also rejects
duplicates.

## Commands and exit codes

| Command | Effect |
| --- | --- |
| `jjf validate db-design.json` | Validates and prints `db-design.json: OK` |
| `jjf export xlsx db-design.json -o db-design.xlsx` | Validates, then writes the workbook and prints `db-design.xlsx: written` |
| `jjf export xlsx db-design.json` | Same, writing next to the input with the extension replaced |
| `jjf export xlsx db-design.json -o -` | Writes to standard output; refused when that is a terminal |
| `jjf version` | Prints the tool version |

Success goes to standard output; errors and usage go to standard error.

| Code | Meaning | Act on it by |
| --- | --- | --- |
| 0 | success | — |
| 1 | general error | reporting the internal error |
| 2 | invalid input | fixing the command line, the path, the JSON syntax, or the `jjf` version |
| 3 | **schema violation** | fixing the contents of the JSON |
| 4 | output failure | creating the output directory or fixing its permissions |

Codes 3 and 2 are the fork in the road: 3 means the document is wrong, 2 means
the invocation or the environment is.

## formatVersion policy

`formatVersion` versions the JSON format, independently of the `jjf` tool
version. The current value is `"1.0"`; the format is `MAJOR.MINOR`.

Write `"1.0"` in a new document and leave the existing value untouched when
editing one. **Never change it yourself** — only a maintainer raises it, and only
when the format changes incompatibly. On `unsupported formatVersion "2.0"`,
upgrade `jjf` rather than rewriting the document.

## Reference material

| File | Contents |
| --- | --- |
| [references/fields.md](references/fields.md) | Every field, its allowed values and its conventions, including the complete `dbms` and `onUpdate`/`onDelete` enums |
| [references/types.md](references/types.md) | Recommended `type` spellings per DBMS |
| [references/errors.md](references/errors.md) | Real validation messages mapped to cause and fix |
| [references/recipes.md](references/recipes.md) | Worked JSON for adding a table, editing a column, adding keys and indexes |
| [references/xlsx-output.md](references/xlsx-output.md) | How to read the generated workbook, and what it cannot be asked to do |

A complete working document lives at `examples/db-design.example.json` in the
jjf repository. Use it as a template for a new document.

## Out of scope

`jjf` does none of the following. Say so plainly instead of fabricating JSON
that pretends otherwise.

- **Semantic validation.** Foreign key targets, duplicate table or column names,
  the existence of key and index columns, type compatibility across a foreign key
  and `nullable` consistency with the primary key are all unchecked, and so are
  the author's responsibility.
- DDL or SQL generation
- Connecting to a database, or importing a schema from one
- Migrations, schema diffs, breaking-change detection
- ER diagrams, Mermaid, or Markdown output — `xlsx` is the only format
- Converting Excel back to JSON, or editing Excel directly
- Customizing the workbook layout, colours, or template
- Extending the document with properties the schema does not define
