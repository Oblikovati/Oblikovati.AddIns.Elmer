// SPDX-License-Identifier: GPL-2.0-only

package elmer

import (
	"encoding/json"
	"sync"
	"testing"
	"time"

	"oblikovati.org/api/types"
	"oblikovati.org/api/wire"
)

// fakeHost is a named fake HostCaller (no live host): it records the wire methods it is
// asked to call, enough to drive the M0 scaffold (command registration, the
// Notify → study → status path) without a running Oblikovati host.
type fakeHost struct {
	mu      sync.Mutex
	calls   []string
	release chan struct{} // non-nil once armGate is called: gated calls block until this closes
	started chan struct{} // buffered 1: signaled the instant a gated call is recorded
}

func newFakeHost() *fakeHost { return &fakeHost{} }

// armGate makes every future call to wire.MethodStatusSetText block until the returned release
// channel is closed, after first signaling on the returned started channel — so a test can prove
// a call is genuinely in flight (not just "probably started by now") before acting on that fact.
// Other methods, and this fake with no gate armed, are unaffected (default non-blocking Call).
func (h *fakeHost) armGate() (release, started chan struct{}) {
	release = make(chan struct{})
	started = make(chan struct{}, 1)
	h.mu.Lock()
	h.release, h.started = release, started
	h.mu.Unlock()
	return release, started
}

func (h *fakeHost) Call(method string, _ []byte) ([]byte, error) {
	h.mu.Lock()
	h.calls = append(h.calls, method)
	release, started := h.release, h.started
	h.mu.Unlock()

	if method == wire.MethodStatusSetText && release != nil {
		select {
		case started <- struct{}{}:
		default: // already signaled by an earlier gated call
		}
		<-release
	}
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
// RunStudyCommandID drives launchStudy on a goroutine and that a second trigger fired while the
// first is PROVABLY still running coalesces rather than launching a second study. "Provably" is
// the point: a bare back-to-back pair of Notify calls doesn't prove the guard fired — the first
// study could legitimately have finished before the second Notify, making a second study
// correct, not a bug. So this test gates the first study's status call open, waits for the fake
// host to record that call has actually started, fires the second trigger while the first is
// certainly still in flight, then releases the gate and asserts exactly one status call total.
func TestNotifyRunStudyLaunchesGoroutineOnce(t *testing.T) {
	h := newFakeHost()
	release, started := h.armGate()
	e := NewEngine(h)
	ev := commandStartedEvent(RunStudyCommandID)

	e.Notify(ev) // launches the study goroutine; it blocks in reportStatus on the gate

	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("first study never reached reportStatus")
	}

	e.Notify(ev) // fired while the first call is provably in flight; must coalesce

	close(release)
	waitIdle(t, e)

	if got := h.count(wire.MethodStatusSetText); got != 1 {
		t.Fatalf("expected exactly one status call (second trigger coalesced), got %d; calls=%v", got, h.methods())
	}
}

// TestRegisterCommandsCreatesEveryCommand verifies RegisterCommands issues one commands.create
// call per entry in elmerCommands. It only proves call count — the fake host discards the
// payload it's called with, so it cannot prove ribbon placement; that's asserted directly
// against commandArgs's output in TestCommandArgsLandOnFEATab below.
func TestRegisterCommandsCreatesEveryCommand(t *testing.T) {
	h := newFakeHost()
	if err := NewEngine(h).RegisterCommands(); err != nil {
		t.Fatalf("RegisterCommands: %v", err)
	}
	if got := h.count(wire.MethodCommandsCreate); got != len(elmerCommands) {
		t.Errorf("commands.create called %d times, want %d; calls=%v", got, len(elmerCommands), h.methods())
	}
}

// TestCommandArgsLandOnFEATab asserts every Elmer command's built commandArgs places it on the
// shared FEA ribbon tab, in Elmer's own category — checked directly against the wire.CreateCommandArgs
// values (as ccx's ribbon_layout_test.go does), not through a fake host that discards the payload.
func TestCommandArgsLandOnFEATab(t *testing.T) {
	for _, c := range elmerCommands {
		a := commandArgs(c.id, c.name, c.tip)
		if a.Ribbon != types.PartRibbon || a.Tab != elmerRibbonTab || a.Category != elmerRibbonPanel {
			t.Errorf("command %q placement wrong: ribbon=%q tab=%q category=%q", c.id, a.Ribbon, a.Tab, a.Category)
		}
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
