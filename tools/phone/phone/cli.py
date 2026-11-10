from __future__ import annotations

import argparse
import json
import sys

from phone.lifecycle import (
    command_adb_connect,
    command_adb_disconnect,
    command_apply,
    command_down,
    command_install,
    command_start,
    command_status,
    command_stop,
    command_up,
)
from phone.spike import run_spike
from phone.stream import command_stream


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(prog="phone")
    sub = parser.add_subparsers(dest="cmd", required=True)

    up = sub.add_parser("up")
    up.add_argument("--name", required=True)
    up.add_argument("--profile", required=True)
    up.add_argument("--proxy", default="")

    down = sub.add_parser("down")
    down.add_argument("--name", required=True)

    stop = sub.add_parser("stop")
    stop.add_argument("--name", required=True)

    start = sub.add_parser("start")
    start.add_argument("--name", required=True)
    start.add_argument("--serial", required=True)
    start.add_argument("--profile", required=True)
    start.add_argument("--proxy", default="")

    status = sub.add_parser("status")
    status.add_argument("--serial", required=True)

    apply_cmd = sub.add_parser("apply-profile")
    apply_cmd.add_argument("--serial", required=True)
    apply_cmd.add_argument("--profile", required=True)
    apply_cmd.add_argument("--proxy", default="")

    install = sub.add_parser("install-apk")
    install.add_argument("--serial", required=True)
    install.add_argument("--apk", required=True)
    install.add_argument("--package", required=True)

    adb_connect = sub.add_parser("adb-connect")
    adb_connect.add_argument("--serial", required=True)

    adb_disconnect = sub.add_parser("adb-disconnect")
    adb_disconnect.add_argument("--serial", required=True)

    stream = sub.add_parser("stream")
    stream.add_argument("--serial", required=True)
    stream.add_argument("--url", required=True)
    stream.add_argument("--width", type=int, default=0)
    stream.add_argument("--height", type=int, default=0)

    spike = sub.add_parser("spike")
    spike.add_argument("--profile", required=True)
    spike.add_argument("--execute", action="store_true")

    args = parser.parse_args(argv)
    if args.cmd == "up":
        return command_up(args.name, args.profile, args.proxy)
    if args.cmd == "down":
        return command_down(args.name)
    if args.cmd == "stop":
        return command_stop(args.name)
    if args.cmd == "start":
        return command_start(args.name, args.serial, args.profile, args.proxy)
    if args.cmd == "status":
        return command_status(args.serial)
    if args.cmd == "apply-profile":
        return command_apply(args.serial, args.profile, args.proxy)
    if args.cmd == "install-apk":
        return command_install(args.serial, args.apk, args.package)
    if args.cmd == "adb-connect":
        return command_adb_connect(args.serial)
    if args.cmd == "adb-disconnect":
        return command_adb_disconnect(args.serial)
    if args.cmd == "stream":
        return command_stream(args.serial, args.url, args.width, args.height)
    if args.cmd == "spike":
        payload = run_spike(args.profile, args.execute)
        json.dump(payload, sys.stdout)
        sys.stdout.write("\n")
        if args.execute and not payload.get("ok"):
            return 2
        return 0
    return 2


if __name__ == "__main__":
    raise SystemExit(main())
