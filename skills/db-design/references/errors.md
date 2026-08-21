# Validation errors and their fixes

The messages `jjf` actually prints, mapped to cause and fix. Every message
below is quoted verbatim from the tool.

Back to [SKILL.md](../SKILL.md).

## Reading the output

A schema violation report starts with `<input path>: does not conform ...`, lists
one violation per line as a JSON Pointer and a message, and ends with a count.
**The pointer names exactly what to fix.** `/tables/0/columns/1/name` is the
`name` of the second column of the first table — **indices start at 0**.

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

Every violation is reported in one run. Fix all of them before running again.
The root of the document is pointed at as `(document root)`.

## Schema violations (exit code 3)

| Message | Cause | Fix |
| --- | --- | --- |
| `missing property 'nullable'` | a column has no `nullable` | add `"nullable": true` or `false`; it is never optional |
| `missing property 'logicalName'` | a table or column has no logical name | add one, in any language |
| `missing property 'name'` | an index has no name | `indexes[]` requires `name`; use `ix_<table>_<columns>` |
| `got string, want boolean` | `"nullable": "true"` was quoted | unquote it to `true` / `false` |
| `got string, want integer` | `"length": "30"` was quoted | unquote it to `30` |
| `value must be one of 'PostgreSQL', 'MySQL', 'MariaDB', 'SQLite', 'Oracle', 'SQLServer'` | `dbms` is misspelled | pick one of the six values exactly, see [fields.md](fields.md) |
| `value must be one of 'CASCADE', 'RESTRICT', 'SET NULL', 'SET DEFAULT', 'NO ACTION'` | `onUpdate` / `onDelete` is misspelled | pick one of the five; `SET NULL` is space-separated |
| `'order-lines' does not match pattern '^[A-Za-z_][A-Za-z0-9_]*$'` | an identifier has a hyphen, dot, non-ASCII character or leading digit | use ASCII letters, digits and underscores; move the readable name to `logicalName` |
| `'VARCHAR(30)' does not match pattern '^[A-Za-z][A-Za-z0-9_ ]*$'` | a parameter was baked into `type` | split into `"type": "VARCHAR", "length": 30` |
| `'1' does not match pattern '^[0-9]+\.[0-9]+$'` | `formatVersion` is not `MAJOR.MINOR` | write `"1.0"` |
| `additional properties 'engine' not allowed` | a property the schema does not define was added to a table | remove it and move the content to `description` |
| `additional properties 'comment' not allowed` | the same on a column | use `description` |
| `minLength: got 0, want 1` | an identifier or `logicalName` is the empty string | give it a value; only `description` may be empty |
| `minItems: got 0, want 1` | an empty array such as `"columns": []` | put at least one entry in, or delete the parent |
| `items at 0 and 1 are equal` | a column name is listed twice in one key | remove the duplicate; the list requires unique items |
| `properties 'precision' required, if 'scale' exists` | `scale` was written without `precision` | add `precision`, or drop `scale` |
| `got array, want object` | the document root is an array | the root is a `{ }` object |

## Referential warnings (exit code 0, or 2 with `-strict`)

A document that conforms to the schema is then checked against itself. Each
finding is one line on standard error, shaped `<input>: warning: <what>: <the
problem>` — `<what>` names the object, because there is no line number to give.

```text
db-design.json: warning: primary key pk_orders on table orders: names column "id", which the table declares nullable
db-design.json: warning: foreign key fk_orders_customer on table orders: references table "customers", which this document does not define
db-design.json: warning: index ix_orders_placed_at on table orders: names column "placed_at", which the table does not define
```

The run still **succeeds**: standard output says `db-design.json: OK, 3
warning(s)` and the exit code is 0. `jjf validate -strict` prints the same
warnings and then fails with `jjf: 3 warning(s) with -strict` and exit code
**2, not 3** — code 3 means the document does not conform to the JSON Schema
and nothing else, and a referential finding is not a schema violation.

| Message | Cause | Fix |
| --- | --- | --- |
| `names column "placed_at", which the table does not define` | a key or index names a column its table has no `columns[]` entry for | add the column, or correct the spelling; identifier case is never folded |
| `references table "customers", which this document does not define` | a foreign key points at a table this document has no entry for | add the table, or correct the name |
| `names 1 column(s) but references 2` | the two ends of a foreign key list a different number of columns | make `columns` and `references.columns` the same length, in matching order |
| `references (email) of table accounts, which no primary key, unique key or unique index there constrains to be unique` | the referenced columns are not the target's primary key, one of its unique keys, or a unique index | point the foreign key at the target's key, or add a unique key or `"unique": true` index covering those columns |
| `names column "id", which the table declares nullable` | a primary key column has `"nullable": true` | set `"nullable": false`; SQL forces a primary key column NOT NULL anyway |
| `defines column "email" more than once` | one table has two `columns[]` entries with the same `name` | remove or rename one of them |
| `declares more than one constraint or index called "pk_accounts"` | one table uses the same name for two of its `primaryKey` / `uniqueKeys[]` / `foreignKeys[]` / `indexes[]` | rename one; unnamed constraints never collide |

Whether the design is a *good* one is not checked, so do not offer normalization,
index-strategy or type advice as though `jjf` had asked for it. Duplicate table
names across a document, type compatibility across a foreign key, and the
uniqueness of index names across a schema are not checked either.

## Failures before validation (exit code 2)

| Output | Cause | Fix |
| --- | --- | --- |
| `jjf: db-design.json: line 5, column 4: invalid character '}' looking for beginning of object key string` | invalid JSON syntax, such as a trailing comma | fix the reported line and column |
| `jjf: open db-design.json: no such file or directory` | wrong path | check the path |
| `jjf: unsupported formatVersion "2.0"; this jjf supports 1.x - please upgrade jjf` | the document uses a newer format than this build | upgrade `jjf`. **Never rewrite the document to get around this** |
| `jjf: unsupported format "csv"; supported formats: xlsx, dot` | an export format that does not exist | the formats are `xlsx` and `dot` |
| `jjf: validate takes exactly one input file, got 0` | no input path given | pass the path |
| `jjf: refusing to write a workbook to the terminal; redirect standard output or pass -o <file>` | `-o -` with a terminal on standard output | redirect the output or pass a file path |

## Output failures (exit code 4)

| Output | Cause | Fix |
| --- | --- | --- |
| `jjf: cannot create output file: /nonexistent-dir/x.xlsx: no such file or directory` | the output directory does not exist | create the directory, or choose another path |

## Encoding

`jjf` accepts a UTF-8 BOM, but other tools choke on one. Save documents as
**UTF-8 without BOM, with LF line endings**.
