# The database design JSON format

[README](../README.md) · [日本語](db-design-format.ja.md)

A complete example lives in
[`examples/db-design.example.json`](../examples/db-design.example.json), and the
formal definition of the structure in
[`schema/db-design.schema.json`](../schema/db-design.schema.json).

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
| Defaults | `default` is a string only. A SQL literal includes its quotes (`"'pending'"`). No DEFAULT clause means the key is simply absent |
| Enums | `dbms`: `PostgreSQL`, `MySQL`, `MariaDB`, `SQLite`, `Oracle`, `SQLServer`. `onUpdate` / `onDelete`: `CASCADE`, `RESTRICT`, `SET NULL`, `SET DEFAULT`, `NO ACTION` |

Writing `$schema` at the root gives you completion and warnings in editors such as
VS Code. `jjf` itself never reads the value.

**Validation today is structural only.** Semantic consistency is **not** checked:
whether a foreign key's target exists, whether table or column names are
duplicated, whether the columns named by a primary key or an index exist.

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
