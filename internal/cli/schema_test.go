package cli

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

// TestSchemaMatchesItemJSON is the drift guard: every serialisable itemJSON
// field must appear in the schema's properties, and vice versa. Add a field to
// the DTO without describing it and this fails.
func TestSchemaMatchesItemJSON(t *testing.T) {
	want := map[string]bool{}
	rt := reflect.TypeOf(itemJSON{})
	for i := 0; i < rt.NumField(); i++ {
		tag := rt.Field(i).Tag.Get("json")
		name, _, _ := strings.Cut(tag, ",")
		if name == "" || name == "-" {
			continue
		}
		// "board" only appears in --all aggregation, not a per-item property.
		if name == "board" {
			continue
		}
		want[name] = true
	}

	props := buildSchema().Properties
	for name := range want {
		if _, ok := props[name]; !ok {
			t.Errorf("schema missing property %q present in itemJSON", name)
		}
	}
	for name := range props {
		if !want[name] {
			t.Errorf("schema has property %q with no itemJSON field", name)
		}
	}
}

func TestSchemaIsValidJSON(t *testing.T) {
	b, err := json.Marshal(buildSchema())
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(b, &doc); err != nil {
		t.Fatal(err)
	}
	if doc["$schema"] == "" || doc["properties"] == nil {
		t.Error("schema missing $schema or properties")
	}
	// priority enum is a fixed closed set; status is config-driven but never empty.
	props := doc["properties"].(map[string]any)
	if enum := props["priority"].(map[string]any)["enum"]; len(enum.([]any)) != 3 {
		t.Errorf("priority enum: want 3, got %v", enum)
	}
	if enum := props["status"].(map[string]any)["enum"]; len(enum.([]any)) == 0 {
		t.Error("status enum empty; want config order or default")
	}
}
