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

### Referential checks

A document that conforms to the schema is then checked **against itself**. Eight
things are looked at:

- every column named by a primary key, a unique key, a foreign key or an index
  is defined by the table that declares it
- every foreign key names a table this document defines
- a foreign key names as many columns as it references
- the referenced columns, as a set, are constrained to be unique in the target
  table — by its primary key, by one of its unique keys, or by a unique index
- no primary key column is declared `nullable: true`
- one table never uses the same column name, or the same constraint or index
  name, twice
- no column declares a `default` that is empty — write no `default` key at all
  when the column has no DEFAULT clause
- every `default` reads as a SQL expression. A string default carries its SQL
  quoting, so the string `now` is written `"'now'"`; the bare `"now"` is a
  column reference, which no DEFAULT may contain

Each finding is one line on standard error, naming the object rather than a
place in the file:

```text
db-design.json: warning: foreign key fk_orders_customer on table orders: references table "customers", which this document does not define
db-design.json: warning: primary key pk_orders on table orders: names column "id", which the table declares nullable
db-design.json: warning: index ix_orders_placed_at on table orders: names column "placed_at", which the table does not define
```

They are **warnings**. The exit code stays 0 and standard output reports the
count beside the verdict, so a document that passes today keeps passing:

```text
db-design.json: OK, 3 warning(s)
```

- `-strict` turns every warning into an error. The warnings are printed either
  way; `-strict` changes only what the run is worth, and nothing is written to
  standard output in that case

```sh
jjf validate -strict db-design.json   # exit code 2 when anything was found
```

A strict failure is **exit code 2, not 3**. Code 3 means the document does not
conform to the JSON Schema and nothing else; a referential finding is not a
schema violation, and asking for `-strict` is a property of the invocation.

Whether the design is a *good* one is not checked and never will be:
normalization, index strategy, type suitability and naming conventions are the
author's. Duplicate table names across a document, and the uniqueness of index
names across a schema, are not checked either. A `default` is only read as an
expression, never evaluated: jjf does not run it, does not check it against the
column's `type`, and does not object to a function it has never heard of.

## export

```sh
jjf export xlsx db-design.json -o db-design.xlsx
jjf export svg  db-design.json -o er.svg
jjf export ddl  db-design.json -o schema.sql
```

Three formats are supported: `xlsx`, an Excel design document; `svg`, an entity
relationship diagram `jjf` draws itself; and `ddl`, a DDL script for the
database the document names. They share the same contract.

- The input is always validated first. **A document that fails validation
  produces no output file at all, not even a single byte**
- Leave `-o` out and the output goes **next to the input, with the extension
  replaced by the one of the chosen format** (`docs/db-design.json` →
  `docs/db-design.xlsx`, `docs/db-design.svg` or `docs/db-design.sql`). `ddl`
  is the one format whose extension is not its name: a `.ddl` file is nothing,
  and calling the format `sql` would promise arbitrary SQL rather than one
  schema-creating script
- `-o -` writes to standard output. It is **refused when standard output is a
  terminal and the format is binary**, which today means `xlsx` alone; a pipe or
  a redirect is always fine
- The output is written to a temporary file and renamed into place, so a failure
  part way through never leaves a corrupt file behind
- **The same input always produces byte-identical output**, in every format
- **`ddl` alone refuses a document that contradicts itself** (exit code 2). SQL a
  database rejects is worth nothing, while a slightly broken document still makes
  a useful workbook and a useful diagram — so `xlsx` and `svg` render it and
  `jjf validate` is where those contradictions are reported

### xlsx

```sh
# into a pipe
jjf export xlsx db-design.json -o - | sha256sum

# writing straight to the terminal is refused (exit code 2)
jjf export xlsx db-design.json -o -
# jjf: refusing to write a workbook to the terminal; redirect standard output or pass -o <file>
```

The workbook holds a cover sheet, a list of tables, and one sheet per table.

### svg

```sh
jjf export svg db-design.json -o er.svg
```

`jjf` **draws** this one. There is no renderer to install and nothing to
configure: the ranking, the ordering, the coordinates and the text metrics are
all its own, and the `.svg` opens in a browser and displays in a README as it
is.

```text
db-design.json --[jjf]--> er.svg
```

- SVG is text, so **`-o -` may be written to a terminal here**, unlike `xlsx`
- One box per table, one row per column carrying the `PK` / `FK` markers, the
  physical and logical names and the type, and one relationship per foreign
  key. The crow's foot notation on each relationship is inferred from the
  document; the rules are below
- A foreign key naming a table the document does **not** define is drawn as a
  dashed stub. The exporter checks nothing and reports nothing, ever: such a
  document is legal and the diagram shows exactly what the JSON claims.
  `jjf validate` is where the same document is reported, as a warning
- **The background is an opaque white rectangle**, not transparency. A
  transparent diagram of dark text is unreadable on a dark-mode README — the
  likeliest place this file is looked at — and making it follow the theme would
  need a `<style>` block carrying `prefers-color-scheme`, which is exactly what
  GitHub's SVG sanitiser strips: the file would be right in a browser and wrong
  where it was made to go
- **The layout has no knobs**, and will not grow any. `jjf` owns this drawing
  the way it owns the workbook: there is no flag for the direction, the spacing
  or the splines, exactly as there is no flag for the workbook's column widths
  or its colours. A knob would also cost the promise above — the same input
  would no longer produce byte-identical output, only the same input with the
  same flags

#### Cardinality

The crow's foot notation is **inferred**; the JSON never states a cardinality.

- The child side is **one** when the foreign key's columns, as a set, are
  constrained to be unique in the child table — by its primary key, by one of
  its unique keys, or by a unique index — and **many** otherwise
- The child side is always **optional**. A primary key, a unique key and
  `NOT NULL` all constrain how many children one parent row may have, never how
  few, so nothing in the document says a parent row must have one
- The parent side is always **one**: a foreign key names one specific row
- The parent side is **optional** when any foreign key column is nullable — a
  child row may then exist while pointing at no parent — and **mandatory** when
  every one of them is `NOT NULL`

### ddl

```sh
jjf export ddl db-design.json -o schema.sql
psql -d mydb -f schema.sql            # a PostgreSQL document
mysql mydb < schema.sql               # a MySQL one
```

`jjf` writes SQL **text** and never connects to a database, so it gains no
runtime dependency here either. Applying the script is your own step, with your
own client.

```text
db-design.json --[jjf]--> schema.sql --[your psql or mysql]--> a database
```

The script **creates a schema from nothing**. Applying it to a database that
already has one is not supported and will not become supported: moving an
existing schema from one state to another means knowing the state it is in,
which is introspection, which is a different tool. Like the `.xlsx` and the
`.svg`, the `.sql` is a **build artifact** — regenerate it, never edit it, never
treat it as the design — and the file says so in its own first two lines.

#### PostgreSQL and MySQL

`database.dbms` selects the dialect and must be present. `jjf export ddl` is the
only command that reads the field, and it reads it strictly: an absent value is
an error rather than a default, and so is a value naming a system `jjf` does not
write.

```text
jjf: ddl export needs the document to name its target; add "dbms": "PostgreSQL" or "MySQL" to "database"
jjf: ddl export supports PostgreSQL, MySQL; this document names "MariaDB"
```

Both exit 2, and both are reported before anything else, so a MariaDB document is
never lectured about PostgreSQL's rules.

The two dialects `jjf` writes are the two it can check end to end: each has an
importer that reads a real database back, and a round trip in CI that runs the
two against each other on a live server. A dialect without those would ship on
golden files alone, which prove only that the generator emits what it emitted —
so `MariaDB`, `SQLite`, `Oracle` and `SQLServer` are refused rather than
approximated. For one of those, the answer is that `jjf` does not generate DDL
for it, not SQL for a system nobody verified.

#### What it writes

Whichever dialect is chosen:

- **Statement phases, fixed.** Every `CREATE TABLE` (with `PRIMARY KEY` and
  `UNIQUE` inline) comes first, then every `CREATE [UNIQUE] INDEX`, then every
  foreign key as an `ALTER TABLE`. Document order within each phase, and
  **nothing is sorted**. Fixing the phases is what removes every ordering
  dependency between tables, so mutual and self references need no topological
  sort
- **Foreign keys are never inline.** Phase 2 has to come first because both
  systems accept a plain `UNIQUE INDEX` as a foreign key target, not only a
  `UNIQUE` constraint
- **`default` is copied out verbatim** after `DEFAULT `. The field is defined as
  SQL expression text, and `jjf validate` has already refused an empty one or one
  that does not read as an expression
- **A fixed two-line header**, with no timestamp, no tool version and no input
  path — a version would make two builds of `jjf` disagree about the same
  document, and this is the artifact that gets diffed. Both dialects write the
  same two lines
- **All or nothing.** The whole document is checked, then the file is written

##### PostgreSQL

- **Four phases**: the three above and then every `COMMENT ON`
- **`autoIncrement` becomes `GENERATED BY DEFAULT AS IDENTITY`**, the standard
  form PostgreSQL recommends over `SERIAL`
- **Every identifier is double-quoted**, always, which preserves case and makes
  reserved words such as `order` and `user` work with no keyword list. **Type
  names are never quoted**: `"integer"` is not a type, and `"ORDER_STATUS"` is a
  different type from the lowercase one PostgreSQL would have created
- **`logicalName` and `description` become `COMMENT ON`**, joined by a real
  newline: the first line is the logical name and the rest is the description,
  which is exactly how `jjf import` reads a comment back. An object whose logical
  name is just its physical name and which has no description gets no comment,
  because that is the state an import leaves for an object the dump had none for
- The script assumes `standard_conforming_strings = on`, PostgreSQL's default
  since 9.1: a backslash inside a string literal is an ordinary character. No
  `SET` is emitted

##### MySQL

- **Three phases.** MySQL has no `COMMENT ON` statement at all, so there is
  nowhere to put a fourth: `logicalName` and `description` are folded into
  `CREATE TABLE` as `COMMENT '...'` at the end of a column definition and
  `COMMENT='...'` after the closing parenthesis. The two fields are joined by a
  real newline exactly as they are for PostgreSQL
- **`autoIncrement` becomes `AUTO_INCREMENT`**, written after `NOT NULL` and
  after any `DEFAULT` — the order `mysqldump` writes, so a generated script and a
  dump of the database it created read alike
- **Every identifier is backtick-quoted**, always, for the same reasons; an
  embedded backtick would be doubled. Double quotes are not used, because MySQL
  reads `"order"` as a string literal unless `ANSI_QUOTES` is in `sql_mode` — and
  no `SET` is emitted to put it there
- **A backslash inside a string literal is doubled.** MySQL treats one as an
  escape character unless `NO_BACKSLASH_ESCAPES` is in `sql_mode`, which is not
  the default, so an undoubled `C:\tmp` in a `description` would reach the
  database as `C:` followed by a tab
- **`TINYINT` keeps its `length` and every other integer type drops one.** A
  display width has been deprecated since MySQL 8.0.17 and `mysqldump` no longer
  writes one, so `INT` with `length: 11` is written `INT`; `tinyint(1)` it does
  still write, because that is how a boolean is stored
- **A type attribute keeps its place after the parameters**: `DECIMAL` with
  `precision: 10`, `scale: 2` and a `type` of `DECIMAL UNSIGNED` is written
  `DECIMAL(10,2) UNSIGNED`, because `DECIMAL UNSIGNED(10,2)` is a syntax error

#### The refusal

`ddl` refuses a document that contradicts itself, and writes nothing.

```text
db-design.json: error: primary key pk_orders on table orders: names column "id", which the table declares nullable
db-design.json: error: foreign key fk_orders_customer on table orders: references table "customers", which this document does not define
jjf: 2 problem(s) prevent PostgreSQL DDL generation
```

The dialect in that last line is the one the document itself chose: a MySQL
document is refused with `prevent MySQL DDL generation`.

The exit code is **2**, not 4: the document is what is wrong, and 4 has to keep
meaning that the environment stopped the write. There is no `-strict` and there
will not be one — a refusal is not a warning that can be waived, because waiving
it would produce SQL the database rejects.

Most of the reasons are the ones `jjf validate` reports as warnings. The rest
are checked only here, because they are statements about a database system
rather than about the document — and they are **not the same list for the two
dialects**. The summary line names the dialect that refused, for that reason.

For PostgreSQL:

- **The schema-wide namespace.** Table names, index names, and the names of
  `PRIMARY KEY` and `UNIQUE` constraints all live in one namespace per schema, so
  none of them may collide with any other. Foreign key constraint names do not:
  they are scoped per table, and two tables may legally carry a constraint of one
  name
- **Identity columns.** A column that is `autoIncrement` may not also carry a
  `default` — PostgreSQL refuses a column that is both — and may not be
  `nullable`, because PostgreSQL would silently make it `NOT NULL` and the
  database would stop matching the document

For MySQL, where the namespaces are **the exact inverse**:

- **Two tables of one name**, which MySQL cannot create any more than PostgreSQL
  can
- **Two foreign keys of one name**, anywhere in the schema. InnoDB keeps foreign
  key names per database and answers a collision with `Duplicate foreign key
  constraint name`. Index names are the other way round: they live in the table,
  so two MySQL tables may each carry an `ix_created`
- **`AUTO_INCREMENT`, four ways.** At most one such column per table; it must be
  the first column of the table's `primaryKey` or of one of its `uniqueKeys`
  (an `indexes` entry cannot serve, because the script creates it a phase after
  the table); it may not also carry a `default`; and it may not be `nullable`,
  because MySQL would store it `NOT NULL` and the database would stop matching
  the document
- **A key over a `TEXT`, `BLOB` or `JSON` column.** MySQL wants a prefix length
  for the first two and a generated column for the third, and the design format
  has nowhere to put either
- **`VARCHAR` or `VARBINARY` with no `length`.** MySQL has no default width for
  them, so the column would be a syntax error rather than a column

#### What it does not write

The design format has no place for these, so the script never contains them:
`CHECK` constraints, `CREATE TYPE`, schemas other than the default, collations,
partial and expression indexes, index methods, `DEFERRABLE`, storage parameters,
partitioning, and row-level security. The MySQL script is the same idea against
a different grammar: no `CHECK` constraints, no triggers, no views, no table
options — engine, character set, collation, row format, partitioning — and no
`ON UPDATE CURRENT_TIMESTAMP`, because a script that guessed at an engine or a
collation would be writing a design decision nobody made.
`database.logicalName` and `database.description` are not written either: with
no `CREATE SCHEMA` and no `CREATE DATABASE` there is no object for a comment to
attach to, and `jjf import` never fills those two fields, so nothing is lost.

Two consequences are worth stating plainly.

- **A user-defined type is named but never created.** Column types are opaque
  strings, so a document naming an enum or a domain imported from PostgreSQL
  produces a script that references a type no statement in it creates. The script
  parses and fails on execution. That is a limitation of the design format, not a
  bug, and closing it would mean teaching the schema about type definitions
- **A parameter a known type cannot take is dropped** without a word. `INTEGER`
  with `length: 11` is written `INTEGER`, because `integer(11)` is DDL PostgreSQL
  rejects. For a type `jjf` does not know, the name and its parameters are
  reproduced as the document states them

`ENUM` and `SET` are the same edge in MySQL, one step sharper. Their parenthesis
holds a value list rather than a number, the format has nowhere to keep one, and
MySQL rejects a bare `ENUM` when it parses the script rather than when it runs
it. They are still emitted as the document writes them, because *every* `ENUM`
column is in that state — including one `jjf import` has just produced from a
real database — so refusing them would refuse documents `jjf` itself wrote. A
`VARCHAR` with no `length` is **not** the same case and is refused: `mysqldump`
always writes the length, so no imported document can hit that refusal.

`ON UPDATE NO ACTION` and `ON DELETE NO ACTION` do not survive a round trip
through a database: `NO ACTION` is the default in both systems, so neither
`pg_dump` nor `mysqldump` writes it and `jjf import` records nothing. That is
expected rather than a bug. Two MySQL-only cases behave the same way: a named
`primaryKey` comes back unnamed, because MySQL calls every primary key
`PRIMARY`, and `SET DEFAULT` as a referential action is stored and dumped back
but never performed — InnoDB treats it as `NO ACTION` at run time.

#### Byte-for-byte determinism

**The same input always produces a byte-identical `.xlsx`, `.svg` and `.sql`.**
No generation timestamp is embedded, no tool version is written into the
output, the ZIP timestamps are fixed, and nothing depends on Go's map iteration
order.

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

mysqldump --no-data --default-character-set=utf8mb4 mydb > schema.sql
jjf import mysql schema.sql -o db-design.json
```

Builds a design document from a schema dump. The input is a **file**: `jjf` never
connects to a database.

| Dialect | Input |
| --- | --- |
| `postgres` | `pg_dump --schema-only` |
| `mysql` | `mysqldump --no-data` |

- The generated document is **validated against the schema before it is written**,
  so `import` can never produce a document that `jjf validate` would reject
- Leave `-o` out and the output goes **next to the input, with the extension
  replaced by `.json`** (`schema.sql` → `schema.json`)
- `-o -` writes to standard output. Unlike `export` this is allowed on a terminal
  as well, because JSON is text worth reading
- `-schema` chooses the PostgreSQL schema to import, `public` by default. A design
  document has nowhere to put a schema qualification, so exactly one schema is
  imported at a time and everything else is dropped. It is **PostgreSQL-only**,
  and passing it to `mysql` is an **error rather than a silently ignored flag** —
  a MySQL schema *is* a database, so there is no second level to choose
- `-database` names the database in the generated document. Without it the name
  comes from a `\connect` line for `postgres` and from a `USE` statement or the
  header banner for `mysql`, and otherwise from the input file name — which then
  has to be a legal identifier itself
- `-strict` turns every warning into an error. Nothing is written in that case
- Dumps from **pg_dump 13 to 18** and from **MySQL 8.0 and 8.4** are what this was
  written against, verified against real dumps committed in the repository: every
  PostgreSQL major in that range imports to the same document, byte for byte, and so
  does every captured MySQL series. The version banner in the dump header is read,
  and a dump from outside those ranges produces a warning rather than a failure. The
  supported range is exactly what the captures cover, and for MySQL it is stated over
  majors, so a 9.x capture is what would widen it

**Pass `--default-character-set=utf8mb4` to `mysqldump`.** Without it the client
may negotiate `latin1`, and every Japanese `COMMENT` in the dump is encoded twice.
Such a dump still parses, still imports and still round-trips, so nothing will tell
you but the mojibake in your own `logicalName` fields.

### What jjf says about a dump

There are three tiers, and which one applies is decided by what the design format
can hold — not by how unusual the SQL is.

| Tier | Example | What happens |
| --- | --- | --- |
| Skipped in silence | `SET`, `GRANT`, `CREATE VIEW`, `CREATE FUNCTION`, `OWNER TO`, and for MySQL also `LOCK TABLES`, `DROP TABLE`, `DELIMITER`, a trigger or a routine, and every table option | Nothing. A dump is full of these, and warning about each would bury the warnings that matter |
| Warned about | a `CHECK` constraint, a partial or expression index, `INCLUDE`, a non-btree access method, `DEFERRABLE`, `INHERITS`, a generated column, and for MySQL also a `FULLTEXT` or `SPATIAL` index, an index prefix length, `DESC` in a key, `ON UPDATE CURRENT_TIMESTAMP`, an `ENUM` or `SET` value list, an inline `REFERENCES`, partitioning, and a non-InnoDB engine | One line on standard error naming the dump line, and **the surrounding table or index is still imported** |
| An error | SQL that does not parse, a name the format cannot hold, the same table defined twice | Exit 2. Nothing is written |

```text
$ jjf import postgres schema.sql -o db-design.json
schema.sql:14: warning: constraint users_email_check on table public.users: check constraint is not imported
schema.sql:20: warning: index users_email_live_idx on table public.users: partial index predicate is not imported
schema.sql:22: warning: index users_doc_idx on table public.users: access method gin is not imported; recorded as a plain index
db-design.json: written
```

```text
$ jjf import mysql schema.sql -o db-design.json
schema.sql:31: warning: index ft_users_bio on table users: full-text index is not imported
schema.sql:32: warning: constraint ck_users_email on table users: check constraint is not imported
schema.sql:29: warning: users.updated_at: ON UPDATE CURRENT_TIMESTAMP is not represented
db-design.json: written
```

The warnings are in the order they were found, not in line order: everything the
parser noticed comes first, and everything the resolution pass noticed comes after.

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

- the **first line** of the comment becomes the `logicalName`
- the **rest** becomes the `description`
- a table or column **without a comment** gets its physical name as its
  `logicalName`

The comment is a `COMMENT ON` statement for PostgreSQL and an inline `COMMENT` on
the column or a `COMMENT=` table option for MySQL. The split is the same one
`jjf export ddl` composes, in both dialects, which is what lets a document survive
a trip through a real database unchanged.

That last rule is a starting point to edit, not an answer. The generated document
is meant to be opened and given real names.

### What is not imported

Views, materialized views, functions, triggers, routines, events, types beyond the
name of an enum used as a column type, extensions, partitioning, inheritance, row
level security, privileges, and sequences beyond deciding which column
auto-increments.

`CHECK` and exclusion constraints, index predicates and expressions, `INCLUDE`
columns, operator classes, `DESC` / `NULLS` ordering and `DEFERRABLE` flags have
nowhere to live in the design format, so they warn and are dropped. Anything
outside the schema `-schema` selected is dropped too — silently, except for a
foreign key that pointed into it, which is a real relationship and is reported.

For MySQL the list adds:

| Not imported | Why |
| --- | --- |
| Table options — engine, character set, collation, row format, `AUTO_INCREMENT` counter | The format has nowhere for them. A non-InnoDB engine is warned about anyway, because only InnoDB enforces the foreign keys the document declares |
| Partitioning | The partitioned table is not the table the document would describe. Its columns still are, so the table is imported and the partitioning alone is reported |
| `FULLTEXT` and `SPATIAL` indexes | Neither is an index over the columns the document would name: one answers a `MATCH … AGAINST` and the other an R-tree query |
| An index prefix length, `KEY ix (body(255))` | The index still covers the column the document names, so it is imported and the narrowing is reported |
| `DESC` in a key | MySQL 8 has real descending indexes; the format records the columns only |
| `ON UPDATE CURRENT_TIMESTAMP` | An automatic update rule, not a default value. Folding it into `default` would produce a script MySQL refuses |
| An `ENUM` or `SET` value list | The format has nowhere to keep one. The type name survives and the values are named in the warning |
| The index InnoDB creates to back a foreign key | It arrives under the foreign key's own name, and a jjf document keeps constraint and index names in one namespace per table. The foreign key is what recreates it, so nothing is lost |
| An inline `REFERENCES` on a column, `pid INT REFERENCES parent (id)` | MySQL parses the clause and then creates nothing from it: no `FOREIGN KEY` line in `SHOW CREATE TABLE`, no row in `information_schema.REFERENTIAL_CONSTRAINTS`, and the same answer under MyISAM as under InnoDB. Importing it would put a constraint in the document that the database does not have. The table-level `FOREIGN KEY` spelling, which MySQL does honour, is imported as usual |
| `DEFAULT NULL` on a nullable column | MySQL writes it for every nullable column it was not given a default for, and it says exactly what `nullable` already says. Dropped in silence, because a warning per column would bury the rest |

How a type becomes a `type` plus `length` / `precision` / `scale` is in the format
reference, for [PostgreSQL](db-design-format.md#postgresql-types-on-import) and for
[MySQL](db-design-format.md#mysql-types-on-import).

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
the file is, or which version of `jjf` is installed. A 3 means JSON Schema
conformance and nothing else: a referential finding is not a schema violation, so
`validate -strict` reports one as 2, exactly as `import -strict` does.

Success messages go to standard output; errors and usage go to standard error.
