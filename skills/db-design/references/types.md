# Recommended type names per DBMS

`type` is a free string as far as the schema is concerned, but inconsistent
spelling lowers the quality of a design document directly. Pick from the column
matching `database.dbms`, and follow the existing document when it spells a type
differently.

Back to [SKILL.md](../SKILL.md). Field rules for `type` are in
[fields.md](fields.md).

| Purpose | PostgreSQL | MySQL / MariaDB | SQLite | Oracle | SQLServer |
| --- | --- | --- | --- | --- | --- |
| Variable-length text | `VARCHAR` | `VARCHAR` | `TEXT` | `VARCHAR2` | `NVARCHAR` |
| Fixed-length text | `CHAR` | `CHAR` | `TEXT` | `CHAR` | `NCHAR` |
| Long text | `TEXT` | `TEXT` | `TEXT` | `CLOB` | `NVARCHAR MAX` |
| Boolean | `BOOLEAN` | `TINYINT` | `INTEGER` | `NUMBER` | `BIT` |
| Small integer | `SMALLINT` | `SMALLINT` | `INTEGER` | `NUMBER` | `SMALLINT` |
| Integer | `INTEGER` | `INT` | `INTEGER` | `NUMBER` | `INT` |
| Big integer | `BIGINT` | `BIGINT` | `INTEGER` | `NUMBER` | `BIGINT` |
| Exact decimal | `NUMERIC` | `DECIMAL` | `NUMERIC` | `NUMBER` | `DECIMAL` |
| Floating point | `DOUBLE PRECISION` | `DOUBLE` | `REAL` | `BINARY_DOUBLE` | `FLOAT` |
| Date | `DATE` | `DATE` | `TEXT` | `DATE` | `DATE` |
| Timestamp | `TIMESTAMP` | `DATETIME` | `TEXT` | `TIMESTAMP` | `DATETIME2` |
| Timestamp with zone | `TIMESTAMP WITH TIME ZONE` | `TIMESTAMP` | `TEXT` | `TIMESTAMP WITH TIME ZONE` | `DATETIMEOFFSET` |
| Binary | `BYTEA` | `VARBINARY` | `BLOB` | `BLOB` | `VARBINARY` |
| UUID | `UUID` | `CHAR` | `TEXT` | `RAW` | `UNIQUEIDENTIFIER` |
| JSON | `JSONB` | `JSON` | `TEXT` | `CLOB` | `NVARCHAR MAX` |

Notes:

- **Two of these columns are load-bearing.** `jjf export ddl` reads the column
  matching `database.dbms`, and the two dialects it writes are **PostgreSQL** and
  **MySQL**. For the other four it writes nothing at all and exits 2, so their
  columns are advice for whoever writes that DDL by hand.
- MariaDB shares the MySQL vocabulary, which is why they share a column, but a
  MariaDB document is **not** exported: it is a `dbms` value of its own, with no
  importer and no live-server check behind it. The reason is the verification
  and not the types.
- `NVARCHAR MAX` spells `NVARCHAR(MAX)` without parentheses, which the pattern
  forbids. Note "MAX length" in `description` so the parentheses can be restored
  by whoever writes the SQL Server DDL by hand.
- For a type `jjf export ddl` does not recognise, the name passes through
  unchanged and its parameters follow the `length` → `precision` + `scale` →
  `precision` precedence the workbook and the ER diagram already use.
- SQLite has type affinities rather than types. Stay with `TEXT`, `INTEGER`,
  `REAL`, `BLOB` and `NUMERIC`.
- Oracle's `NUMBER` expresses its purpose through `precision` and `scale` — an
  integer is `"scale": 0`.

Writing for MySQL, four of those rules are about what the generated DDL will and
will not run:

- **`VARCHAR` and `VARBINARY` need a `length`.** MySQL has no default for either,
  so `jjf export ddl` refuses the document rather than writing a column the server
  answers with a syntax error. `CHAR`, `BINARY`, `BIT` and `DECIMAL` all have
  server defaults and are written without one.
- **`ENUM` and `SET` cannot be expressed.** The format has nowhere to put a value
  list, so `"type": "ENUM"` produces `ENUM` with no values — a script that parses
  and then fails on execution. That is the MySQL face of the limitation the
  PostgreSQL column has for a user-defined type. Use `VARCHAR` and a `CHECK` kept
  outside the document, or accept that the DDL needs a hand afterwards.
- **`TINYINT` with `"length": 1` is the boolean idiom**, it is what an import of a
  MySQL database produces, and `TINYINT` is the only integer that keeps its
  length: every other one drops it, because a display width is deprecated and
  `mysqldump` no longer writes one.
- **`UNSIGNED` is part of the type name** — `"type": "BIGINT UNSIGNED"` — which
  the schema's pattern allows because it permits spaces. The generated DDL puts
  it after the parameters: `DECIMAL(10,2) UNSIGNED`.
