// SPDX-License-Identifier: GPL-2.0-only

package elmer

import (
	"strconv"
	"strings"

	"oblikovati.org/api/client"
	"oblikovati.org/api/types"
	"oblikovati.org/api/wire"
)

// panel.go is the M1-era grouped panel: Mesh (size/order), Material (young/poisson/
// density), Load (type + magnitude), Result (field selector), and a Run button — the
// grouped-panel subset of Oblikovati.AddIns.CalculiX's ccx/panel.go (package ccx -> elmer
// only), trimmed to M1's single-equation, single-material, single-load-template aggregate
// (no analysis-type switch, no constraint builder, no per-body scoping).

// PanelID is the stable dockable-window id the Elmer add-in owns.
const PanelID = "com.oblikovati.elmer.panel"

// ShowPanel creates (or replaces) the Elmer study-parameters dockable window: the editable
// study settings plus a Run button. Edits arrive as panel.valueChanged events
// (applyPanelEdit). The result-field selector is engine state, not part of the femmodel
// aggregate (see applyResultEdit's doc comment), so it is read separately from study().
func (e *Engine) ShowPanel() (wire.OKResult, error) {
	s := e.study()
	rf := e.resultFieldKind()
	return e.api.DockableWindows().Set(wire.DockableWindowSpec{
		ID:       PanelID,
		Title:    "Elmer FEM",
		Dock:     types.DockRight,
		Visible:  true,
		Controls: panelControls(s, rf),
	})
}

// panelControls builds the parameter controls, grouped into titled sections: Mesh,
// Material, Load, Result, then a "Run Elmer Study" button. The host's control vocabulary
// has no real group box, so each section is a heading label followed by its controls and a
// trailing separator (mirrors ccx/panel.go's section layout).
func panelControls(s StudySettings, resultField string) []wire.PanelControlSpec {
	return joinControls(
		header("Elmer FEM Study", "Select the fixed face first, then the loaded face(s)."),
		meshSection(s),
		materialSection(s),
		loadSection(s),
		resultSection(resultField),
		[]wire.PanelControlSpec{client.PanelButton("run", "Run Elmer Study", RunStudyCommandID)},
	)
}

// meshSection builds the Mesh control group: element size and order.
func meshSection(s StudySettings) []wire.PanelControlSpec {
	return section("Mesh",
		client.PanelTextBox("mesh_size", "Max element size (mm)", formatNum(s.Mesh.MaxSizeMM)),
		client.PanelDropdown("element_order", "Element order", elementOrderOptions(), elementOrderLabel(s.Mesh.Order)),
	)
}

// materialSection builds the Material control group: the M1 single-material fields.
func materialSection(s StudySettings) []wire.PanelControlSpec {
	return section("Material",
		client.PanelTextBox("young", "Young's modulus (GPa)", formatNum(s.Material.YoungGPa)),
		client.PanelTextBox("poisson", "Poisson's ratio", formatNum(s.Material.Poisson)),
		client.PanelTextBox("density", "Density (g/cm³)", formatNum(s.Material.DensityGCm3)),
	)
}

// loadSection builds the Load control group: the type selector plus both magnitude fields
// (only the one matching load_type is written into the deck — see deck.go's
// elasticityInputFrom).
func loadSection(s StudySettings) []wire.PanelControlSpec {
	return section("Load",
		client.PanelDropdown("load_type", "Load type", loadTypeOptions(), s.Load.LoadType),
		client.PanelTextBox("load", "Force on loaded faces (N)", formatNum(s.Load.LoadN)),
		client.PanelTextBox("pressure", "Pressure on loaded faces (MPa)", formatNum(s.Load.PressureMPa)),
	)
}

// loadTypeOptions lists the load-type dropdown's choices in display order.
func loadTypeOptions() []string { return []string{"force", "pressure"} }

// resultSection builds the Result control group: the rendered-field selector.
func resultSection(field string) []wire.PanelControlSpec {
	return section("Result",
		client.PanelDropdown("result_field", "Result field", resultFieldOptions(), field),
	)
}

// resultFieldOptions lists the result-field dropdown's choices in display order.
func resultFieldOptions() []string { return []string{resultFieldVonMises, resultFieldDisplacement} }

// header builds the panel's title + a one-line usage hint.
func header(title, hint string) []wire.PanelControlSpec {
	return []wire.PanelControlSpec{
		client.PanelLabel("hdr", title),
		client.PanelLabel("hint", hint),
		client.PanelSeparator(),
	}
}

// section builds a titled control group: a heading label, the controls, and a trailing
// separator (the dockable-window analog of a group box).
func section(title string, controls ...wire.PanelControlSpec) []wire.PanelControlSpec {
	out := []wire.PanelControlSpec{client.PanelLabel(labelID(title), title)}
	out = append(out, controls...)
	return append(out, client.PanelSeparator())
}

// joinControls flattens the section groups into one control list.
func joinControls(groups ...[]wire.PanelControlSpec) []wire.PanelControlSpec {
	var out []wire.PanelControlSpec
	for _, g := range groups {
		out = append(out, g...)
	}
	return out
}

// labelID derives a stable control id for a section heading from its title.
func labelID(title string) string {
	return "sec_" + strings.ToLower(strings.ReplaceAll(strings.Fields(title)[0], "&", "and"))
}

// applyPanelEdit writes one edited study parameter back into the engine, keyed by control
// id. The Mesh/Material/Load controls reach the femmodel aggregate via their typed
// applyAgg* helpers; the Result control reaches the engine-only resultField (see
// applyResultEdit).
func (e *Engine) applyPanelEdit(controlID, value string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.applyAggMeshEdit(controlID, value) {
		return
	}
	if e.applyAggMaterialEdit(controlID, value) {
		return
	}
	if e.applyAggLoadEdit(controlID, value) {
		return
	}
	e.applyResultEdit(controlID, value)
}

// applyAggMeshEdit routes mesh_size/element_order to the Analysis.Mesh aggregate.
func (e *Engine) applyAggMeshEdit(controlID, value string) bool {
	m := e.analysis.Mesh()
	switch controlID {
	case "mesh_size":
		m.MaxSizeMM = panelNum(value, m.MaxSizeMM)
	case "element_order":
		m.Order = parseElementOrder(value, m.Order)
	default:
		return false
	}
	e.analysis.SetMesh(m)
	return true
}

// applyAggMaterialEdit routes young/poisson/density to the Analysis.DefaultMaterial
// aggregate.
func (e *Engine) applyAggMaterialEdit(controlID, value string) bool {
	mat := e.analysis.DefaultMaterial()
	switch controlID {
	case "young":
		mat.YoungGPa = panelNum(value, mat.YoungGPa)
	case "poisson":
		mat.Poisson = panelNum(value, mat.Poisson)
	case "density":
		mat.DensityGCm3 = panelNum(value, mat.DensityGCm3)
	default:
		return false
	}
	e.analysis.SetDefaultMaterial(mat)
	return true
}

// applyAggLoadEdit routes load_type/load/pressure to the Analysis.LoadDefaults aggregate.
func (e *Engine) applyAggLoadEdit(controlID, value string) bool {
	l := e.analysis.LoadDefaults()
	switch controlID {
	case "load_type":
		l.LoadType = strings.TrimSpace(value)
	case "load":
		l.LoadN = panelNum(value, l.LoadN)
	case "pressure":
		l.PressureMPa = panelNum(value, l.PressureMPa)
	default:
		return false
	}
	e.analysis.SetLoadDefaults(l)
	return true
}

// applyResultEdit routes the Result group's field selector to the engine-only
// resultField: M1's femmodel aggregate has no Results object yet (see
// femmodel.Analysis's doc comment — no flat-settings escape hatch, and M1 seeds no result
// object at all), so the selector lives on the engine under e.mu until a later milestone
// grows the aggregate a real ResultObject (task-12-report.md).
func (e *Engine) applyResultEdit(controlID, value string) {
	if controlID == "result_field" {
		e.resultField = strings.TrimSpace(value)
	}
}

// elementOrderOptions / elementOrderLabel / parseElementOrder map femmodel.MeshObject's
// Order (1|2) to the dropdown's human-readable labels.
func elementOrderOptions() []string { return []string{"linear (order 1)", "quadratic (order 2)"} }

func elementOrderLabel(order int) string {
	if order == 1 {
		return "linear (order 1)"
	}
	return "quadratic (order 2)"
}

func parseElementOrder(value string, fallback int) int {
	switch {
	case strings.HasPrefix(value, "linear"):
		return 1
	case strings.HasPrefix(value, "quadratic"):
		return 2
	default:
		return fallback
	}
}

// formatNum renders a parameter value compactly (no trailing zeros) for the panel.
func formatNum(v float64) string { return strconv.FormatFloat(v, 'g', -1, 64) }

// panelNum reads the leading number from a form value (e.g. "5 mm" -> 5), keeping the
// fallback when the field is empty or half-typed.
func panelNum(value string, fallback float64) float64 {
	fields := strings.Fields(value)
	if len(fields) == 0 {
		return fallback
	}
	v, err := strconv.ParseFloat(fields[0], 64)
	if err != nil {
		return fallback
	}
	return v
}
