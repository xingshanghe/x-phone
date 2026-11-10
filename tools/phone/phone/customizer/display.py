from __future__ import annotations

from phone.customizer.identity import StepResult
from phone.profile import DeviceProfile


def apply_display(adb, profile: DeviceProfile) -> StepResult:
    adb.shell(f"wm size {profile.width}x{profile.height}")
    adb.shell(f"wm density {profile.dpi}")
    size = adb.shell("wm size")
    density = adb.shell("wm density")
    wanted = f"{profile.width}x{profile.height}"
    if wanted not in size or str(profile.dpi) not in density:
        return StepResult("display", False, f"size={size!r} density={density!r}")
    return StepResult("display", True, wanted)
