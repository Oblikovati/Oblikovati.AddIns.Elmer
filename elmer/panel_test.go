// SPDX-License-Identifier: GPL-2.0-only

package elmer

import (
	"encoding/json"
	"testing"

	"oblikovati.org/api/wire"
)

// TestPanelEditRoutesMeshMaterialLoadToAggregate proves every Mesh/Material/Load control
// lands in the femmodel aggregate via applyPanelEdit's typed applyAgg* helpers.
func TestPanelEditRoutesMeshMaterialLoadToAggregate(t *testing.T) {
	e := NewEngine(nil)

	e.applyPanelEdit("mesh_size", "3.5")
	e.applyPanelEdit("element_order", "linear (order 1)")
	m := e.analysis.Mesh()
	if m.MaxSizeMM != 3.5 || m.Order != 1 {
		t.Fatalf("mesh edits did not land in the aggregate: %+v", m)
	}

	e.applyPanelEdit("young", "123")
	e.applyPanelEdit("poisson", "0.28")
	e.applyPanelEdit("density", "2.7")
	mat := e.analysis.DefaultMaterial()
	if mat.YoungGPa != 123 || mat.Poisson != 0.28 || mat.DensityGCm3 != 2.7 {
		t.Fatalf("material edits did not land in the aggregate: %+v", mat)
	}

	e.applyPanelEdit("load_type", "pressure")
	e.applyPanelEdit("load", "500")
	e.applyPanelEdit("pressure", "2.5")
	l := e.analysis.LoadDefaults()
	if l.LoadType != "pressure" || l.LoadN != 500 || l.PressureMPa != 2.5 {
		t.Fatalf("load edits did not land in the aggregate: %+v", l)
	}
}

// TestPanelEditRoutesResultFieldToEngineOnlyField proves the Result group's selector lands
// on the engine's own resultField (M1's femmodel aggregate has no Results object yet — see
// panel.go's applyResultEdit doc comment), NOT the aggregate.
func TestPanelEditRoutesResultFieldToEngineOnlyField(t *testing.T) {
	e := NewEngine(nil)
	if got := e.resultFieldKind(); got != resultFieldVonMises {
		t.Fatalf("default resultField = %q, want %q", got, resultFieldVonMises)
	}
	e.applyPanelEdit("result_field", "displacement")
	if got := e.resultFieldKind(); got != resultFieldDisplacement {
		t.Fatalf("result_field edit did not land in the engine field: %v", got)
	}
}

// TestStudyProjectsAggregateEdits proves study() reflects an applyPanelEdit mutation —
// the seam runStudy reads from.
func TestStudyProjectsAggregateEdits(t *testing.T) {
	e := NewEngine(nil)
	e.applyPanelEdit("young", "70")
	s := e.study()
	if s.Material.YoungGPa != 70 {
		t.Fatalf("study() did not reflect the young edit: %+v", s.Material)
	}
}

// TestShowPanelSetsDockableWindow proves ShowPanel calls dockable_windows.set exactly
// once, carrying the Run button bound to RunStudyCommandID.
func TestShowPanelSetsDockableWindow(t *testing.T) {
	h := newFakeHost()
	e := NewEngine(h)
	if _, err := e.ShowPanel(); err != nil {
		t.Fatalf("ShowPanel: %v", err)
	}
	if got := h.count(wire.MethodDockableWindowsSet); got != 1 {
		t.Fatalf("dockable_windows.set called %d times, want 1; calls=%v", got, h.methods())
	}
}

// TestPanelControlsIncludeRunButton pins panelControls' Run button: id "run", bound to
// RunStudyCommandID, so a click actually fires the study.
func TestPanelControlsIncludeRunButton(t *testing.T) {
	controls := panelControls(StudySettings{}, resultFieldVonMises)
	for _, c := range controls {
		if c.ID == "run" {
			if c.CommandID != RunStudyCommandID {
				t.Fatalf("Run button CommandID = %q, want %q", c.CommandID, RunStudyCommandID)
			}
			return
		}
	}
	t.Fatal("panelControls: no Run button found")
}

// TestNotifyPanelValueChangedRoutesToAggregate proves the wire-level event path: a
// panel.valueChanged event for our PanelID reaches applyPanelEdit.
func TestNotifyPanelValueChangedRoutesToAggregate(t *testing.T) {
	e := NewEngine(newFakeHost())
	ev, err := marshalPanelValueChanged(PanelID, "young", "99")
	if err != nil {
		t.Fatalf("marshal event: %v", err)
	}
	e.Notify(ev)
	if mat := e.analysis.DefaultMaterial(); mat.YoungGPa != 99 {
		t.Fatalf("Notify did not route the panel edit to the aggregate: %+v", mat)
	}
}

// TestNotifyPanelValueChangedIgnoresOtherWindows proves an event for a different window id
// is dropped rather than mistakenly applied to our aggregate.
func TestNotifyPanelValueChangedIgnoresOtherWindows(t *testing.T) {
	e := NewEngine(newFakeHost())
	before := e.analysis.DefaultMaterial().YoungGPa
	ev, err := marshalPanelValueChanged("some.other.panel", "young", "99")
	if err != nil {
		t.Fatalf("marshal event: %v", err)
	}
	e.Notify(ev)
	if got := e.analysis.DefaultMaterial().YoungGPa; got != before {
		t.Fatalf("Notify applied an edit meant for a different window: young = %v, want unchanged %v", got, before)
	}
}

// TestElementOrderLabelAndParseRoundTrip pins both dropdown-mapping directions for the
// quadratic branch (the linear branch is already exercised by
// TestPanelEditRoutesMeshMaterialLoadToAggregate).
func TestElementOrderLabelAndParseRoundTrip(t *testing.T) {
	if got := elementOrderLabel(2); got != "quadratic (order 2)" {
		t.Errorf("elementOrderLabel(2) = %q, want %q", got, "quadratic (order 2)")
	}
	if got := parseElementOrder("quadratic (order 2)", 1); got != 2 {
		t.Errorf("parseElementOrder(quadratic) = %d, want 2", got)
	}
	if got := parseElementOrder("garbage", 2); got != 2 {
		t.Errorf("parseElementOrder(garbage) = %d, want the fallback 2", got)
	}
}

// TestPanelNumFallsBackOnEmptyOrInvalid pins panelNum's two fallback paths: an empty value
// and a non-numeric leading token both keep the caller's current setting.
func TestPanelNumFallsBackOnEmptyOrInvalid(t *testing.T) {
	if got := panelNum("", 42); got != 42 {
		t.Errorf("panelNum(\"\") = %v, want the fallback 42", got)
	}
	if got := panelNum("not-a-number", 42); got != 42 {
		t.Errorf("panelNum(garbage) = %v, want the fallback 42", got)
	}
	if got := panelNum("5 mm", 0); got != 5 {
		t.Errorf("panelNum(\"5 mm\") = %v, want 5", got)
	}
}

// marshalPanelValueChanged builds the wire bytes for a panel.valueChanged event.
func marshalPanelValueChanged(windowID, controlID, value string) ([]byte, error) {
	return json.Marshal(struct {
		Type      string `json:"type"`
		WindowId  string `json:"windowId"`
		ControlId string `json:"controlId"`
		Value     string `json:"value"`
	}{Type: wire.EventPanelValueChanged, WindowId: windowID, ControlId: controlID, Value: value})
}
