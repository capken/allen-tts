package core

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// VoiceAliasEntry 是 voices.yaml 中的一个别名：一名多厂商映射（设计文档 8.3）。
type VoiceAliasEntry struct {
	Description string
	Language    string            // 别名携带的默认语言，仅在用户未传 --lang 时生效
	IDs         map[string]string // provider -> 真实音色 ID
}

// LoadAliases 解析 voices.yaml。文件不存在返回空表，不算错误。
func LoadAliases(path string) (map[string]VoiceAliasEntry, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]VoiceAliasEntry{}, nil
		}
		return nil, NewError(ErrInvalidArgument, "cannot read %s: %v", path, err)
	}
	var raw map[string]map[string]string
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return nil, NewError(ErrInvalidArgument, "invalid voices.yaml %s: %v", path, err)
	}
	out := make(map[string]VoiceAliasEntry, len(raw))
	for name, m := range raw {
		e := VoiceAliasEntry{IDs: map[string]string{}}
		for k, v := range m {
			switch k {
			case "description":
				e.Description = v
			case "language":
				e.Language = v
			default:
				e.IDs[k] = v
			}
		}
		out[name] = e
	}
	return out, nil
}

// ResolveAlias 把 @alias 解析为当前 provider 的音色 ID。
// 非 @ 前缀的 voice 原样返回。别名在该 provider 无映射时列出可用厂商（设计文档 8.3）。
func ResolveAlias(voice, provider, aliasPath string) (id, alias, language string, err error) {
	if !strings.HasPrefix(voice, "@") {
		return voice, "", "", nil
	}
	name := strings.TrimPrefix(voice, "@")
	aliases, err := LoadAliases(aliasPath)
	if err != nil {
		return "", "", "", err
	}
	entry, ok := aliases[name]
	if !ok {
		return "", "", "", NewError(ErrInvalidArgument, "voice alias %q not found in %s", voice, aliasPath)
	}
	realID, ok := entry.IDs[provider]
	if !ok {
		avail := make([]string, 0, len(entry.IDs))
		for p := range entry.IDs {
			avail = append(avail, p)
		}
		sort.Strings(avail)
		return "", "", "", NewError(ErrInvalidArgument,
			"voice alias %q has no mapping for provider %q (available: %s)",
			voice, provider, fmt.Sprint(strings.Join(avail, ", ")))
	}
	return realID, voice, entry.Language, nil
}
