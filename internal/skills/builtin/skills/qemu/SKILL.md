# QEMU

Use this skill for running virtual machines with QEMU.

## Workflow

1. Use `qemu_run` to start a VM. Accepts `image` (required, path to qcow2/iso), `memory`, `smp`, `cpu`, `drive`, `net`, `nographic`, `monitor`, `serial`, `args`.
2. The tool defaults to `qemu-system-x86_64`. For other architectures, use `run_command`.

## Tips

- Common memory sizes: `2G`, `4G`.
- Use `nographic: true` for headless/CI environments.
- Use `net: "user"` for user-mode networking or `net: "tap"` for bridged.
- Use `monitor: "stdio"` for interactive monitor access.
- Use `serial: "stdio"` to redirect serial to stdio.

## Safety

- QEMU can access host devices (USB, PCI). Review VM configuration before running.
- Large disk images may take time to boot. Use appropriate timeouts.
- Avoid exposing host devices without understanding the security implications.
