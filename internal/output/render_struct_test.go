package output

import (
	"bytes"
	"strings"
	"testing"
)

// 命令层可以把自定义结构体交给渲染器（如 channel +health 的报告）。
// 渲染器只认 JSON 原生类型，因此必须先归一化 —— 否则整个结构体会被当成
// 一个标量打成一行 JSON，表格与 pretty 就退化成不可读的单行。
type healthLike struct {
	Total    int          `json:"total"`
	Healthy  int          `json:"healthy"`
	Disabled []channelRow `json:"disabled"`
}

type channelRow struct {
	ID     int    `json:"id"`
	Name   string `json:"name"`
	Reason string `json:"reason,omitempty"`
}

func sampleReport() *healthLike {
	return &healthLike{
		Total:   3,
		Healthy: 1,
		Disabled: []channelRow{
			{ID: 2, Name: "azure-backup", Reason: "自动禁用"},
		},
	}
}

func TestRenderPrettyExpandsStructFields(t *testing.T) {
	var buf bytes.Buffer
	if err := RenderPretty(&buf, sampleReport(), false); err != nil {
		t.Fatalf("RenderPretty: %v", err)
	}
	got := buf.String()

	// 结构体应被展开成多行键值，而不是压成一行 JSON。
	if strings.Count(strings.TrimSpace(got), "\n") == 0 {
		t.Fatalf("结构体未展开，仍是单行:\n%s", got)
	}
	if strings.Contains(got, `{"total"`) {
		t.Errorf("不该输出原始 JSON 串:\n%s", got)
	}
	// json tag 必须生效（total 而非 Total）。
	for _, want := range []string{"total", "healthy", "disabled", "azure-backup"} {
		if !strings.Contains(got, want) {
			t.Errorf("输出缺少 %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "Total") {
		t.Errorf("应使用 json tag 的小写字段名:\n%s", got)
	}
}

func TestRenderTableHandlesStructPayload(t *testing.T) {
	var buf bytes.Buffer
	if err := RenderTable(&buf, sampleReport(), nil, false); err != nil {
		t.Fatalf("RenderTable: %v", err)
	}
	got := buf.String()
	// 单个对象渲染成 字段/值 两列表格。
	if !strings.Contains(got, "total") || !strings.Contains(got, "healthy") {
		t.Errorf("表格应含字段名:\n%s", got)
	}
	if strings.Count(strings.TrimSpace(got), "\n") == 0 {
		t.Errorf("结构体未展开成表格:\n%s", got)
	}
}

// 结构体切片应渲染成正常的多列表格。
func TestRenderTableHandlesStructSlice(t *testing.T) {
	rows := []channelRow{
		{ID: 1, Name: "openai-main"},
		{ID: 2, Name: "azure-backup", Reason: "自动禁用"},
	}
	var buf bytes.Buffer
	if err := RenderTable(&buf, rows, []string{"id", "name"}, false); err != nil {
		t.Fatalf("RenderTable: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) != 3 {
		t.Fatalf("期望表头 + 2 行，得到:\n%s", buf.String())
	}
	if !strings.Contains(lines[1], "openai-main") || !strings.Contains(lines[2], "azure-backup") {
		t.Errorf("数据行不符:\n%s", buf.String())
	}
}

func TestRenderCSVHandlesStructSlice(t *testing.T) {
	rows := []channelRow{{ID: 1, Name: "openai-main"}}
	var buf bytes.Buffer
	if err := RenderCSV(&buf, rows, []string{"id", "name"}); err != nil {
		t.Fatalf("RenderCSV: %v", err)
	}
	got := buf.String()
	if !strings.Contains(got, "id,name") {
		t.Errorf("缺少表头:\n%s", got)
	}
	if !strings.Contains(got, "1,openai-main") {
		t.Errorf("缺少数据行:\n%s", got)
	}
}

// 归一化不应破坏已是原生形态的数据。
func TestNormalizePreservesNativeTypes(t *testing.T) {
	native := map[string]any{"a": float64(1), "b": "x"}
	if got := normalize(native); got == nil {
		t.Fatal("map 不应被丢弃")
	}
	list := []any{float64(1), "x"}
	if got, ok := normalize(list).([]any); !ok || len(got) != 2 {
		t.Errorf("[]any 应原样保留, 得到 %#v", normalize(list))
	}
	if got := normalize(nil); got != nil {
		t.Errorf("nil 应保持 nil, 得到 %#v", got)
	}
	if got := normalize("s"); got != "s" {
		t.Errorf("字符串应原样保留, 得到 %#v", got)
	}
}
