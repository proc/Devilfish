package search

// SearchStats counts what the search did, for judging how well move ordering
// and each pruning rule are working ("bench stats"). The counters are plain
// increments on the worker's own memory and do not affect the search.
type SearchStats struct {
	Nodes  uint64 // main-search nodes (depth > 0)
	QNodes uint64 // quiescence nodes

	TTProbes uint64 // main search
	TTHits   uint64
	TTCuts   uint64

	RFPCuts    uint64 // reverse futility pruning
	RFPGuarded uint64 // would have cut, but a threat/king-safety guard forbade it
	RazorTries uint64
	RazorCuts  uint64
	NMPTries   uint64 // null-move searches
	NMPCuts    uint64
	NMPGuarded uint64 // eligible, but a threat/king-safety guard forbade it
	ProbTries  uint64 // ProbCut nodes that searched at least one capture
	ProbCuts   uint64
	IIR        uint64
	SETries    uint64 // singular searches
	SEExtends  uint64
	SEMultiCut uint64
	CheckExts  uint64

	LMPPruned  uint64 // moves skipped by each move-loop rule
	FutPruned  uint64
	HistPruned uint64
	SEEPruned  uint64

	MovesSearched uint64 // moves made in the main search
	LMRSearches   uint64 // moves searched at reduced depth
	LMRPlies      uint64 // total plies of reduction
	LMRReSearches uint64 // reduced searches that beat alpha and were repeated
	LMREligible   uint64 // late moves at depth >= 3, not in check: candidates for reduction
	LMRTableZero  uint64 // of those, the table itself gave no reduction
	LMRNoCheck    uint64 // not reduced because the move gives check
	LMRNoThreat   uint64 // not reduced: makes or escapes a threat
	LMRNoPawn     uint64 // not reduced: advanced pawn push
	LMRNoCapture  uint64 // not reduced: capture or promotion
	LMRNoAdjust   uint64 // reduction cancelled by the history/killer/improving adjustments
	PVSReSearches uint64 // null-window searches repeated with the full window

	NodesWithMoves uint64 // nodes that searched at least one move
	BetaCuts       uint64
	BetaCutsFirst  uint64 // on the first move searched
	BetaCutsTT     uint64 // by the hash move
	BetaCutIndex   uint64 // sum of (moves searched before the cutoff move)
	AllNodes       uint64 // nodes where no move beat alpha

	QStandPatCuts uint64
	QFutPruned    uint64
	QSEEPruned    uint64
	QMoves        uint64
	QBetaCuts     uint64
	QBetaFirst    uint64
}

// Add accumulates o into s.
func (s *SearchStats) Add(o *SearchStats) {
	s.Nodes += o.Nodes
	s.QNodes += o.QNodes
	s.TTProbes += o.TTProbes
	s.TTHits += o.TTHits
	s.TTCuts += o.TTCuts
	s.RFPCuts += o.RFPCuts
	s.RFPGuarded += o.RFPGuarded
	s.RazorTries += o.RazorTries
	s.RazorCuts += o.RazorCuts
	s.NMPTries += o.NMPTries
	s.NMPCuts += o.NMPCuts
	s.NMPGuarded += o.NMPGuarded
	s.ProbTries += o.ProbTries
	s.ProbCuts += o.ProbCuts
	s.IIR += o.IIR
	s.SETries += o.SETries
	s.SEExtends += o.SEExtends
	s.SEMultiCut += o.SEMultiCut
	s.CheckExts += o.CheckExts
	s.LMPPruned += o.LMPPruned
	s.FutPruned += o.FutPruned
	s.HistPruned += o.HistPruned
	s.SEEPruned += o.SEEPruned
	s.MovesSearched += o.MovesSearched
	s.LMRSearches += o.LMRSearches
	s.LMRPlies += o.LMRPlies
	s.LMRReSearches += o.LMRReSearches
	s.LMREligible += o.LMREligible
	s.LMRTableZero += o.LMRTableZero
	s.LMRNoCheck += o.LMRNoCheck
	s.LMRNoThreat += o.LMRNoThreat
	s.LMRNoPawn += o.LMRNoPawn
	s.LMRNoCapture += o.LMRNoCapture
	s.LMRNoAdjust += o.LMRNoAdjust
	s.PVSReSearches += o.PVSReSearches
	s.NodesWithMoves += o.NodesWithMoves
	s.BetaCuts += o.BetaCuts
	s.BetaCutsFirst += o.BetaCutsFirst
	s.BetaCutsTT += o.BetaCutsTT
	s.BetaCutIndex += o.BetaCutIndex
	s.AllNodes += o.AllNodes
	s.QStandPatCuts += o.QStandPatCuts
	s.QFutPruned += o.QFutPruned
	s.QSEEPruned += o.QSEEPruned
	s.QMoves += o.QMoves
	s.QBetaCuts += o.QBetaCuts
	s.QBetaFirst += o.QBetaFirst
}
