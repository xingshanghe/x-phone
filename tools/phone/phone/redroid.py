from __future__ import annotations

import re

from phone.customizer.props import boot_properties
from phone.profile import DeviceProfile

_PORT_RE = re.compile(r":(\d+)->5555")
DEFAULT_IMAGE = "redroid/redroid:14.0.0-latest"


def used_adb_ports(docker_ps: str) -> set[int]:
    return {int(match) for match in _PORT_RE.findall(docker_ps)}


ADB_PORT_START = 18120
ADB_PORT_END = 18219


def allocate_port(used: set[int], start: int = ADB_PORT_START, end: int = ADB_PORT_END) -> int:
    for port in range(start, end + 1):
        if port not in used:
            return port
    raise RuntimeError(f"no free adb port in {start}-{end}")


def docker_run_args(
    name: str,
    profile: DeviceProfile,
    adb_port: int,
    image: str = DEFAULT_IMAGE,
) -> list[str]:
    return [
        "docker",
        "run",
        "-d",
        "--privileged",
        "--name",
        name,
        "-p",
        f"{adb_port}:5555",
        "--cpus",
        str(profile.cpu_cores),
        "--memory",
        f"{profile.memory_mb}m",
        image,
        *boot_properties(profile),
    ]


def docker_rm_args(name: str) -> list[str]:
    return ["docker", "rm", "-f", name]


def binderfs_from_filesystems(text: str) -> bool:
    for line in text.splitlines():
        fields = line.split()
        if not fields:
            continue
        name = fields[-1]
        if name in {"binder", "binderfs"}:
            return True
    return False
