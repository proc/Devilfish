package nnue

import (
	"encoding/binary"
	"fmt"
	"math"
	"math/bits"
	"os"
	"sync"
	"unsafe"

	"devilfish/internal/bitboard"
	"devilfish/internal/board"
	"devilfish/internal/hugemem"
	"devilfish/internal/types"
)

// Stockfish networks (file version 0x6A448AFA, architecture SFNNv20: threat,
// pawn-pair and king-relative piece-square inputs -> 1024 -> 32 -> 32 -> 1,
// with up to 192 layer stacks chosen by material).
//
// The feature transformer has the same shape as the 8Chess one: piece-square
// rows (int8 here, int16 there) and int8 threat rows summed into a 1024-wide int16 accumulator per
// perspective, then a clipped pairwise product. So such a network is held in
// an EightNetwork and evaluated incrementally by EightState; this file adds
// what differs: the file format, the feature numbering, the pawn-pair
// features, the dense layers and the conversion of the result to centipawns.

const (
	SFVersion = 0x6A448AFA

	sfThreatFeatures = 59808
	sfPawnIDs        = 96 // 48 squares x 2 colours
	sfPairFeatures   = sfPawnIDs * (sfPawnIDs - 1) / 2
	sfThreatRows     = sfThreatFeatures + sfPairFeatures // rows of the int8 table
	sfKingSquares    = 32                                // king buckets: the squares of half a board
	sfPieceSquares   = 11 * 64                           // per king bucket
	sfBlocks         = 3                                 // piece-square blocks, by heavy material (see sfBlock)
	sfKingBuckets    = sfBlocks * sfKingSquares          // block and king bucket together
	sfPieceFeatures  = sfKingBuckets * sfPieceSquares
	sfRawStacks      = 192 // layer stacks before those sharing weights are merged
	sfL2             = 32
	sfL3             = 32

	sfStackTableHash = 0x5A7AB1E5
)

// sfStack is one layer stack. Weights are stored by input (the file stores
// them by output), so that a zero input costs nothing.
type sfStack struct {
	fc0Bias [sfL2]int32
	// The first layer as two 16-output layers in both layouts eightL1Layer
	// takes: outputs 0-15 and 16-31.
	fc0ByInput [2][EightL1Size][EightL2Size]int8
	fc0File    [2][EightL2Size * EightL1Size]int8
	fc0        [EightL1Size][sfL2]int8 // the same weights, [input][output]
	// And by group of four inputs, [group][output][input in group]: the
	// layout a dot-product instruction takes (see sfFirstLayer).
	fc0Chunk [EightL1Size / 4][sfL2][4]int8

	fc1Bias  [sfL3]int32
	fc1      [2 * sfL2][sfL3]int8        // [input][output]
	fc1Chunk [2 * sfL2 / 4][sfL3][4]int8 // [group of four inputs][output][input in group]

	fc2Bias int32
	fc2     [2*sfL2 + 2*sfL3]int8
}

type sfNet struct {
	stacks     []sfStack           // the layer stacks stored in the file
	stackIndex [sfRawStacks]uint16 // raw layer stack (see sfLayerStack) -> stacks
	desc       string
}

// sfReader reads the little-endian sections of a network file.
type sfReader struct {
	data []byte
	pos  int
	err  error
}

func (r *sfReader) fail(format string, args ...any) {
	if r.err == nil {
		r.err = fmt.Errorf(format, args...)
	}
}

func (r *sfReader) take(n int) []byte {
	if r.err != nil {
		return nil
	}
	if n < 0 || r.pos+n > len(r.data) {
		r.fail("unexpected end of file at byte %d (need %d more)", r.pos, n)
		return nil
	}
	b := r.data[r.pos : r.pos+n]
	r.pos += n
	return b
}

func (r *sfReader) u32() uint32 {
	b := r.take(4)
	if b == nil {
		return 0
	}
	return binary.LittleEndian.Uint32(b)
}

func (r *sfReader) u16() uint16 {
	b := r.take(2)
	if b == nil {
		return 0
	}
	return binary.LittleEndian.Uint16(b)
}

// expect reads a section hash.
func (r *sfReader) expect(want uint32, what string) {
	if got := r.u32(); r.err == nil && got != want {
		r.fail("%s: hash %#08x, expected %#08x (a different network architecture)", what, got, want)
	}
}

// Section hashes, computed as Stockfish computes them.
func sfFeatureHash() uint32 {
	var h uint32
	for _, c := range []uint32{0x2c41eaea, 0x5c80fa69, 0xab005cef} { // threats, pawn pairs, piece-squares
		h = h<<1 | h>>31
		h ^= c
	}
	return h ^ (EightL1Size * 2)
}

func sfStackHash() uint32 {
	affine := func(prev, outputs uint32) uint32 {
		return (0xCC03DAE4 + outputs) ^ (prev >> 1) ^ (prev << 31)
	}
	relu := func(prev uint32) uint32 { return 0x538D24C7 + prev }
	h := uint32(0xEC42E90D) ^ (EightL1Size * 2)
	h = affine(h, sfL2)
	h = relu(h)
	h = affine(h, sfL3)
	h = relu(h)
	return affine(h, 1)
}

// StockfishName is the EvalFile value that selects the Stockfish network
// built into the binary.
const StockfishName = "<stockfish>"

// EmbeddedSFName is the file name of that network.
const EmbeddedSFName = "nn-68752b755c93.nnue"

var (
	embeddedSFOnce sync.Once
	embeddedSFNet  *EightNetwork
	embeddedSFErr  error
)

// EmbeddedSF returns the Stockfish network built into the binary. It is
// parsed on first use (about a second) and kept.
func EmbeddedSF() (*EightNetwork, error) {
	embeddedSFOnce.Do(func() {
		embeddedSFNet, embeddedSFErr = LoadSFBytes(embeddedSFNNUE)
	})
	return embeddedSFNet, embeddedSFErr
}

// LoadSFFile loads a Stockfish network file.
func LoadSFFile(path string) (*EightNetwork, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return LoadSFBytes(data)
}

// LoadSFBytes parses a Stockfish network.
func LoadSFBytes(data []byte) (*EightNetwork, error) {
	initEightTables()
	if sfThreats.features != sfThreatFeatures {
		return nil, fmt.Errorf("internal error: threat tables give %d features, expected %d", sfThreats.features, sfThreatFeatures)
	}
	r := &sfReader{data: data}
	if v := r.u32(); r.err == nil && v != SFVersion {
		return nil, fmt.Errorf("unsupported Stockfish network version %#08x (supported: %#08x)", v, uint32(SFVersion))
	}
	r.expect(sfFeatureHash()^sfStackHash()^sfStackTableHash, "network")
	net := &EightNetwork{sf: &sfNet{}}
	sf := net.sf
	sf.desc = string(r.take(int(r.u32())))

	// Layer stack table: for each raw stack, the one whose weights it uses.
	// Only stacks that use their own are stored, in order.
	r.expect(sfStackTableHash, "layer stack table")
	if n := r.u32(); r.err == nil && n != sfRawStacks {
		return nil, fmt.Errorf("layer stack table has %d entries, expected %d", n, sfRawStacks)
	}
	stored := int(r.u32())
	var fallback [sfRawStacks]uint16
	for i := range fallback {
		fallback[i] = r.u16()
	}
	if r.err != nil {
		return nil, r.err
	}
	if stored < 1 || stored > sfRawStacks {
		return nil, fmt.Errorf("%d layer stacks stored, expected 1 to %d", stored, sfRawStacks)
	}
	own := 0
	for i, f := range fallback {
		if int(f) >= sfRawStacks || fallback[f] != f {
			return nil, fmt.Errorf("layer stack table: entry %d refers to stack %d, which is not stored", i, f)
		}
		if int(f) == i {
			sf.stackIndex[i] = uint16(own)
			own++
		}
	}
	if own != stored {
		return nil, fmt.Errorf("layer stack table lists %d stored stacks, header says %d", own, stored)
	}
	for i, f := range fallback {
		sf.stackIndex[i] = sf.stackIndex[f]
	}

	// Feature transformer: biases, threat rows, pawn-pair rows, piece-square
	// rows, all uncompressed.
	r.expect(sfFeatureHash(), "feature transformer")
	raw := r.take(EightL1Size * 2)
	if r.err != nil {
		return nil, r.err
	}
	for i := range net.FTBiases {
		net.FTBiases[i] = int16(binary.LittleEndian.Uint16(raw[2*i:]))
	}
	// The two tables are read row by row at random during search, so they go
	// on huge pages where the OS allows, as the 8Chess tables do.
	raw = r.take(sfThreatRows * EightL1Size)
	if r.err != nil {
		return nil, r.err
	}
	net.FTThreatWeights, _ = hugemem.Make[int8](sfThreatRows * EightL1Size)
	copy(net.FTThreatWeights, unsafe.Slice((*int8)(unsafe.Pointer(&raw[0])), len(raw)))

	// Piece-square rows are int8 too, and are kept so: the accumulator
	// kernel for threat rows applies them.
	raw = r.take(sfPieceFeatures * EightL1Size)
	if r.err != nil {
		return nil, r.err
	}
	net.FTPieceWeights8, _ = hugemem.Make[int8](sfPieceFeatures * EightL1Size)
	copy(net.FTPieceWeights8, unsafe.Slice((*int8)(unsafe.Pointer(&raw[0])), len(raw)))

	// Layer stacks: biases (int32), then weights (int8) by output.
	sf.stacks = make([]sfStack, stored)
	for k := range sf.stacks {
		st := &sf.stacks[k]
		r.expect(sfStackHash(), "layer stack")
		for o := range st.fc0Bias {
			st.fc0Bias[o] = int32(r.u32())
		}
		w := r.take(sfL2 * EightL1Size)
		if r.err != nil {
			return nil, r.err
		}
		for o := 0; o < sfL2; o++ {
			half, j := o/EightL2Size, o%EightL2Size
			for i := 0; i < EightL1Size; i++ {
				v := int8(w[o*EightL1Size+i])
				st.fc0[i][o] = v
				st.fc0Chunk[i/4][o][i%4] = v
				st.fc0ByInput[half][i][j] = v
				st.fc0File[half][(i/4)*EightL2Size*4+j*4+i%4] = v
			}
		}
		for o := range st.fc1Bias {
			st.fc1Bias[o] = int32(r.u32())
		}
		w = r.take(sfL3 * 2 * sfL2)
		if r.err != nil {
			return nil, r.err
		}
		for o := 0; o < sfL3; o++ {
			for i := 0; i < 2*sfL2; i++ {
				st.fc1[i][o] = int8(w[o*2*sfL2+i])
				st.fc1Chunk[i/4][o][i%4] = st.fc1[i][o]
			}
		}
		st.fc2Bias = int32(r.u32())
		w = r.take(len(st.fc2))
		if r.err != nil {
			return nil, r.err
		}
		for i := range st.fc2 {
			st.fc2[i] = int8(w[i])
		}
	}
	if r.err != nil {
		return nil, r.err
	}
	if r.pos != len(data) {
		return nil, fmt.Errorf("%d unread bytes after the last layer stack", len(data)-r.pos)
	}
	return net, nil
}

// IsSF reports whether net is a Stockfish network.
func (net *EightNetwork) IsSF() bool { return net != nil && net.sf != nil }

// Description returns the text stored in a Stockfish network file.
func (net *EightNetwork) Description() string {
	if net == nil || net.sf == nil {
		return ""
	}
	return net.sf.desc
}

// sfPawnBand[s] is where another pawn forms a pair with a pawn on s: the
// same and the adjacent files, ranks 2 to 7, s itself excluded.
var sfPawnBand = func() (t [64]bitboard.Bitboard) {
	for s := 8; s < 56; s++ {
		for o := 8; o < 56; o++ {
			if d := o%8 - s%8; o != s && d >= -1 && d <= 1 {
				t[s] |= bitboard.Bitboard(1) << uint(o)
			}
		}
	}
	return
}()

// sfKingBucket[sq] is the king bucket for a king on sq, seen from its own
// side (rank 1 nearest): one bucket per square of the mirrored half board.
var sfKingBucket = func() (t [64]int) {
	for sq := 0; sq < 64; sq++ {
		file := sq % 8
		if file > 3 {
			file = 7 - file
		}
		t[sq] = (7-sq/8)*4 + file
	}
	return
}()

// sfBlock is the piece-square block perspective pov uses in b, in order of
// increasing heavy material: 0 with no rook or queen on the board, 1 with
// some but no enemy queen, 2 when the opponent has a queen.
func sfBlock(b *board.Board, pov int) int {
	if b.PieceBB[types.MakePiece(types.Color(pov^1), types.Queen)] != 0 {
		return 2
	}
	if b.PieceBB[types.WhiteRook]|b.PieceBB[types.BlackRook]|b.PieceBB[types.WhiteQueen]|b.PieceBB[types.BlackQueen] != 0 {
		return 1
	}
	return 0
}

// sfView numbers perspective pov's features for a position with that side's
// king on ksq and piece-square block `block`. Every feature mirrors the board
// when the king is on files a-d, so that it is always seen on files e-h.
func sfView(pov int, ksq int, block int) eightView {
	v := eightView{sf: true, flip: 56 * pov, mirror: ksq&7 < 4}
	if v.mirror {
		v.flip ^= 7
	}
	v.bucket = block*sfKingSquares + sfKingBucket[ksq^(56*pov)]
	return v
}

// sfPieceFeature numbers piece p on sq: own and enemy pieces of each type get
// 64 squares each, and the two kings share one block.
func sfPieceFeature(v *eightView, pov int, p types.Piece, sq int) int32 {
	ptype := int(p.Type()) - 1
	block := 10
	if ptype < 5 {
		block = ptype*2 + (int(p.Color()) ^ pov)
	}
	return int32(v.bucket*sfPieceSquares + block*64 + (sq ^ v.flip))
}

// sfPawnPairRow is the row, in the threat table, of the pair formed by a
// pawn of colour ca on a and one of colour cb on b.
func sfPawnPairRow(ca, a, cb, b int, mirror bool, pov int) int32 {
	o := 56 * pov
	if mirror {
		o ^= 7
	}
	ia := 48*(ca^pov) + (a ^ o) - 8
	ib := 48*(cb^pov) + (b ^ o) - 8
	if ia < ib {
		ia, ib = ib, ia
	}
	return int32(sfThreatFeatures + ia*(ia-1)/2 + ib)
}

// pawnPairs lists, for the perspectives in want, the pawn-pair rows of x in
// which at least one pawn stands on d.
func pawnPairs(x *eightSnap, d bitboard.Bitboard, views *[2]eightView, want [2]bool, out *[2]rowList) {
	black := x.bb[types.BlackPawn]
	pawns := x.bb[types.WhitePawn] | black
	u := pawns & d
	rest := pawns &^ u
	for u != 0 {
		a := int(bitboard.PopLSB(&u))
		ca := int(uint64(black) >> uint(a) & 1)
		// Partners: pawns off d, and those on d not yet taken as `a`.
		for m := sfPawnBand[a] & (rest | u); m != 0; {
			b := int(bitboard.PopLSB(&m))
			cb := int(uint64(black) >> uint(b) & 1)
			for pov := 0; pov < 2; pov++ {
				if want[pov] {
					out[pov].push(sfPawnPairRow(ca, a, cb, b, views[pov].mirror, pov))
				}
			}
		}
	}
}

// refSFActivate is the definition of sfActivate.
func refSFActivate(x, bias *[32]int32, out *[64]uint8, bits uint) {
	for i := range x {
		v := x[i] + bias[i]
		out[i] = uint8(min(127, (int64(v)*int64(v))>>(2*bits+7)))
		out[32+i] = uint8(min(max(v>>bits, 0), 127))
	}
}

// refSFSecondLayer is the definition of sfSecondLayer.
func refSFSecondLayer(act *[128]uint8, st *sfStack, out *[32]int32) {
	var nz [2*sfL2 + 8]uint16
	n := nonzero64((*[64]uint8)(act[:64]), &nz)
	refSparse32(&nz[0], n, &act[0], &st.fc1[0][0], out)
}

// refDot128 is the definition of sfDot128.
func refDot128(a *[128]uint8, w *[128]int8) int32 {
	var sum int32
	for i, v := range a {
		sum += int32(v) * int32(w[i])
	}
	return sum
}

// nonzero64 lists the indices of the non-zero bytes of in, in order, and
// returns their number, without data-dependent branches (see l1NonzeroIdx).
func nonzero64(in *[64]uint8, out *[72]uint16) int {
	const (
		lo7    = 0x7f7f7f7f7f7f7f7f
		hi     = 0x8080808080808080
		gather = 0x0102040810204080
		lanes  = 0x0001000100010001
	)
	src := unsafe.Pointer(in)
	dst := unsafe.Pointer(out)
	n := 0
	for c := uintptr(0); c < 64; c += 8 {
		x := *(*uint64)(unsafe.Add(src, c))
		m := (((((x & lo7) + lo7) | x) & hi) >> 7) * gather >> 56
		row := (*[2]uint64)(unsafe.Pointer(&l1NzIdx[m]))
		d := (*[2]uint64)(unsafe.Add(dst, n*2))
		base := uint64(c) * lanes
		d[0] = row[0] + base
		d[1] = row[1] + base
		n += bits.OnesCount64(m)
	}
	return n
}

// sfScratch holds sfForward's large temporaries, so that they are not
// cleared on every call as local arrays would be.
type sfScratch struct {
	ft  [EightL1Size]uint8      // feature transformer output
	idx [EightL1Size + 8]uint16 // its non-zero inputs (or groups of four)
}

// sfLayerStack picks the layer stack for b from its piece count, which
// sides have a queen, where the pawns stand and how many officers (pieces
// other than pawns and kings) a side has.
func sfLayerStack(sf *sfNet, b *board.Board, occCount int) *sfStack {
	stm := b.SideToMove
	band := (occCount - 1) / 4
	if band < 0 {
		band = 0
	} else if band > 7 {
		band = 7
	}
	queens := 0
	if b.PieceBB[types.MakePiece(stm, types.Queen)] != 0 {
		queens = 2
	}
	if b.PieceBB[types.MakePiece(stm.Other(), types.Queen)] != 0 {
		queens++
	}
	// Pawns on both halves of the board (files a-d and e-h), on one, or none.
	const queenSide = bitboard.Bitboard(0x0f0f0f0f0f0f0f0f)
	pawns := b.PieceBB[types.WhitePawn] | b.PieceBB[types.BlackPawn]
	layout := 1
	if pawns == 0 {
		layout = 2
	} else if pawns&queenSide != 0 && pawns&^queenSide != 0 {
		layout = 0
	}
	officers := func(c types.Color) int {
		return bitboard.Count(b.PieceBB[types.MakePiece(c, types.Knight)] | b.PieceBB[types.MakePiece(c, types.Bishop)] |
			b.PieceBB[types.MakePiece(c, types.Rook)] | b.PieceBB[types.MakePiece(c, types.Queen)])
	}
	many := 0
	if max(officers(types.White), officers(types.Black)) >= 2 {
		many = 1
	}
	return &sf.stacks[sf.stackIndex[((band*4+queens)*3+layout)*2+many]]
}

// sfForward runs the network on finished accumulators and returns its output
// for the side to move, in Stockfish's internal units.
func sfForward(net *EightNetwork, scratch *sfScratch, accPst, accThr *[2][EightL1Size]int16, b *board.Board, occCount int) int32 {
	stm := int(b.SideToMove)
	st := sfLayerStack(net.sf, b, occCount)

	// Clipped pairwise product of each perspective's accumulator, the side
	// to move first.
	ftOut := &scratch.ft
	for flip := 0; flip < 2; flip++ {
		pov := stm ^ flip
		activateEightFTNEON(&ftOut[flip*(EightL1Size/2)], &accPst[pov][0], &accThr[pov][0])
	}

	// First layer: 32 outputs from the (mostly zero) 1024 inputs.
	var fc0 [sfL2]int32
	sfFirstLayer(ftOut, st, &fc0, &scratch.idx)

	// Its outputs (plus bias) feed the next layer twice, squared and
	// clipped, each 0..127: act[0:32] and act[32:64]. Many are zero.
	var act [2*sfL2 + 2*sfL3]uint8
	sfActivate(&fc0, &st.fc0Bias, (*[64]uint8)(act[:64]), 7)

	var fc1 [sfL3]int32
	sfSecondLayer(&act, st, &fc1)
	sfActivate(&fc1, &st.fc1Bias, (*[64]uint8)(act[64:]), 6)

	out := st.fc2Bias + sfDot128(&act, &st.fc2)
	// The skip connection takes the first layer's outputs with their bias.
	fc0[sfL2-2] += st.fc0Bias[sfL2-2]
	fc0[sfL2-1] += st.fc0Bias[sfL2-1]
	// Skip connection from the first layer's last two outputs.
	out += fc0[sfL2-2] - fc0[sfL2-1]

	// 1.0 is 128 * 64 * 2 here, and 600 * 16 on the way out.
	return int32(int64(out)*(600*16)/(128*64*2)) / 16
}

// sfMaterial holds what Stockfish's evaluation scaling reads from a position.
type sfMaterial struct {
	simple   int64   // material balance for the side to move (pawn 208)
	total    int64   // 521 per pawn plus the pieces' values, both sides
	unitsPer float64 // internal units to the pawn (the win-rate model's a)
}

func sfMaterialOf(b *board.Board) (m sfMaterial) {
	us, them := b.SideToMove, b.SideToMove.Other()
	value := [...]int64{types.Pawn: 208, types.Knight: 781, types.Bishop: 825, types.Rook: 1276, types.Queen: 2538}
	weight := [...]int64{types.Pawn: 1, types.Knight: 3, types.Bishop: 3, types.Rook: 5, types.Queen: 9}
	var count int64
	for t := types.Pawn; t <= types.Queen; t++ {
		mine := int64(bitboard.Count(b.PieceBB[types.MakePiece(us, t)]))
		theirs := int64(bitboard.Count(b.PieceBB[types.MakePiece(them, t)]))
		m.simple += value[t] * (mine - theirs)
		if t == types.Pawn {
			m.total += 521 * (mine + theirs)
		} else {
			m.total += value[t] * (mine + theirs)
		}
		count += weight[t] * (mine + theirs)
	}
	x := float64(min(max(count, 17), 78)) / 58.0
	m.unitsPer = ((-142.72052667*x+372.35176398)*x-340.71073572)*x + 415.23490212
	return m
}

// sfAlignment measures whether the network's output and the material balance
// agree: positive when they do (a straightforward position), negative when
// the evaluation rests on compensation. Within -2048..2048.
func sfAlignment(simple, nnue int64) int64 {
	se := simple * 1024 / (abs(simple) + 1024)
	nn := nnue * 1024 / (abs(nnue) + 1024)
	return se * nn / 512
}

// sfToCP turns the network output into centipawns for the side to move:
// Stockfish's scaling by alignment and by material (with no optimism term),
// then its conversion from internal units, under which 100 is the advantage
// that wins half the time.
func sfToCP(nnue int32, b *board.Board) int32 {
	m := sfMaterialOf(b)
	n := int64(nnue)
	base := n + n*sfAlignment(m.simple, n)/65536
	v := base * (90649 + m.total) / 90649
	return int32(math.Round(100 * float64(v) / m.unitsPer))
}

// SFOptimismCP is Stockfish's optimism term for a position whose evaluation
// (from sfToCP) is cp, in centipawns for the side to move. optimism is that
// side's optimism in internal units. The term is optimism scaled by the
// alignment of evaluation and material, so it favours straightforward
// positions for the side that is ahead and complicated ones for the other.
func SFOptimismCP(b *board.Board, cp int32, optimism int32) int32 {
	m := sfMaterialOf(b)
	// The network output that cp came from, near enough (the alignment
	// scaling changes it by at most 3%).
	nnue := int64(float64(cp) * m.unitsPer / 100 * 90649 / float64(90649+m.total))
	v := int64(optimism) * sfAlignment(m.simple, nnue) / 16384 * (90649 + m.total) / 90649
	return int32(math.Round(100 * float64(v) / m.unitsPer))
}

func abs(x int64) int64 {
	if x < 0 {
		return -x
	}
	return x
}

// sfStates holds evaluator states for one-off evaluations.
var sfStates = sync.Pool{New: func() any { return NewEightState(1) }}

// SFEvaluateRaw returns the network's output for b, as Stockfish's "eval"
// command reports it ("NNUE evaluation ... internal units"). ok is false if
// b has no king of either colour.
func SFEvaluateRaw(net *EightNetwork, b *board.Board) (out int32, ok bool) {
	if net == nil || net.sf == nil {
		return 0, false
	}
	s := sfStates.Get().(*EightState)
	defer sfStates.Put(s)
	s.Reset()
	if !s.Prepare(net, b, 0, 0, false) {
		return 0, false
	}
	cur := &s.plies[0]
	return sfForward(net, &s.sfScratch, &cur.pst, &cur.thr, b, bitboard.Count(b.Occupied())), true
}

// sfEvaluate evaluates b from nothing, in centipawns for the side to move.
func sfEvaluate(net *EightNetwork, b *board.Board) int32 {
	out, ok := SFEvaluateRaw(net, b)
	if !ok {
		return 0
	}
	return sfToCP(out, b)
}
