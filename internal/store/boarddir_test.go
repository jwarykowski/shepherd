package store

import "testing"

func TestBoardDirRoundTrip(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("SHEPHERD_TODO_FILE", "")
	t.Setenv("SHEPHERD_CONFIG", "")

	if got := BoardDir("web"); got != "" {
		t.Fatalf("unset dir should be empty, got %q", got)
	}
	if err := SetBoardDir("web", "/src/web"); err != nil {
		t.Fatal(err)
	}
	if err := SetBoardDir("api", "/src/api"); err != nil {
		t.Fatal(err)
	}
	if got := BoardDir("web"); got != "/src/web" {
		t.Fatalf("BoardDir(web) = %q", got)
	}
	// overwrite, then clear
	if err := SetBoardDir("web", "/src/web2"); err != nil {
		t.Fatal(err)
	}
	if got := BoardDir("web"); got != "/src/web2" {
		t.Fatalf("overwrite failed: %q", got)
	}
	if err := SetBoardDir("web", ""); err != nil {
		t.Fatal(err)
	}
	if got := BoardDir("web"); got != "" {
		t.Fatalf("clear failed: %q", got)
	}
	if got := BoardDir("api"); got != "/src/api" { // sibling survived the clear
		t.Fatalf("api dir clobbered: %q", got)
	}
	if err := SetBoardDir("bad name", "/x"); err == nil {
		t.Fatal("invalid board name should be rejected")
	}
}
