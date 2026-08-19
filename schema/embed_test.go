package schemadata

import (
	"encoding/json"
	"testing"
)

func TestDBDesignIsValidSchemaDocument(t *testing.T) {
	if len(DBDesign) == 0 {
		t.Fatal("DBDesign is empty; the schema was not embedded")
	}

	var doc map[string]any
	if err := json.Unmarshal(DBDesign, &doc); err != nil {
		t.Fatalf("embedded schema is not valid JSON: %v", err)
	}

	if got, want := doc["$id"], DBDesignURL; got != want {
		t.Errorf("$id = %v, want %v", got, want)
	}
	if got, want := doc["$schema"], "https://json-schema.org/draft/2020-12/schema"; got != want {
		t.Errorf("$schema = %v, want %v", got, want)
	}
	if _, ok := doc["$defs"]; !ok {
		t.Error("embedded schema has no $defs")
	}
}
