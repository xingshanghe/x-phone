from __future__ import annotations

import json
import time

from phone.adb import RealAdb
from phone.customizer.pipeline import apply_profile, report_payload
from phone.profile import DeviceProfile, load_profile
from phone.redroid import allocate_port, docker_rm_args, docker_run_args, used_adb_ports
from phone.shell import CommandError, SubprocessRunner


def _emit(payload: dict) -> int:
    print(json.dumps(payload), flush=True)
    return 0 if payload.get("ok") else 1


def bring_up(
    name: str,
    profile_path: str,
    proxy: str = "",
    image: str | None = None,
    runner: SubprocessRunner | None = None,
) -> dict:
    proc = runner or SubprocessRunner()
    profile = load_profile(profile_path)
    ps = proc.run(["docker", "ps", "--format", "{{.Ports}}"])
    if ps.code != 0:
        return {"ok": False, "error": ps.stderr.strip() or "docker ps failed"}
    port = allocate_port(used_adb_ports(ps.stdout))
    args = docker_run_args(name, profile, port, image or "redroid/redroid:14.0.0-latest")
    created = proc.run(args, timeout=180)
    if created.code != 0:
        return {"ok": False, "error": created.stderr.strip() or created.stdout.strip()}
    container_id = created.stdout.strip()
    serial = f"127.0.0.1:{port}"
    try:
        wait = _wait_boot(proc, serial)
        if not wait["ok"]:
            return {**wait, "containerId": container_id, "adbPort": port}
        report = apply_profile(RealAdb(serial, proc), profile, proxy)
        body = report_payload(report)
        body.update({"containerId": container_id, "adbPort": port, "serial": serial})
        return body
    except CommandError as exc:
        return {"ok": False, "error": str(exc), "containerId": container_id, "adbPort": port}


def _wait_boot(proc: SubprocessRunner, serial: str, attempts: int = 60) -> dict:
    proc.run(["adb", "connect", serial], timeout=20)
    adb = RealAdb(serial, proc)
    for _ in range(attempts):
        try:
            if adb.getprop("sys.boot_completed") == "1":
                return {"ok": True}
        except CommandError:
            pass
        time.sleep(2)
    return {"ok": False, "error": "sys.boot_completed did not become 1"}


def start_container(name: str, serial: str, profile_path: str, proxy: str, runner: SubprocessRunner | None = None) -> dict:
    proc = runner or SubprocessRunner()
    started = proc.run(["docker", "start", name], timeout=60)
    if started.code != 0:
        return {"ok": False, "error": started.stderr.strip() or started.stdout.strip()}
    boot = _wait_boot(proc, serial, attempts=60)
    if not boot["ok"]:
        return boot
    steps = apply_profile(RealAdb(serial, proc), load_profile(profile_path), proxy)
    body = report_payload(steps)
    body.update({"serial": serial, "containerId": name})
    return body


def stop_container(name: str, runner: SubprocessRunner | None = None) -> dict:
    proc = runner or SubprocessRunner()
    result = proc.run(["docker", "stop", name], timeout=60)
    if result.code != 0:
        return {"ok": False, "error": result.stderr.strip() or result.stdout.strip()}
    return {"ok": True, "name": name}


def tear_down(name: str, runner: SubprocessRunner | None = None) -> dict:
    proc = runner or SubprocessRunner()
    result = proc.run(docker_rm_args(name), timeout=60)
    if result.code != 0:
        return {"ok": False, "error": result.stderr.strip() or result.stdout.strip()}
    return {"ok": True, "name": name}


def parse_wm_size(text: str) -> tuple[int, int]:
    override = ""
    physical = ""
    for line in text.splitlines():
        if "Override size:" in line:
            override = line
        elif "Physical size:" in line:
            physical = line
    chosen = override or physical or text
    if "x" not in chosen:
        return 0, 0
    left, _, right = chosen.rpartition("x")
    width = "".join(ch for ch in left if ch.isdigit())
    height = "".join(ch for ch in right if ch.isdigit())
    if not width or not height:
        return 0, 0
    return int(width), int(height)


def connect_adb(serial: str, runner: SubprocessRunner | None = None) -> dict:
    proc = runner or SubprocessRunner()
    connected = proc.run(["adb", "connect", serial], timeout=20)
    detail = (connected.stdout + "\n" + connected.stderr).strip()
    lowered = detail.lower()
    if connected.code != 0 or "failed to connect" in lowered or "cannot connect" in lowered or "unable to connect" in lowered:
        return {"ok": False, "error": detail or "adb connect failed", "serial": serial}
    adb = RealAdb(serial, proc)
    try:
        size = adb.shell("wm size")
    except CommandError as exc:
        return {"ok": False, "error": str(exc), "serial": serial}
    width, height = parse_wm_size(size)
    port = 0
    if ":" in serial:
        port = int(serial.rsplit(":", 1)[-1])
    return {"ok": True, "serial": serial, "adbPort": port, "width": width, "height": height}


def disconnect_adb(serial: str, runner: SubprocessRunner | None = None) -> dict:
    proc = runner or SubprocessRunner()
    result = proc.run(["adb", "disconnect", serial], timeout=20)
    detail = (result.stdout + "\n" + result.stderr).strip()
    lowered = detail.lower()
    if result.code != 0 and "no such device" not in lowered:
        return {"ok": False, "error": detail or "adb disconnect failed", "serial": serial}
    return {"ok": True, "serial": serial}


def install_apk(serial: str, apk_path: str, package_name: str, runner: SubprocessRunner | None = None) -> dict:
    proc = runner or SubprocessRunner()
    installed = proc.run(["adb", "-s", serial, "install", "-r", apk_path], timeout=180)
    if installed.code != 0:
        detail = installed.stderr.strip() or installed.stdout.strip()
        return {"ok": False, "error": detail or "adb install failed"}
    if not package_name:
        return {"ok": False, "error": "package name is required"}
    adb = RealAdb(serial, proc)
    try:
        located = adb.shell(f"pm path {package_name}")
    except CommandError as exc:
        return {"ok": False, "error": str(exc)}
    if "package:" not in located:
        return {"ok": False, "error": f"pm path missed {package_name}"}
    return {"ok": True, "package": package_name}


def container_status(serial: str, runner: SubprocessRunner | None = None) -> dict:
    adb = RealAdb(serial, runner or SubprocessRunner())
    try:
        booted = adb.getprop("sys.boot_completed") == "1"
    except CommandError as exc:
        return {"ok": False, "error": str(exc), "booted": False}
    return {"ok": booted, "booted": booted, "serial": serial}


def command_up(name: str, profile_path: str, proxy: str) -> int:
    return _emit(bring_up(name, profile_path, proxy))


def command_down(name: str) -> int:
    return _emit(tear_down(name))


def command_stop(name: str) -> int:
    return _emit(stop_container(name))


def command_start(name: str, serial: str, profile_path: str, proxy: str) -> int:
    return _emit(start_container(name, serial, profile_path, proxy))


def command_install(serial: str, apk_path: str, package_name: str) -> int:
    return _emit(install_apk(serial, apk_path, package_name))


def command_adb_connect(serial: str) -> int:
    return _emit(connect_adb(serial))


def command_adb_disconnect(serial: str) -> int:
    return _emit(disconnect_adb(serial))


def command_status(serial: str) -> int:
    return _emit(container_status(serial))


def command_apply(serial: str, profile_path: str, proxy: str) -> int:
    profile: DeviceProfile = load_profile(profile_path)
    steps = apply_profile(RealAdb(serial), profile, proxy)
    return _emit(report_payload(steps))
