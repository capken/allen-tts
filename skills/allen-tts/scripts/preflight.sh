#!/usr/bin/env sh
# 一次性探测技能运行所需的全部状态，输出 JSON 供调用方读取。
#
# 设计约束：
#   - 只输出布尔值与版本号，绝不输出密钥内容。
#   - 任何探测失败都降级为 false / 空值，不中断、不报错退出。
#   - 版本检查带缓存且限时，网络不通时静默跳过。
set -u

SKILL_DIR=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
SKILL_VERSION=$(tr -d ' \t\r\n' < "${SKILL_DIR}/VERSION" 2>/dev/null || true)
[ -n "$SKILL_VERSION" ] || SKILL_VERSION="unknown"

# 本技能用到的 CLI 参数所要求的最低版本
MIN_CLI_VERSION="0.1.0"
REPO="capken/allen-tts"
VERSION_URL="https://raw.githubusercontent.com/${REPO}/main/skills/allen-tts/VERSION"
CHECK_TTL=86400

# ---------- 工具函数 ----------

# ver_ge A B -> 输出 1 表示 A >= B（按 major.minor.patch 数值比较）
ver_ge() {
  awk -v a="$1" -v b="$2" 'BEGIN{
    n=split(a,x,"."); m=split(b,y,".")
    for(i=1;i<=3;i++){
      xi=(i<=n?x[i]+0:0); yi=(i<=m?y[i]+0:0)
      if(xi>yi){print 1; exit}
      if(xi<yi){print 0; exit}
    }
    print 1
  }'
}

json_str() { printf '"%s"' "$(printf '%s' "$1" | sed 's/\\/\\\\/g; s/"/\\"/g')"; }

# ---------- 路径 ----------

if [ -n "${ALLEN_TTS_CONFIG:-}" ]; then
  CONFIG_PATH="$ALLEN_TTS_CONFIG"
else
  CONFIG_PATH="${HOME}/.allen-tts/config.yaml"
fi
CONFIG_DIR=$(dirname "$CONFIG_PATH")
VOICES_PATH="${CONFIG_DIR}/voices.yaml"
CACHE_PATH="${CONFIG_DIR}/.skill-update-check"

# ---------- CLI ----------

BIN_PATH=$(command -v allen-tts 2>/dev/null || true)
CLI_VERSION=""
CLI_COMPARABLE=false
CLI_OK=true
if [ -n "$BIN_PATH" ]; then
  CLI_VERSION=$(allen-tts --version 2>/dev/null | awk '{print $NF}' || true)
  probe=$(printf '%s' "$CLI_VERSION" | sed 's/^v//')
  # 伪版本（go install）与 dev 构建无法比较，跳过最低版本校验而不是误报过旧
  case "$probe" in
    ''|*[!0-9.]*) CLI_COMPARABLE=false ;;
    *) CLI_COMPARABLE=true ;;
  esac
  if [ "$CLI_COMPARABLE" = true ] && [ "$(ver_ge "$probe" "$MIN_CLI_VERSION")" = "0" ]; then
    CLI_OK=false
  fi
fi

GO_AVAILABLE=false
command -v go >/dev/null 2>&1 && GO_AVAILABLE=true

# ---------- 配置与密钥 ----------

CONFIG_EXISTS=false
[ -f "$CONFIG_PATH" ] && CONFIG_EXISTS=true

DEFAULT_PROVIDER=""
KEY_FISH=false; KEY_CARTESIA=false; KEY_MINIMAX=false

if [ -n "$BIN_PATH" ]; then
  # config show 已经做好了 环境变量 > 配置文件 的合并，且密钥是打码的
  SHOW=$(allen-tts config show 2>/dev/null || true)
  # YAML 里的空值会输出成 ""，要把引号剥掉才能判空
  DEFAULT_PROVIDER=$(printf '%s\n' "$SHOW" | awk -F': *' '/^default_provider:/{v=$2; gsub(/"/,"",v); print v; exit}')
  KEYS=$(printf '%s\n' "$SHOW" | awk '
    /^providers:$/ {inp=1; next}
    {
      if (inp && $0 ~ /^[^ ]/) inp=0
      if (inp && $0 ~ /^    [a-z]+:$/) { cur=$1; sub(":","",cur); next }
      if (inp && cur != "" && $0 ~ /^        api_key:/) {
        v=$0; sub(/^ *api_key: */,"",v); gsub(/"/,"",v)
        print cur "=" (v=="" ? "0" : "1")
      }
    }')
  for kv in $KEYS; do
    case "$kv" in
      fish=1) KEY_FISH=true ;;
      cartesia=1) KEY_CARTESIA=true ;;
      minimax=1) KEY_MINIMAX=true ;;
    esac
  done
fi

KEY_FOR_DEFAULT=false
case "$DEFAULT_PROVIDER" in
  fish) KEY_FOR_DEFAULT=$KEY_FISH ;;
  cartesia) KEY_FOR_DEFAULT=$KEY_CARTESIA ;;
  minimax) KEY_FOR_DEFAULT=$KEY_MINIMAX ;;
esac

# ---------- 默认音色（@default 别名）----------

DEFAULT_VOICE_SET=false
DEFAULT_VOICE_ID=""
if [ -n "$BIN_PATH" ] && [ -n "$DEFAULT_PROVIDER" ]; then
  # 用 CLI 自己解析别名最权威：--dry-run 不发请求也不需要密钥
  if allen-tts speak -p "$DEFAULT_PROVIDER" -v @default --dry-run "probe" >/dev/null 2>&1; then
    DEFAULT_VOICE_SET=true
    DEFAULT_VOICE_ID=$(awk -v p="${DEFAULT_PROVIDER}:" '
      /^default:/ {ind=1; next}
      ind && /^[^ ]/ {ind=0}
      ind && $1==p {print $2; exit}
    ' "$VOICES_PATH" 2>/dev/null || true)
  fi
fi

# ---------- 技能版本检查（带缓存、限时、失败静默）----------

NOW=$(date +%s 2>/dev/null || echo 0)
LATEST=""
if [ -f "$CACHE_PATH" ]; then
  CACHED_TS=0; CACHED_VER=""
  read -r CACHED_TS CACHED_VER < "$CACHE_PATH" 2>/dev/null || true
  case "$CACHED_TS" in
    ''|*[!0-9]*) CACHED_TS=0 ;;
  esac
  if [ "$((NOW - CACHED_TS))" -lt "$CHECK_TTL" ]; then
    LATEST="$CACHED_VER"
  fi
fi
if [ -z "$LATEST" ] && command -v curl >/dev/null 2>&1; then
  # 只读版本号字符串，不执行任何远端内容；安装仍然锁定 tag
  LATEST=$(curl -fsSL --max-time 5 "$VERSION_URL" 2>/dev/null | tr -d ' \t\r\n' || true)
  case "$LATEST" in
    ''|*[!0-9.]*) LATEST="" ;;
  esac
  if [ -n "$LATEST" ]; then
    mkdir -p "$CONFIG_DIR" 2>/dev/null || true
    printf '%s %s\n' "$NOW" "$LATEST" > "$CACHE_PATH" 2>/dev/null || true
  fi
fi

UPDATE_AVAILABLE=false
if [ -n "$LATEST" ] && [ "$SKILL_VERSION" != "unknown" ] && [ "$LATEST" != "$SKILL_VERSION" ]; then
  [ "$(ver_ge "$LATEST" "$SKILL_VERSION")" = "1" ] && UPDATE_AVAILABLE=true
fi

# ---------- 汇总 ----------

MISSING=""
add_missing() { MISSING="${MISSING}${MISSING:+,}$(json_str "$1")"; }

[ -z "$BIN_PATH" ] && add_missing "binary"
[ "$CLI_OK" = false ] && add_missing "cli_too_old"
if [ -n "$BIN_PATH" ]; then
  [ -z "$DEFAULT_PROVIDER" ] && add_missing "default_provider"
  [ -n "$DEFAULT_PROVIDER" ] && [ "$KEY_FOR_DEFAULT" = false ] && add_missing "api_key"
  [ -n "$DEFAULT_PROVIDER" ] && [ "$DEFAULT_VOICE_SET" = false ] && add_missing "default_voice"
fi

READY=false
[ -z "$MISSING" ] && READY=true

printf '{\n'
printf '  "ready": %s,\n' "$READY"
printf '  "missing": [%s],\n' "$MISSING"
printf '  "skill_version": %s,\n' "$(json_str "$SKILL_VERSION")"
printf '  "binary": { "installed": %s, "path": %s, "version": %s, "meets_min_version": %s, "min_required": %s },\n' \
  "$([ -n "$BIN_PATH" ] && echo true || echo false)" \
  "$(json_str "$BIN_PATH")" "$(json_str "$CLI_VERSION")" "$CLI_OK" "$(json_str "$MIN_CLI_VERSION")"
printf '  "config": { "exists": %s, "path": %s },\n' "$CONFIG_EXISTS" "$(json_str "$CONFIG_PATH")"
printf '  "default_provider": %s,\n' "$(json_str "$DEFAULT_PROVIDER")"
printf '  "api_key_set": { "fish": %s, "cartesia": %s, "minimax": %s },\n' "$KEY_FISH" "$KEY_CARTESIA" "$KEY_MINIMAX"
printf '  "default_voice": { "configured": %s, "id": %s },\n' "$DEFAULT_VOICE_SET" "$(json_str "$DEFAULT_VOICE_ID")"
printf '  "go_available": %s,\n' "$GO_AVAILABLE"
printf '  "update_available": %s' "$UPDATE_AVAILABLE"
if [ "$UPDATE_AVAILABLE" = true ]; then
  printf ',\n  "latest_skill_version": %s,\n' "$(json_str "$LATEST")"
  printf '  "release_notes": %s\n' "$(json_str "https://github.com/${REPO}/releases")"
else
  printf '\n'
fi
printf '}\n'
