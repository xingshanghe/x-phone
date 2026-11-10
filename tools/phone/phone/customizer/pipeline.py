from __future__ import annotations

import json

from phone.customizer.display import apply_display
from phone.customizer.hardware import apply_hardware
from phone.customizer.identity import KNOWN_GAPS, StepResult, apply_identity
from phone.customizer.locale import apply_locale
from phone.customizer.proxy import apply_proxy
from phone.profile import DeviceProfile


def apply_profile(adb, profile: DeviceProfile, proxy: str = "") -> list[StepResult]:
    steps: list[StepResult] = []
    actions = (
        lambda: apply_identity(adb, profile),
        lambda: apply_display(adb, profile),
        lambda: apply_locale(adb, profile),
        lambda: apply_proxy(adb, proxy),
        lambda: apply_hardware(adb, profile),
    )
    for action in actions:
        result = action()
        steps.append(result)
        if not result.ok:
            return steps
    return steps


def report_payload(steps: list[StepResult]) -> dict:
    return {
        "ok": all(step.ok for step in steps) and bool(steps),
        "steps": [{"name": step.name, "ok": step.ok, "detail": step.detail} for step in steps],
        "gaps": list(KNOWN_GAPS),
    }


def report_json(steps: list[StepResult]) -> str:
    return json.dumps(report_payload(steps), separators=(",", ":"))
