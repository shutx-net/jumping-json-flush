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

## Self-consistency warnings (exit code 0, or 2 with `-strict`)

A document that conforms to the schema is then checked against itself. Each
finding is one line on standard error, shaped `<input>: warning: <what>: <the
problem>` — `<what>` names the object, because there is no line number to give.

```text
db-design.json: warning: primary key pk_orders on table orders: names column "id", which the table declares nullable
db-design.json: warning: foreign key fk_orders_customer on table orders: references table "customers", which this document does not define
db-design.json: warning: index ix_orders_placed_at on table orders: names column "placed_at", which the table does not define
db-design.json: warning: column created_at on table orders: declares the default "now", in which "now" is a bare word; a string literal is written 'now'
```

The run still **succeeds**: standard output says `db-design.json: OK, 4
warning(s)` and the exit code is 0. `jjf validate -strict` prints the same
warnings and then fails with `jjf: 4 warning(s) with -strict` and exit code
**2, not 3** — code 3 means the document does not conform to the JSON Schema
and nothing else, and a self-consistency finding is not a schema violation.

| Message | Cause | Fix |
| --- | --- | --- |
| `names column "placed_at", which the table does not define` | a key or index names a column its table has no `columns[]` entry for | add the column, or correct the spelling; identifier case is never folded |
| `references table "customers", which this document does not define` | a foreign key points at a table this document has no entry for | add the table, or correct the name |
| `names 1 column(s) but references 2` | the two ends of a foreign key list a different number of columns | make `columns` and `references.columns` the same length, in matching order |
| `references (email) of table accounts, which no primary key, unique key or unique index there constrains to be unique` | the referenced columns are not the target's primary key, one of its unique keys, or a unique index | point the foreign key at the target's key, or add a unique key or `"unique": true` index covering those columns |
| `names column "id", which the table declares nullable` | a primary key column has `"nullable": true` | set `"nullable": false`; SQL forces a primary key column NOT NULL anyway |
| `defines column "email" more than once` | one table has two `columns[]` entries with the same `name` | remove or rename one of them |
| `declares more than one constraint or index called "pk_accounts"` | one table uses the same name for two of its `primaryKey` / `uniqueKeys[]` / `foreignKeys[]` / `indexes[]` | rename one; unnamed constraints never collide |
| `declares an empty default; omit the "default" key when the column has no DEFAULT clause` | a column has `"default": ""`, which is a DEFAULT clause with nothing in it | delete the key when there is no DEFAULT clause; write `"''"` for a default of the empty string |
| `declares the default "now", in which "now" is a bare word; a string literal is written 'now'` | an unquoted word was written where a SQL string literal was meant; in SQL it is a column reference | add the SQL quotes: `"default": "'now'"`. A function keeps its parentheses (`"now()"`); a keyword constant such as `CURRENT_TIMESTAMP` needs nothing |
| `declares the default "it's", which has an unbalanced single quote` | a quote in a `default` opens a string that is never closed — usually an apostrophe inside an unquoted word | double the apostrophe inside the literal: `"'it''s'"` |
| `declares the default "(1 + 2", which has an unbalanced parenthesis` | a `default` opens a parenthesis it never closes, or closes one it never opened | balance them |
| `declares the default "1 --", which starts a comment at "--"; the text after it is not part of the expression` | a `default` contains `--` or `/*`, which starts a SQL comment. The generated DDL writes a column on one line, so the comment would swallow the clauses written after the default — including `NOT NULL` | delete the comment; a note about the column goes in `description`, which is a field of its own. The same characters inside a string literal, as in `"'-- literal'"`, are ordinary text and are not reported |
| `declares the default "1 ;", which ends its statement at ";"; a DEFAULT is one expression` | a `default` contains `;`, which ends the statement rather than continuing the expression | delete it; a `default` is one expression and never a statement |

Whether the design is a *good* one is not checked, so do not offer normalization,
index-strategy or type advice as though `jjf` had asked for it. Duplicate table
names across a document and type compatibility across a foreign key are not
checked either. The uniqueness of index names across a schema is not checked by
`jjf validate` — it is a PostgreSQL rule rather than a statement about the
document — but `jjf export ddl` does check it before it writes; see the section
below. A `default` is
only read as an expression, never evaluated: `jjf` does not run it, does not
check it against the column's `type`, and does not object to a function it has
never heard of.

## Failures before validation (exit code 2)

| Output | Cause | Fix |
| --- | --- | --- |
| `jjf: db-design.json: line 5, column 4: invalid character '}' looking for beginning of object key string` | invalid JSON syntax, such as a trailing comma | fix the reported line and column |
| `jjf: open db-design.json: no such file or directory` | wrong path | check the path |
| `jjf: unsupported formatVersion "2.0"; this jjf supports 1.x - please upgrade jjf` | the document uses a newer format than this build | upgrade `jjf`. **Never rewrite the document to get around this** |
| `jjf: unsupported format "csv"; supported formats: xlsx, svg, ddl` | an export format that does not exist | the formats are `xlsx`, `svg` and `ddl` |
| `jjf: validate takes exactly one input file, got 0` | no input path given | pass the path |
| `jjf: refusing to write a workbook to the terminal; redirect standard output or pass -o <file>` | `-o -` with a terminal on standard output | redirect the output or pass a file path |

## `jjf export ddl` refusals (exit code 2)

`ddl` is the only export that refuses a document, and it writes nothing when it
does. `xlsx` and `svg` render the same document happily, because a slightly
broken document still makes a useful workbook and a useful diagram. See
[ddl-output.md](ddl-output.md).

The dialect named in the summary line is the document's own — `PostgreSQL DDL
generation` for a PostgreSQL document and `MySQL DDL generation` for a MySQL
one — so match the shape of the line and not one spelling of it. The findings
below the summary differ by dialect too, because each states a fact about one
database system.

| Output | Cause | Fix |
| --- | --- | --- |
| `jjf: ddl export needs the document to name its target; add "dbms": "PostgreSQL" or "MySQL" to "database"` | `database.dbms` is absent; this is the only command that requires it | add the value that really is the target |
| `jjf: ddl export supports PostgreSQL, MySQL; this document names "MariaDB"` | the document targets one of the four systems `jjf` writes no DDL for | say so plainly. Do **not** offer the MySQL script as near enough for MariaDB, and do not offer PostgreSQL SQL and hope |
| `db-design.json: error: <finding>` followed by `jjf: 2 problem(s) prevent <dialect> DDL generation` | the document contradicts itself | one line per problem, in the shapes listed above and below. Run `jjf validate` and fix what it reports |

PostgreSQL only:

| Output | Cause | Fix |
| --- | --- | --- |
| `db-design.json: error: table order_items: declares index "ix_created", a name already used by index ix_created on table orders; PostgreSQL puts tables, indexes and the indexes behind PRIMARY KEY and UNIQUE in one namespace per schema` | two objects share a name PostgreSQL keeps in one schema-wide namespace: tables, indexes, `PRIMARY KEY` and `UNIQUE` names | rename one. Foreign key names are per table and may repeat |
| `db-design.json: error: table orders: is the second table this document calls "orders"; PostgreSQL cannot create two tables of one name in a schema` | the document defines one table name twice; `jjf validate` does not report this | rename or remove one |
| `db-design.json: error: column id on table orders: is autoIncrement and also declares a default; PostgreSQL refuses a column that is both an identity column and has a DEFAULT` | a column carries `autoIncrement` and `default` | drop one of the two |
| `db-design.json: error: column id on table orders: is autoIncrement and declared nullable; PostgreSQL makes an identity column NOT NULL, so the database would not match the document` | an `autoIncrement` column says `"nullable": true` | set `"nullable": false` |
| `db-design.json: error: column id on table orders: is autoIncrement and declares type "NUMERIC"; PostgreSQL makes an identity column only of smallint, integer or bigint` | an `autoIncrement` column declares any other type | change the type, or drop `autoIncrement` and give the column a `default` instead |
| `db-design.json: error: table order_items_xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx: has a name of 66 bytes; PostgreSQL truncates an identifier to 63 bytes, so the object it creates would not be the one this document names` | a table, column, constraint or index name is longer than 63 bytes; the schema allows 128 | shorten it. PostgreSQL would not fail, it would create an object of another name |

MySQL only. The namespaces are the mirror image of PostgreSQL's: table names are
schema-wide in both, but index names are **per table** here and foreign key names
are **schema-wide**, which is the opposite of PostgreSQL on both counts.

| Output | Cause | Fix |
| --- | --- | --- |
| `db-design.json: error: table orders: is the second table this document calls "orders"; MySQL cannot create two tables of one name in a schema` | the document defines one table name twice; `jjf validate` does not report this | rename or remove one |
| `db-design.json: error: table order_items: declares foreign key "fk_parent", a name already used by foreign key fk_parent on table orders; InnoDB keeps foreign key names in one namespace per schema` | two foreign keys share a name; this is what PostgreSQL permits and MySQL does not | rename one, conventionally after the referencing table |
| `db-design.json: error: column spare on table counters: is autoIncrement, and so is column "id" on the same table; MySQL allows one AUTO_INCREMENT column per table` | two `autoIncrement` columns in one table | keep one; the other is an ordinary column |
| `db-design.json: error: column seq on table sequences: is autoIncrement and leads no key; MySQL needs it to be the first column of this table's primaryKey or of one of its uniqueKeys, and an entry in indexes cannot serve because the script creates it after the table` | an `autoIncrement` column leads no key | make it the first column of `primaryKey` or of a `uniqueKeys` entry. An `indexes` entry does not count |
| `db-design.json: error: column id on table tallies: is autoIncrement and also declares a default; MySQL refuses a default on an AUTO_INCREMENT column` | a column carries `autoIncrement` and `default` | drop the `default` |
| `db-design.json: error: column id on table drafts: is autoIncrement and declared nullable; MySQL makes an AUTO_INCREMENT column NOT NULL, so the database would not match the document` | an `autoIncrement` column says `"nullable": true` | set `"nullable": false` |
| `db-design.json: error: table documents: declares index "ix_documents_body" over column "body", whose type is TEXT; MySQL refuses such a column in a key without a prefix length, which the document has nowhere to put` | a key or index covers a `TEXT`, `BLOB` or `JSON` column | drop the index, or index a `VARCHAR` column beside it. A prefix length cannot be expressed |
| `db-design.json: error: column name on table labels: declares type "VARCHAR" and no length; MySQL has no default length for it, so the column would be a syntax error rather than a column` | `VARCHAR` or `VARBINARY` with no `length` | add `"length"`. `CHAR`, `BINARY`, `BIT` and `DECIMAL` have server defaults and need none |
| `db-design.json: error: column colour on table themes: declares the default "1 #", in which "#" starts a comment; MySQL reads one to the end of the line, which is where the generated column definition continues` | a `default` contains `#` outside a string literal. `jjf validate` does not report this one, because `#` is a comment in MySQL and an operator in PostgreSQL — `--`, `/*` and `;` are reported there for every dialect | delete it. A `#` inside a string literal, as in `"'#ff0000'"`, is ordinary text and is not reported |
| `db-design.json: error: column id on table tallies: is autoIncrement and declares type "DECIMAL"; MySQL auto-increments only its integer and floating-point types` | an `autoIncrement` column declares any other type | change the type. `DECIMAL` and `NUMERIC` are refused; `FLOAT`, `DOUBLE` and `BOOLEAN` are not |
| `db-design.json: error: table order_items: declares foreign key "fk_orders" referencing (b, a) of table "orders"; MySQL needs an index on that table whose leading columns are those, in that order, and its primaryKey, uniqueKeys and indexes all begin differently` | a composite foreign key names the referenced columns in another order than the target's key declares them. `jjf validate` compares them as a set, which is right for PostgreSQL | reorder `references.columns`, or add an `indexes` entry on the referenced table in that order |
| `db-design.json: error: column body on table documents: declares type "TEXT" and the default 'x'; MySQL gives a BLOB, TEXT, GEOMETRY or JSON column no default unless it is written as a parenthesised expression, as in ('x')` | a plain `default` on one of those four families | parenthesise it, as in `"default": "('x')"`, or drop it |
| `db-design.json: error: column due on table tasks: declares the default "CURRENT_DATE", which MySQL takes only in parentheses; write it as (CURRENT_DATE), because an unparenthesised DEFAULT admits a literal, CURRENT_TIMESTAMP, LOCALTIME, LOCALTIMESTAMP and NOW() and nothing else` | a `default` begins with a bare word MySQL's `DEFAULT` grammar does not admit. `jjf validate` accepts it, because it is a niladic constant in other systems | parenthesise it, as the message shows |
| `db-design.json: error: column note on table orders: needs a COMMENT of 2256 characters, which is its logicalName and its description joined; MySQL stores at most 1024 characters on a column` | the joined comment is longer than MySQL stores: 1024 characters on a column, 2048 on a table | shorten `description`, or `logicalName`. Both fields can be inside the schema's own limits and still join into one comment that is not |
| `db-design.json: error: table a_name_of_more_than_sixty_four_characters_yyyyyyyyyyyyyyyyyyyyyyy: has a name of 65 characters; MySQL refuses an identifier longer than 64 characters` | a table, column, constraint or index name is longer than 64 characters; the schema allows 128 | shorten it |

Both dialects:

| Output | Cause | Fix |
| --- | --- | --- |
| `db-design.json: error: column note on table orders: carries U+0000 in its description; PostgreSQL cannot store that character in text at all, so the COMMENT statement would be rejected with every table already created` | `logicalName` or `description` contains U+0000. The schema puts no restriction on the characters of either field | remove it. No other control character is refused: they reach the database and come back unchanged |
| `db-design.json: error: column note on table orders: carries U+0000 in its description; the mysql client refuses to send a statement containing that character` | the same field under MySQL. The clause differs because the wall does: MySQL's own text types hold the character and its client will not send it | remove it |

`SET DEFAULT` as a referential action produces **no** message and is not a
refusal: MySQL 8 accepts the clause, stores it and dumps it back, and InnoDB
simply never performs it. An `ENUM` or a `SET` without its values is not a
refusal either — that script parses here and fails on the server. Both are in
[ddl-output.md](ddl-output.md) under what the format cannot hold.

Two more failures reach the server rather than the refusal, because deciding
them needs a per-system type catalogue `jjf` does not keep: two ends of a
foreign key whose types are incompatible, and a `length` or `precision` outside
the bound its type carries. `VARCHAR(70000)` and `NUMERIC(2000)` are rejected
when the script runs.

## `jjf import` failures (exit code 2)

`import` reads a dump **file** and never a server. It skips what it cannot map
in silence, warns about what it maps but cannot hold, and fails outright only
when the document it would write is not one it could then read back.

| Output | Cause | Fix |
| --- | --- | --- |
| `jjf: unsupported dialect "oracle"; supported dialects: postgres, mysql` | a dialect `jjf` has no importer for | the two are `postgres`, for `pg_dump --schema-only`, and `mysql`, for `mysqldump --no-data` |
| `jjf: -schema does not apply to the mysql dialect: a mysql dump holds one database, which is what the document describes` | `-schema` on a MySQL import | drop the flag. It belongs to `postgres` alone, where a database holds many schemas |
| `jjf: schema.sql: line 6: table name "order-items" cannot be represented in a jjf document: names must match ^[A-Za-z_][A-Za-z0-9_]*$ and be at most 128 characters` | MySQL allows almost any character inside backticks, and the design format does not | rename the object in the database, or leave it out of the document. **Never** rename it in the JSON alone: the document would then describe a database that does not exist |
| `jjf: schema.sql: database name "shop-2024" cannot be represented in a jjf document: names must match ^[A-Za-z_][A-Za-z0-9_]*$ and be at most 128 characters; pass -database <name>` | the dump's own database name is not a legal identifier | do what the message says: `-database <name>` |
| `jjf: schema.sql: line 24: expected "," or ")" in table definition, got end of statement` | the file is truncated or is not a dump | check the file; `import` never guesses at a repair |
| `jjf: 11 warning(s) with -strict` | `-strict` and the dump carries something the format cannot hold | read the warnings. Without `-strict` the document is still written and each warning names a line |

A warning is not a failure. `ON UPDATE CURRENT_TIMESTAMP is not represented`,
`parameters of type bigint are not represented` and
`dump was produced by MySQL server 5.7.44-log; jjf supports 8 and may misread
this file` all leave a document behind; they say what the format could not carry
across, and each names the line it came from.

## Output failures (exit code 4)

| Output | Cause | Fix |
| --- | --- | --- |
| `jjf: cannot create output file: /nonexistent-dir/x.xlsx: no such file or directory` | the output directory does not exist | create the directory, or choose another path |

## Encoding

`jjf` accepts a UTF-8 BOM, but other tools choke on one. Save documents as
**UTF-8 without BOM, with LF line endings**.
