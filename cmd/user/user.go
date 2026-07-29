// Package user 实现 user 命令域：自身信息与（管理员）用户管理。
package user

import (
	"fmt"

	"github.com/huangxin8899/new-api-cli/errs"
	"github.com/huangxin8899/new-api-cli/internal/client"
	"github.com/huangxin8899/new-api-cli/internal/cmdutil"
	"github.com/spf13/cobra"
)

var defaultColumns = []string{"id", "username", "display_name", "role", "status", "group", "quota", "used_quota", "request_count"}

// NewCmd 构造 user 命令树。
func NewCmd(f *cmdutil.Factory) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "user <subcommand>",
		Short: "用户管理",
		Long: `查看自身信息，以及（需要管理员权限）管理站点用户。

list / search / get / create / manage / update / delete 都需要 role>=10；
self / models / groups 任何已登录用户都可用。`,
	}
	cmd.AddCommand(
		newSelfCmd(f),
		newModelsCmd(f),
		newGroupsCmd(f),
		newListCmd(f),
		newSearchCmd(f),
		newGetCmd(f),
		newCreateCmd(f),
		newManageCmd(f),
		newUpdateCmd(f),
		newDeleteCmd(f),
	)
	return cmd
}

func newSelfCmd(f *cmdutil.Factory) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "self",
		Aliases: []string{"me"},
		Short:   "查看当前登录用户",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return f.RunSingle(cmdutil.Context(cmd), client.Request{
				Method: "GET",
				Path:   "/api/user/self",
			})
		},
	}
	cmdutil.SetRisk(cmd, cmdutil.RiskRead)
	return cmd
}

func newModelsCmd(f *cmdutil.Factory) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "models",
		Short: "列出当前用户可用的模型",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return f.RunSingle(cmdutil.Context(cmd), client.Request{
				Method: "GET",
				Path:   "/api/user/models",
			})
		},
	}
	cmdutil.SetRisk(cmd, cmdutil.RiskRead)
	return cmd
}

func newGroupsCmd(f *cmdutil.Factory) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "groups",
		Short: "列出可用的用户分组及其倍率",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return f.RunSingle(cmdutil.Context(cmd), client.Request{
				Method: "GET",
				Path:   "/api/user/self/groups",
			})
		},
	}
	cmdutil.SetRisk(cmd, cmdutil.RiskRead)
	return cmd
}

func newListCmd(f *cmdutil.Factory) *cobra.Command {
	var lf cmdutil.ListFlags
	var sortBy, sortOrder string
	cmd := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "列出所有用户（需管理员）",
		Args:    cobra.NoArgs,
		Example: "  new-api-cli user list --format table\n  new-api-cli user list --sort-by quota --sort-order desc",
		RunE: func(cmd *cobra.Command, _ []string) error {
			q := cmdutil.NewQuery().Str("sort_by", sortBy).Str("sort_order", sortOrder)
			return f.RunList(cmdutil.Context(cmd), client.Request{
				Method: "GET",
				Path:   "/api/user/",
				Query:  q.Values(),
			}, &lf, defaultColumns)
		},
	}
	lf.Register(cmd)
	cmd.Flags().StringVar(&sortBy, "sort-by", "", "排序字段，如 id|quota|used_quota|request_count")
	cmd.Flags().StringVar(&sortOrder, "sort-order", "", "排序方向：asc|desc")
	cmdutil.SetRisk(cmd, cmdutil.RiskRead)
	return cmd
}

func newSearchCmd(f *cmdutil.Factory) *cobra.Command {
	var lf cmdutil.ListFlags
	var keyword, group string
	var role, status int
	cmd := &cobra.Command{
		Use:     "search",
		Short:   "搜索用户（需管理员）",
		Args:    cobra.NoArgs,
		Example: "  new-api-cli user search --keyword alice\n  new-api-cli user search --role 10",
		RunE: func(cmd *cobra.Command, _ []string) error {
			q := cmdutil.NewQuery().Str("keyword", keyword).Str("group", group).
				Int("role", role).Int("status", status)
			return f.RunList(cmdutil.Context(cmd), client.Request{
				Method: "GET",
				Path:   "/api/user/search",
				Query:  q.Values(),
			}, &lf, defaultColumns)
		},
	}
	lf.Register(cmd)
	cmd.Flags().StringVar(&keyword, "keyword", "", "用户名/显示名/邮箱关键字")
	cmd.Flags().StringVar(&group, "group", "", "按分组过滤")
	cmd.Flags().IntVar(&role, "role", 0, "按角色过滤：1 普通 10 管理员 100 超级管理员")
	cmd.Flags().IntVar(&status, "status", 0, "按状态过滤：1 启用 2 禁用")
	cmdutil.SetRisk(cmd, cmdutil.RiskRead)
	return cmd
}

func newGetCmd(f *cmdutil.Factory) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "get <id>",
		Short: "查看单个用户（需管理员）",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := cmdutil.ParseID(args[0], "<id>")
			if err != nil {
				return err
			}
			return f.RunSingle(cmdutil.Context(cmd), client.Request{
				Method: "GET",
				Path:   fmt.Sprintf("/api/user/%d", id),
			})
		},
	}
	cmdutil.SetRisk(cmd, cmdutil.RiskRead)
	return cmd
}

func newCreateCmd(f *cmdutil.Factory) *cobra.Command {
	var username, password, displayName string
	var role int
	cmd := &cobra.Command{
		Use:     "create",
		Aliases: []string{"add"},
		Short:   "创建用户（需管理员）",
		Long: `创建用户。

只能创建比自己角色低的用户：管理员（10）不能创建管理员，需超级管理员（100）操作。
密码长度需 8-20 位。`,
		Args:    cobra.NoArgs,
		Example: "  new-api-cli user create --username alice --password 'S3cret!pass'",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if username == "" || password == "" {
				return errs.NewValidationError(errs.SubtypeMissingArgument,
					"--username 与 --password 均为必填").
					WithParams("--username", "--password")
			}
			body := map[string]any{
				"username":     username,
				"password":     password,
				"display_name": displayName,
			}
			if cmd.Flags().Changed("role") {
				body["role"] = role
			}
			return f.RunSingle(cmdutil.Context(cmd), client.Request{
				Method: "POST",
				Path:   "/api/user/",
				Body:   body,
			}, cmdutil.WithFallback(map[string]any{"username": username}),
				cmdutil.WithMessage("用户已创建"))
		},
	}
	fl := cmd.Flags()
	fl.StringVar(&username, "username", "", "用户名（<=20 字符）")
	fl.StringVar(&password, "password", "", "密码（8-20 位）")
	fl.StringVar(&displayName, "display-name", "", "显示名，默认同用户名")
	fl.IntVar(&role, "role", 0, "角色：1 普通 10 管理员（须低于自己的角色）")
	cmdutil.SetRisk(cmd, cmdutil.RiskWrite)
	return cmd
}

// manageActions 是 /api/user/manage 支持的动作。
var manageActions = []string{"enable", "disable", "delete", "promote", "demote"}

func newManageCmd(f *cmdutil.Factory) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "manage <id> <action>",
		Short: "启用/禁用/提权/降权/删除用户（需管理员）",
		Long: `对用户执行管理动作。

动作：
  enable    启用
  disable   禁用（无法禁用超级管理员）
  promote   提升为管理员（仅超级管理员可执行）
  demote    降为普通用户（仅超级管理员可执行）
  delete    删除（软删除，无法删除超级管理员）`,
		Args:    cobra.ExactArgs(2),
		Example: "  new-api-cli user manage 42 disable --yes\n  new-api-cli user manage 42 promote --yes",
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := cmdutil.ParseID(args[0], "<id>")
			if err != nil {
				return err
			}
			action, err := cmdutil.ParseEnum(args[1], "<action>", manageActions)
			if err != nil {
				return err
			}
			if err := f.Confirm(fmt.Sprintf("对用户 #%d 执行 %s", id, action)); err != nil {
				return err
			}
			return f.RunSingle(cmdutil.Context(cmd), client.Request{
				Method: "POST",
				Path:   "/api/user/manage",
				Body:   map[string]any{"id": id, "action": action},
			}, cmdutil.WithFallback(map[string]any{"id": id, "action": action}))
		},
	}
	cmdutil.SetRisk(cmd, cmdutil.RiskHighRisk)
	return cmd
}

func newUpdateCmd(f *cmdutil.Factory) *cobra.Command {
	var displayName, group, password, remark, email string
	var quota, role, status int
	cmd := &cobra.Command{
		Use:   "update <id>",
		Short: "更新用户（需管理员）",
		Long: `更新用户字段。

服务端为整体替换语义，本命令先读取当前用户再合并你传入的 flag，
未指定的字段保持原值。`,
		Args:    cobra.ExactArgs(1),
		Example: "  new-api-cli user update 42 --quota 5000000\n  new-api-cli user update 42 --group vip",
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := cmdutil.ParseID(args[0], "<id>")
			if err != nil {
				return err
			}
			fl := cmd.Flags()
			names := []string{"display-name", "group", "quota", "role", "status", "password", "remark", "email"}
			if !cmdutil.AnyFlagChanged(cmd, names...) {
				return errs.NewValidationError(errs.SubtypeInvalidArgument, "没有要更新的字段").
					WithHint("至少指定一个，如 --quota / --group / --status")
			}
			ctx := cmdutil.Context(cmd)
			body, err := f.FetchObject(ctx, fmt.Sprintf("/api/user/%d", id), fmt.Sprintf("用户 %d", id))
			if err != nil {
				return err
			}
			body["id"] = id
			// 未显式改密码时必须去掉该字段，否则会把空密码写回。
			delete(body, "password")
			if fl.Changed("display-name") {
				body["display_name"] = displayName
			}
			if fl.Changed("group") {
				body["group"] = group
			}
			if fl.Changed("quota") {
				body["quota"] = quota
			}
			if fl.Changed("role") {
				body["role"] = role
			}
			if fl.Changed("status") {
				body["status"] = status
			}
			if fl.Changed("remark") {
				body["remark"] = remark
			}
			if fl.Changed("email") {
				body["email"] = email
			}
			if fl.Changed("password") {
				body["password"] = password
			}
			return f.RunSingle(ctx, client.Request{
				Method: "PUT",
				Path:   "/api/user/",
				Body:   body,
			}, cmdutil.WithFallback(map[string]any{"id": id}))
		},
	}
	fl := cmd.Flags()
	fl.StringVar(&displayName, "display-name", "", "显示名")
	fl.StringVar(&group, "group", "", "分组")
	fl.IntVar(&quota, "quota", 0, "额度（原始 quota 数值）")
	fl.IntVar(&role, "role", 0, "角色：1 普通 10 管理员")
	fl.IntVar(&status, "status", 0, "状态：1 启用 2 禁用")
	fl.StringVar(&password, "password", "", "重置密码（8-20 位）")
	fl.StringVar(&remark, "remark", "", "备注（仅管理员可见）")
	fl.StringVar(&email, "email", "", "邮箱")
	cmdutil.SetRisk(cmd, cmdutil.RiskWrite)
	return cmd
}

func newDeleteCmd(f *cmdutil.Factory) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "delete <id>",
		Aliases: []string{"rm"},
		Short:   "删除用户（需管理员）",
		Long: `删除用户。

服务端为软删除，但会立即失效该用户的全部令牌 —— 正在使用这些 key 的
调用会当场失败。`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := cmdutil.ParseID(args[0], "<id>")
			if err != nil {
				return err
			}
			if err := f.Confirm(fmt.Sprintf("删除用户 #%d，其所有令牌将立即失效", id)); err != nil {
				return err
			}
			return f.RunSingle(cmdutil.Context(cmd), client.Request{
				Method: "DELETE",
				Path:   fmt.Sprintf("/api/user/%d", id),
			}, cmdutil.WithFallback(map[string]any{"id": id, "deleted": true}))
		},
	}
	cmdutil.SetRisk(cmd, cmdutil.RiskHighRisk)
	return cmd
}
