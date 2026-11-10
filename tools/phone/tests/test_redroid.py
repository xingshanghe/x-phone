from pathlib import Path

from phone.lifecycle import parse_wm_size
from phone.profile import load_profile
from phone.redroid import allocate_port, binderfs_from_filesystems, docker_run_args, used_adb_ports
from phone.spike import probe_environment, run_spike, spike_plan
from phone.stream import frame_message, resolve_point, screencap_args, swipe_args, tap_args

ROOT = Path(__file__).resolve().parents[3]


def test_binderfs_detects_kernel_filesystem_entry():
    text = "nodev tmpfs\nnodev binderfs\n        ext4\n"
    assert binderfs_from_filesystems(text) is True
    assert binderfs_from_filesystems("nodev proc\n") is False


def test_port_allocation_skips_published_adb_ports():
    used = used_adb_ports("0.0.0.0:18120->5555/tcp, 0.0.0.0:18122->5555/tcp")
    assert allocate_port(used) == 18121


def test_docker_run_sets_profile_resources_and_properties():
    profile = load_profile(ROOT / "profiles/pixel_8.json")
    args = docker_run_args("xphone-1", profile, 18124)
    assert args[0:3] == ["docker", "run", "-d"]
    assert "--cpus" in args and args[args.index("--cpus") + 1] == "9"
    assert "--memory" in args and args[args.index("--memory") + 1] == "8192m"
    assert "ro.product.model=Pixel 8" in args
    assert "ro.hardware.egl=Mali-G715" in args
    assert "androidboot.redroid_width=1080" in args
    assert "androidboot.redroid_height=2400" in args


def test_spike_plan_contains_two_containers_and_scrcpy():
    profile = load_profile(ROOT / "profiles/pixel_8.json")
    plan = spike_plan(profile)
    assert len(plan["containers"]) == 2
    assert plan["containers"][0][plan["containers"][0].index("--name") + 1] == "xphone-spike-a"
    assert plan["containers"][1][plan["containers"][1].index("--name") + 1] == "xphone-spike-b"
    assert plan["scrcpy"][0] == "scrcpy"
    assert "127.0.0.1:18220" in plan["scrcpy"]


def test_spike_dry_run_does_not_require_linux():
    payload = run_spike(str(ROOT / "profiles/pixel_8.json"), execute=False)
    assert payload["executed"] is False
    assert "environment" in payload
    env = probe_environment("nodev proc\n")
    assert env["binderfs"] is False


def test_parse_wm_size_prefers_override():
    text = "Physical size: 1080x2400\nOverride size: 720x1280\n"
    assert parse_wm_size(text) == (720, 1280)
    assert parse_wm_size("Physical size: 1080x2400") == (1080, 2400)


def test_frame_and_tap_commands():
    message = frame_message(b"\x89PNG")
    assert '"mime":"image/png"' in message
    assert screencap_args("127.0.0.1:18120")[-2:] == ["screencap", "-p"]
    assert tap_args("127.0.0.1:18120", 12, 40)[-3:] == ["tap", "12", "40"]
    assert resolve_point({"x": 10, "y": 20}, 1080, 2400) == (10, 20)
    assert resolve_point({"xr": 0.5, "yr": 0.25}, 1080, 2400) == (540, 600)
    assert resolve_point({"xr": 0.5, "yr": 0.25}, 720, 1280) == (360, 320)
    assert swipe_args("127.0.0.1:18120", 1, 2, 3, 4, 300)[-6:] == ["swipe", "1", "2", "3", "4", "300"]
