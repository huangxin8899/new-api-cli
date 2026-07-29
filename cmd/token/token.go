// Package token 实现 token 命令域：当前登录用户的 API 令牌管理。
package token

import (
	"fmt"
	"strings"

	"github.com/huangxin8899/new-api-cli/errs"
	"github.com/huangxin8899/new-api-cli/internal/client"
	"github.com/huangxin8899/new-api-cli/internal/cmdutil"

	"github.com/spf13/cobra"
)

// defaultColumns 是令牌列表的默认投影。服务端返回的 key 已是掩码，可安全展示。
var defaultColumns = []string{"id", "name", "status", "key", "used_quota", "remain_quota", "unlimited_quota", "group", "expired_time"}

// specFlags 列出所有可变字段的 flag 名，供"是否有改动"判定复用。
var specFlags = []string{"name", "quota", "unlimited", "expired-at", "group", "model-limits", "allow-ips", "cross-group-retry", "status"}

// NewCmd 构造 token 命令树。
func NewCmd(f *cmdutil.Factory) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "token <subcommand>",
		Short: "API 令牌（sk-...）管理",
		Long: `管理当前登录用户的 API 令牌。

令牌明文只在 ` + "`token key`" + ` 显式索取时可见；列表与详情中一律为掩码。`,
	}
	cmd.AddCommand(
		newListCmd(f),
		newSearchCmd(f),
		newGetCmd(f),
		newKeyCmd(f),
		newCreateCmd(f),
		newUpdateCmd(f),
		newDeleteCmd(f),
	)
	return cmd
}

func newListCmd(f *cmdutil.Factory) *cobra.Command {
	var lf cmdutil.ListFlags
	cmd := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "列出我的令牌",
		Args:    cobra.NoArgs,
		Example: "  new-api-cli token list\n  new-api-cli token list --all --format table",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return f.RunList(cmdutil.Context(cmd), client.Request{
				Method: "GET",
				Path:   "/api/token/",
			}, &lf, defaultColumns)
		},
	}
	lf.Register(cmd)
	cmdutil.SetRisk(cmd, cmdutil.RiskRead)
	return cmd
}

func newSearchCmd(f *cmdutil.Factory) *cobra.Command {
	var lf cmdutil.ListFlags
	var keyword, tokenKey string
	cmd := &cobra.Command{
		Use:     "search",
		Short:   "按名称或 key 搜索我的令牌",
		Args:    cobra.NoArgs,
		Example: "  new-api-cli token search --keyword prod",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if keyword == "" && tokenKey == "" {
				return errs.NewValidationError(errs.SubtypeMissingArgument,
					"至少需要 --keyword 或 --token 之一").
					WithHint("例如 new-api-cli token search --keyword prod").
					WithParams("--keyword", "--token")
			}
			return f.RunList(cmdutil.Context(cmd), client.Request{
				Method: "GET",
				Path:   "/api/token/search",
				Query:  cmdutil.NewQuery().Str("keyword", keyword).Str("token", tokenKey).Values(),
			}, &lf, defaultColumns)
		},
	}
	lf.Register(cmd)
	cmd.Flags().StringVar(&keyword, "keyword", "", "名称关键字")
	cmd.Flags().StringVar(&tokenKey, "token", "", "令牌 key 片段")
	cmdutil.SetRisk(cmd, cmdutil.RiskRead)
	return cmd
}

func newGetCmd(f *cmdutil.Factory) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "get <id>",
		Short: "查看单个令牌（key 为掩码）",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := cmdutil.ParseID(args[0], "<id>")
			if err != nil {
				return err
			}
			return f.RunSingle(cmdutil.Context(cmd), client.Request{
				Method: "GET",
				Path:   fmt.Sprintf("/api/token/%d", id),
			}, cmdutil.WithColumns(defaultColumns...))
		},
	}
	cmdutil.SetRisk(cmd, cmdutil.RiskRead)
	return cmd
}

func newKeyCmd(f *cmdutil.Factory) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "key <id>...",
		Short: "取回令牌明文 key（敏感操作）",
		Long: `取回一个或多个令牌的明文 key。

明文 key 等同于调用凭证，会原样打印到 stdout。在共享终端或会被留存的日志中
执行时请注意泄露风险。`,
		Args:    cobra.MinimumNArgs(1),
		Example: "  new-api-cli token key 12\n  new-api-cli token key 12,13,14 --yes",
		RunE: func(cmd *cobra.Command, args []string) error {
			ids, err := cmdutil.ParseIDs(args, "<id>")
			if err != nil {
				return err
			}
			if err := f.Confirm(fmt.Sprintf("明文打印 %d 个令牌的 key", len(ids))); err != nil {
				return err
			}
			// 单个 id 有专用接口，直接返回裸 key。
			if len(ids) == 1 {
				return f.RunSingle(cmdutil.Context(cmd), client.Request{
					Method: "POST",
					Path:   fmt.Sprintf("/api/token/%d/key", ids[0]),
				})
			}
			return f.RunSingle(cmdutil.Context(cmd), client.Request{
				Method: "POST",
				Path:   "/api/token/batch/keys",
				Body:   map[string]any{"ids": ids},
			})
		},
	}
	cmdutil.SetRisk(cmd, cmdutil.RiskHighRisk)
	return cmd
}

// tokenSpec 收拢令牌的可变字段。不用指针：改动与否由 Flags().Changed 判定，
// 这样 update 能把"用户显式传了什么"与"零值"区分开。
type tokenSpec struct {
	name            string
	quota           int
	unlimited       bool
	expiredAt       string
	group           string
	modelLimits     string
	allowIPs        string
	crossGroupRetry bool
	status          int
}

func (s *tokenSpec) register(cmd *cobra.Command) {
	fl := cmd.Flags()
	fl.StringVar(&s.name, "name", "", "令牌名称（<=50 字符）")
	fl.IntVar(&s.quota, "quota", 0, "剩余额度（原始 quota 数值）")
	fl.BoolVar(&s.unlimited, "unlimited", false, "无限额度")
	fl.StringVar(&s.expiredAt, "expired-at", "", "过期时间：never | Unix 秒 | 2026-01-31T10:00:00Z")
	fl.StringVar(&s.group, "group", "", "分组")
	fl.StringVar(&s.modelLimits, "model-limits", "", "允许的模型，逗号分隔（留空为不限制）")
	fl.StringVar(&s.allowIPs, "allow-ips", "", "允许的 IP，逗号分隔")
	fl.BoolVar(&s.crossGroupRetry, "cross-group-retry", false, "跨分组重试（仅 auto 分组有效）")
	fl.IntVar(&s.status, "status", 0, "状态：1 启用 2 禁用")
}

// expiredTimeValue 把 --expired-at 映射到接口的 int64 字段，-1 是"永不过期"。
func expiredTimeValue(raw string) (int64, error) {
	if strings.EqualFold(strings.TrimSpace(raw), "never") || strings.TrimSpace(raw) == "-1" {
		return -1, nil
	}
	return cmdutil.ParseTimestamp(raw, "--expired-at")
}

func newCreateCmd(f *cmdutil.Factory) *cobra.Command {
	var spec tokenSpec
	cmd := &cobra.Command{
		Use:     "create",
		Aliases: []string{"add"},
		Short:   "创建令牌",
		Args:    cobra.NoArgs,
		Example: "  new-api-cli token create --name prod --unlimited\n  new-api-cli token create --name trial --quota 500000 --expired-at 2026-12-31",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if strings.TrimSpace(spec.name) == "" {
				return errs.NewValidationError(errs.SubtypeMissingArgument, "--name 为必填").
					WithParams("--name")
			}
			body := map[string]any{
				"name":                 spec.name,
				"remain_quota":         spec.quota,
				"unlimited_quota":      spec.unlimited,
				"group":                spec.group,
				"cross_group_retry":    spec.crossGroupRetry,
				"expired_time":         int64(-1),
				"model_limits_enabled": spec.modelLimits != "",
				"model_limits":         spec.modelLimits,
			}
			if spec.allowIPs != "" {
				body["allow_ips"] = spec.allowIPs
			}
			if cmd.Flags().Changed("expired-at") {
				v, err := expiredTimeValue(spec.expiredAt)
				if err != nil {
					return err
				}
				body["expired_time"] = v
			}
			// 服务端创建接口不回传对象，用 fallback 回显请求里的名称，
			// 让调用方（尤其是 Agent）拿到可继续操作的锚点。
			return f.RunSingle(cmdutil.Context(cmd), client.Request{
				Method: "POST",
				Path:   "/api/token/",
				Body:   body,
			},
				cmdutil.WithFallback(map[string]any{"name": spec.name}),
				cmdutil.WithMessage("令牌已创建；用 `new-api-cli token search --keyword "+spec.name+"` 取 id，再用 `new-api-cli token key <id>` 取明文"),
			)
		},
	}
	spec.register(cmd)
	cmdutil.SetRisk(cmd, cmdutil.RiskWrite)
	return cmd
}

func newUpdateCmd(f *cmdutil.Factory) *cobra.Command {
	var spec tokenSpec
	cmd := &cobra.Command{
		Use:   "update <id>",
		Short: "更新令牌（只改传入的字段）",
		Long: `更新令牌。

服务端的更新接口是整体替换语义，因此本命令先读取当前令牌，把你传入的 flag
合并上去后再提交 —— 未指定的字段保持原值。只改 --status 时走服务端的
status_only 快路径，不做读改写。`,
		Args:    cobra.ExactArgs(1),
		Example: "  new-api-cli token update 12 --name prod-v2\n  new-api-cli token update 12 --status 2",
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := cmdutil.ParseID(args[0], "<id>")
			if err != nil {
				return err
			}
			fl := cmd.Flags()
			if !cmdutil.AnyFlagChanged(cmd, specFlags...) {
				return errs.NewValidationError(errs.SubtypeInvalidArgument, "没有要更新的字段").
					WithHint("至少指定一个字段，如 --name / --status / --quota")
			}

			ctx := cmdutil.Context(cmd)
			body := map[string]any{"id": id}
			query := cmdutil.NewQuery()

			// 只改状态时走服务端快路径，避免整体替换带来的字段丢失风险。
			if fl.Changed("status") && !cmdutil.AnyFlagChanged(cmd, "name", "quota", "unlimited", "expired-at", "group", "model-limits", "allow-ips", "cross-group-retry") {
				body["status"] = spec.status
				query.Str("status_only", "1")
			} else {
				cur, err := f.FetchObject(ctx, fmt.Sprintf("/api/token/%d", id), fmt.Sprintf("令牌 %d", id))
				if err != nil {
					return err
				}
				body = cur
				body["id"] = id
				// 掩码后的 key 绝不能写回，否则会把真 key 替换成 "sk-1234****"。
				delete(body, "key")
				if fl.Changed("name") {
					body["name"] = spec.name
				}
				if fl.Changed("quota") {
					body["remain_quota"] = spec.quota
				}
				if fl.Changed("unlimited") {
					body["unlimited_quota"] = spec.unlimited
				}
				if fl.Changed("group") {
					body["group"] = spec.group
				}
				if fl.Changed("cross-group-retry") {
					body["cross_group_retry"] = spec.crossGroupRetry
				}
				if fl.Changed("allow-ips") {
					body["allow_ips"] = spec.allowIPs
				}
				if fl.Changed("model-limits") {
					body["model_limits"] = spec.modelLimits
					body["model_limits_enabled"] = spec.modelLimits != ""
				}
				if fl.Changed("status") {
					body["status"] = spec.status
				}
				if fl.Changed("expired-at") {
					v, err := expiredTimeValue(spec.expiredAt)
					if err != nil {
						return err
					}
					body["expired_time"] = v
				}
			}

			return f.RunSingle(ctx, client.Request{
				Method: "PUT",
				Path:   "/api/token/",
				Query:  query.Values(),
				Body:   body,
			}, cmdutil.WithColumns(defaultColumns...))
		},
	}
	spec.register(cmd)
	cmdutil.SetRisk(cmd, cmdutil.RiskWrite)
	return cmd
}

func newDeleteCmd(f *cmdutil.Factory) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "delete <id>...",
		Aliases: []string{"rm"},
		Short:   "删除令牌（不可恢复）",
		Args:    cobra.MinimumNArgs(1),
		Example: "  new-api-cli token delete 12\n  new-api-cli token delete 12,13 --yes",
		RunE: func(cmd *cobra.Command, args []string) error {
			ids, err := cmdutil.ParseIDs(args, "<id>")
			if err != nil {
				return err
			}
			if err := f.Confirm(fmt.Sprintf("删除 %d 个令牌 %v，使用这些 key 的调用会立即失败", len(ids), ids)); err != nil {
				return err
			}
			req := client.Request{
				Method: "DELETE",
				Path:   fmt.Sprintf("/api/token/%d", ids[0]),
			}
			if len(ids) > 1 {
				req = client.Request{
					Method: "POST",
					Path:   "/api/token/batch",
					Body:   map[string]any{"ids": ids},
				}
			}
			return f.RunSingle(cmdutil.Context(cmd), req,
				cmdutil.WithMessage(fmt.Sprintf("已删除 %d 个令牌", len(ids))))
		},
	}
	cmdutil.SetRisk(cmd, cmdutil.RiskHighRisk)
	return cmd
}
