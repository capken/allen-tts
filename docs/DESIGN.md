# allen-tts 设计文档

> 面向 Agent 的统一多厂商 TTS 命令行工具。封装 Fish Audio / Cartesia / MiniMax 三家 TTS HTTP API，对外暴露一套统一参数，屏蔽各家在参数命名、量纲、位置、响应形态上的差异。
>
> 本文档面向实现者（Claude Code）。所有"必须 / 禁止 / 应当"为实现约束。

---

## 1. 项目概述

### 1.1 目标

- Agent 只需学习一套参数，即可在三家 TTS 之间自由切换（`-p fish|cartesia|minimax`）。
- 输入输出契约稳定、机器可读：音频落文件或 stdout，元数据以 JSON 输出，日志走 stderr，退出码有语义。
- 每个厂商特有能力都可通过逃生口（`--extra`）直接使用，CLI 不成为能力瓶颈。
- 单二进制分发，无运行时依赖。

### 1.2 非目标（v1 不做）

- 不统一各家的文本内嵌标记（说话人标签、停顿标记、语气词），v1 原样透传。
- 不做音色克隆 / 音色管理的完整封装（仅预留子命令）。
- 不做音频后处理（拼接、变速、格式转码）。
- 不做 WebSocket 双向流式；仅支持 HTTP 单向流式。

### 1.3 技术选型

- **语言**：Go 1.22+（单二进制、交叉编译方便、agent 环境无需安装运行时）。
- **CLI 框架**：`spf13/cobra`；配置解析用 `gopkg.in/yaml.v3`。
- **HTTP**：标准库 `net/http`，不引入重型 SDK；每家 adapter 自己拼请求体。
- **依赖原则**：尽量少依赖；MiniMax 的 hex 解码、SSE 解析全部用标准库实现。

---

## 2. 三家 API 差异分析（设计依据）

| 维度 | Fish Audio `/v1/tts` | Cartesia `/tts/bytes` | MiniMax `/v1/t2a_v2` |
|---|---|---|---|
| Base URL | `https://api.fish.audio` | `https://api.cartesia.ai` | `https://api.minimax.cn`（备用 `https://api-bj.minimaxi.com`） |
| 鉴权 | `Authorization: Bearer` | `Authorization: Bearer` + **必填** `Cartesia-Version: 2026-08-14` | `Authorization: Bearer` |
| 模型选择 | **HTTP header** `model`：`s1` / `s2-pro` / `s2.1-pro`(默认) / `s2.1-pro-free` / `drama-3-preview` | body `model_id`：`sonic-3.6`(默认) / `sonic-3.5` / `sonic-3` / `sonic-latest` | body `model`：`speech-2.8-hd` / `speech-2.8-turbo` / `speech-2.6-*` / `speech-02-*` / `speech-01-*` |
| 文本字段 | `text` | `transcript` | `text`（<10000 字符，>3000 推荐流式） |
| 音色 | `reference_id`：string 或 string[]（多说话人）；零样本克隆需 msgpack 内联 `references` | `voice`：string 或 `{id}` | `voice_setting.voice_id`；另有 `timbre_weights[]` 混音 |
| 语速 | `prosody.speed` [0.5, 2.0] | `generation_config.speed` **[0.6, 1.5]** | `voice_setting.speed` [0.5, 2.0] |
| 音量 | `prosody.volume` **dB**，0 = 不变 | `generation_config.volume` **倍数** [0.5, 2.0] | `voice_setting.vol` **倍数** (0, 10] |
| 音调 | — | — | `voice_setting.pitch` [-12, 12] 半音 |
| 情绪 | —（用 `temperature` / `top_p` 控表现力） | `generation_config.emotion`，约 60 个枚举 | `voice_setting.emotion`：happy/sad/angry/fearful/disgusted/surprised/calm/fluent/whisper（部分随模型版本变化） |
| 语言 | — | `language` 或 `locale`（**二选一，同设返回 400**），BCP-47 | `language_boost`：英文语言名（`Chinese` / `Chinese,Yue` / `English` / … / `auto`） |
| 输出格式 | 扁平：`format`(wav/pcm/mp3/opus) + `sample_rate` + `mp3_bitrate`(**kbps**: 64/128/192) + `opus_bitrate`(**bps**) | 嵌套 `output_format{container(wav/mp3/raw), encoding, sample_rate, bit_rate(**bps**)}` | 嵌套 `audio_setting{format(mp3/pcm/flac/wav/pcmu_raw/pcmu_wav/opus), sample_rate, bitrate(**bps**), channel(1/2)}` |
| 采样率枚举 | 8k/16k/24k/32k/44.1k（wav/pcm）；32k/44.1k（mp3）；48k（opus） | 8k/16k/22.05k/24k/44.1k/48k | 8k/16k/22.05k/24k/32k(默认)/44.1k |
| 响应体 | 原始音频字节（chunked） | 原始音频字节 | **JSON**，`data.audio` 为 hex 字符串；`output_format=url` 时返回 URL；错误藏在 HTTP 200 的 `base_resp.status_code` |
| 流式 | 天然 chunked | 独立 SSE / WS 端点（v1 不支持） | 同端点 `stream:true` → SSE，每 chunk 含 hex 片段 |
| 特有能力 | `latency` 档位、采样参数、多说话人标签 `<\|speaker:n\|>`、`chunk_length` 等 | `pronunciation_dict_id`、`normalization` 模式、`accent` | 停顿标记 `<#x#>`、语气词 `(laughs)`、`pronunciation_dict.tone[]`、字幕、`voice_modify` 效果器、`aigc_watermark` |

**三个真正需要设计的难点**：
1. 音量量纲不一致（dB vs 倍数）。
2. 参数位置不一致（header / 扁平 / 嵌套）。
3. 响应形态不一致（原始字节 vs JSON+hex+软错误）。

其余差异都是改名、单位换算、范围裁剪。

---

## 3. 架构

```
┌──────────────────────────────────────────────────┐
│  cmd/allen-tts          cobra 命令层               │
│  speak / voices / models / capabilities / config  │
└───────────────┬──────────────────────────────────┘
                │ SpeakRequest（统一参数）
┌───────────────▼──────────────────────────────────┐
│  internal/core                                    │
│  - 参数校验与归一化（validate.go）                   │
│  - 能力检查与降级（capability.go）                   │
│  - 音色别名解析（alias.go）                          │
│  - 输出写入（stdout / file / stream）               │
│  - 统一错误类型（errors.go）                         │
└───────────────┬──────────────────────────────────┘
                │ Provider 接口
┌───────────────▼──────────────────────────────────┐
│  internal/provider                                │
│  ├─ fish/      header model + 扁平 body            │
│  ├─ cartesia/  Cartesia-Version + 嵌套 output_format│
│  └─ minimax/   JSON+hex 解码 + base_resp 错误映射    │
└──────────────────────────────────────────────────┘
```

### 3.1 Provider 接口

```go
package provider

type Provider interface {
    Name() string
    Capabilities() Capabilities
    // 把统一请求翻译成厂商请求并执行；返回音频流与元数据。
    // 实现必须做：单位换算、范围裁剪（写入 warnings）、不支持字段的 drop。
    Speak(ctx context.Context, req core.SpeakRequest) (*Result, error)
    ListVoices(ctx context.Context) ([]Voice, error)   // v1 可返回 ErrNotImplemented
    ListModels() []ModelInfo                            // 静态表即可
}

type Result struct {
    Audio      io.ReadCloser   // 已解码为原始音频字节（MiniMax 需 hex 解码后再给出）
    Format     string
    SampleRate int
    Bitrate    int             // kbps，未知则 0
    Channels   int
    DurationMS int64           // 未知则 0
    Usage      Usage           // 计费字符数等，未知则零值
    RequestID  string
    Warnings   []string
    Extras     map[string]any  // 厂商特有返回，如 MiniMax subtitle_file
}
```

### 3.2 Capabilities 声明

每个 adapter 以静态结构声明能力，core 层据此校验、裁剪、降级；`allen-tts capabilities` 命令直接序列化该结构。

```go
type Capabilities struct {
    Models        []ModelInfo
    DefaultModel  string
    Formats       []string           // 统一格式名
    SampleRates   map[string][]int   // format -> 合法采样率
    Bitrates      []int              // kbps
    Speed         *Range             // nil = 不支持
    Volume        *Range             // 统一倍数量纲
    Pitch         *Range
    Emotions      []string           // 空 = 不支持
    Language      bool
    Channels      []int
    Latency       bool
    Stream        bool
    MultiSpeaker  bool
}
```

---

## 4. 统一参数（SpeakRequest）

```go
type SpeakRequest struct {
    Provider   string  // 必填（或来自配置默认值）
    Model      string  // 可选，缺省用 adapter 默认
    Text       string  // 必填

    Voice      string  // 音色 ID 或 @别名
    Language   string  // BCP-47，如 zh / en / zh-HK / yue

    Speed      float64 // 倍数，统一范围 [0.5, 2.0]，默认 1.0
    Volume     float64 // 倍数，1.0 = 不变，统一范围 [0.5, 2.0]，默认 1.0
    Pitch      int     // 半音 [-12, 12]，默认 0
    Emotion    string  // 见 4.1

    Format     string  // mp3 | wav | pcm | opus | flac，默认 mp3
    SampleRate int     // Hz；0 = 用厂商默认
    Bitrate    int     // kbps；0 = 用厂商默认
    Channels   int     // 1 | 2，默认 1

    Latency    string  // quality | balanced | low，默认 quality
    Stream     bool

    Extra      map[string]any // 深合并进厂商原始 payload，不校验
}
```

### 4.1 统一情绪枚举

`neutral` `happy` `sad` `angry` `fearful` `surprised` `calm` `excited`

- 各 adapter 负责映射到自家枚举（见第 7 节）。
- 不在统一枚举内的值：对支持 emotion 的厂商**原样透传**（Cartesia 枚举很广，MiniMax 有 `whisper`/`fluent`）；对不支持的厂商 drop + warn。

### 4.2 参数优先级

```
CLI 参数  >  环境变量  >  ~/.allen-tts/config.yaml  >  adapter 默认值
```

### 4.3 降级策略

`--on-unsupported=warn|error|drop`，默认 `warn`：

| 情况 | warn（默认） | error | drop |
|---|---|---|---|
| 字段厂商不支持（如 Fish 收到 pitch） | 丢弃并写入 warnings | 退出码 2 | 静默丢弃 |
| 数值超出厂商范围（如 Cartesia speed=1.8） | clamp 到边界并写入 warnings | 退出码 2 | 静默 clamp |
| 采样率不在枚举内 | 取最近合法值并写入 warnings | 退出码 2 | 静默取最近值 |
| 格式厂商不支持（如 Cartesia flac） | **始终报错**，退出码 2 | 同左 | 同左 |

格式不支持永远报错，因为静默换格式会导致 agent 拿到与文件扩展名不符的内容。

---

## 5. CLI 命令规范

命令名：`allen-tts`

### 5.1 `speak` — 合成

```
allen-tts speak [flags] [TEXT]

文本来源（优先级由高到低）：位置参数 TEXT > --text-file PATH > stdin

Flags:
  -p, --provider string     fish | cartesia | minimax
  -m, --model string
  -v, --voice string        音色 ID 或 @alias
      --lang string         BCP-47 语言码
      --speed float         默认 1.0
      --volume float        默认 1.0
      --pitch int           默认 0
      --emotion string
  -f, --format string       mp3 | wav | pcm | opus | flac（默认 mp3）
      --sample-rate int
      --bitrate int         kbps
      --channels int        默认 1
      --latency string      quality | balanced | low
      --stream              流式接收，边收边写
  -o, --output PATH         输出文件；"-" 表示 stdout；缺省 = 自动命名 ./tts-<ts>.<ext>
      --json                stdout 输出 JSON 元数据（此时音频必须写文件，-o 不能为 "-"）
      --extra JSON          深合并进厂商 payload
      --on-unsupported      warn | error | drop（默认 warn）
      --dry-run             不发请求，打印最终厂商 payload（含 header）并退出
      --timeout duration    默认 60s
```

约束：
- `--json` 与 `-o -` 互斥（stdout 只能承载一种内容），冲突时退出码 2。
- `--dry-run` 输出脱敏后的完整 HTTP 请求（method、url、headers（Authorization 打码）、body），供调试映射逻辑。
- `--stream` 且 `-o -` 时，逐 chunk 写 stdout；`--stream` 且 `-o file` 时逐 chunk 追加写文件。

### 5.2 `voices list` — 列音色

```
allen-tts voices list -p <provider> [--json]
```
v1 至少实现 MiniMax（有查询 API）与 Cartesia（List Voices）；Fish 可先返回 not implemented。同时输出本地 `voices.yaml` 中的别名。

### 5.3 `models` — 列模型

```
allen-tts models -p <provider> [--json]
```
静态表，包含 id、是否默认、备注（如"支持多说话人"）。

### 5.4 `capabilities` — 能力查询

```
allen-tts capabilities -p <provider> [--json]
```
输出第 3.2 节的 Capabilities 结构。Agent 应先查再调。

### 5.5 `config` — 配置管理

```
allen-tts config init            生成 ~/.allen-tts/config.yaml 模板
allen-tts config show            显示合并后的有效配置（密钥打码）
allen-tts config check [-p X]    校验密钥可用（发一个最小请求或调用 voices 接口）
```

### 5.6 `voice create` — 预留

v1 仅注册子命令并返回 `not implemented`，说明 Fish 克隆需 msgpack 内联 references，后续版本实现。

---

## 6. 输出契约

### 6.1 `--json` 成功输出（stdout，单行或缩进均可，必须是合法 JSON）

```json
{
  "ok": true,
  "provider": "minimax",
  "model": "speech-2.8-hd",
  "voice": "male-qn-qingse",
  "voice_alias": "@narrator",
  "output": "/abs/path/out.mp3",
  "format": "mp3",
  "sample_rate": 32000,
  "bitrate": 128,
  "channels": 1,
  "bytes": 160323,
  "duration_ms": 9900,
  "usage": { "characters": 26 },
  "request_id": "01b8bf9bb7433cc75c18eee6cfa8fe21",
  "warnings": ["pitch clamped from 15 to 12"],
  "extras": { "subtitle_url": "https://..." }
}
```

- 未知字段用零值或省略，**不要编造**（Fish/Cartesia 无 duration，输出 0 或省略）。
- `output` 必须是绝对路径。

### 6.2 `--json` 失败输出（stdout）+ 非零退出码

```json
{
  "ok": false,
  "error": {
    "code": "rate_limit",
    "provider": "minimax",
    "provider_code": "1039",
    "http_status": 200,
    "message": "触发 TPM 限流",
    "retryable": true
  }
}
```

### 6.3 非 `--json` 模式

- 成功：stderr 打印一行摘要（`wrote /abs/path/out.mp3 (160 KB, 9.9s)`），stdout 不输出（除非 `-o -`）。
- 失败：stderr 打印错误，非零退出。

### 6.4 退出码与统一错误码

| 退出码 | `error.code` | 含义 | 来源示例 |
|---|---|---|---|
| 0 | — | 成功 | |
| 2 | `invalid_argument` | 本地参数错误 | 缺 voice、格式不支持、`--json` 与 `-o -` 冲突 |
| 3 | `auth` | 鉴权失败 | Fish 401；MiniMax 1004 |
| 4 | `rate_limit` / `quota` | 限流或欠费 | Fish 402/503；MiniMax 1002/1039 |
| 5 | `invalid_text` | 文本被厂商拒绝 | MiniMax 1042（非法字符>10%）、2013 |
| 6 | `provider_error` | 厂商服务端错误 | 5xx；MiniMax 1000/1001 |
| 7 | `network` | 网络/超时 | |

`retryable`：4、6、7 为 true；2、3、5 为 false。

---

## 7. Adapter 映射规则

通用规则（三家都适用）：
- 先应用 `Extra` 之前的所有统一映射，最后把 `Extra` **深合并**（对象递归合并，标量覆盖，数组覆盖）进 payload。Extra 里的字段不做任何校验。
- 所有 clamp / drop / 取最近值都必须追加人类可读 warning。
- 请求必须带 `User-Agent: allen-tts/<version>`。

### 7.1 Fish Audio

```
POST https://api.fish.audio/v1/tts
Headers:
  Authorization: Bearer $FISH_API_KEY
  Content-Type: application/json
  model: <Model 或默认 s2.1-pro>
```

| 统一字段 | 目标 | 规则 |
|---|---|---|
| Text | `text` | 直传 |
| Voice | `reference_id` | 直传字符串；若包含逗号则 split 为数组（多说话人） |
| Language | — | drop + warn |
| Speed | `prosody.speed` | clamp [0.5, 2.0] |
| Volume | `prosody.volume` | `20 * log10(Volume)`，保留 1 位小数；Volume=1.0 → 0 |
| Pitch | — | drop + warn |
| Emotion | — | drop + warn |
| Format | `format` | mp3/wav/pcm/opus 直传；flac → error |
| SampleRate | `sample_rate` | 按 format 取最近合法值（见第 2 节枚举）；0 则不写该字段 |
| Bitrate | `mp3_bitrate` | format=mp3 时写，取最近的 64/128/192（kbps 直传）|
| Bitrate | `opus_bitrate` | format=opus 时写 `Bitrate*1000`，取最近的 24000/32000/48000/64000 |
| Channels | — | 只支持 1；2 → drop + warn |
| Latency | `latency` | quality→`normal`，balanced→`balanced`，low→`low` |
| Stream | — | 响应本身 chunked，逐块写即可 |

响应：HTTP 200 → body 即音频字节。401→auth，402→quota，503→rate_limit，其他 4xx→invalid_argument（附 message），5xx→provider_error。无 duration/usage，置零。

### 7.2 Cartesia

```
POST https://api.cartesia.ai/tts/bytes
Headers:
  Authorization: Bearer $CARTESIA_API_KEY
  Cartesia-Version: 2026-08-14
  Content-Type: application/json
```

| 统一字段 | 目标 | 规则 |
|---|---|---|
| Model | `model_id` | 默认 `sonic-3.6` |
| Text | `transcript` | 直传 |
| Voice | `voice` | 直传字符串（不要包成对象） |
| Language | `language` | 直传 BCP-47；**永远不写 `locale`** |
| Speed | `generation_config.speed` | clamp [0.6, 1.5] |
| Volume | `generation_config.volume` | clamp [0.5, 2.0] |
| Pitch | — | drop + warn |
| Emotion | `generation_config.emotion` | 统一枚举映射：neutral→neutral, happy→happy, sad→sad, angry→angry, fearful→scared, surprised→surprised, calm→calm, excited→excited；其他值原样透传 |
| Format=mp3 | `output_format` | `{container:"mp3", sample_rate, bit_rate: Bitrate*1000}`；sample_rate 默认 44100，bit_rate 默认 128000；bit_rate 取最近的 32k/64k/96k/128k/192k |
| Format=wav | `output_format` | `{container:"wav", encoding:"pcm_s16le", sample_rate}` |
| Format=pcm | `output_format` | `{container:"raw", encoding:"pcm_s16le", sample_rate}` |
| Format=opus/flac | — | error |
| SampleRate | 见上 | 取最近的 8000/16000/22050/24000/44100/48000 |
| Channels | — | 2 → drop + warn |
| Latency | — | drop（不 warn，Cartesia 本身低延迟） |
| Stream | — | v1 不支持流式；`--stream` 时 warn 并退化为非流式 |

`generation_config` 仅 sonic-3 及以上支持；若 Model 不以 `sonic-3` 开头且非 `sonic-latest`，不写 `generation_config` 并 warn。

响应：HTTP 200 → body 即音频字节。401/403→auth，402→quota，429→rate_limit，400→invalid_argument（透传 message），5xx→provider_error。

### 7.3 MiniMax

```
POST https://api.minimax.cn/v1/t2a_v2
Headers:
  Authorization: Bearer $MINIMAX_API_KEY
  Content-Type: application/json
```

| 统一字段 | 目标 | 规则 |
|---|---|---|
| Model | `model` | 默认 `speech-2.8-hd`；Latency=low 且用户未显式指定 model 时改为 `speech-2.8-turbo` |
| Text | `text` | 直传；长度 ≥10000 → 本地报 invalid_text |
| Voice | `voice_setting.voice_id` | 直传 |
| Language | `language_boost` | 映射表：`zh`/`zh-CN`→Chinese；`zh-HK`/`zh-TW`/`yue`→`Chinese,Yue`；`en`/`en-*`→English；`ja`→Japanese；`ko`→Korean；`fr`→French；`de`→German；`es`→Spanish；`pt`→Portuguese；`ru`→Russian；`ar`→Arabic；`th`→Thai；`vi`→Vietnamese；`id`→Indonesian；`it`→Italian；`auto`→auto；其他未知码 → drop + warn |
| Speed | `voice_setting.speed` | clamp [0.5, 2.0] |
| Volume | `voice_setting.vol` | clamp (0, 10]；统一范围内直传 |
| Pitch | `voice_setting.pitch` | clamp [-12, 12] |
| Emotion | `voice_setting.emotion` | neutral→calm；excited→fluent（仅 speech-2.6-* 支持，其他模型 drop+warn）；happy/sad/angry/fearful/surprised/calm 同名；`whisper` 透传但 speech-2.8-* 不支持时 drop+warn；其他值 drop + warn |
| Format | `audio_setting.format` | mp3/wav/pcm/opus/flac 直传 |
| SampleRate | `audio_setting.sample_rate` | 取最近的 8000/16000/22050/24000/32000/44100；0 → 不写（厂商默认 32000） |
| Bitrate | `audio_setting.bitrate` | `Bitrate*1000`，取最近的 32000/64000/128000/256000；仅 mp3 写 |
| Channels | `audio_setting.channel` | 1 或 2 直传 |
| Stream | `stream` | 直传；为 true 时额外写 `stream_options.exclude_aggregated_audio: true`（避免末 chunk 重复整段音频） |
| — | `output_format` | 固定 `hex`（不用 url 模式） |

响应处理（**关键**）：
1. HTTP 非 200 → 按状态码映射（401→auth，429→rate_limit，5xx→provider_error）。
2. HTTP 200 → 解析 JSON，先检查 `base_resp.status_code`：
   - `0` 正常
   - `1004` → auth
   - `1002`、`1039` → rate_limit
   - `1042`、`2013` → invalid_text / invalid_argument
   - `1000`、`1001` → provider_error
   - 其他非 0 → provider_error，`provider_code` 带原值
3. 非流式：`data.audio` hex 解码为字节；`extra_info.audio_length`→duration_ms，`audio_size`→bytes 校验，`usage_characters`→usage.characters，`audio_sample_rate`/`bitrate`/`audio_channel` 回填元数据；`data.subtitle_file` 若存在放入 `extras.subtitle_url`。
4. 流式（SSE，`text/event-stream`）：逐 event 解析 `data:` 行 JSON，`data.status==1` 的 chunk hex 解码后写出；`status==2` 为结束事件，读取其 `extra_info`（已设 exclude_aggregated_audio，其 audio 字段应为空或忽略）。
5. `trace_id` → request_id。

---

## 8. 配置与认证

### 8.1 环境变量

```
FISH_API_KEY
CARTESIA_API_KEY
MINIMAX_API_KEY
ALLEN_TTS_PROVIDER          默认厂商
ALLEN_TTS_CONFIG            配置文件路径，默认 ~/.allen-tts/config.yaml
MINIMAX_BASE_URL            可选，覆盖为备用地址
```

### 8.2 `~/.allen-tts/config.yaml`

```yaml
default_provider: minimax
on_unsupported: warn
timeout: 60s

providers:
  fish:
    api_key: ${FISH_API_KEY}        # 支持 ${ENV} 引用
    model: s2.1-pro
    format: mp3
  cartesia:
    api_key: ${CARTESIA_API_KEY}
    model: sonic-3.6
  minimax:
    api_key: ${MINIMAX_API_KEY}
    model: speech-2.8-hd
    base_url: https://api.minimax.cn

defaults:                            # 全局默认，可被 providers.<x> 覆盖
  format: mp3
  speed: 1.0
  volume: 1.0
```

### 8.3 音色别名 `~/.allen-tts/voices.yaml`

一个别名映射到多家真实音色 ID，agent 切换厂商时只改 `-p`。

```yaml
narrator:
  fish: 7f92f8afb8ec43bf81429cc1c9199cb1
  cartesia: db6b0ed5-d5d3-463d-ae85-518a07d3c2b4
  minimax: male-qn-qingse
  description: 中性男声，适合旁白

host:
  minimax: Cantonese_podacast_host_1
  language: yue                      # 别名可携带默认 language
```

解析规则：`--voice @narrator` → 查当前 provider 对应 ID；若该 provider 无映射 → 退出码 2，message 列出该别名可用的厂商。别名携带的 `language` 仅在用户未传 `--lang` 时生效。

---

## 9. 目录结构

```
allen-tts/
├── cmd/allen-tts/
│   └── main.go
├── internal/
│   ├── cli/                 # cobra 命令定义，只做参数解析与输出，不含业务逻辑
│   │   ├── root.go
│   │   ├── speak.go
│   │   ├── voices.go
│   │   ├── models.go
│   │   ├── capabilities.go
│   │   └── config.go
│   ├── core/
│   │   ├── request.go       # SpeakRequest 定义
│   │   ├── validate.go      # 本地校验、默认值填充
│   │   ├── capability.go    # 能力检查、clamp/drop、warnings
│   │   ├── alias.go         # voices.yaml 解析
│   │   ├── output.go        # 写文件 / stdout / 流式写
│   │   ├── errors.go        # 统一错误类型与退出码
│   │   └── merge.go         # Extra 深合并
│   ├── config/
│   │   └── config.go        # 三层配置合并，${ENV} 展开
│   └── provider/
│       ├── provider.go      # 接口、Capabilities、Result
│       ├── registry.go      # 按名字取 provider
│       ├── httpx/           # 共享 HTTP client、重试（仅对 retryable）、UA
│       ├── fish/
│       │   ├── fish.go
│       │   └── fish_test.go
│       ├── cartesia/
│       │   ├── cartesia.go
│       │   └── cartesia_test.go
│       └── minimax/
│           ├── minimax.go
│           ├── sse.go
│           └── minimax_test.go
├── testdata/                # 各家响应样本（含 MiniMax 错误 JSON、SSE 片段）
├── docs/
│   └── DESIGN.md            # 本文档
├── go.mod
├── Makefile                 # build / test / cross-compile
└── README.md
```

---

## 10. 实现里程碑

**M1 — 骨架与 MiniMax**
- cobra 骨架、配置加载、SpeakRequest 校验、错误/退出码。
- MiniMax adapter 非流式 + 流式，含 base_resp 软错误映射和 hex 解码。
- `speak --json`、`--dry-run`、`capabilities`。
- 验收：`allen-tts speak -p minimax --voice male-qn-qingse --json "你好"` 得到合法 mp3 与正确元数据。

**M2 — Fish 与 Cartesia**
- 两个 adapter，重点是 Fish 的 header model + dB 换算，Cartesia 的嵌套 output_format + 版本头。
- `models` 命令。
- 验收：同一条命令只换 `-p`，三家都能出音频；`--dry-run` 展示的 payload 与第 7 节表格一致。

**M3 — Agent 体验**
- 音色别名 `voices.yaml`、`voices list`（MiniMax、Cartesia）。
- `--on-unsupported` 三种策略。
- `config init/show/check`。
- README 写清 agent 调用范式（先 `capabilities` 再 `speak --json`，根据 `retryable` 决定重试/切厂商）。

**M4 — 打磨**
- 对 retryable 错误的指数退避重试（最多 3 次，可 `--no-retry` 关闭）。
- 交叉编译 darwin/linux × amd64/arm64，GitHub Release。
- `voice create` 子命令（Fish msgpack）。

---

## 11. 测试策略

- **映射单元测试**（最重要）：对每个 adapter，给定 SpeakRequest 断言生成的 payload 与 headers。覆盖：单位换算（volume→dB、kbps→bps）、clamp 边界、采样率取最近值、Extra 深合并覆盖、不支持字段 drop 且 warning 文案正确。
- **响应解析测试**：用 `testdata/` 中的样本，覆盖 MiniMax HTTP 200 + `status_code!=0`、SSE 流分块（含跨 chunk 边界的行）、Fish/Cartesia 各错误状态码。
- **HTTP mock**：`httptest.Server` 模拟三家端点，端到端跑 `speak`，断言退出码与 JSON 输出。
- **集成测试**（可选，需真实 key）：以 `-tags integration` 隔离，CI 默认不跑。
- 禁止在测试中写真实密钥；`--dry-run` 输出必须对 `Authorization` 打码（保留前 6 位）。

---

## 12. 附录：三家原始请求示例（供对照 `--dry-run` 输出）

### Fish
```http
POST /v1/tts HTTP/1.1
Host: api.fish.audio
Authorization: Bearer ****
Content-Type: application/json
model: s2.1-pro

{"text":"Hello","reference_id":"7f92...","prosody":{"speed":1.2,"volume":0.8},"format":"mp3","sample_rate":44100,"mp3_bitrate":128,"latency":"normal"}
```

### Cartesia
```http
POST /tts/bytes HTTP/1.1
Host: api.cartesia.ai
Authorization: Bearer ****
Cartesia-Version: 2026-08-14
Content-Type: application/json

{"model_id":"sonic-3.6","transcript":"Hello","voice":"db6b...","language":"en","output_format":{"container":"mp3","sample_rate":44100,"bit_rate":128000},"generation_config":{"speed":1.2,"volume":1.1,"emotion":"happy"}}
```

### MiniMax
```http
POST /v1/t2a_v2 HTTP/1.1
Host: api.minimax.cn
Authorization: Bearer ****
Content-Type: application/json

{"model":"speech-2.8-hd","text":"你好","stream":false,"output_format":"hex","voice_setting":{"voice_id":"male-qn-qingse","speed":1.2,"vol":1.1,"pitch":0,"emotion":"happy"},"audio_setting":{"sample_rate":32000,"bitrate":128000,"format":"mp3","channel":1},"language_boost":"Chinese"}
```

---

## 13. 开放问题（实现时如遇到请在 PR 中说明决策）

1. Cartesia 的 `sonic-latest` 是否总是 ≥ sonic-3，从而可写 `generation_config`？当前按"是"处理。
2. Fish 多说话人用逗号分隔 `--voice` 是否直观？备选：`--voice a --voice b` 重复 flag。
3. 统一 `Volume` 上限定为 2.0 是为了与 Cartesia 对齐；MiniMax 实际可到 10，用户如需更大值走 `--extra`。
4. 是否需要 `--seed`/`--temperature` 作为统一参数？目前认为只有 Fish 支持，暂放 `--extra`。
