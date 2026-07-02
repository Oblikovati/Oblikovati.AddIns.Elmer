// SPDX-License-Identifier: GPL-2.0-only

package elmer

// modelUnitM is the host length unit expressed in metres: the kernel length unit is the
// centimetre (1 model unit = 0.01 m, see ADR-0042 / units of measure #146 in the host
// repo). ElmerSolver's native mesh-database format is unit-agnostic but this add-in's SIF
// decks (Task 11+) and meshfmt.Mesh (Task 9) are written in SI (metres/Pa/etc.), so host
// coordinates are scaled by this on the way in.
//
// This mirrors Oblikovati.AddIns.CalculiX's ccx/units.go modelUnitMM (host cm -> mm, *10)
// with the target unit changed: CalculiX decks are written in mm/N/MPa, ElmerSolver's SI
// convention wants metres, so the scale factor is *0.01 rather than *10 (renamed from
// modelUnitMM to avoid a misleading "MM" name on a metres-denominated constant — the only
// deliberate identifier deviation from the ccx clone, see task-10-report.md).
const modelUnitM = 0.01
