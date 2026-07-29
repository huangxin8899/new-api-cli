package cmdutil

import (
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api-cli/errs"
)

// ParseEnum 校验一个取值受限的参数，返回归一化后的小写值。
func ParseEnum(raw, name string, allowed []string) (string, error) {
	v := strings.ToLower(strings.TrimSpace(raw))
	for _, a := range allowed {
		if v == a {
			return v, nil
		}
	}
	return "", errs.NewValidationError(errs.SubtypeInvalidArgument,
		"%s 不支持 %q", name, raw).
		WithHint("可选值：%s", strings.Join(allowed, " | ")).
		WithParams(name)
}

// ParseEnumInt 校验一个字面量取值受限、但线上传输为整数的参数。
//
// New API 有不少 int 编码的枚举（日志类型、渠道状态等）。命令行暴露可读的
// 名字，这里负责翻译成协议要求的数字，顺带在报错里列出全部合法名字。
func ParseEnumInt(raw, name string, allowed map[string]int) (int, error) {
	v := strings.ToLower(strings.TrimSpace(raw))
	if code, ok := allowed[v]; ok {
		return code, nil
	}
	names := make([]string, 0, len(allowed))
	for k := range allowed {
		names = append(names, k)
	}
	sort.Slice(names, func(i, j int) bool { return allowed[names[i]] < allowed[names[j]] })
	return 0, errs.NewValidationError(errs.SubtypeInvalidArgument,
		"%s 不支持 %q", name, raw).
		WithHint("可选值：%s", strings.Join(names, " | ")).
		WithParams(name)
}

// ParseIDs 解析一串资源 ID 参数，并去重保序。
func ParseIDs(raw []string, name string) ([]int, error) {
	seen := make(map[int]bool, len(raw))
	ids := make([]int, 0, len(raw))
	for _, item := range raw {
		// 同时接受 "1,2,3" 与多个位置参数两种写法。
		for _, part := range strings.Split(item, ",") {
			part = strings.TrimSpace(part)
			if part == "" {
				continue
			}
			id, err := ParseID(part, name)
			if err != nil {
				return nil, err
			}
			if !seen[id] {
				seen[id] = true
				ids = append(ids, id)
			}
		}
	}
	if len(ids) == 0 {
		return nil, errs.NewValidationError(errs.SubtypeMissingArgument,
			"%s 至少需要一个 ID", name).WithParams(name)
	}
	return ids, nil
}

// 支持的时间字面量格式，按尝试顺序排列。
var timeLayouts = []string{
	time.RFC3339,
	"2006-01-02T15:04:05",
	"2006-01-02 15:04:05",
	"2006-01-02 15:04",
	"2006-01-02",
}

// ParseTimestamp 把时间参数解析成 Unix 秒。
//
// 接受三种写法：Unix 秒（10 位数字）、RFC3339，以及 2006-01-02 15:04:05
// 这类本地时间字面量。不带时区的字面量按本机时区解释。
func ParseTimestamp(raw, name string) (int64, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return 0, errs.NewValidationError(errs.SubtypeMissingArgument,
			"%s 不能为空", name).WithParams(name)
	}
	if n, err := strconv.ParseInt(s, 10, 64); err == nil {
		return n, nil
	}
	for _, layout := range timeLayouts {
		if t, err := time.ParseInLocation(layout, s, time.Local); err == nil {
			return t.Unix(), nil
		}
	}
	return 0, errs.NewValidationError(errs.SubtypeInvalidArgument,
		"%s 无法解析为时间：%q", name, raw).
		WithHint("支持 Unix 秒、2026-01-31、2026-01-31 10:00:00 或 2026-01-31T10:00:00Z").
		WithParams(name)
}

// ParseRelativeSince 把 "7d" / "24h" / "30m" 这类相对时长换算成起始 Unix 秒。
func ParseRelativeSince(raw, name string, now time.Time) (int64, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return 0, nil
	}
	if len(s) > 1 && strings.HasSuffix(s, "d") {
		days, err := strconv.Atoi(strings.TrimSuffix(s, "d"))
		if err == nil && days >= 0 {
			return now.AddDate(0, 0, -days).Unix(), nil
		}
	}
	if d, err := time.ParseDuration(s); err == nil && d >= 0 {
		return now.Add(-d).Unix(), nil
	}
	return 0, errs.NewValidationError(errs.SubtypeInvalidArgument,
		"%s 无法解析为时长：%q", name, raw).
		WithHint("支持 7d、24h、90m 这类写法").
		WithParams(name)
}
