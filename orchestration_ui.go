package main

// UI glue for "orchestrated mode": InseyetsInstaller started by sf-validator.
// The protocol logic lives in internal/orchestration (unit-tested).
//
// Without --source-root / --dest-root / --wait-signal on the command line,
// orchCfg is nil and EVERY function below does nothing, so the installer
// behaves exactly like a normal double-click launch.
//
// Orchestrated flow:
//  1. GUI opens, installers are detected on the source folder and IMMEDIATELY
//     displayed with their Desktop destination paths (and a visual badge under
//     each path box showing whether it runs from the Desktop copy or from an
//     external custom location).
//  2. The Start button reads "Install when copy is verified". One click ARMS it
//     (options lock, the button becomes "Cancel").
//  3. The installation starts the moment the install is armed AND sf-validator
//     reports the copy as verified ("ok"). Whichever happens last triggers it
//     immediately: there is no timer.
//  4. "failed" status, or an sf-validator that stopped responding, blocks the
//     install for that session.

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"github.com/lxn/walk"

	"inseyets-installer/internal/orchestration"
)

const (
	startButtonStandaloneText = "Start Installation"
	startButtonArmText        = "Install when copy is verified"
	startButtonCancelText     = "Cancel (armed)"
)

var (
	orchCfg      *orchestration.Config // nil = standalone mode
	orchArgError string                // set when the flags were incomplete

	// The booleans and the text below are only touched on the UI thread.
	orchArmed      bool
	orchFired      bool
	orchFailed     bool
	orchStatusText string

	// Set from the UI thread, read by the polling goroutine.
	orchDone atomic.Bool
)

// parseOrchestrationArgs reads the command line once at startup.
func parseOrchestrationArgs() {
	cfg, err := orchestration.ParseArgs(os.Args[1:])
	if err != nil {
		orchArgError = err.Error()
		return
	}
	orchCfg = cfg
}

// orchIsActive reports whether orchestrated mode is active.
func orchIsActive() bool {
	return orchCfg != nil
}

// orchRemapIfEnabled immediately remaps a source path to the Desktop destination
// when orchestrated mode is active, so the GUI shows the Desktop path from the start.
func orchRemapIfEnabled(p string) string {
	if orchCfg == nil || p == "" {
		return p
	}
	return orchestration.RemapPath(p, orchCfg.SourceRoot, orchCfg.DestRoot)
}

// orchHandleBrowse remaps a file chosen via "Browse..." if it sits inside the
// source student folder, and leaves it untouched if it sits anywhere else.
func orchHandleBrowse(selected string) string {
	if orchCfg == nil || selected == "" {
		return selected
	}
	remapped := orchestration.RemapPath(selected, orchCfg.SourceRoot, orchCfg.DestRoot)
	if remapped != selected {
		logMessage(fmt.Sprintf("[sf-validator] Selected installer inside source folder; mapped to Desktop copy: %s", remapped))
	} else {
		logMessage(fmt.Sprintf("[sf-validator] Selected external installer (outside student folder): %s", selected))
	}
	return remapped
}

// orchArmedNow tells updateOptionsEnablement that options must stay locked.
func orchArmedNow() bool {
	return orchCfg != nil && orchArmed
}

// isUnderDir reports whether path p is inside or equal to dir (case-insensitive).
func isUnderDir(p, dir string) bool {
	cp := filepath.Clean(p)
	cd := filepath.Clean(dir)
	if len(cp) < len(cd) || !strings.EqualFold(cp[:len(cd)], cd) {
		return false
	}
	rest := cp[len(cd):]
	if rest != "" && !strings.HasSuffix(cd, string(filepath.Separator)) && rest[0] != filepath.Separator {
		return false
	}
	return true
}

// orchUpdateTargetLabels updates the visual badge below each installer LineEdit
// so the user can immediately see whether the path points to the Desktop copy
// or to an external custom location.
func orchUpdateTargetLabels() {
	if orchCfg == nil {
		return
	}
	type pair struct {
		le  *walk.LineEdit
		lbl *walk.Label
	}
	items := []pair{
		{leDCodePath, lblDCodeTarget},
		{lePAPath, lblPATarget},
		{leUFEDPath, lblUFEDTarget},
	}
	for _, it := range items {
		if it.le == nil || it.lbl == nil {
			continue
		}
		raw := strings.TrimSpace(it.le.Text())
		if raw == "" {
			setTextIfChanged(it.lbl.Text, it.lbl.SetText, "")
			continue
		}
		// If the user pasted a source-folder path manually, remap it live.
		remapped := orchestration.RemapPath(raw, orchCfg.SourceRoot, orchCfg.DestRoot)
		if remapped != raw {
			_ = it.le.SetText(remapped)
			raw = remapped
		}

		if isUnderDir(raw, orchCfg.DestRoot) {
			it.lbl.SetTextColor(walk.RGB(0, 128, 55))
			setTextIfChanged(it.lbl.Text, it.lbl.SetText, "✔ Target: Desktop copy (remapped from source media)")
		} else {
			it.lbl.SetTextColor(walk.RGB(210, 105, 0))
			setTextIfChanged(it.lbl.Text, it.lbl.SetText, "➔ Target: External custom path (outside student folder)")
		}
	}
}

// setupOrchestration is called once, just before the window's message loop starts.
func setupOrchestration() {
	if orchArgError != "" {
		logMessage("[sf-validator] Ignoring orchestration arguments: " + orchArgError + " Running in standalone mode.")
		return
	}
	if orchCfg == nil {
		return
	}

	logMessage("[sf-validator] Orchestrated mode.")
	logMessage(fmt.Sprintf("[sf-validator] Source folder:      %s", orchCfg.SourceRoot))
	logMessage(fmt.Sprintf("[sf-validator] Destination folder: %s", orchCfg.DestRoot))
	logMessage("[sf-validator] Auto-detected paths have been mapped to the Desktop destination.")
	logMessage("[sf-validator] Choose your options, then click \"" + startButtonArmText + "\".")

	orchStatusText = "waiting for sf-validator..."
	orchUpdateTargetLabels()
	updateStartButtonState()
	go pollOrchestrationStatus()
}

// onStartClicked replaces the direct call to startInstallation() in the button handler.
func onStartClicked() {
	// Standalone, or already started once: original behaviour.
	if orchCfg == nil || orchFired {
		startInstallation()
		return
	}
	if orchFailed {
		return
	}

	if orchArmed {
		orchArmed = false
		setInputsEnabled(true)
		logMessage("[sf-validator] Disarmed. Options unlocked.")
	} else {
		// Arming without any installer selected would make the later start fail:
		// tell the user instead of silently doing nothing.
		if !anyInstallerSelected() {
			walk.MsgBox(mw, "Attention", "Please select at least one installer (use Browse...) before arming the installation.", walk.MsgBoxIconWarning)
			return
		}
		orchArmed = true
		setInputsEnabled(false)
		logMessage("[sf-validator] Armed: the installation starts as soon as the copy is verified.")
	}
	updateStartButtonState()
}

// anyInstallerSelected reports whether at least one installer path is filled in.
func anyInstallerSelected() bool {
	return strings.TrimSpace(leDCodePath.Text()) != "" ||
		strings.TrimSpace(lePAPath.Text()) != "" ||
		strings.TrimSpace(leUFEDPath.Text()) != ""
}

// setTextIfChanged avoids needless repaints/flicker when refreshing every poll.
func setTextIfChanged(get func() string, set func(string) error, value string) {
	if get() != value {
		_ = set(value)
	}
}

// orchRefreshStartButton is called at the top of updateStartButtonState.
// It returns true when it handled the button and status label itself.
func orchRefreshStartButton() bool {
	if orchCfg == nil || orchFired || isInstalling {
		return false
	}

	hasAny := anyInstallerSelected()

	var btnText, statusText string
	btnEnabled := true
	switch {
	case orchFailed:
		btnText, btnEnabled = "Install blocked", false
		statusText = "Status: " + orchStatusText
	case orchArmed:
		btnText = startButtonCancelText
		statusText = "Status: Armed - " + orchStatusText
	default:
		// Deliberately NOT disabled when no installer is selected yet: the button
		// stays clickable and explains what is missing, so it can never look stuck.
		btnText = startButtonArmText
		if hasAny {
			statusText = "Status: Choose your options, then click to arm. sf-validator: " + orchStatusText
		} else {
			statusText = "Status: Please select at least one installer above (Browse...). sf-validator: " + orchStatusText
		}
	}

	setTextIfChanged(btnStart.Text, btnStart.SetText, btnText)
	if btnStart.Enabled() != btnEnabled {
		btnStart.SetEnabled(btnEnabled)
	}
	setTextIfChanged(lblStatus.Text, lblStatus.SetText, statusText)
	return true
}

// pollOrchestrationStatus runs in its own goroutine and checks the status file
// twice a second until a final decision (start or fail) has been taken.
func pollOrchestrationStatus() {
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	lastGood := time.Now()
	for range ticker.C {
		if orchDone.Load() {
			return
		}
		st, err := orchestration.ReadStatus(orchCfg.StatusFile)
		if err == nil {
			lastGood = time.Now()
		}
		now := time.Now()
		lastGoodCopy := lastGood

		mw.Synchronize(func() {
			if orchDone.Load() {
				return
			}
			applyOrchestrationDecision(orchestration.Decide(st, err, now, lastGoodCopy, orchArmed))
		})
	}
}

// applyOrchestrationDecision runs on the UI thread.
func applyOrchestrationDecision(d orchestration.Decision) {
	switch d.Action {
	case orchestration.Fail:
		orchDone.Store(true)
		orchFailed = true
		orchArmed = false
		orchStatusText = d.Text
		logMessage("[sf-validator] ERROR: " + d.Text)
		setInputsEnabled(true)
		updateStartButtonState()
		walk.MsgBox(mw, "sf-validator", d.Text, walk.MsgBoxIconError)

	case orchestration.Start:
		orchDone.Store(true)
		logMessage("[sf-validator] Copy verified.")
		fireOrchestratedInstall()

	default: // Wait
		// Refresh on every poll (not only when the text changes) so the button and
		// label always reflect the current state, whatever event ordering occurred.
		orchStatusText = d.Text
		updateStartButtonState()
	}
}

// fireOrchestratedInstall ensures every detected installer inside the student
// folder points at the verified Desktop copy (and leaves external paths as-is),
// then starts the normal installation. UI thread only.
func fireOrchestratedInstall() {
	orchFired = true
	orchArmed = false
	_ = btnStart.SetText(startButtonStandaloneText)

	edits := []*walk.LineEdit{leDCodePath, lePAPath, leUFEDPath}
	for _, le := range edits {
		oldPath := strings.TrimSpace(le.Text())
		if oldPath == "" {
			continue
		}
		newPath := orchestration.RemapPath(oldPath, orchCfg.SourceRoot, orchCfg.DestRoot)
		if newPath != oldPath {
			_ = le.SetText(newPath)
		}
		if isUnderDir(newPath, orchCfg.DestRoot) {
			logMessage(fmt.Sprintf("[sf-validator] Installing from verified Desktop copy: %s", newPath))
		} else {
			logMessage(fmt.Sprintf("[sf-validator] Installing from external custom location: %s", newPath))
		}
	}

	// Logs (PA_logs, UFED_logs) go next to the verified copy, not on the USB drive.
	toolDir = orchCfg.DestRoot

	startInstallation()
}
