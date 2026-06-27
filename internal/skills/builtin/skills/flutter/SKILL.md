# Flutter

Use this skill for Flutter app development, testing, and building.

## Workflow

1. Start with `list_files` and `search_text` to locate `lib/main.dart`, `pubspec.yaml`, and test files.
2. Use `pub_get` to resolve dependencies after cloning or changing `pubspec.yaml`.
3. Use `flutter_run` to run the app on a connected device or emulator. Accepts `device`, `route`, `debug`, and `verbose`.
4. Use `flutter_build` to produce release artifacts. Accepts `targets` (apk, appbundle, ios, web, etc.), `release_mode`, and `device_id`.
5. Use `flutter_test` to run unit and widget tests. Accepts `path` and `tags`.

## Tips

- Common `targets` for `flutter_build`: `apk`, `appbundle`, `ios`, `ios-simulator`, `web`, `windows`, `macos`, `linux`.
- Use `web_renderer: "html"` or `web_renderer: "canvaskit"` for web builds.
- Use `release_mode: "profile"` for performance testing.
- Run `dart_analyze` before committing to catch issues early.

## Safety

- `flutter_run` launches the app. Review the code before running on a device.
- `flutter_build` produces distributable artifacts. Verify the output path and contents.
- Network operations (`pub_get`, `pub_upgrade`) respect the approval policy.
