# Editing recipes

Worked JSON for each kind of edit, plus the one recipe that starts a document
instead of changing it. Every recipe ends the same way: run
`jjf validate <input.json>` and only stop once it passes. A `warning:` line does
not fail the run, but it is worth fixing all the same — see
[errors.md](errors.md).

Back to [SKILL.md](../SKILL.md). Allowed values are in [fields.md](fields.md).

## Bootstrap from a PostgreSQL dump

When the database already exists, do not transcribe it by hand. Dump the schema
and import it:

```sh
pg_dump --schema-only mydb > schema.sql
jjf import postgres schema.sql -o db-design.json
```

The input is a **file**, not a connection: `jjf` never talks to a server. Any
`pg_dump --schema-only` output from major 13 to 18 works.

Two things about the result need your attention.

**Logical names are placeholders.** `logicalName` is required on every table and
column, and a dump has no such thing. `jjf` fills it from `COMMENT ON` where the
database has one and otherwise repeats the physical name, so `"logicalName":
"created_at"` means "nobody has written one yet", not "the logical name is
created_at". Replacing those with real names is the first edit after an import.

**Warnings are not noise.** Anything the design format cannot hold — CHECK
constraints, partial index predicates, `INCLUDE` columns, exclusion constraints,
generated columns — is reported on standard error and then dropped. The document
is therefore a narrower description of the database than the database itself.
Read every warning before treating the JSON as complete, and say what was lost.

Useful flags:

| Flag | Effect |
| --- | --- |
| `-schema <name>` | Import that schema instead of `public`. One schema per document; identifiers cannot carry a `schema.` prefix |
| `-database <name>` | Set `database.name` explicitly instead of deriving it from the file name |
| `-strict` | Turn every warning into an error, so nothing is dropped silently |

An import that fails writes no file at all — it validates the document it built
before touching the disk — so there is never a half-written document to clean up.

## Add a table

Append to `tables`. Do not reorder existing entries — array order becomes sheet
order in the workbook.

```json
{
  "name": "shipments",
  "logicalName": "Shipment",
  "description": "Shipments made against an order. One order may ship more than once.",
  "columns": [
    {
      "name": "id",
      "logicalName": "Shipment ID",
      "type": "BIGINT",
      "nullable": false,
      "autoIncrement": true
    },
    {
      "name": "order_id",
      "logicalName": "Order ID",
      "type": "BIGINT",
      "nullable": false
    },
    {
      "name": "shipped_at",
      "logicalName": "Shipped at",
      "type": "TIMESTAMP WITH TIME ZONE",
      "nullable": false,
      "default": "CURRENT_TIMESTAMP"
    }
  ],
  "primaryKey": {
    "name": "pk_shipments",
    "columns": ["id"]
  },
  "foreignKeys": [
    {
      "name": "fk_shipments_order",
      "columns": ["order_id"],
      "references": {
        "table": "orders",
        "columns": ["id"]
      },
      "onUpdate": "CASCADE",
      "onDelete": "CASCADE"
    }
  ],
  "indexes": [
    {
      "name": "ix_shipments_order_id",
      "columns": ["order_id"]
    }
  ]
}
```

Check: `name`, `logicalName` and at least one column are present, and every
column has `nullable`.

## Add a column

Insert at the **intended position** in the table's `columns` — array order becomes
row order. Appending is the default; business columns conventionally go before
audit columns such as `created_at` and `updated_at`.

```json
{
  "name": "cancelled_at",
  "logicalName": "Cancelled at",
  "description": "NULL while the order is not cancelled.",
  "type": "TIMESTAMP WITH TIME ZONE",
  "nullable": true
}
```

Adding a NOT NULL column to a table that already holds rows means deciding the
default at the same time.

```json
{
  "name": "channel",
  "logicalName": "Order channel",
  "description": "One of web, phone, store.",
  "type": "VARCHAR",
  "length": 20,
  "nullable": false,
  "default": "'web'"
}
```

## Change a column

**Change only the properties named in the request.** No incidental reformatting or
reordering.

- Widen a column: change the `length` number and nothing else
- Switch to a numeric type: change `type`, remove `length`, add `precision` and
  `scale`

```json
{
  "name": "unit_price",
  "logicalName": "Unit price",
  "type": "NUMERIC",
  "precision": 12,
  "scale": 2,
  "nullable": false,
  "default": "0"
}
```

- Make it NOT NULL: set `nullable` to `false`. Check first that the column is not
  the NULL side of a `SET NULL` foreign key, and note in `description` what
  happens to existing NULL rows
- Drop the default: **delete the `default` key**. Do not set it to `""`, which
  `jjf validate` warns about

## Remove a column

After deleting the entry from `columns`, **delete every reference to that column
name**. `jjf validate` warns about a leftover reference, but a warning leaves the
exit code successful, so the run still passes without `-strict`.

Places to check:

- `primaryKey.columns`
- `uniqueKeys[].columns`
- `foreignKeys[].columns`
- `foreignKeys[].references.columns` of other tables whose `references.table` is
  this table
- `indexes[].columns`

If `columns` would end up empty, delete the whole table — one column is the
minimum. If `primaryKey.columns` would end up empty, delete the `primaryKey`
object.

## Add a foreign key

Append to the `foreignKeys` of the referencing table. The target columns are
normally the primary key or a unique key of the target table.

```json
{
  "name": "fk_order_items_product",
  "columns": ["product_id"],
  "references": {
    "table": "products",
    "columns": ["id"]
  },
  "onUpdate": "CASCADE",
  "onDelete": "RESTRICT"
}
```

- `columns` and `references.columns` must agree in **count and order** — a
  composite key's ordering is meaningful
- Give the referencing column the same `type` as its target
- Add the referencing column first if it does not exist yet
- Index the foreign key column as well: add `ix_<table>_<columns>` to `indexes`
- Choosing `onDelete`: delete children with the parent is `CASCADE`, forbid
  deleting a parent that still has children is `RESTRICT`, clear the reference is
  `SET NULL` (which requires `"nullable": true` on that column)

## Add an index

```json
{
  "name": "ix_orders_status_ordered_at",
  "columns": ["status", "ordered_at"],
  "unique": false
}
```

- `name` is **required**
- In a composite index, put the most selective column first
- For a unique constraint use `uniqueKeys`, not an index with `unique: true`

## Add a unique key

```json
{
  "name": "uq_users_email",
  "columns": ["email"]
}
```

`name` is optional — the DBMS will invent one — but a design document meant to be
read should name it.

## Set a primary key

```json
{
  "name": "pk_order_items",
  "columns": ["order_id", "line_no"]
}
```

The order of `columns` is the key order. Every column in it must be
`"nullable": false`.
