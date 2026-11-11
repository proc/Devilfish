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

	// Clamp right to [min, 255]
	VSMIN  V30.H8, V4.H8, V4.H8
	VSMIN  V30.H8, V5.H8, V5.H8

	// Multiply left * right -> int32
	VSMULL  V0.H4, V4.H4, V8.S4
	VSMULL2 V0.H8, V4.H8, V9.S4
	VSMULL  V1.H4, V5.H4, V10.S4
	VSMULL2 V1.H8, V5.H8, V11.S4

	// Right shift by 9
	VSSHR  $9, V8.S4, V8.S4
	VSSHR  $9, V9.S4, V9.S4
	VSSHR  $9, V10.S4, V10.S4
	VSSHR  $9, V11.S4, V11.S4

	// Saturate clamp to [0, 255] and narrow to unsigned 16-bit
	VSQXTUN  V8.S4, V12.H4
	VSQXTUN2 V9.S4, V12.H8
	VSQXTUN  V10.S4, V13.H4
	VSQXTUN2 V11.S4, V13.H8

	// Saturate narrow to unsigned 8-bit
	VUQXTN   V12.H8, V14.B8
	VUQXTN2  V13.H8, V14.B16

	// Store 16 bytes
	VST1.P [V14.B16], 16(R0)

	SUB    $1, R6
	CBNZ   R6, loop_act

	RET
