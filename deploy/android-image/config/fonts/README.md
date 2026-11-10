# 中文字体

把 `.ttf / .otf / .ttc` 放本目录，容器启动前会被复制到 `/system/fonts/`（构建期同样会烤进镜像）。

## 是否真的需要？

先确认系统里有没有 CJK 字体：

```sh
adb shell ls /system/fonts | grep -iE 'cjk|NotoSansSC'
```

AOSP 一般自带 `NotoSansCJK-Regular.ttc`（Android 12+ 也可能是 `NotoSansCJK-VF.otf.ttc`）。
若已存在且显示正常，不需要额外放字体；只有在缺字、或要换成品牌字体（如思源黑体 / MiSans）时才需要。

## 换字体的完整改法

1. 字体文件放本目录，例如 `NotoSansSC-Regular.otf`。
2. 改 `/system/etc/fonts.xml`（一次性，可在 `config/init.d/` 钩子或构建期执行）：

```sh
# 备份
cp /system/etc/fonts.xml /system/etc/fonts.xml.bak
# 把 CJK 回退族指向你的字体（★按实际文件名替换）
sed -i 's|>NotoSansCJK-Regular.ttc<|>NotoSansSC-Regular.otf<|g' /system/etc/fonts.xml
sed -i 's|>NotoSansCJK-VF.otf.ttc<|>NotoSansSC-Regular.otf<|g' /system/etc/fonts.xml
```

3. 重启容器并验证：`adb shell ls /system/fonts` + 截图确认中文不出现方框。

⚠ `fonts.xml` 写错会导致系统字体加载失败、无法进入系统。务必先备份，并在单实例上验证。
