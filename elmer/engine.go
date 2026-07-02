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

	mu      sync.Mutex // guards running
	running bool       // a study is in flight (coalesces overlapping triggers)
}

// NewEngine binds the engine to the host transport.
func NewEngine(host HostCaller) *Engine {
	return &Engine{host: host, api: client.New(host)}
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
	if hdr.Type == wire.EventCommandStarted {
		e.onCommandStarted(ev)
	}
}

// onCommandStarted dispatches our registered commands. The study runs through launchStudy's
// coalescing guard, off the session goroutine (see Notify).
func (e *Engine) onCommandStarted(ev []byte) {
	var c struct {
		Command string `json:"command"`
	}
	if json.Unmarshal(ev, &c) != nil {
		return
	}
	if c.Command == RunStudyCommandID {
		e.launchStudy()
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
// status bar instead. Task 11 fills in the real pipeline; until then it reports not-implemented.
func (e *Engine) runAndReport() {
	defer func() {
		e.mu.Lock()
		e.running = false
		e.mu.Unlock()
		if r := recover(); r != nil {
			e.reportStatus(fmt.Sprintf("Elmer study crashed: %v", r))
		}
	}()
	e.reportStatus("Elmer: not implemented yet")
}

// reportStatus surfaces a study's outcome on the host status bar (best-effort: a status
// failure must not mask the study result).
func (e *Engine) reportStatus(msg string) { _, _ = e.api.Status().SetText(msg) }
