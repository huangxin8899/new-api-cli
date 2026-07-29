// Package data 实现 data 命令域：额度消耗的按日聚合统计。
//
// 与 log 域的区别：log 是逐条调用明细，data 是服务端预聚合的按日/按模型
// 汇总，适合画趋势图或做月度对账，数据量小得多。
package data

import (
	"time"

	"github.com/QuantumNous/new-api-cli/errs"
	"github.com/QuantumNous/new-api-cli/internal/client"
	"github.com/QuantumNous/new-api-cli/internal/cmdutil"
	"github.com/spf13/cobra"
)

var defaultColumns = []string{"created_at", "user_id", "username", "model_name", "count", "quota", "token_used"}

// NewCmd 构造 data 命令树。
func NewCmd(f *cmdutil.Factory) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "data <subcommand>",
		Short: "额度消耗的按日聚合统计",
		Long: `查看按日聚合的额度消耗数据。

所有子命令都需要时间范围。未指定时默认取最近 7 天。`,
	}
	cmd.AddCommand(
		newListCmd(f),
		newUsersCmd(f),
		newSelfCmd(f),
		newFlowCmd(f),
	)
	return cmd
}

// rangeFlags 是所有 data 子命令共享的时间范围参数。
type rangeFlags struct {
	since string
	start string
	end   string
}

func (r *rangeFlags) register(cmd *cobra.Command) {
	fl := cmd.Flags()
	fl.StringVar(&r.since, "since", "7d", "相对时间范围，如 7d、24h、30d")
	fl.StringVar(&r.start, "start", "", "起始时间（覆盖 --since）")
	fl.StringVar(&r.end, "end", "", "结束时间，默认当前")
}

// resolve 把时间参数落成服务端要求的起止 Unix 秒。
// 这些接口把 0 当作非法值（flow 系列会直接报错），所以两端都必须给实值。
func (r *rangeFlags) resolve(cmd *cobra.Command) (int64, int64, error) {
	now := time.Now()
	end := now.Unix()
	if r.end != "" {
		v, err := cmdutil.ParseTimestamp(r.end, "--end")
		if err != nil {
			return 0, 0, err
		}
		end = v
	}

	var start int64
	switch {
	case cmd.Flags().Changed("start"):
		v, err := cmdutil.ParseTimestamp(r.start, "--start")
		if err != nil {
			return 0, 0, err
		}
		start = v
	default:
		v, err := cmdutil.ParseRelativeSince(r.since, "--since", now)
		if err != nil {
			return 0, 0, err
		}
		start = v
	}

	if start <= 0 {
		start = now.AddDate(0, 0, -7).Unix()
	}
	if end < start {
		return 0, 0, errs.NewValidationError(errs.SubtypeInvalidArgument,
			"结束时间早于起始时间").
			WithHint("检查 --start 与 --end 的先后顺序").
			WithParams("--start", "--end")
	}
	return start, end, nil
}

func (r *rangeFlags) query(cmd *cobra.Command, extra func(*cmdutil.Query)) (*cmdutil.Query, error) {
	start, end, err := r.resolve(cmd)
	if err != nil {
		return nil, err
	}
	q := cmdutil.NewQuery().
		Int64("start_timestamp", start).
		Int64("end_timestamp", end)
	if extra != nil {
		extra(q)
	}
	return q, nil
}

func newListCmd(f *cmdutil.Factory) *cobra.Command {
	var rf rangeFlags
	var username string
	var columns []string
	cmd := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "全站按日消耗（需管理员）",
		Args:    cobra.NoArgs,
		Example: "  new-api-cli data list --since 30d\n  new-api-cli data list --username alice --format table",
		RunE: func(cmd *cobra.Command, _ []string) error {
			q, err := rf.query(cmd, func(q *cmdutil.Query) { q.Str("username", username) })
			if err != nil {
				return err
			}
			return f.RunSingle(cmdutil.Context(cmd), client.Request{
				Method: "GET",
				Path:   "/api/data/",
				Query:  q.Values(),
			}, cmdutil.WithColumns(pick(columns, defaultColumns)...))
		},
	}
	rf.register(cmd)
	cmd.Flags().StringVar(&username, "username", "", "只看该用户")
	cmd.Flags().StringSliceVar(&columns, "columns", nil, "table/csv 输出的列")
	cmdutil.SetRisk(cmd, cmdutil.RiskRead)
	return cmd
}

func newUsersCmd(f *cmdutil.Factory) *cobra.Command {
	var rf rangeFlags
	var columns []string
	cmd := &cobra.Command{
		Use:     "users",
		Short:   "按用户汇总消耗（需管理员）",
		Args:    cobra.NoArgs,
		Example: "  new-api-cli data users --since 30d --format table",
		RunE: func(cmd *cobra.Command, _ []string) error {
			q, err := rf.query(cmd, nil)
			if err != nil {
				return err
			}
			return f.RunSingle(cmdutil.Context(cmd), client.Request{
				Method: "GET",
				Path:   "/api/data/users",
				Query:  q.Values(),
			}, cmdutil.WithColumns(pick(columns, []string{"user_id", "username", "count", "quota", "token_used"})...))
		},
	}
	rf.register(cmd)
	cmd.Flags().StringSliceVar(&columns, "columns", nil, "table/csv 输出的列")
	cmdutil.SetRisk(cmd, cmdutil.RiskRead)
	return cmd
}

func newSelfCmd(f *cmdutil.Factory) *cobra.Command {
	var rf rangeFlags
	var columns []string
	cmd := &cobra.Command{
		Use:   "self",
		Short: "我的按日消耗",
		Long: `查看当前登录用户的按日消耗。

服务端限制单次查询跨度不超过 31 天，超出会直接报错。`,
		Args:    cobra.NoArgs,
		Example: "  new-api-cli data self\n  new-api-cli data self --since 30d --format table",
		RunE: func(cmd *cobra.Command, _ []string) error {
			q, err := rf.query(cmd, nil)
			if err != nil {
				return err
			}
			return f.RunSingle(cmdutil.Context(cmd), client.Request{
				Method: "GET",
				Path:   "/api/data/self",
				Query:  q.Values(),
			}, cmdutil.WithColumns(pick(columns, defaultColumns)...))
		},
	}
	rf.register(cmd)
	cmd.Flags().StringSliceVar(&columns, "columns", nil, "table/csv 输出的列")
	cmdutil.SetRisk(cmd, cmdutil.RiskRead)
	return cmd
}

func newFlowCmd(f *cmdutil.Factory) *cobra.Command {
	var rf rangeFlags
	var username string
	var self bool
	var columns []string
	cmd := &cobra.Command{
		Use:     "flow",
		Short:   "额度流水（充值、消耗、退款等）",
		Args:    cobra.NoArgs,
		Example: "  new-api-cli data flow --since 30d\n  new-api-cli data flow --self",
		RunE: func(cmd *cobra.Command, _ []string) error {
			q, err := rf.query(cmd, func(q *cmdutil.Query) {
				if !self {
					q.Str("username", username)
				}
			})
			if err != nil {
				return err
			}
			path := "/api/data/flow"
			if self {
				path = "/api/data/flow/self"
			}
			return f.RunSingle(cmdutil.Context(cmd), client.Request{
				Method: "GET",
				Path:   path,
				Query:  q.Values(),
			}, cmdutil.WithColumns(pick(columns, nil)...))
		},
	}
	rf.register(cmd)
	cmd.Flags().StringVar(&username, "username", "", "只看该用户（需管理员）")
	cmd.Flags().BoolVar(&self, "self", false, "只看自己，普通用户用这个")
	cmd.Flags().StringSliceVar(&columns, "columns", nil, "table/csv 输出的列")
	cmdutil.SetRisk(cmd, cmdutil.RiskRead)
	return cmd
}

// pick 让 --columns 覆盖默认列。
func pick(user, fallback []string) []string {
	if len(user) > 0 {
		return user
	}
	return fallback
}
