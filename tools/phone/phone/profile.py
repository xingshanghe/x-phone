from __future__ import annotations

import json
from dataclasses import dataclass
from pathlib import Path


@dataclass(frozen=True)
class DeviceProfile:
    id: str
    brand: str
    manufacturer: str
    model: str
    device: str
    product: str
    fingerprint: str
    release: str
    sdk: str
    width: int
    height: int
    dpi: int
    locale: str
    timezone: str
    gpu: str
    cpu_cores: int
    memory_mb: int
    touch_points: int


def load_profile(path: str | Path) -> DeviceProfile:
    raw = json.loads(Path(path).read_text(encoding="utf-8"))
    return profile_from_dict(raw)


def profile_from_dict(raw: dict) -> DeviceProfile:
    return DeviceProfile(
        id=str(raw["id"]),
        brand=str(raw["brand"]),
        manufacturer=str(raw["manufacturer"]),
        model=str(raw["model"]),
        device=str(raw["device"]),
        product=str(raw["product"]),
        fingerprint=str(raw["fingerprint"]),
        release=str(raw["release"]),
        sdk=str(raw["sdk"]),
        width=int(raw["width"]),
        height=int(raw["height"]),
        dpi=int(raw["dpi"]),
        locale=str(raw["locale"]),
        timezone=str(raw["timezone"]),
        gpu=str(raw["gpu"]),
        cpu_cores=int(raw.get("cpuCores", raw.get("cpu_cores"))),
        memory_mb=int(raw.get("memoryMb", raw.get("memory_mb"))),
        touch_points=int(raw.get("touchPoints", raw.get("touch_points"))),
    )
