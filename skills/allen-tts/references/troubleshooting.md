# 排错

先看退出码，再看 `--json` 输出里的 `error.code` 和 `error.retryable`。
`retryable` 字段已经替你判断过能不能重试，直接用它，不要自己猜。

| 退出码 | error.code | 含义 | 该怎么做 |
|---|---|---|---|
| 2 | `invalid_argument` | 本地参数错误 | 改参数。重试没有意义 |
| 3 | `auth` | 密钥无效或缺失 | 回到 setup.md 第 3 步 |
| 4 | `rate_limit` / `quota` | 限流或欠费 | 退避后重试；持续失败考虑换服务商 |
| 5 | `invalid_text` | 文本被服务商拒绝 | 分段或清理文本，别直接重试 |
| 6 | `provider_error` | 厂商服务端错误 | 可重试；持续失败换服务商 |
| 7 | `network` | 网络错误或超时 | 可重试；检查网络，必要时调大 `--timeout` |

## 常见现象

**`command not found: allen-tts`**
多半不是没装，而是安装目录不在 PATH 上。安装脚本打印过实际路径，把那个目录加进 PATH。
也可以直接用完整路径调用先解开阻塞。

**`voice alias "@default" has no mapping for provider "X"`**
换了服务商但没给这家设过音色。回到 setup.md 第 4 步，为这家选一个。
`@default` 是一个跨服务商的别名，每家都需要单独映射。

**`X does not support format "flac"`**
格式不支持永远报错，不会静默替换。换个格式，或换一家支持的服务商。

**MiniMax 返回 HTTP 200 却报错**
这是它的设计：错误码放在响应体的 `base_resp` 里。命令行工具已经把它翻译成了统一错误码，
按上表处理即可，`provider_code` 字段保留了厂商原始码方便查证。

**参数似乎没生效**
先看 `warnings`——多半是被裁剪或丢弃了。要看最终发出去的请求长什么样，加 `--dry-run`：
它会打印完整的 HTTP 请求（method、URL、请求头、请求体），且 Authorization 已打码，
可以安全展示给用户。加了 `--dry-run` 不会真的发请求，也不消耗额度。

**配置似乎没读到**
`allen-tts config show` 显示合并后的有效配置（密钥已打码），并在 stderr 打印配置文件路径。
参数优先级是：命令行参数 > 环境变量 > 配置文件 > 厂商默认值。
