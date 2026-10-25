package tui

import (
	"reflect"
	"testing"

	binding "charm.land/bubbles/v2/key"
)

// TestKeymapNoClashes pins one key per action: no two bindings share a key.
func TestKeymapNoClashes(t *testing.T) {
	v := reflect.ValueOf(keys)
	owner := map[string]string{}
	for i := range v.NumField() {
		name := v.Type().Field(i).Name
		for _, k := range v.Field(i).Interface().(binding.Binding).Keys() {
			if prev, ok := owner[k]; ok {
				t.Errorf("key %q bound to both %s and %s", k, prev, name)
			}
			owner[k] = name
		}
	}
}
