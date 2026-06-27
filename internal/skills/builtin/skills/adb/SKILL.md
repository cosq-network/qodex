# ADB (Android Debug Bridge)

Use this skill for Android device interaction via ADB.

## Workflow

1. Use `adb_devices` to list connected devices.
2. Use `adb_shell` to run commands on a device. Accepts `command` (required) and optional `serial`.
3. Use `adb_push` to copy files to a device. Accepts `local` and `remote` (both required).
4. Use `adb_pull` to copy files from a device. Accepts `remote` and `local` (both required).
5. For multi-device setups, use `serial` to target a specific device.

## Tips

- Use `adb_shell` with `command: "pm list packages"` to list installed apps.
- Use `adb_shell` with `command: "logcat -d"` to dump logs.
- Use `adb_push` to deploy APKs or data files.
- Use `adb_pull` to extract screenshots or logs.
- Use `adb_devices` first to verify connections.

## Safety

- `adb_shell` executes commands on the device with the app's permissions.
- `adb_push` and `adb_pull` transfer files. Review paths before overwriting.
- Avoid `adb_shell` with destructive commands (e.g. `rm -rf /`) without double-checking.
