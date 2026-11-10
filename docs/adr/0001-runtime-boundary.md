# ADR 0001: Android runtime boundary

## Status

Accepted

## Context

x-phone v1 is an operator console for Android cloud phones. The control plane is Go. Node tools are Python. The console is React. iOS and HarmonyOS need different runtimes and are out of scope.

Redroid runs Android 14 in a privileged Docker container on a Linux host whose kernel provides binderfs. That container cannot live inside the API process container.

Damru is public source under PolyForm Noncommercial 1.0.0. Copying its modules, native patches, device-profile data, or images into this repository would keep that license on the derived work. Commercial hosting needs a separate license from the Damru maintainers.

## Decision

- `RuntimeKind` for v1 is `android-redroid` only.
- Each Android host runs `cmd/agent`. The API server never calls Docker.
- Device customization is implemented in `tools/phone/customizer`. We read Damru's layering as a design reference and write our own code and profile files.
- Browser viewing uses a WebSocket of JPEG frames from `adb exec-out screencap -p`, with taps sent as `adb shell input tap`. The spike also checks that the `scrcpy` binary can start against a running container when the host is Linux. `scrcpy` remains the operator's local viewer; the console does not embed the scrcpy video protocol.
- Object storage (MinIO when configured, local files otherwise) holds APKs and task logs. MySQL or SQLite holds metadata. Redis, when configured, stores host heartbeat keys with a 45 second TTL. The database remains the source of truth if Redis is absent.

## Consequences

A second instance on a host is rejected while binderfs is unavailable, before any container is created. Hardware identity that depends on patching GPU libraries or intercepting syscalls is not part of this runtime. See ADR 0002 for the hardware readback we do implement.
