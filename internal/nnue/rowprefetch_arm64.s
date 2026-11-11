//go:build arm64

#include "textflag.h"

// See rowprefetch_asm.go.
//
// func prefetchRows(base unsafe.Pointer, rows *int32, n int, shift uint, lines int)
TEXT ·prefetchRows(SB), NOSPLIT|NOFRAME, $0-40
	MOVD base+0(FP), R0
	MOVD rows+8(FP), R1
	MOVD n+16(FP), R2
	MOVD shift+24(FP), R3
	MOVD lines+32(FP), R4
	CBZ  R2, done
loop:
	MOVWU.P 4(R1), R5
	LSL  R3, R5, R5
	ADD  R0, R5, R5
	MOVD R4, R6
inner:
	PRFM (R5), PLDL1KEEP
	ADD  $64, R5, R5
	SUB  $1, R6, R6
	CBNZ R6, inner
	SUB  $1, R2, R2
	CBNZ R2, loop
done:
	RET
