package eval

type Score struct {
	mg int32
	eg int32
}

func (s Score) Add(other Score) Score {
	return Score{mg: s.mg + other.mg, eg: s.eg + other.eg}
}

func (s Score) Sub(other Score) Score {
	return Score{mg: s.mg - other.mg, eg: s.eg - other.eg}
}

func (s Score) MG() int32 {
	return s.mg
}

func (s Score) EG() int32 {
	return s.eg
}

const (
	PawnPhase   = 0
	KnightPhase = 1
	BishopPhase = 1
	RookPhase   = 2
	QueenPhase  = 4
	TotalPhase  = 24
)

var PiecePhase = [7]int32{
	0,           // None
	PawnPhase,   // Pawn
	KnightPhase, // Knight
	BishopPhase, // Bishop
	RookPhase,   // Rook
	QueenPhase,  // Queen
	0,           // King
}

var PieceValues = [7]Score{
	{mg: 0, eg: 0},
	{mg: 100, eg: 130},  // Pawn
	{mg: 320, eg: 335},  // Knight
	{mg: 330, eg: 345},  // Bishop
	{mg: 500, eg: 530},  // Rook
	{mg: 950, eg: 1020}, // Queen
	{mg: 0, eg: 0},      // King
}

var SEEPieceValues = [7]int32{
	0,
	100,  // Pawn
	320,  // Knight
	330,  // Bishop
	500,  // Rook
	950,  // Queen
	20000, // King
}
