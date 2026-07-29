// Package cmdutil 提供命令层共享的装配件：Factory、风险等级、输入解析。
package cmdutil

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"strings"

	"github.com/huangxin8899/new-api-cli/errs"
	"github.com/huangxin8899/new-api-cli/internal/client"
	"github.com/huangxin8899/new-api-cli/internal/config"
	"github.com/huangxin8899/new-api-cli/internal/output"
	"github.com/spf13/cobra"
)

// IOStreams 把进程的三个标准流收拢成一个可替换的结构，便于测试注入。
type IOStreams struct {
	In  io.Reader
	Out io.Writer
	Err io.Writer
}

// SystemIOStreams 返回绑定到真实进程流的 IOStreams。
func SystemIOStreams() IOStreams {
	return IOStreams{In: os.Stdin, Out: os.Stdout, Err: os.Stderr}
}

// GlobalFlags 是所有命令共享的全局开关。
type GlobalFlags struct {
	Profile  string
	BaseURL  string
	Token    string
	Format   string
	JQ       string
	Yes      bool
	DryRun   bool
	Verbose  bool
	Insecure bool
	Timeout  int
	UserID   int
	NoColor  bool
}

// Factory 是命令的依赖入口。命令只依赖 Factory，不直接碰全局状态，
// 这样单测可以替换掉客户端与输出流。
type Factory struct {
	IOStreams IOStreams
	Globals   *GlobalFlags

	// Settings 惰性解析连接配置（flag > 环境变量 > 配置文件）。
	Settings func() (*config.Settings, error)
	// Client 惰性构造 HTTP 客户端。
	Client func() (*client.Client, error)
	// Emitter 按 --format/--jq 构造输出器。
	Emitter func() (*output.Emitter, error)

	// SkillContent 是编译期嵌入的 skill 文档，根目录为 skill 列表。
	// 由 root 包在 init 时注入；未注入时 skills 命令报"未嵌入"。
	SkillContent fs.FS
}

// NewFactory 装配一个使用真实文件系统与网络的 Factory。
func NewFactory(streams IOStreams, globals *GlobalFlags) *Factory {
	f := &Factory{IOStreams: streams, Globals: globals}

	var cachedSettings *config.Settings
	f.Settings = func() (*config.Settings, error) {
		if cachedSettings != nil {
			return cachedSettings, nil
		}
		s, err := config.ResolveSettings(config.Overrides{
			Profile:  globals.Profile,
			BaseURL:  globals.BaseURL,
			Token:    globals.Token,
			Insecure: globals.Insecure,
			Timeout:  globals.Timeout,
			UserID:   globals.UserID,
		})
		if err != nil {
			return nil, err
		}
		cachedSettings = s
		return s, nil
	}

	f.Client = func() (*client.Client, error) {
		s, err := f.Settings()
		if err != nil {
			return nil, err
		}
		c := client.New(s)
		c.Verbose = globals.Verbose
		c.Log = streams.Err
		return c, nil
	}

	f.Emitter = func() (*output.Emitter, error) {
		format, err := output.ParseFormat(globals.Format)
		if err != nil {
			return nil, err
		}
		e := &output.Emitter{
			Out:    streams.Out,
			Err:    streams.Err,
			Format: format,
			JQ:     globals.JQ,
			Color:  !globals.NoColor && output.SupportsColor(streams.Out),
		}
		return e, nil
	}

	return f
}

// 风险等级：写进命令注解，help 中展示，并驱动 high-risk-write 的确认门禁。
const (
	RiskRead     = "read"
	RiskWrite    = "write"
	RiskHighRisk = "high-risk-write"
)

const riskAnnotation = "new-api-cli/risk"

// SetRisk 标注命令的风险等级。
func SetRisk(cmd *cobra.Command, risk string) {
	if cmd.Annotations == nil {
		cmd.Annotations = map[string]string{}
	}
	cmd.Annotations[riskAnnotation] = risk
}

// RiskOf 读取命令的风险等级，未标注时视为只读。
func RiskOf(cmd *cobra.Command) string {
	if cmd.Annotations == nil {
		return RiskRead
	}
	if r, ok := cmd.Annotations[riskAnnotation]; ok && r != "" {
		return r
	}
	return RiskRead
}

// Confirm 是破坏性操作的统一门禁。
//
// 非交互场景（Agent、CI）必须显式传 --yes；交互场景则提示用户输入 y。
// action 用于组织提示语，例如 "删除渠道 #12 (openai-main)"。
func (f *Factory) Confirm(action string) error {
	if f.Globals.Yes {
		return nil
	}
	p := f.NewPrompter()
	if !p.Interactive() {
		return errs.NewValidationError(errs.SubtypeConfirmRequired,
			"该操作不可撤销：%s", action).
			WithHint("确认无误后加 --yes 执行").
			WithParams("--yes")
	}
	if !p.YesNo(fmt.Sprintf("确认%s？此操作不可撤销", action)) {
		return errs.NewValidationError(errs.SubtypeConfirmRequired, "已取消：%s", action)
	}
	return nil
}

// ReadJSONInput 解析 --data/--params 这类输入：
// 内联 JSON、@file 读文件、- 读 stdin。
func (f *Factory) ReadJSONInput(raw, flagName string) (map[string]any, error) {
	text, err := f.ReadTextInput(raw, flagName)
	if err != nil || strings.TrimSpace(text) == "" {
		return nil, err
	}
	var out map[string]any
	if err := json.Unmarshal([]byte(text), &out); err != nil {
		return nil, errs.NewValidationError(errs.SubtypeInvalidJSON,
			"%s 不是合法的 JSON 对象: %v", flagName, err).
			WithHint(`示例：%s '{"name":"demo"}'；也支持 @file.json 或 - 从标准输入读取`, flagName).
			WithParams(flagName)
	}
	return out, nil
}

// ReadTextInput 解析文本输入，支持 @file 与 -（stdin）。
func (f *Factory) ReadTextInput(raw, flagName string) (string, error) {
	switch {
	case raw == "":
		return "", nil
	case raw == "-":
		b, err := io.ReadAll(f.IOStreams.In)
		if err != nil {
			return "", errs.NewValidationError(errs.SubtypeFileIO,
				"从标准输入读取 %s 失败: %v", flagName, err).WithParams(flagName)
		}
		return string(b), nil
	case strings.HasPrefix(raw, "@"):
		path := strings.TrimPrefix(raw, "@")
		b, err := os.ReadFile(path)
		if err != nil {
			return "", errs.NewValidationError(errs.SubtypeFileIO,
				"读取文件 %s 失败: %v", path, err).WithParams(flagName)
		}
		return string(b), nil
	default:
		return raw, nil
	}
}

// EmitResult 是命令输出结果的统一出口。
func (f *Factory) EmitResult(r output.Result) error {
	e, err := f.Emitter()
	if err != nil {
		return err
	}
	return e.Emit(r)
}

// EmitData 是只有数据、无附加元信息时的便捷出口。
func (f *Factory) EmitData(data any, columns ...string) error {
	return f.EmitResult(output.Result{Data: data, Columns: columns})
}

// DryRunResult 在 --dry-run 下输出将要发起的请求，不真正调用。
func (f *Factory) DryRunResult(req client.Request) error {
	payload := map[string]any{
		"method": strings.ToUpper(req.Method),
		"path":   client.NormalizePath(req.Path),
	}
	if len(req.Query) > 0 {
		payload["query"] = req.Query
	}
	if req.Body != nil {
		payload["body"] = req.Body
	} else if len(req.RawBody) > 0 {
		var parsed any
		if json.Unmarshal(req.RawBody, &parsed) == nil {
			payload["body"] = parsed
		} else {
			payload["body"] = string(req.RawBody)
		}
	}
	if s, err := f.Settings(); err == nil {
		payload["base_url"] = s.BaseURL
	}
	return f.EmitResult(output.Result{Data: payload, DryRun: true, Message: "dry-run：未发起真实请求"})
}

// AnyFlagChanged 判断给定的 flag 是否至少有一个被显式传入。
//
// 更新类命令用它区分"用户想把字段设为零值"与"用户没提这个字段"——
// 只看变量值无法区分 --quota 0 和根本没传 --quota。
func AnyFlagChanged(cmd *cobra.Command, names ...string) bool {
	for _, n := range names {
		if cmd.Flags().Changed(n) {
			return true
		}
	}
	return false
}

// Context 返回命令的上下文，cobra 未注入时退回 Background。
func Context(cmd *cobra.Command) context.Context {
	if ctx := cmd.Context(); ctx != nil {
		return ctx
	}
	return context.Background()
}
