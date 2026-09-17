package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/spf13/cobra"

	"github.com/capken/allen-tts/internal/config"
	"github.com/capken/allen-tts/internal/core"
	"github.com/capken/allen-tts/internal/provider"
	"github.com/capken/allen-tts/internal/provider/registry"
)

func newVoicesCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "voices", Short: "音色相关命令"}
	cmd.AddCommand(newVoicesListCmd())
	return cmd
}

func newVoicesListCmd() *cobra.Command {
	var flagProvider string
	var flagJSON bool
	cmd := &cobra.Command{
		Use:   "list",
		Short: "列出厂商音色与本地别名",
		RunE: func(cmd *cobra.Command, args []string) error {
			jsonMode = flagJSON
			cfg, err := config.Load()
			if err != nil {
				return err
			}
			name := cfg.EffectiveProvider(flagProvider)
			p, err := registry.New(name, cfg, 30*time.Second, true)
			if err != nil {
				return err
			}

			voices, err := p.ListVoices(cmd.Context())
			notImplemented := errors.Is(err, provider.ErrNotImplemented)
			if err != nil && !notImplemented {
				return err
			}

			aliases, err := core.LoadAliases(config.VoicesPath())
			if err != nil {
				return err
			}
			type aliasOut struct {
				Alias       string `json:"alias"`
				ID          string `json:"id"`
				Description string `json:"description,omitempty"`
				Language    string `json:"language,omitempty"`
			}
			var aliasList []aliasOut
			for aname, e := range aliases {
				if id, ok := e.IDs[name]; ok {
					aliasList = append(aliasList, aliasOut{Alias: "@" + aname, ID: id, Description: e.Description, Language: e.Language})
				}
			}
			sort.Slice(aliasList, func(i, j int) bool { return aliasList[i].Alias < aliasList[j].Alias })

			if flagJSON {
				out, _ := json.MarshalIndent(map[string]any{
					"provider":        name,
					"voices":          voices,
					"aliases":         aliasList,
					"not_implemented": notImplemented,
				}, "", "  ")
				fmt.Println(string(out))
				return nil
			}
			if notImplemented {
				fmt.Printf("voices list is not implemented for %s in v1\n", name)
			}
			for _, v := range voices {
				line := v.ID
				if v.Name != "" {
					line += "\t" + v.Name
				}
				if v.Language != "" {
					line += "\t[" + v.Language + "]"
				}
				if v.Description != "" {
					line += "\t" + v.Description
				}
				fmt.Println(line)
			}
			if len(aliasList) > 0 {
				fmt.Println("\n# local aliases (voices.yaml)")
				for _, a := range aliasList {
					line := fmt.Sprintf("%s\t-> %s", a.Alias, a.ID)
					if a.Description != "" {
						line += "\t" + a.Description
					}
					fmt.Println(line)
				}
			}
			return nil
		},
	}
	cmd.Flags().StringVarP(&flagProvider, "provider", "p", "", "fish | cartesia | minimax")
	cmd.Flags().BoolVar(&flagJSON, "json", false, "输出 JSON")
	return cmd
}

// newVoiceCmd 预留 voice create（设计文档 5.6）。
func newVoiceCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "voice", Short: "音色管理（预留）"}
	cmd.AddCommand(&cobra.Command{
		Use:   "create",
		Short: "创建克隆音色（v1 未实现）",
		RunE: func(cmd *cobra.Command, args []string) error {
			return core.NewError(core.ErrInvalidArgument,
				"voice create is not implemented in v1 (Fish cloning requires msgpack inline references; planned for a later version)")
		},
	})
	return cmd
}
