// Package model holds the Go types that mirror schema/db-design.schema.json
// one for one, plus decoding of a database design document into them.
//
// The JSON Schema is the single source of truth: these types follow it, never
// the other way around. Whether a field is a pointer is decided by one rule
// only - whether "absent" and the zero value differ in meaning.
package model

// CurrentFormatVersion is the format version jjf WRITES into a document it
// generates. SupportedFormatMajor in decode.go is the version jjf READS; the
// two must keep agreeing on the major, which the tests assert.
const CurrentFormatVersion = "1.0"

// Document is the root of a jjf database design document.
type Document struct {
	// Schema mirrors the optional "$schema" reference that editors use for
	// completion. jjf ignores its value, but the field must exist so that
	// the model does not drift from the schema.
	Schema string `json:"$schema,omitempty"`

	FormatVersion string   `json:"formatVersion"`
	Database      Database `json:"database"`
	Tables        []Table  `json:"tables"`
}

// DBMS is a target database management system. The schema constrains it to the
// values below.
type DBMS string

// Supported database management systems.
const (
	DBMSPostgreSQL DBMS = "PostgreSQL"
	DBMSMySQL      DBMS = "MySQL"
	DBMSMariaDB    DBMS = "MariaDB"
	DBMSSQLite     DBMS = "SQLite"
	DBMSOracle     DBMS = "Oracle"
	DBMSSQLServer  DBMS = "SQLServer"
)

// ReferentialAction is a foreign key referential action. The schema constrains
// it to the values below.
type ReferentialAction string

// Referential actions a foreign key may declare.
const (
	ActionCascade    ReferentialAction = "CASCADE"
	ActionRestrict   ReferentialAction = "RESTRICT"
	ActionSetNull    ReferentialAction = "SET NULL"
	ActionSetDefault ReferentialAction = "SET DEFAULT"
	ActionNoAction   ReferentialAction = "NO ACTION"
)

// Database describes the target database itself.
type Database struct {
	Name string `json:"name"`

	// Optional descriptive fields. Absent and empty render identically, so a
	// plain string loses nothing.
	LogicalName string `json:"logicalName,omitempty"`
	Description string `json:"description,omitempty"`
	DBMS        DBMS   `json:"dbms,omitempty"`
}

// Table is a single table definition.
type Table struct {
	Name        string `json:"name"`
	LogicalName string `json:"logicalName"`
	Description string `json:"description,omitempty"`

	Columns []Column `json:"columns"`

	// PrimaryKey is a pointer so that a table without a primary key is
	// explicitly representable rather than an empty struct.
	PrimaryKey  *PrimaryKey  `json:"primaryKey,omitempty"`
	UniqueKeys  []UniqueKey  `json:"uniqueKeys,omitempty"`
	ForeignKeys []ForeignKey `json:"foreignKeys,omitempty"`
	Indexes     []Index      `json:"indexes,omitempty"`
}

// Column is a single column definition.
type Column struct {
	Name        string `json:"name"`
	LogicalName string `json:"logicalName"`
	Description string `json:"description,omitempty"`
	Type        string `json:"type"`

	// Optional numeric attributes. Pointers because "absent" must render as an
	// empty cell, which differs from an explicit 0.
	Length    *int `json:"length,omitempty"`
	Precision *int `json:"precision,omitempty"`
	Scale     *int `json:"scale,omitempty"`

	// Required by the schema, therefore always present: a plain bool is safe.
	Nullable bool `json:"nullable"`

	// Optional. Pointer because "absent" (no DEFAULT clause) differs from the
	// empty string (DEFAULT '').
	Default *string `json:"default,omitempty"`

	// Optional with a schema default of false: absent and false mean the same
	// thing, so a plain bool loses no information.
	AutoIncrement bool `json:"autoIncrement,omitempty"`
}

// PrimaryKey is a primary key constraint. Name is optional because most
// database systems generate one automatically.
type PrimaryKey struct {
	Name    string   `json:"name,omitempty"`
	Columns []string `json:"columns"`
}

// UniqueKey is a unique key constraint.
type UniqueKey struct {
	Name    string   `json:"name,omitempty"`
	Columns []string `json:"columns"`
}

// ForeignKey is a foreign key constraint.
type ForeignKey struct {
	Name       string    `json:"name,omitempty"`
	Columns    []string  `json:"columns"`
	References Reference `json:"references"`

	OnUpdate ReferentialAction `json:"onUpdate,omitempty"`
	OnDelete ReferentialAction `json:"onDelete,omitempty"`
}

// Reference is the target of a foreign key.
type Reference struct {
	Table   string   `json:"table"`
	Columns []string `json:"columns"`
}

// Index is a secondary index. The schema requires a name because an index is
// always addressed by one in DDL and in the generated documents.
type Index struct {
	Name    string   `json:"name"`
	Columns []string `json:"columns"`
	Unique  bool     `json:"unique,omitempty"`
}
