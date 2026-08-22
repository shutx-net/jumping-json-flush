# The database design JSON format

[README](https://github.com/shutx-net/jumping-json-flush/blob/main/README.md) · [日本語](db-design-format.ja.md)

A complete example lives in
[`examples/db-design.example.json`](https://github.com/shutx-net/jumping-json-flush/blob/main/examples/db-design.example.json), and the
formal definition of the structure in
[`schema/db-design.schema.json`](https://github.com/shutx-net/jumping-json-flush/blob/main/schema/db-design.schema.json).

```json
{
  "$schema": "https://raw.githubusercontent.com/shutx-net/jumping-json-flush/main/schema/db-design.schema.json",
  "formatVersion": "1.0",
  "database": {
    "name": "ec_shop",
    "logicalName": "ECサイト",
    "dbms": "PostgreSQL"
  },
  "tables": [
    {
      "name": "users",
      "logicalName": "会員",
      "columns": [
        {
          "name": "id",
          "logicalName": "会員ID",
          "type": "BIGINT",
          "nullable": false,
          "autoIncrement": true
        },
        {
          "name": "email",
          "logicalName": "メールアドレス",
          "type": "VARCHAR",
          "length": 255,
          "nullable": false
        }
      ],
      "primaryKey": { "name": "pk_users", "columns": ["id"] },
      "uniqueKeys": [{ "name": "uq_users_email", "columns": ["email"] }]
    }
  ]
}
```

The essentials:

| Item | Rule |
| --- | --- |
| Encoding | UTF-8 (a BOM is accepted). LF line endings are recommended |
| Required at the root | `formatVersion`, `database`, `tables` |
| Required per table | `name`, `logicalName`, `columns` |
| Required per column | `name`, `logicalName`, `type`, `nullable` |
| Unknown properties | **rejected** on every object (`additionalProperties: false`) |
| Physical names | `^[A-Za-z_][A-Za-z0-9_]*$`, at most 128 characters. Japanese belongs in `logicalName` |
| Type names | Parameters may not be inlined as in `VARCHAR(30)`. Split them into `type: "VARCHAR"` plus `length: 30` |
| Defaults | `default` is SQL expression text, copied verbatim into the DEFAULT clause. A string default carries its SQL quoting (`"'pending'"`). No DEFAULT clause means the key is simply absent; an empty `""` is a warning |
| Enums | `dbms`: `PostgreSQL`, `MySQL`, `MariaDB`, `SQLite`, `Oracle`, `SQLServer`. `onUpdate` / `onDelete`: `CASCADE`, `RESTRICT`, `SET NULL`, `SET DEFAULT`, `NO ACTION` |

`dbms` is descriptive for every command except `jjf export ddl`, which requires
it and requires it to be `PostgreSQL`. Nothing else in `jjf` branches on it.

### PostgreSQL types on import

`jjf import postgres` splits a PostgreSQL type into the `type` name and the
numeric attributes the schema keeps beside it. **`varchar(255)` is a length while
`timestamp(3)` is a fractional-second precision**, which is the one sentence most
likely to save a reader an hour, and **`TIMESTAMP` and `TIMESTAMPTZ` never
collapse into one another**, because the difference changes what the stored data
means.

| PostgreSQL | `type` | Parameters |
| --- | --- | --- |
| `character varying`, `varchar` | `VARCHAR` | `length` |
| `character`, `char`, `bpchar` | `CHAR` | `length` |
| `bit` | `BIT` | `length` |
| `bit varying`, `varbit` | `BIT VARYING` | `length` |
| `numeric`, `decimal` | `NUMERIC` | `precision`, `scale` |
| `timestamp without time zone`, `timestamp` | `TIMESTAMP` | `precision` |
| `timestamp with time zone`, `timestamptz` | `TIMESTAMPTZ` | `precision` |
| `time without time zone`, `time` | `TIME` | `precision` |
| `time with time zone`, `timetz` | `TIMETZ` | `precision` |
| `interval`, `interval <fields>` | `INTERVAL` | `precision` |
| `integer`, `int`, `int4` | `INTEGER` | — |
| `bigint`, `int8` | `BIGINT` | — |
| `smallint`, `int2` | `SMALLINT` | — |
| `boolean`, `bool` | `BOOLEAN` | — |
| `double precision`, `float8`, `float` | `DOUBLE PRECISION` | — |
| `real`, `float4` | `REAL` | — |
| `serial`, `bigserial`, `smallserial` | `INTEGER`, `BIGINT`, `SMALLINT` | `autoIncrement: true` |
| `text`, `bytea`, `uuid`, `json`, `jsonb`, `date`, `money`, `inet`, … | the same name in upper case | — |
| any array (`text[]`, `character varying(30)[]`) | `TEXT ARRAY`, `VARCHAR ARRAY` | those of the element |

A user-defined type or enum keeps its name in upper case, without the `public.`
qualification pg_dump writes. A parameter the format has no room for — the field
qualifier of `interval day to second`, the arguments of a PostGIS type — is
dropped with a warning.

The rest of the command, including what it does not import, is in
[Using jjf](usage.md#import).

Writing `$schema` at the root gives you completion and warnings in editors such as
VS Code. `jjf` itself never reads the value.

**`jjf validate` checks the structure, then the document against itself**: that
the columns named by its keys and indexes exist, that every foreign key names a
table this document defines, matches it column for column and targets columns
that table constrains to be unique, that no primary key column is declared
nullable, that one table never uses the same column or constraint name twice,
and that no column declares a default that is empty or does not read as a SQL
expression. Those findings are warnings; `-strict` turns them into a failure. See
[Referential checks](usage.md#referential-checks).

**Whether the design is a good one is not checked.** Normalization, index
strategy, type suitability and naming conventions are the author's, as is the
type compatibility of the two ends of a foreign key. Duplicate *table* names
across a document — as opposed to duplicate *column* names within one table,
which are checked — and the uniqueness of index names across a schema are not
checked either.

## What the generated workbook contains

| Sheet | Contents |
| --- | --- |
| 表紙 (cover) | Database name, logical name, DBMS, table count, format version, description |
| テーブル一覧 (table list) | Physical name, logical name, description, column count and sheet name of every table |
| テーブル定義 (table definition) | One sheet per table: the column definitions, then a block each for the primary key, unique keys, foreign keys and indexes |

Notation:

- In the `NULL` and `自動採番` (auto increment) columns, `○` means yes and an empty
  cell means no
- The `長さ` (size) column holds one of `length`, `precision` or
  `precision,scale`. It stays empty for a type that declares no size
- Sheet names are truncated to 31 characters (Excel's limit), and a collision gets
  a `(2)`, `(3)`, … suffix. The table list sheet prints **the name actually
  allocated**, so truncation and numbering are visible
- Layout and colours are fixed inside `jjf` and cannot be controlled from the
  JSON
