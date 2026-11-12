package uci

import (
	"testing"

	"devilfish/internal/search"
)

func TestApplySearchParams(t *testing.T) {
	saved := search.P
	defer func() {
		search.P = saved
		search.SetParam("LMRBase", saved.LMRBase) // rebuilds the LMR table
	}()

	applySearchParams(map[string]int{"LMRBase": 45, "rfp_depth": 6, "Fut-Margin": 100, "QSChecks": 1, "RFPMargin": 99999, "NoSuchKnob": 3})
	got := search.P
	if got.LMRBase != 45 || got.RFPDepth != 6 || got.FutMargin != 100 || got.QSChecks != 1 {
		t.Errorf("values not applied: %+v", got)
	}
	if got.RFPMargin != 200 {
		t.Errorf("RFPMargin should be clamped to its maximum 200, got %d", got.RFPMargin)
	}
}
