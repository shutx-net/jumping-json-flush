package schema

// This file models the subset of JSON Schema Draft 2020-12 that
// schema/db-design.schema.json actually uses, and nothing beyond it. It is not
// a general implementation of the specification and is not meant to become
// one: it reads one schema, the one this tool embeds.
//
// There is no library behind it because AGENTS.md makes minimal dependencies
// binding, and the surface that has to be covered is small and fixed. The
// embedded schema uses fourteen validation keywords, each in one uniform
// shape; every $ref in it is a flat "#/$defs/<name>" that resolves inside the
// document; and none of the combinator, conditional or unevaluated keywords
// that make a general implementation hard appears anywhere in it. A general
// implementation would buy nothing here that this does not, and would cost two
// modules in go.mod.
//
// The keywords it implements are type, enum, $ref, properties, required,
// additionalProperties, dependentRequired, items, minItems, uniqueItems,
// minLength, maxLength, pattern and minimum. It also reads, and then ignores,
// the annotation keys $schema, $id, title, description, default and format.
// format in particular stays an annotation and is never asserted, which is
// what Draft 2020-12 says it is by default.
//
// It departs from the specification in one deliberate way. A conforming
// implementation must ignore a keyword it does not recognise, and this one
// must not. An unimplemented keyword would otherwise sit in the schema looking
// exactly like a rule while enforcing nothing, and a document that broke it
// would validate cleanly - the failure invisible precisely where a schema is
// meant to be the thing that cannot be got wrong. So the schema is decoded
// into Go structs with Decoder.DisallowUnknownFields, and an unrecognised
// keyword stops the tool at startup with the keyword named. Whoever adds a
// keyword to the schema is the person best placed to implement it or to
// withdraw it, and they find out while they are still looking at the schema.

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"math/big"
	"regexp"
	"slices"
	"strings"
)

// defsPrefix is the only $ref form this schema uses. Every reference in it
// names a definition in the same document, which is what makes resolution a
// map lookup rather than a resolver.
const defsPrefix = "#/$defs/"

// schemaNode is one node of a decoded schema document.
//
// It deliberately does NOT implement json.Unmarshaler. DisallowUnknownFields
// does not reach inside a type's own UnmarshalJSON, which receives raw bytes
// and decides for itself what to do with them, so a custom unmarshaler here
// would silently disable the safety valve for every node in the schema.
// Everything is therefore plain fields, plain maps and plain pointers, decoded
// by one decoder that has the setting on.
//
// A pointer is used wherever the zero value is a legal value of the keyword
// and nil has to be the only way of saying "absent": minItems, minLength and
// maxLength may legally be 0, additionalProperties is legally false, and
// minimum is legally 0. Type, Pattern and Ref are plain strings because the
// empty string is not a legal value of any of them, and UniqueItems is a plain
// bool because only true asserts anything.
type schemaNode struct {
	Ref string `json:"$ref"`

	// Type is a string and not []string, and Enum is []string and not []any,
	// because that is the only shape this schema uses. A type array or a
	// non-string enum then fails to decode with "cannot unmarshal ...", which
	// is the behaviour wanted: a shape nobody implemented must not look like a
	// rule that is being enforced.
	Type string   `json:"type"`
	Enum []string `json:"enum"`

	// AdditionalProperties is *bool for the same reason. All nine uses in this
	// schema are the literal false, and the subschema form fails to decode.
	Properties           map[string]*schemaNode `json:"properties"`
	Required             []string               `json:"required"`
	AdditionalProperties *bool                  `json:"additionalProperties"`
	DependentRequired    map[string][]string    `json:"dependentRequired"`

	// Items is a single subschema and not a slice, so that the tuple form -
	// which Draft 2020-12 spells prefixItems anyway - fails to decode rather
	// than being read as something it is not.
	Items       *schemaNode `json:"items"`
	MinItems    *int        `json:"minItems"`
	UniqueItems bool        `json:"uniqueItems"`

	MinLength *int   `json:"minLength"`
	MaxLength *int   `json:"maxLength"`
	Pattern   string `json:"pattern"`

	Minimum *json.Number `json:"minimum"`

	Defs map[string]*schemaNode `json:"$defs"`

	// These six are read and never used. They are here so that the schema's
	// own $schema, $id, title, descriptions, defaults and format decode
	// instead of being reported as unknown keywords; leaving them out would
	// make the safety valve fire on annotations, which assert nothing and are
	// meant to be there.
	Schema      string          `json:"$schema"`
	ID          string          `json:"$id"`
	Title       string          `json:"title"`
	Description string          `json:"description"`
	Default     json.RawMessage `json:"default"`
	Format      string          `json:"format"`

	// These three are lower case because they are not part of the document.
	// They are what compile worked out from it: the node a $ref finally leads
	// to, the compiled pattern, and the parsed minimum.
	target  *schemaNode
	re      *regexp.Regexp
	minimum *big.Rat
}

// compileSchema decodes a schema document and prepares it for evaluation.
//
// The decode is where an unimplemented keyword is caught: DisallowUnknownFields
// turns a keyword schemaNode has no field for into
// `json: unknown field "allOf"`. encoding/json names the field but not its
// location, which is enough - the keyword name finds it at once in a schema of
// this size, and a decoder that tracked locations would be a great deal of
// machinery for a message a maintainer reads once and acts on immediately.
func compileSchema(raw []byte) (*schemaNode, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()

	var root schemaNode
	if err := dec.Decode(&root); err != nil {
		return nil, fmt.Errorf("parse embedded schema: %w", err)
	}
	// Decode stops at the end of the first value, so a file carrying a second
	// document after the schema would otherwise be accepted with the second
	// half never read.
	if _, err := dec.Token(); err != io.EOF {
		return nil, errors.New("parse embedded schema: invalid character after top-level value")
	}

	if err := root.compile(&root, "#"); err != nil {
		return nil, fmt.Errorf("compile embedded schema: %w", err)
	}
	return &root, nil
}

// compile prepares one node and everything below it. loc is a human readable
// schema location - "#", then "/properties/<name>", "/$defs/<name>" and
// "/items" - and exists only so that an error names the node it came from.
func (n *schemaNode) compile(root *schemaNode, loc string) error {
	if n.Ref != "" {
		// Follow the chain to the first node that is not itself a $ref, so
		// that the evaluator never has to chase one while validating. The hop
		// limit is what turns a cycle into a startup error rather than a hang:
		// a $ref leading only to another $ref never descends into the
		// document, so nothing shrinks and the recursion would not terminate.
		for hops, ref := 0, n.Ref; ; hops++ {
			name, ok := strings.CutPrefix(ref, defsPrefix)
			if !ok {
				return fmt.Errorf("%s: $ref %q does not start with %q", loc, n.Ref, defsPrefix)
			}
			target, ok := root.Defs[name]
			if !ok {
				return fmt.Errorf("%s: $ref %q names no definition", loc, n.Ref)
			}
			if target.Ref == "" {
				n.target = target
				break
			}
			if hops >= len(root.Defs) {
				return fmt.Errorf("%s: $ref %q leads through a reference cycle", loc, n.Ref)
			}
			ref = target.Ref
		}
	}

	// A misspelt type name would otherwise match no value at all and turn the
	// node into an assertion that every document is wrong, which is a much
	// harder mistake to trace than a refusal at startup.
	if n.Type != "" {
		switch n.Type {
		case "null", "boolean", "number", "integer", "string", "array", "object":
		default:
			return fmt.Errorf("%s: unsupported \"type\": %q", loc, n.Type)
		}
	}

	if n.Pattern != "" {
		re, err := regexp.Compile(n.Pattern)
		if err != nil {
			return fmt.Errorf("%s: %q is not a valid regular expression: %w", loc, n.Pattern, err)
		}
		n.re = re
	}

	if n.Minimum != nil {
		r, ok := new(big.Rat).SetString(n.Minimum.String())
		if !ok {
			return fmt.Errorf("%s: %q is not a number", loc, n.Minimum.String())
		}
		n.minimum = r
	}

	// Both maps are walked through sorted keys so that a schema with more than
	// one broken node always reports the same one.
	for _, name := range slices.Sorted(maps.Keys(n.Properties)) {
		if err := n.Properties[name].compile(root, loc+"/properties/"+name); err != nil {
			return err
		}
	}
	for _, name := range slices.Sorted(maps.Keys(n.Defs)) {
		if err := n.Defs[name].compile(root, loc+"/$defs/"+name); err != nil {
			return err
		}
	}
	if n.Items != nil {
		if err := n.Items.compile(root, loc+"/items"); err != nil {
			return err
		}
	}
	return nil
}
