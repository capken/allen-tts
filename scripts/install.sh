#!/usr/bin/env sh
# allen-tts 安装脚本：下载对应平台的发布二进制、校验 sha256、装到 PATH 上。
#
#   curl -fsSL https://raw.githubusercontent.com/capken/allen-tts/main/scripts/install.sh | sh
#
# 环境变量：
#   VERSION   指定版本（默认取最新 Release），如 v0.1.0
#   PREFIX    安装目录（默认 /usr/local/bin，不可写则回退 ~/.local/bin）
set -eu

REPO="capken/allen-tts"
BIN="allen-tts"

die() { echo "error: $*" >&2; exit 1; }

command -v curl >/dev/null 2>&1 || die "需要 curl"
command -v tar  >/dev/null 2>&1 || die "需要 tar"

# 平台识别
os=$(uname -s | tr '[:upper:]' '[:lower:]')
case "$os" in
  darwin|linux) ;;
  *) die "暂不支持的系统: $os（仅提供 darwin / linux 预编译包，可改用 go install）" ;;
esac
arch=$(uname -m)
case "$arch" in
  x86_64|amd64) arch=amd64 ;;
  arm64|aarch64) arch=arm64 ;;
  *) die "暂不支持的架构: $arch（可改用 go install）" ;;
esac

# 版本解析
version="${VERSION:-}"
if [ -z "$version" ]; then
  version=$(curl -fsSL "https://api.github.com/repos/${REPO}/releases/latest" \
    | sed -n 's/.*"tag_name": *"\([^"]*\)".*/\1/p' | head -n1)
  [ -n "$version" ] || die "拿不到最新版本号，请设 VERSION=vX.Y.Z 重试"
fi

tarball="${BIN}_${version}_${os}_${arch}.tar.gz"
base="https://github.com/${REPO}/releases/download/${version}"

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT INT TERM

echo "下载 ${tarball} ..."
curl -fsSL -o "${tmp}/${tarball}" "${base}/${tarball}" \
  || die "下载失败，确认 ${version} 这个 Release 存在且包含 ${os}/${arch} 产物"

# 校验 sha256：校验文件拿不到就明确告知跳过，不静默放行
if curl -fsSL -o "${tmp}/checksums.txt" "${base}/checksums.txt" 2>/dev/null; then
  expected=$(sed -n "s|^\([0-9a-f]\{64\}\)  \./\{0,1\}${tarball}$|\1|p" "${tmp}/checksums.txt" | head -n1)
  if [ -z "$expected" ]; then
    echo "warning: checksums.txt 里没有 ${tarball} 的条目，跳过校验" >&2
  else
    if command -v sha256sum >/dev/null 2>&1; then
      actual=$(sha256sum "${tmp}/${tarball}" | cut -d' ' -f1)
    elif command -v shasum >/dev/null 2>&1; then
      actual=$(shasum -a 256 "${tmp}/${tarball}" | cut -d' ' -f1)
    else
      actual=""
      echo "warning: 没有 sha256sum/shasum，跳过校验" >&2
    fi
    if [ -n "$actual" ] && [ "$actual" != "$expected" ]; then
      die "sha256 校验失败：期望 $expected，实际 $actual"
    fi
    [ -n "$actual" ] && echo "sha256 校验通过"
  fi
else
  echo "warning: 取不到 checksums.txt，跳过校验" >&2
fi

tar -xzf "${tmp}/${tarball}" -C "$tmp"
[ -f "${tmp}/${BIN}" ] || die "压缩包里没有 ${BIN}"
chmod +x "${tmp}/${BIN}"

# 选安装目录：优先 PREFIX，其次可写的 /usr/local/bin，最后 ~/.local/bin。不偷偷用 sudo。
prefix="${PREFIX:-}"
if [ -z "$prefix" ]; then
  if [ -w /usr/local/bin ]; then
    prefix=/usr/local/bin
  else
    prefix="${HOME}/.local/bin"
    echo "/usr/local/bin 不可写，改装到 ${prefix}"
  fi
fi
mkdir -p "$prefix" || die "无法创建 $prefix"
[ -w "$prefix" ] || die "$prefix 不可写。可换目录：PREFIX=~/.local/bin sh install.sh，或自行 sudo cp ${tmp}/${BIN} /usr/local/bin/"

mv "${tmp}/${BIN}" "${prefix}/${BIN}"
echo "已安装 ${prefix}/${BIN}"
"${prefix}/${BIN}" --version

case ":${PATH}:" in
  *":${prefix}:"*) ;;
  *) echo ""
     echo "注意：${prefix} 不在 PATH 上，加一行到 shell 配置里："
     echo "  export PATH=\"\$PATH:${prefix}\"" ;;
esac

echo ""
echo "下一步：allen-tts config init  然后设置 API key。详见 README。"
