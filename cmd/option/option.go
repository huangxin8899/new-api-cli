// Package option 实现 option 命令域：站点系统设置的读写。
package option

import (
	"encoding/json"
	"sort"
	"strconv"
	"strings"

	"github.com/huangxin8899/new-api-cli/errs"
	"github.com/huangxin8899/new-api-cli/internal/client"
	"github.com/huangxin8899/new-api-cli/internal/cmdutil"
	"github.com/huangxin8899/new-api-cli/internal/output"

	"github.com/spf13/cobra"
)

// NewCmd 构建 option 命令树。
func NewCmd(f *cmdutil.Factory) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "option <subcommand>",
		Aliases: []string{"options", "setting"},
		Short:   "站点系统设置（需超级管理员）",
		Long: `读写站点系统设置。全部子命令都要求超级管理员（role=100）。

服务端在返回时会过滤掉以 Token / Secret / Key / api_key 结尾的敏感项，
因此 ` + "`list`" + ` 看不到它们的值 —— 但仍然可以用 ` + "`set`" + ` 写入。`,
	}
	cmd.AddCommand(
		newListCmd(f),
		newGetCmd(f),
		newSetCmd(f),
	)
	return cmd
}

func newListCmd(f *cmdutil.Factory) *cobra.Command {
	var prefix string
	var columns []string
	cmd := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "列出全部设置项",
		Args:    cobra.NoArgs,
		Example: "  new-api-cli option list\n  new-api-cli option list --prefix Quota --format table",
		RunE: func(cmd *cobra.Command, _ []string) error {
			items, err := fetchOptions(cmd, f)
			if err != nil {
				return err
			}
			if prefix != "" {
				items = filterByPrefix(items, prefix)
			}
			cols := columns
			if len(cols) == 0 {
				cols = []string{"key", "value"}
			}
			return f.EmitResult(output.Result{
				Data:    items,
				Meta:    &output.Meta{Count: len(items)},
				Columns: cols,
			})
		},
	}
	cmd.Flags().StringVar(&prefix, "prefix", "", "只保留 key 以此开头的设置项（不区分大小写）")
	cmd.Flags().StringSliceVar(&columns, "columns", nil, "table/csv 输出的列")
	cmdutil.SetRisk(cmd, cmdutil.RiskRead)
	return cmd
}

func newGetCmd(f *cmdutil.Factory) *cobra.Command {
	var raw bool
	cmd := &cobra.Command{
		Use:     "get <key>",
		Short:   "读取单个设置项",
		Args:    cobra.ExactArgs(1),
		Example: "  new-api-cli option get QuotaPerUnit\n  new-api-cli option get ModelRatio --raw",
		RunE: func(cmd *cobra.Command, args []string) error {
			key := strings.TrimSpace(args[0])
			items, err := fetchOptions(cmd, f)
			if err != nil {
				return err
			}
			for _, item := range items {
				if !strings.EqualFold(item.Key, key) {
					continue
				}
				if raw {
					// --raw 直出裸值，便于 `$(... option get X --raw)` 取用。
					_, err := f.IOStreams.Out.Write([]byte(item.Value + "\n"))
					return err
				}
				return f.EmitData(item)
			}
			return errs.NewAPIError(errs.SubtypeNotFound,
				"设置项 %q 不存在或已被服务端过滤", key).
				WithHint("敏感项（以 Token/Secret/Key 结尾）不会被返回；用 new-api-cli option list 查看可读的 key")
		},
	}
	cmd.Flags().BoolVar(&raw, "raw", false, "只输出值本身，不带信封")
	cmdutil.SetRisk(cmd, cmdutil.RiskRead)
	return cmd
}

func newSetCmd(f *cmdutil.Factory) *cobra.Command {
	var valueFile string
	cmd := &cobra.Command{
		Use:   "set <key> [value]",
		Short: "写入设置项（高危）",
		Long: `写入一个设置项。

设置项直接影响线上行为：改动 ` + "`ModelRatio`" + ` 会立刻改变计费，改动
` + "`PasswordLoginEnabled`" + ` 之类的开关可能把你自己锁在外面。执行前请确认 key
拼写与值格式，必要时先用 ` + "`option get`" + ` 记录原值以便回滚。

值可以作为位置参数给出，或用 --value-file 从文件/标准输入读取（适合
ModelRatio 这类很长的 JSON）。`,
		Args:    cobra.RangeArgs(1, 2),
		Example: "  new-api-cli option set DisplayInCurrencyEnabled true --yes\n  new-api-cli option set ModelRatio --value-file @ratio.json --yes",
		RunE: func(cmd *cobra.Command, args []string) error {
			key := strings.TrimSpace(args[0])
			if key == "" {
				return errs.NewValidationError(errs.SubtypeMissingArgument, "<key> 不能为空")
			}

			var value string
			switch {
			case valueFile != "" && len(args) > 1:
				return errs.NewValidationError(errs.SubtypeInvalidArgument,
					"不能同时给出位置参数 value 与 --value-file").
					WithParams("--value-file")
			case valueFile != "":
				v, err := f.ReadTextInput(valueFile, "--value-file")
				if err != nil {
					return err
				}
				value = strings.TrimSpace(v)
			case len(args) > 1:
				value = args[1]
			default:
				return errs.NewValidationError(errs.SubtypeMissingArgument,
					"缺少要写入的值").
					WithHint("给出位置参数，或用 --value-file @file / --value-file -")
			}

			if err := f.Confirm("修改站点设置 " + key + "（影响线上行为）"); err != nil {
				return err
			}
			return f.RunSingle(cmdutil.Context(cmd), client.Request{
				Method: "PUT",
				Path:   "/api/option/",
				Body:   map[string]any{"key": key, "value": typedValue(value)},
			}, cmdutil.WithFallback(map[string]any{"key": key, "value": value}))
		},
	}
	cmd.Flags().StringVar(&valueFile, "value-file", "", "从文件（@path）或标准输入（-）读取值")
	cmdutil.SetRisk(cmd, cmdutil.RiskHighRisk)
	return cmd
}

// optionItem 是设置项的线上结构。
type optionItem struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

func fetchOptions(cmd *cobra.Command, f *cmdutil.Factory) ([]optionItem, error) {
	c, err := f.Client()
	if err != nil {
		return nil, err
	}
	resp, err := c.Do(cmdutil.Context(cmd), client.Request{
		Method: "GET",
		Path:   "/api/option/",
	})
	if err != nil {
		return nil, err
	}
	var items []optionItem
	if err := resp.Decode(&items); err != nil {
		return nil, err
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Key < items[j].Key })
	return items, nil
}

func filterByPrefix(items []optionItem, prefix string) []optionItem {
	lower := strings.ToLower(prefix)
	out := make([]optionItem, 0, len(items))
	for _, item := range items {
		if strings.HasPrefix(strings.ToLower(item.Key), lower) {
			out = append(out, item)
		}
	}
	return out
}

// typedValue 把命令行上的字符串还原成合适的 JSON 类型。
//
// 服务端接受 bool / number / string 并统一转成字符串存储，但布尔开关写成
// 字符串 "true" 与写成 true 在部分校验分支上表现不同，所以这里按字面量还原。
// JSON 对象/数组保持字符串原样 —— 服务端就是按字符串存 ModelRatio 的。
func typedValue(raw string) any {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "true":
		return true
	case "false":
		return false
	}
	if n, err := strconv.ParseFloat(strings.TrimSpace(raw), 64); err == nil {
		// 仅当往返一致时才当数字，避免 "1.0"、"007" 这类被悄悄改写。
		if strconv.FormatFloat(n, 'f', -1, 64) == strings.TrimSpace(raw) {
			return n
		}
	}
	// 合法 JSON 对象/数组按字符串提交，与服务端存储形式一致。
	var probe any
	if json.Unmarshal([]byte(raw), &probe) == nil {
		return raw
	}
	return raw
}
