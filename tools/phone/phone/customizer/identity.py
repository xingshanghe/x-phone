from __future__ import annotations

from dataclasses import dataclass

from phone.customizer.props import identity_props
from phone.profile import DeviceProfile


@dataclass(frozen=True)
class StepResult:
    name: str
    ok: bool
    detail: str


KNOWN_GAPS = ("baseband", "play_integrity", "camera", "gps")


def apply_identity(adb, profile: DeviceProfile) -> StepResult:
    mismatches: list[str] = []
    for key, expected in identity_props(profile):
        if not _ensure_prop(adb, key, expected):
            mismatches.append(f"{key}={adb.getprop(key)!r}")
    if mismatches:
        return StepResult("identity", False, "; ".join(mismatches))
    return StepResult("identity", True, "matched")


def _ensure_prop(adb, key: str, expected: str) -> bool:
    if adb.getprop(key) == expected:
        return True
    adb.try_setprop(key, expected)
    return adb.getprop(key) == expected
