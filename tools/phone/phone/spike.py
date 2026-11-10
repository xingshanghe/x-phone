from __future__ import annotations

import json
import platform
import shutil
from pathlib import Path

from phone.profile import DeviceProfile, load_profile
from phone.redroid import (
    DEFAULT_IMAGE,
    allocate_port,
    binderfs_from_filesystems,
    docker_rm_args,
    docker_run_args,
    used_adb_ports,
)
from phone.shell import SubprocessRunner


def probe_environment(filesystems: str | None = None) -> dict:
    if filesystems is None:
        proc = Path("/proc/filesystems")
        filesystems = proc.read_text(encoding="utf-8") if proc.exists() else ""
    return {
        "os": platform.system().lower(),
        "docker": shutil.which("docker") is not None,
        "adb": shutil.which("adb") is not None,
        "scrcpy": shutil.which("scrcpy") is not None,
        "binderfs": binderfs_from_filesystems(filesystems),
        "image": DEFAULT_IMAGE,
    }


def spike_plan(profile: DeviceProfile) -> dict:
    first = docker_run_args("xphone-spike-a", profile, 18220)
    second = docker_run_args("xphone-spike-b", profile, 18221)
    return {
        "containers": [first, second],
        "readback": [
            "getprop sys.boot_completed",
            "getprop ro.product.model",
            "getprop ro.build.fingerprint",
            "wm size",
            "wm density",
        ],
        "scrcpy": ["scrcpy", "--serial", "127.0.0.1:18220", "--no-audio", "--no-control", "--max-fps", "5"],
        "cleanup": [docker_rm_args("xphone-spike-a"), docker_rm_args("xphone-spike-b")],
    }


def run_spike(profile_path: str, execute: bool, runner: SubprocessRunner | None = None) -> dict:
    profile = load_profile(profile_path)
    env = probe_environment()
    plan = spike_plan(profile)
    ready = env["os"] == "linux" and env["docker"] and env["binderfs"]
    payload = {"ok": ready, "environment": env, "plan": plan, "executed": False}
    if not execute:
        return payload
    if not ready:
        payload["error"] = "spike execute needs linux, docker, and binderfs"
        return payload
    proc = runner or SubprocessRunner()
    created: list[str] = []
    try:
        for args in plan["containers"]:
            result = proc.run(args, timeout=180)
            if result.code != 0:
                payload["ok"] = False
                payload["error"] = result.stderr.strip() or result.stdout.strip()
                return payload
            created.append(args[args.index("--name") + 1])
        from phone.adb import RealAdb
        from phone.lifecycle import _wait_boot

        serial = "127.0.0.1:18220"
        boot = _wait_boot(proc, serial, attempts=90)
        if not boot["ok"]:
            payload["ok"] = False
            payload["error"] = boot.get("error", "boot failed")
            return payload
        adb = RealAdb(serial, proc)
        model = adb.getprop("ro.product.model")
        booted = adb.getprop("sys.boot_completed")
        if model != profile.model or booted != "1":
            payload["ok"] = False
            payload["error"] = f"readback model={model!r} boot={booted!r}"
            return payload
        payload["executed"] = True
        payload["readback"] = {"model": model, "bootCompleted": booted}
        return payload
    finally:
        for name in created:
            proc.run(docker_rm_args(name), timeout=60)


def format_spike(payload: dict) -> str:
    return json.dumps(payload)
