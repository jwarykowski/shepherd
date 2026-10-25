package todo

import "testing"

func TestClaimStatus(t *testing.T) {
	for _, tc := range []struct {
		statuses []string
		want     string
	}{
		{nil, "in-progress"},
		{[]string{"open", "done"}, "in-progress"},
		{[]string{"open", "doing", "review", "done"}, "doing"},
		{[]string{"todo", "wip", "done"}, "wip"},
	} {
		if got := ClaimStatus(tc.statuses); got != tc.want {
			t.Errorf("ClaimStatus(%v) = %q, want %q", tc.statuses, got, tc.want)
		}
	}
}

func TestClaim(t *testing.T) {
	for _, tc := range []struct {
		name    string
		it      Item
		by      string
		wantErr bool
	}{
		{"open", Item{Text: "a"}, "bot", false},
		{"own claim again", Item{Text: "a", Claimed: "bot"}, "bot", false},
		{"held by other", Item{Text: "a", Claimed: "other"}, "bot", true},
		{"done", Item{Text: "a", Done: true}, "bot", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			it := tc.it
			err := Claim(&it, tc.by, "doing")
			if (err != nil) != tc.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tc.wantErr)
			}
			if err == nil && (it.Claimed != tc.by || it.Status != "doing") {
				t.Fatalf("claim not applied: %+v", it)
			}
			if err != nil && (it.Claimed != tc.it.Claimed || it.Status != tc.it.Status) {
				t.Fatalf("failed claim mutated item: %+v", it)
			}
		})
	}
}

func TestRelease(t *testing.T) {
	for _, tc := range []struct {
		name    string
		it      Item
		by      string
		wantErr bool
	}{
		{"own", Item{Claimed: "bot", Status: "doing"}, "bot", false},
		{"forced", Item{Claimed: "bot", Status: "doing"}, "", false},
		{"other's", Item{Claimed: "other", Status: "doing"}, "bot", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			it := tc.it
			err := Release(&it, tc.by)
			if (err != nil) != tc.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tc.wantErr)
			}
			if err == nil && (it.Claimed != "" || it.Status != "") {
				t.Fatalf("release not applied: %+v", it)
			}
		})
	}
}

func TestSetDoneEndsClaim(t *testing.T) {
	it := Item{Text: "x", Claimed: "bot"}
	SetDone(&it, true)
	if it.Claimed != "" {
		t.Fatalf("claim kept after done: %q", it.Claimed)
	}
}
