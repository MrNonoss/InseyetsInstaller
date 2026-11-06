package orchestration

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestParseArgsStandalone(t *testing.T) {
	for _, args := range [][]string{nil, {}, {"--unknown", "x"}} {
		cfg, err := ParseArgs(args)
		if cfg != nil || err != nil {
			t.Fatalf("args %v: expected standalone (nil,nil), got %v, %v", args, cfg, err)
		}
	}
}

func TestParseArgsComplete(t *testing.T) {
	cfg, err := ParseArgs([]string{
		"--source-root", `E:\Student folder`,
		"--dest-root=C:\\Users\\x\\Desktop\\Student folder",
		"--wait-signal", `C:\Temp\status.json`,
	})
	if err != nil || cfg == nil {
		t.Fatalf("unexpected: %v %v", cfg, err)
	}
	if cfg.SourceRoot != `E:\Student folder` || cfg.DestRoot != `C:\Users\x\Desktop\Student folder` || cfg.StatusFile != `C:\Temp\status.json` {
		t.Errorf("wrong config: %+v", cfg)
	}
}

func TestParseArgsIncompleteFallsBack(t *testing.T) {
	cfg, err := ParseArgs([]string{"--source-root", `E:\x`})
	if cfg != nil || err == nil {
		t.Fatalf("expected error for partial args, got %v, %v", cfg, err)
	}
}

func TestRemapPath(t *testing.T) {
	src, dst := `E:\Student folder`, `C:\Users\x\Desktop\Student folder`
	cases := []struct{ in, want string }{
		{`E:\Student folder\Tools\PA\setup.exe`, `C:\Users\x\Desktop\Student folder\Tools\PA\setup.exe`},
		{`e:\student FOLDER\a.exe`, `C:\Users\x\Desktop\Student folder\a.exe`}, // case-insensitive
		{`E:\Student folder`, dst},                               // the root itself
		{`E:\Student folder2\a.exe`, `E:\Student folder2\a.exe`}, // sibling with same prefix: untouched
		{`D:\Elsewhere\a.exe`, `D:\Elsewhere\a.exe`},             // outside the root: untouched
		{`E:\Other\a.exe`, `E:\Other\a.exe`},
	}
	for _, c := range cases {
		if got := RemapPath(c.in, src, dst); got != c.want {
			t.Errorf("RemapPath(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestRemapPathDriveRoot(t *testing.T) {
	got := RemapPath(`E:\Tools\a.exe`, `E:\`, `C:\Users\x\Desktop\StudentFolder`)
	want := `C:\Users\x\Desktop\StudentFolder\Tools\a.exe`
	if got != want {
		t.Errorf("got %q want %q", got, want)
	}
}

func TestDecide(t *testing.T) {
	now := time.Now()
	fresh := now.Add(-time.Second)
	stale := now.Add(-StaleAfter - time.Second)

	cases := []struct {
		name     string
		st       *Status
		err      error
		lastGood time.Time
		armed    bool
		want     Action
	}{
		{"copying, not armed", &Status{State: StateCopying, UpdatedAt: fresh}, nil, now, false, Wait},
		{"copying, armed still waits", &Status{State: StateCopying, UpdatedAt: fresh}, nil, now, true, Wait},
		{"verifying", &Status{State: StateVerifying, UpdatedAt: fresh}, nil, now, true, Wait},
		{"ok but not armed waits for click", &Status{State: StateOK, UpdatedAt: fresh}, nil, now, false, Wait},
		{"ok and armed starts", &Status{State: StateOK, UpdatedAt: fresh}, nil, now, true, Start},
		{"ok stays valid even if old", &Status{State: StateOK, UpdatedAt: stale}, nil, now, true, Start},
		{"failed blocks (not armed)", &Status{State: StateFailed, Message: "x", UpdatedAt: fresh}, nil, now, false, Fail},
		{"failed blocks (armed)", &Status{State: StateFailed, UpdatedAt: fresh}, nil, now, true, Fail},
		{"stale copying = sf-validator died", &Status{State: StateCopying, UpdatedAt: stale}, nil, now, true, Fail},
		{"stale verifying = sf-validator died", &Status{State: StateVerifying, UpdatedAt: stale}, nil, now, false, Fail},
		{"unreadable briefly waits", nil, errors.New("nope"), now.Add(-2 * time.Second), true, Wait},
		{"unreadable too long fails", nil, errors.New("nope"), now.Add(-UnreadableAfter - time.Second), true, Fail},
		{"unknown state waits", &Status{State: "weird", UpdatedAt: fresh}, nil, now, true, Wait},
	}
	for _, c := range cases {
		if got := Decide(c.st, c.err, now, c.lastGood, c.armed); got.Action != c.want {
			t.Errorf("%s: got action %d (%q), want %d", c.name, got.Action, got.Text, c.want)
		}
	}
}

func TestReadStatusFromSfValidatorJSON(t *testing.T) {
	// Same shape as the document written by sf-validator.
	doc := `{
  "run_id": "20261101-185230-9f3a1c2e",
  "state": "copying",
  "message": "Copying student folder to the Desktop",
  "percent": 42,
  "pid": 12345,
  "updated_at": "2026-11-01T17:52:31Z",
  "source_root": "E:\\Student folder",
  "dest_root": "C:\\Users\\x\\Desktop\\Student folder"
}`
	p := filepath.Join(t.TempDir(), "status.json")
	if err := os.WriteFile(p, []byte(doc), 0o644); err != nil {
		t.Fatal(err)
	}
	st, err := ReadStatus(p)
	if err != nil {
		t.Fatal(err)
	}
	if st.State != StateCopying || st.Percent != 42 || st.PID != 12345 || st.UpdatedAt.IsZero() {
		t.Errorf("unexpected status: %+v", st)
	}

	if _, err := ReadStatus(filepath.Join(t.TempDir(), "missing.json")); err == nil {
		t.Error("expected error for missing file")
	}
}
