package board

var (
	ZobristPiece    [15][64]uint64
	ZobristCastling [16]uint64
	ZobristEP       [8]uint64
	ZobristSide     uint64
)

func init() {
	// Deterministic PRNG: SplitMix64
	seed := uint64(0x1844674407370955)
	rand64 := func() uint64 {
		seed += 0x9e3779b97f4a7c15
		z := seed
		z = (z ^ (z >> 30)) * 0xbf58476d1ce4e5b9
		z = (z ^ (z >> 27)) * 0x94d049bb133111eb
		return z ^ (z >> 31)
	}

	for p := 0; p < 15; p++ {
		for sq := 0; sq < 64; sq++ {
			ZobristPiece[p][sq] = rand64()
		}
	}
	for c := 0; c < 16; c++ {
		ZobristCastling[c] = rand64()
	}
	for f := 0; f < 8; f++ {
		ZobristEP[f] = rand64()
	}
	ZobristSide = rand64()
}
