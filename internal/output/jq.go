package output

import (
	"encoding/json"

	"github.com/huangxin8899/new-api-cli/errs"
	"github.com/itchyny/gojq"
)

// ApplyJQ 用 jq 表达式过滤负载。
//
// 单条结果直接返回该值；多条结果返回数组；无结果返回 nil —— 这样
// `--jq '.items[].name'` 会得到一个字符串数组，符合直觉。
func ApplyJQ(expr string, data any) (any, error) {
	query, err := gojq.Parse(expr)
	if err != nil {
		return nil, errs.NewValidationError(errs.SubtypeInvalidArgument,
			"jq 表达式解析失败: %v", err).
			WithHint(`表达式语法见 https://jqlang.github.io/jq/manual/，例如 --jq '.items[] | {id, name}'`).
			WithParams("--jq")
	}
	// gojq 只接受由 encoding/json 反序列化出的原生类型，先做一次归一化。
	normalized, err := normalizeForJQ(data)
	if err != nil {
		return nil, err
	}

	iter := query.Run(normalized)
	var results []any
	for {
		v, ok := iter.Next()
		if !ok {
			break
		}
		if jqErr, isErr := v.(error); isErr {
			var halt *gojq.HaltError
			if ok := asHaltError(jqErr, &halt); ok && halt.Value() == nil {
				break
			}
			return nil, errs.NewValidationError(errs.SubtypeInvalidArgument,
				"jq 求值失败: %v", jqErr).WithParams("--jq")
		}
		results = append(results, v)
	}

	switch len(results) {
	case 0:
		return nil, nil
	case 1:
		return results[0], nil
	default:
		return results, nil
	}
}

func asHaltError(err error, target **gojq.HaltError) bool {
	h, ok := err.(*gojq.HaltError)
	if ok {
		*target = h
	}
	return ok
}

// normalizeForJQ 把任意 Go 值转成 map/[]any/基础类型。
func normalizeForJQ(data any) (any, error) {
	switch data.(type) {
	case nil, bool, string, float64, int, []any, map[string]any:
		// 已是原生形态；int 由 gojq 内部处理。
		return data, nil
	}
	b, err := json.Marshal(data)
	if err != nil {
		return nil, errs.NewInternalError("无法为 jq 序列化数据: %v", err)
	}
	var out any
	if err := json.Unmarshal(b, &out); err != nil {
		return nil, errs.NewInternalError("无法为 jq 反序列化数据: %v", err)
	}
	return out, nil
}
