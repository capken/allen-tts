package minimax

import (
	"bufio"
	"bytes"
	"io"
)

// parseSSE 用标准库解析 text/event-stream：按行累积 data: 字段，
// 空行分发一个事件。行可跨底层 chunk 边界，由 bufio 处理。
// onEvent 返回非 nil 错误时中止解析并透传该错误。
func parseSSE(r io.Reader, onEvent func(data []byte) error) error {
	scanner := bufio.NewScanner(r)
	// 音频 hex 分片可能很大，放宽单行上限到 16MB。
	scanner.Buffer(make([]byte, 64*1024), 16*1024*1024)

	var data []byte
	flush := func() error {
		if len(data) == 0 {
			return nil
		}
		ev := data
		data = nil
		return onEvent(ev)
	}
	for scanner.Scan() {
		line := scanner.Bytes()
		line = bytes.TrimSuffix(line, []byte("\r"))
		if len(line) == 0 { // 事件边界
			if err := flush(); err != nil {
				return err
			}
			continue
		}
		if bytes.HasPrefix(line, []byte(":")) { // 注释行
			continue
		}
		if v, ok := bytes.CutPrefix(line, []byte("data:")); ok {
			v = bytes.TrimPrefix(v, []byte(" "))
			if len(data) > 0 {
				data = append(data, '\n')
			}
			data = append(data, v...)
		}
		// 其余字段（event:/id:/retry:）v1 用不到，忽略。
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	return flush() // 流结束时可能有未跟空行的尾事件
}
