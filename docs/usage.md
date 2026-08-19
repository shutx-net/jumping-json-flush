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
| 2 | invalid input | wrong arguments, missing file, JSON syntax error, unsupported `formatVersion`, unknown output format, `-o -` pointed at a terminal |
| 3 | schema validation error | a JSON Schema violation |
| 4 | output generation error | the destination cannot be written, the directory does not exist |

What matters in CI is being able to **tell 3 from 2**. A 3 is a problem with the
contents of the design JSON; a 2 is a problem with how the tool was called, where
the file is, or which version of `jjf` is installed.

Success messages go to standard output; errors and usage go to standard error.
