// Package registry 按名字构造 provider adapter，注入配置层的密钥与默认值。
package registry

import (
	"sort"
	"time"

	"github.com/capken/allen-tts/internal/config"
	"github.com/capken/allen-tts/internal/core"
	"github.com/capken/allen-tts/internal/provider"
	"github.com/capken/allen-tts/internal/provider/cartesia"
	"github.com/capken/allen-tts/internal/provider/fish"
	"github.com/capken/allen-tts/internal/provider/minimax"
)

// Names 返回全部厂商名（升序）。
func Names() []string {
	names := []string{"fish", "cartesia", "minimax"}
	sort.Strings(names)
	return names
}

// New 构造指定厂商的 adapter。requireKey 为 true 时缺密钥直接报错
// （--dry-run 等本地操作可传 false）。
func New(name string, cfg *config.Config, timeout time.Duration, requireKey bool) (provider.Provider, error) {
	if name == "" {
		return nil, core.NewError(core.ErrInvalidArgument,
			"no provider specified: use -p fish|cartesia|minimax, ALLEN_TTS_PROVIDER, or default_provider in config")
	}
	key := cfg.APIKey(name)
	pc := cfg.Providers[name]
	switch name {
	case "fish":
		if requireKey && key == "" {
			return nil, missingKey(name, "FISH_API_KEY")
		}
		return fish.New(fish.Options{APIKey: key, BaseURL: cfg.BaseURL(name), Model: pc.Model, Timeout: timeout}), nil
	case "cartesia":
		if requireKey && key == "" {
			return nil, missingKey(name, "CARTESIA_API_KEY")
		}
		return cartesia.New(cartesia.Options{APIKey: key, BaseURL: cfg.BaseURL(name), Model: pc.Model, Timeout: timeout}), nil
	case "minimax":
		if requireKey && key == "" {
			return nil, missingKey(name, "MINIMAX_API_KEY")
		}
		return minimax.New(minimax.Options{APIKey: key, BaseURL: cfg.BaseURL(name), Model: pc.Model, Timeout: timeout}), nil
	}
	return nil, core.NewError(core.ErrInvalidArgument, "unknown provider %q (want fish|cartesia|minimax)", name)
}

func missingKey(name, env string) error {
	return core.NewError(core.ErrAuth, "no API key for %s: set %s or providers.%s.api_key in config", name, env, name)
}
