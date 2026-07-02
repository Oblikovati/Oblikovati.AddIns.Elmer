// SPDX-License-Identifier: GPL-2.0-only

package main

// Blank import: the //go:embed directive below needs the embed package linked even
// though no symbol from it is referenced directly.
import _ "embed"

// manifestJSON is the add-in manifest returned over the C ABI (ObkAddInManifest).
// Embedded from manifest.json so the file is the single source of truth.
//
//go:embed manifest.json
var manifestJSON string
