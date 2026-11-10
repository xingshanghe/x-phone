# 用户级预装 APK（可卸载）

把 `.apk` 放本目录，开机完成后由 `/opt/xphone/post-boot.sh` 用
`pm install -r -g --bypass-low-target-sdk-block` 安装（覆盖安装 + 自动授予运行时权限）。

- 幂等：已存在时会返回 `INSTALL_FAILED_ALREADY_EXISTS` 并跳过，不会重复安装。
- 用户可自行卸载；容器重建时若 `/data` 保留，卸载状态会保留（不会被重新装上）。
- 校验用 `expect-packages.txt`（一行一个包名）供 `./scripts/verify.sh` 比对。
