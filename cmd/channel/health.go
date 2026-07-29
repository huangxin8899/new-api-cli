package channel

import (
	"fmt"
	"sort"
	"strings"
)

// healthReport 是 +health 的本地聚合结果。字段带 JSON tag，因为它会直接
// 作为 data 输出，Agent 依赖这些键名。
type healthReport struct {
	Total    int             `json:"total"`
	Healthy  int             `json:"healthy"`
	Disabled []healthChannel `json:"disabled"`
	Slow     []healthChannel `json:"slow"`
	LowFunds []healthChannel `json:"low_balance"`
}

// healthChannel 是报告里的单条渠道摘要，只保留判断问题所需的字段。
type healthChannel struct {
	ID           int     `json:"id"`
	Name         string  `json:"name"`
	Status       int     `json:"status"`
	StatusText   string  `json:"status_text"`
	Group        string  `json:"group,omitempty"`
	Tag          string  `json:"tag,omitempty"`
	ResponseTime int     `json:"response_time_ms,omitempty"`
	Balance      float64 `json:"balance,omitempty"`
	Reason       string  `json:"reason,omitempty"`
}

// 渠道状态：0 未知，1 启用，2 手动禁用，3 自动禁用。
const (
	chStatusUnknown = 0
	chStatusEnabled = 1
	chStatusManual  = 2
	chStatusAuto    = 3
)

func statusText(status int) string {
	switch status {
	case chStatusEnabled:
		return "启用"
	case chStatusManual:
		return "手动禁用"
	case chStatusAuto:
		return "自动禁用"
	case chStatusUnknown:
		return "未知"
	default:
		return fmt.Sprintf("status=%d", status)
	}
}

// buildHealthReport 在本地把渠道列表分类。items 是 /api/channel/ 返回的原始
// JSON 对象，字段缺失时按零值处理 —— 不同 New API 版本的字段集并不完全一致。
func buildHealthReport(items []any, slowMS int, minBalance float64) *healthReport {
	report := &healthReport{
		Total:    len(items),
		Disabled: []healthChannel{},
		Slow:     []healthChannel{},
		LowFunds: []healthChannel{},
	}
	for _, item := range items {
		row, ok := item.(map[string]any)
		if !ok {
			continue
		}
		ch := healthChannel{
			ID:           jsonInt(row["id"]),
			Name:         jsonString(row["name"]),
			Status:       jsonInt(row["status"]),
			Group:        jsonString(row["group"]),
			Tag:          jsonString(row["tag"]),
			ResponseTime: jsonInt(row["response_time"]),
			Balance:      jsonFloat(row["balance"]),
		}
		ch.StatusText = statusText(ch.Status)

		problem := false
		if ch.Status != chStatusEnabled {
			d := ch
			d.Reason = ch.StatusText
			report.Disabled = append(report.Disabled, d)
			problem = true
		}
		// 响应时间为 0 表示从未测试过，不算慢。
		if ch.Status == chStatusEnabled && ch.ResponseTime > slowMS {
			s := ch
			s.Reason = fmt.Sprintf("响应 %dms 超过阈值 %dms", ch.ResponseTime, slowMS)
			report.Slow = append(report.Slow, s)
			problem = true
		}
		// 余额为 0 的渠道通常是不支持余额查询的类型，不作为低余额告警。
		if ch.Balance > 0 && ch.Balance < minBalance {
			l := ch
			l.Reason = fmt.Sprintf("余额 %.2f 低于阈值 %.2f", ch.Balance, minBalance)
			report.LowFunds = append(report.LowFunds, l)
			problem = true
		}
		if !problem {
			report.Healthy++
		}
	}

	for _, list := range [][]healthChannel{report.Disabled, report.Slow, report.LowFunds} {
		sort.Slice(list, func(i, j int) bool { return list[i].ID < list[j].ID })
	}
	return report
}

// summary 生成一行人类可读的结论，写到 stderr 不污染 stdout。
func (r *healthReport) summary() string {
	if len(r.Disabled) == 0 && len(r.Slow) == 0 && len(r.LowFunds) == 0 {
		return fmt.Sprintf("%d 个渠道全部健康", r.Total)
	}
	parts := make([]string, 0, 3)
	if n := len(r.Disabled); n > 0 {
		parts = append(parts, fmt.Sprintf("%d 个未启用", n))
	}
	if n := len(r.Slow); n > 0 {
		parts = append(parts, fmt.Sprintf("%d 个响应慢", n))
	}
	if n := len(r.LowFunds); n > 0 {
		parts = append(parts, fmt.Sprintf("%d 个余额不足", n))
	}
	return fmt.Sprintf("%d 个渠道中 %d 个健康；%s", r.Total, r.Healthy, strings.Join(parts, "，"))
}

// JSON 数字统一解码成 float64，这里按需窄化。

func jsonInt(v any) int {
	switch n := v.(type) {
	case float64:
		return int(n)
	case int:
		return n
	default:
		return 0
	}
}

func jsonFloat(v any) float64 {
	if n, ok := v.(float64); ok {
		return n
	}
	return 0
}

func jsonString(v any) string {
	switch s := v.(type) {
	case string:
		return s
	case nil:
		return ""
	default:
		return fmt.Sprintf("%v", s)
	}
}
