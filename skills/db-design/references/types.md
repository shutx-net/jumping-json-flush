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

- `NVARCHAR MAX` spells `NVARCHAR(MAX)` without parentheses, which the pattern
  forbids. Note "MAX length" in `description` so the parentheses can be restored
  by whoever writes the SQL Server DDL by hand — `jjf export ddl` will not, since
  it generates PostgreSQL and nothing else, reading the PostgreSQL column of this
  table.
- For a type `jjf export ddl` does not recognise, the name passes through
  unchanged and its parameters follow the `length` → `precision` + `scale` →
  `precision` precedence the workbook and the ER diagram already use.
- SQLite has type affinities rather than types. Stay with `TEXT`, `INTEGER`,
  `REAL`, `BLOB` and `NUMERIC`.
- MySQL's `TINYINT` used as a boolean conventionally carries `"length": 1`.
- Oracle's `NUMBER` expresses its purpose through `precision` and `scale` — an
  integer is `"scale": 0`.
