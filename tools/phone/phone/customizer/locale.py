from __future__ import annotations

from phone.customizer.identity import StepResult, _ensure_prop
from phone.profile import DeviceProfile


def apply_locale(adb, profile: DeviceProfile) -> StepResult:
    locale_ok = _ensure_prop(adb, "persist.sys.locale", profile.locale)
    adb.shell(f"settings put system system_locales {profile.locale}")
    zone_ok = _ensure_prop(adb, "persist.sys.timezone", profile.timezone)
    if locale_ok and zone_ok:
        return StepResult("locale", True, f"{profile.locale} {profile.timezone}")
    return StepResult(
        "locale",
        False,
        f"locale={adb.getprop('persist.sys.locale')!r} tz={adb.getprop('persist.sys.timezone')!r}",
    )
