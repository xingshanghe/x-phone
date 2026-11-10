from __future__ import annotations

import asyncio
import base64
import contextlib
import json
import subprocess

from phone.shell import CommandError


def screencap_args(serial: str) -> list[str]:
    return ["adb", "-s", serial, "exec-out", "screencap", "-p"]


def tap_args(serial: str, x: int, y: int) -> list[str]:
    return ["adb", "-s", serial, "shell", "input", "tap", str(int(x)), str(int(y))]


def scrcpy_args(serial: str) -> list[str]:
    return ["scrcpy", "--serial", serial, "--no-audio", "--no-control", "--max-fps", "5"]


def frame_message(payload: bytes, mime: str = "image/png") -> str:
    return json.dumps(
        {"type": "frame", "mime": mime, "data": base64.b64encode(payload).decode("ascii")},
        separators=(",", ":"),
    )


def capture_screen(serial: str) -> bytes:
    completed = subprocess.run(screencap_args(serial), check=False, capture_output=True, timeout=20)
    if completed.returncode != 0 or not completed.stdout:
        detail = completed.stderr.decode("utf-8", "replace").strip()
        raise CommandError(detail or "screencap failed")
    return completed.stdout


def send_tap(serial: str, x: int, y: int) -> None:
    run_input(tap_args(serial, x, y), "input tap failed")


def swipe_args(serial: str, x: int, y: int, x2: int, y2: int, duration: int) -> list[str]:
    return ["adb", "-s", serial, "shell", "input", "swipe", str(x), str(y), str(x2), str(y2), str(duration)]


def motion_args(serial: str, action: str, x: int, y: int) -> list[str]:
    return ["adb", "-s", serial, "shell", "input", "motionevent", action, str(x), str(y)]


def key_args(serial: str, key: str) -> list[str]:
    return ["adb", "-s", serial, "shell", "input", "keyevent", key]


def text_args(serial: str, text: str) -> list[str]:
    return ["adb", "-s", serial, "shell", "input", "text", text.replace(" ", "%s")]


def scale_ratio(ratio: float, size: int) -> int:
    value = int(round(ratio * size))
    if value >= size:
        return size - 1
    if value < 0:
        return 0
    return value


def resolve_point(body: dict, width: int, height: int, x_key: str = "x", y_key: str = "y", xr_key: str = "xr", yr_key: str = "yr") -> tuple[int, int]:
    if x_key in body and y_key in body:
        return int(body[x_key]), int(body[y_key])
    if width < 1 or height < 1:
        raise CommandError("video size is unknown")
    return scale_ratio(float(body[xr_key]), width), scale_ratio(float(body[yr_key]), height)


def apply_input(serial: str, body: dict, width: int, height: int) -> None:
    kind = body.get("type")
    if kind == "tap":
        x, y = resolve_point(body, width, height)
        send_tap(serial, x, y)
        return
    if kind == "swipe":
        x, y = resolve_point(body, width, height)
        x2, y2 = resolve_point(body, width, height, "x2", "y2", "x2r", "y2r")
        run_input(swipe_args(serial, x, y, x2, y2, int(body.get("duration") or 300)), "input swipe failed")
        return
    if kind == "longpress":
        x, y = resolve_point(body, width, height)
        run_input(swipe_args(serial, x, y, x, y, int(body.get("duration") or 800)), "input longpress failed")
        return
    if kind == "pointer":
        x, y = resolve_point(body, width, height)
        action = str(body.get("action", "")).upper()
        run_input(motion_args(serial, action, x, y), "input motionevent failed")
        return
    if kind == "key":
        run_input(key_args(serial, str(body.get("key", ""))), "input keyevent failed")
        return
    if kind == "text":
        run_input(text_args(serial, str(body.get("text", ""))), "input text failed")


def run_input(args: list[str], fallback: str) -> None:
    completed = subprocess.run(args, check=False, capture_output=True, timeout=10)
    if completed.returncode != 0:
        detail = completed.stderr.decode("utf-8", "replace").strip()
        raise CommandError(detail or fallback)


async def read_control(socket, serial: str, width: int, height: int) -> None:
    while True:
        incoming = await socket.recv()
        try:
            body = json.loads(incoming)
        except json.JSONDecodeError:
            continue
        if not isinstance(body, dict):
            continue
        try:
            await asyncio.to_thread(apply_input, serial, body, width, height)
        except CommandError as exc:
            await socket.send(json.dumps({"type": "status", "text": str(exc)}))


async def stream_frames(serial: str, url: str, width: int = 0, height: int = 0) -> None:
    import websockets

    async with websockets.connect(url) as socket:
        if width > 0 and height > 0:
            await socket.send(json.dumps({"type": "video-size", "width": width, "height": height}))
        control = asyncio.create_task(read_control(socket, serial, width, height))
        try:
            while True:
                try:
                    png = await asyncio.to_thread(capture_screen, serial)
                    await socket.send(frame_message(png))
                except CommandError as exc:
                    await socket.send(json.dumps({"type": "status", "text": str(exc)}))
        finally:
            control.cancel()
            with contextlib.suppress(asyncio.CancelledError):
                await control


def command_stream(serial: str, url: str, width: int = 0, height: int = 0) -> int:
    try:
        asyncio.run(stream_frames(serial, url, width, height))
    except KeyboardInterrupt:
        return 0
    return 0
