// Package output 负责 new-api-cli 的全部终端输出。
//
// 输出契约（Agent 依赖它做解析，改动即为破坏性变更）：
//
//	成功 → stdout，退出码 0：{"ok":true,"data":...,"meta":{...}}
//	失败 → stderr，退出码非 0：{"ok":false,"error":{"type":...,"message":...}}
//
// stdout 只承载结果，进度、警告一律走 stderr，便于 `cmd | jq` 直接消费。
package output

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/QuantumNous/new-api-cli/errs"
)

// Format 是输出格式枚举。
type Format string

const (
	// FormatJSON 完整 JSON 信封（默认，Agent 友好）。
	FormatJSON Format = "json"
	// FormatNDJSON 每行一条记录，适合管道流式处理。
	FormatNDJSON Format = "ndjson"
	// FormatTable 对齐表格，适合人类阅读。
	FormatTable Format = "table"
	// FormatCSV 逗号分隔值，适合导入表格软件。
	FormatCSV Format = "csv"
	// FormatPretty 键值缩进渲染，适合看单条记录。
	FormatPretty Format = "pretty"
)

// AllFormats 列出全部合法格式，用于校验与 shell 补全。
var AllFormats = []Format{FormatJSON, FormatNDJSON, FormatTable, FormatCSV, FormatPretty}

// ParseFormat 校验并归一化格式名。
func ParseFormat(raw string) (Format, error) {
	f := Format(strings.ToLower(strings.TrimSpace(raw)))
	for _, known := range AllFormats {
		if f == known {
			return f, nil
		}
	}
	names := make([]string, 0, len(AllFormats))
	for _, known := range AllFormats {
		names = append(names, string(known))
	}
	return "", errs.NewValidationError(errs.SubtypeInvalidArgument,
		"未知的输出格式 %q", raw).
		WithHint("可选值：%s", strings.Join(names, "|")).
		WithParams("--format")
}

// Envelope 是成功响应的信封。
type Envelope struct {
	OK     bool  `json:"ok"`
	DryRun bool  `json:"dry_run,omitempty"`
	Data   any   `json:"data"`
	Meta   *Meta `json:"meta,omitempty"`
}

// Meta 承载分页与批量操作的附加信息。
type Meta struct {
	Count    int    `json:"count,omitempty"`
	Total    int    `json:"total,omitempty"`
	Page     int    `json:"page,omitempty"`
	PageSize int    `json:"page_size,omitempty"`
	Profile  string `json:"profile,omitempty"`
	BaseURL  string `json:"base_url,omitempty"`
	Message  string `json:"message,omitempty"`
}

type errorEnvelope struct {
	OK    bool         `json:"ok"`
	Error *errs.Problem `json:"error"`
}

// Result 是命令交给 Emitter 的全部内容。
type Result struct {
	// Data 是要输出的负载。
	Data any
	// Meta 可选，写入信封的 meta 字段。
	Meta *Meta
	// Columns 指定 table/csv 的列顺序；为空时按数据首条记录的键推断。
	Columns []string
	// DryRun 标记这是一次预演，不曾真正调用接口。
	DryRun bool
	// Message 是人类可读的一句话总结，仅在 table/pretty 下打印。
	Message string
}

// Emitter 按配置把 Result 渲染到流上。
type Emitter struct {
	Out    io.Writer
	Err    io.Writer
	Format Format
	// JQ 为非空时，先用该表达式过滤 data 再渲染。
	JQ string
	// Color 控制是否输出 ANSI 颜色。
	Color bool
}

// NewEmitter 构造一个默认写往 stdout/stderr 的 Emitter。
func NewEmitter() *Emitter {
	return &Emitter{
		Out:    os.Stdout,
		Err:    os.Stderr,
		Format: FormatJSON,
		Color:  SupportsColor(os.Stdout),
	}
}

// Emit 渲染一次成功结果。
func (e *Emitter) Emit(r Result) error {
	data := r.Data
	if e.JQ != "" {
		filtered, err := ApplyJQ(e.JQ, data)
		if err != nil {
			return err
		}
		data = filtered
	}

	switch e.Format {
	case FormatJSON:
		return e.emitJSON(r, data)
	case FormatNDJSON:
		return e.emitNDJSON(data)
	case FormatTable:
		return e.emitTable(r, data)
	case FormatCSV:
		return e.emitCSV(r, data)
	case FormatPretty:
		return e.emitPretty(r, data)
	default:
		return e.emitJSON(r, data)
	}
}

func (e *Emitter) emitJSON(r Result, data any) error {
	env := Envelope{OK: true, DryRun: r.DryRun, Data: data, Meta: r.Meta}
	if env.Meta != nil && env.Meta.Message == "" && r.Message != "" {
		env.Meta.Message = r.Message
	}
	return writeJSON(e.Out, env)
}

func (e *Emitter) emitNDJSON(data any) error {
	rows, ok := asRows(data)
	if !ok {
		return writeJSONLine(e.Out, data)
	}
	for _, row := range rows {
		if err := writeJSONLine(e.Out, row); err != nil {
			return err
		}
	}
	return nil
}

func (e *Emitter) emitTable(r Result, data any) error {
	if r.DryRun {
		fmt.Fprintln(e.Err, e.dim("[dry-run] 未真正发起请求"))
	}
	if err := RenderTable(e.Out, data, r.Columns, e.Color); err != nil {
		return err
	}
	e.printFooter(r)
	return nil
}

func (e *Emitter) emitCSV(r Result, data any) error {
	return RenderCSV(e.Out, data, r.Columns)
}

func (e *Emitter) emitPretty(r Result, data any) error {
	if r.DryRun {
		fmt.Fprintln(e.Err, e.dim("[dry-run] 未真正发起请求"))
	}
	if err := RenderPretty(e.Out, data, e.Color); err != nil {
		return err
	}
	e.printFooter(r)
	return nil
}

func (e *Emitter) printFooter(r Result) {
	if r.Message != "" {
		fmt.Fprintln(e.Err, e.dim(r.Message))
	}
	if r.Meta == nil {
		return
	}
	parts := make([]string, 0, 3)
	if r.Meta.Count > 0 {
		parts = append(parts, fmt.Sprintf("本页 %d 条", r.Meta.Count))
	}
	if r.Meta.Total > 0 {
		parts = append(parts, fmt.Sprintf("共 %d 条", r.Meta.Total))
	}
	if r.Meta.Page > 0 {
		parts = append(parts, fmt.Sprintf("第 %d 页", r.Meta.Page))
	}
	if len(parts) > 0 {
		fmt.Fprintln(e.Err, e.dim(strings.Join(parts, " · ")))
	}
}

// EmitError 把错误渲染成错误信封写入 stderr，并返回退出码。
// JSON/NDJSON/CSV 下写结构化信封；table/pretty 下写人类可读文本。
func (e *Emitter) EmitError(err error) int {
	if err == nil {
		return errs.ExitOK
	}
	code := errs.ExitCodeOf(err)
	typed, ok := errs.Unwrap(err)
	if !ok {
		typed = errs.NewInternalError("%s", err.Error())
	}

	switch e.Format {
	case FormatTable, FormatPretty:
		fmt.Fprintln(e.Err, e.red("错误: ")+typed.Message)
		if typed.Hint != "" {
			fmt.Fprintln(e.Err, e.dim("提示: "+typed.Hint))
		}
		if typed.RequestID != "" {
			fmt.Fprintln(e.Err, e.dim("request_id: "+typed.RequestID))
		}
	default:
		_ = writeJSON(e.Err, errorEnvelope{OK: false, Error: &typed.Problem})
	}
	return code
}

// Warn 输出一条警告到 stderr，永远不污染 stdout。
func (e *Emitter) Warn(format string, a ...any) {
	fmt.Fprintln(e.Err, e.yellow("警告: ")+fmt.Sprintf(format, a...))
}

// Info 输出一条提示到 stderr。
func (e *Emitter) Info(format string, a ...any) {
	fmt.Fprintln(e.Err, e.dim(fmt.Sprintf(format, a...)))
}

func writeJSON(w io.Writer, v any) error {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		return errs.NewInternalError("序列化输出失败: %v", err)
	}
	_, err := w.Write(buf.Bytes())
	return err
}

func writeJSONLine(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	return enc.Encode(v)
}
