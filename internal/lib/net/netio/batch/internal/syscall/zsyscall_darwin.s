#include "textflag.h"

TEXT libc_sendmsg_x_trampoline<>(SB),NOSPLIT,$0-0
	JMP	libc_sendmsg_x(SB)

GLOBL	·libc_sendmsg_x_trampoline_addr(SB), RODATA, $8
DATA	·libc_sendmsg_x_trampoline_addr(SB)/8, $libc_sendmsg_x_trampoline<>(SB)

TEXT libc_recvmsg_x_trampoline<>(SB),NOSPLIT,$0-0
	JMP	libc_recvmsg_x(SB)

GLOBL	·libc_recvmsg_x_trampoline_addr(SB), RODATA, $8
DATA	·libc_recvmsg_x_trampoline_addr(SB)/8, $libc_recvmsg_x_trampoline<>(SB)
