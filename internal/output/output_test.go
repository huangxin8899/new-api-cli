package output

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api-cli/errs"
)

// newTestEmitter 构造一个写入内存缓冲、关闭颜色的 Emitter。
func newTestEmitter(format Format) (*Emitter, *bytes.Buffer, *bytes.Buffer) {
	var out, errBuf bytes.Buffer
	return &Emitter{Out: &out, Err: &errBuf, Format: format, Color: false}, &out, &errBuf
}

func TestEmitJSONEnvelopeContract(t *testing.T) {
	e, out, errBuf := newTestEmitter(FormatJSON)
	err := e.Emit(Result{
		Data: map[string]any{"id": 7, "name": "openai-main"},
		Meta: &Meta{Count: 1, Total: 42, Page: 2},
	})
	if err != nil {
		t.Fatalf("Emit: %v", err)
	}
	if errBuf.Len() != 0 {
		t.Errorf("成功结果不该写 stderr，实际写了 %q", errBuf.String())
	}

	var env struct {
		OK   bool `json:"ok"`
		Data struct {
			ID   int    `json:"id"`
			Name string `json:"name"`
		} `json:"data"`
		Meta struct {
			Count int `json:"count"`
			Total int `json:"total"`
			Page  int `json:"page"`
		} `json:"meta"`
	}
	if err := json.Unmarshal(out.Bytes(), &env); err != nil {
		t.Fatalf("stdout 不是合法 JSON: %v\n%s", err, out.String())
	}
	if !env.OK {
		t.Error("ok 应为 true")
	}
	if env.Data.ID != 7 || env.Data.Name != "openai-main" {
		t.Errorf("data 不符: %+v", env.Data)
	}
	if env.Meta.Count != 1 || env.Meta.Total != 42 || env.Meta.Page != 2 {
		t.Errorf("meta 不符: %+v", env.Meta)
	}
}

func TestEmitErrorEnvelopeGoesToStderr(t *testing.T) {
	e, out, errBuf := newTestEmitter(FormatJSON)
	code := e.EmitError(errs.NewAuthError(errs.SubtypeForbidden, "权限不足").
		WithHint("需要管理员"))

	if code != errs.ExitForbidden {
		t.Errorf("退出码 = %d, 期望 %d", code, errs.ExitForbidden)
	}
	if out.Len() != 0 {
		t.Errorf("错误不该污染 stdout，实际写了 %q", out.String())
	}

	var env struct {
		OK    bool `json:"ok"`
		Error struct {
			Type    string `json:"type"`
			Subtype string `json:"subtype"`
			Message string `json:"message"`
			Hint    string `json:"hint"`
		} `json:"error"`
	}
	if err := json.Unmarshal(errBuf.Bytes(), &env); err != nil {
		t.Fatalf("stderr 不是合法 JSON: %v\n%s", err, errBuf.String())
	}
	if env.OK {
		t.Error("ok 应为 false")
	}
	if env.Error.Type != "auth" || env.Error.Subtype != "forbidden" {
		t.Errorf("error 分类不符: %+v", env.Error)
	}
	if env.Error.Message != "权限不足" || env.Error.Hint != "需要管理员" {
		t.Errorf("error 内容不符: %+v", env.Error)
	}
}

// 非类型化错误也必须产出合法信封，否则 Agent 会在意外错误上解析失败。
func TestEmitErrorWrapsUntypedError(t *testing.T) {
	e, _, errBuf := newTestEmitter(FormatJSON)
	code := e.EmitError(errors.New("某个未预期的失败"))
	if code != errs.ExitError {
		t.Errorf("退出码 = %d, 期望 %d", code, errs.ExitError)
	}
	var env struct {
		OK    bool `json:"ok"`
		Error struct {
			Type    string `json:"type"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(errBuf.Bytes(), &env); err != nil {
		t.Fatalf("stderr 不是合法 JSON: %v", err)
	}
	if env.Error.Type != "internal" {
		t.Errorf("type = %q, 期望 internal", env.Error.Type)
	}
	if env.Error.Message != "某个未预期的失败" {
		t.Errorf("message = %q", env.Error.Message)
	}
}

func TestEmitNDJSONOneRecordPerLine(t *testing.T) {
	e, out, _ := newTestEmitter(FormatNDJSON)
	err := e.Emit(Result{Data: []any{
		map[string]any{"id": 1},
		map[string]any{"id": 2},
		map[string]any{"id": 3},
	}})
	if err != nil {
		t.Fatalf("Emit: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 3 {
		t.Fatalf("期望 3 行，得到 %d 行:\n%s", len(lines), out.String())
	}
	for i, line := range lines {
		var m map[string]any
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			t.Errorf("第 %d 行不是合法 JSON: %v", i+1, err)
		}
	}
}

func TestEmitTableProjectsColumnsInOrder(t *testing.T) {
	e, out, _ := newTestEmitter(FormatTable)
	err := e.Emit(Result{
		Data: []any{
			map[string]any{"id": float64(1), "name": "a", "status": float64(1), "extra": "hidden"},
			map[string]any{"id": float64(2), "name": "b", "status": float64(2), "extra": "hidden"},
		},
		Columns: []string{"id", "name", "status"},
	})
	if err != nil {
		t.Fatalf("Emit: %v", err)
	}
	text := out.String()
	lines := strings.Split(strings.TrimSpace(text), "\n")
	if len(lines) != 3 {
		t.Fatalf("期望表头 + 2 行，得到:\n%s", text)
	}
	// 表头有意大写（render.go），比较时忽略大小写。
	header := strings.ToLower(lines[0])
	if i, j, k := strings.Index(header, "id"), strings.Index(header, "name"), strings.Index(header, "status"); !(i < j && j < k) {
		t.Errorf("列顺序不符: %q", lines[0])
	}
	if strings.Contains(text, "hidden") {
		t.Errorf("--columns 之外的字段不该出现:\n%s", text)
	}
}

func TestEmitCSVQuotesEmbeddedCommas(t *testing.T) {
	e, out, _ := newTestEmitter(FormatCSV)
	err := e.Emit(Result{
		Data:    []any{map[string]any{"name": "a,b", "note": `say "hi"`}},
		Columns: []string{"name", "note"},
	})
	if err != nil {
		t.Fatalf("Emit: %v", err)
	}
	text := out.String()
	if !strings.Contains(text, `"a,b"`) {
		t.Errorf("含逗号的值应被引号包裹:\n%s", text)
	}
	if !strings.Contains(text, `"say ""hi"""`) {
		t.Errorf("双引号应被转义:\n%s", text)
	}
}

func TestApplyJQ(t *testing.T) {
	tests := []struct {
		name string
		expr string
		data any
		want string
	}{
		{
			name: "提取字段",
			expr: ".name",
			data: map[string]any{"name": "openai", "id": float64(1)},
			want: `"openai"`,
		},
		{
			name: "数组映射",
			expr: "[.[].id]",
			data: []any{map[string]any{"id": float64(1)}, map[string]any{"id": float64(2)}},
			want: `[1,2]`,
		},
		{
			name: "条件过滤",
			expr: "[.[] | select(.status == 1) | .name]",
			data: []any{
				map[string]any{"name": "up", "status": float64(1)},
				map[string]any{"name": "down", "status": float64(2)},
			},
			want: `["up"]`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ApplyJQ(tt.expr, tt.data)
			if err != nil {
				t.Fatalf("ApplyJQ: %v", err)
			}
			b, err := json.Marshal(got)
			if err != nil {
				t.Fatalf("Marshal: %v", err)
			}
			if string(b) != tt.want {
				t.Errorf("= %s, 期望 %s", b, tt.want)
			}
		})
	}
}

func TestApplyJQInvalidExpressionIsValidationError(t *testing.T) {
	_, err := ApplyJQ(".[", map[string]any{})
	if err == nil {
		t.Fatal("非法 jq 表达式应报错")
	}
	typed, ok := errs.Unwrap(err)
	if !ok {
		t.Fatalf("应是类型化错误，得到 %T", err)
	}
	if typed.Type != errs.TypeValidation {
		t.Errorf("type = %q, 期望 validation", typed.Type)
	}
}

// Sanitize 是终端注入的防线：服务端返回的字段会被直接打印，
// 其中的 ANSI 转义必须失效。
func TestSanitizeStripsTerminalEscapes(t *testing.T) {
	tests := []struct {
		name  string
		in    string
		notIn string
	}{
		{"ANSI 颜色", "\x1b[31mred\x1b[0m", "\x1b["},
		{"回车覆盖", "real\rfake", "\r"},
		{"退格", "abc\b\b\bxyz", "\b"},
		{"响铃", "ding\a", "\a"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Sanitize(tt.in)
			if strings.Contains(got, tt.notIn) {
				t.Errorf("Sanitize(%q) = %q，仍含 %q", tt.in, got, tt.notIn)
			}
		})
	}
}

func TestParseFormat(t *testing.T) {
	for _, name := range []string{"json", "JSON", " table ", "csv", "ndjson", "pretty"} {
		if _, err := ParseFormat(name); err != nil {
			t.Errorf("ParseFormat(%q) 应成功: %v", name, err)
		}
	}
	_, err := ParseFormat("yaml")
	if err == nil {
		t.Fatal("未知格式应报错")
	}
	typed, ok := errs.Unwrap(err)
	if !ok || typed.Type != errs.TypeValidation {
		t.Errorf("应是 validation 错误，得到 %v", err)
	}
	if !strings.Contains(typed.Hint, "json") {
		t.Errorf("提示应列出可选值，实际 %q", typed.Hint)
	}
}

// 表格里的 CJK 字符按两格宽计算，否则列会错位。
func TestDisplayWidthCountsWideRunes(t *testing.T) {
	if got := displayWidth("abc"); got != 3 {
		t.Errorf("displayWidth(\"abc\") = %d, 期望 3", got)
	}
	if got := displayWidth("渠道"); got != 4 {
		t.Errorf("displayWidth(\"渠道\") = %d, 期望 4", got)
	}
	if got := displayWidth("a渠b"); got != 4 {
		t.Errorf("displayWidth(\"a渠b\") = %d, 期望 4", got)
	}
}

func TestScalarString(t *testing.T) {
	tests := []struct {
		in   any
		want string
	}{
		{nil, ""},
		{"text", "text"},
		{true, "true"},
		{float64(10), "10"},       // JSON 整数不该显示成 10.0
		{float64(1.5), "1.5"},
		{float64(1700000000), "1700000000"}, // 时间戳不该走科学计数法
	}
	for _, tt := range tests {
		if got := ScalarString(tt.in); got != tt.want {
			t.Errorf("ScalarString(%v) = %q, 期望 %q", tt.in, got, tt.want)
		}
	}
}
