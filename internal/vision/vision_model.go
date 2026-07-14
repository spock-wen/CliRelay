package vision

import "strings"

// SupportsVisionByModelName 用名字启发式判断模型是否支持原生视觉。
// 规则：模型名（已转小写）包含 vision/multimodal/omni，或按分隔符切出的
// token 之一为 "vl"，视为视觉模型。
func SupportsVisionByModelName(model string) bool {
	model = strings.ToLower(strings.TrimSpace(model))
	if model == "" {
		return false
	}
	if strings.Contains(model, "vision") ||
		strings.Contains(model, "multimodal") ||
		strings.Contains(model, "omni") {
		return true
	}
	for _, token := range strings.FieldsFunc(model, func(r rune) bool {
		return r == '-' || r == '_' || r == '.' || r == '/' || r == ':'
	}) {
		if token == "vl" {
			return true
		}
	}
	return false
}
