from __future__ import annotations

import subprocess
from dataclasses import dataclass


class CommandError(RuntimeError):
    pass


@dataclass(frozen=True)
class CommandResult:
    code: int
    stdout: str
    stderr: str


class SubprocessRunner:
    def run(self, args: list[str], timeout: float = 120) -> CommandResult:
        completed = subprocess.run(
            args,
            check=False,
            capture_output=True,
            text=True,
            timeout=timeout,
        )
        return CommandResult(completed.returncode, completed.stdout, completed.stderr)


def sh_quote(value: str) -> str:
    return "'" + value.replace("'", "'\\''") + "'"
