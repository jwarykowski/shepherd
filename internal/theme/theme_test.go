package theme

import (
	"image/color"
	"reflect"
	"testing"

	"charm.land/lipgloss/v2"
)

func TestParseColor(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want color.Color
		ok   bool
	}{
		{"4", lipgloss.Color("4"), true},
		{"255", lipgloss.Color("255"), true},
		{`"#88c0d0"`, lipgloss.Color("#88c0d0"), true},
		{"#abc", lipgloss.Color("#abc"), true},
		{"Red", lipgloss.Color("1"), true},
		{"bright-blue", lipgloss.Color("12"), true},
		{"none", nil, true},
		{"256", nil, false},
		{"#12345", nil, false},
		{"teal", nil, false},
	} {
		got, ok := ParseColor(tc.in)
		if ok != tc.ok || !reflect.DeepEqual(got, tc.want) {
			t.Errorf("ParseColor(%q) = %v, %v; want %v, %v", tc.in, got, ok, tc.want, tc.ok)
		}
	}
}

// TestBuiltinsComplete pins that every coloured built-in sets every token but
// prio_low in both variants, and that the cursor row stands out from the text.
func TestBuiltinsComplete(t *testing.T) {
	for _, name := range Names() {
		if name == "mono" {
			continue
		}
		for _, light := range []bool{false, true} {
			p := Resolve(name, light, nil)
			v := reflect.ValueOf(p)
			for i := range v.NumField() {
				f := v.Type().Field(i).Name
				if f != "PrioLow" && v.Field(i).IsNil() {
					t.Errorf("%s (light=%v): %s unset", name, light, f)
				}
			}
			if reflect.DeepEqual(p.CursorBG, p.Accent) {
				t.Errorf("%s (light=%v): cursor matches accent", name, light)
			}
		}
	}
	if p := Resolve("mono", false, nil); !reflect.DeepEqual(p, Palette{}) {
		t.Errorf("mono has colours: %+v", p)
	}
}

func TestResolveOverridesAndFallback(t *testing.T) {
	kv := map[string]string{"theme.accent": `"#ff0000"`, "theme.prio_low": "none", "theme.bogus": "1", "theme.ok": "nope", "view": "lane"}
	p := Resolve("nord", false, kv)
	base := Resolve("nord", false, nil)
	if !reflect.DeepEqual(p.Accent, lipgloss.Color("#ff0000")) || p.PrioLow != nil {
		t.Errorf("overrides not applied: %+v", p)
	}
	if !reflect.DeepEqual(p.OK, base.OK) || !reflect.DeepEqual(p.Info, base.Info) {
		t.Error("an override touched another token")
	}
	if got := Resolve("nope", false, nil); !reflect.DeepEqual(got, Resolve(Default, false, nil)) {
		t.Error("unknown theme did not fall back to the default")
	}
	if Next("nord") == "nord" || Next(Names()[len(Names())-1]) != Default {
		t.Error("Next does not cycle")
	}
}
