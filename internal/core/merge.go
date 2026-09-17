package core

// DeepMerge 把 src 深合并进 dst：对象递归合并，标量与数组覆盖（设计文档第 7 节）。
// 返回 dst 本身。src 中的字段不做任何校验。
func DeepMerge(dst, src map[string]any) map[string]any {
	if dst == nil {
		dst = map[string]any{}
	}
	for k, sv := range src {
		if sm, ok := sv.(map[string]any); ok {
			if dm, ok := dst[k].(map[string]any); ok {
				dst[k] = DeepMerge(dm, sm)
				continue
			}
		}
		dst[k] = sv
	}
	return dst
}
