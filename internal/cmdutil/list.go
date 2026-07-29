package cmdutil

import (
	"context"
	"net/url"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api-cli/errs"
	"github.com/QuantumNous/new-api-cli/internal/client"
	"github.com/QuantumNous/new-api-cli/internal/output"
	"github.com/spf13/cobra"
)

// ListFlags 是所有列表命令共享的翻页开关。
type ListFlags struct {
	Page     int
	PageSize int
	All      bool
	Limit    int
	Columns  []string
}

// Register 把翻页 flag 挂到命令上。
func (l *ListFlags) Register(cmd *cobra.Command) {
	cmd.Flags().IntVar(&l.Page, "page", 1, "页码，从 1 开始")
	cmd.Flags().IntVar(&l.PageSize, "page-size", 20, "每页条数（服务端上限 100）")
	cmd.Flags().BoolVar(&l.All, "all", false, "自动翻页取回全部结果")
	cmd.Flags().IntVar(&l.Limit, "limit", 0, "配合 --all 使用，最多取回多少条（0 = 不限）")
	cmd.Flags().StringSliceVar(&l.Columns, "columns", nil, "table/csv 输出的列，如 --columns id,name,status")
}

// Options 转换成客户端可用的翻页参数。
func (l *ListFlags) Options() client.ListOptions {
	return client.ListOptions{Page: l.Page, PageSize: l.PageSize, All: l.All, Limit: l.Limit}
}

// RunList 执行一次列表查询并输出。defaultColumns 在用户未指定 --columns 时生效。
func (f *Factory) RunList(ctx context.Context, req client.Request, l *ListFlags, defaultColumns []string) error {
	if f.Globals.DryRun {
		return f.DryRunResult(req)
	}
	c, err := f.Client()
	if err != nil {
		return err
	}
	res, err := c.List(ctx, req, l.Options())
	if err != nil {
		return err
	}

	columns := defaultColumns
	if len(l.Columns) > 0 {
		columns = l.Columns
	}
	meta := &output.Meta{Count: len(res.Items), Total: res.Total, Page: res.Page}
	if !l.All {
		meta.PageSize = l.PageSize
	}
	result := output.Result{Data: res.Items, Meta: meta, Columns: columns}
	if res.Truncated {
		result.Message = "结果已截断（受 --limit 或翻页上限限制），加大 --limit 可取回更多"
	}
	return f.EmitResult(result)
}

// RunSingle 执行一次返回单个对象的调用并输出。
func (f *Factory) RunSingle(ctx context.Context, req client.Request, opts ...SingleOption) error {
	cfg := singleConfig{}
	for _, opt := range opts {
		opt(&cfg)
	}
	if f.Globals.DryRun {
		return f.DryRunResult(req)
	}
	c, err := f.Client()
	if err != nil {
		return err
	}
	resp, err := c.Do(ctx, req)
	if err != nil {
		return err
	}
	data, err := resp.Any()
	if err != nil {
		return err
	}
	if data == nil && cfg.fallback != nil {
		data = cfg.fallback
	}
	message := cfg.message
	if message == "" {
		message = resp.Message
	}
	return f.EmitResult(output.Result{Data: data, Columns: cfg.columns, Message: message})
}

// FetchObject 读取一个对象并返回通用 map。
//
// 服务端的多数更新接口是整体替换语义：只提交改动字段会把其余字段清空。
// 更新命令因此先用它取回当前对象，把 flag 合并上去后再整体提交。返回
// map 而非结构体，是为了让服务端新增的字段也能原样穿过读-改-写循环，
// 而不是被 CLI 的结构体定义悄悄丢掉。
//
// label 用于错误信息，例如 "用户 42"。
func (f *Factory) FetchObject(ctx context.Context, path, label string) (map[string]any, error) {
	c, err := f.Client()
	if err != nil {
		return nil, err
	}
	resp, err := c.Do(ctx, client.Request{Method: "GET", Path: path})
	if err != nil {
		return nil, err
	}
	data, err := resp.Any()
	if err != nil {
		return nil, err
	}
	m, ok := data.(map[string]any)
	if !ok {
		return nil, errs.NewAPIError(errs.SubtypeBadResponse,
			"读取%s失败：响应不是对象", label).
			WithHint("确认该 ID 存在且当前账号有权访问")
	}
	return m, nil
}

type singleConfig struct {
	columns  []string
	message  string
	fallback any
}

// SingleOption 定制 RunSingle 的输出。
type SingleOption func(*singleConfig)

// WithColumns 指定 table/csv 的列。
func WithColumns(cols ...string) SingleOption {
	return func(c *singleConfig) { c.columns = cols }
}

// WithMessage 指定人类可读的结果摘要。
func WithMessage(msg string) SingleOption {
	return func(c *singleConfig) { c.message = msg }
}

// WithFallback 在服务端返回空 data 时用它兜底（例如回显刚提交的对象）。
func WithFallback(v any) SingleOption {
	return func(c *singleConfig) { c.fallback = v }
}

// ParseID 解析形如 "12" 的资源 ID 参数。
func ParseID(raw, name string) (int, error) {
	id, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || id <= 0 {
		return 0, errs.NewValidationError(errs.SubtypeInvalidArgument,
			"%s 需要是正整数，收到 %q", name, raw).
			WithParams(name)
	}
	return id, nil
}

// Query 是构造查询参数的小助手：跳过零值，避免发送一堆空参数。
type Query struct{ values url.Values }

// NewQuery 创建空查询。
func NewQuery() *Query { return &Query{values: url.Values{}} }

// Str 添加一个非空字符串参数。
func (q *Query) Str(key, value string) *Query {
	if strings.TrimSpace(value) != "" {
		q.values.Set(key, value)
	}
	return q
}

// Int 添加一个非零整数参数。
func (q *Query) Int(key string, value int) *Query {
	if value != 0 {
		q.values.Set(key, strconv.Itoa(value))
	}
	return q
}

// Int64 添加一个非零 int64 参数（时间戳）。
func (q *Query) Int64(key string, value int64) *Query {
	if value != 0 {
		q.values.Set(key, strconv.FormatInt(value, 10))
	}
	return q
}

// Bool 无条件添加布尔参数。
func (q *Query) Bool(key string, value bool) *Query {
	q.values.Set(key, strconv.FormatBool(value))
	return q
}

// Values 返回底层 url.Values。
func (q *Query) Values() url.Values { return q.values }
