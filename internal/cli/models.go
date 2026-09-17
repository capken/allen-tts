package cli

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/capken/allen-tts/internal/config"
	"github.com/capken/allen-tts/internal/provider/registry"
)

func newModelsCmd() *cobra.Command {
	var flagProvider string
	var flagJSON bool
	cmd := &cobra.Command{
		Use:   "models",
		Short: "列出厂商模型（静态表）",
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
			models := p.ListModels()
			if flagJSON {
				out, _ := json.MarshalIndent(map[string]any{"provider": name, "models": models}, "", "  ")
				fmt.Println(string(out))
				return nil
			}
			for _, m := range models {
				line := m.ID
				if m.Default {
					line += "\t(default)"
				}
				if m.Notes != "" {
					line += "\t" + m.Notes
				}
				fmt.Println(line)
			}
			return nil
		},
	}
	cmd.Flags().StringVarP(&flagProvider, "provider", "p", "", "fish | cartesia | minimax")
	cmd.Flags().BoolVar(&flagJSON, "json", false, "输出 JSON")
	return cmd
}
