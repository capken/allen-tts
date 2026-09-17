package core

// SpeakRequest 是统一合成请求（设计文档第 4 节）。
// 所有字段量纲统一：Speed/Volume 为倍数，Pitch 为半音，Bitrate 为 kbps。
type SpeakRequest struct {
	Provider string
	Model    string // 空 = 用 adapter 默认
	Text     string

	Voice      string // 音色 ID（别名已在 CLI 层解析）
	VoiceAlias string // 原始 @别名（仅用于输出元数据）
	Language   string // BCP-47

	Speed   float64 // 默认 1.0
	Volume  float64 // 倍数，1.0 = 不变
	Pitch   int     // 半音 [-12, 12]
	Emotion string

	Format     string // mp3 | wav | pcm | opus | flac
	SampleRate int    // Hz；0 = 厂商默认
	Bitrate    int    // kbps；0 = 厂商默认
	Channels   int    // 1 | 2

	Latency string // quality | balanced | low
	Stream  bool

	Extra map[string]any // 深合并进厂商原始 payload，不校验

	// OnUnsupported 是降级策略：warn | error | drop（设计文档 4.3）。
	OnUnsupported string
}

// ValidFormats 是统一格式全集；具体厂商支持子集由 Capabilities 声明。
var ValidFormats = []string{"mp3", "wav", "pcm", "opus", "flac"}

// Validate 做本地参数校验与默认值填充，错误统一为 invalid_argument。
func (r *SpeakRequest) Validate() error {
	if r.Text == "" {
		return NewError(ErrInvalidArgument, "text is required (positional arg, --text-file, or stdin)")
	}
	if r.Speed == 0 {
		r.Speed = 1.0
	}
	if r.Volume == 0 {
		r.Volume = 1.0
	}
	if r.Format == "" {
		r.Format = "mp3"
	}
	if r.Channels == 0 {
		r.Channels = 1
	}
	if r.Latency == "" {
		r.Latency = "quality"
	}
	if r.OnUnsupported == "" {
		r.OnUnsupported = "warn"
	}
	switch r.OnUnsupported {
	case "warn", "error", "drop":
	default:
		return NewError(ErrInvalidArgument, "invalid --on-unsupported %q (want warn|error|drop)", r.OnUnsupported)
	}
	valid := false
	for _, f := range ValidFormats {
		if r.Format == f {
			valid = true
			break
		}
	}
	if !valid {
		return NewError(ErrInvalidArgument, "invalid format %q (want mp3|wav|pcm|opus|flac)", r.Format)
	}
	switch r.Latency {
	case "quality", "balanced", "low":
	default:
		return NewError(ErrInvalidArgument, "invalid --latency %q (want quality|balanced|low)", r.Latency)
	}
	if r.Channels != 1 && r.Channels != 2 {
		return NewError(ErrInvalidArgument, "invalid --channels %d (want 1 or 2)", r.Channels)
	}
	return nil
}
