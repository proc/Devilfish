//go:build amd64

#include "textflag.h"

// prefetchRows prefetches the first `lines` cache lines (64 bytes
// each) of every weight row listed in rows[:n]; row k starts at
// base + k<<shift. It has no other effect.
//
// func prefetchRows(base unsafe.Pointer, rows *int32, n int, shift uint, lines int)
TEXT ·prefetchRows(SB), NOSPLIT, $0-40
	MOVQ  base+0(FP), DX
	MOVQ  rows+8(FP), SI
	MOVQ  n+16(FP), R10
	MOVQ  shift+24(FP), CX
	MOVQ  lines+32(FP), R11
	TESTQ R10, R10
	JZ    done
loop:
	MOVL (SI), AX
	SHLQ CX, AX
	ADDQ DX, AX
	MOVQ R11, R8
inner:
	PREFETCHT0 (AX)
	ADDQ $64, AX
	DECQ R8
	JNZ  inner
	ADDQ $4, SI
	DECQ R10
	JNZ  loop
done:
	RET
