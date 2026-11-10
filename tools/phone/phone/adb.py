from __future__ import annotations

from phone.profile import DeviceProfile
from phone.shell import CommandError, SubprocessRunner, sh_quote


class RealAdb:
    def __init__(self, serial: str, runner: SubprocessRunner | None = None) -> None:
        self.serial = serial
        self.runner = runner or SubprocessRunner()

    def getprop(self, key: str) -> str:
        return self.shell(f"getprop {sh_quote(key)}").strip()

    def try_setprop(self, key: str, value: str) -> None:
        quoted_key = sh_quote(key)
        quoted_value = sh_quote(value)
        script = (
            f"if command -v resetprop >/dev/null 2>&1; then "
            f"resetprop {quoted_key} {quoted_value}; "
            f"else setprop {quoted_key} {quoted_value}; fi"
        )
        self.shell(script)

    def shell(self, script: str) -> str:
        result = self.runner.run(["adb", "-s", self.serial, "shell", script])
        if result.code != 0:
            detail = result.stderr.strip() or result.stdout.strip()
            raise CommandError(detail or f"adb shell failed: {script}")
        return result.stdout
