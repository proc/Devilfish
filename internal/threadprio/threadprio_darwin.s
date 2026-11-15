#include "textflag.h"

TEXT libc_pthread_set_qos_class_self_np_trampoline<>(SB),NOSPLIT,$0-0
	JMP	libc_pthread_set_qos_class_self_np(SB)

GLOBL	·libc_pthread_set_qos_class_self_np_trampoline_addr(SB), RODATA, $8
DATA	·libc_pthread_set_qos_class_self_np_trampoline_addr(SB)/8, $libc_pthread_set_qos_class_self_np_trampoline<>(SB)
