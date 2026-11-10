from __future__ import annotations

from phone.profile import DeviceProfile


def identity_props(profile: DeviceProfile) -> tuple[tuple[str, str], ...]:
    return (
        ("ro.product.brand", profile.brand),
        ("ro.product.manufacturer", profile.manufacturer),
        ("ro.product.model", profile.model),
        ("ro.product.device", profile.device),
        ("ro.product.name", profile.product),
        ("ro.build.fingerprint", profile.fingerprint),
        ("ro.build.version.release", profile.release),
        ("ro.build.version.sdk", profile.sdk),
    )


def boot_properties(profile: DeviceProfile) -> tuple[str, ...]:
    props = [f"{key}={value}" for key, value in identity_props(profile)]
    props.append(f"ro.hardware.egl={profile.gpu}")
    props.append(f"androidboot.redroid_width={profile.width}")
    props.append(f"androidboot.redroid_height={profile.height}")
    props.append(f"androidboot.redroid_dpi={profile.dpi}")
    props.append("androidboot.redroid_gpu_mode=guest")
    return tuple(props)
