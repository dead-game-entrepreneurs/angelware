OUTPUT_ARCH(mips)
ENTRY(hook_update)
SECTIONS {
 . = 0x3bd000;
 .text : { *(.text.hooks) *(.text*) }
 .rodata : { *(.rodata*) }
 .data : { *(.data*) }
 . = ALIGN(16);
 .bss (NOLOAD) : { *(.bss*) *(.sbss*) *(COMMON) }
 ASSERT(. <= 0x3c0000, "OutRun shim exceeds verified padding")
 /DISCARD/ : { *(.MIPS.abiflags) *(.reginfo) *(.pdr) *(.comment) *(.gnu.attributes) *(.eh_frame*) }
}
