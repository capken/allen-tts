package core

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"
)

// OutputPath 决定音频落点："-" 表 stdout；空则自动命名 ./tts-<ts>.<ext>。
// 返回绝对路径（stdout 时返回 "-"）。
func OutputPath(flag, format string) (string, error) {
	if flag == "-" {
		return "-", nil
	}
	p := flag
	if p == "" {
		p = fmt.Sprintf("tts-%s.%s", time.Now().Format("20060102-150405"), format)
	}
	abs, err := filepath.Abs(p)
	if err != nil {
		return "", NewError(ErrInvalidArgument, "cannot resolve output path %q: %v", p, err)
	}
	return abs, nil
}

// WriteAudio 把音频流写到目标（文件或 stdout），逐块拷贝以兼容流式，返回字节数。
func WriteAudio(dst string, audio io.Reader) (int64, error) {
	var w io.Writer
	if dst == "-" {
		w = os.Stdout
	} else {
		f, err := os.Create(dst)
		if err != nil {
			return 0, NewError(ErrInvalidArgument, "cannot create %s: %v", dst, err)
		}
		defer f.Close()
		w = f
	}
	n, err := io.Copy(w, audio)
	if err != nil {
		return n, NewError(ErrNetwork, "error while receiving audio: %v", err)
	}
	return n, nil
}
