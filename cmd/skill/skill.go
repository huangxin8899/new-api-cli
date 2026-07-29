// Package skill 实现 skills 命令域：向 AI Agent 提供随二进制发布的 skill 文档。
//
// 包名是 skill，用户可见的动词是 skills。
package skill

import (
	"fmt"

	"github.com/huangxin8899/new-api-cli/errs"
	"github.com/huangxin8899/new-api-cli/internal/cmdutil"
	"github.com/huangxin8899/new-api-cli/internal/output"
	"github.com/huangxin8899/new-api-cli/internal/skillcontent"
	"github.com/spf13/cobra"
)

func newReader(f *cmdutil.Factory) (*skillcontent.Reader, error) {
	if f.SkillContent == nil {
		return nil, errs.NewInternalError("该构建未嵌入 skill 内容").
			WithHint("用 `go build .`（而不是 `go build ./main.go`）构建，嵌入才会生效")
	}
	return skillcontent.New(f.SkillContent), nil
}

// NewCmd 构造 skills 命令组。
func NewCmd(f *cmdutil.Factory) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "skills <subcommand>",
		Short: "读取内置的 Agent 技能文档",
		Long: `读取编译期嵌入在二进制里的 skill 文档（SKILL.md 与 references/ 下的引用文件）。

文档随 CLI 一起发布，因此内容与当前 CLI 版本天然一致 —— Agent 不需要
额外下载或猜测命令用法。

推荐用法：先 ` + "`skills list`" + ` 看有哪些域，再 ` + "`skills read new-api-shared`" + `
读通用规则（认证、风险门禁、JSON 契约），然后按任务读对应域的 skill。`,
		Example: `  new-api-cli skills list
  new-api-cli skills read new-api-shared
  new-api-cli skills list new-api-channel
  new-api-cli skills read new-api-channel/references/new-api-channel-health.md`,
	}
	cmd.AddCommand(newListCmd(f), newReadCmd(f))
	return cmd
}

func newListCmd(f *cmdutil.Factory) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "list [name[/path]]",
		Aliases: []string{"ls"},
		Short:   "列出全部 skill，或列出某个 skill 路径下的一层（类似 ls）",
		Args:    cobra.MaximumNArgs(1),
		Example: `  new-api-cli skills list                                  # 全部 skill：名称、描述、版本
  new-api-cli skills list new-api-channel                  # 列出该 skill 下的一层
  new-api-cli skills list new-api-channel/references       # 列出子目录下的一层`,
		RunE: func(cmd *cobra.Command, args []string) error {
			r, err := newReader(f)
			if err != nil {
				return err
			}
			if len(args) == 0 {
				skills, err := r.List()
				if err != nil {
					return err
				}
				return f.EmitResult(output.Result{
					Data:    skills,
					Meta:    &output.Meta{Count: len(skills)},
					Columns: []string{"name", "version", "description"},
				})
			}
			entries, listed, err := r.ListPath(args[0])
			if err != nil {
				return err
			}
			return f.EmitResult(output.Result{
				Data:    entries,
				Meta:    &output.Meta{Count: len(entries)},
				Columns: []string{"path", "is_dir"},
				Message: "已列出 " + listed,
			})
		},
	}
	cmdutil.SetRisk(cmd, cmdutil.RiskRead)
	return cmd
}

func newReadCmd(f *cmdutil.Factory) *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "read <name>[/<path>] [path]",
		Short: "打印 skill 的 SKILL.md，或其下某个文件（默认输出原始 markdown）",
		Long: `打印 skill 文档。

只给 skill 名时读它的 SKILL.md；再给一个相对路径时读该 skill 下的文件。
默认把原始 markdown 原样写到 stdout（便于 Agent 直接消费），提示信息走
stderr；加 --json 则包成标准 JSON 信封。`,
		Args: cobra.RangeArgs(1, 2),
		Example: `  new-api-cli skills read new-api-shared                                        # 该 skill 的 SKILL.md
  new-api-cli skills read new-api-channel references/new-api-channel-health.md  # 读该 skill 下的文件
  new-api-cli skills read new-api-channel/references/new-api-channel-health.md  # 同上，斜杠写法
  new-api-cli skills read new-api-shared --json                                 # JSON 信封`,
		RunE: func(cmd *cobra.Command, args []string) error {
			name, relpath := parseReadTarget(args)
			r, err := newReader(f)
			if err != nil {
				return err
			}

			var content []byte
			var pathOut string
			if relpath == "" {
				content, err = r.ReadSkill(name)
				pathOut = "SKILL.md"
			} else {
				content, pathOut, err = r.ReadReference(name, relpath)
			}
			if err != nil {
				return err
			}

			isMain := pathOut == "SKILL.md"
			if asJSON {
				data := map[string]any{"skill": name, "path": pathOut, "content": string(content)}
				if isMain {
					data["guidance"] = readGuidance(name)
				}
				return f.EmitData(data)
			}
			// 原始输出与文件逐字节一致，指引写到 stderr 不污染管道。
			if _, err := f.IOStreams.Out.Write(content); err != nil {
				return errs.NewInternalError("写出内容失败: %v", err)
			}
			if isMain {
				fmt.Fprintln(f.IOStreams.Err, readGuidance(name))
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "输出 JSON 信封而不是原始 markdown")
	cmdutil.SetRisk(cmd, cmdutil.RiskRead)
	return cmd
}

// parseReadTarget 把 1 或 2 个位置参数映射成 (name, relpath)：
// 单个 "<a>/<b>" 在第一个 '/' 处切分；relpath 为空表示读 SKILL.md。
func parseReadTarget(args []string) (name, relpath string) {
	if len(args) == 2 {
		return args[0], args[1]
	}
	return skillcontent.SplitArg(args[0])
}

// readGuidance 把跨 skill 的 "../new-api-foo/..." 引用翻译回 skills read 的形式 ——
// 路径守卫会拒绝字面量 "../"，所以相对写法必须改写。
func readGuidance(name string) string {
	return fmt.Sprintf("提示: 读本 skill 自己的文件（如 references/...）用 "+
		"`new-api-cli skills read %s <相对路径>`，这样内容与当前 CLI 版本一致。"+
		"引用其他 skill（`../new-api-foo/...`）时去掉开头的 `../`："+
		"`new-api-cli skills read new-api-foo/...`。", name)
}
