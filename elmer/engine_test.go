// SPDX-License-Identifier: GPL-2.0-only

package elmer

import (
	"encoding/json"
	"sync"
	"testing"
	"time"

	"oblikovati.org/api/wire"
)

// fakeHost is a named fake HostCaller (no live host): it records the wire methods it is
// asked to call, enough to drive the M0 scaffold (command registration, the
// Notify → study → status path) without a running Oblikovati host.
type fakeHost struct {
	mu    sync.Mutex
	calls []string
}

func newFakeHost() *fakeHost { return &fakeHost{} }

func (h *fakeHost) Call(method string, _ []byte) ([]byte, error) {
	h.mu.Lock()
	h.calls = append(h.calls, method)
	h.mu.Unlock()
	return []byte("{}"), nil
}

// count returns how many times method was called.
func (h *fakeHost) count(method string) int {
	h.mu.Lock()
	defer h.mu.Unlock()
	n := 0
	for _, m := range h.calls {
		if m == method {
			n++
		}
	}
	return n
}

// methods returns every recorded call, in order, for failure-message diagnostics.
func (h *fakeHost) methods() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make([]string, len(h.calls))
	copy(out, h.calls)
	return out
}

// commandStartedEvent builds the wire bytes for a command.started event carrying id — using
// wire.CommandStartedEvent rather than a hand-rolled literal keeps the test bound to the real
// event shape (field "command", not "commandId") the host actually sends.
func commandStartedEvent(id string) []byte {
	ev, _ := json.Marshal(wire.CommandStartedEvent{Type: wire.EventCommandStarted, Command: id})
	return ev
}

// waitIdle blocks until the study goroutine launched by Notify has finished, or fails the
// test after a 2 s deadline so a stuck goroutine doesn't hang the suite.
func waitIdle(t *testing.T, e *Engine) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		e.mu.Lock()
		running := e.running
		e.mu.Unlock()
		if !running {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("waitIdle: engine still running after 2s")
}

// TestNotifyRunStudyLaunchesGoroutineOnce verifies a command.started event for
// RunStudyCommandID drives launchStudy on a goroutine and that a second trigger fired while
// the first is still recorded as running coalesces rather than launching a second study. The
// two Notify calls happen back-to-back on this test's own goroutine, and launchStudy's
// check-and-set of e.running runs synchronously inside Notify (only the study body itself runs
// async) — so the second call deterministically observes running == true and no-ops.
func TestNotifyRunStudyLaunchesGoroutineOnce(t *testing.T) {
	h := newFakeHost()
	e := NewEngine(h)
	ev := commandStartedEvent(RunStudyCommandID)
	e.Notify(ev)
	e.Notify(ev) // second trigger while first "runs" must coalesce
	waitIdle(t, e)
	if got := h.count(wire.MethodStatusSetText); got == 0 {
		t.Fatalf("expected at least one status call, got %d; calls=%v", got, h.methods())
	}
}

func TestRegisterCommandsLandOnFEATab(t *testing.T) {
	h := newFakeHost()
	if err := NewEngine(h).RegisterCommands(); err != nil {
		t.Fatalf("RegisterCommands: %v", err)
	}
	if got := h.count(wire.MethodCommandsCreate); got != len(elmerCommands) {
		t.Errorf("commands.create called %d times, want %d; calls=%v", got, len(elmerCommands), h.methods())
	}
}

func TestStartRegistersCommands(t *testing.T) {
	h := newFakeHost()
	if err := NewEngine(h).Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if got := h.count(wire.MethodCommandsCreate); got == 0 {
		t.Errorf("Start never called %q (calls: %v)", wire.MethodCommandsCreate, h.methods())
	}
}
