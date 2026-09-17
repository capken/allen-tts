package config

import (
	"os"
	"path/filepath"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/capken/allen-tts/internal/core"
)

// ProviderConfig 是 config.yaml 中 providers.<name> 一节。
type ProviderConfig struct {
	APIKey  string `yaml:"api_key"`
	Model   string `yaml:"model"`
	Format  string `yaml:"format"`
	BaseURL string `yaml:"base_url"`
}

// Defaults 是全局默认，可被 providers.<x> 覆盖。
type Defaults struct {
	Format string  `yaml:"format"`
	Speed  float64 `yaml:"speed"`
	Volume float64 `yaml:"volume"`
}

// Config 是 ~/.allen-tts/config.yaml 的解析结果（设计文档 8.2）。
type Config struct {
	DefaultProvider string                    `yaml:"default_provider"`
	OnUnsupported   string                    `yaml:"on_unsupported"`
	Timeout         string                    `yaml:"timeout"`
	Providers       map[string]ProviderConfig `yaml:"providers"`
	Defaults        Defaults                  `yaml:"defaults"`

	Path string `yaml:"-"` // 实际加载的文件路径
}

// Dir 返回配置目录（voices.yaml 与 config.yaml 同目录）。
func Dir() string {
	if p := os.Getenv("ALLEN_TTS_CONFIG"); p != "" {
		return filepath.Dir(p)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "."
	}
	return filepath.Join(home, ".allen-tts")
}

// DefaultPath 返回配置文件路径（ALLEN_TTS_CONFIG 可覆盖）。
func DefaultPath() string {
	if p := os.Getenv("ALLEN_TTS_CONFIG"); p != "" {
		return p
	}
	return filepath.Join(Dir(), "config.yaml")
}

// VoicesPath 返回音色别名文件路径。
func VoicesPath() string {
	return filepath.Join(Dir(), "voices.yaml")
}

// Load 读取并解析配置；文件不存在返回零值配置而非错误。
// 文件内容先做 ${ENV} 展开再解析（设计文档 8.2）。
func Load() (*Config, error) {
	path := DefaultPath()
	cfg := &Config{Path: path, Providers: map[string]ProviderConfig{}}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return nil, core.NewError(core.ErrInvalidArgument, "cannot read config %s: %v", path, err)
	}
	expanded := os.ExpandEnv(string(data))
	if err := yaml.Unmarshal([]byte(expanded), cfg); err != nil {
		return nil, core.NewError(core.ErrInvalidArgument, "invalid config %s: %v", path, err)
	}
	cfg.Path = path
	if cfg.Providers == nil {
		cfg.Providers = map[string]ProviderConfig{}
	}
	return cfg, nil
}

var keyEnv = map[string]string{
	"fish":     "FISH_API_KEY",
	"cartesia": "CARTESIA_API_KEY",
	"minimax":  "MINIMAX_API_KEY",
}

// APIKey 按优先级 环境变量 > 配置文件 返回密钥。
func (c *Config) APIKey(provider string) string {
	if env := keyEnv[provider]; env != "" {
		if v := os.Getenv(env); v != "" {
			return v
		}
	}
	return c.Providers[provider].APIKey
}

// BaseURL 返回厂商 API 基址覆盖（仅 MiniMax 有环境变量口）。
func (c *Config) BaseURL(provider string) string {
	if provider == "minimax" {
		if v := os.Getenv("MINIMAX_BASE_URL"); v != "" {
			return v
		}
	}
	return c.Providers[provider].BaseURL
}

// EffectiveProvider 按优先级 CLI > 环境变量 > 配置 返回厂商名。
func (c *Config) EffectiveProvider(flag string) string {
	if flag != "" {
		return flag
	}
	if v := os.Getenv("ALLEN_TTS_PROVIDER"); v != "" {
		return v
	}
	return c.DefaultProvider
}

// EffectiveTimeout 解析配置里的 timeout，非法或缺省时返回 fallback。
func (c *Config) EffectiveTimeout(fallback time.Duration) time.Duration {
	if c.Timeout == "" {
		return fallback
	}
	d, err := time.ParseDuration(c.Timeout)
	if err != nil || d <= 0 {
		return fallback
	}
	return d
}

// Template 是 `config init` 生成的模板。
const Template = `# allen-tts 配置文件。字符串值支持 ${ENV} 引用。
default_provider: minimax
on_unsupported: warn        # warn | error | drop
timeout: 60s

providers:
  fish:
    api_key: ${FISH_API_KEY}
    model: s2.1-pro
  cartesia:
    api_key: ${CARTESIA_API_KEY}
    model: sonic-3.6
  minimax:
    api_key: ${MINIMAX_API_KEY}
    model: speech-2.8-hd
    base_url: https://api.minimax.cn

defaults:
  format: mp3
  speed: 1.0
  volume: 1.0
`
