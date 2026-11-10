/*
 * xphone_fake —— 二进制层特征抹除（LD_PRELOAD hook 库，自研，不依赖任何第三方反检测项目）
 *
 * 解决的问题：只改 Android 属性挡不住在 native 层取证的检测。典型取证点：
 *   1) glGetString(GL_RENDERER)   -> 软件渲染会返回 SwiftShader / ANGLE，一读就知道是容器
 *   2) /proc/meminfo、/proc/cpuinfo -> 真实宿主内存与 CPU，与伪装机型矛盾
 *   3) sysconf(_SC_NPROCESSORS_CONF) -> 宿主核数
 *   4) __system_property_get()    -> 绕过 Java Build，直接在 native 读属性区
 *
 * 注入方式（见 native/xphone-fake/README.md）：
 *   库放 /system/lib64/libxphone_fake.so，由 init.rc 在 early-init 执行
 *   `setenv LD_PRELOAD /system/lib64/libxphone_fake.so`，zygote 及其所有子进程继承。
 *
 * 环境变量（由 init.rc / device.env 提供）：
 *   XFAKE_DIR     伪造文件目录，默认 /xphone/fake（meminfo / cpuinfo / props.map …）
 *   XFAKE_DEBUG=1 打开 stderr 日志
 *   XFAKE_GL_RENDERER / XFAKE_GL_VENDOR / XFAKE_GL_VERSION  GPU 字符串
 *   XFAKE_CPU_COUNT / XFAKE_MEM_MB                          CPU 核数与内存（MB）
 *
 * ⚠ 该库会被注入到系统全部 native 进程，任何缺陷都可能造成开机循环。
 *   因此内置两道保险（见 xfake_disabled）：
 *     1) 文件保险丝：${XFAKE_DIR}/disable 存在 -> 全库直通（失效但不崩溃），
 *        出问题时在宿主执行 `touch config/fake/disable && docker restart` 即可恢复；
 *     2) 环境保险丝：XFAKE_DISABLE=1 -> 同上。
 *   默认关闭（STEALTH_PRELOAD=0），用 scripts/test-preload.sh 做单实例验证后再打开。
 */

#define _GNU_SOURCE
#include <dlfcn.h>
#include <fcntl.h>
#include <stdarg.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <unistd.h>

#define GL_VENDOR 0x1F00
#define GL_RENDERER 0x1F01
#define GL_VERSION 0x1F02
#define GL_EXTENSIONS 0x1F03
#define GL_SHADING_LANGUAGE_VERSION 0x8B8C

/* sysconf 的 name 直接用 bionic 头文件里的宏（NDK 编译，保证与目标平台一致），
 * 不要硬编码数字：glibc / bionic 的取值不同，硬编码必然埋雷。 */

static int xfake_debug(void) {
  static int cached = -1;
  if (cached < 0) cached = (getenv("XFAKE_DEBUG") != NULL);
  return cached;
}
#define XLOG(...)                                              \
  do {                                                         \
    if (xfake_debug()) fprintf(stderr, "[xphone-fake] " __VA_ARGS__); \
  } while (0)

static const char *xfake_dir(void) {
  static const char *d;
  if (!d) {
    d = getenv("XFAKE_DIR");
    if (!d || !*d) d = "/xphone/fake";
  }
  return d;
}

static const char *env_or(const char *key, const char *fallback) {
  const char *v = getenv(key);
  return (v && *v) ? v : fallback;
}

/* ------------------------------------------------------------------ 保险丝 */
/* 返回非 0 时，全部 hook 变为纯直通。结果只判定一次（进程级缓存）。 */
static int g_disabled = -1;
static int xfake_disabled(void) {
  if (g_disabled >= 0) return g_disabled;
  g_disabled = 0;
  const char *e = getenv("XFAKE_DISABLE");
  if (e && *e && *e != '0') {
    g_disabled = 1;
  } else {
    char buf[512];
    snprintf(buf, sizeof(buf), "%s/disable", xfake_dir());
    if (access(buf, F_OK) == 0) g_disabled = 1; /* access 是直接系统调用，不会递归进 hook */
  }
  if (g_disabled) XLOG("kill-switch active, all hooks pass through\n");
  return g_disabled;
}
__attribute__((constructor)) static void xfake_init(void) {
  (void)xfake_disabled(); /* 尽早判定，避免在关键路径上首次检查 */
}

/* ------------------------------------------------------------------ 文件重定向 */
static char *map_path(const char *path) {
  if (!path || xfake_disabled()) return NULL;
  struct entry { const char *src; const char *name; };
  static const struct entry table[] = {
      {"/proc/meminfo", "meminfo"},
      {"/proc/cpuinfo", "cpuinfo"},
      {"/proc/stat", "stat"},
      {"/proc/version", "version"},
  };
  size_t i;
  for (i = 0; i < sizeof(table) / sizeof(table[0]); i++) {
    if (strcmp(path, table[i].src) == 0) {
      char buf[512];
      snprintf(buf, sizeof(buf), "%s/%s", xfake_dir(), table[i].name);
      if (access(buf, R_OK) == 0) {
        XLOG("redirect %s -> %s\n", path, buf);
        return strdup(buf);
      }
      return NULL; /* 没准备伪造文件就走真实文件，不猜 */
    }
  }
  /* eth0 -> wlan0：真机不该有 eth0 */
  if (strncmp(path, "/sys/class/net/eth0", 19) == 0) {
    char buf[512];
    snprintf(buf, sizeof(buf), "/sys/class/net/wlan0%s", path + 19);
    if (access(buf, R_OK) == 0) {
      XLOG("redirect %s -> %s\n", path, buf);
      return strdup(buf);
    }
  }
  return NULL;
}

int open(const char *path, int flags, ...) {
  static int (*real)(const char *, int, ...);
  if (!real) real = dlsym(RTLD_NEXT, "open");
  mode_t mode = 0;
  if (flags & O_CREAT) {
    va_list ap; va_start(ap, flags); mode = va_arg(ap, int); va_end(ap);
  }
  char *fake = map_path(path);
  if (fake) { int fd = real(fake, flags, mode); free(fake); return fd; }
  return real(path, flags, mode);
}

int open64(const char *path, int flags, ...) {
  static int (*real)(const char *, int, ...);
  if (!real) real = dlsym(RTLD_NEXT, "open64");
  mode_t mode = 0;
  if (flags & O_CREAT) {
    va_list ap; va_start(ap, flags); mode = va_arg(ap, int); va_end(ap);
  }
  char *fake = map_path(path);
  if (fake) { int fd = real(fake, flags, mode); free(fake); return fd; }
  return real(path, flags, mode);
}

int openat(int dirfd, const char *path, int flags, ...) {
  static int (*real)(int, const char *, int, ...);
  if (!real) real = dlsym(RTLD_NEXT, "openat");
  mode_t mode = 0;
  if (flags & O_CREAT) {
    va_list ap; va_start(ap, flags); mode = va_arg(ap, int); va_end(ap);
  }
  char *fake = map_path(path);
  if (fake) { int fd = real(dirfd, fake, flags, mode); free(fake); return fd; }
  return real(dirfd, path, flags, mode);
}

FILE *fopen(const char *path, const char *mode) {
  static FILE *(*real)(const char *, const char *);
  if (!real) real = dlsym(RTLD_NEXT, "fopen");
  char *fake = map_path(path);
  if (fake) { FILE *f = real(fake, mode); free(fake); return f; }
  return real(path, mode);
}

FILE *fopen64(const char *path, const char *mode) {
  static FILE *(*real)(const char *, const char *);
  if (!real) real = dlsym(RTLD_NEXT, "fopen64");
  char *fake = map_path(path);
  if (fake) { FILE *f = real(fake, mode); free(fake); return f; }
  return real(path, mode);
}

/* ------------------------------------------------------------------ sysconf */
long sysconf(int name) {
  static long (*real)(int);
  if (!real) real = dlsym(RTLD_NEXT, "sysconf");
  if (!xfake_disabled()) {
    const char *cpu = getenv("XFAKE_CPU_COUNT");
    const char *mem = getenv("XFAKE_MEM_MB");
    switch (name) {
#if defined(_SC_NPROCESSORS_CONF) && defined(_SC_NPROCESSORS_ONLN)
      case _SC_NPROCESSORS_CONF:
      case _SC_NPROCESSORS_ONLN:
        if (cpu && *cpu) { long v = atol(cpu); if (v > 0) { XLOG("sysconf(nproc) -> %ld\n", v); return v; } }
        break;
#endif
#ifdef _SC_PHYS_PAGES
      case _SC_PHYS_PAGES:
#endif
#ifdef _SC_AVPHYS_PAGES
      case _SC_AVPHYS_PAGES:
#endif
#if defined(_SC_PHYS_PAGES) || defined(_SC_AVPHYS_PAGES)
        if (mem && *mem) {
          long mb = atol(mem);
          if (mb > 0) {
            long pages = mb * 1024L * 1024L / real(_SC_PAGESIZE);
            XLOG("sysconf(pages) -> %ld\n", pages);
            return pages;
          }
        }
        break;
#endif
      default:
        break;
    }
  }
  return real(name);
}

/* --------------------------------------------------------- 属性区（native 读取）*/
/* props.map：一行一条 key=value，命中则直接返回，绕开 Java 层无法绕开的 native 检测 */
#define MAX_MAP 128
static char *map_keys[MAX_MAP];
static char *map_vals[MAX_MAP];
static int map_len = 0;
static int map_loaded = 0;

static void load_props_map(void) {
  if (map_loaded) return;
  map_loaded = 1;
  char path[512];
  snprintf(path, sizeof(path), "%s/props.map", xfake_dir());
  FILE *f = fopen(path, "r"); /* 注意：这里还没 hook 完成时使用真实 fopen 即可 */
  if (!f) return;
  char line[1024];
  while (fgets(line, sizeof(line), f) && map_len < MAX_MAP) {
    char *eq = strchr(line, '=');
    if (!eq) continue;
    *eq = '\0';
    char *nl = strchr(eq + 1, '\n');
    if (nl) *nl = '\0';
    map_keys[map_len] = strdup(line);
    map_vals[map_len] = strdup(eq + 1);
    map_len++;
  }
  fclose(f);
  XLOG("loaded %d prop overrides\n", map_len);
}

int __system_property_get(const char *name, char *value) {
  static int (*real)(const char *, char *);
  if (!real) real = dlsym(RTLD_NEXT, "__system_property_get");
  if (!xfake_disabled()) {
  load_props_map();
  int i;
  for (i = 0; i < map_len; i++) {
    if (strcmp(map_keys[i], name) == 0) {
      strcpy(value, map_vals[i]);
      XLOG("prop override %s=%s\n", name, value);
      return (int)strlen(value);
    }
  }
  }
  return real(name, value);
}

/* ------------------------------------------------------------------ GLES 字符串 */
const unsigned char *glGetString(unsigned int name) {
  static const unsigned char *(*real)(unsigned int);
  if (!real) real = dlsym(RTLD_NEXT, "glGetString");
  if (!real) return (const unsigned char *)"";
  if (!xfake_disabled()) {
  switch (name) {
    case GL_VENDOR:   return (const unsigned char *)env_or("XFAKE_GL_VENDOR", "Qualcomm");
    case GL_RENDERER: return (const unsigned char *)env_or("XFAKE_GL_RENDERER", "Adreno (TM) 740");
    case GL_VERSION:  return (const unsigned char *)env_or("XFAKE_GL_VERSION", "OpenGL ES 3.2 V@0502.0 (GIT@abc)");
    case GL_SHADING_LANGUAGE_VERSION:
      return (const unsigned char *)"OpenGL ES GLSL ES 3.20";
    default: break;
  }
  }
  return real(name);
}

const unsigned char *glGetStringi(unsigned int name, unsigned int index) {
  static const unsigned char *(*real)(unsigned int, unsigned int);
  if (!real) real = dlsym(RTLD_NEXT, "glGetStringi");
  if (!real) return (const unsigned char *)"";
  if (name == GL_EXTENSIONS) return real(name, index);
  return real(name, index);
}
