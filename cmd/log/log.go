// Package log 实现 log 命令域：调用日志查询与用量统计。
package log

import (
	"time"

	"github.com/QuantumNous/new-api-cli/errs"
	"github.com/QuantumNous/new-api-cli/internal/client"
	"github.com/QuantumNous/new-api-cli/internal/cmdutil"

	"github.com/spf13/cobra"
)

// 日志类型取值来自 New API 的 model.LogType* 常量。
var logTypes = map[string]int{
	"all":     0,
	"topup":   1,
	"consume": 2,
	"manage":  3,
	"system":  4,
	"error":   5,
	"refund":  6,
	"login":   7,
}

var defaultColumns = []string{"created_at", "username", "token_name", "model_name", "quota", "prompt_tokens", "completion_tokens", "use_time", "channel_id"}

// NewCmd 构造 log 命令树。
func NewCmd(f *cmdutil.Factory) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "log <subcommand>",
		Short: "调用日志与用量统计",
		Long: `查询调用日志。

` + "`list`" + ` / ` + "`stat`" + ` 查全站数据，需要管理员；` + "`self`" + ` / ` + "`self-stat`" + `
查当前用户自己的数据，普通用户即可。`,
	}
	cmd.AddCommand(
		newListCmd(f, false),
		newListCmd(f, true),
		newStatCmd(f, false),
		newStatCmd(f, true),
	)
	return cmd
}

// logFilter 收拢日志查询的过滤条件。self 与全站两条路径共用同一组 flag，
// 只是 self 路径不接受 --username / --channel。
type logFilter struct {
	logType    string
	since      string
	start      string
	end        string
	username   string
	tokenName  string
	modelName  string
	group      string
	channel    int
	requestID  string
	upstreamID string
}

func (fl *logFilter) register(cmd *cobra.Command, self bool) {
	s := cmd.Flags()
	s.StringVar(&fl.logType, "type", "all", "日志类型：all|topup|consume|manage|system|error|refund|login")
	s.StringVar(&fl.since, "since", "", "最近多久，如 24h / 7d（与 --start 互斥）")
	s.StringVar(&fl.start, "start", "", "起始时间：Unix 秒或 2026-01-31 10:00:00")
	s.StringVar(&fl.end, "end", "", "结束时间，同上")
	s.StringVar(&fl.tokenName, "token-name", "", "按令牌名过滤")
	s.StringVar(&fl.modelName, "model", "", "按模型名过滤")
	s.StringVar(&fl.group, "group", "", "按分组过滤")
	s.StringVar(&fl.requestID, "request-id", "", "按请求 ID 精确查找")
	s.StringVar(&fl.upstreamID, "upstream-request-id", "", "按上游请求 ID 查找")
	if !self {
		s.StringVar(&fl.username, "username", "", "按用户名过滤")
		s.IntVar(&fl.channel, "channel", 0, "按渠道 ID 过滤")
	}
}

// query 把过滤条件编成查询参数。--since 与 --start 互斥，避免出现
// 两个都给却只有一个生效的静默歧义。
func (fl *logFilter) query(cmd *cobra.Command) (*cmdutil.Query, error) {
	q := cmdutil.NewQuery()

	if fl.since != "" && fl.start != "" {
		return nil, errs.NewValidationError(errs.SubtypeInvalidArgument,
			"--since 与 --start 只能给一个").
			WithHint("相对区间用 --since 7d，绝对区间用 --start/--end").
			WithParams("--since", "--start")
	}

	t, err := cmdutil.ParseEnumInt(fl.logType, "--type", logTypes)
	if err != nil {
		return nil, err
	}
	// 0 是"不按类型过滤"，Int 会跳过零值，正好符合语义。
	q.Int("type", t)

	switch {
	case fl.since != "":
		ts, err := cmdutil.ParseRelativeSince(fl.since, "--since", time.Now())
		if err != nil {
			return nil, err
		}
		q.Int64("start_timestamp", ts)
	case fl.start != "":
		ts, err := cmdutil.ParseTimestamp(fl.start, "--start")
		if err != nil {
			return nil, err
		}
		q.Int64("start_timestamp", ts)
	}
	if fl.end != "" {
		ts, err := cmdutil.ParseTimestamp(fl.end, "--end")
		if err != nil {
			return nil, err
		}
		q.Int64("end_timestamp", ts)
	}

	q.Str("username", fl.username).
		Str("token_name", fl.tokenName).
		Str("model_name", fl.modelName).
		Str("group", fl.group).
		Str("request_id", fl.requestID).
		Str("upstream_request_id", fl.upstreamID).
		Int("channel", fl.channel)
	return q, nil
}

func newListCmd(f *cmdutil.Factory, self bool) *cobra.Command {
	var lf cmdutil.ListFlags
	var filter logFilter

	use, short, path := "list", "查询全站调用日志（需管理员）", "/api/log/"
	example := "  new-api-cli log list --since 24h\n  new-api-cli log list --model gpt-4o --format table"
	if self {
		use, short, path = "self", "查询自己的调用日志", "/api/log/self"
		example = "  new-api-cli log self --since 7d\n  new-api-cli log self --request-id abc123"
	}

	cmd := &cobra.Command{
		Use:     use,
		Short:   short,
		Args:    cobra.NoArgs,
		Example: example,
		RunE: func(cmd *cobra.Command, _ []string) error {
			q, err := filter.query(cmd)
			if err != nil {
				return err
			}
			return f.RunList(cmdutil.Context(cmd), client.Request{
				Method: "GET",
				Path:   path,
				Query:  q.Values(),
			}, &lf, defaultColumns)
		},
	}
	lf.Register(cmd)
	filter.register(cmd, self)
	cmdutil.SetRisk(cmd, cmdutil.RiskRead)
	return cmd
}

func newStatCmd(f *cmdutil.Factory, self bool) *cobra.Command {
	var filter logFilter

	use, short, path := "stat", "统计全站消耗额度与 RPM/TPM（需管理员）", "/api/log/stat"
	if self {
		use, short, path = "self-stat", "统计自己的消耗额度与 RPM/TPM", "/api/log/self/stat"
	}

	cmd := &cobra.Command{
		Use:     use,
		Short:   short,
		Args:    cobra.NoArgs,
		Example: "  new-api-cli log " + use + " --since 24h",
		RunE: func(cmd *cobra.Command, _ []string) error {
			q, err := filter.query(cmd)
			if err != nil {
				return err
			}
			return f.RunSingle(cmdutil.Context(cmd), client.Request{
				Method: "GET",
				Path:   path,
				Query:  q.Values(),
			}, cmdutil.WithColumns("quota", "rpm", "tpm"))
		},
	}
	filter.register(cmd, self)
	cmdutil.SetRisk(cmd, cmdutil.RiskRead)
	return cmd
}
