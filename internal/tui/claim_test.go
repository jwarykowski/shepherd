package tui

import (
	"shepherd/internal/todo"
	"strings"
	"testing"

	"charm.land/bubbles/v2/textinput"
	"github.com/charmbracelet/x/ansi"
)

// TestClaimShown pins that a claimed item names its claimer on the board, in
// the table and lane views, and in the detail view.
func TestClaimShown(t *testing.T) {
	m := model{
		input: textinput.New(), w: 120, statuses: []string{"open", "in-progress", "done"},
		items: []todo.Item{{ID: "a", Text: "ship it", Status: "in-progress", Claimed: "agent-1"}},
	}
	for _, v := range []viewMode{viewCategory, viewTable, viewLane} {
		m.view = v
		if out := ansi.Strip(m.render()); !strings.Contains(out, "ship it (agent-1)") {
			t.Fatalf("view %d missing claimer:\n%s", v, out)
		}
	}
	m.view = viewCategory
	if out := ansi.Strip(m.detailView()); !strings.Contains(out, "claimed") || !strings.Contains(out, "agent-1") {
		t.Fatalf("detail missing claimer:\n%s", out)
	}
}
