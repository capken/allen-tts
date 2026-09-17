package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	"github.com/capken/allen-tts/internal/config"
	"github.com/capken/allen-tts/internal/core"
	"github.com/capken/allen-tts/internal/provider"
	"github.com/capken/allen-tts/internal/provider/registry"
)

func newConfigCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "config", Short: "配置管理"}
	cmd.AddCommand(newConfigInitCmd(), newConfigShowCmd(), newConfigCheckCmd())
	return cmd
}

func newConfigInitCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "init",
		Short: "生成配置模板 ~/.allen-tts/config.yaml",
		RunE: func(cmd *cobra.Command, args []string) error {
			path := config.DefaultPath()
			if _, err := os.Stat(path); err == nil {
				return core.NewError(core.ErrInvalidArgument, "config already exists at %s (remove it first to regenerate)", path)
			}
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				return core.NewError(core.ErrInvalidArgument, "cannot create config dir: %v", err)
			}
			if err := os.WriteFile(path, []byte(config.Template), 0o600); err != nil {
				return core.NewError(core.ErrInvalidArgument, "cannot write config: %v", err)
			}
			fmt.Fprintln(os.Stderr, "wrote", path)
			return nil
		},
	}
}

// maskKey 保留密钥前 6 位，其余打码。
func maskKey(k string) string {
	if k == "" {
		return ""
	}
	if len(k) > 6 {
		return k[:6] + "****"
	}
	return "****"
}

func newConfigShowCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "show",
		Short: "显示合并后的有效配置（密钥打码）",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load()
			if err != nil {
				return err
			}
			// 展示合并后的有效值（环境变量优先），密钥打码。
			masked := *cfg
			masked.Providers = map[string]config.ProviderConfig{}
			for _, name := range registry.Names() {
				pc := cfg.Providers[name]
				pc.APIKey = maskKey(cfg.APIKey(name))
				pc.BaseURL = cfg.BaseURL(name)
				masked.Providers[name] = pc
			}
			masked.DefaultProvider = cfg.EffectiveProvider("")
			out, err := yaml.Marshal(&masked)
			if err != nil {
				return err
			}
			fmt.Fprintln(os.Stderr, "# config file:", cfg.Path)
			fmt.Print(string(out))
			return nil
		},
	}
}

func newConfigCheckCmd() *cobra.Command {
	var flagProvider string
	cmd := &cobra.Command{
		Use:   "check",
		Short: "校验各厂商密钥可用（发最小请求）",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load()
			if err != nil {
				return err
			}
			names := registry.Names()
			if flagProvider != "" {
				names = []string{flagProvider}
			}
			failed := false
			for _, name := range names {
				if flagProvider == "" && cfg.APIKey(name) == "" {
					fmt.Printf("%-10s skipped (no API key configured)\n", name)
					continue
				}
				p, err := registry.New(name, cfg, 30*time.Second, true)
				if err != nil {
					fmt.Printf("%-10s FAIL: %v\n", name, err)
					failed = true
					continue
				}
				checker, ok := p.(provider.Checker)
				if !ok {
					fmt.Printf("%-10s skipped (no check available)\n", name)
					continue
				}
				if err := checker.Check(cmd.Context()); err != nil {
					fmt.Printf("%-10s FAIL: %v\n", name, err)
					failed = true
					continue
				}
				fmt.Printf("%-10s OK\n", name)
			}
			if failed {
				return core.NewError(core.ErrAuth, "one or more providers failed the check")
			}
			return nil
		},
	}
	cmd.Flags().StringVarP(&flagProvider, "provider", "p", "", "只检查指定厂商")
	return cmd
}
