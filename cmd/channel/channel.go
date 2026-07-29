// Package channel 实现 channel 命令域：上游渠道的增删改查与运维操作。
//
// 渠道是 New API 最核心也最危险的资源 —— 它持有上游 API key，禁用一个渠道会
// 立刻影响线上流量。因此这里的写操作大多标为 high-risk-write，需要 --yes。
package channel

import (
	"fmt"
	"strings"

	"github.com/huangxin8899/new-api-cli/errs"
	"github.com/huangxin8899/new-api-cli/internal/client"
	"github.com/huangxin8899/new-api-cli/internal/cmdutil"
	"github.com/huangxin8899/new-api-cli/internal/output"

	"github.com/spf13/cobra"
)

// defaultColumns 是渠道列表的表格投影。列表接口不返回 key，所以这里不会泄露凭证。
var defaultColumns = []string{"id", "name", "type", "status", "group", "priority", "weight", "balance", "response_time", "used_quota", "tag"}

// NewCmd 构造 channel 命令树。
func NewCmd(f *cmdutil.Factory) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "channel <subcommand>",
		Aliases: []string{"ch"},
		Short:   "上游渠道管理（需要管理员）",
		Long: `管理上游渠道：列表、创建、改配置、启停、连通性测试、余额刷新。

渠道直接决定线上请求走向。` + "`disable`" + `、` + "`delete`" + `、` + "`key`" + ` 一类操作
标为 high-risk-write，非交互环境必须显式加 --yes。`,
	}
	cmd.AddCommand(
		newListCmd(f),
		newSearchCmd(f),
		newGetCmd(f),
		newKeyCmd(f),
		newCreateCmd(f),
		newUpdateCmd(f),
		newDeleteCmd(f),
		newEnableCmd(f),
		newDisableCmd(f),
		newTestCmd(f),
		newBalanceCmd(f),
		newModelsCmd(f),
		newTagCmd(f),
		newFixCmd(f),
		newHealthCmd(f),
	)
	return cmd
}

func newListCmd(f *cmdutil.Factory) *cobra.Command {
	var lf cmdutil.ListFlags
	var group, status, sortBy, sortOrder string
	var chanType int
	var tagMode bool
	cmd := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "列出渠道",
		Args:    cobra.NoArgs,
		Example: "  new-api-cli channel list --format table\n  new-api-cli channel list --status disabled\n  new-api-cli channel list --all --group vip",
		RunE: func(cmd *cobra.Command, _ []string) error {
			statusValue, err := parseStatusFilter(status)
			if err != nil {
				return err
			}
			q := cmdutil.NewQuery().
				Str("group", group).
				Str("status", statusValue).
				Str("sort_by", sortBy).
				Str("sort_order", sortOrder).
				Int("type", chanType)
			if tagMode {
				q.Bool("tag_mode", true)
			}
			return f.RunList(cmdutil.Context(cmd), client.Request{
				Method: "GET",
				Path:   "/api/channel/",
				Query:  q.Values(),
			}, &lf, defaultColumns)
		},
	}
	lf.Register(cmd)
	fl := cmd.Flags()
	fl.StringVar(&group, "group", "", "按分组过滤")
	fl.StringVar(&status, "status", "", "按状态过滤：all | enabled | disabled")
	fl.IntVar(&chanType, "type", 0, "按渠道类型过滤（数字，见站点文档）")
	fl.StringVar(&sortBy, "sort-by", "", "排序字段：id|name|priority|balance|response_time|test_time")
	fl.StringVar(&sortOrder, "sort-order", "", "排序方向：asc|desc")
	fl.BoolVar(&tagMode, "tag-mode", false, "按标签聚合返回")
	cmdutil.SetRisk(cmd, cmdutil.RiskRead)
	return cmd
}

// parseStatusFilter 把人类可读的状态名映射到服务端约定的数字。
// 服务端语义：-1 全部，1 启用，0 禁用（含手动与自动禁用）。
func parseStatusFilter(raw string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "":
		return "", nil
	case "all":
		return "-1", nil
	case "enabled", "on":
		return "1", nil
	case "disabled", "off":
		return "0", nil
	default:
		return "", errs.NewValidationError(errs.SubtypeInvalidArgument,
			"--status 只接受 all | enabled | disabled，收到 %q", raw).
			WithParams("--status")
	}
}

func newSearchCmd(f *cmdutil.Factory) *cobra.Command {
	var lf cmdutil.ListFlags
	var keyword, group, modelName, status string
	cmd := &cobra.Command{
		Use:     "search",
		Short:   "搜索渠道（按名称、分组或支持的模型）",
		Args:    cobra.NoArgs,
		Example: "  new-api-cli channel search --keyword openai\n  new-api-cli channel search --model gpt-4o",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if keyword == "" && group == "" && modelName == "" {
				return errs.NewValidationError(errs.SubtypeMissingArgument,
					"至少需要 --keyword、--group 或 --model 之一").
					WithHint("例如 new-api-cli channel search --model gpt-4o")
			}
			statusValue, err := parseStatusFilter(status)
			if err != nil {
				return err
			}
			q := cmdutil.NewQuery().
				Str("keyword", keyword).
				Str("group", group).
				Str("model", modelName).
				Str("status", statusValue)
			return f.RunList(cmdutil.Context(cmd), client.Request{
				Method: "GET",
				Path:   "/api/channel/search",
				Query:  q.Values(),
			}, &lf, defaultColumns)
		},
	}
	lf.Register(cmd)
	fl := cmd.Flags()
	fl.StringVar(&keyword, "keyword", "", "名称关键字")
	fl.StringVar(&group, "group", "", "分组")
	fl.StringVar(&modelName, "model", "", "支持该模型的渠道")
	fl.StringVar(&status, "status", "", "状态：all | enabled | disabled")
	cmdutil.SetRisk(cmd, cmdutil.RiskRead)
	return cmd
}

func newGetCmd(f *cmdutil.Factory) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "get <id>",
		Short: "查看单个渠道（不含 key）",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := cmdutil.ParseID(args[0], "<id>")
			if err != nil {
				return err
			}
			return f.RunSingle(cmdutil.Context(cmd), client.Request{
				Method: "GET",
				Path:   fmt.Sprintf("/api/channel/%d", id),
			})
		},
	}
	cmdutil.SetRisk(cmd, cmdutil.RiskRead)
	return cmd
}

func newKeyCmd(f *cmdutil.Factory) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "key <id>",
		Short: "取回渠道的上游 API key（超级管理员）",
		Long: `取回渠道配置的上游 API key 明文。

这是整个 CLI 最敏感的读操作：拿到的是上游服务商的凭证。服务端要求
role=100（超级管理员），且可能要求二次验证。key 会原样打印到 stdout。`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := cmdutil.ParseID(args[0], "<id>")
			if err != nil {
				return err
			}
			if err := f.Confirm(fmt.Sprintf("打印渠道 #%d 的上游 API key 明文", id)); err != nil {
				return err
			}
			return f.RunSingle(cmdutil.Context(cmd), client.Request{
				Method: "POST",
				Path:   fmt.Sprintf("/api/channel/%d/key", id),
			})
		},
	}
	cmdutil.SetRisk(cmd, cmdutil.RiskHighRisk)
	return cmd
}

// channelSpec 收拢渠道的可写字段。是否传入由 Flags().Changed 判定，
// 因此 update 能只改用户指定的字段。
type channelSpec struct {
	name          string
	chanType      int
	key           string
	baseURL       string
	models        string
	group         string
	priority      int64
	weight        int
	tag           string
	testModel     string
	modelMapping  string
	remark        string
	autoBan       bool
	status        int
	setting       string
	paramOverride string
}

func (s *channelSpec) register(cmd *cobra.Command) {
	fl := cmd.Flags()
	fl.StringVar(&s.name, "name", "", "渠道名称")
	fl.IntVar(&s.chanType, "type", 0, "渠道类型（1=OpenAI，14=Anthropic 等，见站点文档）")
	fl.StringVar(&s.key, "key", "", "上游 API key；支持 @file 与 - 从标准输入读取")
	fl.StringVar(&s.baseURL, "base-url", "", "上游地址（留空用该类型默认）")
	fl.StringVar(&s.models, "models", "", "支持的模型，逗号分隔")
	fl.StringVar(&s.group, "group", "", "可用分组，逗号分隔")
	fl.Int64Var(&s.priority, "priority", 0, "优先级，越大越优先")
	fl.IntVar(&s.weight, "weight", 0, "同优先级内的权重")
	fl.StringVar(&s.tag, "tag", "", "标签")
	fl.StringVar(&s.testModel, "test-model", "", "连通性测试使用的模型")
	fl.StringVar(&s.modelMapping, "model-mapping", "", "模型重定向 JSON；支持 @file")
	fl.StringVar(&s.remark, "remark", "", "备注（<=255 字符）")
	fl.BoolVar(&s.autoBan, "auto-ban", true, "上游报错时自动禁用")
	fl.IntVar(&s.status, "status", 0, "状态：1 启用 2 禁用")
	fl.StringVar(&s.setting, "setting", "", "渠道额外设置 JSON；支持 @file")
	fl.StringVar(&s.paramOverride, "param-override", "", "请求参数覆盖 JSON；支持 @file")
}

// resolveIndirect 把 --key/--model-mapping 这类支持 @file / - 的字段解析成字面值。
func (s *channelSpec) resolveIndirect(f *cmdutil.Factory) error {
	for _, item := range []struct {
		target *string
		flag   string
	}{
		{&s.key, "--key"},
		{&s.modelMapping, "--model-mapping"},
		{&s.setting, "--setting"},
		{&s.paramOverride, "--param-override"},
	} {
		if *item.target == "" {
			continue
		}
		text, err := f.ReadTextInput(*item.target, item.flag)
		if err != nil {
			return err
		}
		*item.target = strings.TrimSpace(text)
	}
	return nil
}

func newCreateCmd(f *cmdutil.Factory) *cobra.Command {
	var spec channelSpec
	var extra string
	cmd := &cobra.Command{
		Use:     "create",
		Aliases: []string{"add"},
		Short:   "新建渠道",
		Long: `新建上游渠道。

--name、--type、--key、--models 为必填。其余字段留空时用服务端默认值。
需要设置本命令未覆盖的字段时，用 --data 传一段 JSON，它会与 flag 合并
（flag 优先）。`,
		Args:    cobra.NoArgs,
		Example: "  new-api-cli channel create --name openai-main --type 1 --key sk-xxx --models gpt-4o,gpt-4o-mini\n  new-api-cli channel create --name claude --type 14 --key @key.txt --models claude-sonnet-4 --group default,vip",
		RunE: func(cmd *cobra.Command, _ []string) error {
			var missing []string
			if spec.name == "" {
				missing = append(missing, "--name")
			}
			if spec.chanType == 0 && !cmd.Flags().Changed("type") {
				missing = append(missing, "--type")
			}
			if spec.key == "" {
				missing = append(missing, "--key")
			}
			if spec.models == "" {
				missing = append(missing, "--models")
			}
			if len(missing) > 0 {
				return errs.NewValidationError(errs.SubtypeMissingArgument,
					"缺少必填参数：%s", strings.Join(missing, "、")).
					WithHint("最小可用示例：new-api-cli channel create --name openai --type 1 --key sk-xxx --models gpt-4o").
					WithParams(missing...)
			}
			if err := spec.resolveIndirect(f); err != nil {
				return err
			}

			body := map[string]any{}
			if extra != "" {
				parsed, err := f.ReadJSONInput(extra, "--data")
				if err != nil {
					return err
				}
				for k, v := range parsed {
					body[k] = v
				}
			}
			applySpec(cmd, &spec, body, true)

			return f.RunSingle(cmdutil.Context(cmd), client.Request{
				Method: "POST",
				Path:   "/api/channel/",
				Body:   body,
			}, cmdutil.WithFallback(map[string]any{"name": spec.name, "type": spec.chanType}),
				cmdutil.WithMessage("渠道已创建；用 `new-api-cli channel search --keyword "+spec.name+"` 查看 id"))
		},
	}
	spec.register(cmd)
	cmd.Flags().StringVar(&extra, "data", "", "补充字段 JSON（与 flag 合并，flag 优先）")
	cmdutil.SetRisk(cmd, cmdutil.RiskWrite)
	return cmd
}

// applySpec 把发生变化的 flag 写进请求体。isCreate 时把必填字段无条件写入，
// 因为创建接口不接受缺字段。
func applySpec(cmd *cobra.Command, s *channelSpec, body map[string]any, isCreate bool) {
	fl := cmd.Flags()
	set := func(flag, field string, value any) {
		if isCreate || fl.Changed(flag) {
			body[field] = value
		}
	}
	if isCreate {
		body["name"] = s.name
		body["type"] = s.chanType
		body["key"] = s.key
		body["models"] = s.models
		if s.group == "" {
			body["group"] = "default"
		} else {
			body["group"] = s.group
		}
	} else {
		if fl.Changed("name") {
			body["name"] = s.name
		}
		if fl.Changed("type") {
			body["type"] = s.chanType
		}
		if fl.Changed("key") {
			body["key"] = s.key
		}
		if fl.Changed("models") {
			body["models"] = s.models
		}
		if fl.Changed("group") {
			body["group"] = s.group
		}
	}
	set("base-url", "base_url", s.baseURL)
	set("priority", "priority", s.priority)
	set("weight", "weight", s.weight)
	set("tag", "tag", s.tag)
	set("test-model", "test_model", s.testModel)
	set("model-mapping", "model_mapping", s.modelMapping)
	set("remark", "remark", s.remark)
	set("status", "status", s.status)
	set("setting", "setting", s.setting)
	set("param-override", "param_override", s.paramOverride)
	if fl.Changed("auto-ban") || isCreate {
		autoBan := 0
		if s.autoBan {
			autoBan = 1
		}
		body["auto_ban"] = autoBan
	}
}

func newUpdateCmd(f *cmdutil.Factory) *cobra.Command {
	var spec channelSpec
	var extra string
	cmd := &cobra.Command{
		Use:   "update <id>",
		Short: "更新渠道（只改传入的字段）",
		Long: `更新渠道配置。只有显式传入的 flag 会被提交，其余字段服务端保持原值。

注意 --key 会替换上游凭证，--models 是整体覆盖而非追加。`,
		Args:    cobra.ExactArgs(1),
		Example: "  new-api-cli channel update 7 --priority 10\n  new-api-cli channel update 7 --models gpt-4o,gpt-4o-mini,o3",
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := cmdutil.ParseID(args[0], "<id>")
			if err != nil {
				return err
			}
			if err := spec.resolveIndirect(f); err != nil {
				return err
			}
			body := map[string]any{}
			if extra != "" {
				parsed, err := f.ReadJSONInput(extra, "--data")
				if err != nil {
					return err
				}
				for k, v := range parsed {
					body[k] = v
				}
			}
			applySpec(cmd, &spec, body, false)
			if len(body) == 0 {
				return errs.NewValidationError(errs.SubtypeInvalidArgument, "没有要更新的字段").
					WithHint("至少指定一个字段，如 --priority / --models / --status")
			}
			body["id"] = id

			if cmd.Flags().Changed("key") {
				if err := f.Confirm(fmt.Sprintf("替换渠道 #%d 的上游 API key", id)); err != nil {
					return err
				}
			}
			return f.RunSingle(cmdutil.Context(cmd), client.Request{
				Method: "PUT",
				Path:   "/api/channel/",
				Body:   body,
			}, cmdutil.WithFallback(map[string]any{"id": id, "updated": true}))
		},
	}
	spec.register(cmd)
	cmd.Flags().StringVar(&extra, "data", "", "补充字段 JSON（与 flag 合并，flag 优先）")
	cmdutil.SetRisk(cmd, cmdutil.RiskWrite)
	return cmd
}

func newDeleteCmd(f *cmdutil.Factory) *cobra.Command {
	var disabled bool
	cmd := &cobra.Command{
		Use:     "delete [id...]",
		Aliases: []string{"rm"},
		Short:   "删除渠道（不可恢复）",
		Long: `删除一个或多个渠道，或用 --disabled 清理全部已禁用渠道。

删除后走该渠道的模型若无其他渠道承接，相关请求会立即开始失败。`,
		Example: "  new-api-cli channel delete 7 --yes\n  new-api-cli channel delete --disabled --yes",
		RunE: func(cmd *cobra.Command, args []string) error {
			if disabled {
				if len(args) > 0 {
					return errs.NewValidationError(errs.SubtypeInvalidArgument,
						"--disabled 不能与显式 id 同时使用").
						WithParams("--disabled")
				}
				if err := f.Confirm("删除所有已禁用的渠道"); err != nil {
					return err
				}
				return f.RunSingle(cmdutil.Context(cmd), client.Request{
					Method: "DELETE",
					Path:   "/api/channel/disabled",
				})
			}
			if len(args) == 0 {
				return errs.NewValidationError(errs.SubtypeMissingArgument,
					"需要至少一个渠道 id，或使用 --disabled").
					WithHint("例如 new-api-cli channel delete 7 --yes")
			}
			ids, err := cmdutil.ParseIDs(args, "<id>")
			if err != nil {
				return err
			}
			if err := f.Confirm(fmt.Sprintf("删除 %d 个渠道（%v）", len(ids), ids)); err != nil {
				return err
			}
			if len(ids) == 1 {
				return f.RunSingle(cmdutil.Context(cmd), client.Request{
					Method: "DELETE",
					Path:   fmt.Sprintf("/api/channel/%d", ids[0]),
				})
			}
			return f.RunSingle(cmdutil.Context(cmd), client.Request{
				Method: "POST",
				Path:   "/api/channel/batch",
				Body:   map[string]any{"ids": ids},
			})
		},
	}
	cmd.Flags().BoolVar(&disabled, "disabled", false, "删除所有已禁用的渠道")
	cmdutil.SetRisk(cmd, cmdutil.RiskHighRisk)
	return cmd
}

// statusValue 是服务端可手动设置的两个状态。
const (
	statusEnabled  = 1
	statusDisabled = 2
)

func newEnableCmd(f *cmdutil.Factory) *cobra.Command {
	return newStatusCmd(f, "enable", "启用渠道", statusEnabled, cmdutil.RiskWrite)
}

func newDisableCmd(f *cmdutil.Factory) *cobra.Command {
	return newStatusCmd(f, "disable", "禁用渠道（立即停止分流）", statusDisabled, cmdutil.RiskHighRisk)
}

func newStatusCmd(f *cmdutil.Factory, name, short string, status int, risk string) *cobra.Command {
	cmd := &cobra.Command{
		Use:     name + " <id>...",
		Short:   short,
		Args:    cobra.MinimumNArgs(1),
		Example: fmt.Sprintf("  new-api-cli channel %s 7\n  new-api-cli channel %s 7 8 9 --yes", name, name),
		RunE: func(cmd *cobra.Command, args []string) error {
			ids, err := cmdutil.ParseIDs(args, "<id>")
			if err != nil {
				return err
			}
			if status == statusDisabled {
				if err := f.Confirm(fmt.Sprintf("禁用 %d 个渠道（%v），流量会立即转移", len(ids), ids)); err != nil {
					return err
				}
			}
			if len(ids) == 1 {
				return f.RunSingle(cmdutil.Context(cmd), client.Request{
					Method: "POST",
					Path:   fmt.Sprintf("/api/channel/%d/status", ids[0]),
					Body:   map[string]any{"status": status},
				})
			}
			return f.RunSingle(cmdutil.Context(cmd), client.Request{
				Method: "POST",
				Path:   "/api/channel/status/batch",
				Body:   map[string]any{"ids": ids, "status": status},
			})
		},
	}
	cmdutil.SetRisk(cmd, risk)
	return cmd
}

func newTestCmd(f *cmdutil.Factory) *cobra.Command {
	var all bool
	var modelName string
	cmd := &cobra.Command{
		Use:   "test [id]",
		Short: "测试渠道连通性",
		Long: `向上游发一次真实请求验证渠道可用性。

会消耗上游少量额度。--all 会遍历所有渠道，在渠道多时耗时较长，
且服务端可能因测试失败自动禁用渠道（取决于渠道的 auto_ban 设置）。`,
		Example: "  new-api-cli channel test 7\n  new-api-cli channel test 7 --model gpt-4o\n  new-api-cli channel test --all --yes",
		RunE: func(cmd *cobra.Command, args []string) error {
			q := cmdutil.NewQuery().Str("model", modelName)
			if all {
				if len(args) > 0 {
					return errs.NewValidationError(errs.SubtypeInvalidArgument,
						"--all 不能与具体 id 同时使用").WithParams("--all")
				}
				if err := f.Confirm("测试全部渠道（会向所有上游发真实请求，失败的渠道可能被自动禁用）"); err != nil {
					return err
				}
				return f.RunSingle(cmdutil.Context(cmd), client.Request{
					Method: "GET",
					Path:   "/api/channel/test",
					Query:  q.Values(),
				})
			}
			if len(args) != 1 {
				return errs.NewValidationError(errs.SubtypeMissingArgument,
					"需要一个渠道 id，或使用 --all").
					WithHint("例如 new-api-cli channel test 7")
			}
			id, err := cmdutil.ParseID(args[0], "<id>")
			if err != nil {
				return err
			}
			return f.RunSingle(cmdutil.Context(cmd), client.Request{
				Method: "GET",
				Path:   fmt.Sprintf("/api/channel/test/%d", id),
				Query:  q.Values(),
			})
		},
	}
	cmd.Flags().BoolVar(&all, "all", false, "测试所有渠道")
	cmd.Flags().StringVar(&modelName, "model", "", "指定测试用的模型")
	cmdutil.SetRisk(cmd, cmdutil.RiskWrite)
	return cmd
}

func newBalanceCmd(f *cmdutil.Factory) *cobra.Command {
	var all bool
	cmd := &cobra.Command{
		Use:   "balance [id]",
		Short: "刷新渠道余额",
		Args:  cobra.MaximumNArgs(1),
		Example: "  new-api-cli channel balance 7\n" +
			"  new-api-cli channel balance --all",
		RunE: func(cmd *cobra.Command, args []string) error {
			if all || len(args) == 0 {
				return f.RunSingle(cmdutil.Context(cmd), client.Request{
					Method: "GET",
					Path:   "/api/channel/update_balance",
				})
			}
			id, err := cmdutil.ParseID(args[0], "<id>")
			if err != nil {
				return err
			}
			return f.RunSingle(cmdutil.Context(cmd), client.Request{
				Method: "GET",
				Path:   fmt.Sprintf("/api/channel/update_balance/%d", id),
			})
		},
	}
	cmd.Flags().BoolVar(&all, "all", false, "刷新所有渠道余额")
	cmdutil.SetRisk(cmd, cmdutil.RiskWrite)
	return cmd
}

func newModelsCmd(f *cmdutil.Factory) *cobra.Command {
	var enabledOnly bool
	var fetchID int
	cmd := &cobra.Command{
		Use:   "models",
		Short: "列出渠道可用模型",
		Long: `列出模型清单。

默认返回本站支持的全部模型；--enabled 只返回当前有启用渠道承载的模型；
--fetch <id> 直接向该渠道的上游拉取它真实支持的模型列表。`,
		Args:    cobra.NoArgs,
		Example: "  new-api-cli channel models\n  new-api-cli channel models --enabled\n  new-api-cli channel models --fetch 7",
		RunE: func(cmd *cobra.Command, _ []string) error {
			path := "/api/channel/models"
			switch {
			case cmd.Flags().Changed("fetch"):
				path = fmt.Sprintf("/api/channel/fetch_models/%d", fetchID)
			case enabledOnly:
				path = "/api/channel/models_enabled"
			}
			return f.RunSingle(cmdutil.Context(cmd), client.Request{Method: "GET", Path: path})
		},
	}
	cmd.Flags().BoolVar(&enabledOnly, "enabled", false, "只列出有启用渠道的模型")
	cmd.Flags().IntVar(&fetchID, "fetch", 0, "向指定渠道的上游拉取模型列表")
	cmdutil.SetRisk(cmd, cmdutil.RiskRead)
	return cmd
}

func newTagCmd(f *cmdutil.Factory) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "tag <subcommand>",
		Short: "按标签批量操作渠道",
	}
	cmd.AddCommand(
		newTagStatusCmd(f, "enable", "启用某标签下的全部渠道", "/api/channel/tag/enabled", cmdutil.RiskWrite),
		newTagStatusCmd(f, "disable", "禁用某标签下的全部渠道", "/api/channel/tag/disabled", cmdutil.RiskHighRisk),
		newTagSetCmd(f),
		newTagModelsCmd(f),
	)
	return cmd
}

func newTagStatusCmd(f *cmdutil.Factory, name, short, path, risk string) *cobra.Command {
	cmd := &cobra.Command{
		Use:   name + " <tag>",
		Short: short,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			tag := strings.TrimSpace(args[0])
			if tag == "" {
				return errs.NewValidationError(errs.SubtypeInvalidArgument, "标签不能为空").
					WithParams("<tag>")
			}
			if risk == cmdutil.RiskHighRisk {
				if err := f.Confirm(fmt.Sprintf("禁用标签 %q 下的全部渠道", tag)); err != nil {
					return err
				}
			}
			return f.RunSingle(cmdutil.Context(cmd), client.Request{
				Method: "POST",
				Path:   path,
				Body:   map[string]any{"tag": tag},
			})
		},
	}
	cmdutil.SetRisk(cmd, risk)
	return cmd
}

func newTagSetCmd(f *cmdutil.Factory) *cobra.Command {
	var tag string
	cmd := &cobra.Command{
		Use:     "set <id>...",
		Short:   "给一批渠道打标签",
		Args:    cobra.MinimumNArgs(1),
		Example: "  new-api-cli channel tag set 7 8 9 --tag openai-pool",
		RunE: func(cmd *cobra.Command, args []string) error {
			ids, err := cmdutil.ParseIDs(args, "<id>")
			if err != nil {
				return err
			}
			if !cmd.Flags().Changed("tag") {
				return errs.NewValidationError(errs.SubtypeMissingArgument,
					"--tag 为必填（传空字符串表示清除标签）").WithParams("--tag")
			}
			return f.RunSingle(cmdutil.Context(cmd), client.Request{
				Method: "POST",
				Path:   "/api/channel/batch/tag",
				Body:   map[string]any{"ids": ids, "tag": tag},
			})
		},
	}
	cmd.Flags().StringVar(&tag, "tag", "", "要设置的标签，空字符串表示清除")
	cmdutil.SetRisk(cmd, cmdutil.RiskWrite)
	return cmd
}

func newTagModelsCmd(f *cmdutil.Factory) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "models <tag>",
		Short: "查看某标签下渠道支持的模型",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return f.RunSingle(cmdutil.Context(cmd), client.Request{
				Method: "GET",
				Path:   "/api/channel/tag/models",
				Query:  cmdutil.NewQuery().Str("tag", args[0]).Values(),
			})
		},
	}
	cmdutil.SetRisk(cmd, cmdutil.RiskRead)
	return cmd
}

func newFixCmd(f *cmdutil.Factory) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "fix",
		Short: "修复渠道与模型的关联表（abilities）",
		Long: `重建渠道-模型关联表。

当出现「渠道配置了某模型但请求报无可用渠道」时执行。会全表重算，
渠道数量多时耗时较长。`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := f.Confirm("重建全部渠道的模型关联表"); err != nil {
				return err
			}
			return f.RunSingle(cmdutil.Context(cmd), client.Request{
				Method: "POST",
				Path:   "/api/channel/fix",
			})
		},
	}
	cmdutil.SetRisk(cmd, cmdutil.RiskWrite)
	return cmd
}

// newHealthCmd 是一个快捷命令：把「哪些渠道不健康」这个高频问题
// 收敂成一次调用 + 一次本地聚合，省掉人工翻列表。
func newHealthCmd(f *cmdutil.Factory) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "+health",
		Short: "渠道健康概览（禁用、慢响应、低余额）",
		Long: `一次拉取全部渠道，在本地聚合出需要关注的部分：

  - disabled  非启用状态的渠道（含被自动禁用的）
  - slow      响应时间超过 --slow-ms 的渠道
  - low       余额低于 --min-balance 且余额可用的渠道

只读操作，不会向上游发请求。`,
		Args:    cobra.NoArgs,
		Example: "  new-api-cli channel +health\n  new-api-cli channel +health --slow-ms 3000 --format table",
		RunE: func(cmd *cobra.Command, _ []string) error {
			slowMS := mustInt(cmd, "slow-ms")
			minBalance := mustFloat(cmd, "min-balance")

			c, err := f.Client()
			if err != nil {
				return err
			}
			res, err := c.List(cmdutil.Context(cmd), client.Request{
				Method: "GET",
				Path:   "/api/channel/",
				Query:  cmdutil.NewQuery().Str("status", "-1").Values(),
			}, client.ListOptions{All: true, PageSize: 100})
			if err != nil {
				return err
			}

			report := buildHealthReport(res.Items, slowMS, minBalance)
			return f.EmitResult(output.Result{
				Data:    report,
				Meta:    &output.Meta{Total: res.Total, Count: len(res.Items)},
				Message: report.summary(),
			})
		},
	}
	cmd.Flags().Int("slow-ms", 5000, "响应时间超过该毫秒数视为慢")
	cmd.Flags().Float64("min-balance", 1, "余额低于该值视为不足")
	cmdutil.SetRisk(cmd, cmdutil.RiskRead)
	return cmd
}

func mustInt(cmd *cobra.Command, name string) int {
	v, _ := cmd.Flags().GetInt(name)
	return v
}

func mustFloat(cmd *cobra.Command, name string) float64 {
	v, _ := cmd.Flags().GetFloat64(name)
	return v
}
