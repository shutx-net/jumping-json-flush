# AGENTS.md

## Project Overview

**Jumpin' Json Flush** (`jjf`) is a Go CLI for managing database design specifications as structured JSON and generating human-readable artifacts such as Excel workbooks.

The canonical source is JSON. Generated `.xlsx` files are derived artifacts and must never be treated as authoritative data.

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

Unless explicitly requested, do not add:

* semantic database validation
* database connections or introspection
* migration management
* ORM functionality
* DDL generation
* ER diagram generation
* Markdown export
* Excel-to-JSON conversion
* GUI functionality

## Documentation

Update documentation when CLI behavior or the specification format changes.

Keep detailed product and schema documentation outside `AGENTS.md`.
