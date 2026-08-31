package main

import (
	"os"
	"path/filepath"
	"testing"
)

func overrideBoardsDir(t *testing.T) {
	t.Helper()
	old := boardsDir
	boardsDir = func() string { return t.TempDir() }
	t.Cleanup(func() { boardsDir = old })
}

func TestBoardIDRe(t *testing.T) {
	cases := []struct {
		id   string
		want bool
	}{
		{"0831-221540-cellar-tracker", true},
		{"abc-123", true},
		{"a", true},
		{"", false},
		{"-leading-dash", false},
		{"../../etc/passwd", false},
		{"has/slash", false},
		{"has.dot", false},
		{"UPPER", false},
		{"has space", false},
	}
	for _, c := range cases {
		if got := boardIDRe.MatchString(c.id); got != c.want {
			t.Errorf("boardIDRe.MatchString(%q) = %v, want %v", c.id, got, c.want)
		}
	}
}

func TestSlugIDCollisions(t *testing.T) {
	dir := t.TempDir()
	overrideBoardsDir(t)
	old := boardsDir
	boardsDir = func() string { return dir }
	t.Cleanup(func() { boardsDir = old })

	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	a := slugID("Cellar Tracker")
	if err := os.WriteFile(filepath.Join(dir, a+".json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	b := slugID("Cellar Tracker")
	if a == b {
		t.Fatalf("same-second same-title slugID produced duplicate %q", a)
	}
	if b != a+"-2" {
		t.Errorf("collision suffix = %q, want %q", b, a+"-2")
	}
}

func TestSlugIDSanitisation(t *testing.T) {
	overrideBoardsDir(t)
	got := slugID("../../etc/passwd & rm -rf /")
	if !boardIDRe.MatchString(got) {
		t.Errorf("slugID produced %q, which fails boardIDRe", got)
	}
}

func TestValidImageRef(t *testing.T) {
	cases := []struct {
		ref  string
		want bool
	}{
		{"", true},
		{"/img/123.png", true},
		{"https://example.com/x.jpg", true},
		{"http://example.com/x.jpg", true},
		{"javascript:alert(1)", false},
		{"data:image/png;base64,AAAA", false},
		{"file:///etc/passwd", false},
		{"../something", false},
		{"img/relative.png", false},
	}
	for _, c := range cases {
		if got := validImageRef(c.ref); got != c.want {
			t.Errorf("validImageRef(%q) = %v, want %v", c.ref, got, c.want)
		}
	}
}

func TestSaveBoardRejectsBadID(t *testing.T) {
	overrideBoardsDir(t)
	err := saveBoard(Board{ID: "../escape", Title: "x"})
	if err == nil {
		t.Fatal("saveBoard accepted an unsafe id")
	}
}

func TestSaveBoardAtomicWrite(t *testing.T) {
	dir := t.TempDir()
	old := boardsDir
	boardsDir = func() string { return dir }
	t.Cleanup(func() { boardsDir = old })

	if err := saveBoard(Board{ID: "0831-test-board", Title: "t"}); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if filepath.Ext(e.Name()) == ".tmp" {
			t.Errorf("leftover temp file %q after save", e.Name())
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "0831-test-board.json")); err != nil {
		t.Errorf("board file missing: %v", err)
	}
}

func TestParseVerbArgs(t *testing.T) {
	flags, positional := parseVerbArgs([]string{"cellar-tracker", "-sub", "new pitch", "-bullets", "a;b", "-extra"})
	wantFlags := map[string]string{"sub": "new pitch", "bullets": "a;b", "extra": ""}
	for k, want := range wantFlags {
		if flags[k] != want {
			t.Errorf("flags[%q] = %q, want %q", k, flags[k], want)
		}
	}
	if len(positional) != 1 || positional[0] != "cellar-tracker" {
		t.Errorf("positional = %v, want [cellar-tracker]", positional)
	}
}
