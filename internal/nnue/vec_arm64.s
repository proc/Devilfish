//go:build arm64

#include "textflag.h"

// func addVectorNEON(acc *[256]int16, weights *[256]int16)
TEXT ·addVectorNEON(SB), NOSPLIT|NOFRAME, $0-16
	MOVD acc+0(FP), R0
	MOVD weights+8(FP), R1
	MOVD R0, R2

	MOVD $4, R3 // 4 iterations of 128 bytes (512 bytes / 256 int16)
loop_add:
	VLD1.P 64(R0), [V0.H8, V1.H8, V2.H8, V3.H8]
	VLD1.P 64(R0), [V4.H8, V5.H8, V6.H8, V7.H8]
	VLD1.P 64(R1), [V8.H8, V9.H8, V10.H8, V11.H8]
	VLD1.P 64(R1), [V12.H8, V13.H8, V14.H8, V15.H8]
	VADD   V8.H8, V0.H8, V0.H8
	VADD   V9.H8, V1.H8, V1.H8
	VADD   V10.H8, V2.H8, V2.H8
	VADD   V11.H8, V3.H8, V3.H8
	VADD   V12.H8, V4.H8, V4.H8
	VADD   V13.H8, V5.H8, V5.H8
	VADD   V14.H8, V6.H8, V6.H8
	VADD   V15.H8, V7.H8, V7.H8
	VST1.P [V0.H8, V1.H8, V2.H8, V3.H8], 64(R2)
	VST1.P [V4.H8, V5.H8, V6.H8, V7.H8], 64(R2)
	SUB    $1, R3
	CBNZ   R3, loop_add

	RET

// func subVectorNEON(acc *[256]int16, weights *[256]int16)
TEXT ·subVectorNEON(SB), NOSPLIT|NOFRAME, $0-16
	MOVD acc+0(FP), R0
	MOVD weights+8(FP), R1
	MOVD R0, R2

	MOVD $4, R3 // 4 iterations of 128 bytes (512 bytes / 256 int16)
loop_sub:
	VLD1.P 64(R0), [V0.H8, V1.H8, V2.H8, V3.H8]
	VLD1.P 64(R0), [V4.H8, V5.H8, V6.H8, V7.H8]
	VLD1.P 64(R1), [V8.H8, V9.H8, V10.H8, V11.H8]
	VLD1.P 64(R1), [V12.H8, V13.H8, V14.H8, V15.H8]
	VSUB   V8.H8, V0.H8, V0.H8
	VSUB   V9.H8, V1.H8, V1.H8
	VSUB   V10.H8, V2.H8, V2.H8
	VSUB   V11.H8, V3.H8, V3.H8
	VSUB   V12.H8, V4.H8, V4.H8
	VSUB   V13.H8, V5.H8, V5.H8
	VSUB   V14.H8, V6.H8, V6.H8
	VSUB   V15.H8, V7.H8, V7.H8
	VST1.P [V0.H8, V1.H8, V2.H8, V3.H8], 64(R2)
	VST1.P [V4.H8, V5.H8, V6.H8, V7.H8], 64(R2)
	SUB    $1, R3
	CBNZ   R3, loop_sub

	RET

// func addVector1024NEON(acc *[1024]int16, weights *int16)
TEXT ·addVector1024NEON(SB), NOSPLIT|NOFRAME, $0-16
	MOVD acc+0(FP), R0
	MOVD weights+8(FP), R1
	MOVD R0, R2

	MOVD $16, R3 // 16 iterations of 128 bytes (2048 bytes / 1024 int16)
loop_add1024:
	VLD1.P 64(R0), [V0.H8, V1.H8, V2.H8, V3.H8]
	VLD1.P 64(R0), [V4.H8, V5.H8, V6.H8, V7.H8]
	VLD1.P 64(R1), [V8.H8, V9.H8, V10.H8, V11.H8]
	VLD1.P 64(R1), [V12.H8, V13.H8, V14.H8, V15.H8]
	VADD   V8.H8, V0.H8, V0.H8
	VADD   V9.H8, V1.H8, V1.H8
	VADD   V10.H8, V2.H8, V2.H8
	VADD   V11.H8, V3.H8, V3.H8
	VADD   V12.H8, V4.H8, V4.H8
	VADD   V13.H8, V5.H8, V5.H8
	VADD   V14.H8, V6.H8, V6.H8
	VADD   V15.H8, V7.H8, V7.H8
	VST1.P [V0.H8, V1.H8, V2.H8, V3.H8], 64(R2)
	VST1.P [V4.H8, V5.H8, V6.H8, V7.H8], 64(R2)
	SUB    $1, R3
	CBNZ   R3, loop_add1024

	RET

// func addThreatVector1024NEON(acc *[1024]int16, weights *int8)
TEXT ·addThreatVector1024NEON(SB), NOSPLIT|NOFRAME, $0-16
	MOVD acc+0(FP), R0
	MOVD weights+8(FP), R1
	MOVD R0, R2

	MOVD $16, R3 // 16 iterations * 64 elements (1024 elements total)
loop_addthr:
	// Load 64 bytes of weights
	VLD1.P 64(R1), [V16.B16, V17.B16, V18.B16, V19.B16]
	VSXTL  V16.B8, V20.H8
	VSXTL2 V16.B16, V21.H8
	VSXTL  V17.B8, V22.H8
	VSXTL2 V17.B16, V23.H8
	VSXTL  V18.B8, V24.H8
	VSXTL2 V18.B16, V25.H8
	VSXTL  V19.B8, V26.H8
	VSXTL2 V19.B16, V27.H8

	// Load 128 bytes of accumulator
	VLD1.P 64(R0), [V0.H8, V1.H8, V2.H8, V3.H8]
	VLD1.P 64(R0), [V4.H8, V5.H8, V6.H8, V7.H8]

	VADD   V20.H8, V0.H8, V0.H8
	VADD   V21.H8, V1.H8, V1.H8
	VADD   V22.H8, V2.H8, V2.H8
	VADD   V23.H8, V3.H8, V3.H8
	VADD   V24.H8, V4.H8, V4.H8
	VADD   V25.H8, V5.H8, V5.H8
	VADD   V26.H8, V6.H8, V6.H8
	VADD   V27.H8, V7.H8, V7.H8

	VST1.P [V0.H8, V1.H8, V2.H8, V3.H8], 64(R2)
	VST1.P [V4.H8, V5.H8, V6.H8, V7.H8], 64(R2)

	SUB    $1, R3
	CBNZ   R3, loop_addthr

	RET

// func subVector1024NEON(acc *[1024]int16, weights *int16)
TEXT ·subVector1024NEON(SB), NOSPLIT|NOFRAME, $0-16
	MOVD acc+0(FP), R0
	MOVD weights+8(FP), R1
	MOVD R0, R2

	MOVD $16, R3 // 16 iterations of 128 bytes (2048 bytes / 1024 int16)
loop_sub1024:
	VLD1.P 64(R0), [V0.H8, V1.H8, V2.H8, V3.H8]
	VLD1.P 64(R0), [V4.H8, V5.H8, V6.H8, V7.H8]
	VLD1.P 64(R1), [V8.H8, V9.H8, V10.H8, V11.H8]
	VLD1.P 64(R1), [V12.H8, V13.H8, V14.H8, V15.H8]
	VSUB   V8.H8, V0.H8, V0.H8
	VSUB   V9.H8, V1.H8, V1.H8
	VSUB   V10.H8, V2.H8, V2.H8
	VSUB   V11.H8, V3.H8, V3.H8
	VSUB   V12.H8, V4.H8, V4.H8
	VSUB   V13.H8, V5.H8, V5.H8
	VSUB   V14.H8, V6.H8, V6.H8
	VSUB   V15.H8, V7.H8, V7.H8
	VST1.P [V0.H8, V1.H8, V2.H8, V3.H8], 64(R2)
	VST1.P [V4.H8, V5.H8, V6.H8, V7.H8], 64(R2)
	SUB    $1, R3
	CBNZ   R3, loop_sub1024

	RET

// func subThreatVector1024NEON(acc *[1024]int16, weights *int8)
TEXT ·subThreatVector1024NEON(SB), NOSPLIT|NOFRAME, $0-16
	MOVD acc+0(FP), R0
	MOVD weights+8(FP), R1
	MOVD R0, R2

	MOVD $16, R3 // 16 iterations * 64 elements (1024 elements total)
loop_subthr:
	// Load 64 bytes of weights
	VLD1.P 64(R1), [V16.B16, V17.B16, V18.B16, V19.B16]
	VSXTL  V16.B8, V20.H8
	VSXTL2 V16.B16, V21.H8
	VSXTL  V17.B8, V22.H8
	VSXTL2 V17.B16, V23.H8
	VSXTL  V18.B8, V24.H8
	VSXTL2 V18.B16, V25.H8
	VSXTL  V19.B8, V26.H8
	VSXTL2 V19.B16, V27.H8

	// Load 128 bytes of accumulator
	VLD1.P 64(R0), [V0.H8, V1.H8, V2.H8, V3.H8]
	VLD1.P 64(R0), [V4.H8, V5.H8, V6.H8, V7.H8]

	VSUB   V20.H8, V0.H8, V0.H8
	VSUB   V21.H8, V1.H8, V1.H8
	VSUB   V22.H8, V2.H8, V2.H8
	VSUB   V23.H8, V3.H8, V3.H8
	VSUB   V24.H8, V4.H8, V4.H8
	VSUB   V25.H8, V5.H8, V5.H8
	VSUB   V26.H8, V6.H8, V6.H8
	VSUB   V27.H8, V7.H8, V7.H8

	VST1.P [V0.H8, V1.H8, V2.H8, V3.H8], 64(R2)
	VST1.P [V4.H8, V5.H8, V6.H8, V7.H8], 64(R2)

	SUB    $1, R3
	CBNZ   R3, loop_subthr

	RET

// func activateEightFTNEON(out *uint8, pst *int16, thr *int16)
TEXT ·activateEightFTNEON(SB), NOSPLIT|NOFRAME, $0-24
	MOVD out+0(FP), R0
	MOVD pst+8(FP), R1
	MOVD thr+16(FP), R2

	ADD  $1024, R1, R3 // pst high half (+512 int16 = +1024 bytes)
	ADD  $1024, R2, R4 // thr high half (+512 int16 = +1024 bytes)

	MOVD $255, R5
	VDUP R5, V30.H8    // V30 = [255, 255, ...]
	VEOR V31.B16, V31.B16, V31.B16 // V31 = [0, 0, ...]

	MOVD $32, R6       // 32 iterations * 16 bytes = 512 bytes
loop_act:
	// Load 16 elements (32 bytes) of low half
	VLD1.P 32(R1), [V0.H8, V1.H8]
	VLD1.P 32(R2), [V2.H8, V3.H8]
	VADD   V0.H8, V2.H8, V0.H8 // left0
	VADD   V1.H8, V3.H8, V1.H8 // left1

	// Load 16 elements (32 bytes) of high half
	VLD1.P 32(R3), [V4.H8, V5.H8]
	VLD1.P 32(R4), [V6.H8, V7.H8]
	VADD   V4.H8, V6.H8, V4.H8 // right0
	VADD   V5.H8, V7.H8, V5.H8 // right1

	// Clamp left to [0, 255]
	VSMAX  V31.H8, V0.H8, V0.H8
	VSMIN  V30.H8, V0.H8, V0.H8
	VSMAX  V31.H8, V1.H8, V1.H8
	VSMIN  V30.H8, V1.H8, V1.H8

	// Clamp right to [0, 255]
	VSMAX  V31.H8, V4.H8, V4.H8
	VSMIN  V30.H8, V4.H8, V4.H8
	VSMAX  V31.H8, V5.H8, V5.H8
	VSMIN  V30.H8, V5.H8, V5.H8

	// Multiply left * right in 16-bit: since both are in [0, 255],
	// left * right <= 65025 < 65536, so low 16 bits are exact uint16.
	WORD $0x4e649c00 // MUL V0.8H, V0.8H, V4.8H
	WORD $0x4e659c21 // MUL V1.8H, V1.8H, V5.8H

	// Unsigned right shift by 9 -> [0, 127] in each uint16 lane
	WORD $0x6f170400 // USHR V0.8H, V0.8H, #9
	WORD $0x6f170421 // USHR V1.8H, V1.8H, #9

	// Pack low bytes of V0.8H and V1.8H into V14.16B
	WORD $0x4e01180e // UZP1 V14.16B, V0.16B, V1.16B

	// Store 16 bytes
	VST1.P [V14.B16], 16(R0)

	SUB    $1, R6
	CBNZ   R6, loop_act

	RET

// func activateSFNEON(out *uint8, acc *int16)
// Clipped pairwise product for Stockfish accumulators (no threat array):
// out[i] = (clamp(acc[i], 0, 255) * clamp(acc[i+512], 0, 255)) >> 9
// Processes 32 outputs (64 int16 inputs) per iteration.
TEXT ·activateSFNEON(SB), NOSPLIT|NOFRAME, $0-16
	MOVD out+0(FP), R0
	MOVD acc+8(FP), R1
	ADD  $1024, R1, R2 // high half (+512 int16 = +1024 bytes)

	MOVD $255, R5
	VDUP R5, V30.H8
	VEOR V31.B16, V31.B16, V31.B16

	MOVD $16, R6       // 16 iterations * 32 bytes = 512 bytes
loop_actsf:
	VLD1.P 64(R1), [V0.H8, V1.H8, V2.H8, V3.H8]
	VLD1.P 64(R2), [V4.H8, V5.H8, V6.H8, V7.H8]

	VSMAX V31.H8, V0.H8, V0.H8
	VSMIN V30.H8, V0.H8, V0.H8
	VSMAX V31.H8, V1.H8, V1.H8
	VSMIN V30.H8, V1.H8, V1.H8
	VSMAX V31.H8, V2.H8, V2.H8
	VSMIN V30.H8, V2.H8, V2.H8
	VSMAX V31.H8, V3.H8, V3.H8
	VSMIN V30.H8, V3.H8, V3.H8

	VSMAX V31.H8, V4.H8, V4.H8
	VSMIN V30.H8, V4.H8, V4.H8
	VSMAX V31.H8, V5.H8, V5.H8
	VSMIN V30.H8, V5.H8, V5.H8
	VSMAX V31.H8, V6.H8, V6.H8
	VSMIN V30.H8, V6.H8, V6.H8
	VSMAX V31.H8, V7.H8, V7.H8
	VSMIN V30.H8, V7.H8, V7.H8

	WORD $0x4e649c00 // MUL V0.8H, V0.8H, V4.8H
	WORD $0x4e659c21 // MUL V1.8H, V1.8H, V5.8H
	WORD $0x4e669c42 // MUL V2.8H, V2.8H, V6.8H
	WORD $0x4e679c63 // MUL V3.8H, V3.8H, V7.8H

	WORD $0x6f170400 // USHR V0.8H, V0.8H, #9
	WORD $0x6f170421 // USHR V1.8H, V1.8H, #9
	WORD $0x6f170442 // USHR V2.8H, V2.8H, #9
	WORD $0x6f170463 // USHR V3.8H, V3.8H, #9

	WORD $0x4e011808 // UZP1 V8.16B, V0.16B, V1.16B
	WORD $0x4e031849 // UZP1 V9.16B, V2.16B, V3.16B

	VST1.P [V8.B16, V9.B16], 32(R0)

	SUB  $1, R6
	CBNZ R6, loop_actsf

	RET

// ---------------------------------------------------------------------------
// First dense layer, sparse form. See refL1Nonzero and refL1Sparse in
// vec_ref.go for the definitions, and eightL1 for how they are used.
//
// Instructions that older Go assemblers do not know are written as WORD with
// the mnemonic alongside.
// ---------------------------------------------------------------------------

// func l1NonzeroNEON(ft *[1024]uint8, nz *uint32) int
// For each non-zero byte of ft, in order, writes index | value<<16 to nz;
// returns the count. Each 16-byte block becomes a 64-bit mask with four bits
// per byte (CMTST gives 0xFF per non-zero byte, SHRN #4 narrows each byte
// pair to one), and the set nibbles are walked with RBIT+CLZ.
TEXT ·l1NonzeroNEON(SB), NOSPLIT|NOFRAME, $0-24
	MOVD ft+0(FP), R0
	MOVD nz+8(FP), R1
	MOVD R1, R10        // first entry
	MOVD $0, R2         // input index at the start of the block
	MOVD $15, R12
block:
	VLD1.P 16(R0), [V0.B16]
	WORD $0x4e208c01 // CMTST V1.16B, V0.16B, V0.16B
	WORD $0x0f0c8422 // SHRN V2.8B, V1.8H, #4
	FMOVD F2, R3        // the mask
	CBZ   R3, next
	SUB   $16, R0, R7   // start of this block
bits:
	RBIT  R3, R4
	CLZ   R4, R4        // bit position: 4 * byte offset
	LSR   $2, R4, R5    // byte offset in the block
	MOVBU (R7)(R5), R8  // value
	ADD   R2, R5, R6    // input index
	ORR   R8<<16, R6, R9
	MOVW.P R9, 4(R1)
	LSL   R4, R12, R11
	BIC   R11, R3, R3   // clear this byte's nibble
	CBNZ  R3, bits
next:
	ADD $16, R2, R2
	CMP $1024, R2
	BLT block
	SUB R10, R1, R1
	LSR $2, R1, R1
	MOVD R1, ret+16(FP)
	RET

// func l1SparseNEON(nz *uint32, n int, wt *[1024][16]int8, out *[16]int32)
// Per entry: sign-extend the 16-byte weight row to int16, broadcast the value
// to every int16 lane, and multiply-accumulate into four int32 vectors
// (outputs 0-3, 4-7, 8-11, 12-15). Values are at most 255 and weights at
// least -128, so each product is exact in the widening multiply.
TEXT ·l1SparseNEON(SB), NOSPLIT|NOFRAME, $0-32
	MOVD nz+0(FP), R0
	MOVD n+8(FP), R1
	MOVD wt+16(FP), R2
	MOVD out+24(FP), R3
	VEOR V0.B16, V0.B16, V0.B16
	VEOR V1.B16, V1.B16, V1.B16
	VEOR V2.B16, V2.B16, V2.B16
	VEOR V3.B16, V3.B16, V3.B16
	CBZ  R1, done
loop:
	MOVWU.P 4(R0), R4
	AND  $0xFFFF, R4, R5  // input index
	LSR  $16, R4, R6      // value
	ADD  R5<<4, R2, R7    // its weight row
	VLD1 (R7), [V4.B16]
	WORD $0x0f08a485 // SXTL V5.8H, V4.8B (weights 0..7)
	WORD $0x4f08a486 // SXTL2 V6.8H, V4.16B (weights 8..15)
	WORD $0x4e020cc7 // DUP V7.8H, W6 (value)
	WORD $0x0e6780a0 // SMLAL V0.4S, V5.4H, V7.4H
	WORD $0x4e6780a1 // SMLAL2 V1.4S, V5.8H, V7.8H
	WORD $0x0e6780c2 // SMLAL V2.4S, V6.4H, V7.4H
	WORD $0x4e6780c3 // SMLAL2 V3.4S, V6.8H, V7.8H
	SUB  $1, R1, R1
	CBNZ R1, loop
done:
	VST1 [V0.S4, V1.S4, V2.S4, V3.S4], (R3)
	RET

// func l1SparseIdxNEON(idx *uint16, n int, ft *[1024]uint8, wt *[1024][16]int8, out *[16]int32)
// l1SparseNEON with the inputs given as a list of indices; each value is
// read from ft.
TEXT ·l1SparseIdxNEON(SB), NOSPLIT|NOFRAME, $0-40
	MOVD idx+0(FP), R0
	MOVD n+8(FP), R1
	MOVD ft+16(FP), R8
	MOVD wt+24(FP), R2
	MOVD out+32(FP), R3
	VEOR V0.B16, V0.B16, V0.B16
	VEOR V1.B16, V1.B16, V1.B16
	VEOR V2.B16, V2.B16, V2.B16
	VEOR V3.B16, V3.B16, V3.B16
	CBZ  R1, done
loop:
	MOVHU.P 2(R0), R5     // input index
	MOVBU (R8)(R5), R6    // value
	ADD  R5<<4, R2, R7    // its weight row
	VLD1 (R7), [V4.B16]
	WORD $0x0f08a485 // SXTL V5.8H, V4.8B (weights 0..7)
	WORD $0x4f08a486 // SXTL2 V6.8H, V4.16B (weights 8..15)
	WORD $0x4e020cc7 // DUP V7.8H, W6 (value)
	WORD $0x0e6780a0 // SMLAL V0.4S, V5.4H, V7.4H
	WORD $0x4e6780a1 // SMLAL2 V1.4S, V5.8H, V7.8H
	WORD $0x0e6780c2 // SMLAL V2.4S, V6.4H, V7.4H
	WORD $0x4e6780c3 // SMLAL2 V3.4S, V6.8H, V7.8H
	SUB  $1, R1, R1
	CBNZ R1, loop
done:
	VST1 [V0.S4, V1.S4, V2.S4, V3.S4], (R3)
	RET

// func sparse32NEON(idx *uint16, n int, in *uint8, wt *int8, out *[32]int32)
// l1SparseIdxNEON for a layer of 32 outputs: wt holds one 32-byte weight row
// per input. out receives the sums (no bias).
TEXT ·sparse32NEON(SB), NOSPLIT|NOFRAME, $0-40
	MOVD idx+0(FP), R0
	MOVD n+8(FP), R1
	MOVD in+16(FP), R8
	MOVD wt+24(FP), R2
	MOVD out+32(FP), R3
	VEOR V0.B16, V0.B16, V0.B16
	VEOR V1.B16, V1.B16, V1.B16
	VEOR V2.B16, V2.B16, V2.B16
	VEOR V3.B16, V3.B16, V3.B16
	VEOR V4.B16, V4.B16, V4.B16
	VEOR V5.B16, V5.B16, V5.B16
	VEOR V6.B16, V6.B16, V6.B16
	VEOR V7.B16, V7.B16, V7.B16
	CBZ  R1, done
loop:
	MOVHU.P 2(R0), R5     // input index
	MOVBU (R8)(R5), R6    // value
	ADD  R5<<5, R2, R7    // its weight row
	VLD1 (R7), [V8.B16, V9.B16]
	WORD $0x0f08a50a // SXTL V10.8H, V8.8B
	WORD $0x4f08a50b // SXTL2 V11.8H, V8.16B
	WORD $0x0f08a52c // SXTL V12.8H, V9.8B
	WORD $0x4f08a52d // SXTL2 V13.8H, V9.16B
	WORD $0x4e020cce // DUP V14.8H, W6 (value)
	WORD $0x0e6e8140 // SMLAL V0.4S, V10.4H, V14.4H
	WORD $0x4e6e8141 // SMLAL2 V1.4S, V10.8H, V14.8H
	WORD $0x0e6e8162 // SMLAL V2.4S, V11.4H, V14.4H
	WORD $0x4e6e8163 // SMLAL2 V3.4S, V11.8H, V14.8H
	WORD $0x0e6e8184 // SMLAL V4.4S, V12.4H, V14.4H
	WORD $0x4e6e8185 // SMLAL2 V5.4S, V12.8H, V14.8H
	WORD $0x0e6e81a6 // SMLAL V6.4S, V13.4H, V14.4H
	WORD $0x4e6e81a7 // SMLAL2 V7.4S, V13.8H, V14.8H
	SUB  $1, R1, R1
	CBNZ R1, loop
done:
	VST1.P [V0.S4, V1.S4, V2.S4, V3.S4], 64(R3)
	VST1   [V4.S4, V5.S4, V6.S4, V7.S4], (R3)
	RET

// func chunkDot32NEON(idx *uint16, n int, in *uint8, wt *int8, out *[32]int32)
// The 32-output layer by groups of four inputs. For each listed group g, the
// four input bytes at in[4g] are repeated across a vector and multiplied into
// the group's 128 weight bytes ([output][input in group]) with SDOT, which
// adds the four products of each output to that output's sum. The inputs are
// at most 127, so reading them as signed bytes is exact. Requires the
// dot-product extension (FEAT_DotProd).
TEXT ·chunkDot32NEON(SB), NOSPLIT|NOFRAME, $0-40
	MOVD idx+0(FP), R0
	MOVD n+8(FP), R1
	MOVD in+16(FP), R8
	MOVD wt+24(FP), R2
	MOVD out+32(FP), R3
	VEOR V0.B16, V0.B16, V0.B16
	VEOR V1.B16, V1.B16, V1.B16
	VEOR V2.B16, V2.B16, V2.B16
	VEOR V3.B16, V3.B16, V3.B16
	VEOR V4.B16, V4.B16, V4.B16
	VEOR V5.B16, V5.B16, V5.B16
	VEOR V6.B16, V6.B16, V6.B16
	VEOR V7.B16, V7.B16, V7.B16
	CBZ  R1, done
loop:
	MOVHU.P 2(R0), R5     // group index
	ADD  R5<<2, R8, R6    // its four inputs
	WORD $0x4d40c8d0 // LD1R {V16.4S}, [R6]
	ADD  R5<<7, R2, R7    // its weights
	VLD1.P 64(R7), [V8.B16, V9.B16, V10.B16, V11.B16]
	VLD1   (R7), [V12.B16, V13.B16, V14.B16, V15.B16]
	WORD $0x4e909500 // SDOT V0.4S, V8.16B, V16.16B
	WORD $0x4e909521 // SDOT V1.4S, V9.16B, V16.16B
	WORD $0x4e909542 // SDOT V2.4S, V10.16B, V16.16B
	WORD $0x4e909563 // SDOT V3.4S, V11.16B, V16.16B
	WORD $0x4e909584 // SDOT V4.4S, V12.16B, V16.16B
	WORD $0x4e9095a5 // SDOT V5.4S, V13.16B, V16.16B
	WORD $0x4e9095c6 // SDOT V6.4S, V14.16B, V16.16B
	WORD $0x4e9095e7 // SDOT V7.4S, V15.16B, V16.16B
	SUB  $1, R1, R1
	CBNZ R1, loop
done:
	VST1.P [V0.S4, V1.S4, V2.S4, V3.S4], 64(R3)
	VST1   [V4.S4, V5.S4, V6.S4, V7.S4], (R3)
	RET

// func sfAct7NEON(x *[32]int32, bias *[32]int32, out *uint8)
// For v = x[i] + bias[i]: out[i] = min(127, v*v >> 21) and
// out[32+i] = clamp(v >> 7, 0, 127). v is first saturated to 16 bits, which
// changes neither result; SQDMULH then gives v*v >> 15.
TEXT ·sfAct7NEON(SB), NOSPLIT|NOFRAME, $0-24
	MOVD x+0(FP), R0
	MOVD bias+8(FP), R1
	MOVD out+16(FP), R2
	ADD  $32, R2, R3
	VEOR V31.B16, V31.B16, V31.B16
	MOVD $2, R4
loop:
	VLD1.P 64(R0), [V0.S4, V1.S4, V2.S4, V3.S4]
	VLD1.P 64(R1), [V4.S4, V5.S4, V6.S4, V7.S4]
	VADD V4.S4, V0.S4, V0.S4
	VADD V5.S4, V1.S4, V1.S4
	VADD V6.S4, V2.S4, V2.S4
	VADD V7.S4, V3.S4, V3.S4
	WORD $0x0e614808 // SQXTN V8.4H, V0.4S
	WORD $0x4e614828 // SQXTN2 V8.8H, V1.4S
	WORD $0x0e614849 // SQXTN V9.4H, V2.4S
	WORD $0x4e614869 // SQXTN2 V9.8H, V3.4S
	WORD $0x4e68b508 // SQDMULH V8.8H, V8.8H, V8.8H
	WORD $0x4e69b529 // SQDMULH V9.8H, V9.8H, V9.8H
	WORD $0x4f1a0508 // SSHR V8.8H, V8.8H, #6
	WORD $0x4f1a0529 // SSHR V9.8H, V9.8H, #6
	WORD $0x0e21490a // SQXTN V10.8B, V8.8H
	WORD $0x4e21492a // SQXTN2 V10.16B, V9.8H
	VST1.P [V10.B16], 16(R2)
	WORD $0x0f19940b // SQSHRN V11.4H, V0.4S, #7
	WORD $0x4f19942b // SQSHRN2 V11.8H, V1.4S, #7
	WORD $0x0f19944c // SQSHRN V12.4H, V2.4S, #7
	WORD $0x4f19946c // SQSHRN2 V12.8H, V3.4S, #7
	WORD $0x0e21496d // SQXTN V13.8B, V11.8H
	WORD $0x4e21498d // SQXTN2 V13.16B, V12.8H
	WORD $0x4e3f65ad // SMAX V13.16B, V13.16B, V31.16B
	VST1.P [V13.B16], 16(R3)
	SUB  $1, R4, R4
	CBNZ R4, loop
	RET

// func sfAct6NEON(x *[32]int32, bias *[32]int32, out *uint8)
// For v = x[i] + bias[i]: out[i] = min(127, v*v >> 19) and
// out[32+i] = clamp(v >> 6, 0, 127). v is first saturated to 16 bits, which
// changes neither result; SQDMULH then gives v*v >> 15.
TEXT ·sfAct6NEON(SB), NOSPLIT|NOFRAME, $0-24
	MOVD x+0(FP), R0
	MOVD bias+8(FP), R1
	MOVD out+16(FP), R2
	ADD  $32, R2, R3
	VEOR V31.B16, V31.B16, V31.B16
	MOVD $2, R4
loop:
	VLD1.P 64(R0), [V0.S4, V1.S4, V2.S4, V3.S4]
	VLD1.P 64(R1), [V4.S4, V5.S4, V6.S4, V7.S4]
	VADD V4.S4, V0.S4, V0.S4
	VADD V5.S4, V1.S4, V1.S4
	VADD V6.S4, V2.S4, V2.S4
	VADD V7.S4, V3.S4, V3.S4
	WORD $0x0e614808 // SQXTN V8.4H, V0.4S
	WORD $0x4e614828 // SQXTN2 V8.8H, V1.4S
	WORD $0x0e614849 // SQXTN V9.4H, V2.4S
	WORD $0x4e614869 // SQXTN2 V9.8H, V3.4S
	WORD $0x4e68b508 // SQDMULH V8.8H, V8.8H, V8.8H
	WORD $0x4e69b529 // SQDMULH V9.8H, V9.8H, V9.8H
	WORD $0x4f1c0508 // SSHR V8.8H, V8.8H, #4
	WORD $0x4f1c0529 // SSHR V9.8H, V9.8H, #4
	WORD $0x0e21490a // SQXTN V10.8B, V8.8H
	WORD $0x4e21492a // SQXTN2 V10.16B, V9.8H
	VST1.P [V10.B16], 16(R2)
	WORD $0x0f1a940b // SQSHRN V11.4H, V0.4S, #6
	WORD $0x4f1a942b // SQSHRN2 V11.8H, V1.4S, #6
	WORD $0x0f1a944c // SQSHRN V12.4H, V2.4S, #6
	WORD $0x4f1a946c // SQSHRN2 V12.8H, V3.4S, #6
	WORD $0x0e21496d // SQXTN V13.8B, V11.8H
	WORD $0x4e21498d // SQXTN2 V13.16B, V12.8H
	WORD $0x4e3f65ad // SMAX V13.16B, V13.16B, V31.16B
	VST1.P [V13.B16], 16(R3)
	SUB  $1, R4, R4
	CBNZ R4, loop
	RET

// func dot128NEON(a *[128]uint8, w *[128]int8, out *[4]int32)
// out[0]+out[1]+out[2]+out[3] is the dot product of a (each at most 127) and
// w. Requires FEAT_DotProd.
TEXT ·dot128NEON(SB), NOSPLIT|NOFRAME, $0-24
	MOVD a+0(FP), R0
	MOVD w+8(FP), R1
	MOVD out+16(FP), R2
	VEOR V0.B16, V0.B16, V0.B16
	VEOR V1.B16, V1.B16, V1.B16
	MOVD $2, R3
loop:
	VLD1.P 64(R0), [V4.B16, V5.B16, V6.B16, V7.B16]
	VLD1.P 64(R1), [V8.B16, V9.B16, V10.B16, V11.B16]
	WORD $0x4e849500 // SDOT V0.4S, V8.16B, V4.16B
	WORD $0x4e859521 // SDOT V1.4S, V9.16B, V5.16B
	WORD $0x4e869540 // SDOT V0.4S, V10.16B, V6.16B
	WORD $0x4e879561 // SDOT V1.4S, V11.16B, V7.16B
	SUB  $1, R3, R3
	CBNZ R3, loop
	VADD V1.S4, V0.S4, V0.S4
	VST1 [V0.S4], (R2)
	RET

// ---------------------------------------------------------------------------
// Fused accumulator update: dst = src + sum of the added weight rows - sum of
// the removed ones, one block of sixteen vector registers (128 int16) at a
// time, so the accumulator is read and written once. dst and src may be the
// same array. See refFusedPiece/refFusedThreat.
// ---------------------------------------------------------------------------
// func fusedPieceNEON(dst, src *[1024]int16, w *int16, add *int32, nAdd int, rem *int32, nRem int)
TEXT ·fusedPieceNEON(SB), NOSPLIT|NOFRAME, $0-56
	MOVD dst+0(FP), R0
	MOVD src+8(FP), R1
	MOVD w+16(FP), R2
	MOVD add+24(FP), R3
	MOVD nAdd+32(FP), R4
	MOVD rem+40(FP), R5
	MOVD nRem+48(FP), R6
	MOVD $0, R7 // accumulator offset of this block
	MOVD $0, R8 // weight-row offset of this block
block:
	ADD R7, R1, R9
	VLD1.P 64(R9), [V0.H8, V1.H8, V2.H8, V3.H8]
	VLD1.P 64(R9), [V4.H8, V5.H8, V6.H8, V7.H8]
	VLD1.P 64(R9), [V8.H8, V9.H8, V10.H8, V11.H8]
	VLD1.P 64(R9), [V12.H8, V13.H8, V14.H8, V15.H8]
	MOVD $0, R10
adds:
	CMP  R4, R10
	BGE  addsdone
	MOVWU (R3)(R10<<2), R11
	ADD  R11<<11, R2, R12
	ADD  R8, R12, R12
	VLD1.P 64(R12), [V16.H8, V17.H8, V18.H8, V19.H8]
	VADD V16.H8, V0.H8, V0.H8
	VADD V17.H8, V1.H8, V1.H8
	VADD V18.H8, V2.H8, V2.H8
	VADD V19.H8, V3.H8, V3.H8
	VLD1.P 64(R12), [V16.H8, V17.H8, V18.H8, V19.H8]
	VADD V16.H8, V4.H8, V4.H8
	VADD V17.H8, V5.H8, V5.H8
	VADD V18.H8, V6.H8, V6.H8
	VADD V19.H8, V7.H8, V7.H8
	VLD1.P 64(R12), [V16.H8, V17.H8, V18.H8, V19.H8]
	VADD V16.H8, V8.H8, V8.H8
	VADD V17.H8, V9.H8, V9.H8
	VADD V18.H8, V10.H8, V10.H8
	VADD V19.H8, V11.H8, V11.H8
	VLD1.P 64(R12), [V16.H8, V17.H8, V18.H8, V19.H8]
	VADD V16.H8, V12.H8, V12.H8
	VADD V17.H8, V13.H8, V13.H8
	VADD V18.H8, V14.H8, V14.H8
	VADD V19.H8, V15.H8, V15.H8
	ADD  $1, R10, R10
	B    adds
addsdone:
	MOVD $0, R10
rems:
	CMP  R6, R10
	BGE  remsdone
	MOVWU (R5)(R10<<2), R11
	ADD  R11<<11, R2, R12
	ADD  R8, R12, R12
	VLD1.P 64(R12), [V16.H8, V17.H8, V18.H8, V19.H8]
	VSUB V16.H8, V0.H8, V0.H8
	VSUB V17.H8, V1.H8, V1.H8
	VSUB V18.H8, V2.H8, V2.H8
	VSUB V19.H8, V3.H8, V3.H8
	VLD1.P 64(R12), [V16.H8, V17.H8, V18.H8, V19.H8]
	VSUB V16.H8, V4.H8, V4.H8
	VSUB V17.H8, V5.H8, V5.H8
	VSUB V18.H8, V6.H8, V6.H8
	VSUB V19.H8, V7.H8, V7.H8
	VLD1.P 64(R12), [V16.H8, V17.H8, V18.H8, V19.H8]
	VSUB V16.H8, V8.H8, V8.H8
	VSUB V17.H8, V9.H8, V9.H8
	VSUB V18.H8, V10.H8, V10.H8
	VSUB V19.H8, V11.H8, V11.H8
	VLD1.P 64(R12), [V16.H8, V17.H8, V18.H8, V19.H8]
	VSUB V16.H8, V12.H8, V12.H8
	VSUB V17.H8, V13.H8, V13.H8
	VSUB V18.H8, V14.H8, V14.H8
	VSUB V19.H8, V15.H8, V15.H8
	ADD  $1, R10, R10
	B    rems
remsdone:
	ADD R7, R0, R9
	VST1.P [V0.H8, V1.H8, V2.H8, V3.H8], 64(R9)
	VST1.P [V4.H8, V5.H8, V6.H8, V7.H8], 64(R9)
	VST1.P [V8.H8, V9.H8, V10.H8, V11.H8], 64(R9)
	VST1.P [V12.H8, V13.H8, V14.H8, V15.H8], 64(R9)
	ADD $256, R7, R7
	ADD $256, R8, R8
	CMP $2048, R7
	BLT block
	RET

// func fusedThreatNEON(dst, src *[1024]int16, w *int8, add *int32, nAdd int, rem *int32, nRem int)
// The int8 weights are added to the int16 accumulator with the widening
// add/subtract (SADDW/SSUBW on the low eight bytes, SADDW2/SSUBW2 on the high
// eight), which sign-extends and adds in one instruction.
TEXT ·fusedThreatNEON(SB), NOSPLIT|NOFRAME, $0-56
	MOVD dst+0(FP), R0
	MOVD src+8(FP), R1
	MOVD w+16(FP), R2
	MOVD add+24(FP), R3
	MOVD nAdd+32(FP), R4
	MOVD rem+40(FP), R5
	MOVD nRem+48(FP), R6
	MOVD $0, R7 // accumulator offset of this block
	MOVD $0, R8 // weight-row offset of this block
	SUB  $1, R4, R16 // nAdd - 1
	SUB  $1, R6, R17 // nRem - 1
block:
	ADD R7, R1, R9
	FLDPQ 0(R9), (F0, F1)
	FLDPQ 32(R9), (F2, F3)
	FLDPQ 64(R9), (F4, F5)
	FLDPQ 96(R9), (F6, F7)
	FLDPQ 128(R9), (F8, F9)
	FLDPQ 160(R9), (F10, F11)
	FLDPQ 192(R9), (F12, F13)
	FLDPQ 224(R9), (F14, F15)
	ADD  R8, R2, R15 // w + block offset
	MOVD $0, R10
	CMP  R16, R10
	BGE  addstail
adds2:
	ADD  $1, R10, R13
	MOVWU (R3)(R10<<2), R11
	MOVWU (R3)(R13<<2), R14
	ADD  R11<<10, R15, R12
	ADD  R14<<10, R15, R13
	FLDPQ 0(R12), (F16, F17)
	FLDPQ 32(R12), (F18, F19)
	FLDPQ 0(R13), (F20, F21)
	FLDPQ 32(R13), (F22, F23)
	FLDPQ 64(R12), (F24, F25)
	FLDPQ 96(R12), (F26, F27)
	FLDPQ 64(R13), (F28, F29)
	FLDPQ 96(R13), (F30, F31)
	WORD $0x0e301000 // SADDW  V0.8H, V0.8H, V16.8B
	WORD $0x4e301021 // SADDW2 V1.8H, V1.8H, V16.16B
	WORD $0x0e311042 // SADDW  V2.8H, V2.8H, V17.8B
	WORD $0x4e311063 // SADDW2 V3.8H, V3.8H, V17.16B
	WORD $0x0e321084 // SADDW  V4.8H, V4.8H, V18.8B
	WORD $0x4e3210a5 // SADDW2 V5.8H, V5.8H, V18.16B
	WORD $0x0e3310c6 // SADDW  V6.8H, V6.8H, V19.8B
	WORD $0x4e3310e7 // SADDW2 V7.8H, V7.8H, V19.16B
	WORD $0x0e341000 // SADDW  V0.8H, V0.8H, V20.8B
	WORD $0x4e341021 // SADDW2 V1.8H, V1.8H, V20.16B
	WORD $0x0e351042 // SADDW  V2.8H, V2.8H, V21.8B
	WORD $0x4e351063 // SADDW2 V3.8H, V3.8H, V21.16B
	WORD $0x0e361084 // SADDW  V4.8H, V4.8H, V22.8B
	WORD $0x4e3610a5 // SADDW2 V5.8H, V5.8H, V22.16B
	WORD $0x0e3710c6 // SADDW  V6.8H, V6.8H, V23.8B
	WORD $0x4e3710e7 // SADDW2 V7.8H, V7.8H, V23.16B
	WORD $0x0e381108 // SADDW  V8.8H, V8.8H, V24.8B
	WORD $0x4e381129 // SADDW2 V9.8H, V9.8H, V24.16B
	WORD $0x0e39114a // SADDW  V10.8H, V10.8H, V25.8B
	WORD $0x4e39116b // SADDW2 V11.8H, V11.8H, V25.16B
	WORD $0x0e3a118c // SADDW  V12.8H, V12.8H, V26.8B
	WORD $0x4e3a11ad // SADDW2 V13.8H, V13.8H, V26.16B
	WORD $0x0e3b11ce // SADDW  V14.8H, V14.8H, V27.8B
	WORD $0x4e3b11ef // SADDW2 V15.8H, V15.8H, V27.16B
	WORD $0x0e3c1108 // SADDW  V8.8H, V8.8H, V28.8B
	WORD $0x4e3c1129 // SADDW2 V9.8H, V9.8H, V28.16B
	WORD $0x0e3d114a // SADDW  V10.8H, V10.8H, V29.8B
	WORD $0x4e3d116b // SADDW2 V11.8H, V11.8H, V29.16B
	WORD $0x0e3e118c // SADDW  V12.8H, V12.8H, V30.8B
	WORD $0x4e3e11ad // SADDW2 V13.8H, V13.8H, V30.16B
	WORD $0x0e3f11ce // SADDW  V14.8H, V14.8H, V31.8B
	WORD $0x4e3f11ef // SADDW2 V15.8H, V15.8H, V31.16B
	ADD  $2, R10, R10
	CMP  R16, R10
	BLT  adds2
addstail:
	CMP  R4, R10
	BGE  addsdone
	MOVWU (R3)(R10<<2), R11
	ADD  R11<<10, R15, R12
	FLDPQ 0(R12), (F16, F17)
	FLDPQ 32(R12), (F18, F19)
	FLDPQ 64(R12), (F20, F21)
	FLDPQ 96(R12), (F22, F23)
	WORD $0x0e301000 // SADDW  V0.8H, V0.8H, V16.8B
	WORD $0x4e301021 // SADDW2 V1.8H, V1.8H, V16.16B
	WORD $0x0e311042 // SADDW  V2.8H, V2.8H, V17.8B
	WORD $0x4e311063 // SADDW2 V3.8H, V3.8H, V17.16B
	WORD $0x0e321084 // SADDW  V4.8H, V4.8H, V18.8B
	WORD $0x4e3210a5 // SADDW2 V5.8H, V5.8H, V18.16B
	WORD $0x0e3310c6 // SADDW  V6.8H, V6.8H, V19.8B
	WORD $0x4e3310e7 // SADDW2 V7.8H, V7.8H, V19.16B
	WORD $0x0e341108 // SADDW  V8.8H, V8.8H, V20.8B
	WORD $0x4e341129 // SADDW2 V9.8H, V9.8H, V20.16B
	WORD $0x0e35114a // SADDW  V10.8H, V10.8H, V21.8B
	WORD $0x4e35116b // SADDW2 V11.8H, V11.8H, V21.16B
	WORD $0x0e36118c // SADDW  V12.8H, V12.8H, V22.8B
	WORD $0x4e3611ad // SADDW2 V13.8H, V13.8H, V22.16B
	WORD $0x0e3711ce // SADDW  V14.8H, V14.8H, V23.8B
	WORD $0x4e3711ef // SADDW2 V15.8H, V15.8H, V23.16B
addsdone:
	MOVD $0, R10
	CMP  R17, R10
	BGE  remstail
rems2:
	ADD  $1, R10, R13
	MOVWU (R5)(R10<<2), R11
	MOVWU (R5)(R13<<2), R14
	ADD  R11<<10, R15, R12
	ADD  R14<<10, R15, R13
	FLDPQ 0(R12), (F16, F17)
	FLDPQ 32(R12), (F18, F19)
	FLDPQ 0(R13), (F20, F21)
	FLDPQ 32(R13), (F22, F23)
	FLDPQ 64(R12), (F24, F25)
	FLDPQ 96(R12), (F26, F27)
	FLDPQ 64(R13), (F28, F29)
	FLDPQ 96(R13), (F30, F31)
	WORD $0x0e303000 // SSUBW  V0.8H, V0.8H, V16.8B
	WORD $0x4e303021 // SSUBW2 V1.8H, V1.8H, V16.16B
	WORD $0x0e313042 // SSUBW  V2.8H, V2.8H, V17.8B
	WORD $0x4e313063 // SSUBW2 V3.8H, V3.8H, V17.16B
	WORD $0x0e323084 // SSUBW  V4.8H, V4.8H, V18.8B
	WORD $0x4e3230a5 // SSUBW2 V5.8H, V5.8H, V18.16B
	WORD $0x0e3330c6 // SSUBW  V6.8H, V6.8H, V19.8B
	WORD $0x4e3330e7 // SSUBW2 V7.8H, V7.8H, V19.16B
	WORD $0x0e343000 // SSUBW  V0.8H, V0.8H, V20.8B
	WORD $0x4e343021 // SSUBW2 V1.8H, V1.8H, V20.16B
	WORD $0x0e353042 // SSUBW  V2.8H, V2.8H, V21.8B
	WORD $0x4e353063 // SSUBW2 V3.8H, V3.8H, V21.16B
	WORD $0x0e363084 // SSUBW  V4.8H, V4.8H, V22.8B
	WORD $0x4e3630a5 // SSUBW2 V5.8H, V5.8H, V22.16B
	WORD $0x0e3730c6 // SSUBW  V6.8H, V6.8H, V23.8B
	WORD $0x4e3730e7 // SSUBW2 V7.8H, V7.8H, V23.16B
	WORD $0x0e383108 // SSUBW  V8.8H, V8.8H, V24.8B
	WORD $0x4e383129 // SSUBW2 V9.8H, V9.8H, V24.16B
	WORD $0x0e39314a // SSUBW  V10.8H, V10.8H, V25.8B
	WORD $0x4e39316b // SSUBW2 V11.8H, V11.8H, V25.16B
	WORD $0x0e3a318c // SSUBW  V12.8H, V12.8H, V26.8B
	WORD $0x4e3a31ad // SSUBW2 V13.8H, V13.8H, V26.16B
	WORD $0x0e3b31ce // SSUBW  V14.8H, V14.8H, V27.8B
	WORD $0x4e3b31ef // SSUBW2 V15.8H, V15.8H, V27.16B
	WORD $0x0e3c3108 // SSUBW  V8.8H, V8.8H, V28.8B
	WORD $0x4e3c3129 // SSUBW2 V9.8H, V9.8H, V28.16B
	WORD $0x0e3d314a // SSUBW  V10.8H, V10.8H, V29.8B
	WORD $0x4e3d316b // SSUBW2 V11.8H, V11.8H, V29.16B
	WORD $0x0e3e318c // SSUBW  V12.8H, V12.8H, V30.8B
	WORD $0x4e3e31ad // SSUBW2 V13.8H, V13.8H, V30.16B
	WORD $0x0e3f31ce // SSUBW  V14.8H, V14.8H, V31.8B
	WORD $0x4e3f31ef // SSUBW2 V15.8H, V15.8H, V31.16B
	ADD  $2, R10, R10
	CMP  R17, R10
	BLT  rems2
remstail:
	CMP  R6, R10
	BGE  remsdone
	MOVWU (R5)(R10<<2), R11
	ADD  R11<<10, R15, R12
	FLDPQ 0(R12), (F16, F17)
	FLDPQ 32(R12), (F18, F19)
	FLDPQ 64(R12), (F20, F21)
	FLDPQ 96(R12), (F22, F23)
	WORD $0x0e303000 // SSUBW  V0.8H, V0.8H, V16.8B
	WORD $0x4e303021 // SSUBW2 V1.8H, V1.8H, V16.16B
	WORD $0x0e313042 // SSUBW  V2.8H, V2.8H, V17.8B
	WORD $0x4e313063 // SSUBW2 V3.8H, V3.8H, V17.16B
	WORD $0x0e323084 // SSUBW  V4.8H, V4.8H, V18.8B
	WORD $0x4e3230a5 // SSUBW2 V5.8H, V5.8H, V18.16B
	WORD $0x0e3330c6 // SSUBW  V6.8H, V6.8H, V19.8B
	WORD $0x4e3330e7 // SSUBW2 V7.8H, V7.8H, V19.16B
	WORD $0x0e343108 // SSUBW  V8.8H, V8.8H, V20.8B
	WORD $0x4e343129 // SSUBW2 V9.8H, V9.8H, V20.16B
	WORD $0x0e35314a // SSUBW  V10.8H, V10.8H, V21.8B
	WORD $0x4e35316b // SSUBW2 V11.8H, V11.8H, V21.16B
	WORD $0x0e36318c // SSUBW  V12.8H, V12.8H, V22.8B
	WORD $0x4e3631ad // SSUBW2 V13.8H, V13.8H, V22.16B
	WORD $0x0e3731ce // SSUBW  V14.8H, V14.8H, V23.8B
	WORD $0x4e3731ef // SSUBW2 V15.8H, V15.8H, V23.16B
remsdone:
	ADD R7, R0, R9
	FSTPQ (F0, F1), 0(R9)
	FSTPQ (F2, F3), 32(R9)
	FSTPQ (F4, F5), 64(R9)
	FSTPQ (F6, F7), 96(R9)
	FSTPQ (F8, F9), 128(R9)
	FSTPQ (F10, F11), 160(R9)
	FSTPQ (F12, F13), 192(R9)
	FSTPQ (F14, F15), 224(R9)
	ADD $256, R7, R7
	ADD $128, R8, R8
	CMP $2048, R7
	BLT block
	RET

// ---------------------------------------------------------------------------
// Second dense layer: out[j] = clamp(bias[j] + sum_i w[i][j]*x[i], 0, 1) for
// 16 inputs and 32 outputs, eight vectors of four lanes. Separate FMUL and
// FADD (not FMLA), in input order, as refL2 specifies.
// ---------------------------------------------------------------------------

// func l2LayerNEON(w *[16][32]float32, bias *[32]float32, x *[16]float32, out *[32]float32)
TEXT ·l2LayerNEON(SB), NOSPLIT|NOFRAME, $0-32
	MOVD w+0(FP), R0
	MOVD bias+8(FP), R1
	MOVD x+16(FP), R2
	MOVD out+24(FP), R3
	VLD1.P 64(R1), [V0.S4, V1.S4, V2.S4, V3.S4]
	VLD1   (R1), [V4.S4, V5.S4, V6.S4, V7.S4]
	MOVD $16, R4
loop:
	WORD $0x4ddfc850 // LD1R {V16.4S}, [R2], #4 (x[i] in every lane)
	VLD1.P 64(R0), [V17.S4, V18.S4, V19.S4, V20.S4]
	VLD1.P 64(R0), [V21.S4, V22.S4, V23.S4, V24.S4]
	WORD $0x6e30de39 // FMUL V25.4S, V17.4S, V16.4S
	WORD $0x4e39d400 // FADD V0.4S, V0.4S, V25.4S
	WORD $0x6e30de59 // FMUL V25.4S, V18.4S, V16.4S
	WORD $0x4e39d421 // FADD V1.4S, V1.4S, V25.4S
	WORD $0x6e30de79 // FMUL V25.4S, V19.4S, V16.4S
	WORD $0x4e39d442 // FADD V2.4S, V2.4S, V25.4S
	WORD $0x6e30de99 // FMUL V25.4S, V20.4S, V16.4S
	WORD $0x4e39d463 // FADD V3.4S, V3.4S, V25.4S
	WORD $0x6e30deb9 // FMUL V25.4S, V21.4S, V16.4S
	WORD $0x4e39d484 // FADD V4.4S, V4.4S, V25.4S
	WORD $0x6e30ded9 // FMUL V25.4S, V22.4S, V16.4S
	WORD $0x4e39d4a5 // FADD V5.4S, V5.4S, V25.4S
	WORD $0x6e30def9 // FMUL V25.4S, V23.4S, V16.4S
	WORD $0x4e39d4c6 // FADD V6.4S, V6.4S, V25.4S
	WORD $0x6e30df19 // FMUL V25.4S, V24.4S, V16.4S
	WORD $0x4e39d4e7 // FADD V7.4S, V7.4S, V25.4S
	SUB  $1, R4, R4
	CBNZ R4, loop
	VEOR V26.B16, V26.B16, V26.B16
	WORD $0x4f03f61b // FMOV V27.4S, #1.0
	WORD $0x4e3af400 // FMAX V0.4S, V0.4S, V26.4S
	WORD $0x4ebbf400 // FMIN V0.4S, V0.4S, V27.4S
	WORD $0x4e3af421 // FMAX V1.4S, V1.4S, V26.4S
	WORD $0x4ebbf421 // FMIN V1.4S, V1.4S, V27.4S
	WORD $0x4e3af442 // FMAX V2.4S, V2.4S, V26.4S
	WORD $0x4ebbf442 // FMIN V2.4S, V2.4S, V27.4S
	WORD $0x4e3af463 // FMAX V3.4S, V3.4S, V26.4S
	WORD $0x4ebbf463 // FMIN V3.4S, V3.4S, V27.4S
	WORD $0x4e3af484 // FMAX V4.4S, V4.4S, V26.4S
	WORD $0x4ebbf484 // FMIN V4.4S, V4.4S, V27.4S
	WORD $0x4e3af4a5 // FMAX V5.4S, V5.4S, V26.4S
	WORD $0x4ebbf4a5 // FMIN V5.4S, V5.4S, V27.4S
	WORD $0x4e3af4c6 // FMAX V6.4S, V6.4S, V26.4S
	WORD $0x4ebbf4c6 // FMIN V6.4S, V6.4S, V27.4S
	WORD $0x4e3af4e7 // FMAX V7.4S, V7.4S, V26.4S
	WORD $0x4ebbf4e7 // FMIN V7.4S, V7.4S, V27.4S
	VST1.P [V0.S4, V1.S4, V2.S4, V3.S4], 64(R3)
	VST1   [V4.S4, V5.S4, V6.S4, V7.S4], (R3)
	RET

DATA chunkMask0<>+0(SB)/4, $1
DATA chunkMask0<>+4(SB)/4, $2
DATA chunkMask0<>+8(SB)/4, $4
DATA chunkMask0<>+12(SB)/4, $8
GLOBL chunkMask0<>(SB), RODATA|NOPTR, $16

DATA chunkMask1<>+0(SB)/4, $16
DATA chunkMask1<>+4(SB)/4, $32
DATA chunkMask1<>+8(SB)/4, $64
DATA chunkMask1<>+12(SB)/4, $128
GLOBL chunkMask1<>(SB), RODATA|NOPTR, $16

// func nonzeroChunksNEON(ft *[1024]uint8, out *uint16) int
// Branch-free NEON scan for non-zero 4-byte groups in ft (256 groups).
// Processes 64 bytes (16 groups) per iteration. out needs 8 entries of slack.
TEXT ·nonzeroChunksNEON(SB), NOSPLIT|NOFRAME, $0-24
	MOVD ft+0(FP), R0
	MOVD out+8(FP), R1
	MOVD R1, R10
	MOVD $·l1NzIdx(SB), R12
	MOVD $·l1Pop8(SB), R13
	MOVD $chunkMask0<>(SB), R8
	VLD1 (R8), [V30.S4]
	MOVD $chunkMask1<>(SB), R8
	VLD1 (R8), [V31.S4]
	VEOR V29.B16, V29.B16, V29.B16 // chunk base = 0
	MOVD $8, R8
	VDUP R8, V28.H8                // step = 8
	MOVD $16, R14                  // 16 * 64 bytes = 1024 bytes
loop_nz:
	VLD1.P 64(R0), [V0.S4, V1.S4, V2.S4, V3.S4]
	WORD $0x6ea08c00 // CMTST V0.4S, V0.4S, V0.4S
	WORD $0x6ea18c21 // CMTST V1.4S, V1.4S, V1.4S
	WORD $0x6ea28c42 // CMTST V2.4S, V2.4S, V2.4S
	WORD $0x6ea38c63 // CMTST V3.4S, V3.4S, V3.4S
	VAND V30.B16, V0.B16, V0.B16
	VAND V31.B16, V1.B16, V1.B16
	VORR V1.B16, V0.B16, V0.B16
	VAND V30.B16, V2.B16, V2.B16
	VAND V31.B16, V3.B16, V3.B16
	VORR V3.B16, V2.B16, V2.B16
	WORD $0x4eb1b800 // ADDV S0, V0.4S
	WORD $0x4eb1b842 // ADDV S2, V2.4S
	FMOVS F0, R3
	FMOVS F2, R4
	ADD  R3<<4, R12, R5
	VLD1 (R5), [V4.H8]
	VADD V29.H8, V4.H8, V4.H8
	VST1 [V4.H8], (R1)
	MOVBU (R13)(R3), R6
	ADD  R6<<1, R1, R1
	VADD V28.H8, V29.H8, V29.H8
	ADD  R4<<4, R12, R5
	VLD1 (R5), [V5.H8]
	VADD V29.H8, V5.H8, V5.H8
	VST1 [V5.H8], (R1)
	MOVBU (R13)(R4), R7
	ADD  R7<<1, R1, R1
	VADD V28.H8, V29.H8, V29.H8
	SUB  $1, R14, R14
	CBNZ R14, loop_nz
	SUB  R10, R1, R1
	LSR  $1, R1, R1
	MOVD R1, ret+16(FP)
	RET

// func sfSecondLayerNEON(act *uint8, wt *int8, out *[32]int32)
// Computes all 16 groups of 4 inputs (64 bytes) * 32 outputs using indexed SDOT.
TEXT ·sfSecondLayerNEON(SB), NOSPLIT|NOFRAME, $0-24
	MOVD act+0(FP), R0
	MOVD wt+8(FP), R1
	MOVD out+16(FP), R2
	VEOR V0.B16, V0.B16, V0.B16
	VEOR V1.B16, V1.B16, V1.B16
	VEOR V2.B16, V2.B16, V2.B16
	VEOR V3.B16, V3.B16, V3.B16
	VEOR V4.B16, V4.B16, V4.B16
	VEOR V5.B16, V5.B16, V5.B16
	VEOR V6.B16, V6.B16, V6.B16
	VEOR V7.B16, V7.B16, V7.B16
	VLD1 (R0), [V8.B16, V9.B16, V10.B16, V11.B16]
	MOVD $4, R3
loop_l2:
	VLD1.P 64(R1), [V16.B16, V17.B16, V18.B16, V19.B16]
	VLD1.P 64(R1), [V20.B16, V21.B16, V22.B16, V23.B16]
	WORD $0x4f88e200 // SDOT V0.4S, V16.16B, V8.4B[0]
	WORD $0x4f88e221 // SDOT V1.4S, V17.16B, V8.4B[0]
	WORD $0x4f88e242 // SDOT V2.4S, V18.16B, V8.4B[0]
	WORD $0x4f88e263 // SDOT V3.4S, V19.16B, V8.4B[0]
	WORD $0x4f88e284 // SDOT V4.4S, V20.16B, V8.4B[0]
	WORD $0x4f88e2a5 // SDOT V5.4S, V21.16B, V8.4B[0]
	WORD $0x4f88e2c6 // SDOT V6.4S, V22.16B, V8.4B[0]
	WORD $0x4f88e2e7 // SDOT V7.4S, V23.16B, V8.4B[0]

	VLD1.P 64(R1), [V24.B16, V25.B16, V26.B16, V27.B16]
	VLD1.P 64(R1), [V28.B16, V29.B16, V30.B16, V31.B16]
	WORD $0x4fa8e300 // SDOT V0.4S, V24.16B, V8.4B[1]
	WORD $0x4fa8e321 // SDOT V1.4S, V25.16B, V8.4B[1]
	WORD $0x4fa8e342 // SDOT V2.4S, V26.16B, V8.4B[1]
	WORD $0x4fa8e363 // SDOT V3.4S, V27.16B, V8.4B[1]
	WORD $0x4fa8e384 // SDOT V4.4S, V28.16B, V8.4B[1]
	WORD $0x4fa8e3a5 // SDOT V5.4S, V29.16B, V8.4B[1]
	WORD $0x4fa8e3c6 // SDOT V6.4S, V30.16B, V8.4B[1]
	WORD $0x4fa8e3e7 // SDOT V7.4S, V31.16B, V8.4B[1]

	VLD1.P 64(R1), [V16.B16, V17.B16, V18.B16, V19.B16]
	VLD1.P 64(R1), [V20.B16, V21.B16, V22.B16, V23.B16]
	WORD $0x4f88ea00 // SDOT V0.4S, V16.16B, V8.4B[2]
	WORD $0x4f88ea21 // SDOT V1.4S, V17.16B, V8.4B[2]
	WORD $0x4f88ea42 // SDOT V2.4S, V18.16B, V8.4B[2]
	WORD $0x4f88ea63 // SDOT V3.4S, V19.16B, V8.4B[2]
	WORD $0x4f88ea84 // SDOT V4.4S, V20.16B, V8.4B[2]
	WORD $0x4f88eaa5 // SDOT V5.4S, V21.16B, V8.4B[2]
	WORD $0x4f88eac6 // SDOT V6.4S, V22.16B, V8.4B[2]
	WORD $0x4f88eae7 // SDOT V7.4S, V23.16B, V8.4B[2]

	VLD1.P 64(R1), [V24.B16, V25.B16, V26.B16, V27.B16]
	VLD1.P 64(R1), [V28.B16, V29.B16, V30.B16, V31.B16]
	WORD $0x4fa8eb00 // SDOT V0.4S, V24.16B, V8.4B[3]
	WORD $0x4fa8eb21 // SDOT V1.4S, V25.16B, V8.4B[3]
	WORD $0x4fa8eb42 // SDOT V2.4S, V26.16B, V8.4B[3]
	WORD $0x4fa8eb63 // SDOT V3.4S, V27.16B, V8.4B[3]
	WORD $0x4fa8eb84 // SDOT V4.4S, V28.16B, V8.4B[3]
	WORD $0x4fa8eba5 // SDOT V5.4S, V29.16B, V8.4B[3]
	WORD $0x4fa8ebc6 // SDOT V6.4S, V30.16B, V8.4B[3]
	WORD $0x4fa8ebe7 // SDOT V7.4S, V31.16B, V8.4B[3]

	VORR V9.B16, V9.B16, V8.B16
	VORR V10.B16, V10.B16, V9.B16
	VORR V11.B16, V11.B16, V10.B16
	SUB  $1, R3, R3
	CBNZ R3, loop_l2

	VST1.P [V0.S4, V1.S4, V2.S4, V3.S4], 64(R2)
	VST1   [V4.S4, V5.S4, V6.S4, V7.S4], (R2)
	RET
