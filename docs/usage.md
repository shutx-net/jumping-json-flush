# Using jjf

[README](https://github.com/shutx-net/jumping-json-flush/blob/main/README.md) · [日本語](usage.ja.md)

## validate

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

## export

```sh
jjf export xlsx db-design.json -o db-design.xlsx
jjf export dot  db-design.json -o er.dot
```

Two formats are supported: `xlsx`, an Excel design document, and `dot`, a
Graphviz entity relationship diagram. Both share the same contract.

- The input is always validated first. **A document that fails validation
  produces no output file at all, not even a single byte**
- Leave `-o` out and the output goes **next to the input, with the extension
  replaced by the one of the chosen format** (`docs/db-design.json` →
  `docs/db-design.xlsx` or `docs/db-design.dot`)
- `-o -` writes to standard output. It is **refused when standard output is a
  terminal and the format is binary**, which today means `xlsx` alone; a pipe or
  a redirect is always fine
- The output is written to a temporary file and renamed into place, so a failure
  part way through never leaves a corrupt file behind
- **The same input always produces byte-identical output**, in either format

### xlsx

```sh
# into a pipe
jjf export xlsx db-design.json -o - | sha256sum

# writing straight to the terminal is refused (exit code 2)
jjf export xlsx db-design.json -o -
# jjf: refusing to write a workbook to the terminal; redirect standard output or pass -o <file>
```

The workbook holds a cover sheet, a list of tables, and one sheet per table.

### dot

```sh
jjf export dot db-design.json -o er.dot
```

`jjf` writes DOT **source** and never runs graphviz, so it gains no runtime
dependency. Turning the `.dot` into an image is your own step, with your own
`dot`:

```text
db-design.json --[jjf]--> er.dot --[your dot]--> SVG/PNG
```

```sh
dot -Tsvg er.dot -o er.svg
```

- DOT is text, so **`-o -` may be written to a terminal here**, unlike `xlsx`
- The diagram has one node per table — an HTML-like table of its columns, each
  row carrying the `PK` / `FK` markers, the physical and logical names and the
  type — and one edge per foreign key
- A foreign key naming a table the document does **not** define renders as a
  dashed stub node rather than failing. Foreign key targets are deliberately
  never checked: semantic validation is out of scope, so such a document is
  legal and the diagram shows exactly what the JSON claims

#### Cardinality

The crow's foot notation is **inferred**; the JSON never states a cardinality.

- The child side is **one** when the foreign key's columns, as a set, are
  constrained to be unique in the child table — by its primary key, by one of
  its unique keys, or by a unique index — and **many** otherwise
- The child side is **optional** when any foreign key column is nullable, and
  **mandatory** when every one of them is `NOT NULL`
- The parent side is always **one** and **mandatory**: a foreign key names one
  specific row

#### Byte-for-byte determinism

**The same input always produces a byte-identical `.xlsx` and `.dot`.** No
generation timestamp is embedded, no tool version is written into the output, the
ZIP timestamps are fixed, and nothing depends on Go's map iteration order.

```sh
jjf export xlsx db-design.json -o a.xlsx
jjf export xlsx db-design.json -o b.xlsx
sha256sum a.xlsx b.xlsx   # the two hashes are identical
```

That makes it possible to compare artifact hashes in CI, and to treat "the design
document changed although the JSON did not" as the anomaly it is.

## import

```sh
pg_dump --schema-only mydb > schema.sql
jjf import postgres schema.sql -o db-design.json
```

Builds a design document from a PostgreSQL schema dump. The input is a **file**:
`jjf` never connects to a database, and `postgres` is the only dialect.

- The generated document is **validated against the schema before it is written**,
  so `import` can never produce a document that `jjf validate` would reject
- Leave `-o` out and the output goes **next to the input, with the extension
  replaced by `.json`** (`schema.sql` → `schema.json`)
- `-o -` writes to standard output. Unlike `export` this is allowed on a terminal
  as well, because JSON is text worth reading
- `-schema` chooses the PostgreSQL schema to import, `public` by default. A design
  document has nowhere to put a schema qualification, so exactly one schema is
  imported at a time and everything else is dropped
- `-database` names the database in the generated document. Without it the name
  comes from a `\connect` line when the dump has one, and otherwise from the input
  file name — which then has to be a legal identifier itself
- `-strict` turns every warning into an error. Nothing is written in that case
- Dumps from **pg_dump 13 to 18** are what this was written against, verified
  against real dumps from every major in that range: all six import to the same
  document, byte for byte. The version banner in the dump header is read, and a
  dump from outside that range produces a warning rather than a failure

### What jjf says about a dump

There are three tiers, and which one applies is decided by what the design format
can hold — not by how unusual the SQL is.

| Tier | Example | What happens |
| --- | --- | --- |
| Skipped in silence | `SET`, `GRANT`, `CREATE VIEW`, `CREATE FUNCTION`, `OWNER TO` | Nothing. A dump is full of these, and warning about each would bury the warnings that matter |
| Warned about | a `CHECK` constraint, a partial or expression index, `INCLUDE`, a non-btree access method, `DEFERRABLE`, `INHERITS`, a generated column | One line on standard error naming the dump line, and **the surrounding table or index is still imported** |
| An error | SQL that does not parse, a name the format cannot hold, the same table defined twice | Exit 2. Nothing is written |

```text
$ jjf import postgres schema.sql -o db-design.json
schema.sql:14: warning: constraint users_email_check on table public.users: check constraint is not imported
schema.sql:20: warning: index users_email_live_idx on table public.users: partial index predicate is not imported
schema.sql:22: warning: index users_doc_idx on table public.users: access method gin is not imported; recorded as a plain index
db-design.json: written
```

The `file:line: warning:` shape is what editors and CI annotators already parse.
Warnings go to standard error, the success line to standard output.

An identifier the format cannot hold is an **error, never a silent rename**: a
table called `"user-profiles"` stops the import instead of quietly becoming
`user_profiles`, because a renamed document looks correct and describes a database
that does not exist. Constraint names are the one exception — the schema makes
them optional, so an unusable one is dropped with a warning and the constraint is
imported without a name.

### logicalName and description

The schema requires a `logicalName` on every table and column, and a dump has
none. So:

- the **first line** of a `COMMENT ON` becomes the `logicalName`
- the **rest** becomes the `description`
- a table or column **without a comment** gets its physical name as its
  `logicalName`

That last rule is a starting point to edit, not an answer. The generated document
is meant to be opened and given real names.

### What is not imported

Views, materialized views, functions, triggers, types beyond the name of an enum
used as a column type, extensions, partitioning, inheritance, row level security,
privileges, and sequences beyond deciding which column auto-increments.

`CHECK` and exclusion constraints, index predicates and expressions, `INCLUDE`
columns, operator classes, `DESC` / `NULLS` ordering and `DEFERRABLE` flags have
nowhere to live in the design format, so they warn and are dropped. Anything
outside the schema `-schema` selected is dropped too — silently, except for a
foreign key that pointed into it, which is a real relationship and is reported.

How a PostgreSQL type becomes a `type` plus `length` / `precision` / `scale` is in
[the format reference](db-design-format.md#postgresql-types-on-import).

## version

```sh
jjf version
# jjf v0.1.0
# built with go1.24.0 for linux/amd64
```

A release binary reports its tag name; one installed with `go install` reports the
module version Go recorded.

## Exit codes

| Code | Meaning | Typical cause |
| --- | --- | --- |
| 0 | success | — |
| 1 | general error | an internal error that fits none of the other categories |
| 2 | invalid input | wrong arguments, missing file, JSON syntax error, unsupported `formatVersion`, unknown output format, `-o -` pointed at a terminal for a binary format such as `xlsx`, a dump that cannot be parsed, `-strict` with warnings |
| 3 | schema validation error | a JSON Schema violation |
| 4 | output generation error | the destination cannot be written, the directory does not exist |

What matters in CI is being able to **tell 3 from 2**. A 3 is a problem with the
contents of the design JSON; a 2 is a problem with how the tool was called, where
the file is, or which version of `jjf` is installed.

Success messages go to standard output; errors and usage go to standard error.
