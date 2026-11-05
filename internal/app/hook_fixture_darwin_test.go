package app

import (
	"encoding/json"
	"io"

	"easycode/internal/domain"
)

func serveHookToolSample(w io.Writer, family domain.ProviderFamily, capability domain.HookCapability) {
	switch capability {
	case domain.HookPatch:
		if family == domain.ProviderOpenAI {
			writeOpenAIPatchSample(w, 1)
		} else {
			writeAnthropicPatchSample(w, 1)
		}
	case domain.HookWrite:
		serveWriteScenarioSample(w, family, 1, []writeScenarioCall{{"Write", `{"file_path":"written","content":"new"}`}})
	case domain.HookExec:
		writeExecCommandSample(w, family, "printf executed > exec-marker", 1000)
	}
}

// containsHookPreview 同时解码 Responses 函数输出中的内层 JSON，避免只匹配安全 code。
func containsHookPreview(data []byte, want string) bool {
	var value any
	if json.Unmarshal(data, &value) != nil {
		return false
	}
	var visit func(any) bool
	visit = func(v any) bool {
		switch x := v.(type) {
		case string:
			if x == want {
				return true
			}
			if json.Valid([]byte(x)) {
				return containsHookPreview([]byte(x), want)
			}
		case []any:
			for _, item := range x {
				if visit(item) {
					return true
				}
			}
		case map[string]any:
			for _, item := range x {
				if visit(item) {
					return true
				}
			}
		}
		return false
	}
	return visit(value)
}
