# ADR 0002: Device customizer

## Status

Accepted

## Context

A cloud phone should boot as one coherent device: the model, display, locale, proxy, and the hardware figures the system reports need to match the selected profile. Damru splits similar work into identity, display, hardware, and network layers. This ADR records our own module boundary and the commands that prove each layer. It does not copy upstream source.

## Decision

`tools/phone/customizer` runs steps in order and stops on the first failure. Each step is applied again after the container restarts.

| Step | Profile fields | Apply | Readback |
| --- | --- | --- | --- |
| identity | brand, manufacturer, model, device, product, fingerprint, release, sdk | container command properties, then `setprop` or `resetprop` when the running value differs | `getprop` for each `ro.product.*`, `ro.build.fingerprint`, `ro.build.version.release`, `ro.build.version.sdk` |
| display | width, height, dpi | `wm size`, `wm density`, plus `androidboot.redroid_width`, `androidboot.redroid_height`, `androidboot.redroid_dpi` at container start | `wm size` contains `WIDTHxHEIGHT`; `wm density` contains the dpi |
| locale | locale, timezone | `persist.sys.locale`, `settings put system system_locales`, `persist.sys.timezone` | `getprop persist.sys.locale`, `getprop persist.sys.timezone` |
| proxy | instance HTTP proxy | `settings put global http_proxy host:port`; delete the setting when empty | `settings get global http_proxy` |
| hardware | gpu, cpuCores, memoryMb, touchPoints | GPU name via `ro.hardware.egl`; CPU and memory via Docker `--cpus` and `--memory`; touch via `persist.xphone.touch_points` | `getprop ro.hardware.egl`; `nproc`; `MemTotal` in `/proc/meminfo` within 256 MiB; `getprop persist.xphone.touch_points` |

CPU count and memory are cgroup limits on the container, so `nproc` and `MemTotal` are the container's real limits. The GPU string is the Android property `ro.hardware.egl`. Touch points are our property `persist.xphone.touch_points`. We do not patch GLES/Vulkan binaries and we do not install syscall hooks.

These items stay recorded gaps, including after the hardware step: baseband, Play Integrity, camera, GPS. A GLES query inside an app may still show the host GPU when the image ignores `ro.hardware.egl`; the step fails if `getprop` does not match.

## Consequences

`apply-profile` exits non-zero when any readback mismatches. The instance stays out of `running` until identity, display, locale, and proxy match. The hardware step uses the same fail-closed readback. Profile JSON in `profiles/` is authored for this project.
