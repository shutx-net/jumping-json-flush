# Jumpin' Json Flush

[README](https://github.com/shutx-net/jumping-json-flush/blob/main/README.md) · [日本語](index.ja.md)

**Jumpin' Json Flush** (`jjf`) keeps database design information in structured
JSON as the single source of truth and turns it into design documents people can
read: an Excel workbook, an ER diagram as SVG, and a PostgreSQL or MySQL DDL
script.

```sh
jjf import postgres schema.sql -o db-design.json   # or: jjf import mysql
jjf validate db-design.json
jjf export xlsx db-design.json -o db-design.xlsx
jjf export svg db-design.json -o er.svg
jjf export ddl db-design.json -o schema.sql
```

- **[Installing jjf](install.md)** — the one liner, pinning a version, choosing a
  directory, verifying a download by hand
- **[Using jjf](usage.md)** — every command, the rules for `-o`, and the exit
  codes a pipeline reads
- **[The database design JSON format](db-design-format.md)** — every field, every
  rule, and the three sheets of the generated workbook

The project overview, wiring `jjf` into CI, the Agent Skill and what is out of
scope are in the
[repository README](https://github.com/shutx-net/jumping-json-flush/blob/main/README.md).
