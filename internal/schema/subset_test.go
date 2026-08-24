package schema

import (
	"strings"
	"testing"

	schemadata "github.com/shutx-net/jumping-json-flush/schema"
)

func TestCompileSchemaAcceptsTheEmbeddedSchema(t *testing.T) {
	if _, err := compileSchema(schemadata.DBDesign); err != nil {
		t.Fatalf("compileSchema(DBDesign) = %v, want nil", err)
	}
}

// TestCompileSchemaRefusesUnsupportedKeywords guards the one place this
// validator is deliberately not specification compliant.
//
// A conforming implementation ignores a keyword it does not recognise. This
// one refuses it, because a keyword nobody implemented would otherwise sit in
// the schema looking like a rule while enforcing nothing at all. That refusal
// is the whole reason the schema is decoded into Go structs instead of into a
// map, and this test is what says so out loud.
func TestCompileSchemaRefusesUnsupportedKeywords(t *testing.T) {
	tests := []struct {
		keyword string
		doc     string
	}{
		{"allOf", `{"type":"object","allOf":[]}`},
		{"if", `{"type":"object","if":{}}`},
		{"const", `{"type":"string","const":"x"}`},
		{"patternProperties", `{"type":"object","patternProperties":{}}`},
		{"prefixItems", `{"type":"array","prefixItems":[]}`},
		{"unevaluatedProperties", `{"type":"object","unevaluatedProperties":false}`},
		{"maximum", `{"type":"integer","maximum":10}`},
		{"multipleOf", `{"type":"integer","multipleOf":2}`},
		// The last three also prove the refusal reaches inside $defs, inside
		// properties and inside items, which is where a keyword would most
		// plausibly be added.
		{"const", `{"$defs":{"x":{"type":"string","const":"y"}}}`},
		{"anyOf", `{"type":"object","properties":{"a":{"anyOf":[]}}}`},
		{"contains", `{"type":"array","items":{"contains":{}}}`},
	}

	for _, tt := range tests {
		t.Run(tt.keyword, func(t *testing.T) {
			_, err := compileSchema([]byte(tt.doc))
			if err == nil {
				t.Fatalf("compileSchema(%s) = nil, want an error naming %q", tt.doc, tt.keyword)
			}
			if !strings.Contains(err.Error(), "unknown field") {
				t.Errorf("error %q does not report an unknown field", err)
			}
			if !strings.Contains(err.Error(), tt.keyword) {
				t.Errorf("error %q does not name %q", err, tt.keyword)
			}
		})
	}
}

func TestCompileSchemaRefusesUnsupportedShapes(t *testing.T) {
	tests := []struct {
		name string
		doc  string
		want string // a substring of the message, or "" to assert only that it failed
	}{
		{"a type array", `{"type":["string","null"]}`, "cannot unmarshal"},
		{"the tuple form of items", `{"items":[{"type":"string"}]}`, "cannot unmarshal"},
		{"the subschema form of additionalProperties", `{"additionalProperties":{"type":"string"}}`, "cannot unmarshal"},
		{"a non-string enum", `{"enum":[1,2]}`, "cannot unmarshal"},
		{"a boolean schema", `true`, ""},
		{"a misspelt type name", `{"type":"objct"}`, `unsupported "type"`},
		{"a pattern that does not compile", `{"type":"string","pattern":"["}`, "not a valid regular expression"},
		{"a second document in the same file", `{} {}`, "invalid character after top-level value"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := compileSchema([]byte(tt.doc))
			if err == nil {
				t.Fatalf("compileSchema(%s) = nil, want an error", tt.doc)
			}
			if tt.want != "" && !strings.Contains(err.Error(), tt.want) {
				t.Errorf("error %q does not contain %q", err, tt.want)
			}
		})
	}
}

// TestCompileSchemaAcceptsAnnotationKeywords is the other half of the safety
// valve: keys that assert nothing must be accepted, not refused, or the schema
// could not carry its own title and descriptions.
func TestCompileSchemaAcceptsAnnotationKeywords(t *testing.T) {
	const doc = `{
		"$schema": "https://json-schema.org/draft/2020-12/schema",
		"$id": "https://example.com/x.json",
		"title": "a title",
		"description": "a description",
		"default": false,
		"format": "uri-reference",
		"type": "string"
	}`

	if _, err := compileSchema([]byte(doc)); err != nil {
		t.Fatalf("compileSchema(annotations) = %v, want nil", err)
	}
}

func TestCompileSchemaRefusesBrokenReferences(t *testing.T) {
	tests := []struct {
		name string
		doc  string
		want string
	}{
		{
			"a $ref naming no definition",
			`{"$ref":"#/$defs/nope","$defs":{}}`,
			"names no definition",
		},
		{
			"a remote $ref",
			`{"$ref":"https://example.com/other.json"}`,
			"does not start with",
		},
		{
			"a $ref outside $defs",
			`{"$ref":"#/properties/a","properties":{"a":{"type":"string"}}}`,
			"does not start with",
		},
		{
			"a $ref cycle",
			`{"$defs":{"a":{"$ref":"#/$defs/b"},"b":{"$ref":"#/$defs/a"}},"$ref":"#/$defs/a"}`,
			"reference cycle",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := compileSchema([]byte(tt.doc))
			if err == nil {
				t.Fatalf("compileSchema(%s) = nil, want an error", tt.doc)
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Errorf("error %q does not contain %q", err, tt.want)
			}
		})
	}

	// A chain that ends somewhere is not a cycle, and it has to be followed to
	// the end: the evaluator reads target as a plain node and never chases a
	// second hop.
	t.Run("a chain is followed to the end", func(t *testing.T) {
		const doc = `{"$ref":"#/$defs/a","$defs":{"a":{"$ref":"#/$defs/b"},"b":{"type":"string"}}}`

		root, err := compileSchema([]byte(doc))
		if err != nil {
			t.Fatalf("compileSchema(%s) = %v, want nil", doc, err)
		}
		if root.target == nil {
			t.Fatal("root.target is nil; the chain was not resolved")
		}
		if got, want := root.target.Type, "string"; got != want {
			t.Errorf("root.target.Type = %q, want %q", got, want)
		}
	})
}
