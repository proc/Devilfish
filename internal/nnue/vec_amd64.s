//go:build amd64

#include "textflag.h"

// x86-64 NNUE kernels: SSE2, AVX2 and AVX-512 versions of each. See vec_amd64.go for how one set is chosen, and
// vec_ref.go for the portable definitions they must match.
//
// Loads and stores are unaligned throughout: accumulators may live on the
// stack, where Go guarantees only 8-byte alignment.

// func cpuid(eaxArg, ecxArg uint32) (eax, ebx, ecx, edx uint32)
TEXT ·cpuid(SB), NOSPLIT, $0-24
	MOVL eaxArg+0(FP), AX
	MOVL ecxArg+4(FP), CX
	CPUID
	MOVL AX, eax+8(FP)
	MOVL BX, ebx+12(FP)
	MOVL CX, ecx+16(FP)
	MOVL DX, edx+20(FP)
	RET

// func xgetbv() (eax, edx uint32)
TEXT ·xgetbv(SB), NOSPLIT, $0-8
	MOVL $0, CX
	XGETBV
	MOVL AX, eax+0(FP)
	MOVL DX, edx+4(FP)
	RET

// ---------------------------------------------------------------------------
// SSE2: 128-bit registers, part of every amd64 CPU.
// ---------------------------------------------------------------------------

// func addVector256SSE2(acc *[256]int16, weights *[256]int16)
TEXT ·addVector256SSE2(SB), NOSPLIT, $0-16
	MOVQ acc+0(FP), DI
	MOVQ weights+8(FP), SI
	MOVQ $8, CX
loop:
	MOVOU 0(DI), X0
	MOVOU 16(DI), X1
	MOVOU 32(DI), X2
	MOVOU 48(DI), X3
	MOVOU 0(SI), X4
	MOVOU 16(SI), X5
	MOVOU 32(SI), X6
	MOVOU 48(SI), X7
	PADDW X4, X0
	PADDW X5, X1
	PADDW X6, X2
	PADDW X7, X3
	MOVOU X0, 0(DI)
	MOVOU X1, 16(DI)
	MOVOU X2, 32(DI)
	MOVOU X3, 48(DI)
	ADDQ $64, DI
	ADDQ $64, SI
	DECQ CX
	JNZ  loop
	RET

// func subVector256SSE2(acc *[256]int16, weights *[256]int16)
TEXT ·subVector256SSE2(SB), NOSPLIT, $0-16
	MOVQ acc+0(FP), DI
	MOVQ weights+8(FP), SI
	MOVQ $8, CX
loop:
	MOVOU 0(DI), X0
	MOVOU 16(DI), X1
	MOVOU 32(DI), X2
	MOVOU 48(DI), X3
	MOVOU 0(SI), X4
	MOVOU 16(SI), X5
	MOVOU 32(SI), X6
	MOVOU 48(SI), X7
	PSUBW X4, X0
	PSUBW X5, X1
	PSUBW X6, X2
	PSUBW X7, X3
	MOVOU X0, 0(DI)
	MOVOU X1, 16(DI)
	MOVOU X2, 32(DI)
	MOVOU X3, 48(DI)
	ADDQ $64, DI
	ADDQ $64, SI
	DECQ CX
	JNZ  loop
	RET

// func addVector1024SSE2(acc *[1024]int16, weights *int16)
TEXT ·addVector1024SSE2(SB), NOSPLIT, $0-16
	MOVQ acc+0(FP), DI
	MOVQ weights+8(FP), SI
	MOVQ $32, CX
loop:
	MOVOU 0(DI), X0
	MOVOU 16(DI), X1
	MOVOU 32(DI), X2
	MOVOU 48(DI), X3
	MOVOU 0(SI), X4
	MOVOU 16(SI), X5
	MOVOU 32(SI), X6
	MOVOU 48(SI), X7
	PADDW X4, X0
	PADDW X5, X1
	PADDW X6, X2
	PADDW X7, X3
	MOVOU X0, 0(DI)
	MOVOU X1, 16(DI)
	MOVOU X2, 32(DI)
	MOVOU X3, 48(DI)
	ADDQ $64, DI
	ADDQ $64, SI
	DECQ CX
	JNZ  loop
	RET

// func subVector1024SSE2(acc *[1024]int16, weights *int16)
TEXT ·subVector1024SSE2(SB), NOSPLIT, $0-16
	MOVQ acc+0(FP), DI
	MOVQ weights+8(FP), SI
	MOVQ $32, CX
loop:
	MOVOU 0(DI), X0
	MOVOU 16(DI), X1
	MOVOU 32(DI), X2
	MOVOU 48(DI), X3
	MOVOU 0(SI), X4
	MOVOU 16(SI), X5
	MOVOU 32(SI), X6
	MOVOU 48(SI), X7
	PSUBW X4, X0
	PSUBW X5, X1
	PSUBW X6, X2
	PSUBW X7, X3
	MOVOU X0, 0(DI)
	MOVOU X1, 16(DI)
	MOVOU X2, 32(DI)
	MOVOU X3, 48(DI)
	ADDQ $64, DI
	ADDQ $64, SI
	DECQ CX
	JNZ  loop
	RET

// func addThreatVector1024SSE2(acc *[1024]int16, weights *int8)
// Sign-extends each weight byte to a word: unpacking a register with itself
// puts the byte in both halves of the word, and an arithmetic shift right by 8
// leaves it sign-extended.
TEXT ·addThreatVector1024SSE2(SB), NOSPLIT, $0-16
	MOVQ acc+0(FP), DI
	MOVQ weights+8(FP), SI
	MOVQ $32, CX // 32 weights (64 accumulator bytes) per iteration
loop:
	MOVOU 0(SI), X4
	MOVOU 16(SI), X6
	MOVO  X4, X5
	MOVO  X6, X7
	PUNPCKLBW X4, X4
	PUNPCKHBW X5, X5
	PUNPCKLBW X6, X6
	PUNPCKHBW X7, X7
	PSRAW $8, X4
	PSRAW $8, X5
	PSRAW $8, X6
	PSRAW $8, X7
	MOVOU 0(DI), X0
	MOVOU 16(DI), X1
	MOVOU 32(DI), X2
	MOVOU 48(DI), X3
	PADDW X4, X0
	PADDW X5, X1
	PADDW X6, X2
	PADDW X7, X3
	MOVOU X0, 0(DI)
	MOVOU X1, 16(DI)
	MOVOU X2, 32(DI)
	MOVOU X3, 48(DI)
	ADDQ $64, DI
	ADDQ $32, SI
	DECQ CX
	JNZ  loop
	RET

// func subThreatVector1024SSE2(acc *[1024]int16, weights *int8)
// Sign-extends each weight byte to a word: unpacking a register with itself
// puts the byte in both halves of the word, and an arithmetic shift right by 8
// leaves it sign-extended.
TEXT ·subThreatVector1024SSE2(SB), NOSPLIT, $0-16
	MOVQ acc+0(FP), DI
	MOVQ weights+8(FP), SI
	MOVQ $32, CX // 32 weights (64 accumulator bytes) per iteration
loop:
	MOVOU 0(SI), X4
	MOVOU 16(SI), X6
	MOVO  X4, X5
	MOVO  X6, X7
	PUNPCKLBW X4, X4
	PUNPCKHBW X5, X5
	PUNPCKLBW X6, X6
	PUNPCKHBW X7, X7
	PSRAW $8, X4
	PSRAW $8, X5
	PSRAW $8, X6
	PSRAW $8, X7
	MOVOU 0(DI), X0
	MOVOU 16(DI), X1
	MOVOU 32(DI), X2
	MOVOU 48(DI), X3
	PSUBW X4, X0
	PSUBW X5, X1
	PSUBW X6, X2
	PSUBW X7, X3
	MOVOU X0, 0(DI)
	MOVOU X1, 16(DI)
	MOVOU X2, 32(DI)
	MOVOU X3, 48(DI)
	ADDQ $64, DI
	ADDQ $32, SI
	DECQ CX
	JNZ  loop
	RET

// func activateEightFTSSE2(out *uint8, pst *int16, thr *int16)
// out[i] = clamp(pst[i]+thr[i], 0, 255) * clamp(pst[i+512]+thr[i+512], 0, 255) >> 9
// for i in 0..511. Both factors are at most 255, so the product fits in an
// unsigned 16-bit word: the low half of the multiply is exact and a logical
// shift gives the result (at most 127).
TEXT ·activateEightFTSSE2(SB), NOSPLIT, $0-24
	MOVQ out+0(FP), DI
	MOVQ pst+8(FP), SI
	MOVQ thr+16(FP), DX
	PXOR    X14, X14 // 0
	PCMPEQW X15, X15
	PSRLW   $8, X15  // 255 in every word
	MOVQ $32, CX     // 16 outputs per iteration
loop:
	MOVOU 0(SI), X0
	MOVOU 16(SI), X1
	MOVOU 0(DX), X4
	MOVOU 16(DX), X5
	PADDW X4, X0
	PADDW X5, X1
	MOVOU 1024(SI), X2 // pst + 512 elements
	MOVOU 1040(SI), X3
	MOVOU 1024(DX), X6
	MOVOU 1040(DX), X7
	PADDW X6, X2
	PADDW X7, X3
	PMAXSW X14, X0
	PMAXSW X14, X1
	PMAXSW X14, X2
	PMAXSW X14, X3
	PMINSW X15, X0
	PMINSW X15, X1
	PMINSW X15, X2
	PMINSW X15, X3
	PMULLW X2, X0
	PMULLW X3, X1
	PSRLW  $9, X0
	PSRLW  $9, X1
	PACKUSWB X1, X0
	MOVOU X0, 0(DI)
	ADDQ $32, SI
	ADDQ $32, DX
	ADDQ $16, DI
	DECQ CX
	JNZ  loop
	RET

// ---------------------------------------------------------------------------
// AVX2: 256-bit registers. Every routine ends with VZEROUPPER so that later
// SSE code does not pay the AVX-to-SSE transition penalty.
// ---------------------------------------------------------------------------

// func addVector256AVX2(acc *[256]int16, weights *[256]int16)
TEXT ·addVector256AVX2(SB), NOSPLIT, $0-16
	MOVQ acc+0(FP), DI
	MOVQ weights+8(FP), SI
	MOVQ $4, CX
loop:
	VMOVDQU 0(DI), Y0
	VMOVDQU 32(DI), Y1
	VMOVDQU 64(DI), Y2
	VMOVDQU 96(DI), Y3
	VPADDW 0(SI), Y0, Y0
	VPADDW 32(SI), Y1, Y1
	VPADDW 64(SI), Y2, Y2
	VPADDW 96(SI), Y3, Y3
	VMOVDQU Y0, 0(DI)
	VMOVDQU Y1, 32(DI)
	VMOVDQU Y2, 64(DI)
	VMOVDQU Y3, 96(DI)
	ADDQ $128, DI
	ADDQ $128, SI
	DECQ CX
	JNZ  loop
	VZEROUPPER
	RET

// func subVector256AVX2(acc *[256]int16, weights *[256]int16)
TEXT ·subVector256AVX2(SB), NOSPLIT, $0-16
	MOVQ acc+0(FP), DI
	MOVQ weights+8(FP), SI
	MOVQ $4, CX
loop:
	VMOVDQU 0(DI), Y0
	VMOVDQU 32(DI), Y1
	VMOVDQU 64(DI), Y2
	VMOVDQU 96(DI), Y3
	VPSUBW 0(SI), Y0, Y0
	VPSUBW 32(SI), Y1, Y1
	VPSUBW 64(SI), Y2, Y2
	VPSUBW 96(SI), Y3, Y3
	VMOVDQU Y0, 0(DI)
	VMOVDQU Y1, 32(DI)
	VMOVDQU Y2, 64(DI)
	VMOVDQU Y3, 96(DI)
	ADDQ $128, DI
	ADDQ $128, SI
	DECQ CX
	JNZ  loop
	VZEROUPPER
	RET

// func addVector1024AVX2(acc *[1024]int16, weights *int16)
TEXT ·addVector1024AVX2(SB), NOSPLIT, $0-16
	MOVQ acc+0(FP), DI
	MOVQ weights+8(FP), SI
	MOVQ $16, CX
loop:
	VMOVDQU 0(DI), Y0
	VMOVDQU 32(DI), Y1
	VMOVDQU 64(DI), Y2
	VMOVDQU 96(DI), Y3
	VPADDW 0(SI), Y0, Y0
	VPADDW 32(SI), Y1, Y1
	VPADDW 64(SI), Y2, Y2
	VPADDW 96(SI), Y3, Y3
	VMOVDQU Y0, 0(DI)
	VMOVDQU Y1, 32(DI)
	VMOVDQU Y2, 64(DI)
	VMOVDQU Y3, 96(DI)
	ADDQ $128, DI
	ADDQ $128, SI
	DECQ CX
	JNZ  loop
	VZEROUPPER
	RET

// func subVector1024AVX2(acc *[1024]int16, weights *int16)
TEXT ·subVector1024AVX2(SB), NOSPLIT, $0-16
	MOVQ acc+0(FP), DI
	MOVQ weights+8(FP), SI
	MOVQ $16, CX
loop:
	VMOVDQU 0(DI), Y0
	VMOVDQU 32(DI), Y1
	VMOVDQU 64(DI), Y2
	VMOVDQU 96(DI), Y3
	VPSUBW 0(SI), Y0, Y0
	VPSUBW 32(SI), Y1, Y1
	VPSUBW 64(SI), Y2, Y2
	VPSUBW 96(SI), Y3, Y3
	VMOVDQU Y0, 0(DI)
	VMOVDQU Y1, 32(DI)
	VMOVDQU Y2, 64(DI)
	VMOVDQU Y3, 96(DI)
	ADDQ $128, DI
	ADDQ $128, SI
	DECQ CX
	JNZ  loop
	VZEROUPPER
	RET

// func addThreatVector1024AVX2(acc *[1024]int16, weights *int8)
TEXT ·addThreatVector1024AVX2(SB), NOSPLIT, $0-16
	MOVQ acc+0(FP), DI
	MOVQ weights+8(FP), SI
	MOVQ $16, CX // 64 weights (128 accumulator bytes) per iteration
loop:
	VPMOVSXBW 0(SI), Y4
	VPMOVSXBW 16(SI), Y5
	VPMOVSXBW 32(SI), Y6
	VPMOVSXBW 48(SI), Y7
	VMOVDQU 0(DI), Y0
	VMOVDQU 32(DI), Y1
	VMOVDQU 64(DI), Y2
	VMOVDQU 96(DI), Y3
	VPADDW Y4, Y0, Y0
	VPADDW Y5, Y1, Y1
	VPADDW Y6, Y2, Y2
	VPADDW Y7, Y3, Y3
	VMOVDQU Y0, 0(DI)
	VMOVDQU Y1, 32(DI)
	VMOVDQU Y2, 64(DI)
	VMOVDQU Y3, 96(DI)
	ADDQ $128, DI
	ADDQ $64, SI
	DECQ CX
	JNZ  loop
	VZEROUPPER
	RET

// func subThreatVector1024AVX2(acc *[1024]int16, weights *int8)
TEXT ·subThreatVector1024AVX2(SB), NOSPLIT, $0-16
	MOVQ acc+0(FP), DI
	MOVQ weights+8(FP), SI
	MOVQ $16, CX // 64 weights (128 accumulator bytes) per iteration
loop:
	VPMOVSXBW 0(SI), Y4
	VPMOVSXBW 16(SI), Y5
	VPMOVSXBW 32(SI), Y6
	VPMOVSXBW 48(SI), Y7
	VMOVDQU 0(DI), Y0
	VMOVDQU 32(DI), Y1
	VMOVDQU 64(DI), Y2
	VMOVDQU 96(DI), Y3
	VPSUBW Y4, Y0, Y0
	VPSUBW Y5, Y1, Y1
	VPSUBW Y6, Y2, Y2
	VPSUBW Y7, Y3, Y3
	VMOVDQU Y0, 0(DI)
	VMOVDQU Y1, 32(DI)
	VMOVDQU Y2, 64(DI)
	VMOVDQU Y3, 96(DI)
	ADDQ $128, DI
	ADDQ $64, SI
	DECQ CX
	JNZ  loop
	VZEROUPPER
	RET

// func activateEightFTAVX2(out *uint8, pst *int16, thr *int16)
// Same computation as activateEightFTSSE2, 32 outputs per iteration.
// VPACKUSWB packs within each 128-bit lane, giving byte groups in the order
// a.lo, b.lo, a.hi, b.hi; VPERMQ $0xD8 puts them back in sequence.
TEXT ·activateEightFTAVX2(SB), NOSPLIT, $0-24
	MOVQ out+0(FP), DI
	MOVQ pst+8(FP), SI
	MOVQ thr+16(FP), DX
	VPXOR    Y14, Y14, Y14 // 0
	VPCMPEQW Y15, Y15, Y15
	VPSRLW   $8, Y15, Y15  // 255 in every word
	MOVQ $16, CX
loop:
	VMOVDQU 0(SI), Y0
	VMOVDQU 32(SI), Y1
	VPADDW  0(DX), Y0, Y0
	VPADDW  32(DX), Y1, Y1
	VMOVDQU 1024(SI), Y2 // pst + 512 elements
	VMOVDQU 1056(SI), Y3
	VPADDW  1024(DX), Y2, Y2
	VPADDW  1056(DX), Y3, Y3
	VPMAXSW Y14, Y0, Y0
	VPMAXSW Y14, Y1, Y1
	VPMAXSW Y14, Y2, Y2
	VPMAXSW Y14, Y3, Y3
	VPMINSW Y15, Y0, Y0
	VPMINSW Y15, Y1, Y1
	VPMINSW Y15, Y2, Y2
	VPMINSW Y15, Y3, Y3
	VPMULLW Y2, Y0, Y0
	VPMULLW Y3, Y1, Y1
	VPSRLW  $9, Y0, Y0
	VPSRLW  $9, Y1, Y1
	VPACKUSWB Y1, Y0, Y0
	VPERMQ  $0xD8, Y0, Y0
	VMOVDQU Y0, 0(DI)
	ADDQ $64, SI
	ADDQ $64, DX
	ADDQ $32, DI
	DECQ CX
	JNZ  loop
	VZEROUPPER
	RET

// ---------------------------------------------------------------------------
// AVX-512 (F + BW): 512-bit registers. Every routine ends with VZEROUPPER so
// that later SSE code does not pay the AVX-to-SSE transition penalty.
// ---------------------------------------------------------------------------

// func addVector256AVX512(acc *[256]int16, weights *[256]int16)
TEXT ·addVector256AVX512(SB), NOSPLIT, $0-16
	MOVQ acc+0(FP), DI
	MOVQ weights+8(FP), SI
	MOVQ $2, CX
loop:
	VMOVDQU64 0(DI), Z0
	VMOVDQU64 64(DI), Z1
	VMOVDQU64 128(DI), Z2
	VMOVDQU64 192(DI), Z3
	VPADDW 0(SI), Z0, Z0
	VPADDW 64(SI), Z1, Z1
	VPADDW 128(SI), Z2, Z2
	VPADDW 192(SI), Z3, Z3
	VMOVDQU64 Z0, 0(DI)
	VMOVDQU64 Z1, 64(DI)
	VMOVDQU64 Z2, 128(DI)
	VMOVDQU64 Z3, 192(DI)
	ADDQ $256, DI
	ADDQ $256, SI
	DECQ CX
	JNZ  loop
	VZEROUPPER
	RET

// func subVector256AVX512(acc *[256]int16, weights *[256]int16)
TEXT ·subVector256AVX512(SB), NOSPLIT, $0-16
	MOVQ acc+0(FP), DI
	MOVQ weights+8(FP), SI
	MOVQ $2, CX
loop:
	VMOVDQU64 0(DI), Z0
	VMOVDQU64 64(DI), Z1
	VMOVDQU64 128(DI), Z2
	VMOVDQU64 192(DI), Z3
	VPSUBW 0(SI), Z0, Z0
	VPSUBW 64(SI), Z1, Z1
	VPSUBW 128(SI), Z2, Z2
	VPSUBW 192(SI), Z3, Z3
	VMOVDQU64 Z0, 0(DI)
	VMOVDQU64 Z1, 64(DI)
	VMOVDQU64 Z2, 128(DI)
	VMOVDQU64 Z3, 192(DI)
	ADDQ $256, DI
	ADDQ $256, SI
	DECQ CX
	JNZ  loop
	VZEROUPPER
	RET

// func addVector1024AVX512(acc *[1024]int16, weights *int16)
TEXT ·addVector1024AVX512(SB), NOSPLIT, $0-16
	MOVQ acc+0(FP), DI
	MOVQ weights+8(FP), SI
	MOVQ $8, CX
loop:
	VMOVDQU64 0(DI), Z0
	VMOVDQU64 64(DI), Z1
	VMOVDQU64 128(DI), Z2
	VMOVDQU64 192(DI), Z3
	VPADDW 0(SI), Z0, Z0
	VPADDW 64(SI), Z1, Z1
	VPADDW 128(SI), Z2, Z2
	VPADDW 192(SI), Z3, Z3
	VMOVDQU64 Z0, 0(DI)
	VMOVDQU64 Z1, 64(DI)
	VMOVDQU64 Z2, 128(DI)
	VMOVDQU64 Z3, 192(DI)
	ADDQ $256, DI
	ADDQ $256, SI
	DECQ CX
	JNZ  loop
	VZEROUPPER
	RET

// func subVector1024AVX512(acc *[1024]int16, weights *int16)
TEXT ·subVector1024AVX512(SB), NOSPLIT, $0-16
	MOVQ acc+0(FP), DI
	MOVQ weights+8(FP), SI
	MOVQ $8, CX
loop:
	VMOVDQU64 0(DI), Z0
	VMOVDQU64 64(DI), Z1
	VMOVDQU64 128(DI), Z2
	VMOVDQU64 192(DI), Z3
	VPSUBW 0(SI), Z0, Z0
	VPSUBW 64(SI), Z1, Z1
	VPSUBW 128(SI), Z2, Z2
	VPSUBW 192(SI), Z3, Z3
	VMOVDQU64 Z0, 0(DI)
	VMOVDQU64 Z1, 64(DI)
	VMOVDQU64 Z2, 128(DI)
	VMOVDQU64 Z3, 192(DI)
	ADDQ $256, DI
	ADDQ $256, SI
	DECQ CX
	JNZ  loop
	VZEROUPPER
	RET

// func addThreatVector1024AVX512(acc *[1024]int16, weights *int8)
TEXT ·addThreatVector1024AVX512(SB), NOSPLIT, $0-16
	MOVQ acc+0(FP), DI
	MOVQ weights+8(FP), SI
	MOVQ $8, CX // 128 weights (256 accumulator bytes) per iteration
loop:
	VPMOVSXBW 0(SI), Z4
	VPMOVSXBW 32(SI), Z5
	VPMOVSXBW 64(SI), Z6
	VPMOVSXBW 96(SI), Z7
	VMOVDQU64 0(DI), Z0
	VMOVDQU64 64(DI), Z1
	VMOVDQU64 128(DI), Z2
	VMOVDQU64 192(DI), Z3
	VPADDW Z4, Z0, Z0
	VPADDW Z5, Z1, Z1
	VPADDW Z6, Z2, Z2
	VPADDW Z7, Z3, Z3
	VMOVDQU64 Z0, 0(DI)
	VMOVDQU64 Z1, 64(DI)
	VMOVDQU64 Z2, 128(DI)
	VMOVDQU64 Z3, 192(DI)
	ADDQ $256, DI
	ADDQ $128, SI
	DECQ CX
	JNZ  loop
	VZEROUPPER
	RET

// func subThreatVector1024AVX512(acc *[1024]int16, weights *int8)
TEXT ·subThreatVector1024AVX512(SB), NOSPLIT, $0-16
	MOVQ acc+0(FP), DI
	MOVQ weights+8(FP), SI
	MOVQ $8, CX // 128 weights (256 accumulator bytes) per iteration
loop:
	VPMOVSXBW 0(SI), Z4
	VPMOVSXBW 32(SI), Z5
	VPMOVSXBW 64(SI), Z6
	VPMOVSXBW 96(SI), Z7
	VMOVDQU64 0(DI), Z0
	VMOVDQU64 64(DI), Z1
	VMOVDQU64 128(DI), Z2
	VMOVDQU64 192(DI), Z3
	VPSUBW Z4, Z0, Z0
	VPSUBW Z5, Z1, Z1
	VPSUBW Z6, Z2, Z2
	VPSUBW Z7, Z3, Z3
	VMOVDQU64 Z0, 0(DI)
	VMOVDQU64 Z1, 64(DI)
	VMOVDQU64 Z2, 128(DI)
	VMOVDQU64 Z3, 192(DI)
	ADDQ $256, DI
	ADDQ $128, SI
	DECQ CX
	JNZ  loop
	VZEROUPPER
	RET

// func activateEightFTAVX512(out *uint8, pst *int16, thr *int16)
// Same computation as activateEightFTSSE2, 64 outputs per iteration. The
// results are at most 127, so VPMOVWB's truncation to bytes is exact.
TEXT ·activateEightFTAVX512(SB), NOSPLIT, $0-24
	MOVQ out+0(FP), DI
	MOVQ pst+8(FP), SI
	MOVQ thr+16(FP), DX
	VPXORQ Z31, Z31, Z31 // 0
	MOVL $255, AX
	VPBROADCASTW AX, Z30 // 255 in every word
	MOVQ $8, CX
loop:
	VMOVDQU64 0(SI), Z0
	VMOVDQU64 64(SI), Z1
	VPADDW 0(DX), Z0, Z0
	VPADDW 64(DX), Z1, Z1
	VMOVDQU64 1024(SI), Z2 // pst + 512 elements
	VMOVDQU64 1088(SI), Z3
	VPADDW 1024(DX), Z2, Z2
	VPADDW 1088(DX), Z3, Z3
	VPMAXSW Z31, Z0, Z0
	VPMAXSW Z31, Z1, Z1
	VPMAXSW Z31, Z2, Z2
	VPMAXSW Z31, Z3, Z3
	VPMINSW Z30, Z0, Z0
	VPMINSW Z30, Z1, Z1
	VPMINSW Z30, Z2, Z2
	VPMINSW Z30, Z3, Z3
	VPMULLW Z2, Z0, Z0
	VPMULLW Z3, Z1, Z1
	VPSRLW $9, Z0, Z0
	VPSRLW $9, Z1, Z1
	VPMOVWB Z0, 0(DI)
	VPMOVWB Z1, 32(DI)
	ADDQ $128, SI
	ADDQ $128, DX
	ADDQ $64, DI
	DECQ CX
	JNZ  loop
	VZEROUPPER
	RET

// ---------------------------------------------------------------------------
// First dense layer, sparse form: out[j] = sum over entries e of
// value(e) * wt[index(e)][j], for j in 0..15. See refL1Sparse.
//
// Entries are taken two at a time (a, b). Their weight rows are interleaved
// into int16 pairs (a_j, b_j), and the values packed as one int32
// (v_a | v_b<<16) broadcast to every lane, so PMADDWD gives
// a_j*v_a + b_j*v_b for each output in a 32-bit lane. Values are at most 255
// and weights at least -128, so every product and pair sum is exact.
//
// Register use in every version: SI walks the entries, CX counts pairs, DX is
// the weight table, AX holds the packed values, R8 and R9 the row offsets.
// ---------------------------------------------------------------------------

// Loads entries a and b from SI and leaves the row offsets in R8, R9 and the
// packed value pair in AX.
#define L1_PAIR_SETUP \
	MOVL 0(SI), AX       \
	MOVL 4(SI), BX       \
	MOVL AX, R8          \
	ANDL $0xFFFF, R8     \
	SHLQ $4, R8          \
	MOVL BX, R9          \
	ANDL $0xFFFF, R9     \
	SHLQ $4, R9          \
	SHRL $16, AX         \
	SHRL $16, BX         \
	SHLL $16, BX         \
	ORL  BX, AX

// func l1SparseSSE2(nz *uint32, n int, wt *[1024][16]int8, out *[16]int32)
TEXT ·l1SparseSSE2(SB), NOSPLIT, $0-32
	MOVQ nz+0(FP), SI
	MOVQ n+8(FP), CX
	MOVQ wt+16(FP), DX
	MOVQ out+24(FP), DI
	PXOR X0, X0 // outputs 0..3
	PXOR X1, X1 // 4..7
	PXOR X2, X2 // 8..11
	PXOR X3, X3 // 12..15
	SHRQ $1, CX
	JZ   done
loop:
	L1_PAIR_SETUP
	MOVQ   AX, X6
	PSHUFL $0, X6, X6 // value pair in every dword
	MOVOU (DX)(R8*1), X8  // row a
	MOVOU (DX)(R9*1), X10 // row b
	MOVO  X8, X9
	MOVO  X10, X11
	// Sign-extend each row to two vectors of eight int16.
	PUNPCKLBW X8, X8
	PUNPCKHBW X9, X9
	PUNPCKLBW X10, X10
	PUNPCKHBW X11, X11
	PSRAW $8, X8  // a0..a7
	PSRAW $8, X9  // a8..a15
	PSRAW $8, X10 // b0..b7
	PSRAW $8, X11 // b8..b15
	// Interleave a and b word by word.
	MOVO X8, X12
	MOVO X9, X13
	PUNPCKLWL X10, X8  // a0 b0 .. a3 b3
	PUNPCKHWL X10, X12 // a4 b4 .. a7 b7
	PUNPCKLWL X11, X9  // a8 b8 .. a11 b11
	PUNPCKHWL X11, X13 // a12 b12 .. a15 b15
	PMADDWL X6, X8
	PMADDWL X6, X12
	PMADDWL X6, X9
	PMADDWL X6, X13
	PADDL X8, X0
	PADDL X12, X1
	PADDL X9, X2
	PADDL X13, X3
	ADDQ $8, SI
	DECQ CX
	JNZ  loop
done:
	MOVOU X0, 0(DI)
	MOVOU X1, 16(DI)
	MOVOU X2, 32(DI)
	MOVOU X3, 48(DI)
	RET

// func l1SparseAVX2(nz *uint32, n int, wt *[1024][16]int8, out *[16]int32)
TEXT ·l1SparseAVX2(SB), NOSPLIT, $0-32
	MOVQ nz+0(FP), SI
	MOVQ n+8(FP), CX
	MOVQ wt+16(FP), DX
	MOVQ out+24(FP), DI
	VPXOR Y0, Y0, Y0 // outputs 0..7
	VPXOR Y1, Y1, Y1 // 8..15
	SHRQ $1, CX
	JZ   done
loop:
	L1_PAIR_SETUP
	VMOVD        AX, X6
	VPBROADCASTD X6, Y6
	VMOVDQU (DX)(R8*1), X2 // row a
	VMOVDQU (DX)(R9*1), X3 // row b
	VPUNPCKLBW X3, X2, X4  // a0 b0 .. a7 b7 (bytes)
	VPUNPCKHBW X3, X2, X5  // a8 b8 .. a15 b15
	VPMOVSXBW X4, Y4
	VPMOVSXBW X5, Y5
	VPMADDWD Y6, Y4, Y4
	VPMADDWD Y6, Y5, Y5
	VPADDD Y4, Y0, Y0
	VPADDD Y5, Y1, Y1
	ADDQ $8, SI
	DECQ CX
	JNZ  loop
done:
	VMOVDQU Y0, 0(DI)
	VMOVDQU Y1, 32(DI)
	VZEROUPPER
	RET

// func l1SparseAVX512(nz *uint32, n int, wt *[1024][16]int8, out *[16]int32)
TEXT ·l1SparseAVX512(SB), NOSPLIT, $0-32
	MOVQ nz+0(FP), SI
	MOVQ n+8(FP), CX
	MOVQ wt+16(FP), DX
	MOVQ out+24(FP), DI
	VPXORQ Z0, Z0, Z0 // outputs 0..15
	SHRQ $1, CX
	JZ   done
loop:
	L1_PAIR_SETUP
	VPBROADCASTD AX, Z6
	VMOVDQU (DX)(R8*1), X2
	VMOVDQU (DX)(R9*1), X3
	VPUNPCKLBW  X3, X2, X4    // pairs for outputs 0..7
	VPUNPCKHBW  X3, X2, X5    // pairs for outputs 8..15
	VINSERTI128 $1, X5, Y4, Y4
	VPMOVSXBW   Y4, Z4
	VPMADDWD    Z6, Z4, Z4
	VPADDD      Z4, Z0, Z0
	ADDQ $8, SI
	DECQ CX
	JNZ  loop
done:
	VMOVDQU64 Z0, 0(DI)
	VZEROUPPER
	RET


// ---------------------------------------------------------------------------
// Non-zero scan for the first dense layer: for each non-zero byte of ft, in
// order, write one entry (index | value<<16) to nz, and return the count.
// Each version turns a block of bytes into a bitmask of the non-zero ones and
// then walks the set bits. BSF rather than TZCNT keeps to the baseline ISA.
// Register use: SI is ft, DI the next free entry, R10 the first entry, R11
// the input index at the start of the current block, AX the block's mask.
// ---------------------------------------------------------------------------

// func l1NonzeroSSE2(ft *[1024]uint8, nz *uint32) int
TEXT ·l1NonzeroSSE2(SB), NOSPLIT, $0-24
	MOVQ ft+0(FP), SI
	MOVQ nz+8(FP), DI
	MOVQ DI, R10
	XORQ R11, R11
	PXOR X15, X15
block:
	MOVOU    (SI)(R11*1), X0
	PCMPEQB  X15, X0        // 0xFF where the byte is zero
	PMOVMSKB X0, AX
	XORL     $0xFFFF, AX    // set bits: non-zero bytes
	TESTQ AX, AX
	JZ    next
bits:
	BSFQ    AX, BX
	LEAQ    (R11)(BX*1), R8    // input index
	MOVBLZX (SI)(R8*1), R9     // its value
	SHLL    $16, R9
	ORL     R8, R9
	MOVL    R9, (DI)
	ADDQ    $4, DI
	LEAQ    -1(AX), R9
	ANDQ    R9, AX             // clear the lowest set bit
	JNZ     bits
next:
	ADDQ $16, R11
	CMPQ R11, $1024
	JB   block
	SUBQ R10, DI
	SHRQ $2, DI
	MOVQ DI, ret+16(FP)
	RET

// func l1NonzeroAVX2(ft *[1024]uint8, nz *uint32) int
TEXT ·l1NonzeroAVX2(SB), NOSPLIT, $0-24
	MOVQ ft+0(FP), SI
	MOVQ nz+8(FP), DI
	MOVQ DI, R10
	XORQ R11, R11
	VPXOR Y15, Y15, Y15
block:
	VMOVDQU   (SI)(R11*1), Y0
	VPCMPEQB  Y15, Y0, Y0   // 0xFF where the byte is zero
	VPMOVMSKB Y0, AX
	NOTL      AX            // set bits: non-zero bytes (upper half stays clear)
	TESTQ AX, AX
	JZ    next
bits:
	BSFQ    AX, BX
	LEAQ    (R11)(BX*1), R8    // input index
	MOVBLZX (SI)(R8*1), R9     // its value
	SHLL    $16, R9
	ORL     R8, R9
	MOVL    R9, (DI)
	ADDQ    $4, DI
	LEAQ    -1(AX), R9
	ANDQ    R9, AX             // clear the lowest set bit
	JNZ     bits
next:
	ADDQ $32, R11
	CMPQ R11, $1024
	JB   block
	SUBQ R10, DI
	SHRQ $2, DI
	MOVQ DI, ret+16(FP)
	VZEROUPPER
	RET

// func l1NonzeroAVX512(ft *[1024]uint8, nz *uint32) int
TEXT ·l1NonzeroAVX512(SB), NOSPLIT, $0-24
	MOVQ ft+0(FP), SI
	MOVQ nz+8(FP), DI
	MOVQ DI, R10
	XORQ R11, R11
block:
	VMOVDQU64 (SI)(R11*1), Z0
	VPTESTMB  Z0, Z0, K1    // bit set where the byte is non-zero
	KMOVQ     K1, AX
	TESTQ AX, AX
	JZ    next
bits:
	BSFQ    AX, BX
	LEAQ    (R11)(BX*1), R8    // input index
	MOVBLZX (SI)(R8*1), R9     // its value
	SHLL    $16, R9
	ORL     R8, R9
	MOVL    R9, (DI)
	ADDQ    $4, DI
	LEAQ    -1(AX), R9
	ANDQ    R9, AX             // clear the lowest set bit
	JNZ     bits
next:
	ADDQ $64, R11
	CMPQ R11, $1024
	JB   block
	SUBQ R10, DI
	SHRQ $2, DI
	MOVQ DI, ret+16(FP)
	VZEROUPPER
	RET

// ---------------------------------------------------------------------------
// Fused accumulator update: dst = src + sum of the added weight rows - sum of
// the removed ones. Works through the accumulator one register block at a
// time (eight vector registers), applying every row to the block before
// storing it, so the 2 KB accumulator is read once and written once however
// many rows change. dst and src may be the same array. Row k of the piece
// weights starts at byte k<<11 (1024 int16); of the threat weights at k<<10
// (1024 int8). See refFusedPiece/refFusedThreat.
//
// Registers: DI dst, SI src, DX weights, R8/R9 added rows and count,
// R10/R11 removed rows and count, BX block offset in the accumulator, R12 the
// matching offset in a weight row, CX the row counter, AX the row address.
// ---------------------------------------------------------------------------

// func fusedPieceSSE2(dst, src *[1024]int16, w *int16, add *int32, nAdd int, rem *int32, nRem int)
TEXT ·fusedPieceSSE2(SB), NOSPLIT, $0-56
	MOVQ dst+0(FP), DI
	MOVQ src+8(FP), SI
	MOVQ w+16(FP), DX
	MOVQ add+24(FP), R8
	MOVQ nAdd+32(FP), R9
	MOVQ rem+40(FP), R10
	MOVQ nRem+48(FP), R11
	XORQ BX, BX  // accumulator offset of this block
	XORQ R12, R12 // weight-row offset of this block
block:
	MOVOU 0(SI)(BX*1), X0
	MOVOU 16(SI)(BX*1), X1
	MOVOU 32(SI)(BX*1), X2
	MOVOU 48(SI)(BX*1), X3
	MOVOU 64(SI)(BX*1), X4
	MOVOU 80(SI)(BX*1), X5
	MOVOU 96(SI)(BX*1), X6
	MOVOU 112(SI)(BX*1), X7
	XORQ CX, CX
adds:
	CMPQ CX, R9
	JAE  addsdone
	MOVL (R8)(CX*4), AX
	SHLQ $11, AX
	ADDQ DX, AX
	ADDQ R12, AX
	MOVOU 0(AX), X8
	PADDW X8, X0
	MOVOU 16(AX), X9
	PADDW X9, X1
	MOVOU 32(AX), X10
	PADDW X10, X2
	MOVOU 48(AX), X11
	PADDW X11, X3
	MOVOU 64(AX), X12
	PADDW X12, X4
	MOVOU 80(AX), X13
	PADDW X13, X5
	MOVOU 96(AX), X14
	PADDW X14, X6
	MOVOU 112(AX), X15
	PADDW X15, X7
	INCQ CX
	JMP  adds
addsdone:
	XORQ CX, CX
rems:
	CMPQ CX, R11
	JAE  remsdone
	MOVL (R10)(CX*4), AX
	SHLQ $11, AX
	ADDQ DX, AX
	ADDQ R12, AX
	MOVOU 0(AX), X8
	PSUBW X8, X0
	MOVOU 16(AX), X9
	PSUBW X9, X1
	MOVOU 32(AX), X10
	PSUBW X10, X2
	MOVOU 48(AX), X11
	PSUBW X11, X3
	MOVOU 64(AX), X12
	PSUBW X12, X4
	MOVOU 80(AX), X13
	PSUBW X13, X5
	MOVOU 96(AX), X14
	PSUBW X14, X6
	MOVOU 112(AX), X15
	PSUBW X15, X7
	INCQ CX
	JMP  rems
remsdone:
	MOVOU X0, 0(DI)(BX*1)
	MOVOU X1, 16(DI)(BX*1)
	MOVOU X2, 32(DI)(BX*1)
	MOVOU X3, 48(DI)(BX*1)
	MOVOU X4, 64(DI)(BX*1)
	MOVOU X5, 80(DI)(BX*1)
	MOVOU X6, 96(DI)(BX*1)
	MOVOU X7, 112(DI)(BX*1)
	ADDQ $128, BX
	ADDQ $128, R12
	CMPQ BX, $2048
	JB   block
	RET

// func fusedThreatSSE2(dst, src *[1024]int16, w *int8, add *int32, nAdd int, rem *int32, nRem int)
TEXT ·fusedThreatSSE2(SB), NOSPLIT, $0-56
	MOVQ dst+0(FP), DI
	MOVQ src+8(FP), SI
	MOVQ w+16(FP), DX
	MOVQ add+24(FP), R8
	MOVQ nAdd+32(FP), R9
	MOVQ rem+40(FP), R10
	MOVQ nRem+48(FP), R11
	XORQ BX, BX  // accumulator offset of this block
	XORQ R12, R12 // weight-row offset of this block
block:
	MOVOU 0(SI)(BX*1), X0
	MOVOU 16(SI)(BX*1), X1
	MOVOU 32(SI)(BX*1), X2
	MOVOU 48(SI)(BX*1), X3
	MOVOU 64(SI)(BX*1), X4
	MOVOU 80(SI)(BX*1), X5
	MOVOU 96(SI)(BX*1), X6
	MOVOU 112(SI)(BX*1), X7
	XORQ CX, CX
adds:
	CMPQ CX, R9
	JAE  addsdone
	MOVL (R8)(CX*4), AX
	SHLQ $10, AX
	ADDQ DX, AX
	ADDQ R12, AX
	MOVOU 0(AX), X8
	MOVO X8, X9
	PUNPCKLBW X8, X8
	PUNPCKHBW X9, X9
	PSRAW $8, X8
	PSRAW $8, X9
	PADDW X8, X0
	PADDW X9, X1
	MOVOU 16(AX), X10
	MOVO X10, X11
	PUNPCKLBW X10, X10
	PUNPCKHBW X11, X11
	PSRAW $8, X10
	PSRAW $8, X11
	PADDW X10, X2
	PADDW X11, X3
	MOVOU 32(AX), X12
	MOVO X12, X13
	PUNPCKLBW X12, X12
	PUNPCKHBW X13, X13
	PSRAW $8, X12
	PSRAW $8, X13
	PADDW X12, X4
	PADDW X13, X5
	MOVOU 48(AX), X14
	MOVO X14, X15
	PUNPCKLBW X14, X14
	PUNPCKHBW X15, X15
	PSRAW $8, X14
	PSRAW $8, X15
	PADDW X14, X6
	PADDW X15, X7
	INCQ CX
	JMP  adds
addsdone:
	XORQ CX, CX
rems:
	CMPQ CX, R11
	JAE  remsdone
	MOVL (R10)(CX*4), AX
	SHLQ $10, AX
	ADDQ DX, AX
	ADDQ R12, AX
	MOVOU 0(AX), X8
	MOVO X8, X9
	PUNPCKLBW X8, X8
	PUNPCKHBW X9, X9
	PSRAW $8, X8
	PSRAW $8, X9
	PSUBW X8, X0
	PSUBW X9, X1
	MOVOU 16(AX), X10
	MOVO X10, X11
	PUNPCKLBW X10, X10
	PUNPCKHBW X11, X11
	PSRAW $8, X10
	PSRAW $8, X11
	PSUBW X10, X2
	PSUBW X11, X3
	MOVOU 32(AX), X12
	MOVO X12, X13
	PUNPCKLBW X12, X12
	PUNPCKHBW X13, X13
	PSRAW $8, X12
	PSRAW $8, X13
	PSUBW X12, X4
	PSUBW X13, X5
	MOVOU 48(AX), X14
	MOVO X14, X15
	PUNPCKLBW X14, X14
	PUNPCKHBW X15, X15
	PSRAW $8, X14
	PSRAW $8, X15
	PSUBW X14, X6
	PSUBW X15, X7
	INCQ CX
	JMP  rems
remsdone:
	MOVOU X0, 0(DI)(BX*1)
	MOVOU X1, 16(DI)(BX*1)
	MOVOU X2, 32(DI)(BX*1)
	MOVOU X3, 48(DI)(BX*1)
	MOVOU X4, 64(DI)(BX*1)
	MOVOU X5, 80(DI)(BX*1)
	MOVOU X6, 96(DI)(BX*1)
	MOVOU X7, 112(DI)(BX*1)
	ADDQ $128, BX
	ADDQ $64, R12
	CMPQ BX, $2048
	JB   block
	RET

// func fusedPieceAVX2(dst, src *[1024]int16, w *int16, add *int32, nAdd int, rem *int32, nRem int)
TEXT ·fusedPieceAVX2(SB), NOSPLIT, $0-56
	MOVQ dst+0(FP), DI
	MOVQ src+8(FP), SI
	MOVQ w+16(FP), DX
	MOVQ add+24(FP), R8
	MOVQ nAdd+32(FP), R9
	MOVQ rem+40(FP), R10
	MOVQ nRem+48(FP), R11
	XORQ BX, BX  // accumulator offset of this block
	XORQ R12, R12 // weight-row offset of this block
block:
	VMOVDQU 0(SI)(BX*1), Y0
	VMOVDQU 32(SI)(BX*1), Y1
	VMOVDQU 64(SI)(BX*1), Y2
	VMOVDQU 96(SI)(BX*1), Y3
	VMOVDQU 128(SI)(BX*1), Y4
	VMOVDQU 160(SI)(BX*1), Y5
	VMOVDQU 192(SI)(BX*1), Y6
	VMOVDQU 224(SI)(BX*1), Y7
	XORQ CX, CX
adds:
	CMPQ CX, R9
	JAE  addsdone
	MOVL (R8)(CX*4), AX
	SHLQ $11, AX
	ADDQ DX, AX
	ADDQ R12, AX
	VPADDW 0(AX), Y0, Y0
	VPADDW 32(AX), Y1, Y1
	VPADDW 64(AX), Y2, Y2
	VPADDW 96(AX), Y3, Y3
	VPADDW 128(AX), Y4, Y4
	VPADDW 160(AX), Y5, Y5
	VPADDW 192(AX), Y6, Y6
	VPADDW 224(AX), Y7, Y7
	INCQ CX
	JMP  adds
addsdone:
	XORQ CX, CX
rems:
	CMPQ CX, R11
	JAE  remsdone
	MOVL (R10)(CX*4), AX
	SHLQ $11, AX
	ADDQ DX, AX
	ADDQ R12, AX
	VPSUBW 0(AX), Y0, Y0
	VPSUBW 32(AX), Y1, Y1
	VPSUBW 64(AX), Y2, Y2
	VPSUBW 96(AX), Y3, Y3
	VPSUBW 128(AX), Y4, Y4
	VPSUBW 160(AX), Y5, Y5
	VPSUBW 192(AX), Y6, Y6
	VPSUBW 224(AX), Y7, Y7
	INCQ CX
	JMP  rems
remsdone:
	VMOVDQU Y0, 0(DI)(BX*1)
	VMOVDQU Y1, 32(DI)(BX*1)
	VMOVDQU Y2, 64(DI)(BX*1)
	VMOVDQU Y3, 96(DI)(BX*1)
	VMOVDQU Y4, 128(DI)(BX*1)
	VMOVDQU Y5, 160(DI)(BX*1)
	VMOVDQU Y6, 192(DI)(BX*1)
	VMOVDQU Y7, 224(DI)(BX*1)
	ADDQ $256, BX
	ADDQ $256, R12
	CMPQ BX, $2048
	JB   block
	VZEROUPPER
	RET

// func fusedThreatAVX2(dst, src *[1024]int16, w *int8, add *int32, nAdd int, rem *int32, nRem int)
TEXT ·fusedThreatAVX2(SB), NOSPLIT, $0-56
	MOVQ dst+0(FP), DI
	MOVQ src+8(FP), SI
	MOVQ w+16(FP), DX
	MOVQ add+24(FP), R8
	MOVQ nAdd+32(FP), R9
	MOVQ rem+40(FP), R10
	MOVQ nRem+48(FP), R11
	XORQ BX, BX  // accumulator offset of this block
	XORQ R12, R12 // weight-row offset of this block
block:
	VMOVDQU 0(SI)(BX*1), Y0
	VMOVDQU 32(SI)(BX*1), Y1
	VMOVDQU 64(SI)(BX*1), Y2
	VMOVDQU 96(SI)(BX*1), Y3
	VMOVDQU 128(SI)(BX*1), Y4
	VMOVDQU 160(SI)(BX*1), Y5
	VMOVDQU 192(SI)(BX*1), Y6
	VMOVDQU 224(SI)(BX*1), Y7
	XORQ CX, CX
adds:
	CMPQ CX, R9
	JAE  addsdone
	MOVL (R8)(CX*4), AX
	SHLQ $10, AX
	ADDQ DX, AX
	ADDQ R12, AX
	VPMOVSXBW 0(AX), Y8
	VPMOVSXBW 16(AX), Y9
	VPMOVSXBW 32(AX), Y10
	VPMOVSXBW 48(AX), Y11
	VPMOVSXBW 64(AX), Y12
	VPMOVSXBW 80(AX), Y13
	VPMOVSXBW 96(AX), Y14
	VPMOVSXBW 112(AX), Y15
	VPADDW Y8, Y0, Y0
	VPADDW Y9, Y1, Y1
	VPADDW Y10, Y2, Y2
	VPADDW Y11, Y3, Y3
	VPADDW Y12, Y4, Y4
	VPADDW Y13, Y5, Y5
	VPADDW Y14, Y6, Y6
	VPADDW Y15, Y7, Y7
	INCQ CX
	JMP  adds
addsdone:
	XORQ CX, CX
rems:
	CMPQ CX, R11
	JAE  remsdone
	MOVL (R10)(CX*4), AX
	SHLQ $10, AX
	ADDQ DX, AX
	ADDQ R12, AX
	VPMOVSXBW 0(AX), Y8
	VPMOVSXBW 16(AX), Y9
	VPMOVSXBW 32(AX), Y10
	VPMOVSXBW 48(AX), Y11
	VPMOVSXBW 64(AX), Y12
	VPMOVSXBW 80(AX), Y13
	VPMOVSXBW 96(AX), Y14
	VPMOVSXBW 112(AX), Y15
	VPSUBW Y8, Y0, Y0
	VPSUBW Y9, Y1, Y1
	VPSUBW Y10, Y2, Y2
	VPSUBW Y11, Y3, Y3
	VPSUBW Y12, Y4, Y4
	VPSUBW Y13, Y5, Y5
	VPSUBW Y14, Y6, Y6
	VPSUBW Y15, Y7, Y7
	INCQ CX
	JMP  rems
remsdone:
	VMOVDQU Y0, 0(DI)(BX*1)
	VMOVDQU Y1, 32(DI)(BX*1)
	VMOVDQU Y2, 64(DI)(BX*1)
	VMOVDQU Y3, 96(DI)(BX*1)
	VMOVDQU Y4, 128(DI)(BX*1)
	VMOVDQU Y5, 160(DI)(BX*1)
	VMOVDQU Y6, 192(DI)(BX*1)
	VMOVDQU Y7, 224(DI)(BX*1)
	ADDQ $256, BX
	ADDQ $128, R12
	CMPQ BX, $2048
	JB   block
	VZEROUPPER
	RET

// func fusedPieceAVX512(dst, src *[1024]int16, w *int16, add *int32, nAdd int, rem *int32, nRem int)
TEXT ·fusedPieceAVX512(SB), NOSPLIT, $0-56
	MOVQ dst+0(FP), DI
	MOVQ src+8(FP), SI
	MOVQ w+16(FP), DX
	MOVQ add+24(FP), R8
	MOVQ nAdd+32(FP), R9
	MOVQ rem+40(FP), R10
	MOVQ nRem+48(FP), R11
	XORQ BX, BX  // accumulator offset of this block
	XORQ R12, R12 // weight-row offset of this block
block:
	VMOVDQU64 0(SI)(BX*1), Z0
	VMOVDQU64 64(SI)(BX*1), Z1
	VMOVDQU64 128(SI)(BX*1), Z2
	VMOVDQU64 192(SI)(BX*1), Z3
	VMOVDQU64 256(SI)(BX*1), Z4
	VMOVDQU64 320(SI)(BX*1), Z5
	VMOVDQU64 384(SI)(BX*1), Z6
	VMOVDQU64 448(SI)(BX*1), Z7
	XORQ CX, CX
adds:
	CMPQ CX, R9
	JAE  addsdone
	MOVL (R8)(CX*4), AX
	SHLQ $11, AX
	ADDQ DX, AX
	ADDQ R12, AX
	VPADDW 0(AX), Z0, Z0
	VPADDW 64(AX), Z1, Z1
	VPADDW 128(AX), Z2, Z2
	VPADDW 192(AX), Z3, Z3
	VPADDW 256(AX), Z4, Z4
	VPADDW 320(AX), Z5, Z5
	VPADDW 384(AX), Z6, Z6
	VPADDW 448(AX), Z7, Z7
	INCQ CX
	JMP  adds
addsdone:
	XORQ CX, CX
rems:
	CMPQ CX, R11
	JAE  remsdone
	MOVL (R10)(CX*4), AX
	SHLQ $11, AX
	ADDQ DX, AX
	ADDQ R12, AX
	VPSUBW 0(AX), Z0, Z0
	VPSUBW 64(AX), Z1, Z1
	VPSUBW 128(AX), Z2, Z2
	VPSUBW 192(AX), Z3, Z3
	VPSUBW 256(AX), Z4, Z4
	VPSUBW 320(AX), Z5, Z5
	VPSUBW 384(AX), Z6, Z6
	VPSUBW 448(AX), Z7, Z7
	INCQ CX
	JMP  rems
remsdone:
	VMOVDQU64 Z0, 0(DI)(BX*1)
	VMOVDQU64 Z1, 64(DI)(BX*1)
	VMOVDQU64 Z2, 128(DI)(BX*1)
	VMOVDQU64 Z3, 192(DI)(BX*1)
	VMOVDQU64 Z4, 256(DI)(BX*1)
	VMOVDQU64 Z5, 320(DI)(BX*1)
	VMOVDQU64 Z6, 384(DI)(BX*1)
	VMOVDQU64 Z7, 448(DI)(BX*1)
	ADDQ $512, BX
	ADDQ $512, R12
	CMPQ BX, $2048
	JB   block
	VZEROUPPER
	RET

// func fusedThreatAVX512(dst, src *[1024]int16, w *int8, add *int32, nAdd int, rem *int32, nRem int)
TEXT ·fusedThreatAVX512(SB), NOSPLIT, $0-56
	MOVQ dst+0(FP), DI
	MOVQ src+8(FP), SI
	MOVQ w+16(FP), DX
	MOVQ add+24(FP), R8
	MOVQ nAdd+32(FP), R9
	MOVQ rem+40(FP), R10
	MOVQ nRem+48(FP), R11
	XORQ BX, BX  // accumulator offset of this block
	XORQ R12, R12 // weight-row offset of this block
block:
	VMOVDQU64 0(SI)(BX*1), Z0
	VMOVDQU64 64(SI)(BX*1), Z1
	VMOVDQU64 128(SI)(BX*1), Z2
	VMOVDQU64 192(SI)(BX*1), Z3
	VMOVDQU64 256(SI)(BX*1), Z4
	VMOVDQU64 320(SI)(BX*1), Z5
	VMOVDQU64 384(SI)(BX*1), Z6
	VMOVDQU64 448(SI)(BX*1), Z7
	XORQ CX, CX
adds:
	CMPQ CX, R9
	JAE  addsdone
	MOVL (R8)(CX*4), AX
	SHLQ $10, AX
	ADDQ DX, AX
	ADDQ R12, AX
	VPMOVSXBW 0(AX), Z8
	VPMOVSXBW 32(AX), Z9
	VPMOVSXBW 64(AX), Z10
	VPMOVSXBW 96(AX), Z11
	VPMOVSXBW 128(AX), Z12
	VPMOVSXBW 160(AX), Z13
	VPMOVSXBW 192(AX), Z14
	VPMOVSXBW 224(AX), Z15
	VPADDW Z8, Z0, Z0
	VPADDW Z9, Z1, Z1
	VPADDW Z10, Z2, Z2
	VPADDW Z11, Z3, Z3
	VPADDW Z12, Z4, Z4
	VPADDW Z13, Z5, Z5
	VPADDW Z14, Z6, Z6
	VPADDW Z15, Z7, Z7
	INCQ CX
	JMP  adds
addsdone:
	XORQ CX, CX
rems:
	CMPQ CX, R11
	JAE  remsdone
	MOVL (R10)(CX*4), AX
	SHLQ $10, AX
	ADDQ DX, AX
	ADDQ R12, AX
	VPMOVSXBW 0(AX), Z8
	VPMOVSXBW 32(AX), Z9
	VPMOVSXBW 64(AX), Z10
	VPMOVSXBW 96(AX), Z11
	VPMOVSXBW 128(AX), Z12
	VPMOVSXBW 160(AX), Z13
	VPMOVSXBW 192(AX), Z14
	VPMOVSXBW 224(AX), Z15
	VPSUBW Z8, Z0, Z0
	VPSUBW Z9, Z1, Z1
	VPSUBW Z10, Z2, Z2
	VPSUBW Z11, Z3, Z3
	VPSUBW Z12, Z4, Z4
	VPSUBW Z13, Z5, Z5
	VPSUBW Z14, Z6, Z6
	VPSUBW Z15, Z7, Z7
	INCQ CX
	JMP  rems
remsdone:
	VMOVDQU64 Z0, 0(DI)(BX*1)
	VMOVDQU64 Z1, 64(DI)(BX*1)
	VMOVDQU64 Z2, 128(DI)(BX*1)
	VMOVDQU64 Z3, 192(DI)(BX*1)
	VMOVDQU64 Z4, 256(DI)(BX*1)
	VMOVDQU64 Z5, 320(DI)(BX*1)
	VMOVDQU64 Z6, 384(DI)(BX*1)
	VMOVDQU64 Z7, 448(DI)(BX*1)
	ADDQ $512, BX
	ADDQ $256, R12
	CMPQ BX, $2048
	JB   block
	VZEROUPPER
	RET

// ---------------------------------------------------------------------------
// Second dense layer: out[j] = clamp(bias[j] + sum_i w[i][j]*x[i], 0, 1) for
// 16 inputs and 32 outputs. One accumulator lane per output; inputs are added
// in order with a separate multiply and add (no FMA), so the result is the
// same as the scalar code's. See refL2.
// ---------------------------------------------------------------------------

// func l2LayerSSE2(w *[16][32]float32, bias *[32]float32, x *[16]float32, out *[32]float32)
TEXT ·l2LayerSSE2(SB), NOSPLIT, $0-32
	MOVQ w+0(FP), DX
	MOVQ bias+8(FP), BX
	MOVQ x+16(FP), SI
	MOVQ out+24(FP), DI
	MOVUPS 0(BX), X0
	MOVUPS 16(BX), X1
	MOVUPS 32(BX), X2
	MOVUPS 48(BX), X3
	MOVUPS 64(BX), X4
	MOVUPS 80(BX), X5
	MOVUPS 96(BX), X6
	MOVUPS 112(BX), X7
	MOVQ $16, CX
loop:
	MOVSS (SI), X8
	SHUFPS $0, X8, X8
	MOVUPS 0(DX), X9
	MULPS X8, X9
	ADDPS X9, X0
	MOVUPS 16(DX), X9
	MULPS X8, X9
	ADDPS X9, X1
	MOVUPS 32(DX), X9
	MULPS X8, X9
	ADDPS X9, X2
	MOVUPS 48(DX), X9
	MULPS X8, X9
	ADDPS X9, X3
	MOVUPS 64(DX), X9
	MULPS X8, X9
	ADDPS X9, X4
	MOVUPS 80(DX), X9
	MULPS X8, X9
	ADDPS X9, X5
	MOVUPS 96(DX), X9
	MULPS X8, X9
	ADDPS X9, X6
	MOVUPS 112(DX), X9
	MULPS X8, X9
	ADDPS X9, X7
	ADDQ $4, SI
	ADDQ $128, DX
	DECQ CX
	JNZ  loop
	XORPS X14, X14
	MOVL $0x3f800000, AX
	MOVQ AX, X15
	SHUFPS $0, X15, X15
	MAXPS X14, X0
	MINPS X15, X0
	MOVUPS X0, 0(DI)
	MAXPS X14, X1
	MINPS X15, X1
	MOVUPS X1, 16(DI)
	MAXPS X14, X2
	MINPS X15, X2
	MOVUPS X2, 32(DI)
	MAXPS X14, X3
	MINPS X15, X3
	MOVUPS X3, 48(DI)
	MAXPS X14, X4
	MINPS X15, X4
	MOVUPS X4, 64(DI)
	MAXPS X14, X5
	MINPS X15, X5
	MOVUPS X5, 80(DI)
	MAXPS X14, X6
	MINPS X15, X6
	MOVUPS X6, 96(DI)
	MAXPS X14, X7
	MINPS X15, X7
	MOVUPS X7, 112(DI)
	RET

// func l2LayerAVX2(w *[16][32]float32, bias *[32]float32, x *[16]float32, out *[32]float32)
TEXT ·l2LayerAVX2(SB), NOSPLIT, $0-32
	MOVQ w+0(FP), DX
	MOVQ bias+8(FP), BX
	MOVQ x+16(FP), SI
	MOVQ out+24(FP), DI
	VMOVUPS 0(BX), Y0
	VMOVUPS 32(BX), Y1
	VMOVUPS 64(BX), Y2
	VMOVUPS 96(BX), Y3
	MOVQ $16, CX
loop:
	VBROADCASTSS (SI), Y8
	VMULPS 0(DX), Y8, Y9
	VMULPS 32(DX), Y8, Y10
	VMULPS 64(DX), Y8, Y11
	VMULPS 96(DX), Y8, Y12
	VADDPS Y9, Y0, Y0
	VADDPS Y10, Y1, Y1
	VADDPS Y11, Y2, Y2
	VADDPS Y12, Y3, Y3
	ADDQ $4, SI
	ADDQ $128, DX
	DECQ CX
	JNZ  loop
	VXORPS Y14, Y14, Y14
	MOVL $0x3f800000, AX
	VMOVD AX, X15
	VBROADCASTSS X15, Y15
	VMAXPS Y14, Y0, Y0
	VMINPS Y15, Y0, Y0
	VMOVUPS Y0, 0(DI)
	VMAXPS Y14, Y1, Y1
	VMINPS Y15, Y1, Y1
	VMOVUPS Y1, 32(DI)
	VMAXPS Y14, Y2, Y2
	VMINPS Y15, Y2, Y2
	VMOVUPS Y2, 64(DI)
	VMAXPS Y14, Y3, Y3
	VMINPS Y15, Y3, Y3
	VMOVUPS Y3, 96(DI)
	VZEROUPPER
	RET

// func l2LayerAVX512(w *[16][32]float32, bias *[32]float32, x *[16]float32, out *[32]float32)
TEXT ·l2LayerAVX512(SB), NOSPLIT, $0-32
	MOVQ w+0(FP), DX
	MOVQ bias+8(FP), BX
	MOVQ x+16(FP), SI
	MOVQ out+24(FP), DI
	VMOVUPS 0(BX), Z0
	VMOVUPS 64(BX), Z1
	MOVQ $16, CX
loop:
	VBROADCASTSS (SI), Z8
	VMULPS 0(DX), Z8, Z9
	VMULPS 64(DX), Z8, Z10
	VADDPS Z9, Z0, Z0
	VADDPS Z10, Z1, Z1
	ADDQ $4, SI
	ADDQ $128, DX
	DECQ CX
	JNZ  loop
	VPXORD Z14, Z14, Z14
	MOVL $0x3f800000, AX
	VMOVD AX, X15
	VBROADCASTSS X15, Z15
	VMAXPS Z14, Z0, Z0
	VMINPS Z15, Z0, Z0
	VMOVUPS Z0, 0(DI)
	VMAXPS Z14, Z1, Z1
	VMINPS Z15, Z1, Z1
	VMOVUPS Z1, 64(DI)
	VZEROUPPER
	RET
