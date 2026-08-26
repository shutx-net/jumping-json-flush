# DDL generation — design record

**Status: adopted and implemented**, as `internal/export/ddl` and
`jjf export ddl`. This document is the specification the implementation follows.
The choices in it were settled before any code because several of them are
expensive to revisit once output has been shipped.

## Why jjf generates DDL

`AGENTS.md` carries the reason in full: the JSON is the representation an agent
authors, and everything else is derived from it. Without DDL that principle only
half holds. A design can be imported from a database and rendered for people,
but a design *written* in JSON cannot become a database unless someone writes
the SQL by hand — and that hand-written SQL immediately becomes a second source
of truth, which is the thing the whole tool exists to prevent.

The value is not saved typing. It is that the mistakes made when DDL is written
directly — a foreign key with no target, a nullable primary key column, a bare
word where a string literal belongs, statements in an order the database rejects
— stop being mistakes anyone can make. The document is checked, and the
generator is the only thing that writes SQL.

## What this commits us to

Generated DDL is the first artifact jjf produces that is *code*: text a database
executes, and text a reader will be tempted to keep. That tempts a reader into
treating it as a file to edit and commit, at which point every later improvement
to the generator — a better type mapping, a corrected quoting rule — shows up in
their repository as a diff that is not a schema change.

The resolution is not to freeze the format. It is to be explicit that the `.sql`
is a build artifact, exactly like the `.xlsx` and the `.svg`: regenerate it,
never edit it, and do not treat it as the design. The generated file says so in
its own header, the way the SVG exporter already does. What that buys is the
right to improve the generator — announced, and batched into a release — instead
of a format frozen by the first person who committed its output.

That is a weaker promise than "the bytes never change", and it is the same
promise the other two exporters already make. The choices below are still
written down in advance, because changing one is a release note and a
regeneration for every user, which is a cost worth incurring deliberately rather
than by accident.

## What it does not commit us to

The generated DDL creates a schema from nothing. Applying it to a database that
already has one is not a supported operation and will not become one: knowing
how to move an existing schema from one state to another requires knowing the
state it is in, which means introspection, which is a different tool.

This is a deliberate simplification, not an omission awaiting work. A design
that has already been applied somewhere is changed by writing the migration by
hand.

## Scope of the generator

A dialect is in scope when it can be answered by something other than its own
output. Two things have to exist for it: an importer that reads a real database
back into a document, and a round trip in CI that runs the two against each
other on a live server. A dialect that cannot be checked end to end would ship
on golden files alone, which prove only that the generator emits what it
emitted.

That is the gate, and it is why the list is shorter than the six systems the
`dbms` enum names rather than a statement about any one of them. Two costs
decide how fast it grows:

1. `default` holds verbatim SQL expression text. It is the first field to break
   across dialects: `'{}'::jsonb` is PostgreSQL syntax, and `CURRENT_TIMESTAMP`
   does not mean quite the same thing everywhere.
2. Each dialect multiplies exactly the surface the section above says can never
   be changed freely — every renderer added is another thing a release note has
   to be able to describe.

Nothing else in jjf branches on `dbms`. The DDL exporter is its only consumer,
and it reads it strictly rather than guessing.

## Dialects

`database.dbms` selects the dialect, and it must be present.

| `dbms` | DDL | What answers it |
|---|---|---|
| `PostgreSQL` | written | `internal/importer/postgres`, and the `verify` leg of `.github/workflows/pg-fixtures.yml` |
| `MySQL` | written | `internal/importer/mysql`, and `.github/workflows/mysql-fixtures.yml` |
| `MariaDB`, `SQLite`, `Oracle`, `SQLServer` | refused | nothing: no importer and no live-server leg, so they could ship on golden files alone |

Both mistakes are errors and neither is a default. An absent `dbms` is refused
rather than assumed to mean PostgreSQL, because generating SQL for a database
nobody named is worse than saying so; a value from the second row is refused
with a message that names the dialects jjf does write, so an author learns what
the tool does rather than only what it will not do.

MariaDB is the interesting refusal, because it is the one a reader will think is
an oversight. It is close enough to MySQL that mapping it onto the MySQL writer
is one line of code, and that is exactly what the gate above forbids: it has no
importer and no CI leg, its dump tool writes a different banner, its type set
has `UUID` and `INET6`, and its `sql_mode` defaults differ. Claiming it because
it is MySQL-shaped would make the round trip's green light mean nothing for half
the documents it claimed to cover.

## Settled format choices

| # | Choice | Decision | Why, and why not the alternative |
|---|---|---|---|
| 1 | Statement order | Four fixed phases: `CREATE TABLE` (with `PRIMARY KEY` and `UNIQUE` inline) → `CREATE [UNIQUE] INDEX` → `ALTER TABLE ADD CONSTRAINT FOREIGN KEY` → `COMMENT ON`. Document order within each phase. | Removes every ordering dependency between tables, so mutual and self references need no topological sort and no cycle handling. Sorting would also be a determinism liability; not sorting is both simpler and safer. |
| 2 | Foreign key placement | Phase 3, never inline. | Phase 2 must precede it: PostgreSQL accepts a plain `UNIQUE INDEX` as a foreign key target, not only a `UNIQUE` constraint, so `indexes[].unique` is a legitimate source of the referenced uniqueness and has to exist first. Inline foreign keys cannot express that, nor a cycle. |
| 3 | `autoIncrement` | `GENERATED BY DEFAULT AS IDENTITY`. | Standard SQL, and the form PostgreSQL itself recommends over `SERIAL`. Available since PostgreSQL 10, so it needs no version gate anywhere in the supported range. |
| 4 | Identifier quoting | Always double-quote every identifier. | The schema's `identifier` pattern is `^[A-Za-z_][A-Za-z0-9_]*$`, so quoting is always safe and never ambiguous. Quoting unconditionally also preserves case and makes reserved words (`order`, `user`) work without a keyword list to maintain. |
| 5 | Column types | Reconstruct from `type` plus `length` / `precision` / `scale`. | The reverse of the importer's normalisation. The type name is passed through as written: jjf does not maintain a per-system type catalogue, and inventing one would be database-design judgement. |
| 6 | `default` | Emitted verbatim after `DEFAULT `. | The field is defined as SQL expression text. An ambiguous or malformed value does not corrupt silently — PostgreSQL forbids column references in `DEFAULT`, so a bare identifier is always rejected. `jjf validate` (C7/C8) catches the same mistakes before the database sees them. |
| 7 | `logicalName` / `description` | `COMMENT ON TABLE` and `COMMENT ON COLUMN`. | Closes the round trip: the importer already reads these back into the same two fields. |
| 8 | Header | A fixed comment naming the JSON as the source of truth, as the SVG exporter already writes. No timestamp, no tool version, no input path. | The static line is what makes the build-artifact policy visible at the point a reader is deciding whether to keep the file. The omissions are determinism: a version or a timestamp in the output makes two builds of jjf disagree about the same document, and DDL is the artifact where that matters most, because it is the one that gets diffed. |
| 9 | Failure policy | All or nothing. Validate the whole document, then write. | The opposite of the importer, deliberately. A partially written DDL file that fails on statement 40 after creating twelve tables is worse than no file. |
| 10 | Target-version flag | None. | Every construct the schema can express predates PostgreSQL 13 — the newest is identity columns, from 10 — so a `-pg-version` flag would have nothing to switch on. A flag that changes no output is a lie and a permanent interface. Revisit only when a construct that genuinely diverges enters the schema; `NULLS NOT DISTINCT` (PostgreSQL 15+) would be the first. |

## MySQL: where it diverges

The numbered choices above were settled for PostgreSQL and are not renumbered.
The rows below record where MySQL's answer differs, and each cites the choice it
is answering, so a reader can tell a considered divergence from an oversight.
Everything not listed here is the same decision for both dialects: the header,
document order within every phase, nothing sorted, `default` verbatim, and all
or nothing. Choice 10 in particular stands: there is no target-version flag for
MySQL either, because every construct the schema can express predates MySQL 5.7
and a flag that changes no output is a lie and a permanent interface.

| # | Choice | Decision | Why, and why not the alternative |
|---|---|---|---|
| M1 | Statement order (choices 1, 2) | **Three** fixed phases: `CREATE TABLE` (with `PRIMARY KEY` and `UNIQUE` inline) → `CREATE [UNIQUE] INDEX` → `ALTER TABLE ADD CONSTRAINT FOREIGN KEY`. Comments are folded into phase 1, not written as a fourth phase. | Choice 1's fourth phase has nowhere to go: MySQL has no `COMMENT ON` statement at all, so folding is the only encoding rather than a stylistic choice. The rest of choice 1 survives untouched — no `CREATE TABLE` refers to another table, so mutual and self references still need no topological sort — and choice 2's argument survives verbatim, because MySQL also accepts a plain `UNIQUE INDEX` as a foreign key target and phase 2 must therefore precede phase 3. Not a fourth phase of `ALTER TABLE ... MODIFY COLUMN ... COMMENT`, which would have to restate the column's whole definition and could then disagree with itself. |
| M2 | Identifier quoting (choice 4) | Always backtick-quote every identifier; an embedded backtick is doubled. | Choice 4's argument, with a different delimiter: the schema's `identifier` pattern is `^[A-Za-z_][A-Za-z0-9_]*$`, so quoting is always safe and never ambiguous, it preserves case, and it makes `order` and `user` work without a keyword list to maintain. The doubling is defensive rather than reachable — that pattern forbids a backtick — and is exercised only by a unit test, exactly as the double-quote doubling is. Not ANSI double quotes, which MySQL reads as a string literal unless `ANSI_QUOTES` is in `sql_mode` — and M6's rule against emitting a `SET` forbids putting it there. |
| M3 | `autoIncrement` (choices 3, 9) | `AUTO_INCREMENT`, after `NOT NULL` and after any `DEFAULT`. Four preconditions refused before a byte is written: more than one such column in a table; a column that is not the leading column of the table's `primaryKey` or of one of its `uniqueKeys`; one that also carries a `default`; one declared `nullable`. | `GENERATED BY DEFAULT AS IDENTITY` is not MySQL syntax, and MySQL's own form carries rules PostgreSQL's does not. The first two are ERROR 1075 and the third is ERROR 1067, so a document in any of those states would produce a script the database rejects. The fourth is MySQL's silent case, the one C5 and PostgreSQL's identity rule already exist for: MySQL accepts the column, stores it `NOT NULL`, and leaves the document and the database disagreeing. The key must be one `CREATE TABLE` itself carries — an `indexes[]` entry would do for MySQL, but M1 does not create it until phase 2, and the table is rejected before it exists. |
| M4 | `logicalName` / `description` (choice 7) | `COMMENT '...'` at the end of the column definition, and `COMMENT='...'` after the closing parenthesis of the table. | Choice 7 closes the round trip through `COMMENT ON`; MySQL has no such statement, so the same two fields ride on the definitions themselves. The join is unchanged — first line the logical name, the rest the description — so `jjf import mysql` splits an inline comment exactly where `jjf import postgres` splits a `COMMENT ON`, and an object whose logical name is its physical name and which has no description still gets no comment at all. |
| M5 | Name namespaces (choice 9) | Table names are schema-wide and must all differ. `FOREIGN KEY` constraint names are schema-wide and must all differ. Index names and `PRIMARY KEY` / `UNIQUE` constraint names are per table, and C6 already covers those. | The exact inverse of PostgreSQL, which the Preconditions section already half states. InnoDB keeps foreign key names in a per-database namespace and answers a collision with ERROR 1826, `Duplicate foreign key constraint name`; index names live in the table, so two tables may each carry an `ix_created`, and an index may even take a name a table already has. Not PostgreSQL's walk reused on the theory that being stricter than the target is safe: it would refuse a legal MySQL document and pass an illegal one, in both directions at once. |
| M6 | String literals (choice 7) | The apostrophe is doubled **and so is the backslash**, in one pass. No `SET` of any kind is emitted. | MySQL treats a backslash inside a string as an escape character unless `NO_BACKSLASH_ESCAPES` is in `sql_mode`, which is not the default: `'C:\tmp'` reaches the database as `C:` followed by a tab. A Windows path in a `description` is the obvious way to meet it, and the failure is silent — the script runs and stores the wrong text. One pass, because two chained passes would double what the first had just produced. The only literals this generator writes are the comment texts of choice 7, so this row is where the escaping is settled. Not `SET sql_mode='NO_BACKSLASH_ESCAPES'` at the top of the script, which is the same answer `internal/export/ddl/text.go` already gives for PostgreSQL: one setting invites `character_set_client` and `time_zone` after it, and a script whose effect depends on a session variable is a script whose effect depends on where it was run from. |
| M7 | Type parameters (choice 5) | Postfix only: `DATETIME(3)`, `DECIMAL(10,2)`, `VARCHAR(255)`. A trailing `UNSIGNED`, `ZEROFILL` or `UNSIGNED ZEROFILL` is split off the type name, the parameters are rendered against what remains, and the attribute is put back after them: `DECIMAL(10,2) UNSIGNED`, `BIGINT UNSIGNED`. `TINYINT` keeps a `length`; every other integer type drops one. | Choice 5 is unchanged — the type name is passed through as written and there is no per-system catalogue — so this row decides only where the parenthesis goes. MySQL has no analogue of the infix `TIMESTAMP(3) WITH TIME ZONE` that PostgreSQL needs, and the attribute suffix is the same problem from the other end: `DECIMAL UNSIGNED(10,2)` is ERROR 1064, and the schema's `columnType` pattern permits the space that makes `BIGINT UNSIGNED` a legal value in the first place. The integer display width has been deprecated since 8.0.17 and `mysqldump` no longer writes one, so `INT(11)` would be emitting a construct the server is removing; `tinyint(1)` it still writes, because that is how a boolean is stored, and `skills/db-design/references/types.md` tells authors to write `"length": 1` for exactly that. |
| M8 | What the type string alone decides (choices 5, 9) | Refused: `VARCHAR` and `VARBINARY` with no `length`; a `primaryKey`, `uniqueKeys` entry or index over a column whose type is in the `TEXT`, `BLOB` or `JSON` family. Emitted as written: `ENUM` and `SET`, for which the format has nowhere to put a value list. | All three are decided by the type name alone, and they part on one question: can a document `jjf import mysql` produced ever be in this state? A bare `VARCHAR` is ERROR 1064 and `mysqldump` always writes the length, so the refusal cannot break the round trip and it names two spellings rather than starting a type catalogue. A key over a `TEXT` column is ERROR 1170, and ERROR 3152 for `JSON`; the format has nowhere for the prefix length or the generated column that would make it legal, so there is no correct DDL to write. `ENUM` is the opposite: the format has nowhere for a value list, so *every* `ENUM` column is in this state including one the importer has just produced, and refusing it would make the round trip fail by construction. It is the MySQL face of the limitation choice 5 already states for a user-defined type — sharper only in that MySQL answers a bare `ENUM` at parse time rather than on execution. |

Two things MySQL takes and then does not do. Neither is a refusal, because the
script is accepted and a refusal would break the round trip; both are drift the
harness reports and does not gate.

* A named `primaryKey` is written as `CONSTRAINT <name> PRIMARY KEY (...)`,
  which MySQL accepts and then calls `PRIMARY`, because that is what it calls
  every primary key. The name is emitted anyway — the document said it — and it
  does not come back.
* `SET DEFAULT` as a referential action is written as the document spells it.
  MySQL 8.0 accepts it, records it, and `mysqldump` writes it back, so the
  document survives a round trip unchanged; InnoDB simply never performs the
  action, and a delete that the document says would set the default is refused
  with ERROR 1451 instead. That is a fact about the storage engine at run time
  rather than about the DDL, and this generator writes DDL.

## Preconditions checked before writing

The generator refuses a document that would produce DDL the database rejects.
Everything a document can get wrong here fails loudly in PostgreSQL — the value
of checking first is that nothing is written and nothing is half-created, not
that a silent corruption is avoided.

The list below is PostgreSQL's. MySQL's is M3, M5 and M8 above, and the two are
not one list with a different database's name on it: the namespaces are
inverted, and each dialect has a rule the other has no analogue for.

Already implemented, in `internal/check` and reported by `jjf validate`:

* C1–C4 — key, index and foreign key columns exist; the referenced table is
  defined; the column counts agree; the referenced column set is constrained
  unique by a primary key, a unique key or a unique index
* C5 — no primary key column is `nullable`. This is the one inconsistency
  PostgreSQL does *not* report: it silently forces such a column to `NOT NULL`,
  leaving the document and the database disagreeing
* C6 — no duplicate column, constraint or index name within one table
* C7–C8 — `default` is non-empty and reads as a SQL expression

Checked by the generator rather than by `jjf validate`, because each is a
statement about PostgreSQL rather than about the document — which is why
`AGENTS.md` excludes them from `validate` by name and why they live here:

* Table names, index names and the names of `PRIMARY KEY` and `UNIQUE`
  constraints occupy a single namespace per schema, and must all differ. Those
  four and no more: PostgreSQL keeps them in `pg_class`, which also holds
  sequences and views and is unique per schema, and a `PRIMARY KEY` or `UNIQUE`
  constraint is backed by an index that lives there. Foreign key constraint
  names are excluded, because they live in `pg_constraint`, which is unique per
  TABLE: two tables may each carry a constraint called `fk_parent` and
  PostgreSQL accepts both. C6 already covers the per-table case. Table names
  belong in the list because `internal/check` deliberately does not report
  duplicate table names, so nothing else would catch a document that defines
  `orders` twice. MySQL, by contrast, scopes index names per table.
* A column that is `autoIncrement` does not also carry a `default`. PostgreSQL
  refuses "both default and identity specified for column", so choice 3 and
  choice 6 together would emit a statement the database rejects.
* A column that is `autoIncrement` is not `nullable`. This is C5's silent case
  for a column outside the primary key: PostgreSQL accepts it and makes the
  identity column `NOT NULL` anyway, leaving the document and the database
  disagreeing.

Two limits are worth stating. A constraint the document leaves unnamed gets its
name from PostgreSQL (`t_pkey`, `t_col_key`), and that name is outside the
namespace check because the document never says it; predicting PostgreSQL's
naming is not attempted. And a `default` is read as an expression, never
evaluated, so a cast to a type nothing creates is caught by the database and not
here.

## What is deliberately not emitted

The schema has no place for these, so the generator cannot produce them:
`CHECK` constraints, `CREATE TYPE`, schemas other than the default, collations,
partial and expression indexes, index methods, `DEFERRABLE`, storage parameters,
partitioning, and row-level security.

MySQL's list is the same idea against a different grammar: no `CHECK`
constraints, no triggers, no views, no table options — engine, character set,
collation, row format, partitioning — and no `ON UPDATE CURRENT_TIMESTAMP`. The
format has nowhere for any of them, and a generated script that guessed at an
engine or a collation would be writing a design decision nobody made. A
`mysqldump` of the database the script created therefore always carries table
options the document never wrote, which is drift the round trip reports and does
not gate.

One of these has a sharp edge worth stating plainly. Column types are opaque
strings, so a document that names a user-defined type — an enum or a domain
imported from PostgreSQL — produces DDL that references a type no statement in
the file creates. The DDL is syntactically valid and fails on execution. This is
a limitation of the format, not a bug in the generator, and closing it would
mean teaching the schema about type definitions.

## Verification

Golden files alone are insufficient: they prove only that the generator emits
what it emitted. The real oracle is the database.

```
document.json → jjf export ddl → live PostgreSQL → pg_dump → jjf import → document.json
```

The comparison is at the document level, never at the SQL level: `pg_dump`
writes a random token into its `\restrict` / `\unrestrict` lines, so two dumps of
the same schema never match byte for byte.

Two properties this pins that nothing else can:

* **C5's silent case.** A `nullable` primary key column comes back as
  `nullable: false`, so the round trip disagrees with the source document.
* **Quasi-idempotence.** A hand-written quoted literal acquires an explicit cast
  on the first pass — `'now'` becomes `'now'::text` — and is stable from the
  second onwards. Numeric and keyword constants never move. The test should
  assert the second pass equals the first, not that the first equals the input.

This needs live PostgreSQL in CI, which is the expensive part of adopting DDL
generation and the reason the infrastructure is worth building before the
generator. It is wired into the `verify` leg of
`.github/workflows/pg-fixtures.yml`, which already starts a real server per
PostgreSQL major — so the generator is answered by every renderer the importer
claims to read, and not by one of them. The documents are the three under
`internal/export/ddl/testdata/` that the golden test already calls the shapes
the generator has to survive.

Applying the generated script under `psql -v ON_ERROR_STOP=1` is a gate in its
own right, and the first fact no golden file could ever establish: that what the
generator emits is something a database accepts.

`edge.json` is applied after a per-document prelude that creates its
user-defined type. That is the remedy the section above prescribes — the type
has to exist in the target database — and not a softening of the rule that the
generator emits no `CREATE TYPE`; a green round trip says nothing whatever about
type definitions.

The first pass against the input document is written into the job summary and
deliberately not gated. Every difference it shows belongs to PostgreSQL rather
than to jjf, so freezing them would make a PostgreSQL release a jjf failure with
no jjf change to make. Naming the known classes in the report is what buys the
value the pinning would have — a reader can tell documented drift from news —
without the false red.

MySQL passes the same gate with its own tools, and the shape is deliberately
identical so that one reader can check both:

```
document.json → jjf export ddl → live MySQL → mysqldump --no-data → jjf import mysql → document.json
```

`internal/export/ddl/testdata/roundtrip-mysql.sh` takes each MySQL fixture round
it twice, and `.github/workflows/mysql-fixtures.yml` runs that script and the
fixture regeneration against a real server, once per captured server series —
the same job shape `pg-fixtures.yml` runs once per PostgreSQL major, for the
same reason. Feeding the generated script to the `mysql` client, which stops at
the first error in batch mode and exits non-zero, is again a gate in its own
right, and again the first fact no golden file could establish.

The comparison is at the document level here too, and the first pass against the
input document is written into the job summary and deliberately not gated. Four
classes of difference are known, belong to MySQL rather than to jjf, and are
expected in pass 1: a `BOOLEAN` column comes back as `TINYINT` with `length` 1,
because that is how MySQL stores one; every foreign key acquires an index MySQL
created to back it, which the document never wrote; a named `primaryKey` comes
back unnamed, as the note closing the MySQL table says; and table options
appear — engine, charset, collation, row format — that the format has nowhere to
put. The gate is that pass 2 equals pass 1.

## Output stability policy

The `.sql` is a build artifact. It is generated, not authored; it carries a
header saying so; and the supported way to get the current one is to run the
exporter again. jjf does not promise that two releases emit identical bytes for
the same document.

That is not a licence to churn. Pin the output with golden files, treat a change
in what is emitted as a change worth a release note, and batch such changes into
one release rather than dribbling them out — the discipline `gofmt` uses, minus
the promise `gofmt` makes.

No `--compat` or output-version flag. Maintaining every past renderer costs more
than the regeneration it would save, and the header already tells a reader what
to do instead.
