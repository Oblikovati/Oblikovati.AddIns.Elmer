// SPDX-License-Identifier: GPL-2.0-only

// Package elmer is the host-facing core of the Elmer multiphysics FEA add-in: it
// turns host bodies into finite-element studies (surface mesh → volume mesh →
// solver input → solve → field render) using only the Apache-2.0
// oblikovati.org/api client. The cgo c-shared shell (../export.go) owns the C ABI;
// this package owns the pipeline and stays cgo-free so it unit-tests everywhere.
package elmer

import (
	"encoding/json"
	"fmt"
	"sync"

	"oblikovati.org/api/client"
	"oblikovati.org/api/wire"
	"oblikovati.org/elmer/elmer/femmodel"
)

// HostCaller is the transport the engine talks to the host through — exactly the
// api/client Caller contract, supplied by the cgo shell at Activate (or a fake in
// tests). Keeping it an interface here keeps this package cgo-free and testable.
type HostCaller interface {
	Call(method string, req []byte) ([]byte, error)
}

// Engine runs Elmer studies against a live host.
type Engine struct {
	host HostCaller
	api  *client.Client

	mu          sync.Mutex         // guards analysis, resultField, running, scratchDir
	analysis    *femmodel.Analysis // tree-owned source of truth (Mesh/Material/Load)
	resultField string             // M1: "vonmises" | "displacement" (engine-only, see panel.go's applyResultEdit)
	running     bool               // a study is in flight (coalesces overlapping triggers)
	scratchDir  string             // current study's kept-on-failure scratch dir, "" until known (study.go's runStudy)

	// solve runs the resolved deck in dir and returns ElmerSolver's combined stdout — a
	// stubbable seam (defaults to runElmerSolver) so tests can drive the whole pipeline
	// without a real solve.
	solve func(dir string) (string, error)
}

// NewEngine binds the engine to the host transport with the default study parameters.
func NewEngine(host HostCaller) *Engine {
	return &Engine{
		host: host, api: client.New(host),
		analysis:    femmodel.NewDefaultAnalysis(),
		resultField: resultFieldVonMises,
		solve:       runElmerSolver,
	}
}

// study snapshots the study model under lock and projects it to the flat StudySettings the
// pipeline consumes — the ONE seam the mesh/deck/solve/render path reads.
func (e *Engine) study() StudySettings {
	e.mu.Lock()
	defer e.mu.Unlock()
	return projectAnalysis(e.analysis)
}

// resultFieldKind returns the panel-selected result field under lock.
func (e *Engine) resultFieldKind() string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.resultField
}

// setScratchDir records the current study's scratch dir (or "" once it's no longer known to
// be current, e.g. at the start of a fresh run) — see runAndReport's panic-recovery path,
// which reads it back through scratchDirSnapshot to name a crash's kept dir the same way the
// ordinary error path already does (study.go's runStudy).
func (e *Engine) setScratchDir(dir string) {
	e.mu.Lock()
	e.scratchDir = dir
	e.mu.Unlock()
}

// scratchDirSnapshot returns the current study's scratch dir, or "" if none is known yet
// (e.g. a panic before os.MkdirTemp ran).
func (e *Engine) scratchDirSnapshot() string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.scratchDir
}

// Notify receives host event bytes. A command.started carrying RunStudyCommandID runs the
// Elmer study on a SEPARATE goroutine — never inline, because Notify is invoked on the host's
// session goroutine and a host call from there blocks until the frame loop drains the
// dispatcher (which cannot happen while we're inside it), deadlocking every host call. A
// guard coalesces overlapping triggers so one study is in flight at a time. Grown in later
// tasks (panel/browser events).
func (e *Engine) Notify(ev []byte) {
	var hdr struct {
		Type string `json:"type"`
	}
	if json.Unmarshal(ev, &hdr) != nil {
		return
	}
	switch hdr.Type {
	case wire.EventCommandStarted:
		e.onCommandStarted(ev)
	case wire.EventPanelValueChanged:
		e.onPanelValueChanged(ev)
	}
}

// onCommandStarted dispatches our registered commands. The study runs through launchStudy's
// coalescing guard, off the session goroutine (see Notify); ShowPanel makes a host call
// (DockableWindows().Set), so it too must run off the session goroutine to avoid the
// dispatcher deadlock.
func (e *Engine) onCommandStarted(ev []byte) {
	var c struct {
		Command string `json:"command"`
	}
	if json.Unmarshal(ev, &c) != nil {
		return
	}
	switch c.Command {
	case RunStudyCommandID:
		e.launchStudy()
	case ShowPanelCommandID:
		go func() { _, _ = e.ShowPanel() }()
	}
}

// onPanelValueChanged applies a panel edit. Editing a parameter only mutates engine state
// (no host call) — safe to run inline on the session goroutine.
func (e *Engine) onPanelValueChanged(ev []byte) {
	var p struct {
		WindowId  string `json:"windowId"`
		ControlId string `json:"controlId"`
		Value     string `json:"value"`
	}
	if json.Unmarshal(ev, &p) == nil && p.WindowId == PanelID {
		e.applyPanelEdit(p.ControlId, p.Value)
	}
}

// launchStudy starts one study goroutine, coalescing overlapping triggers, and reports the
// outcome to the host status bar so a failed solve is visible rather than silently empty.
func (e *Engine) launchStudy() {
	e.mu.Lock()
	if e.running {
		e.mu.Unlock()
		return
	}
	e.running = true
	e.mu.Unlock()

	go e.runAndReport()
}

// runAndReport runs one study and reports its outcome, recovering from any panic in the
// pipeline so a bug cannot take down the in-process host — the failure is surfaced on the
// status bar instead, naming the kept scratch dir when one is already known (mirroring the
// ordinary error path's "scratch dir kept for inspection" message, study.go:67) so a crash
// mid-study is just as inspectable as a clean failure.
func (e *Engine) runAndReport() {
	defer func() {
		e.mu.Lock()
		e.running = false
		e.mu.Unlock()
		if r := recover(); r != nil {
			e.reportStatus(crashStatus(r, e.scratchDirSnapshot()))
		}
	}()
	res, err := e.runStudy()
	if err != nil {
		e.reportStatus("Elmer study failed: " + err.Error())
		return
	}
	e.reportStatus(res.Summary())
}

// crashStatus builds runAndReport's panic status message, appending the kept scratch dir
// only when one was already known at panic time (dir == "" for a panic before
// os.MkdirTemp ran, e.g. inside e.study()) — an honest "no path yet" rather than a stale or
// fabricated one.
func crashStatus(r any, dir string) string {
	msg := fmt.Sprintf("Elmer study crashed: %v", r)
	if dir != "" {
		msg += fmt.Sprintf(" (scratch dir kept for inspection: %s)", dir)
	}
	return msg
}

// reportStatus surfaces a study's outcome on the host status bar (best-effort: a status
// failure must not mask the study result).
func (e *Engine) reportStatus(msg string) { _, _ = e.api.Status().SetText(msg) }
