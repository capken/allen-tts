# allen-tts

面向 Agent 的统一多厂商 TTS 命令行工具。封装 **Fish Audio** / **Cartesia** / **MiniMax** 三家 TTS HTTP API，对外暴露一套统一参数：agent 只需学习一套参数，换厂商只改 `-p`。

设计文档见 [docs/DESIGN.md](docs/DESIGN.md)。

## 安装

单二进制，无运行时依赖。三选一：

```bash
# 1) 一键安装脚本（推荐）：自动识别平台、校验 sha256
curl -fsSL https://raw.githubusercontent.com/capken/allen-tts/main/scripts/install.sh | sh

# 2) 有 Go 工具链
go install github.com/capken/allen-tts/cmd/allen-tts@latest

# 3) 从源码构建
make build          # 产出 bin/allen-tts
make cross          # 交叉编译 darwin/linux × amd64/arm64
```

安装脚本默认装到 `/usr/local/bin`，不可写时回退到 `~/.local/bin`（不会静默提权）。
可用 `PREFIX=~/bin` 指定目录、`VERSION=v0.1.0` 指定版本。
也可以直接从 [Releases](https://github.com/capken/allen-tts/releases) 下载对应平台的 tar.gz。

## 快速开始

```bash
export MINIMAX_API_KEY=...     # 或 FISH_API_KEY / CARTESIA_API_KEY

allen-tts speak -p minimax --voice male-qn-qingse --json "你好，世界"
allen-tts speak -p fish     --voice 7f92f8afb8ec43bf81429cc1c9199cb1 "Hello"
allen-tts speak -p cartesia --voice db6b0ed5-... --lang en "Hello"
```

统一参数（三家通用，量纲统一）：

| 参数 | 含义 | 统一量纲 |
|---|---|---|
| `--speed` | 语速 | 倍数 [0.5, 2.0] |
| `--volume` | 音量 | 倍数 [0.5, 2.0]（Fish 的 dB 由工具换算） |
| `--pitch` | 音调 | 半音 [-12, 12] |
| `--emotion` | 情绪 | neutral/happy/sad/angry/fearful/surprised/calm/excited |
| `--lang` | 语言 | BCP-47（zh / en / yue / …） |
| `--format` | 格式 | mp3 / wav / pcm / opus / flac |
| `--bitrate` | 码率 | kbps |

厂商不支持的字段按 `--on-unsupported=warn|error|drop` 降级（默认 warn：丢弃/裁剪并写入 warnings）。**格式不支持永远报错**，不会静默换格式。厂商特有能力走 `--extra '<json>'` 深合并进原始 payload。

## Agent 调用范式

1. **先查能力**：`allen-tts capabilities -p <provider> --json`
2. **再合成**：`allen-tts speak -p <provider> --json ...`
   - stdout 是单个 JSON：成功含 `output`（绝对路径）、`duration_ms`、`warnings` 等；失败含 `error.code` 与 `error.retryable`。
3. **按错误处理**：`retryable: true`（rate_limit/quota/provider_error/network）→ 重试或换厂商；`false`（invalid_argument/auth/invalid_text）→ 修正参数。
4. **调试映射**：`--dry-run` 打印脱敏后的最终厂商 HTTP 请求。

退出码：`0` 成功，`2` 参数错误，`3` 鉴权，`4` 限流/欠费，`5` 文本被拒，`6` 厂商服务端错误，`7` 网络/超时。

## 配置

```bash
allen-tts config init     # 生成 ~/.allen-tts/config.yaml 模板
allen-tts config show     # 显示合并后的有效配置（密钥打码）
allen-tts config check    # 校验各家密钥可用
```

参数优先级：`CLI 参数 > 环境变量 > ~/.allen-tts/config.yaml > adapter 默认值`。

环境变量：`FISH_API_KEY` `CARTESIA_API_KEY` `MINIMAX_API_KEY` `ALLEN_TTS_PROVIDER` `ALLEN_TTS_CONFIG` `MINIMAX_BASE_URL`。

### 音色别名（~/.allen-tts/voices.yaml）

一个别名映射多家真实音色 ID，切厂商只改 `-p`：

```yaml
narrator:
  fish: 7f92f8afb8ec43bf81429cc1c9199cb1
  cartesia: db6b0ed5-d5d3-463d-ae85-518a07d3c2b4
  minimax: male-qn-qingse
  description: 中性男声，适合旁白
host:
  minimax: Cantonese_podacast_host_1
  language: yue          # 别名可携带默认语言
```

```bash
allen-tts speak -p minimax -v @narrator "你好"
```

## 命令

| 命令 | 说明 |
|---|---|
| `speak [flags] [TEXT]` | 合成。文本来源：位置参数 > `--text-file` > stdin |
| `voices list -p X` | 列音色（MiniMax / Cartesia；Fish v1 未实现）+ 本地别名 |
| `models -p X` | 列模型（静态表） |
| `capabilities -p X` | 输出能力声明（JSON） |
| `config init/show/check` | 配置管理 |
| `voice create` | 预留（v1 未实现） |

## 流式

- `--stream -o -`：逐 chunk 写 stdout；`--stream -o file`：逐 chunk 写文件。
- Fish：响应天然 chunked；MiniMax：SSE + hex 解码；Cartesia：v1 不支持，自动退化为非流式并 warn。

## 开发

```bash
make test    # 单元测试（映射、clamp、响应解析、SSE、错误码）
make vet
```

### 发布

推一个 `v*` tag 即触发 [release workflow](.github/workflows/release.yml)：先跑 `go vet` + 全量测试作为门禁，
再交叉编译 darwin/linux × amd64/arm64，带 sha256 校验文件发到 GitHub Releases。

```bash
git tag v0.1.0 && git push origin v0.1.0
```

版本号通过 `-ldflags` 注入；`go install` 等未注入的场景回退到 module 版本（`runtime/debug.ReadBuildInfo`）。
重跑同一个 tag 会覆盖产物而不是失败，也可在 Actions 页面手动触发并指定 tag。

## 作为 Agent 技能使用

仓库里的 [`skills/allen-tts/`](skills/allen-tts/) 是一个 agent 技能，让 agent 能引导用户完成
安装与初始化，之后直接按需求调用本工具合成语音。它是纯 markdown 加 shell 脚本，
不依赖任何特定 agent 的打包格式。

### 安装：把下面这段话发给你的 agent

```text
请帮我安装 allen-tts 技能（一个多厂商 TTS 命令行工具的使用技能）。

1. 获取 v0.2.0 版本的技能目录，二选一：
   git clone --depth 1 --branch v0.2.0 https://github.com/capken/allen-tts /tmp/allen-tts-skill
   或者下载 https://github.com/capken/allen-tts/archive/refs/tags/v0.2.0.tar.gz 后解压。

2. 把其中的 skills/allen-tts/ 整个目录复制到你加载技能的位置，目录名保持 allen-tts。
   Claude Code 放到 ~/.claude/skills/allen-tts/；其他 agent 放到你自己的技能或规则目录。
   如果你的技能格式和 SKILL.md 的 frontmatter 不一致，请做相应转换，但不要改动正文内容。

3. 给 scripts/ 目录下的 .sh 文件加上可执行权限。

4. 装完后告诉我这几件事，让我确认装进来的是什么：
   - 技能所在的绝对路径
   - 目录下的文件清单
   - VERSION 文件的内容
   - SKILL.md 中 description 字段的原文
   - SKILL.md 正文的要点摘要，特别是它会让你执行哪些命令

5. 在我确认之前，不要执行技能里的任何脚本。
```

第 4、5 步是有意加的：安装技能等于往你的机器里放入会被后续对话自动加载的指令，
应该先看清内容再启用。同理，上面锁定了 `v0.2.0` 这个 tag 而不是 `main`，
这样你和别人装到的是同一份东西。

### 更新

技能每次运行时会做一次带缓存的版本检查（每天最多一次，网络不通就跳过）。
有新版本时它会在完成当前任务后提示你，并附上 release notes 链接，由你决定是否更新。
它不会自动更新自己。

更新就是按新的 tag 重跑上面那段安装流程、覆盖旧目录即可——技能目录是无状态的，
你的配置、密钥引用和音色别名都在 `~/.allen-tts/` 下，不会丢失。

### 版本号说明

技能有自己的版本（`skills/allen-tts/VERSION`），和命令行工具的版本、仓库 tag 是**相互独立**的。
这样只改命令行工具的发版不会让所有用户收到"技能有更新"的误报。
更新检查比对的是技能自己的版本号。

### 技能目录结构

```
skills/allen-tts/
├── SKILL.md                    # 主流程：先探测状态，再分支到引导或合成
├── VERSION                     # 技能版本，用于更新检查
├── scripts/
│   ├── preflight.sh            # 一次性探测全部状态，输出 JSON
│   └── configure.sh            # 幂等写入默认服务商与默认音色别名
└── references/
    ├── setup.md                # 四步引导：安装、选服务商、配密钥、选音色
    ├── usage.md                # 合成参数、三家能力差异、长文本策略
    └── troubleshooting.md      # 退出码与常见现象的处置
```

技能本身不保存任何密钥。密钥只通过环境变量提供，配置文件里仅保留 `${ENV}` 引用。

## v1 已知边界（见设计文档第 1.2 / 10 节）

- 不支持 WebSocket 双向流式（Cartesia 流式待后续版本）。
- 音色克隆（`voice create`）未实现。
- retryable 错误的自动指数退避重试计划在 M4。
