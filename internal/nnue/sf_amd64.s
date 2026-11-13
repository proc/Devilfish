//go:build amd64

#include "textflag.h"

// x86-64 kernels added for the Stockfish network's dense layers and for the
// branch-free non-zero scans. See sf_amd64.go for how they are chosen and
// stockfish.go / vec_ref.go for the definitions they must match.

// ---------------------------------------------------------------------------
// Non-zero groups: for each group of four bytes of ft (256 groups) with any
// non-zero byte, in order, write its index to out; return the count. A
// compare against zero and MOVMSKPS give one bit per group; the bits select
// a row of a table of positions, which is offset by the block's first group
// and stored whole. Only as many entries as the mask has bits are kept, by
// advancing the output that far, so there is no data-dependent branch.
// out needs eight entries of slack.
// ---------------------------------------------------------------------------

// func nonzeroChunksAVX2(ft *[1024]uint8, out *uint16) int
TEXT ·nonzeroChunksAVX2(SB), NOSPLIT, $0-24
	MOVQ ft+0(FP), SI
	MOVQ out+8(FP), DI
	LEAQ ·l1NzIdx(SB), R12
	XORQ R11, R11 // byte offset of the block
	XORQ CX, CX   // entries written
	VPXOR Y15, Y15, Y15
block:
	VMOVDQU   (SI)(R11*1), Y0
	VPCMPEQD  Y15, Y0, Y0
	VMOVMSKPS Y0, AX
	XORL      $0xFF, AX // set bits: groups with a non-zero byte
	MOVQ      AX, BX
	SHLQ      $4, BX
	VMOVDQU   (R12)(BX*1), X1 // positions of the set bits
	MOVQ      R11, BX
	SHRQ      $2, BX
	VMOVD     BX, X2
	VPBROADCASTW X2, X2
	VPADDW    X2, X1, X1
	VMOVDQU   X1, (DI)(CX*2)
	POPCNTL   AX, AX
	ADDQ      AX, CX
	ADDQ $32, R11
	CMPQ R11, $1024
	JB   block
	MOVQ CX, ret+16(FP)
	VZEROUPPER
	RET

// func nonzeroChunksSSE2(ft *[1024]uint8, out *uint16) int
TEXT ·nonzeroChunksSSE2(SB), NOSPLIT, $0-24
	MOVQ ft+0(FP), SI
	MOVQ out+8(FP), DI
	LEAQ ·l1ChunkIdx(SB), R12
	LEAQ ·l1Pop4(SB), R13
	XORQ R11, R11
	XORQ CX, CX
	PXOR X15, X15
block:
	MOVOU    (SI)(R11*1), X0
	PCMPEQL  X15, X0
	MOVMSKPS X0, AX
	XORL     $0xF, AX
	MOVQ     (R12)(AX*8), X1 // four positions
	MOVQ     R11, BX
	SHRQ     $2, BX
	MOVQ     BX, X2
	PSHUFLW  $0, X2, X2
	PADDW    X2, X1
	MOVQ     X1, (DI)(CX*2)
	MOVBLZX  (R13)(AX*1), AX
	ADDQ     AX, CX
	ADDQ $16, R11
	CMPQ R11, $1024
	JB   block
	MOVQ CX, ret+16(FP)
	RET

// ---------------------------------------------------------------------------
// A 32-output layer by groups of four inputs. For each listed group g, the
// four input bytes at in[4g] are repeated across a vector and multiplied into
// the group's 128 weight bytes ([output][input in group]): PMADDUBSW gives
// the two pair sums of each output as int16, PMADDWD by ones adds the pairs
// into the output's int32 lane. The inputs are at most 127 and the weights
// at least -128, so a pair sum is within +-32512 and nothing saturates.
// out receives the sums (no bias).
// ---------------------------------------------------------------------------

// func chunkDot32AVX2(idx *uint16, n int, in *uint8, wt *int8, out *[32]int32)
TEXT ·chunkDot32AVX2(SB), NOSPLIT, $0-40
	MOVQ idx+0(FP), SI
	MOVQ n+8(FP), CX
	MOVQ in+16(FP), R12
	MOVQ wt+24(FP), DX
	MOVQ out+32(FP), DI
	VPXOR Y0, Y0, Y0
	VPXOR Y1, Y1, Y1
	VPXOR Y2, Y2, Y2
	VPXOR Y3, Y3, Y3
	VPCMPEQW Y14, Y14, Y14
	VPSRLW   $15, Y14, Y14 // 1 in every word
	TESTQ CX, CX
	JZ    done
loop:
	MOVWLZX (SI), R8
	VPBROADCASTD (R12)(R8*4), Y5
	SHLQ $7, R8
	VPMADDUBSW 0(DX)(R8*1), Y5, Y6
	VPMADDUBSW 32(DX)(R8*1), Y5, Y7
	VPMADDUBSW 64(DX)(R8*1), Y5, Y8
	VPMADDUBSW 96(DX)(R8*1), Y5, Y9
	VPMADDWD Y14, Y6, Y6
	VPMADDWD Y14, Y7, Y7
	VPMADDWD Y14, Y8, Y8
	VPMADDWD Y14, Y9, Y9
	VPADDD Y6, Y0, Y0
	VPADDD Y7, Y1, Y1
	VPADDD Y8, Y2, Y2
	VPADDD Y9, Y3, Y3
	ADDQ $2, SI
	DECQ CX
	JNZ  loop
done:
	VMOVDQU Y0, 0(DI)
	VMOVDQU Y1, 32(DI)
	VMOVDQU Y2, 64(DI)
	VMOVDQU Y3, 96(DI)
	VZEROUPPER
	RET

// One quarter of a group's outputs in the SSSE3 version: weights at
// off(DX)(R8), inputs in X8, sums in acc.
#define CHUNK_SSSE3(off, acc) \
	MOVO       X8, X10            \
	MOVOU      off(DX)(R8*1), X9  \
	PMADDUBSW  X9, X10            \
	PMADDWL    X14, X10           \
	PADDL      X10, acc

// func chunkDot32SSSE3(idx *uint16, n int, in *uint8, wt *int8, out *[32]int32)
TEXT ·chunkDot32SSSE3(SB), NOSPLIT, $0-40
	MOVQ idx+0(FP), SI
	MOVQ n+8(FP), CX
	MOVQ in+16(FP), R12
	MOVQ wt+24(FP), DX
	MOVQ out+32(FP), DI
	PXOR X0, X0
	PXOR X1, X1
	PXOR X2, X2
	PXOR X3, X3
	PXOR X4, X4
	PXOR X5, X5
	PXOR X6, X6
	PXOR X7, X7
	PCMPEQW X14, X14
	PSRLW   $15, X14 // 1 in every word
	TESTQ CX, CX
	JZ    done
loop:
	MOVWLZX (SI), R8
	MOVL    (R12)(R8*4), AX
	MOVQ    AX, X8
	PSHUFL  $0, X8, X8
	SHLQ    $7, R8
	CHUNK_SSSE3(0, X0)
	CHUNK_SSSE3(16, X1)
	CHUNK_SSSE3(32, X2)
	CHUNK_SSSE3(48, X3)
	CHUNK_SSSE3(64, X4)
	CHUNK_SSSE3(80, X5)
	CHUNK_SSSE3(96, X6)
	CHUNK_SSSE3(112, X7)
	ADDQ $2, SI
	DECQ CX
	JNZ  loop
done:
	MOVOU X0, 0(DI)
	MOVOU X1, 16(DI)
	MOVOU X2, 32(DI)
	MOVOU X3, 48(DI)
	MOVOU X4, 64(DI)
	MOVOU X5, 80(DI)
	MOVOU X6, 96(DI)
	MOVOU X7, 112(DI)
	RET

// ---------------------------------------------------------------------------
// Dot product of 128 unsigned bytes (each at most 127) and 128 signed bytes,
// by the same PMADDUBSW / PMADDWD pair. The eight int32 written to out add
// up to the result.
// ---------------------------------------------------------------------------

// func dot128AVX2(a *[128]uint8, w *[128]int8, out *[8]int32)
TEXT ·dot128AVX2(SB), NOSPLIT, $0-24
	MOVQ a+0(FP), SI
	MOVQ w+8(FP), DX
	MOVQ out+16(FP), DI
	VPCMPEQW Y14, Y14, Y14
	VPSRLW   $15, Y14, Y14
	VMOVDQU 0(SI), Y0
	VMOVDQU 32(SI), Y1
	VMOVDQU 64(SI), Y2
	VMOVDQU 96(SI), Y3
	VPMADDUBSW 0(DX), Y0, Y0
	VPMADDUBSW 32(DX), Y1, Y1
	VPMADDUBSW 64(DX), Y2, Y2
	VPMADDUBSW 96(DX), Y3, Y3
	VPMADDWD Y14, Y0, Y0
	VPMADDWD Y14, Y1, Y1
	VPMADDWD Y14, Y2, Y2
	VPMADDWD Y14, Y3, Y3
	VPADDD Y1, Y0, Y0
	VPADDD Y3, Y2, Y2
	VPADDD Y2, Y0, Y0
	VMOVDQU Y0, (DI)
	VZEROUPPER
	RET

#define DOT_SSSE3(off, acc) \
	MOVOU     off(SI), X8 \
	MOVOU     off(DX), X9 \
	PMADDUBSW X9, X8      \
	PMADDWL   X14, X8     \
	PADDL     X8, acc

// func dot128SSSE3(a *[128]uint8, w *[128]int8, out *[8]int32)
TEXT ·dot128SSSE3(SB), NOSPLIT, $0-24
	MOVQ a+0(FP), SI
	MOVQ w+8(FP), DX
	MOVQ out+16(FP), DI
	PXOR X0, X0
	PXOR X1, X1
	PCMPEQW X14, X14
	PSRLW   $15, X14
	DOT_SSSE3(0, X0)
	DOT_SSSE3(16, X1)
	DOT_SSSE3(32, X0)
	DOT_SSSE3(48, X1)
	DOT_SSSE3(64, X0)
	DOT_SSSE3(80, X1)
	DOT_SSSE3(96, X0)
	DOT_SSSE3(112, X1)
	MOVOU X0, 0(DI)
	MOVOU X1, 16(DI)
	RET

// ---------------------------------------------------------------------------
// Activations of the dense layers, sixteen values per step, SSE2 (thirty-two
// values in all, so wider registers would gain nothing). For
// v = x[i] + bias[i]:
//   out[i]    = min(127, v*v >> (2*bits+7))
//   out[32+i] = clamp(v >> bits, 0, 127)
// v is first saturated to 16 bits (PACKSSDW), which changes neither result;
// PMULHW then gives v*v >> 16, and the signed-saturating pack to bytes caps
// at 127. SQSHIFT is 2*bits+7-16.
// ---------------------------------------------------------------------------

// Expects SI = x, DX = bias, DI = out.
#define SF_ACT(BITS, SQSHIFT) \
	PXOR X15, X15             \
	MOVQ $2, CX               \
actloop:                      \
	MOVOU 0(SI), X0           \
	MOVOU 16(SI), X1          \
	MOVOU 32(SI), X2          \
	MOVOU 48(SI), X3          \
	MOVOU 0(DX), X4           \
	MOVOU 16(DX), X5          \
	MOVOU 32(DX), X6          \
	MOVOU 48(DX), X7          \
	PADDL X4, X0              \
	PADDL X5, X1              \
	PADDL X6, X2              \
	PADDL X7, X3              \
	MOVO  X0, X4              \
	MOVO  X2, X5              \
	PACKSSLW X1, X4           \
	PACKSSLW X3, X5           \
	PMULHW X4, X4             \
	PMULHW X5, X5             \
	PSRLW  $SQSHIFT, X4       \
	PSRLW  $SQSHIFT, X5       \
	PACKSSWB X5, X4           \
	MOVOU X4, 0(DI)           \
	PSRAL $BITS, X0           \
	PSRAL $BITS, X1           \
	PSRAL $BITS, X2           \
	PSRAL $BITS, X3           \
	PACKSSLW X1, X0           \
	PACKSSLW X3, X2           \
	PMAXSW X15, X0            \
	PMAXSW X15, X2            \
	PACKSSWB X2, X0           \
	MOVOU X0, 32(DI)          \
	ADDQ $64, SI              \
	ADDQ $64, DX              \
	ADDQ $16, DI              \
	DECQ CX                   \
	JNZ  actloop              \
	RET

// func sfAct7SSE2(x *[32]int32, bias *[32]int32, out *uint8)
TEXT ·sfAct7SSE2(SB), NOSPLIT, $0-24
	MOVQ x+0(FP), SI
	MOVQ bias+8(FP), DX
	MOVQ out+16(FP), DI
	SF_ACT(7, 5)

// func sfAct6SSE2(x *[32]int32, bias *[32]int32, out *uint8)
TEXT ·sfAct6SSE2(SB), NOSPLIT, $0-24
	MOVQ x+0(FP), SI
	MOVQ bias+8(FP), DX
	MOVQ out+16(FP), DI
	SF_ACT(6, 3)

// func psqtApplySSE2(dst *[8]int32, table *int32, add *int32, nAdd int, rem *int32, nRem int)
// dst += the eight-value rows of table listed in add, -= those in rem.
TEXT ·psqtApplySSE2(SB), NOSPLIT, $0-48
	MOVQ dst+0(FP), DI
	MOVQ table+8(FP), DX
	MOVQ add+16(FP), R8
	MOVQ nAdd+24(FP), R9
	MOVQ rem+32(FP), R10
	MOVQ nRem+40(FP), R11
	MOVOU 0(DI), X0
	MOVOU 16(DI), X1
	TESTQ R9, R9
	JZ    addsdone
adds:
	MOVL  (R8), AX
	SHLQ  $5, AX
	MOVOU 0(DX)(AX*1), X2
	MOVOU 16(DX)(AX*1), X3
	PADDL X2, X0
	PADDL X3, X1
	ADDQ  $4, R8
	DECQ  R9
	JNZ   adds
addsdone:
	TESTQ R11, R11
	JZ    remsdone
rems:
	MOVL  (R10), AX
	SHLQ  $5, AX
	MOVOU 0(DX)(AX*1), X2
	MOVOU 16(DX)(AX*1), X3
	PSUBL X2, X0
	PSUBL X3, X1
	ADDQ  $4, R10
	DECQ  R11
	JNZ   rems
remsdone:
	MOVOU X0, 0(DI)
	MOVOU X1, 16(DI)
	RET

// ---------------------------------------------------------------------------
// l1SparseSSE2 / l1SparseAVX2 (vec_amd64.s) with the inputs given as a list
// of indices, the values read from ft. Entries are taken two at a time; an
// odd last entry is paired with itself at value zero.
// Registers as there, plus R12 = ft, R13 = n.
// ---------------------------------------------------------------------------

// Loads indices a and b from SI: row offsets in R8, R9, packed values in AX.
#define L1_IDX_PAIR \
	MOVWLZX 0(SI), R8        \
	MOVWLZX 2(SI), R9        \
	MOVBLZX (R12)(R8*1), AX  \
	MOVBLZX (R12)(R9*1), BX  \
	SHLL    $16, BX          \
	ORL     BX, AX           \
	SHLQ    $4, R8           \
	SHLQ    $4, R9

// The last, unpaired index: b is a again, with value zero.
#define L1_IDX_LAST \
	MOVWLZX 0(SI), R8        \
	MOVBLZX (R12)(R8*1), AX  \
	SHLQ    $4, R8           \
	MOVQ    R8, R9

#define L1_BODY_SSE2 \
	MOVQ   AX, X6            \
	PSHUFL $0, X6, X6        \
	MOVOU (DX)(R8*1), X8     \
	MOVOU (DX)(R9*1), X10    \
	MOVO  X8, X9             \
	MOVO  X10, X11           \
	PUNPCKLBW X8, X8         \
	PUNPCKHBW X9, X9         \
	PUNPCKLBW X10, X10       \
	PUNPCKHBW X11, X11       \
	PSRAW $8, X8             \
	PSRAW $8, X9             \
	PSRAW $8, X10            \
	PSRAW $8, X11            \
	MOVO X8, X12             \
	MOVO X9, X13             \
	PUNPCKLWL X10, X8        \
	PUNPCKHWL X10, X12       \
	PUNPCKLWL X11, X9        \
	PUNPCKHWL X11, X13       \
	PMADDWL X6, X8           \
	PMADDWL X6, X12          \
	PMADDWL X6, X9           \
	PMADDWL X6, X13          \
	PADDL X8, X0             \
	PADDL X12, X1            \
	PADDL X9, X2             \
	PADDL X13, X3

// func l1SparseIdxSSE2(idx *uint16, n int, ft *uint8, wt *[1024][16]int8, out *[16]int32)
TEXT ·l1SparseIdxSSE2(SB), NOSPLIT, $0-40
	MOVQ idx+0(FP), SI
	MOVQ n+8(FP), R13
	MOVQ ft+16(FP), R12
	MOVQ wt+24(FP), DX
	MOVQ out+32(FP), DI
	PXOR X0, X0
	PXOR X1, X1
	PXOR X2, X2
	PXOR X3, X3
	MOVQ R13, CX
	SHRQ $1, CX
	JZ   last
loop:
	L1_IDX_PAIR
	L1_BODY_SSE2
	ADDQ $4, SI
	DECQ CX
	JNZ  loop
last:
	TESTQ $1, R13
	JZ    done
	L1_IDX_LAST
	L1_BODY_SSE2
done:
	MOVOU X0, 0(DI)
	MOVOU X1, 16(DI)
	MOVOU X2, 32(DI)
	MOVOU X3, 48(DI)
	RET

#define L1_BODY_AVX2 \
	VMOVD        AX, X6         \
	VPBROADCASTD X6, Y6         \
	VMOVDQU (DX)(R8*1), X2      \
	VMOVDQU (DX)(R9*1), X3      \
	VPUNPCKLBW X3, X2, X4       \
	VPUNPCKHBW X3, X2, X5       \
	VPMOVSXBW X4, Y4            \
	VPMOVSXBW X5, Y5            \
	VPMADDWD Y6, Y4, Y4         \
	VPMADDWD Y6, Y5, Y5         \
	VPADDD Y4, Y0, Y0           \
	VPADDD Y5, Y1, Y1

// func l1SparseIdxAVX2(idx *uint16, n int, ft *uint8, wt *[1024][16]int8, out *[16]int32)
TEXT ·l1SparseIdxAVX2(SB), NOSPLIT, $0-40
	MOVQ idx+0(FP), SI
	MOVQ n+8(FP), R13
	MOVQ ft+16(FP), R12
	MOVQ wt+24(FP), DX
	MOVQ out+32(FP), DI
	VPXOR Y0, Y0, Y0
	VPXOR Y1, Y1, Y1
	MOVQ R13, CX
	SHRQ $1, CX
	JZ   last
loop:
	L1_IDX_PAIR
	L1_BODY_AVX2
	ADDQ $4, SI
	DECQ CX
	JNZ  loop
last:
	TESTQ $1, R13
	JZ    done
	L1_IDX_LAST
	L1_BODY_AVX2
done:
	VMOVDQU Y0, 0(DI)
	VMOVDQU Y1, 32(DI)
	VZEROUPPER
	RET
