# Jumpin' Json Flush

[README](https://github.com/shutx-net/jumping-json-flush/blob/main/README.md) · [日本語](index.ja.md)

**Jumpin' Json Flush** (`jjf`) keeps database design information in structured
JSON as the single source of truth and turns it into an Excel design document
people can read.

```sh
jjf import postgres schema.sql -o db-design.json
jjf validate db-design.json
jjf export xlsx db-design.json -o db-design.xlsx
```

- **[Installing jjf](install.md)** — the one liner, pinning a version, choosing a
  directory, verifying a download by hand
- **[Using jjf](usage.md)** — the three commands, the rules for `-o`, and the exit
  codes a pipeline reads
- **[The database design JSON format](db-design-format.md)** — every field, every
  rule, and the three sheets of the generated workbook

The project overview, wiring `jjf` into CI, the Agent Skill and what is out of
scope are in the
[repository README](https://github.com/shutx-net/jumping-json-flush/blob/main/README.md).
