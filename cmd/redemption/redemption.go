// Package redemption 实现 redemption 命令域：兑换码的批量签发与管理。
package redemption

import (
	"fmt"
	"strings"

	"github.com/huangxin8899/new-api-cli/errs"
	"github.com/huangxin8899/new-api-cli/internal/client"
	"github.com/huangxin8899/new-api-cli/internal/cmdutil"
	"github.com/huangxin8899/new-api-cli/internal/output"
	"github.com/spf13/cobra"
)

var defaultColumns = []string{"id", "name", "status", "quota", "used_user_id", "created_time", "redeemed_time", "expired_time"}

// NewCmd 构造 redemption 命令树。
func NewCmd(f *cmdutil.Factory) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "redemption <subcommand>",
		Aliases: []string{"redeem"},
		Short:   "兑换码管理（需管理员）",
		Long: `批量签发与管理兑换码。

兑换码明文只在 ` + "`create`" + ` 的返回里出现一次，列表接口不会再给出。
请在创建时就保存好输出。`,
	}
	cmd.AddCommand(
		newListCmd(f),
		newSearchCmd(f),
		newGetCmd(f),
		newCreateCmd(f),
		newUpdateCmd(f),
		newDeleteCmd(f),
		newPruneCmd(f),
	)
	return cmd
}

func newListCmd(f *cmdutil.Factory) *cobra.Command {
	var lf cmdutil.ListFlags
	cmd := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "列出兑换码",
		Args:    cobra.NoArgs,
		Example: "  new-api-cli redemption list\n  new-api-cli redemption list --all --format table",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return f.RunList(cmdutil.Context(cmd), client.Request{
				Method: "GET",
				Path:   "/api/redemption/",
			}, &lf, defaultColumns)
		},
	}
	lf.Register(cmd)
	cmdutil.SetRisk(cmd, cmdutil.RiskRead)
	return cmd
}

func newSearchCmd(f *cmdutil.Factory) *cobra.Command {
	var lf cmdutil.ListFlags
	var keyword string
	cmd := &cobra.Command{
		Use:     "search <keyword>",
		Short:   "按名称或码搜索兑换码",
		Args:    cobra.MaximumNArgs(1),
		Example: "  new-api-cli redemption search 双十一",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 1 {
				keyword = args[0]
			}
			if strings.TrimSpace(keyword) == "" {
				return errs.NewValidationError(errs.SubtypeMissingArgument,
					"需要搜索关键字").
					WithHint("例如 new-api-cli redemption search 双十一").
					WithParams("<keyword>")
			}
			return f.RunList(cmdutil.Context(cmd), client.Request{
				Method: "GET",
				Path:   "/api/redemption/search",
				Query:  cmdutil.NewQuery().Str("keyword", keyword).Values(),
			}, &lf, defaultColumns)
		},
	}
	lf.Register(cmd)
	cmd.Flags().StringVar(&keyword, "keyword", "", "搜索关键字（等价于位置参数）")
	cmdutil.SetRisk(cmd, cmdutil.RiskRead)
	return cmd
}

func newGetCmd(f *cmdutil.Factory) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "get <id>",
		Short: "查看单个兑换码",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := cmdutil.ParseID(args[0], "<id>")
			if err != nil {
				return err
			}
			return f.RunSingle(cmdutil.Context(cmd), client.Request{
				Method: "GET",
				Path:   fmt.Sprintf("/api/redemption/%d", id),
			}, cmdutil.WithColumns(defaultColumns...))
		},
	}
	cmdutil.SetRisk(cmd, cmdutil.RiskRead)
	return cmd
}

func newCreateCmd(f *cmdutil.Factory) *cobra.Command {
	var name, expiredAt string
	var quota, count int
	cmd := &cobra.Command{
		Use:     "create",
		Aliases: []string{"add"},
		Short:   "批量签发兑换码",
		Long: `批量签发兑换码。

返回的 data 是本次生成的码明文数组 —— 只此一次，请立刻保存。
服务端限制：--name 1-20 字符，--count 1-100。`,
		Args:    cobra.NoArgs,
		Example: "  new-api-cli redemption create --name 双十一 --quota 500000 --count 10\n  new-api-cli redemption create --name 试用 --quota 100000 --count 1 --expired-at 2026-12-31",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if strings.TrimSpace(name) == "" {
				return errs.NewValidationError(errs.SubtypeMissingArgument, "--name 为必填").
					WithParams("--name")
			}
			if count <= 0 || count > 100 {
				return errs.NewValidationError(errs.SubtypeInvalidArgument,
					"--count 需在 1-100 之间，收到 %d", count).
					WithParams("--count")
			}
			if quota <= 0 {
				return errs.NewValidationError(errs.SubtypeInvalidArgument,
					"--quota 需为正数").
					WithHint("quota 是原始额度数值，站点默认 500000 约等于 $1").
					WithParams("--quota")
			}
			body := map[string]any{
				"name":         name,
				"quota":        quota,
				"count":        count,
				"expired_time": 0,
			}
			if cmd.Flags().Changed("expired-at") {
				v, err := expiredValue(expiredAt)
				if err != nil {
					return err
				}
				body["expired_time"] = v
			}
			ctx := cmdutil.Context(cmd)
			req := client.Request{Method: "POST", Path: "/api/redemption/", Body: body}
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
			return f.EmitResult(output.Result{
				Data:    data,
				Meta:    &output.Meta{Count: count},
				Message: "以上兑换码明文仅此一次可见，请立即保存",
			})
		},
	}
	fl := cmd.Flags()
	fl.StringVar(&name, "name", "", "批次名称（1-20 字符）")
	fl.IntVar(&quota, "quota", 0, "每个码的额度（原始 quota 数值）")
	fl.IntVar(&count, "count", 1, "生成数量（1-100）")
	fl.StringVar(&expiredAt, "expired-at", "", "过期时间：never | Unix 秒 | 2026-12-31")
	cmdutil.SetRisk(cmd, cmdutil.RiskWrite)
	return cmd
}

// expiredValue 把 --expired-at 映射到服务端字段；0 表示永不过期。
func expiredValue(raw string) (int64, error) {
	if strings.EqualFold(raw, "never") || raw == "0" {
		return 0, nil
	}
	return cmdutil.ParseTimestamp(raw, "--expired-at")
}

func newUpdateCmd(f *cmdutil.Factory) *cobra.Command {
	var name, expiredAt string
	var quota, status int
	cmd := &cobra.Command{
		Use:   "update <id>",
		Short: "更新兑换码",
		Long: `更新兑换码。

服务端为整体替换语义，本命令先读取当前记录再合并你传入的 flag。`,
		Args:    cobra.ExactArgs(1),
		Example: "  new-api-cli redemption update 7 --status 2\n  new-api-cli redemption update 7 --name 双十一-延期 --expired-at 2027-01-31",
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := cmdutil.ParseID(args[0], "<id>")
			if err != nil {
				return err
			}
			fl := cmd.Flags()
			if !cmdutil.AnyFlagChanged(cmd, "name", "quota", "status", "expired-at") {
				return errs.NewValidationError(errs.SubtypeInvalidArgument, "没有要更新的字段").
					WithHint("至少指定一个，如 --name / --status / --quota")
			}
			ctx := cmdutil.Context(cmd)
			body, err := f.FetchObject(ctx, fmt.Sprintf("/api/redemption/%d", id), fmt.Sprintf("兑换码 %d", id))
			if err != nil {
				return err
			}
			body["id"] = id
			// 码明文不参与更新，避免把服务端的值覆盖成空。
			delete(body, "key")
			if fl.Changed("name") {
				body["name"] = name
			}
			if fl.Changed("quota") {
				body["quota"] = quota
			}
			if fl.Changed("status") {
				body["status"] = status
			}
			if fl.Changed("expired-at") {
				v, err := expiredValue(expiredAt)
				if err != nil {
					return err
				}
				body["expired_time"] = v
			}
			return f.RunSingle(ctx, client.Request{
				Method: "PUT",
				Path:   "/api/redemption/",
				Body:   body,
			}, cmdutil.WithColumns(defaultColumns...))
		},
	}
	fl := cmd.Flags()
	fl.StringVar(&name, "name", "", "批次名称")
	fl.IntVar(&quota, "quota", 0, "额度")
	fl.IntVar(&status, "status", 0, "状态：1 未使用 2 已禁用 3 已使用")
	fl.StringVar(&expiredAt, "expired-at", "", "过期时间：never | Unix 秒 | 2026-12-31")
	cmdutil.SetRisk(cmd, cmdutil.RiskWrite)
	return cmd
}

func newDeleteCmd(f *cmdutil.Factory) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "delete <id>",
		Aliases: []string{"rm"},
		Short:   "删除兑换码（不可恢复）",
		Args:    cobra.ExactArgs(1),
		Example: "  new-api-cli redemption delete 7 --yes",
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := cmdutil.ParseID(args[0], "<id>")
			if err != nil {
				return err
			}
			if err := f.Confirm(fmt.Sprintf("删除兑换码 #%d", id)); err != nil {
				return err
			}
			return f.RunSingle(cmdutil.Context(cmd), client.Request{
				Method: "DELETE",
				Path:   fmt.Sprintf("/api/redemption/%d", id),
			}, cmdutil.WithFallback(map[string]any{"id": id, "deleted": true}))
		},
	}
	cmdutil.SetRisk(cmd, cmdutil.RiskHighRisk)
	return cmd
}

func newPruneCmd(f *cmdutil.Factory) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "prune",
		Short: "清理已使用/已失效的兑换码（不可恢复）",
		Long: `批量删除服务端判定为已失效的兑换码。

失效判定由服务端做（已兑换、已禁用、已过期）。删除后无法恢复，
建议先用 ` + "`redemption list --all`" + ` 确认范围。`,
		Args:    cobra.NoArgs,
		Example: "  new-api-cli redemption prune --yes",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := f.Confirm("清理全部已失效兑换码"); err != nil {
				return err
			}
			return f.RunSingle(cmdutil.Context(cmd), client.Request{
				Method: "DELETE",
				Path:   "/api/redemption/invalid",
			}, cmdutil.WithMessage("data 为被删除的条数"))
		},
	}
	cmdutil.SetRisk(cmd, cmdutil.RiskHighRisk)
	return cmd
}
