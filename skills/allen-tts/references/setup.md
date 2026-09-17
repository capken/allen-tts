# 安装与初始化

按 preflight 输出的 `missing` 顺序处理，已经具备的步骤直接跳过。
全程只在必要时打断用户，能自动做的不要问。

## 第 1 步：安装命令行工具（missing 含 `binary` 或 `cli_too_old`）

首选一键脚本，它会自动识别平台、校验 sha256：

```
curl -fsSL https://raw.githubusercontent.com/capken/allen-tts/main/scripts/install.sh | sh
```

默认装到 `/usr/local/bin`，不可写时自动回退 `~/.local/bin`，**不会静默提权**。
若用户希望装到别处，用 `PREFIX=~/bin` 指定。

平台不是 macOS / Linux，或脚本取不到发布产物时，看 preflight 的 `go_available`：
为 true 就退回 `go install github.com/capken/allen-tts/cmd/allen-tts@latest`；
为 false 则如实告诉用户需要先装 Go 或手动从 Releases 页面下载，不要硬试。

升级（`cli_too_old`）跑同一条安装命令即可，会覆盖旧版本。

装完执行 `allen-tts --version` 确认。**如果提示 command not found，那通常不是安装失败而是
PATH 问题**——安装脚本已经打印了实际安装路径，把那个目录加进 PATH 即可。

## 第 2 步：选默认服务商（missing 含 `default_provider`）

问用户，并给出选择依据：

- **minimax** — 中文效果最好，支持情绪、音调、语速，返回音频时长、计费字数和字幕文件。中文场景推荐。
- **fish** — 音色库丰富，支持音色克隆与多说话人标记。
- **cartesia** — 英文低延迟，情绪枚举很广。中文支持较弱。

不要替用户决定。选完先别急着写配置，等第 4 步连同音色一起落盘。

## 第 3 步：配置 API 密钥（missing 含 `api_key`）

申请地址：minimax 在 platform.minimaxi.com，fish 在 fish.audio，cartesia 在 play.cartesia.ai。

**让用户自己把密钥写进 shell 配置**，比如在 `~/.zshrc` 或 `~/.bashrc` 里加：

```
export MINIMAX_API_KEY=...      # 另两家是 FISH_API_KEY / CARTESIA_API_KEY
```

加完需要新开终端或 `source` 一次才生效。

如果用户把密钥直接贴进了对话：不要复述它，不要把它写进任何文件，也不要写进命令行参数
（会留在 shell 历史里）。引导他们按上面的方式设环境变量。

设好后验证：

```
allen-tts config check -p <服务商>
```

输出 `OK` 才算过。报 auth 错就是密钥不对或没生效，先确认环境变量在当前 shell 里可见。

## 第 4 步：选默认音色（missing 含 `default_voice`）

拉取可选音色：

```
allen-tts voices list -p <服务商> --json
```

minimax 和 cartesia 有查询接口；**fish 的音色列表当前版本未实现**，需要用户自己从
fish.audio 网站上找到音色 ID 填进来。

列表可能有上百条，不要整个倒给用户。先问清用途（旁白、客服、儿童故事、英文播客……），
挑 3 到 5 个有代表性的推荐，并说明各自适合什么场景。

选定后写入配置：

```
scripts/configure.sh --provider <服务商> --voice <音色ID>
```

音色本身有固定语言时（比如粤语音色），额外带上 `--language yue`。
这个语言设定会跟着 `@default` 别名走，仅在用户没有显式指定语言时生效。
不确定就不要传——脚本对这个字段是声明式的，不传会清掉旧值。

## 验收

配置完跑一次真实合成，确认整条链路打通：

```
allen-tts speak -p <服务商> -v @default --json "配置完成，这是一段测试语音。"
```

把生成的文件路径给用户，请他们听一下确认音色符合预期。不满意就回到第 4 步换一个。

## 更新

preflight 报告 `update_available` 且用户明确同意后：

1. 按新版本的 tag 重新获取技能目录（参见仓库 README 的安装说明，把 tag 换成新版本号）。
2. 直接覆盖现有技能目录即可。**技能目录是无状态的**——用户的配置、密钥引用和音色别名
   都在 `~/.allen-tts/` 下，不会因为覆盖而丢失。
3. 更新后再跑一次 preflight。如果出现 `cli_too_old`，说明新版技能需要更高版本的命令行工具，
   回到第 1 步重跑安装命令。
