package cli

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/capken/allen-tts/internal/config"
	"github.com/capken/allen-tts/internal/provider/registry"
)

func newCapabilitiesCmd() *cobra.Command {
	var flagProvider string
	var flagJSON bool
	cmd := &cobra.Command{
		Use:   "capabilities",
		Short: "输出厂商能力声明（agent 应先查再调）",
		RunE: func(cmd *cobra.Command, args []string) error {
			jsonMode = flagJSON
			cfg, err := config.Load()
			if err != nil {
				return err
			}
			name := cfg.EffectiveProvider(flagProvider)
			p, err := registry.New(name, cfg, 30*time.Second, false)
			if err != nil {
				return err
			}
			// 能力结构本身就是机器可读契约，非 --json 时同样输出缩进 JSON。
			out, err := json.MarshalIndent(map[string]any{"provider": name, "capabilities": p.Capabilities()}, "", "  ")
			if err != nil {
				return err
			}
			fmt.Println(string(out))
			return nil
		},
	}
	cmd.Flags().StringVarP(&flagProvider, "provider", "p", "", "fish | cartesia | minimax")
	cmd.Flags().BoolVar(&flagJSON, "json", false, "输出 JSON（默认已是 JSON）")
	return cmd
}
