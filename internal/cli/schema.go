package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"

	"shepherd/internal/store"
)

// Version is the running build's version, set by main from the embedded
// manifest. Reported inside `schema --json` so a consumer can pin the shape it
// validated against and detect drift. Defaults to "dev" for tests and `go run`.
var Version = "dev"

// schemaDoc is a JSON Schema (draft 2020-12) describing a shepherd item — the
// same shape `list --json` emits and that `add`/`edit` accept via quick-add
// tokens. Agents (and drover's LLM policy) read it to produce and validate items
// against a fixed vocabulary instead of guessing field names and enums.
type schemaDoc struct {
	Schema     string              `json:"$schema"`
	ID         string              `json:"$id"`
	Title      string              `json:"title"`
	Version    string              `json:"version,omitempty"`
	Type       string              `json:"type"`
	Required   []string            `json:"required"`
	AddlProps  bool                `json:"additionalProperties"`
	Properties map[string]property `json:"properties"`
	// Tokens and DueForms are shepherd extensions (x- prefixed): the quick-add
	// grammar `add`/`edit` speak, so a consumer can render a validated item back
	// into a command line rather than reverse-engineering the parser.
	Tokens   []tokenSpec `json:"x-tokens"`
	DueForms []string    `json:"x-dueForms"`
}

type property struct {
	Type        string    `json:"type"`
	Description string    `json:"description,omitempty"`
	Enum        []string  `json:"enum,omitempty"`
	Format      string    `json:"format,omitempty"`
	MinLength   int       `json:"minLength,omitempty"`
	ReadOnly    bool      `json:"readOnly,omitempty"`
	Items       *property `json:"items,omitempty"`
}

// tokenSpec maps one quick-add token to the field it sets. A bare key (e.g.
// "due:") clears that field; note: consumes the rest of the line.
type tokenSpec struct {
	Token string `json:"token"`
	Field string `json:"field"`
	Desc  string `json:"desc,omitempty"`
}

// buildSchema assembles the item schema, drawing the status enum from the same
// config the board reads so the two never drift.
func buildSchema() schemaDoc {
	statuses := store.ConfigStatusOrder()
	if len(statuses) == 0 {
		statuses = []string{"open", "done"}
	}
	return schemaDoc{
		Schema:    "https://json-schema.org/draft/2020-12/schema",
		ID:        "https://github.com/jwarykowski/shepherd/item.schema.json",
		Title:     "shepherd item",
		Version:   Version,
		Type:      "object",
		Required:  []string{"text"},
		AddlProps: false,
		Properties: map[string]property{
			"id":        {Type: "string", ReadOnly: true, Description: "stable opaque id; address items by this, never index"},
			"index":     {Type: "integer", ReadOnly: true, Description: "1-based board position; shifts as the board reorders"},
			"done":      {Type: "boolean"},
			"status":    {Type: "string", Enum: statuses, Description: "named status; empty = default/open. done is the terminal state"},
			"agentic":   {Type: "boolean", Description: "task raised and driven by an autonomous agent; its status lifecycle (hold/go/running) is meaningful only when true"},
			"priority":  {Type: "string", Enum: []string{"H", "M", "L"}, Description: "empty = none"},
			"text":      {Type: "string", MinLength: 1},
			"category":  {Type: "string", Description: "single lowercase tag"},
			"created":   {Type: "string", ReadOnly: true},
			"completed": {Type: "string", ReadOnly: true, Description: "set when done"},
			"defer":     {Type: "string", Format: "date", Description: "YYYY-MM-DD start/defer date"},
			"due":       {Type: "string", Format: "date", Description: "YYYY-MM-DD"},
			"link":      {Type: "string", Format: "uri"},
			"note":      {Type: "string"},
			"subtasks":  {Type: "array", Description: "one level of nested items", Items: &property{Type: "object"}},
		},
		Tokens: []tokenSpec{
			{Token: "@<category>", Field: "category", Desc: "lowercased; bare @ clears"},
			{Token: "!h|!m|!l", Field: "priority", Desc: "bare ! clears"},
			{Token: "due:<date>", Field: "due", Desc: "bare due: clears; see x-dueForms"},
			{Token: "defer:<date>", Field: "defer", Desc: "bare defer: clears"},
			{Token: "link:<url>", Field: "link", Desc: "bare link: clears"},
			{Token: "status:<name>", Field: "status", Desc: "bare status: resets to open"},
			{Token: "agentic", Field: "agentic", Desc: "mark task agent-driven; agentic:false clears"},
			{Token: "note:<text>", Field: "note", Desc: "takes the rest of the line; put last"},
		},
		DueForms: []string{"YYYY-MM-DD", "today", "tomorrow", "week", "next week", "month", "next month", "+Nd"},
	}
}

// cmdSchema prints the item schema as JSON. It takes no flags and no board — the
// shape is static apart from the config-driven status enum. HTML escaping is off
// so the token grammar (<category>, <url>) stays readable.
func cmdSchema(_ []string, w io.Writer) int {
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(buildSchema()); err != nil {
		fmt.Fprintln(os.Stderr, "shepherd:", err)
		return 1
	}
	return 0
}
