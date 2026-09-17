package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/capken/allen-tts/internal/config"
	"github.com/capken/allen-tts/internal/core"
	"github.com/capken/allen-tts/internal/provider"
	"github.com/capken/allen-tts/internal/provider/httpx"
	"github.com/capken/allen-tts/internal/provider/registry"
)

// speakMeta 是 --json 成功输出（设计文档 6.1）。
type speakMeta struct {
	OK         bool           `json:"ok"`
	Provider   string         `json:"provider"`
	Model      string         `json:"model,omitempty"`
	Voice      string         `json:"voice,omitempty"`
	VoiceAlias string         `json:"voice_alias,omitempty"`
	Output     string         `json:"output"`
	Format     string         `json:"format"`
	SampleRate int            `json:"sample_rate,omitempty"`
	Bitrate    int            `json:"bitrate,omitempty"`
	Channels   int            `json:"channels,omitempty"`
	Bytes      int64          `json:"bytes"`
	DurationMS int64          `json:"duration_ms,omitempty"`
	Usage      provider.Usage `json:"usage,omitempty"`
	RequestID  string         `json:"request_id,omitempty"`
	Warnings   []string       `json:"warnings,omitempty"`
	Extras     map[string]any `json:"extras,omitempty"`
}

func newSpeakCmd() *cobra.Command {
	var (
		flagProvider, flagModel, flagVoice, flagLang, flagEmotion string
		flagFormat, flagLatency, flagOutput, flagExtra            string
		flagTextFile, flagOnUnsupported                           string
		flagSpeed, flagVolume                                     float64
		flagPitch, flagSampleRate, flagBitrate, flagChannels      int
		flagStream, flagJSON, flagDryRun                          bool
		flagTimeout                                               time.Duration
	)

	cmd := &cobra.Command{
		Use:   "speak [flags] [TEXT]",
		Short: "合成语音",
		Long:  "合成语音。文本来源优先级：位置参数 TEXT > --text-file PATH > stdin。",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			jsonMode = flagJSON

			if flagJSON && flagOutput == "-" {
				return core.NewError(core.ErrInvalidArgument, "--json conflicts with -o -: stdout can only carry one of audio or metadata")
			}

			cfg, err := config.Load()
			if err != nil {
				return err
			}
			providerName := cfg.EffectiveProvider(flagProvider)

			text, err := resolveText(args, flagTextFile)
			if err != nil {
				return err
			}

			req := core.SpeakRequest{
				Provider: providerName,
				Model:    flagModel,
				Text:     text,
				Voice:    flagVoice,
				Language: flagLang,
				Speed:    flagSpeed,
				Volume:   flagVolume,
				Pitch:    flagPitch,
				Emotion:  flagEmotion,
				Format:   flagFormat,
				Channels: flagChannels,
				Latency:  flagLatency,
				Stream:   flagStream,

				SampleRate:    flagSampleRate,
				Bitrate:       flagBitrate,
				OnUnsupported: flagOnUnsupported,
			}

			// 配置层默认值：仅在 flag 未显式给出时生效（CLI > env > config > adapter 默认）。
			applyConfigDefaults(cmd, cfg, providerName, &req)

			if flagExtra != "" {
				var extra map[string]any
				if err := json.Unmarshal([]byte(flagExtra), &extra); err != nil {
					return core.NewError(core.ErrInvalidArgument, "--extra is not valid JSON: %v", err)
				}
				req.Extra = extra
			}

			// 音色别名解析（设计文档 8.3）
			if strings.HasPrefix(req.Voice, "@") {
				id, alias, aliasLang, err := core.ResolveAlias(req.Voice, providerName, config.VoicesPath())
				if err != nil {
					return err
				}
				req.Voice, req.VoiceAlias = id, alias
				if req.Language == "" && aliasLang != "" {
					req.Language = aliasLang
				}
			}

			if err := req.Validate(); err != nil {
				return err
			}

			timeout := flagTimeout
			if !cmd.Flags().Changed("timeout") {
				timeout = cfg.EffectiveTimeout(timeout)
			}

			p, err := registry.New(providerName, cfg, timeout, !flagDryRun)
			if err != nil {
				return err
			}

			if flagDryRun {
				return printDryRun(p, req)
			}

			ctx := cmd.Context()
			res, err := p.Speak(ctx, req)
			if err != nil {
				return err
			}
			defer res.Audio.Close()

			outPath, err := core.OutputPath(flagOutput, req.Format)
			if err != nil {
				return err
			}
			nbytes, err := core.WriteAudio(outPath, res.Audio)
			if err != nil {
				return err
			}
			if res.Wait != nil {
				res.Wait() // 流式：等尾事件解析完，元数据才完整
			}

			meta := speakMeta{
				OK:         true,
				Provider:   providerName,
				Model:      res.Model,
				Voice:      req.Voice,
				VoiceAlias: req.VoiceAlias,
				Output:     outPath,
				Format:     res.Format,
				SampleRate: firstNonZero(res.SampleRate, req.SampleRate),
				Bitrate:    firstNonZero(res.Bitrate, req.Bitrate),
				Channels:   res.Channels,
				Bytes:      nbytes,
				DurationMS: res.DurationMS,
				Usage:      res.Usage,
				RequestID:  res.RequestID,
				Warnings:   res.Warnings,
				Extras:     res.Extras,
			}

			if flagJSON {
				out, err := json.MarshalIndent(meta, "", "  ")
				if err != nil {
					return err
				}
				fmt.Println(string(out))
				return nil
			}
			for _, w := range res.Warnings {
				fmt.Fprintln(os.Stderr, "warning:", w)
			}
			if outPath != "-" {
				summary := fmt.Sprintf("wrote %s (%s", outPath, humanBytes(nbytes))
				if res.DurationMS > 0 {
					summary += fmt.Sprintf(", %.1fs", float64(res.DurationMS)/1000)
				}
				fmt.Fprintln(os.Stderr, summary+")")
			}
			return nil
		},
	}

	fl := cmd.Flags()
	fl.StringVarP(&flagProvider, "provider", "p", "", "fish | cartesia | minimax")
	fl.StringVarP(&flagModel, "model", "m", "", "厂商模型 ID（缺省用 adapter 默认）")
	fl.StringVarP(&flagVoice, "voice", "v", "", "音色 ID 或 @alias")
	fl.StringVar(&flagLang, "lang", "", "BCP-47 语言码，如 zh / en / yue")
	fl.Float64Var(&flagSpeed, "speed", 1.0, "语速倍数 [0.5, 2.0]")
	fl.Float64Var(&flagVolume, "volume", 1.0, "音量倍数 [0.5, 2.0]，1.0 = 不变")
	fl.IntVar(&flagPitch, "pitch", 0, "音调半音 [-12, 12]")
	fl.StringVar(&flagEmotion, "emotion", "", "情绪：neutral|happy|sad|angry|fearful|surprised|calm|excited 或厂商特有值")
	fl.StringVarP(&flagFormat, "format", "f", "", "mp3 | wav | pcm | opus | flac（默认 mp3）")
	fl.IntVar(&flagSampleRate, "sample-rate", 0, "采样率 Hz（0 = 厂商默认）")
	fl.IntVar(&flagBitrate, "bitrate", 0, "码率 kbps（0 = 厂商默认）")
	fl.IntVar(&flagChannels, "channels", 1, "声道数 1 | 2")
	fl.StringVar(&flagLatency, "latency", "", "quality | balanced | low（默认 quality）")
	fl.BoolVar(&flagStream, "stream", false, "流式接收，边收边写")
	fl.StringVarP(&flagOutput, "output", "o", "", `输出文件；"-" 表示 stdout；缺省自动命名 ./tts-<ts>.<ext>`)
	fl.BoolVar(&flagJSON, "json", false, "stdout 输出 JSON 元数据（音频必须写文件）")
	fl.StringVar(&flagExtra, "extra", "", "JSON，深合并进厂商 payload")
	fl.StringVar(&flagOnUnsupported, "on-unsupported", "", "warn | error | drop（默认 warn）")
	fl.StringVar(&flagTextFile, "text-file", "", "从文件读取文本")
	fl.BoolVar(&flagDryRun, "dry-run", false, "不发请求，打印最终厂商 payload（含 header）")
	fl.DurationVar(&flagTimeout, "timeout", 60*time.Second, "请求超时")
	return cmd
}

// resolveText 按 位置参数 > --text-file > stdin 取文本。
func resolveText(args []string, textFile string) (string, error) {
	if len(args) > 0 && args[0] != "" {
		return args[0], nil
	}
	if textFile != "" {
		data, err := os.ReadFile(textFile)
		if err != nil {
			return "", core.NewError(core.ErrInvalidArgument, "cannot read --text-file: %v", err)
		}
		return strings.TrimRight(string(data), "\n"), nil
	}
	stat, _ := os.Stdin.Stat()
	if (stat.Mode() & os.ModeCharDevice) != 0 {
		return "", core.NewError(core.ErrInvalidArgument, "no text: pass TEXT argument, --text-file, or pipe via stdin")
	}
	data, err := io.ReadAll(os.Stdin)
	if err != nil {
		return "", core.NewError(core.ErrInvalidArgument, "cannot read stdin: %v", err)
	}
	return strings.TrimRight(string(data), "\n"), nil
}

// applyConfigDefaults 把配置文件的 defaults 与 providers.<x> 覆盖进未显式给出的 flag。
func applyConfigDefaults(cmd *cobra.Command, cfg *config.Config, providerName string, req *core.SpeakRequest) {
	pc := cfg.Providers[providerName]
	if req.Format == "" {
		if pc.Format != "" {
			req.Format = pc.Format
		} else if cfg.Defaults.Format != "" {
			req.Format = cfg.Defaults.Format
		}
	}
	if !cmd.Flags().Changed("speed") && cfg.Defaults.Speed != 0 {
		req.Speed = cfg.Defaults.Speed
	}
	if !cmd.Flags().Changed("volume") && cfg.Defaults.Volume != 0 {
		req.Volume = cfg.Defaults.Volume
	}
	if req.OnUnsupported == "" {
		req.OnUnsupported = cfg.OnUnsupported
	}
}

// printDryRun 输出脱敏后的完整 HTTP 请求（设计文档 5.1）。
func printDryRun(p provider.Provider, req core.SpeakRequest) error {
	spec, warnings, err := p.Build(req)
	if err != nil {
		return err
	}
	out, err := json.MarshalIndent(map[string]any{
		"method":   spec.Method,
		"url":      spec.URL,
		"headers":  httpx.RedactHeaders(spec.Headers),
		"body":     spec.Body,
		"warnings": warnings,
	}, "", "  ")
	if err != nil {
		return err
	}
	fmt.Println(string(out))
	return nil
}

func firstNonZero(vals ...int) int {
	for _, v := range vals {
		if v != 0 {
			return v
		}
	}
	return 0
}

func humanBytes(n int64) string {
	switch {
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.0f KB", float64(n)/(1<<10))
	default:
		return fmt.Sprintf("%d B", n)
	}
}
