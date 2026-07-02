// SPDX-License-Identifier: GPL-2.0-only

// Package elmer is the host-facing core of the Elmer multiphysics FEA add-in: it
// turns host bodies into finite-element studies (surface mesh → volume mesh →
// solver input → solve → field render) using only the Apache-2.0
// oblikovati.org/api client. The cgo c-shared shell (../export.go) owns the C ABI;
// this package owns the pipeline and stays cgo-free so it unit-tests everywhere.
package elmer

import (
	"sync"

	"oblikovati.org/api/client"
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

// Notify receives host event bytes. Grown in later tasks.
func (e *Engine) Notify(ev []byte) {}
