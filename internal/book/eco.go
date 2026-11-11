package book

import (
	"fmt"
	"strings"
	"sync"

	"devilfish/internal/board"
	"devilfish/internal/movegen"
	"devilfish/internal/types"
)

// OpeningInfo represents an ECO code and descriptive opening name.
type OpeningInfo struct {
	ECO  string
	Name string
}

func (o OpeningInfo) String() string {
	if o.ECO == "" && o.Name == "" {
		return ""
	}
	if o.ECO == "" {
		return o.Name
	}
	if o.Name == "" {
		return o.ECO
	}
	return fmt.Sprintf("%s: %s", o.ECO, o.Name)
}

type rawOpening struct {
	eco   string
	name  string
	moves string
}

var rawOpenings = []rawOpening{
	// ==========================================
	// A: Flank Openings & Unusual Openings
	// ==========================================
	{"A00", "Polish Opening (Sokolsky)", "b2b4"},
	{"A00", "Grob Opening", "g2g4"},
	{"A01", "Nimzo-Larsen Attack", "b2b3"},
	{"A02", "Bird's Opening", "f2f4"},
	{"A02", "Bird's Opening, From's Gambit", "f2f4 e7e5"},
	{"A04", "Réti Opening", "g1f3"},
	{"A05", "Réti Opening, King's Indian Attack", "g1f3 g8f6 g2g3"},
	{"A06", "Réti Opening, Nimzowitsch-Larsen", "g1f3 d7d5 b2b3"},
	{"A07", "King's Indian Attack", "g1f3 d7d5 g2g3 g8f6 f1g2 c7c6 e1g1 c8g4 d2d3 b8d7 b1d2 e7e5 e2e4"},
	{"A10", "English Opening", "c2c4"},
	{"A10", "English Opening, Anglo-Scandinavian", "c2c4 d7d5"},
	{"A13", "English Opening, Agincourt Defense", "c2c4 e7e6"},
	{"A15", "English Opening, Anglo-Indian Defense", "c2c4 g8f6"},
	{"A20", "English Opening, King's English", "c2c4 e7e5"},
	{"A21", "English Opening, King's English, Reversed Sicilian", "c2c4 e7e5 b1c3"},
	{"A22", "English Opening, Two Knights", "c2c4 e7e5 b1c3 g8f6"},
	{"A25", "English Opening, Closed System", "c2c4 e7e5 b1c3 b8c6 g2g3 g7g6 f1g2 f8g7 d2d3 d7d6"},
	{"A28", "English Opening, Four Knights", "c2c4 e7e5 b1c3 g8f6 g1f3 b8c6"},
	{"A30", "English Opening, Symmetrical", "c2c4 c7c5"},
	{"A34", "English Opening, Symmetrical, Three Knights", "c2c4 c7c5 b1c3 g8f6 g1f3 d7d5"},
	{"A35", "English Opening, Symmetrical, Four Knights", "c2c4 c7c5 b1c3 b8c6 g1f3 g8f6"},
	{"A40", "Queen's Pawn Game", "d2d4"},
	{"A40", "English Defense", "d2d4 b7b6"},
	{"A41", "Old Indian Defense", "d2d4 d7d6 g1f3 c8g4"},
	{"A43", "Old Benoni Defense", "d2d4 c7c5"},
	{"A45", "Trompowsky Attack", "d2d4 g8f6 c1g5"},
	{"A46", "Torre Attack", "d2d4 g8f6 g1f3 e7e6 c1g5"},
	{"A48", "King's Indian Attack (East Indian Defense)", "d2d4 g8f6 g1f3 g7g6 c1g5"},
	{"A51", "Budapest Gambit", "d2d4 g8f6 c2c4 e7e5"},
	{"A52", "Budapest Gambit Accepted", "d2d4 g8f6 c2c4 e7e5 d4e5 g8g4"},
	{"A56", "Benoni Defense", "d2d4 g8f6 c2c4 c7c5 d4d5"},
	{"A57", "Benko Gambit", "d2d4 g8f6 c2c4 c7c5 d4d5 b7b5"},
	{"A58", "Benko Gambit Accepted", "d2d4 g8f6 c2c4 c7c5 d4d5 b7b5 c4b5 a7a6 b5a6"},
	{"A60", "Modern Benoni", "d2d4 g8f6 c2c4 c7c5 d4d5 e7e6"},
	{"A80", "Dutch Defense", "d2d4 f7f5"},
	{"A84", "Dutch Defense, Classical", "d2d4 f7f5 c2c4 e7e6 g1f3 g8f6 g2g3 f8e7 f1g2 e1g1"},
	{"A87", "Dutch Defense, Leningrad", "d2d4 f7f5 c2c4 g8f6 g1f3 g7g6 g2g3 f8g7 f1g2"},
	{"A90", "Dutch Defense, Stonewall", "d2d4 f7f5 c2c4 e7e6 g2g3 g8f6 f1g2 d7d5 g1f3 c7c6"},

	// ==========================================
	// B: Semi-Open Games (excluding French)
	// ==========================================
	{"B00", "King's Pawn Game", "e2e4"},
	{"B00", "Nimzowitsch Defense", "e2e4 b8c6"},
	{"B00", "Owen's Defense", "e2e4 b7b6"},
	{"B01", "Scandinavian Defense", "e2e4 d7d5"},
	{"B01", "Scandinavian Defense, Mieses-Kotroc", "e2e4 d7d5 e4d5 d8d5 b1c3 d5a5"},
	{"B01", "Scandinavian Defense, Modern", "e2e4 d7d5 e4d5 g8f6"},
	{"B02", "Alekhine's Defense", "e2e4 g8f6"},
	{"B03", "Alekhine's Defense, Four Pawns Attack", "e2e4 g8f6 e4e5 f6d5 d2d4 d7d6 c2c4 d5b6 f2f4"},
	{"B04", "Alekhine's Defense, Modern Variation", "e2e4 g8f6 e4e5 f6d5 d2d4 d7d6 g1f3"},
	{"B06", "Modern Defense (Robatsch)", "e2e4 g7g6"},
	{"B07", "Pirc Defense", "e2e4 d7d6 d2d4 g8f6 b1c3 g7g6"},
	{"B09", "Pirc Defense, Austrian Attack", "e2e4 d7d6 d2d4 g8f6 b1c3 g7g6 f2f4 f8g7 g1f3"},
	{"B10", "Caro-Kann Defense", "e2e4 c7c6"},
	{"B12", "Caro-Kann Defense, Advance Variation", "e2e4 c7c6 d2d4 d7d5 e4e5"},
	{"B13", "Caro-Kann Defense, Exchange Variation", "e2e4 c7c6 d2d4 d7d5 e4d5 c6d5"},
	{"B14", "Caro-Kann Defense, Panov-Botvinnik Attack", "e2e4 c7c6 d2d4 d7d5 e4d5 c6d5 c2c4"},
	{"B15", "Caro-Kann Defense, Tartakower (Fantasy)", "e2e4 c7c6 d2d4 d7d5 f2f3"},
	{"B17", "Caro-Kann Defense, Steinitz Variation", "e2e4 c7c6 d2d4 d7d5 b1c3 d5e4 c3e4 b8d7"},
	{"B18", "Caro-Kann Defense, Classical Variation", "e2e4 c7c6 d2d4 d7d5 b1c3 d5e4 c3e4 c8f5"},
	{"B20", "Sicilian Defense", "e2e4 c7c5"},
	{"B21", "Sicilian Defense, Grand Prix Attack", "e2e4 c7c5 f2f4"},
	{"B21", "Sicilian Defense, Smith-Morra Gambit", "e2e4 c7c5 d2d4 c5d4 c2c3"},
	{"B22", "Sicilian Defense, Alapin Variation", "e2e4 c7c5 c2c3"},
	{"B23", "Sicilian Defense, Closed", "e2e4 c7c5 b1c3"},
	{"B27", "Sicilian Defense, Hyperaccelerated Dragon", "e2e4 c7c5 g1f3 g7g6"},
	{"B30", "Sicilian Defense, Old Sicilian", "e2e4 c7c5 g1f3 b8c6"},
	{"B31", "Sicilian Defense, Rossolimo Variation", "e2e4 c7c5 g1f3 b8c6 f1b5 g7g6"},
	{"B32", "Sicilian Defense, Open / O'Kelly", "e2e4 c7c5 g1f3 b8c6 d2d4 c5d4 f3d4"},
	{"B33", "Sicilian Defense, Sveshnikov (Pelikan)", "e2e4 c7c5 g1f3 b8c6 d2d4 c5d4 f3d4 g8f6 b1c3 e7e5"},
	{"B34", "Sicilian Defense, Accelerated Dragon", "e2e4 c7c5 g1f3 b8c6 d2d4 c5d4 f3d4 g7g6"},
	{"B40", "Sicilian Defense, French Variation", "e2e4 c7c5 g1f3 e7e6"},
	{"B41", "Sicilian Defense, Kan (Paulsen)", "e2e4 c7c5 g1f3 e7e6 d2d4 c5d4 f3d4 a7a6"},
	{"B44", "Sicilian Defense, Taimanov Variation", "e2e4 c7c5 g1f3 e7e6 d2d4 c5d4 f3d4 b8c6"},
	{"B50", "Sicilian Defense, Modern", "e2e4 c7c5 g1f3 d7d6"},
	{"B51", "Sicilian Defense, Moscow Variation", "e2e4 c7c5 g1f3 d7d6 f1b5"},
	{"B52", "Sicilian Defense, Canal Attack", "e2e4 c7c5 g1f3 d7d6 f1b5 c8d7"},
	{"B53", "Sicilian Defense, Chekhover Variation", "e2e4 c7c5 g1f3 d7d6 d2d4 c5d4 d1d4"},
	{"B54", "Sicilian Defense, Open", "e2e4 c7c5 g1f3 d7d6 d2d4 c5d4 f3d4"},
	{"B60", "Sicilian Defense, Richter-Rauzer", "e2e4 c7c5 g1f3 d7d6 d2d4 c5d4 f3d4 g8f6 b1c3 b8c6 c1g5"},
	{"B70", "Sicilian Defense, Dragon Variation", "e2e4 c7c5 g1f3 d7d6 d2d4 c5d4 f3d4 g8f6 b1c3 g7g6"},
	{"B72", "Sicilian Defense, Dragon, Classical", "e2e4 c7c5 g1f3 d7d6 d2d4 c5d4 f3d4 g8f6 b1c3 g7g6 f1e2"},
	{"B75", "Sicilian Defense, Dragon, Yugoslav Attack", "e2e4 c7c5 g1f3 d7d6 d2d4 c5d4 f3d4 g8f6 b1c3 g7g6 c1e3 f8g7 f2f3"},
	{"B80", "Sicilian Defense, Scheveningen", "e2e4 c7c5 g1f3 d7d6 d2d4 c5d4 f3d4 g8f6 b1c3 e7e6"},
	{"B80", "Sicilian Defense, Scheveningen, English Attack", "e2e4 c7c5 g1f3 d7d6 d2d4 c5d4 f3d4 g8f6 b1c3 e7e6 c1e3 a7a6 f2f3"},
	{"B90", "Sicilian Defense, Najdorf Variation", "e2e4 c7c5 g1f3 d7d6 d2d4 c5d4 f3d4 g8f6 b1c3 a7a6"},
	{"B90", "Sicilian Defense, Najdorf, English Attack", "e2e4 c7c5 g1f3 d7d6 d2d4 c5d4 f3d4 g8f6 b1c3 a7a6 c1e3 e7e5 d4b3 c8e6 f2f3"},
	{"B92", "Sicilian Defense, Najdorf, Classical", "e2e4 c7c5 g1f3 d7d6 d2d4 c5d4 f3d4 g8f6 b1c3 a7a6 f1e2"},
	{"B95", "Sicilian Defense, Najdorf, 6.Bg5", "e2e4 c7c5 g1f3 d7d6 d2d4 c5d4 f3d4 g8f6 b1c3 a7a6 c1g5"},
	{"B96", "Sicilian Defense, Najdorf, Polugaevsky", "e2e4 c7c5 g1f3 d7d6 d2d4 c5d4 f3d4 g8f6 b1c3 a7a6 c1g5 e7e6 f2f4 b7b5"},
	{"B97", "Sicilian Defense, Najdorf, Poisoned Pawn", "e2e4 c7c5 g1f3 d7d6 d2d4 c5d4 f3d4 g8f6 b1c3 a7a6 c1g5 e7e6 f2f4 d8b6"},

	// ==========================================
	// C: Open Games & French Defense
	// ==========================================
	{"C00", "French Defense", "e2e4 e7e6"},
	{"C01", "French Defense, Exchange Variation", "e2e4 e7e6 d2d4 d7d5 e4d5 e6d5"},
	{"C02", "French Defense, Advance Variation", "e2e4 e7e6 d2d4 d7d5 e4e5"},
	{"C03", "French Defense, Tarrasch Variation", "e2e4 e7e6 d2d4 d7d5 b1d2"},
	{"C05", "French Defense, Tarrasch, Closed", "e2e4 e7e6 d2d4 d7d5 b1d2 g8f6 e4e5 f6d7 f1d3 c7c5 c2c3 b8c6 g1e2"},
	{"C10", "French Defense, Rubinstein Variation", "e2e4 e7e6 d2d4 d7d5 b1c3 d5e4 c3e4"},
	{"C11", "French Defense, Classical Variation", "e2e4 e7e6 d2d4 d7d5 b1c3 g8f6"},
	{"C11", "French Defense, Steinitz Variation", "e2e4 e7e6 d2d4 d7d5 b1c3 g8f6 e4e5 f6d7 f2f4"},
	{"C15", "French Defense, Winawer (Nimzowitsch)", "e2e4 e7e6 d2d4 d7d5 b1c3 f8b4"},
	{"C16", "French Defense, Winawer, Advance", "e2e4 e7e6 d2d4 d7d5 b1c3 f8b4 e4e5 c7c5 a2a3 b4c3 b2c3"},
	{"C20", "King's Pawn Game, Open", "e2e4 e7e5"},
	{"C21", "Center Game", "e2e4 e7e5 d2d4 e5d4"},
	{"C21", "Danish Gambit", "e2e4 e7e5 d2d4 e5d4 c2c3"},
	{"C23", "Bishop's Opening", "e2e4 e7e5 f1c4"},
	{"C25", "Vienna Game", "e2e4 e7e5 b1c3"},
	{"C29", "Vienna Gambit", "e2e4 e7e5 b1c3 g8f6 f2f4"},
	{"C30", "King's Gambit", "e2e4 e7e5 f2f4"},
	{"C31", "King's Gambit Declined, Falkbeer Counter-Gambit", "e2e4 e7e5 f2f4 d7d5"},
	{"C33", "King's Gambit Accepted", "e2e4 e7e5 f2f4 e5f4"},
	{"C40", "King's Knight Opening", "e2e4 e7e5 g1f3"},
	{"C41", "Philidor Defense", "e2e4 e7e5 g1f3 d7d6"},
	{"C42", "Petrov's Defense (Russian Game)", "e2e4 e7e5 g1f3 g8f6"},
	{"C44", "Ponziani Opening", "e2e4 e7e5 g1f3 b8c6 c2c3"},
	{"C44", "Scotch Gambit", "e2e4 e7e5 g1f3 b8c6 d2d4 e5d4 f1c4"},
	{"C45", "Scotch Game", "e2e4 e7e5 g1f3 b8c6 d2d4 e5d4 f3d4"},
	{"C45", "Scotch Game, Mieses Variation", "e2e4 e7e5 g1f3 b8c6 d2d4 e5d4 f3d4 g8f6 d4c6 b7c6 e4e5 d8e7"},
	{"C46", "Three Knights Game", "e2e4 e7e5 g1f3 b8c6 b1c3"},
	{"C47", "Four Knights Game", "e2e4 e7e5 g1f3 b8c6 b1c3 g8f6"},
	{"C48", "Four Knights Game, Spanish Variation", "e2e4 e7e5 g1f3 b8c6 b1c3 g8f6 f1b5"},
	{"C49", "Four Knights Game, Double Spanish", "e2e4 e7e5 g1f3 b8c6 b1c3 g8f6 f1b5 f8b4"},
	{"C50", "Italian Game", "e2e4 e7e5 g1f3 b8c6 f1c4"},
	{"C50", "Hungarian Defense", "e2e4 e7e5 g1f3 b8c6 f1c4 f8e7"},
	{"C51", "Evans Gambit", "e2e4 e7e5 g1f3 b8c6 f1c4 f8c5 b2b4"},
	{"C53", "Giuoco Piano", "e2e4 e7e5 g1f3 b8c6 f1c4 f8c5 c2c3"},
	{"C54", "Giuoco Piano, Main Line", "e2e4 e7e5 g1f3 b8c6 f1c4 f8c5 c2c3 g8f6 d2d4 e5d4 c3d4 c5b4 c1d2 b4d2 b1d2 d7d5"},
	{"C55", "Two Knights Defense", "e2e4 e7e5 g1f3 b8c6 f1c4 g8f6"},
	{"C56", "Two Knights Defense, Modern", "e2e4 e7e5 g1f3 b8c6 f1c4 g8f6 d2d4 e5d4 e1g1"},
	{"C57", "Two Knights Defense, Fried Liver Attack", "e2e4 e7e5 g1f3 b8c6 f1c4 g8f6 f3g5 d7d5 e4d5 f6d5 g5f7"},
	{"C60", "Ruy Lopez (Spanish Opening)", "e2e4 e7e5 g1f3 b8c6 f1b5"},
	{"C62", "Ruy Lopez, Old Steinitz Defense", "e2e4 e7e5 g1f3 b8c6 f1b5 d7d6"},
	{"C63", "Ruy Lopez, Schliemann Defense", "e2e4 e7e5 g1f3 b8c6 f1b5 f7f5"},
	{"C65", "Ruy Lopez, Berlin Defense", "e2e4 e7e5 g1f3 b8c6 f1b5 g8f6"},
	{"C67", "Ruy Lopez, Berlin Defense, Open", "e2e4 e7e5 g1f3 b8c6 f1b5 g8f6 e1g1 f6e4 d2d4 e4d6 b5c6 d7c6 d4e5 d6f5 d1d8 e8d8"},
	{"C68", "Ruy Lopez, Exchange Variation", "e2e4 e7e5 g1f3 b8c6 f1b5 a7a6 b5c6"},
	{"C70", "Ruy Lopez, Morphy Defense", "e2e4 e7e5 g1f3 b8c6 f1b5 a7a6 f1a4"},
	{"C77", "Ruy Lopez, Morphy, Anderssen Variation", "e2e4 e7e5 g1f3 b8c6 f1b5 a7a6 f1a4 g8f6 d2d3"},
	{"C78", "Ruy Lopez, Archangelsk Defense", "e2e4 e7e5 g1f3 b8c6 f1b5 a7a6 f1a4 g8f6 e1g1 b7b5 a4b3 c8b7"},
	{"C80", "Ruy Lopez, Open Defense", "e2e4 e7e5 g1f3 b8c6 f1b5 a7a6 f1a4 g8f6 e1g1 f6e4"},
	{"C84", "Ruy Lopez, Closed", "e2e4 e7e5 g1f3 b8c6 f1b5 a7a6 f1a4 g8f6 e1g1 f8e7"},
	{"C88", "Ruy Lopez, Closed, Anti-Marshall", "e2e4 e7e5 g1f3 b8c6 f1b5 a7a6 f1a4 g8f6 e1g1 f8e7 f1e1 b7b5 a4b3 e1g1 a2a4"},
	{"C89", "Ruy Lopez, Marshall Attack", "e2e4 e7e5 g1f3 b8c6 f1b5 a7a6 f1a4 g8f6 e1g1 f8e7 f1e1 b7b5 a4b3 e1g1 c2c3 d7d5"},
	{"C90", "Ruy Lopez, Closed, Pilnik Variation", "e2e4 e7e5 g1f3 b8c6 f1b5 a7a6 f1a4 g8f6 e1g1 f8e7 f1e1 b7b5 a4b3 d7d6 c2c3 e1g1 d2d3"},
	{"C92", "Ruy Lopez, Closed, Zaitsev Variation", "e2e4 e7e5 g1f3 b8c6 f1b5 a7a6 f1a4 g8f6 e1g1 f8e7 f1e1 b7b5 a4b3 d7d6 c2c3 e1g1 h2h3 c8b7 d2d4 f8e8"},
	{"C95", "Ruy Lopez, Closed, Breyer Variation", "e2e4 e7e5 g1f3 b8c6 f1b5 a7a6 f1a4 g8f6 e1g1 f8e7 f1e1 b7b5 a4b3 d7d6 c2c3 e1g1 h2h3 b8b8 d2d4 b8d7"},
	{"C97", "Ruy Lopez, Closed, Chigorin Variation", "e2e4 e7e5 g1f3 b8c6 f1b5 a7a6 f1a4 g8f6 e1g1 f8e7 f1e1 b7b5 a4b3 d7d6 c2c3 e1g1 h2h3 c6a5 b3c2 c7c5 d2d4 d8c7"},

	// ==========================================
	// D: Closed Games & Semi-Closed Games
	// ==========================================
	{"D00", "Queen's Pawn Game, Mason Variation", "d2d4 d7d5"},
	{"D00", "Blackmar-Diemer Gambit", "d2d4 d7d5 e2e4 d5e4 b1c3 g8f6 f2f3"},
	{"D02", "Queen's Pawn Game, London System", "d2d4 d7d5 g1f3 g8f6 c1f4"},
	{"D04", "Colle System", "d2d4 d7d5 g1f3 g8f6 e2e3"},
	{"D05", "Colle System, Zukertort Variation", "d2d4 d7d5 g1f3 g8f6 e2e3 e7e6 f1d3 c7c5 b2b3"},
	{"D06", "Queen's Gambit", "d2d4 d7d5 c2c4"},
	{"D06", "Queen's Gambit, Marshall Defense", "d2d4 d7d5 c2c4 g8f6"},
	{"D07", "Chigorin Defense", "d2d4 d7d5 c2c4 b8c6"},
	{"D08", "Albin Counter-Gambit", "d2d4 d7d5 c2c4 e7e5"},
	{"D10", "Slav Defense", "d2d4 d7d5 c2c4 c7c6"},
	{"D11", "Slav Defense, Modern", "d2d4 d7d5 c2c4 c7c6 g1f3 g8f6 e2e3"},
	{"D13", "Slav Defense, Exchange Variation", "d2d4 d7d5 c2c4 c7c6 c4d5 c6d5"},
	{"D15", "Slav Defense, Three Knights", "d2d4 d7d5 c2c4 c7c6 g1f3 g8f6 b1c3"},
	{"D16", "Slav Defense, Soultanbeieff", "d2d4 d7d5 c2c4 c7c6 g1f3 g8f6 b1c3 d5c4 a2a4"},
	{"D20", "Queen's Gambit Accepted", "d2d4 d7d5 c2c4 d5c4"},
	{"D25", "Queen's Gambit Accepted, Classical", "d2d4 d7d5 c2c4 d5c4 g1f3 g8f6 e2e3 c8g4 f1c4 e7e6"},
	{"D30", "Queen's Gambit Declined", "d2d4 d7d5 c2c4 e7e6"},
	{"D31", "Semi-Slav Defense, Accelerated", "d2d4 d7d5 c2c4 e7e6 b1c3 c7c6"},
	{"D35", "Queen's Gambit Declined, Exchange", "d2d4 d7d5 c2c4 e7e6 b1c3 g8f6 c4d5 e6d5"},
	{"D37", "Queen's Gambit Declined, Classical", "d2d4 d7d5 c2c4 e7e6 b1c3 g8f6 g1f3 f8e7 c1f4"},
	{"D38", "Queen's Gambit Declined, Ragozin Defense", "d2d4 d7d5 c2c4 e7e6 b1c3 g8f6 g1f3 f8b4"},
	{"D40", "Queen's Gambit Declined, Semi-Tarrasch", "d2d4 d7d5 c2c4 e7e6 b1c3 g8f6 g1f3 c7c5"},
	{"D43", "Semi-Slav Defense", "d2d4 d7d5 c2c4 e7e6 b1c3 g8f6 g1f3 c7c6"},
	{"D45", "Semi-Slav Defense, Stoltz Variation", "d2d4 d7d5 c2c4 e7e6 b1c3 g8f6 g1f3 c7c6 e2e3 b8d7 d1c2"},
	{"D46", "Semi-Slav Defense, Chigorin", "d2d4 d7d5 c2c4 e7e6 b1c3 g8f6 g1f3 c7c6 e2e3 b8d7 f1d3 f8d6"},
	{"D47", "Semi-Slav Defense, Meran Variation", "d2d4 d7d5 c2c4 e7e6 b1c3 g8f6 g1f3 c7c6 e2e3 b8d7 f1d3 d5c4 d3c4 b7b5"},
	{"D50", "Queen's Gambit Declined, 4.Bg5", "d2d4 d7d5 c2c4 e7e6 b1c3 g8f6 c1g5"},
	{"D51", "Queen's Gambit Declined, Cambridge Springs", "d2d4 d7d5 c2c4 e7e6 b1c3 g8f6 c1g5 b8d7 e2e3 c7c6 g1f3 d8a5"},
	{"D55", "Queen's Gambit Declined, Tartakower Defense", "d2d4 d7d5 c2c4 e7e6 b1c3 g8f6 c1g5 f8e7 e2e3 h7h6 g5h4 e1g1 g1f3 b7b6"},
	{"D60", "Queen's Gambit Declined, Orthodox Defense", "d2d4 d7d5 c2c4 e7e6 b1c3 g8f6 c1g5 f8e7 e2e3 e1g1 g1f3 b8d7"},
	{"D70", "Neo-Grünfeld Defense", "d2d4 g8f6 c2c4 g7g6 g1f3 f8g7 g2g3 d7d5"},
	{"D80", "Grünfeld Defense", "d2d4 g8f6 c2c4 g7g6 b1c3 d7d5"},
	{"D85", "Grünfeld Defense, Exchange Variation", "d2d4 g8f6 c2c4 g7g6 b1c3 d7d5 c4d5 f6d5 e2e4 d5c3 b2c3 f8g7"},
	{"D90", "Grünfeld Defense, Three Knights", "d2d4 g8f6 c2c4 g7g6 b1c3 d7d5 g1f3 f8g7"},
	{"D96", "Grünfeld Defense, Russian Variation", "d2d4 g8f6 c2c4 g7g6 b1c3 d7d5 g1f3 f8g7 d1b3"},

	// ==========================================
	// E: Indian Defenses
	// ==========================================
	{"E00", "Catalan Opening", "d2d4 g8f6 c2c4 e7e6 g2g3"},
	{"E01", "Catalan Opening, Closed", "d2d4 g8f6 c2c4 e7e6 g2g3 d7d5 f1g2 f8e7 g1f3 e1g1"},
	{"E04", "Catalan Opening, Open", "d2d4 g8f6 c2c4 e7e6 g2g3 d7d5 f1g2 d5c4 g1f3"},
	{"E11", "Bogo-Indian Defense", "d2d4 g8f6 c2c4 e7e6 g1f3 f8b4"},
	{"E12", "Queen's Indian Defense", "d2d4 g8f6 c2c4 e7e6 g1f3 b7b6"},
	{"E15", "Queen's Indian Defense, Fianchetto", "d2d4 g8f6 c2c4 e7e6 g1f3 b7b6 g2g3 c8a6"},
	{"E20", "Nimzo-Indian Defense", "d2d4 g8f6 c2c4 e7e6 b1c3 f8b4"},
	{"E24", "Nimzo-Indian Defense, Sämisch Variation", "d2d4 g8f6 c2c4 e7e6 b1c3 f8b4 a2a3 b4c3 b2c3"},
	{"E32", "Nimzo-Indian Defense, Classical (Capablanca)", "d2d4 g8f6 c2c4 e7e6 b1c3 f8b4 d1c2"},
	{"E40", "Nimzo-Indian Defense, Rubinstein Variation", "d2d4 g8f6 c2c4 e7e6 b1c3 f8b4 e2e3"},
	{"E43", "Nimzo-Indian Defense, Fischer Variation", "d2d4 g8f6 c2c4 e7e6 b1c3 f8b4 e2e3 b7b6"},
	{"E60", "King's Indian Defense", "d2d4 g8f6 c2c4 g7g6"},
	{"E61", "King's Indian Defense, 3.Nc3", "d2d4 g8f6 c2c4 g7g6 b1c3 f8g7"},
	{"E62", "King's Indian Defense, Fianchetto Variation", "d2d4 g8f6 c2c4 g7g6 g1f3 f8g7 g2g3 e1g1 f1g2 d7d6 b1c3"},
	{"E70", "King's Indian Defense, Normal System", "d2d4 g8f6 c2c4 g7g6 b1c3 f8g7 e2e4 d7d6"},
	{"E76", "King's Indian Defense, Four Pawns Attack", "d2d4 g8f6 c2c4 g7g6 b1c3 f8g7 e2e4 d7d6 f2f4"},
	{"E80", "King's Indian Defense, Sämisch Variation", "d2d4 g8f6 c2c4 g7g6 b1c3 f8g7 e2e4 d7d6 f2f3"},
	{"E90", "King's Indian Defense, 5.Nf3", "d2d4 g8f6 c2c4 g7g6 b1c3 f8g7 e2e4 d7d6 g1f3 e1g1"},
	{"E92", "King's Indian Defense, Classical Variation", "d2d4 g8f6 c2c4 g7g6 b1c3 f8g7 e2e4 d7d6 g1f3 e1g1 f1e2 e7e5"},
	{"E97", "King's Indian Defense, Mar del Plata Variation", "d2d4 g8f6 c2c4 g7g6 b1c3 f8g7 e2e4 d7d6 g1f3 e1g1 f1e2 e7e5 e1g1 b8c6 d4d5 c6e7"},
}

var (
	ecoOnce sync.Once
	ecoMap  map[uint64]OpeningInfo
)

func initECO() {
	ecoMap = make(map[uint64]OpeningInfo, len(rawOpenings)*2)

	for _, ro := range rawOpenings {
		b, err := board.ParseFEN(board.StartFEN)
		if err != nil {
			continue
		}

		moveStrs := strings.Fields(ro.moves)
		valid := true
		for _, mStr := range moveStrs {
			legals := movegen.GenerateLegalMoves(b)
			var foundMove types.Move
			for _, m := range legals {
				if m.String() == mStr {
					foundMove = m
					break
				}
			}
			if foundMove == types.MoveNone {
				valid = false
				break
			}
			b.MakeMove(foundMove)
		}

		if valid {
			key := Hash(b)
			ecoMap[key] = OpeningInfo{ECO: ro.eco, Name: ro.name}
		}
	}
}

// FindOpening looks up the opening name for the given board.
// If the current position is not an exact match in the ECO database,
// it walks backward through the board's move history to find the most
// specific recognized opening reached in the game.
func FindOpening(b *board.Board) OpeningInfo {
	if b == nil {
		return OpeningInfo{}
	}

	ecoOnce.Do(initECO)

	// 1. Direct check on current position
	key := Hash(b)
	if op, ok := ecoMap[key]; ok {
		return op
	}

	// 2. If position has moves in UndoStack, clone and walk backwards
	if len(b.UndoStack) > 0 {
		clone := b.Clone()
		for len(clone.UndoStack) > 0 {
			lastMove := clone.UndoStack[len(clone.UndoStack)-1].Move
			clone.UnmakeMove(lastMove)
			k := Hash(clone)
			if op, ok := ecoMap[k]; ok {
				return op
			}
		}
	}

	return OpeningInfo{}
}
