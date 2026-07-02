// SPDX-License-Identifier: GPL-2.0-only

package elmer

import (
	"oblikovati.org/api/types"
	"oblikovati.org/api/wire"
)

// RunStudyCommandID is the host command the add-in registers on the Part ribbon; firing it (a
// ribbon click, the study panel's Run button, or the MCP bridge's execute_command) runs the
// FEA study on the active document.
const RunStudyCommandID = "Elmer.RunStudy"

// RunStudyAssemblyCommandID is RunStudyCommandID's Assembly-ribbon twin — see elmerCommands'
// doc comment for why placing one logical command on two ribbons needs two ids.
const RunStudyAssemblyCommandID = "Elmer.RunStudy.Assembly"

// ShowPanelCommandID re-opens the study-parameters panel from the Part ribbon. Registered
// here since M0; dispatched to (*Engine).ShowPanel by engine.go's onCommandStarted (Task 12).
const ShowPanelCommandID = "Elmer.ShowPanel"

// ShowPanelAssemblyCommandID is ShowPanelCommandID's Assembly-ribbon twin.
const ShowPanelAssemblyCommandID = "Elmer.ShowPanel.Assembly"

// elmerRibbonTab is Elmer's own document ribbon tab (user directive: Elmer no longer shares
// the "FEA" tab with sibling FEA add-ins — it gets a dedicated "Elmer" tab, mirroring
// Oblikovati.AddIns.CAM's dedicated "CAM" tab, camRibbonTab).
const elmerRibbonTab = "Elmer"

// elmerRibbonPanel is the panel Elmer's commands sit on within its own tab. Named "Study"
// rather than "Elmer" (which would be redundant with the tab name itself) — following
// Oblikovati.AddIns.CalculiX's ccx/ribbon_layout.go convention of naming a FEA add-in's panel
// after what its commands DO ("Solve" there; "Study" here, matching the study panel/commands
// this add-in actually has).
const elmerRibbonPanel = "Study"

// elmerCommands is the exhaustive command list, one entry per (action, ribbon) placement.
// wire.CreateCommandArgs.Ribbon carries exactly one ribbon per commands.create call, and the
// host's command registry keys purely on id (a duplicate id errors, app.CommandManager.Add) —
// there is no wire-level equivalent of the GPL host's own multi-ribbon
// CommandDefinition.WithRibbons(Part, Assembly) built-ins use (e.g. app/commands_sketch.go).
// So putting one logical action on both the Part and Assembly ribbons over the API needs two
// distinct ids, each registered once; engine.go's onCommandStarted dispatches both ids of a
// pair to the same handler, so the command still behaves as one logical action to the user.
var elmerCommands = []struct {
	id, name, tip string
	ribbon        types.RibbonKey
}{
	{RunStudyCommandID, "Run Study", "Mesh, solve, and visualize the field results of the active part with Elmer.", types.PartRibbon},
	{RunStudyAssemblyCommandID, "Run Study", "Mesh, solve, and visualize the field results of the active part with Elmer.", types.AssemblyRibbon},
	{ShowPanelCommandID, "Study Panel", "Open the Elmer study-parameters panel.", types.PartRibbon},
	{ShowPanelAssemblyCommandID, "Study Panel", "Open the Elmer study-parameters panel.", types.AssemblyRibbon},
}

// Start performs the one-time host-facing initialization: register the add-in's commands. It
// MUST NOT run on the host's session goroutine (host calls there block until the frame loop
// drains the dispatcher, deadlocking the head) — the cgo shell runs it on its own goroutine.
func (e *Engine) Start() error {
	return e.RegisterCommands()
}

// RegisterCommands registers every Elmer command on its own "Elmer" ribbon tab, on both the
// Part and Assembly ribbons (also invokable over the MCP bridge's execute_command). Command
// actions fire command.started, which Notify dispatches.
func (e *Engine) RegisterCommands() error {
	for _, c := range elmerCommands {
		if _, err := e.api.Commands().Create(commandArgs(c.id, c.name, c.tip, c.ribbon)); err != nil {
			return err
		}
	}
	return nil
}

// commandArgs builds the host command-registration args, placing the command on Elmer's own
// "Study" panel of its own "Elmer" ribbon tab, on the given ribbon.
func commandArgs(id, name, tip string, ribbon types.RibbonKey) wire.CreateCommandArgs {
	return wire.CreateCommandArgs{
		ID: id, DisplayName: name, Tooltip: tip,
		Ribbon: ribbon, Tab: elmerRibbonTab, Category: elmerRibbonPanel,
	}
}
