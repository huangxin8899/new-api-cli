// Package model 实现 model 命令域：模型元数据与可用模型查询。
package model

import (
	"fmt"

	"github.com/QuantumNous/new-api-cli/errs"
	"github.com/QuantumNous/new-api-cli/internal/client"
	"github.com/QuantumNous/new-api-cli/internal/cmdutil"

	"github.com/spf13/cobra"
)

var defaultColumns = []string{"id", "model_name", "vendor_id", "status", "sync_official", "tags", "matched_count"}

// NewCmd 构建 model 命令树。
func NewCmd(f *cmdutil.Factory) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "model <subcommand>",
		Aliases: []string{"models"},
		Short:   "模型元数据与可用模型",
		Long: `查询模型元数据（名称、供应商、标签、绑定渠道）与当前身份可用的模型列表。

` + "`list`/`search`/`get`" + ` 等元数据操作需要管理员；` + "`available`" + ` 是普通用户视角的可用模型。`,
	}
	cmd.AddCommand(
		newListCmd(f),
		newSearchCmd(f),
		newGetCmd(f),
		newAvailableCmd(f),
		newMissingCmd(f),
		newCreateCmd(f),
		newUpdateCmd(f),
		newDeleteCmd(f),
	)
	return cmd
}

func newListCmd(f *cmdutil.Factory) *cobra.Command {
	var lf cmdutil.ListFlags
	var status, syncOfficial string
	cmd := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "列出模型元数据（需管理员）",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			q := cmdutil.NewQuery().Str("status", status).Str("sync_official", syncOfficial)
			return f.RunList(cmdutil.Context(cmd), client.Request{
				Method: "GET",
				Path:   "/api/models/",
				Query:  q.Values(),
			}, &lf, defaultColumns)
		},
	}
	lf.Register(cmd)
	cmd.Flags().StringVar(&status, "status", "", "状态过滤：1 启用 2 禁用")
	cmd.Flags().StringVar(&syncOfficial, "sync-official", "", "是否同步官方信息：1 是 0 否")
	cmdutil.SetRisk(cmd, cmdutil.RiskRead)
	return cmd
}

func newSearchCmd(f *cmdutil.Factory) *cobra.Command {
	var lf cmdutil.ListFlags
	var keyword, vendor, status, syncOfficial string
	cmd := &cobra.Command{
		Use:     "search",
		Short:   "搜索模型元数据（需管理员）",
		Args:    cobra.NoArgs,
		Example: "  new-api-cli model search --keyword gpt-4",
		RunE: func(cmd *cobra.Command, _ []string) error {
			q := cmdutil.NewQuery().
				Str("keyword", keyword).
				Str("vendor", vendor).
				Str("status", status).
				Str("sync_official", syncOfficial)
			return f.RunList(cmdutil.Context(cmd), client.Request{
				Method: "GET",
				Path:   "/api/models/search",
				Query:  q.Values(),
			}, &lf, defaultColumns)
		},
	}
	lf.Register(cmd)
	cmd.Flags().StringVar(&keyword, "keyword", "", "模型名关键字")
	cmd.Flags().StringVar(&vendor, "vendor", "", "供应商")
	cmd.Flags().StringVar(&status, "status", "", "状态过滤")
	cmd.Flags().StringVar(&syncOfficial, "sync-official", "", "是否同步官方信息")
	cmdutil.SetRisk(cmd, cmdutil.RiskRead)
	return cmd
}

func newGetCmd(f *cmdutil.Factory) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "get <id>",
		Short: "查看模型元数据（需管理员）",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := cmdutil.ParseID(args[0], "<id>")
			if err != nil {
				return err
			}
			return f.RunSingle(cmdutil.Context(cmd), client.Request{
				Method: "GET",
				Path:   fmt.Sprintf("/api/models/%d", id),
			})
		},
	}
	cmdutil.SetRisk(cmd, cmdutil.RiskRead)
	return cmd
}

func newAvailableCmd(f *cmdutil.Factory) *cobra.Command {
	var forAll bool
	cmd := &cobra.Command{
		Use:   "available",
		Short: "列出当前身份可调用的模型",
		Long: `列出当前登录身份可以调用的模型。

默认取 ` + "`/api/user/models`" + `（按当前用户分组解析）；加 --dashboard 走
` + "`/api/models`" + `，返回仪表盘视角的模型清单。`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			path := "/api/user/models"
			if forAll {
				path = "/api/models"
			}
			return f.RunSingle(cmdutil.Context(cmd), client.Request{
				Method: "GET",
				Path:   path,
			})
		},
	}
	cmd.Flags().BoolVar(&forAll, "dashboard", false, "改用 /api/models 的仪表盘视角")
	cmdutil.SetRisk(cmd, cmdutil.RiskRead)
	return cmd
}

func newMissingCmd(f *cmdutil.Factory) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "missing",
		Short: "列出渠道已支持但缺少元数据的模型（需管理员）",
		Long: `找出渠道声明支持、但尚未登记元数据的模型。

这些模型可以被调用，但在定价页与模型列表里缺少描述、图标与供应商信息。`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return f.RunSingle(cmdutil.Context(cmd), client.Request{
				Method: "GET",
				Path:   "/api/models/missing",
			})
		},
	}
	cmdutil.SetRisk(cmd, cmdutil.RiskRead)
	return cmd
}

// modelSpec 收拢模型元数据的可写字段。
type modelSpec struct {
	name         string
	description  string
	icon         string
	tags         string
	vendorID     int
	endpoints    string
	status       int
	syncOfficial int
	nameRule     int
	data         string
}

func (s *modelSpec) register(cmd *cobra.Command) {
	fl := cmd.Flags()
	fl.StringVar(&s.name, "model-name", "", "模型名，如 gpt-4o")
	fl.StringVar(&s.description, "description", "", "描述")
	fl.StringVar(&s.icon, "icon", "", "图标")
	fl.StringVar(&s.tags, "tags", "", "标签，逗号分隔")
	fl.IntVar(&s.vendorID, "vendor-id", 0, "供应商 ID（见 new-api-cli api GET /api/vendors/）")
	fl.StringVar(&s.endpoints, "endpoints", "", "支持的端点，JSON 数组字符串")
	fl.IntVar(&s.status, "status", 0, "状态：1 启用 2 禁用")
	fl.IntVar(&s.syncOfficial, "sync-official", 0, "是否同步官方信息：1 是 0 否")
	fl.IntVar(&s.nameRule, "name-rule", 0, "匹配规则：0 精确 1 前缀 2 包含 3 后缀")
	fl.StringVar(&s.data, "data", "", "完整 JSON 请求体，覆盖上述 flag（支持 @file 与 -）")
}

var specFlags = []string{"model-name", "description", "icon", "tags", "vendor-id",
	"endpoints", "status", "sync-official", "name-rule"}

// apply 把用户显式传入的 flag 覆盖到 body 上。
func (s *modelSpec) apply(cmd *cobra.Command, body map[string]any) {
	fl := cmd.Flags()
	set := func(flag, field string, v any) {
		if fl.Changed(flag) {
			body[field] = v
		}
	}
	set("model-name", "model_name", s.name)
	set("description", "description", s.description)
	set("icon", "icon", s.icon)
	set("tags", "tags", s.tags)
	set("vendor-id", "vendor_id", s.vendorID)
	set("endpoints", "endpoints", s.endpoints)
	set("status", "status", s.status)
	set("sync-official", "sync_official", s.syncOfficial)
	set("name-rule", "name_rule", s.nameRule)
}

func newCreateCmd(f *cmdutil.Factory) *cobra.Command {
	var spec modelSpec
	cmd := &cobra.Command{
		Use:     "create",
		Aliases: []string{"add"},
		Short:   "登记模型元数据（需管理员）",
		Args:    cobra.NoArgs,
		Example: "  new-api-cli model create --model-name gpt-4o --vendor-id 1 --status 1",
		RunE: func(cmd *cobra.Command, _ []string) error {
			body := map[string]any{}
			if spec.data != "" {
				parsed, err := f.ReadJSONInput(spec.data, "--data")
				if err != nil {
					return err
				}
				body = parsed
			}
			spec.apply(cmd, body)
			if _, ok := body["model_name"]; !ok {
				return errs.NewValidationError(errs.SubtypeMissingArgument,
					"--model-name 为必填").WithParams("--model-name")
			}
			return f.RunSingle(cmdutil.Context(cmd), client.Request{
				Method: "POST",
				Path:   "/api/models/",
				Body:   body,
			}, cmdutil.WithFallback(body))
		},
	}
	spec.register(cmd)
	cmdutil.SetRisk(cmd, cmdutil.RiskWrite)
	return cmd
}

func newUpdateCmd(f *cmdutil.Factory) *cobra.Command {
	var spec modelSpec
	cmd := &cobra.Command{
		Use:   "update <id>",
		Short: "更新模型元数据（需管理员）",
		Long: `更新模型元数据。

服务端为整体替换语义，本命令先读取当前记录再合并你传入的 flag。`,
		Args:    cobra.ExactArgs(1),
		Example: "  new-api-cli model update 7 --status 2",
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := cmdutil.ParseID(args[0], "<id>")
			if err != nil {
				return err
			}
			if spec.data == "" && !cmdutil.AnyFlagChanged(cmd, specFlags...) {
				return errs.NewValidationError(errs.SubtypeInvalidArgument, "没有要更新的字段").
					WithHint("至少指定一个，如 --status / --tags / --description")
			}
			ctx := cmdutil.Context(cmd)
			body, err := f.FetchObject(ctx, fmt.Sprintf("/api/models/%d", id), fmt.Sprintf("模型 %d", id))
			if err != nil {
				return err
			}
			if spec.data != "" {
				patch, err := f.ReadJSONInput(spec.data, "--data")
				if err != nil {
					return err
				}
				for k, v := range patch {
					body[k] = v
				}
			}
			spec.apply(cmd, body)
			body["id"] = id
			return f.RunSingle(ctx, client.Request{
				Method: "PUT",
				Path:   "/api/models/",
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
		Use:     "delete <id>",
		Aliases: []string{"rm"},
		Short:   "删除模型元数据（需管理员）",
		Long: `删除模型元数据。

只影响展示信息（描述、图标、供应商），不会影响渠道对该模型的实际支持
——调用依然会成功，只是定价页与模型列表里少了这条描述。`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := cmdutil.ParseID(args[0], "<id>")
			if err != nil {
				return err
			}
			if err := f.Confirm(fmt.Sprintf("删除模型元数据 #%d", id)); err != nil {
				return err
			}
			return f.RunSingle(cmdutil.Context(cmd), client.Request{
				Method: "DELETE",
				Path:   fmt.Sprintf("/api/models/%d", id),
			}, cmdutil.WithFallback(map[string]any{"id": id, "deleted": true}))
		},
	}
	cmdutil.SetRisk(cmd, cmdutil.RiskWrite)
	return cmd
}
