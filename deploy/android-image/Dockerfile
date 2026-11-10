# syntax=docker/dockerfile:1.7
# =============================================================================
# x-phone · 高度定制 Android 容器镜像（基于 redroid）
#
# 分层策略：变化频率从低到高排列，最大化构建缓存命中
#   L0  base     官方 redroid 基础镜像（几乎不变）
#   L1  meta     目录骨架与元信息（几乎不变）
#   L2  prune    精简 / 替换系统应用（低频变）
#   L3  fonts    中文字体（低频变）
#   L4  sysapps  系统级预装 APK（中频变）
#   L5  media    开机动画、铃声等媒体资源（中频变）
#   L6  scripts  入口脚本（高频变，放最后）
#
# 体积说明（重要）：
#   * 删除文件只会产生 overlayfs whiteout，镜像体积不会变小。
#     真正瘦身请用 scripts/build.sh --flatten（export/import 扁平化）。
#   * 所有构建依赖都在 fetcher 阶段，不会进入最终镜像。
# =============================================================================

# ----------------------------- 构建参数（★需按需求替换）----------------------
# 单架构基线：amd64 / Android 14（社区验证最稳；宿主机必须是 x86_64 Linux）
ARG ANDROID_TAG=14.0.0-latest
ARG BASE_IMAGE=redroid/redroid:${ANDROID_TAG}
# 中文字体下载直链（留空则跳过下载，改用 config/fonts/ 里手放的文件）
ARG CJK_FONT_URL=""
# 是否把 config/apps/system/*.apk 在构建期就烤进 /system/priv-app
ARG BAKE_SYSTEM_APPS=1
# 是否执行 config/prune-apps.txt 精简
ARG PRUNE_APPS=1

# =========================== Stage: fetcher（不进最终镜像）===================
FROM alpine:3.21 AS fetcher
ARG CJK_FONT_URL
RUN apk add --no-cache curl ca-certificates \
 && mkdir -p /out/fonts \
 && touch /out/fonts/.keep \
 && if [ -n "${CJK_FONT_URL}" ]; then \
      echo "downloading CJK font: ${CJK_FONT_URL}"; \
      curl -fsSL "${CJK_FONT_URL}" -o /out/fonts/CJK-custom.ttf; \
    else \
      echo "CJK_FONT_URL empty, skip font download"; \
    fi \
 && ls -la /out/fonts

# ================================= Stage: 定制 ===============================
FROM ${BASE_IMAGE}

# Android 镜像里 /bin/sh 不一定存在，显式指定 init 可用的 shell
SHELL ["/system/bin/sh", "-c"]

ARG BAKE_SYSTEM_APPS
ARG PRUNE_APPS

LABEL org.opencontainers.image.title="xphone-android-custom" \
      org.opencontainers.image.description="Highly customized Android (redroid base) image for x-phone cloud phone" \
      org.opencontainers.image.source="https://github.com/xingshanghe/x-phone" \
      maintainer="x-phone"

ENV XPHONE_IMAGE_DIR=/opt/xphone \
    XPHONE_CONFIG_DIR=/xphone \
    XPHONE_VERSION=1.0.0

# ------------------------------- L1 目录骨架 --------------------------------
RUN mkdir -p ${XPHONE_IMAGE_DIR} ${XPHONE_CONFIG_DIR} \
 && mkdir -p ${XPHONE_CONFIG_DIR}/apps/system ${XPHONE_CONFIG_DIR}/apps/user \
 && mkdir -p ${XPHONE_CONFIG_DIR}/fonts ${XPHONE_CONFIG_DIR}/init.d ${XPHONE_CONFIG_DIR}/props \
 && mkdir -p /system/etc/init \
 && chmod 0755 ${XPHONE_IMAGE_DIR} ${XPHONE_CONFIG_DIR}

# ---------------------------- L2 精简 / 替换系统应用 -------------------------
# 说明：删除只减少运行时占用与启动开销，镜像体积靠 --flatten 才会真正下降。
COPY config/prune-apps.txt /tmp/xphone/prune-apps.txt
RUN if [ "$PRUNE_APPS" = "1" ]; then \
      echo "== prune system apps =="; \
      while IFS= read -r p; do \
        case "$p" in ''|'#'*) continue ;; esac; \
        if [ -e "/system/$p" ]; then echo "  rm /system/$p"; rm -rf "/system/$p"; fi; \
      done < /tmp/xphone/prune-apps.txt; \
    fi \
 && rm -f /tmp/xphone/prune-apps.txt

# ------------------------------- L3 中文字体 --------------------------------
COPY --from=fetcher /out/fonts/ /tmp/xphone-fonts/
COPY config/fonts/ /tmp/xphone-fonts-manual/
RUN echo "== install CJK fonts ==" \
 && for f in /tmp/xphone-fonts/* /tmp/xphone-fonts-manual/*; do \
      case "$f" in *\*) continue ;; esac; \
      case "$f" in *.ttf|*.otf|*.ttc) cp -f "$f" /system/fonts/ ;; esac; \
    done \
 && chmod 0644 /system/fonts/* 2>/dev/null || true \
 && ls -la /system/fonts | head -40 \
 && rm -rf /tmp/xphone-fonts /tmp/xphone-fonts-manual

# ---------------------------- L4 系统级预装 APK -----------------------------
# 目录内 APK 会被安装为 /system/priv-app/<Name>/<Name>.apk（不可卸载）
COPY config/apps/system/ /tmp/xphone-sysapps/
RUN if [ "$BAKE_SYSTEM_APPS" = "1" ]; then \
      echo "== bake system apps =="; \
      for apk in /tmp/xphone-sysapps/*.apk; do \
        case "$apk" in *\*) continue ;; esac; \
        n=$(basename "$apk" .apk); \
        mkdir -p "/system/priv-app/$n"; \
        cp -f "$apk" "/system/priv-app/$n/$n.apk"; \
        chmod 0755 "/system/priv-app/$n"; \
        chmod 0644 "/system/priv-app/$n/$n.apk"; \
        echo "  baked: /system/priv-app/$n/$n.apk"; \
      done; \
    fi \
 && rm -rf /tmp/xphone-sysapps

# ----------------------------- L5 开机动画 / 媒体 ---------------------------
# config/bootanimation.zip 存在则覆盖 /system/media/bootanimation.zip
COPY config/bootanimation.zip* /tmp/xphone-media/
RUN if [ -f /tmp/xphone-media/bootanimation.zip ]; then \
      echo "== replace bootanimation =="; \
      cp -f /tmp/xphone-media/bootanimation.zip /system/media/bootanimation.zip; \
      chmod 0644 /system/media/bootanimation.zip; \
    fi \
 && rm -rf /tmp/xphone-media \
 && ls -la /system/media 2>/dev/null | head -20 || true

# ------------------------------- L6 脚本层 ----------------------------------
COPY scripts/post-boot.sh ${XPHONE_IMAGE_DIR}/post-boot.sh
COPY entrypoint.sh ${XPHONE_IMAGE_DIR}/entrypoint.sh
RUN chmod 0755 ${XPHONE_IMAGE_DIR}/entrypoint.sh ${XPHONE_IMAGE_DIR}/post-boot.sh \
 && ls -la ${XPHONE_IMAGE_DIR}

# redroid 的 /init 由我们的入口脚本 exec 拉起，PID 1 仍然是 Android init
ENTRYPOINT ["/opt/xphone/entrypoint.sh"]
CMD []

# 容器内端口：5555 = ADB
EXPOSE 5555
