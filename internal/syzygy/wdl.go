package syzygy

import "fmt"

// WDLToString converts a Syzygy WDL constant into a human-readable string.
func WDLToString(wdl int) string {
	switch wdl {
	case Win:
		return "Win"
	case CursedWin:
		return "Cursed Win (Draw under 50-move rule)"
	case Draw:
		return "Draw"
	case BlessedLoss:
		return "Blessed Loss (Draw under 50-move rule)"
	case Loss:
		return "Loss"
	default:
		return fmt.Sprintf("WDL=%d", wdl)
	}
}

// OpponentWDLToOurResult converts an opponent-perspective WDL outcome to our perspective.
func OpponentWDLToOurResult(wdl int) string {
	switch wdl {
	case Loss:
		return "Win"
	case BlessedLoss:
		return "Cursed Win (Draw under 50-move rule)"
	case Draw:
		return "Draw"
	case CursedWin:
		return "Blessed Loss (Draw under 50-move rule)"
	case Win:
		return "Loss"
	default:
		return fmt.Sprintf("WDL=%d", wdl)
	}
}
