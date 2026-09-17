package version

import "runtime/debug"

// Version 由发布构建通过 -ldflags 注入（见 .github/workflows/release.yml 与 Makefile）。
// 未注入时（如 go install）回退到 module 版本，不再显示无意义的占位值。
var Version = "dev"

func init() {
	if Version != "dev" {
		return
	}
	bi, ok := debug.ReadBuildInfo()
	if !ok {
		return
	}
	// 源码树里直接 go run/go build 时为 "(devel)"，此时保留 "dev"。
	if v := bi.Main.Version; v != "" && v != "(devel)" {
		Version = v
	}
}
