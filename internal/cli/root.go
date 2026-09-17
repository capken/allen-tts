// Package cli 只做参数解析与输出，不含业务逻辑（设计文档第 9 节）。
package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/capken/allen-tts/internal/core"
	"github.com/capken/allen-tts/internal/version"
)

// jsonMode 由 speak 在运行时设置，Execute 据此决定错误输出形态。
var jsonMode bool

func newRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:           "allen-tts",
		Short:         "面向 Agent 的统一多厂商 TTS 命令行工具",
		Long:          "allen-tts 封装 Fish Audio / Cartesia / MiniMax 三家 TTS API，\n对外暴露一套统一参数。音频落文件或 stdout，元数据以 JSON 输出，\n日志走 stderr，退出码有语义。",
		Version:       version.Version,
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.AddCommand(
		newSpeakCmd(),
		newVoicesCmd(),
		newVoiceCmd(),
		newModelsCmd(),
		newCapabilitiesCmd(),
		newConfigCmd(),
	)
	return root
}

// Execute 运行 CLI 并返回进程退出码（设计文档 6.4）。
func Execute() int {
	err := newRootCmd().Execute()
	if err == nil {
		return 0
	}
	var ce *core.Error
	if !errors.As(err, &ce) {
		// cobra 用法错误等本地问题按 invalid_argument 处理
		ce = core.NewError(core.ErrInvalidArgument, "%s", err.Error())
	}
	if jsonMode {
		out, _ := json.MarshalIndent(struct {
			OK    bool        `json:"ok"`
			Error *core.Error `json:"error"`
		}{false, ce}, "", "  ")
		fmt.Println(string(out))
	} else {
		fmt.Fprintln(os.Stderr, "error:", ce.Error())
	}
	return ce.ExitCode()
}
