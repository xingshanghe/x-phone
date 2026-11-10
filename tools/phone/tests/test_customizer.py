from __future__ import annotations

from pathlib import Path

from phone.customizer.pipeline import apply_profile, report_payload
from phone.profile import load_profile

ROOT = Path(__file__).resolve().parents[3]


class MemoryAdb:
    def __init__(self, props: dict[str, str] | None = None, writable: bool = True) -> None:
        self.props = dict(props or {})
        self.writable = writable
        self.size = ""
        self.density = ""
        self.settings = {"http_proxy": "null"}
        self.nproc = "9"
        self.mem_kb = str(8192 * 1024)
        self.commands: list[str] = []

    def getprop(self, key: str) -> str:
        return self.props.get(key, "")

    def try_setprop(self, key: str, value: str) -> None:
        if not self.writable:
            return
        self.props[key] = value

    def shell(self, script: str) -> str:
        self.commands.append(script)
        if script.startswith("wm size "):
            self.size = script.split(" ", 2)[2]
            return ""
        if script == "wm size":
            return f"Override size: {self.size}\n"
        if script.startswith("wm density "):
            self.density = script.split(" ", 2)[2]
            return ""
        if script == "wm density":
            return f"Physical density: {self.density}\n"
        if script.startswith("settings put global http_proxy "):
            self.settings["http_proxy"] = script.split(" ", 4)[4]
            return ""
        if script == "settings delete global http_proxy":
            self.settings["http_proxy"] = "null"
            return ""
        if script == "settings get global http_proxy":
            return self.settings["http_proxy"] + "\n"
        if script.startswith("settings put system system_locales "):
            return ""
        if script == "nproc":
            return self.nproc + "\n"
        if "MemTotal" in script:
            return self.mem_kb + "\n"
        return ""


def test_apply_profile_replays_identity_display_locale_proxy_and_hardware():
    profile = load_profile(ROOT / "profiles/pixel_8.json")
    adb = MemoryAdb(writable=True)
    steps = apply_profile(adb, profile, "10.0.0.8:8080")
    report = report_payload(steps)
    assert report["ok"] is True
    assert [step["name"] for step in report["steps"]] == [
        "identity",
        "display",
        "locale",
        "proxy",
        "hardware",
    ]
    assert adb.getprop("ro.product.model") == "Pixel 8"
    assert adb.getprop("ro.build.fingerprint") == profile.fingerprint
    assert adb.size == "1080x2400"
    assert adb.density == "420"
    assert adb.getprop("persist.sys.locale") == "en-US"
    assert adb.getprop("persist.sys.timezone") == "America/Los_Angeles"
    assert adb.settings["http_proxy"] == "10.0.0.8:8080"
    assert adb.getprop("ro.hardware.egl") == "Mali-G715"
    assert adb.getprop("persist.xphone.touch_points") == "10"
    assert report["gaps"] == ["baseband", "play_integrity", "camera", "gps"]


def test_apply_stops_when_identity_cannot_be_set():
    profile = load_profile(ROOT / "profiles/pixel_8.json")
    adb = MemoryAdb(writable=False)
    steps = apply_profile(adb, profile, "")
    assert len(steps) == 1
    assert steps[0].name == "identity"
    assert steps[0].ok is False


def test_second_apply_keeps_matching_values():
    profile = load_profile(ROOT / "profiles/samsung_s23.json")
    adb = MemoryAdb()
    adb.nproc = "8"
    assert report_payload(apply_profile(adb, profile, ""))["ok"] is True
    assert report_payload(apply_profile(adb, profile, ""))["ok"] is True
    assert adb.getprop("ro.product.model") == "SM-S911B"
