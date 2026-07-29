package output

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
	"unicode"

	"github.com/QuantumNous/new-api-cli/errs"
)

// normalize 把任意 Go 值转成 JSON 原生形态（map / []any / 标量）。
//
// 渲染器只认 JSON 原生类型，但命令层交上来的 Data 可能是自定义结构体
// （例如 channel +health 的报告）。不归一化的话，结构体会掉进 formatCell
// 的兜底分支被整条打成一行 JSON，表格与 pretty 就退化成不可读的单行。
// 走一次 Marshal/Unmarshal 让 json tag 生效，字段名与 JSON 输出保持一致。
func normalize(data any) any {
	switch data.(type) {
	case nil, bool, string, float64, int, int64, json.Number, []any, map[string]any:
		return data
	}
	b, err := json.Marshal(data)
	if err != nil {
		return data
	}
	var out any
	if err := json.Unmarshal(b, &out); err != nil {
		return data
	}
	return out
}

// asRows 把负载归一化成「记录列表」。
// 识别三种形态：数组、New API 的分页对象（{items:[...]}）、单个对象。
func asRows(data any) ([]any, bool) {
	switch v := data.(type) {
	case []any:
		return v, true
	case map[string]any:
		if items, ok := v["items"]; ok {
			if rows, ok := items.([]any); ok {
				return rows, true
			}
			if items == nil {
				return []any{}, true
			}
		}
	case nil:
		return []any{}, true
	}
	return nil, false
}

// inferColumns 推断表格列：以首行键序为准，其余行的新键按字典序追加。
func inferColumns(rows []any) []string {
	seen := map[string]bool{}
	var cols []string
	var extra []string
	for i, row := range rows {
		m, ok := row.(map[string]any)
		if !ok {
			continue
		}
		keys := make([]string, 0, len(m))
		for k := range m {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			if seen[k] {
				continue
			}
			seen[k] = true
			if i == 0 {
				cols = append(cols, k)
			} else {
				extra = append(extra, k)
			}
		}
	}
	return append(cols, extra...)
}

// cellValue 取字段值，支持 "a.b" 点号路径。
func cellValue(row any, col string) any {
	cur := row
	for _, seg := range strings.Split(col, ".") {
		m, ok := cur.(map[string]any)
		if !ok {
			return nil
		}
		cur, ok = m[seg]
		if !ok {
			return nil
		}
	}
	return cur
}

// formatCell 把任意值渲染成单元格文本。
func formatCell(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return Sanitize(t)
	case bool:
		return strconv.FormatBool(t)
	case float64:
		if t == float64(int64(t)) {
			return strconv.FormatInt(int64(t), 10)
		}
		return strconv.FormatFloat(t, 'f', -1, 64)
	case int:
		return strconv.Itoa(t)
	case int64:
		return strconv.FormatInt(t, 10)
	case json.Number:
		return t.String()
	default:
		b, err := json.Marshal(t)
		if err != nil {
			return fmt.Sprintf("%v", t)
		}
		return Sanitize(string(b))
	}
}

// ScalarString 把一个 JSON 标量渲染成字符串字面量，供查询参数拼接等场景复用。
// 整数形态的 float64 不带小数点，布尔与数字按字面量输出，复合值退化为紧凑 JSON。
func ScalarString(v any) string { return formatCell(v) }

// RenderTable 渲染对齐表格。列表渲染成多列表格，单对象渲染成 字段/值 两列。
func RenderTable(w io.Writer, data any, columns []string, color bool) error {
	data = normalize(data)
	rows, ok := asRows(data)
	if !ok {
		if m, isMap := data.(map[string]any); isMap {
			return renderKV(w, m, columns, color)
		}
		_, err := fmt.Fprintln(w, formatCell(data))
		return err
	}
	if len(rows) == 0 {
		_, err := fmt.Fprintln(w, "(空)")
		return err
	}
	cols := columns
	if len(cols) == 0 {
		cols = inferColumns(rows)
	}
	if len(cols) == 0 {
		// 标量数组
		for _, row := range rows {
			fmt.Fprintln(w, formatCell(row))
		}
		return nil
	}

	table := make([][]string, 0, len(rows)+1)
	header := make([]string, len(cols))
	for i, c := range cols {
		header[i] = strings.ToUpper(c)
	}
	table = append(table, header)
	for _, row := range rows {
		line := make([]string, len(cols))
		for i, c := range cols {
			line[i] = truncate(formatCell(cellValue(row, c)), 60)
		}
		table = append(table, line)
	}
	return writeAligned(w, table, color)
}

// renderKV 把单个对象渲染成 字段/值 两列表格。
func renderKV(w io.Writer, m map[string]any, columns []string, color bool) error {
	keys := columns
	if len(keys) == 0 {
		for k := range m {
			keys = append(keys, k)
		}
		sort.Strings(keys)
	}
	table := [][]string{{"字段", "值"}}
	for _, k := range keys {
		table = append(table, []string{k, truncate(formatCell(cellValue(m, k)), 100)})
	}
	return writeAligned(w, table, color)
}

func writeAligned(w io.Writer, table [][]string, color bool) error {
	if len(table) == 0 {
		return nil
	}
	widths := make([]int, len(table[0]))
	for _, row := range table {
		for i, cell := range row {
			if i < len(widths) && displayWidth(cell) > widths[i] {
				widths[i] = displayWidth(cell)
			}
		}
	}
	for rowIdx, row := range table {
		var b strings.Builder
		for i, cell := range row {
			if i > 0 {
				b.WriteString("  ")
			}
			b.WriteString(cell)
			if i < len(row)-1 {
				b.WriteString(strings.Repeat(" ", widths[i]-displayWidth(cell)))
			}
		}
		line := strings.TrimRight(b.String(), " ")
		if rowIdx == 0 && color {
			line = bold(line)
		}
		if _, err := fmt.Fprintln(w, line); err != nil {
			return err
		}
	}
	return nil
}

// RenderCSV 渲染逗号分隔值。
func RenderCSV(w io.Writer, data any, columns []string) error {
	data = normalize(data)
	rows, ok := asRows(data)
	if !ok {
		if m, isMap := data.(map[string]any); isMap {
			rows = []any{m}
		} else {
			_, err := fmt.Fprintln(w, formatCell(data))
			return err
		}
	}
	cols := columns
	if len(cols) == 0 {
		cols = inferColumns(rows)
	}
	cw := csv.NewWriter(w)
	if len(cols) > 0 {
		if err := cw.Write(cols); err != nil {
			return errs.NewInternalError("写入 CSV 失败: %v", err)
		}
	}
	for _, row := range rows {
		line := make([]string, 0, len(cols))
		if len(cols) == 0 {
			line = append(line, formatCell(row))
		} else {
			for _, c := range cols {
				line = append(line, formatCell(cellValue(row, c)))
			}
		}
		if err := cw.Write(line); err != nil {
			return errs.NewInternalError("写入 CSV 失败: %v", err)
		}
	}
	cw.Flush()
	return cw.Error()
}

// RenderPretty 渲染缩进的键值树，便于人类读单条记录。
func RenderPretty(w io.Writer, data any, color bool) error {
	return renderValue(w, normalize(data), 0, color)
}

func renderValue(w io.Writer, v any, indent int, color bool) error {
	pad := strings.Repeat("  ", indent)
	switch t := v.(type) {
	case map[string]any:
		keys := make([]string, 0, len(t))
		for k := range t {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			label := k
			if color {
				label = cyan(k)
			}
			switch child := t[k].(type) {
			case map[string]any:
				if len(child) == 0 {
					fmt.Fprintf(w, "%s%s: {}\n", pad, label)
					continue
				}
				fmt.Fprintf(w, "%s%s:\n", pad, label)
				if err := renderValue(w, child, indent+1, color); err != nil {
					return err
				}
			case []any:
				if len(child) == 0 {
					fmt.Fprintf(w, "%s%s: []\n", pad, label)
					continue
				}
				fmt.Fprintf(w, "%s%s:\n", pad, label)
				if err := renderValue(w, child, indent+1, color); err != nil {
					return err
				}
			default:
				fmt.Fprintf(w, "%s%s: %s\n", pad, label, formatCell(t[k]))
			}
		}
	case []any:
		for i, item := range t {
			switch item.(type) {
			case map[string]any, []any:
				fmt.Fprintf(w, "%s- [%d]\n", pad, i)
				if err := renderValue(w, item, indent+1, color); err != nil {
					return err
				}
			default:
				fmt.Fprintf(w, "%s- %s\n", pad, formatCell(item))
			}
		}
	default:
		fmt.Fprintf(w, "%s%s\n", pad, formatCell(v))
	}
	return nil
}

// Sanitize 移除服务端数据里的 ANSI 转义与控制字符。
// 网关里的渠道名、日志内容等字段可被上游写入，未净化就打印等于把终端控制权
// 交给数据源。
func Sanitize(s string) string {
	if !strings.ContainsFunc(s, isUnsafeRune) {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	skipCSI := false
	for _, r := range s {
		if skipCSI {
			// CSI 序列以 @-~ 区间的字符结尾
			if r >= 0x40 && r <= 0x7E {
				skipCSI = false
			}
			continue
		}
		if r == 0x1B {
			skipCSI = true
			continue
		}
		if isUnsafeRune(r) {
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

func isUnsafeRune(r rune) bool {
	if r == '\t' {
		return false
	}
	return r < 0x20 || r == 0x7F || (r >= 0x80 && r <= 0x9F) ||
		r == '\u2028' || r == '\u2029' || r == '\u200E' || r == '\u200F' ||
		(r >= '\u202A' && r <= '\u202E') || (r >= '\u2066' && r <= '\u2069')
}

// displayWidth 计算字符串在等宽终端里的显示宽度，东亚宽字符按 2 计。
func displayWidth(s string) int {
	w := 0
	for _, r := range s {
		if unicode.IsControl(r) {
			continue
		}
		if isWide(r) {
			w += 2
		} else {
			w++
		}
	}
	return w
}

func isWide(r rune) bool {
	return (r >= 0x1100 && r <= 0x115F) || // 韩文字母
		(r >= 0x2E80 && r <= 0xA4CF && r != 0x303F) || // CJK 部首 … 彝文
		(r >= 0xAC00 && r <= 0xD7A3) || // 韩文音节
		(r >= 0xF900 && r <= 0xFAFF) || // CJK 兼容表意
		(r >= 0xFE30 && r <= 0xFE6F) || // CJK 兼容形式
		(r >= 0xFF00 && r <= 0xFF60) || // 全角 ASCII
		(r >= 0xFFE0 && r <= 0xFFE6) ||
		(r >= 0x1F300 && r <= 0x1F64F) || // emoji
		(r >= 0x1F900 && r <= 0x1F9FF) ||
		(r >= 0x20000 && r <= 0x3FFFD)
}

func truncate(s string, max int) string {
	if displayWidth(s) <= max {
		return s
	}
	w := 0
	var b strings.Builder
	for _, r := range s {
		rw := 1
		if isWide(r) {
			rw = 2
		}
		if w+rw > max-1 {
			break
		}
		b.WriteRune(r)
		w += rw
	}
	return b.String() + "…"
}
