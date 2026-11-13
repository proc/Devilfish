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

// Stockfish networks (file version 0x6A448AFA: threat, pawn-pair and
// king-relative piece-square inputs -> 1024 -> 32 -> 32 -> 1, eight layer
// stacks and eight PSQT buckets chosen by piece count).
//
// The feature transformer has the same shape as the 8Chess one: int16
// piece-square rows and int8 threat rows summed into a 1024-wide int16
// accumulator per perspective, then a clipped pairwise product. So such a
// network is held in an EightNetwork and evaluated incrementally by
// EightState; this file adds what differs: the file format, the feature
// numbering, the pawn-pair features, the PSQT sums, the dense layers and the
// conversion of the result to centipawns.

const (
	SFVersion = 0x6A448AFA

	sfThreatFeatures = 59808
	sfPawnIDs        = 96 // 48 squares x 2 colours
	sfPairFeatures   = sfPawnIDs * (sfPawnIDs - 1) / 2
	sfThreatRows     = sfThreatFeatures + sfPairFeatures // rows of the int8 table
	sfKingBuckets    = 32
	sfPieceSquares   = 11 * 64 // per king bucket
	sfPieceFeatures  = sfKingBuckets * sfPieceSquares
	sfPSQTBuckets    = 8
	sfLayerStacks    = 8
	sfL2             = 32
	sfL3             = 32

	sfLebMagic = "COMPRESSED_LEB128"
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

	fc1Bias [sfL3]int32
	fc1     [2 * sfL2][sfL3]int8 // [input][output]

	fc2Bias int32
	fc2     [2*sfL2 + 2*sfL3]int8
}

type sfNet struct {
	piecePSQT  []int32 // [sfPieceFeatures * sfPSQTBuckets]
	threatPSQT []int32 // [sfThreatRows * sfPSQTBuckets]
	stacks     [sfLayerStacks]sfStack
	desc       string
}

// sfReader reads the little-endian and LEB128 sections of a network file.
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

// expect reads a section hash.
func (r *sfReader) expect(want uint32, what string) {
	if got := r.u32(); r.err == nil && got != want {
		r.fail("%s: hash %#08x, expected %#08x (a different network architecture)", what, got, want)
	}
}

// leb reads count signed LEB128 integers of the given bit width into put.
func (r *sfReader) leb(count int, put func(i int, v int32)) {
	magic := r.take(len(sfLebMagic))
	if r.err == nil && string(magic) != sfLebMagic {
		r.fail("missing %s marker at byte %d", sfLebMagic, r.pos-len(sfLebMagic))
	}
	size := int(r.u32())
	buf := r.take(size)
	if r.err != nil {
		return
	}
	p := 0
	for i := 0; i < count; i++ {
		var result uint32
		shift := uint(0)
		for {
			if p >= len(buf) {
				r.fail("compressed section ends after %d of %d values", i, count)
				return
			}
			c := buf[p]
			p++
			result |= uint32(c&0x7f) << (shift % 32)
			shift += 7
			if c&0x80 == 0 {
				if shift < 32 && c&0x40 != 0 {
					result |= ^uint32(0) << shift
				}
				break
			}
		}
		put(i, int32(result))
	}
	if p != len(buf) {
		r.fail("compressed section has %d unread bytes", len(buf)-p)
	}
}

// Section hashes, computed as Stockfish computes them.
func sfFeatureHash() uint32 {
	var h uint32
	for _, c := range []uint32{0x2e6b9d04, 0x86f2b1dd, 0x7f234cb8} { // threats, pawn pairs, piece-squares
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
	r.expect(sfFeatureHash()^sfStackHash(), "network")
	net := &EightNetwork{sf: &sfNet{}}
	sf := net.sf
	sf.desc = string(r.take(int(r.u32())))

	// Feature transformer.
	r.expect(sfFeatureHash(), "feature transformer")
	r.leb(EightL1Size, func(i int, v int32) { net.FTBiases[i] = int16(v) })
	if r.err != nil {
		return nil, r.err
	}
	// The two tables are read row by row at random during search, so they go
	// on huge pages where the OS allows, as the 8Chess tables do.
	net.FTThreatWeights, _ = hugemem.Make[int8](sfThreatRows * EightL1Size)
	sf.threatPSQT = make([]int32, sfThreatRows*sfPSQTBuckets)
	raw := r.take(sfThreatFeatures * EightL1Size)
	if r.err != nil {
		return nil, r.err
	}
	copy(net.FTThreatWeights, unsafe.Slice((*int8)(unsafe.Pointer(&raw[0])), len(raw)))
	r.leb(sfThreatFeatures*sfPSQTBuckets, func(i int, v int32) { sf.threatPSQT[i] = v })
	raw = r.take(sfPairFeatures * EightL1Size)
	if r.err != nil {
		return nil, r.err
	}
	copy(net.FTThreatWeights[sfThreatFeatures*EightL1Size:], unsafe.Slice((*int8)(unsafe.Pointer(&raw[0])), len(raw)))
	r.leb(sfPairFeatures*sfPSQTBuckets, func(i int, v int32) { sf.threatPSQT[sfThreatFeatures*sfPSQTBuckets+i] = v })

	net.FTPieceWeights, _ = hugemem.Make[int16](sfPieceFeatures * EightL1Size)
	r.leb(sfPieceFeatures*EightL1Size, func(i int, v int32) { net.FTPieceWeights[i] = int16(v) })
	sf.piecePSQT = make([]int32, sfPieceFeatures*sfPSQTBuckets)
	r.leb(sfPieceFeatures*sfPSQTBuckets, func(i int, v int32) { sf.piecePSQT[i] = v })

	// Layer stacks: biases (int32), then weights (int8) by output.
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

// sfView numbers perspective pov's features for a position with that side's
// king on ksq. Piece-square features mirror the board when the king is on
// files a-d; threat and pawn-pair features when it is on files e-h.
func sfView(pov int, ksq int) eightView {
	v := eightView{sf: true, flip: 56 * pov, mirror: ksq&7 >= 4}
	if !v.mirror {
		v.flip ^= 7
	}
	v.bucket = sfKingBucket[ksq^(56*pov)]
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

// refApplyPSQT is the definition of sfApplyPSQT.
func refApplyPSQT(dst *[sfPSQTBuckets]int32, table []int32, add, rem *rowList) {
	if add.n+rem.n == 0 {
		return
	}
	// The rows come from feature numbering that is always within the table.
	base := unsafe.Pointer(&table[0])
	d := *dst
	for i := 0; i < add.n; i++ {
		row := (*[sfPSQTBuckets]int32)(unsafe.Add(base, int(add.rows[i])*(sfPSQTBuckets*4)))
		d[0] += row[0]
		d[1] += row[1]
		d[2] += row[2]
		d[3] += row[3]
		d[4] += row[4]
		d[5] += row[5]
		d[6] += row[6]
		d[7] += row[7]
	}
	for i := 0; i < rem.n; i++ {
		row := (*[sfPSQTBuckets]int32)(unsafe.Add(base, int(rem.rows[i])*(sfPSQTBuckets*4)))
		d[0] -= row[0]
		d[1] -= row[1]
		d[2] -= row[2]
		d[3] -= row[3]
		d[4] -= row[4]
		d[5] -= row[5]
		d[6] -= row[6]
		d[7] -= row[7]
	}
	*dst = d
}

// refSFActivate is the definition of sfActivate.
func refSFActivate(x, bias *[32]int32, out *[64]uint8, bits uint) {
	for i := range x {
		v := x[i] + bias[i]
		out[i] = uint8(min(127, (int64(v)*int64(v))>>(2*bits+7)))
		out[32+i] = uint8(min(max(v>>bits, 0), 127))
	}
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

// SFOutput is a Stockfish network's two outputs for the side to move, in
// Stockfish's internal units.
type SFOutput struct {
	PSQT       int32
	Positional int32
}

// sfForward runs the network on finished accumulators. psqP and psqT are the
// PSQT sums of the piece-square and of the threat/pawn-pair features.
func sfForward(net *EightNetwork, scratch *sfScratch, accPst, accThr *[2][EightL1Size]int16, psqP, psqT *[2][sfPSQTBuckets]int32, b *board.Board, occCount int) SFOutput {
	stm := int(b.SideToMove)
	bucket := (occCount - 1) / 4
	if bucket < 0 {
		bucket = 0
	} else if bucket >= sfLayerStacks {
		bucket = sfLayerStacks - 1
	}
	st := &net.sf.stacks[bucket]

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
	var nz [2*sfL2 + 8]uint16
	n := nonzero64((*[64]uint8)(act[:64]), &nz)

	var fc1 [sfL3]int32
	sparse32(&nz[0], n, &act[0], &st.fc1[0][0], &fc1)
	sfActivate(&fc1, &st.fc1Bias, (*[64]uint8)(act[64:]), 6)

	out := st.fc2Bias + sfDot128(&act, &st.fc2)
	// The skip connection takes the first layer's outputs with their bias.
	fc0[sfL2-2] += st.fc0Bias[sfL2-2]
	fc0[sfL2-1] += st.fc0Bias[sfL2-1]
	// Skip connection from the first layer's last two outputs.
	out += fc0[sfL2-2] - fc0[sfL2-1]

	// 1.0 is 128 * 64 * 2 here, and 600 * 16 on the way out.
	positional := int32(int64(out) * (600 * 16) / (128 * 64 * 2))
	psqt := (psqP[stm][bucket] + psqT[stm][bucket] - psqP[stm^1][bucket] - psqT[stm^1][bucket]) / 2
	return SFOutput{PSQT: psqt / 16, Positional: positional / 16}
}

// sfToCP turns the network outputs into centipawns for the side to move:
// Stockfish's blend of the two outputs and its material scaling (with no
// optimism term), then its conversion from internal units, under which 100
// is the advantage that wins half the time.
func sfToCP(o SFOutput, b *board.Board) int32 {
	count := func(t types.PieceType) int64 {
		return int64(bitboard.Count(b.PieceBB[types.MakePiece(types.White, t)] | b.PieceBB[types.MakePiece(types.Black, t)]))
	}
	p, n, bi, r, q := count(types.Pawn), count(types.Knight), count(types.Bishop), count(types.Rook), count(types.Queen)

	nnue := int64(o.PSQT) + int64(o.Positional)
	complexity := int64(o.PSQT) - int64(o.Positional)
	if complexity < 0 {
		complexity = -complexity
	}
	nnue -= nnue * complexity / 18236
	material := 534*p + 781*n + 825*bi + 1276*r + 2538*q
	v := nnue + nnue*material/91000

	m := float64(min(max(p+3*n+3*bi+5*r+9*q, 17), 78)) / 58.0
	a := ((-142.72052667*m+372.35176398)*m-340.71073572)*m + 415.23490212
	return int32(math.Round(100 * float64(v) / a))
}

// sfStates holds evaluator states for one-off evaluations.
var sfStates = sync.Pool{New: func() any { return NewEightState(1) }}

// SFEvaluateRaw returns the network's outputs for b, as Stockfish's "eval"
// command reports them (their sum is its "NNUE evaluation ... internal
// units"). ok is false if b has no king of either colour.
func SFEvaluateRaw(net *EightNetwork, b *board.Board) (out SFOutput, ok bool) {
	if net == nil || net.sf == nil {
		return out, false
	}
	s := sfStates.Get().(*EightState)
	defer sfStates.Put(s)
	s.Reset()
	if !s.Prepare(net, b, 0, 0, false) {
		return out, false
	}
	cur := &s.plies[0]
	return sfForward(net, &s.sfScratch, &cur.pst, &cur.thr, &cur.psqP, &cur.psqT, b, bitboard.Count(b.Occupied())), true
}

// sfEvaluate evaluates b from nothing, in centipawns for the side to move.
func sfEvaluate(net *EightNetwork, b *board.Board) int32 {
	out, ok := SFEvaluateRaw(net, b)
	if !ok {
		return 0
	}
	return sfToCP(out, b)
}
