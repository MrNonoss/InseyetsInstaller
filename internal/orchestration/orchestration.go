// Package orchestration holds the pure (UI-free) logic used when
// InseyetsInstaller is started by sf-validator.
//
// In that mode the installer is launched with three arguments:
//
//	--source-root <folder>   folder the installers were detected in (e.g. the USB drive)
//	--dest-root   <folder>   copy of that folder (e.g. on the Desktop) where installs must run
//	--wait-signal <file>     JSON status file written by sf-validator
//
// Without those arguments the installer behaves exactly as it always did.
// Keeping this logic outside package main lets it be unit-tested without
// administrator rights (the main package's test binary embeds the UAC manifest).
package orchestration

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Timing rules for the status file.
const (
	// A copying/verifying status that has not been refreshed for this long means
	// sf-validator died. It rewrites the file every second while it is running.
	StaleAfter = 15 * time.Second
	// How long the status file may stay missing/unreadable before giving up.
	UnreadableAfter = 10 * time.Second
)

// Status states written by sf-validator.
const (
	StateCopying   = "copying"
	StateVerifying = "verifying"
	StateOK        = "ok"
	StateFailed    = "failed"
)

// Config is the orchestration request received on the command line.
type Config struct {
	SourceRoot string
	DestRoot   string
	StatusFile string
}

// ParseArgs reads the orchestration flags from args (os.Args[1:]).
//   - none of the flags present  -> (nil, nil): standalone mode, nothing changes
//   - all three present          -> (*Config, nil)
//   - only some present          -> (nil, error): caller logs it and stays standalone
//
// Both "--flag value" and "--flag=value" are accepted. Unknown arguments are ignored.
func ParseArgs(args []string) (*Config, error) {
	values := map[string]string{}
	known := map[string]bool{"--source-root": true, "--dest-root": true, "--wait-signal": true}

	for i := 0; i < len(args); i++ {
		a := args[i]
		if name, val, ok := strings.Cut(a, "="); ok && known[name] {
			values[name] = val
			continue
		}
		if known[a] && i+1 < len(args) {
			values[a] = args[i+1]
			i++
		}
	}

	if len(values) == 0 {
		return nil, nil
	}
	cfg := &Config{
		SourceRoot: values["--source-root"],
		DestRoot:   values["--dest-root"],
		StatusFile: values["--wait-signal"],
	}
	if cfg.SourceRoot == "" || cfg.DestRoot == "" || cfg.StatusFile == "" {
		return nil, fmt.Errorf("incomplete sf-validator arguments: --source-root, --dest-root and --wait-signal must all be provided")
	}
	return cfg, nil
}

// RemapPath rewrites p from under srcRoot to the same relative location under
// dstRoot. Paths outside srcRoot are returned unchanged. The comparison is
// case-insensitive (Windows) and respects folder boundaries, so
// "E:\Student folder2\x" is NOT considered to be inside "E:\Student folder".
func RemapPath(p, srcRoot, dstRoot string) string {
	cp := filepath.Clean(p)
	cs := filepath.Clean(srcRoot)
	if len(cp) < len(cs) || !strings.EqualFold(cp[:len(cs)], cs) {
		return p
	}
	rest := cp[len(cs):]
	// Boundary check (unless srcRoot is a drive root like "E:\", which already ends with a separator).
	if rest != "" && !strings.HasSuffix(cs, string(filepath.Separator)) && rest[0] != filepath.Separator {
		return p
	}
	return filepath.Join(dstRoot, rest)
}

// Status mirrors the JSON document written by sf-validator.
type Status struct {
	RunID     string    `json:"run_id"`
	State     string    `json:"state"`
	Message   string    `json:"message"`
	Percent   int       `json:"percent"`
	PID       int       `json:"pid"`
	UpdatedAt time.Time `json:"updated_at"`
}

// ReadStatus loads and parses the status file.
func ReadStatus(path string) (*Status, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var st Status
	if err := json.Unmarshal(data, &st); err != nil {
		return nil, err
	}
	return &st, nil
}

// Action is what the installer should do after looking at the status.
type Action int

const (
	Wait  Action = iota // keep polling
	Start               // copy verified and install armed: begin installing
	Fail                // never install; show the text to the user
)

// Decision is the outcome of Decide plus a human-readable status line.
type Decision struct {
	Action Action
	Text   string
}

// Decide is the heart of the protocol. It is pure so it can be tested.
//
//	st, readErr : result of the latest ReadStatus
//	now         : current time
//	lastGood    : time of the last successful read (or of the first poll)
//	armed       : the instructor already clicked "Install when copy is verified"
func Decide(st *Status, readErr error, now, lastGood time.Time, armed bool) Decision {
	if readErr != nil || st == nil {
		if now.Sub(lastGood) > UnreadableAfter {
			return Decision{Fail, fmt.Sprintf("Cannot read the sf-validator status file (%v). Installation blocked.", readErr)}
		}
		return Decision{Wait, "waiting for sf-validator..."}
	}

	switch st.State {
	case StateFailed:
		msg := st.Message
		if msg == "" {
			msg = "copy or verification failed"
		}
		return Decision{Fail, "sf-validator reported a failure: " + msg + ". Installation blocked."}

	case StateOK:
		if armed {
			return Decision{Start, "copy verified, starting installation"}
		}
		return Decision{Wait, "copy verified. Click \"Install when copy is verified\" to start."}

	case StateCopying, StateVerifying:
		if now.Sub(st.UpdatedAt) > StaleAfter {
			return Decision{Fail, "sf-validator stopped responding before the copy was verified. Installation blocked."}
		}
		if st.State == StateCopying {
			return Decision{Wait, fmt.Sprintf("sf-validator is copying (%d%%)", st.Percent)}
		}
		return Decision{Wait, "sf-validator is verifying the copy"}
	}

	return Decision{Wait, "waiting for sf-validator..."}
}

