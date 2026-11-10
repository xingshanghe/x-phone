from __future__ import annotations

from phone.customizer.identity import StepResult


def apply_proxy(adb, proxy: str) -> StepResult:
    wanted = (proxy or "").strip()
    if wanted:
        adb.shell(f"settings put global http_proxy {wanted}")
    else:
        adb.shell("settings delete global http_proxy")
    current = adb.shell("settings get global http_proxy").strip()
    if wanted and current == wanted:
        return StepResult("proxy", True, wanted)
    if not wanted and current in ("", "null"):
        return StepResult("proxy", True, "cleared")
    return StepResult("proxy", False, f"got {current!r}")
