// Package schemadata exposes the authoritative jjf JSON Schema files.
//
// It lives beside the schema itself because //go:embed cannot reach above its
// own directory, and it is named schemadata so that it does not collide with
// internal/schema at import sites. It must not import anything except embed.
package schemadata

import _ "embed"

// DBDesign is the authoritative JSON Schema for jjf database design
// documents, embedded verbatim from schema/db-design.schema.json.
//
//go:embed db-design.schema.json
var DBDesign []byte

// DBDesignURL is the canonical identifier of the embedded schema. It matches
// the schema's own $id and is used as the resource URL when the schema is
// registered with a JSON Schema compiler.
const DBDesignURL = "https://raw.githubusercontent.com/shutx-net/jumping-json-flush/main/schema/db-design.schema.json"
