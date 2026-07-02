// SPDX-License-Identifier: GPL-2.0-only

package elmer

import (
	"oblikovati.org/api/types"
	"oblikovati.org/api/wire"
)

// RunStudyCommandID is the host command the add-in registers; firing it (a ribbon click or
// the MCP bridge's execute_command) runs the FEA study on the active part.
const RunStudyCommandID = "Elmer.RunStudy"

// ShowPanelCommandID re-opens the study-parameters panel from the ribbon. Registered here
// since M0; dispatched to (*Engine).ShowPanel by engine.go's onCommandStarted (Task 12).
const ShowPanelCommandID = "Elmer.ShowPanel"

// elmerRibbonTab is the shared FEA document ribbon tab Elmer places its commands on (spec §15
// Q3 default: share the tab with sibling FEA add-ins, keep our own panel on it).
const elmerRibbonTab = "FEA"

// elmerRibbonPanel is Elmer's own panel on the shared FEA tab.
const elmerRibbonPanel = "Elmer"

// elmerCommands is the exhaustive command list; RegisterCommands places each on the FEA tab.
var elmerCommands = []struct{ id, name, tip string }{
	{RunStudyCommandID, "Run Study", "Mesh, solve, and visualize the field results of the active part with Elmer."},
	{ShowPanelCommandID, "Study Panel", "Open the Elmer study-parameters panel."},
}

// Start performs the one-time host-facing initialization: register the add-in's commands. It
// MUST NOT run on the host's session goroutine (host calls there block until the frame loop
// drains the dispatcher, deadlocking the head) — the cgo shell runs it on its own goroutine.
func (e *Engine) Start() error {
	return e.RegisterCommands()
}

// RegisterCommands registers every Elmer command on the FEA ribbon tab (also invokable over
// the MCP bridge's execute_command). Command actions fire command.started, which Notify dispatches.
func (e *Engine) RegisterCommands() error {
	for _, c := range elmerCommands {
		if _, err := e.api.Commands().Create(commandArgs(c.id, c.name, c.tip)); err != nil {
			return err
		}
	}
	return nil
}

// commandArgs builds the host command-registration args, placing the command on Elmer's own
// panel of the shared FEA tab.
func commandArgs(id, name, tip string) wire.CreateCommandArgs {
	return wire.CreateCommandArgs{
		ID: id, DisplayName: name, Tooltip: tip,
		Ribbon: types.PartRibbon, Tab: elmerRibbonTab, Category: elmerRibbonPanel,
	}
}
