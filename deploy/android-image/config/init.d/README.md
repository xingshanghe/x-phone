# 开机后自定义钩子

本目录下的 `*.sh` 会在 `sys.boot_completed=1` 之后、以 root 身份按文件名顺序执行。

示例（`10-custom.sh`）：

```sh
#!/system/bin/sh
# 禁用的包
pm disable-user --user 0 com.android.browser
# 预置文件
mkdir -p /sdcard/Download
cp /xphone/assets/demo.txt /sdcard/Download/
```

注意
- 解释器是 Android 的 `/system/bin/sh`（mksh），不要用 bash 专有语法（数组、`[[ ]]`）。
- 配置目录是只读挂载，脚本不要在 `/xphone` 下写文件；写到 `/data` 或 `/sdcard`。
- 钩子失败不会阻断开机，但会打进 logcat（tag `xphone:postboot`）。
