# 系统级预装 APK（不可卸载）

把 `.apk` 直接放本目录，文件名即目录名（`Foo.apk` → `/system/priv-app/Foo/Foo.apk`）。

- 生效时机：容器启动前由 `entrypoint.sh` 复制，**无需重建镜像**，改完 `docker compose restart` 即可。
- 若希望固化进镜像层（多实例共享、启动更快），保留文件后重新执行 `./scripts/build.sh`（`BAKE_SYSTEM_APPS=1`）。
- 校验用 `expect-packages.txt`（一行一个包名）供 `./scripts/verify.sh` 比对。

⚠ 注意
- APK 需与镜像 ABI 匹配；x86_64 主机跑 arm-only APK 需开启 `NATIVE_BRIDGE=libndk_translation.so`。
- SELinux 默认 `enforcing`：entrypoint 复制 APK 后会自动打 `u:object_r:system_file:s0` 标签（含目录），无需手工处理；宿主文件系统不支持 xattr 时会自动降级为 permissive 并在日志告警。
- 系统应用无法被用户卸载，更新也需替换本目录文件后重启容器。
