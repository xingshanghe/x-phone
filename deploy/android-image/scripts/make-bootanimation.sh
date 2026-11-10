#!/usr/bin/env bash
# =============================================================================
# 生成开机动画 bootanimation.zip
#
#   ./scripts/make-bootanimation.sh --src ./frames --out config/bootanimation.zip --w 1080 --h 2400 --fps 30
#   ./scripts/make-bootanimation.sh --src ./logo.png --out config/bootanimation.zip --w 1080 --h 2400
#
# 输入：
#   --src  图片序列目录（按文件名排序，建议 0000.png 0001.png …）或单张 PNG
# 产物结构（必须 store 模式，即不压缩）：
#   desc.txt
#   part0/0000.png …
#   part1/0000.png …   # 可选：循环播放段
# =============================================================================
set -euo pipefail

SRC=""; OUT="config/bootanimation.zip"; W=1080; H=2400; FPS=30
while [[ $# -gt 0 ]]; do
  case "$1" in
    --src) SRC="$2"; shift 2 ;;
    --out) OUT="$2"; shift 2 ;;
    --w) W="$2"; shift 2 ;;
    --h) H="$2"; shift 2 ;;
    --fps) FPS="$2"; shift 2 ;;
    *) echo "unknown arg: $1"; exit 1 ;;
  esac
done

[[ -n "$SRC" ]] || { echo "需要 --src <图片目录或单张 PNG>"; exit 1; }
command -v zip >/dev/null || { echo "需要 zip：sudo apt install zip"; exit 1; }
command -v convert >/dev/null || echo "提示：未安装 imagemagick，将跳过尺寸归一化（请确保图片尺寸与 ${W}x${H} 一致）"

TMP=$(mktemp -d)
trap 'rm -rf "$TMP"' EXIT
mkdir -p "$TMP/part0"

if [[ -d "$SRC" ]]; then
  mapfile -t files < <(find "$SRC" -maxdepth 1 -type f \( -iname '*.png' -o -iname '*.jpg' \) | sort)
  [[ ${#files[@]} -gt 0 ]] || { echo "目录内没有图片： $SRC"; exit 1; }
  i=0
  for f in "${files[@]}"; do
    dst="$TMP/part0/$(printf '%04d' "$i").png"
    if command -v convert >/dev/null; then
      convert "$f" -resize "${W}x${H}!" -background black -flatten "$dst"
    else
      cp "$f" "$dst"
    fi
    i=$((i + 1))
  done
  echo "已准备 $i 帧"
else
  dst="$TMP/part0/0000.png"
  if command -v convert >/dev/null; then
    convert "$SRC" -resize "${W}x${H}!" -background black -flatten "$dst"
  else
    cp "$SRC" "$dst"
  fi
fi

# desc.txt：宽 高 帧率 / 段类型 循环次数 帧间隔 目录名
cat > "$TMP/desc.txt" <<EOF
${W} ${H} ${FPS}
p 0 0 part0
EOF

rm -f "$OUT"
(cd "$TMP" && zip -0 -r -X "$OLDPWD/$OUT" desc.txt part0 >/dev/null)
echo "生成完成：$OUT"
unzip -l "$OUT" | sed 's/^/  /'

cat <<EOF

下一步：
  docker compose restart    # 容器启动前会把 config/bootanimation.zip 复制到 /system/media/bootanimation.zip
  adb shell ls -l /system/media/bootanimation.zip
EOF
