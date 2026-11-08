//go:build arm64

#include "textflag.h"

// func addVectorNEON(acc *[256]int16, weights *[256]int16)
TEXT ·addVectorNEON(SB), NOSPLIT|NOFRAME, $0-16
	MOVD acc+0(FP), R0
	MOVD weights+8(FP), R1
	MOVD R0, R2

	MOVD $8, R3 // 8 iterations of 64 bytes (512 bytes / 256 int16)
loop_add:
	VLD1.P 64(R0), [V0.H8, V1.H8, V2.H8, V3.H8]
	VLD1.P 64(R1), [V4.H8, V5.H8, V6.H8, V7.H8]
	VADD   V4.H8, V0.H8, V0.H8
	VADD   V5.H8, V1.H8, V1.H8
	VADD   V6.H8, V2.H8, V2.H8
	VADD   V7.H8, V3.H8, V3.H8
	VST1.P [V0.H8, V1.H8, V2.H8, V3.H8], 64(R2)
	SUB    $1, R3
	CBNZ   R3, loop_add

	RET

// func subVectorNEON(acc *[256]int16, weights *[256]int16)
TEXT ·subVectorNEON(SB), NOSPLIT|NOFRAME, $0-16
	MOVD acc+0(FP), R0
	MOVD weights+8(FP), R1
	MOVD R0, R2

	MOVD $8, R3 // 8 iterations of 64 bytes (512 bytes / 256 int16)
loop_sub:
	VLD1.P 64(R0), [V0.H8, V1.H8, V2.H8, V3.H8]
	VLD1.P 64(R1), [V4.H8, V5.H8, V6.H8, V7.H8]
	VSUB   V4.H8, V0.H8, V0.H8
	VSUB   V5.H8, V1.H8, V1.H8
	VSUB   V6.H8, V2.H8, V2.H8
	VSUB   V7.H8, V3.H8, V3.H8
	VST1.P [V0.H8, V1.H8, V2.H8, V3.H8], 64(R2)
	SUB    $1, R3
	CBNZ   R3, loop_sub

	RET
