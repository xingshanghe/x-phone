from __future__ import annotations

from phone.customizer.identity import StepResult, _ensure_prop
from phone.profile import DeviceProfile

_MEMORY_TOLERANCE_MB = 256


def apply_hardware(adb, profile: DeviceProfile) -> StepResult:
    gpu_ok = _ensure_prop(adb, "ro.hardware.egl", profile.gpu)
    touch_ok = _ensure_prop(adb, "persist.xphone.touch_points", str(profile.touch_points))
    cores = adb.shell("nproc").strip()
    mem_kb_text = adb.shell("awk '/MemTotal/ {print $2}' /proc/meminfo").strip()
    problems: list[str] = []
    if not gpu_ok:
        problems.append(f"gpu={adb.getprop('ro.hardware.egl')!r}")
    if cores != str(profile.cpu_cores):
        problems.append(f"cpu={cores!r}")
    try:
        mem_mb = int(mem_kb_text) // 1024
    except ValueError:
        mem_mb = -1
    if abs(mem_mb - profile.memory_mb) > _MEMORY_TOLERANCE_MB:
        problems.append(f"memoryMb={mem_mb}")
    if not touch_ok:
        problems.append(f"touch={adb.getprop('persist.xphone.touch_points')!r}")
    if problems:
        return StepResult("hardware", False, "; ".join(problems))
    return StepResult(
        "hardware",
        True,
        f"{profile.gpu} cpus={profile.cpu_cores} mem={profile.memory_mb} touch={profile.touch_points}",
    )
