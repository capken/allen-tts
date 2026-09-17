#!/usr/bin/env sh
# 幂等地写入默认厂商与默认音色别名。
#
#   configure.sh --provider minimax --voice male-qn-qingse [--language yue]
#
# 只写 config.yaml 的 default_provider 与 voices.yaml 的 @default 别名。
# 绝不写入 API 密钥——密钥只走环境变量，配置里只保留 ${ENV} 引用。
set -eu

PROVIDER=""; VOICE=""; LANGUAGE=""

while [ $# -gt 0 ]; do
  case "$1" in
    --provider) PROVIDER="${2:-}"; shift 2 ;;
    --voice)    VOICE="${2:-}";    shift 2 ;;
    --language) LANGUAGE="${2:-}"; shift 2 ;;
    *) echo "未知参数: $1" >&2; exit 2 ;;
  esac
done

[ -n "$PROVIDER" ] || { echo "error: 必须指定 --provider" >&2; exit 2; }
case "$PROVIDER" in
  fish|cartesia|minimax) ;;
  *) echo "error: --provider 只能是 fish | cartesia | minimax" >&2; exit 2 ;;
esac
command -v allen-tts >/dev/null 2>&1 || { echo "error: 未找到 allen-tts，请先安装 CLI" >&2; exit 2; }

if [ -n "${ALLEN_TTS_CONFIG:-}" ]; then
  CONFIG_PATH="$ALLEN_TTS_CONFIG"
else
  CONFIG_PATH="${HOME}/.allen-tts/config.yaml"
fi
CONFIG_DIR=$(dirname "$CONFIG_PATH")
VOICES_PATH="${CONFIG_DIR}/voices.yaml"
mkdir -p "$CONFIG_DIR"

# ---------- config.yaml ----------

if [ ! -f "$CONFIG_PATH" ]; then
  allen-tts config init >/dev/null
  echo "已生成配置模板 ${CONFIG_PATH}"
fi

tmp="${CONFIG_PATH}.tmp.$$"
awk -v v="$PROVIDER" '
  /^default_provider:/ { print "default_provider: " v; done=1; next }
  { print }
  END { if (!done) print "default_provider: " v }
' "$CONFIG_PATH" > "$tmp"
mv "$tmp" "$CONFIG_PATH"
echo "默认厂商已设为 ${PROVIDER}"

# ---------- voices.yaml 的 @default 别名 ----------

# 在 default: 块内 upsert 一个 "  <key>: <value>" 字段，其余内容原样保留
upsert_default_field() {
  _file="$1"; _key="$2"; _val="$3"
  [ -f "$_file" ] || : > "$_file"
  _tmp="${_file}.tmp.$$"
  awk -v k="$_key" -v val="$_val" '
    function flush_block() {
      if (inblk && !done) { print "  " k ": " val; done=1 }
      inblk = 0
    }
    /^default:[ \t]*$/ { seen=1; inblk=1; print; next }
    {
      if (inblk && $0 ~ /^[^ \t]/) flush_block()
      if (inblk && $1 == k ":") { print "  " k ": " val; done=1; next }
      print
    }
    END {
      flush_block()
      if (!seen) { print "default:"; print "  " k ": " val }
    }
  ' "$_file" > "$_tmp"
  mv "$_tmp" "$_file"
}

# 从 default: 块里删掉某个字段（用于清除不再适用的旧值）
remove_default_field() {
  _file="$1"; _key="$2"
  [ -f "$_file" ] || return 0
  _tmp="${_file}.tmp.$$"
  awk -v k="$_key" '
    /^default:[ \t]*$/ { inblk=1; print; next }
    {
      if (inblk && $0 ~ /^[^ \t]/) inblk=0
      if (inblk && $1 == k ":") next
      print
    }
  ' "$_file" > "$_tmp"
  mv "$_tmp" "$_file"
}

if [ -n "$VOICE" ]; then
  upsert_default_field "$VOICES_PATH" "$PROVIDER" "$VOICE"
  upsert_default_field "$VOICES_PATH" "description" "技能默认音色"
  # language 是声明式的：没传就清掉旧值，避免换音色后残留不适用的语言设定
  if [ -n "$LANGUAGE" ]; then
    upsert_default_field "$VOICES_PATH" "language" "$LANGUAGE"
  else
    remove_default_field "$VOICES_PATH" "language"
  fi
  echo "默认音色 @default（${PROVIDER}）已设为 ${VOICE}"
fi

# ---------- 验证 ----------

if [ -n "$VOICE" ]; then
  if allen-tts speak -p "$PROVIDER" -v @default --dry-run "probe" >/dev/null 2>&1; then
    echo "校验通过：@default 在 ${PROVIDER} 下可正常解析"
  else
    echo "warning: @default 别名写入后仍无法解析，请检查 ${VOICES_PATH}" >&2
    exit 1
  fi
fi
