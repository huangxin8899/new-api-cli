// Package status 实现 status 命令域：站点状态、公开信息与运行时指标。
package status

import (
	"github.com/huangxin8899/new-api-cli/internal/client"
	"github.com/huangxin8899/new-api-cli/internal/cmdutil"

	"github.com/spf13/cobra"
)

// NewCmd 构建 status 命令树。status 本身可直接执行，等价于 status site。
func NewCmd(f *cmdutil.Factory) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "status [subcommand]",
		Short: "站点状态与运行时信息",
		Long: `查看站点状态。

不带子命令时读取 /api/status —— 该接口无需认证，因此它也是排查
"配置对不对、站点通不通" 的第一步。`,
		Args: cobra.NoArgs,
		Example: "  new-api-cli status\n" +
			"  new-api-cli status --base-url https://api.example.com\n" +
			"  new-api-cli status perf",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return f.RunSingle(cmdutil.Context(cmd), client.Request{
				Method: "GET",
				Path:   "/api/status",
				NoAuth: true,
			})
		},
	}
	cmdutil.SetRisk(cmd, cmdutil.RiskRead)
	cmd.AddCommand(
		newTestCmd(f),
		newNoticeCmd(f),
		newAboutCmd(f),
		newPricingCmd(f),
		newModelsCmd(f),
		newPerfCmd(f),
		newInstancesCmd(f),
	)
	return cmd
}

func newTestCmd(f *cmdutil.Factory) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "test",
		Short: "触发站点自检（需管理员）",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return f.RunSingle(cmdutil.Context(cmd), client.Request{
				Method: "GET",
				Path:   "/api/status/test",
			})
		},
	}
	cmdutil.SetRisk(cmd, cmdutil.RiskRead)
	return cmd
}

func newNoticeCmd(f *cmdutil.Factory) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "notice",
		Short: "读取站点公告",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return f.RunSingle(cmdutil.Context(cmd), client.Request{
				Method: "GET",
				Path:   "/api/notice",
				NoAuth: true,
			})
		},
	}
	cmdutil.SetRisk(cmd, cmdutil.RiskRead)
	return cmd
}

func newAboutCmd(f *cmdutil.Factory) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "about",
		Short: "读取站点关于页内容",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return f.RunSingle(cmdutil.Context(cmd), client.Request{
				Method: "GET",
				Path:   "/api/about",
				NoAuth: true,
			})
		},
	}
	cmdutil.SetRisk(cmd, cmdutil.RiskRead)
	return cmd
}

func newPricingCmd(f *cmdutil.Factory) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "pricing",
		Short:   "读取模型价格表",
		Args:    cobra.NoArgs,
		Example: "  new-api-cli status pricing --jq '.data[:5]'",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return f.RunSingle(cmdutil.Context(cmd), client.Request{
				Method: "GET",
				Path:   "/api/pricing",
			})
		},
	}
	cmdutil.SetRisk(cmd, cmdutil.RiskRead)
	return cmd
}

func newModelsCmd(f *cmdutil.Factory) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "models",
		Short: "列出当前身份可用的模型",
		Long: `列出当前登录身份在网关上可调用的模型。

这是 relay 侧的可用性视角，与 ` + "`new-api-cli model list`" + `（模型元数据管理）
不是同一个东西。`,
		Args:    cobra.NoArgs,
		Example: "  new-api-cli status models --jq '.data | length'",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return f.RunSingle(cmdutil.Context(cmd), client.Request{
				Method: "GET",
				Path:   "/api/models",
			})
		},
	}
	cmdutil.SetRisk(cmd, cmdutil.RiskRead)
	return cmd
}

func newPerfCmd(f *cmdutil.Factory) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "perf",
		Short: "读取运行时性能指标（需超级管理员）",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return f.RunSingle(cmdutil.Context(cmd), client.Request{
				Method: "GET",
				Path:   "/api/performance/stats",
			})
		},
	}
	cmdutil.SetRisk(cmd, cmdutil.RiskRead)
	return cmd
}

func newInstancesCmd(f *cmdutil.Factory) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "instances",
		Short: "列出集群实例（需超级管理员）",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return f.RunSingle(cmdutil.Context(cmd), client.Request{
				Method: "GET",
				Path:   "/api/system-info/instances",
			}, cmdutil.WithColumns("node_name", "version", "start_time", "last_heartbeat"))
		},
	}
	cmdutil.SetRisk(cmd, cmdutil.RiskRead)
	return cmd
}
