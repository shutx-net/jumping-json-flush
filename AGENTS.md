# AGENTS.md

## Project Overview

**Jumpin' Json Flush** (`jjf`) is a Go CLI that keeps a database design as
structured JSON and derives every other representation from it.

The JSON is the part that gets authored, and it is meant to be authorable by an
AI agent. A schema-validated tree is something an agent can write, check and
revise reliably; hand-written SQL is something it can only get approximately
right, in the specific ways this tool's own checks enumerate — a foreign key
with no target, a nullable primary key column, a bare word where a string
literal belongs. Keeping the design in JSON moves those from mistakes an agent
might make into states a document cannot reach.

Everything else is derived: Excel workbooks and ER diagrams for people, DDL for
a database. The canonical source is the JSON. Every generated file — `.xlsx`,
`.dot`, `.svg`, `.sql` — is a derived artifact, must never be treated as
authoritative data, and is meant to be regenerated rather than edited or kept.

Initial scope:

* Validate database specification JSON with JSON Schema.
* Parse validated JSON into Go types.
* Generate `.xlsx` database design documents.
* Distribute as a standalone binary suitable for local and CI use.

## Core Principles

* JSON is the single source of truth.
* Keep database data separate from presentation concerns.
* Prefer simple and explicit implementations.
* Minimize third-party dependencies.
* Prefer the Go standard library where practical.
* Avoid CGO and external runtime requirements.
* Do not implement speculative future features.

## Go Implementation

Write idiomatic Go.

Prefer:

* small packages with clear responsibilities
* explicit error handling
* deterministic behavior
* standard-library functionality
* testable functions
* minimal global state

Avoid:

* unnecessary interfaces
* premature abstractions
* reflection without clear justification
* mutable package-level state
* unnecessary concurrency

Keep internal implementation under `internal/` where appropriate.

## Suggested Structure

```text
cmd/jjf/
internal/model/
internal/schema/
internal/export/xlsx/
schema/
skills/
examples/
testdata/
```

Do not create empty packages merely to match this layout.

## Testing

Add or update tests when behavior changes.

Cover at least:

* valid specifications
* invalid JSON
* JSON Schema violations
* missing required fields
* invalid enum values
* prohibited unknown properties
* successful XLSX generation
* invalid-input error handling

Use `testdata/` for representative fixtures.


## Versioning

Tool versions and specification format versions are independent.

* Tool releases follow Semantic Versioning.
* Specification documents use `formatVersion`.
* Change `formatVersion` only when the JSON format itself changes incompatibly.

## Scope

`jjf validate` checks a document against the JSON Schema and then against
itself: whether the columns named by keys and indexes exist, whether every
foreign key names a table the document defines, matches it column for column
and targets columns that table constrains to be unique, whether a primary key
column is nullable, whether one table reuses a column or constraint name, and
whether a column's default is empty or does not read as a SQL expression.
Those findings are warnings; `-strict` makes them a failure.

`jjf export` derives the artifacts. The DDL it writes creates a schema from
nothing. Evolving a schema that already exists is deliberately not in scope:
that requires knowing the state the database is currently in, which is
introspection, which is a different tool. A design that has already been applied
somewhere is changed by writing the migration by hand.

Unless explicitly requested, do not add:

* database design judgement: normalization, index strategy, type suitability,
  naming conventions, or anything else that is an opinion about a design rather
  than a statement about the document
* checks in `jjf validate` that depend on a particular database system, such as
  the uniqueness of index names across a schema. An exporter may check what its
  own target requires before it writes; `validate` speaks about the document
  alone
* database connections or introspection
* migration management. Generated DDL creates a schema from nothing; moving an
  existing schema from one state to another needs to know the state it is in
* ORM functionality
* Markdown export
* Excel-to-JSON conversion
* GUI functionality

## Documentation

Update documentation when CLI behavior or the specification format changes.

Keep detailed product and schema documentation outside `AGENTS.md`.
