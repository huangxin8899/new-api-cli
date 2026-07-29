// Package cmd 组装 new-api-cli 的命令树。
package cmd

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/huangxin8899/new-api-cli/cmd/api"
	"github.com/huangxin8899/new-api-cli/cmd/auth"
	"github.com/huangxin8899/new-api-cli/cmd/channel"
	configcmd "github.com/huangxin8899/new-api-cli/cmd/config"
	"github.com/huangxin8899/new-api-cli/cmd/data"
	"github.com/huangxin8899/new-api-cli/cmd/log"
	modelcmd "github.com/huangxin8899/new-api-cli/cmd/model"
	"github.com/huangxin8899/new-api-cli/cmd/option"
	"github.com/huangxin8899/new-api-cli/cmd/redemption"
	"github.com/huangxin8899/new-api-cli/cmd/skill"
	statuscmd "github.com/huangxin8899/new-api-cli/cmd/status"
	"github.com/huangxin8899/new-api-cli/cmd/token"
	usercmd "github.com/huangxin8899/new-api-cli/cmd/user"
	"github.com/huangxin8899/new-api-cli/errs"
	"github.com/huangxin8899/new-api-cli/internal/build"
	"github.com/huangxin8899/new-api-cli/internal/cmdutil"
	"github.com/huangxin8899/new-api-cli/internal/output"
	"github.com/spf13/cobra"
)

const rootLong = `new-api-cli — New API 网关命令行工具。

面向 AI Agent 的速查：
    读技能文档: new-api-cli skills list             # 内置技能文档，先读 new-api-shared
    浏览命令:   new-api-cli <域> --help          # 每个域下有 + 快捷命令和资源命令
    查看风险:   命令 --help 顶部标注 read | write | high-risk-write
                high-risk-write 必须先获得用户确认，再加 --yes 执行
    过滤输出:   --jq '.items[].name' 提取字段；--format table 给人看
    预演请求:   --dry-run 只打印将要发出的请求，不做任何改动

三层调用，按需选择粒度：
    new-api-cli channel +health                 # 快捷命令 — 一句话完成一件事，优先用它
    new-api-cli channel list --status enabled   # 资源命令 — 与管理接口一一对应
    new-api-cli api GET /api/channel/           # 通用调用 — 覆盖全部接口的兜底`

const rootExample = `  # 首次配置（交互式，只需一次）
  new-api-cli config init

  # 登录（推荐用系统访问令牌，在 New API 个人设置页生成）
  new-api-cli auth login --token sk-xxxx
  new-api-cli auth status

  # 常用查询
  new-api-cli channel +health
  new-api-cli token list --format table
  new-api-cli log list --type error --since 1h --format table`

// 命令分组，让 --help 有结构而不是一长串。
const (
	groupResource = "resource"
	groupOps      = "ops"
	groupSystem   = "system"
	groupSetup    = "setup"
)

// usageError 标记「用户把命令写错了」，退出码 2 并打印用法。
type usageError struct{ err error }

func (u *usageError) Error() string { return u.err.Error() }
func (u *usageError) Unwrap() error { return u.err }

// embeddedSkillContent 保存编译期嵌入的 skill 文档，由 main 包在 init 时注入。
// 单文件预览构建（go build ./main.go）不注入，此时 skills 命令会明确报错，
// 而不是静默返回空列表。
var embeddedSkillContent fs.FS

// SetEmbeddedSkillContent 注入 skill 文档的文件系统，根目录为 skill 列表。
func SetEmbeddedSkillContent(fsys fs.FS) { embeddedSkillContent = fsys }

// Execute 构建命令树、执行并返回进程退出码。
func Execute() int {
	streams := cmdutil.SystemIOStreams()
	globals := &cmdutil.GlobalFlags{}
	f := cmdutil.NewFactory(streams, globals)
	f.SkillContent = embeddedSkillContent
	root := NewRootCmd(f)

	// Ctrl+C 取消进行中的请求，而不是留下半截 TCP 连接。
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	err := root.ExecuteContext(ctx)
	if err == nil {
		return errs.ExitOK
	}
	return handleError(f, err)
}

// NewRootCmd 组装完整命令树。导出以便集成测试直接驱动。
func NewRootCmd(f *cmdutil.Factory) *cobra.Command {
	g := f.Globals

	root := &cobra.Command{
		Use:           "new-api-cli",
		Short:         "New API 网关命令行工具",
		Long:          rootLong,
		Example:       rootExample,
		Version:       build.Version,
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.SetIn(f.IOStreams.In)
	root.SetOut(f.IOStreams.Out)
	root.SetErr(f.IOStreams.Err)

	// flag 解析失败属于用法错误，包装后走退出码 2。
	root.SetFlagErrorFunc(func(cmd *cobra.Command, err error) error {
		return &usageError{err: err}
	})

	pf := root.PersistentFlags()
	pf.StringVar(&g.Profile, "profile", "", "使用指定的配置 profile（默认取 config 中的 current）")
	pf.StringVar(&g.BaseURL, "base-url", "", "New API 站点地址，覆盖配置文件")
	pf.StringVar(&g.Token, "token", "", "系统访问令牌，覆盖已保存的登录态")
	pf.StringVarP(&g.Format, "format", "f", string(output.FormatJSON), "输出格式：json|table|pretty|ndjson|csv")
	pf.StringVarP(&g.JQ, "jq", "q", "", "用 jq 表达式过滤输出，例如 '.items[].name'")
	pf.BoolVar(&g.Yes, "yes", false, "跳过确认，用于破坏性操作与非交互环境")
	pf.BoolVar(&g.DryRun, "dry-run", false, "只打印将要发起的请求，不真正执行")
	pf.BoolVarP(&g.Verbose, "verbose", "v", false, "把请求详情打印到 stderr")
	pf.BoolVar(&g.Insecure, "insecure", false, "跳过 TLS 证书校验（自签名证书的私有部署）")
	pf.IntVar(&g.Timeout, "timeout", 0, "单次请求超时秒数（默认 60）")
	pf.IntVar(&g.UserID, "as-user", 0, "以指定用户身份调用（管理员专用，对应 New-API-User 头）")
	pf.BoolVar(&g.NoColor, "no-color", false, "禁用彩色输出")

	root.AddGroup(
		&cobra.Group{ID: groupSetup, Title: "配置与认证:"},
		&cobra.Group{ID: groupResource, Title: "资源管理:"},
		&cobra.Group{ID: groupOps, Title: "运营与数据:"},
		&cobra.Group{ID: groupSystem, Title: "系统与通用:"},
	)

	addCommand(root, groupSetup, configcmd.NewCmd(f), auth.NewCmd(f))
	addCommand(root, groupResource,
		channel.NewCmd(f),
		token.NewCmd(f),
		usercmd.NewCmd(f),
		modelcmd.NewCmd(f),
		redemption.NewCmd(f),
	)
	addCommand(root, groupOps, log.NewCmd(f), data.NewCmd(f))
	addCommand(root, groupSystem,
		option.NewCmd(f),
		statuscmd.NewCmd(f),
		api.NewCmd(f),
		skill.NewCmd(f),
		newCompletionCmd(),
		newVersionCmd(f),
	)

	registerFlagCompletions(root)
	root.SetHelpTemplate(helpTemplate)
	root.SetUsageTemplate(usageTemplate)
	return root
}

func addCommand(root *cobra.Command, group string, cmds ...*cobra.Command) {
	for _, c := range cmds {
		c.GroupID = group
		root.AddCommand(c)
	}
}

func registerFlagCompletions(root *cobra.Command) {
	_ = root.RegisterFlagCompletionFunc("format",
		func(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective) {
			names := make([]string, 0, len(output.AllFormats))
			for _, f := range output.AllFormats {
				names = append(names, string(f))
			}
			return names, cobra.ShellCompDirectiveNoFileComp
		})
}

// handleError 把错误翻译成用户可见输出与退出码。
func handleError(f *cmdutil.Factory, err error) int {
	var ue *usageError
	if errors.As(err, &ue) {
		fmt.Fprintln(f.IOStreams.Err, "用法错误:", ue.Error())
		fmt.Fprintln(f.IOStreams.Err, "运行 new-api-cli --help 查看用法")
		return errs.ExitUsage
	}
	// cobra 对未知子命令/参数个数不符返回普通 error，按用法错误处理。
	msg := err.Error()
	for _, prefix := range []string{"unknown command", "unknown flag", "unknown shorthand",
		"accepts ", "requires at least", "invalid argument"} {
		if strings.HasPrefix(msg, prefix) {
			fmt.Fprintln(f.IOStreams.Err, "用法错误:", msg)
			return errs.ExitUsage
		}
	}

	e, emitErr := f.Emitter()
	if emitErr != nil {
		// --format 自身非法时不能相信它来选渲染器，但 json 是文档承诺的默认
		// 契约，退回它而不是退回纯文本 —— 否则恰好在 Agent 解析时破约。
		// 报告的是 --format 的错误本身，原始错误作为次要信息附在 stderr。
		fallback := &output.Emitter{
			Out:    f.IOStreams.Out,
			Err:    f.IOStreams.Err,
			Format: output.FormatJSON,
		}
		code := fallback.EmitError(emitErr)
		// 命令自身的输出路径也会撞上同一个 --format 错误，此时 err 与 emitErr
		// 是同一件事，重复打印只会让人以为出了两个问题。
		if err.Error() != emitErr.Error() {
			fmt.Fprintln(f.IOStreams.Err, "（另有错误：", err.Error(), "）")
		}
		return code
	}
	return e.EmitError(err)
}

func newVersionCmd(f *cmdutil.Factory) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "version",
		Short: "显示 CLI 版本",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return f.EmitData(map[string]any{
				"version":    build.Version,
				"commit":     build.Commit,
				"build_date": build.Date,
				"go_version": build.GoVersion(),
				"platform":   build.Platform(),
			})
		},
	}
	cmdutil.SetRisk(cmd, cmdutil.RiskRead)
	return cmd
}

func newCompletionCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "completion <bash|zsh|fish|powershell>",
		Short: "生成 shell 自动补全脚本",
		Long: `生成 shell 自动补全脚本。

  bash:        new-api-cli completion bash > /etc/bash_completion.d/new-api-cli
  zsh:         new-api-cli completion zsh > "${fpath[1]}/_new-api-cli"
  fish:        new-api-cli completion fish > ~/.config/fish/completions/new-api-cli.fish
  powershell:  new-api-cli completion powershell | Out-String | Invoke-Expression`,
		Args:      cobra.ExactArgs(1),
		ValidArgs: []string{"bash", "zsh", "fish", "powershell"},
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()
			switch args[0] {
			case "bash":
				return cmd.Root().GenBashCompletionV2(out, true)
			case "zsh":
				return cmd.Root().GenZshCompletion(out)
			case "fish":
				return cmd.Root().GenFishCompletion(out, true)
			case "powershell":
				return cmd.Root().GenPowerShellCompletionWithDesc(out)
			}
			return errs.NewValidationError(errs.SubtypeInvalidArgument,
				"不支持的 shell: %s", args[0]).
				WithHint("可选：bash|zsh|fish|powershell")
		},
	}
	cmdutil.SetRisk(cmd, cmdutil.RiskRead)
	return cmd
}

// helpTemplate 在标准帮助前面加一行风险标注，让 Agent 一眼看到门禁要求。
const helpTemplate = `{{with (or .Long .Short)}}{{. | trimTrailingWhitespaces}}

{{end}}{{if riskLine .}}{{riskLine .}}

{{end}}{{if or .Runnable .HasSubCommands}}{{.UsageString}}{{end}}`

const usageTemplate = `用法:{{if .Runnable}}
  {{.UseLine}}{{end}}{{if .HasAvailableSubCommands}}
  {{.CommandPath}} [子命令]{{end}}{{if gt (len .Aliases) 0}}

别名:
  {{.NameAndAliases}}{{end}}{{if .HasExample}}

示例:
{{.Example}}{{end}}{{if .HasAvailableSubCommands}}{{$cmds := .Commands}}{{if eq (len .Groups) 0}}

可用命令:{{range $cmds}}{{if (or .IsAvailableCommand (eq .Name "help"))}}
  {{rpad .Name .NamePadding }} {{.Short}}{{end}}{{end}}{{else}}{{range $group := .Groups}}

{{.Title}}{{range $cmds}}{{if (and (eq .GroupID $group.ID) (or .IsAvailableCommand (eq .Name "help")))}}
  {{rpad .Name .NamePadding }} {{.Short}}{{end}}{{end}}{{end}}{{if not .AllChildCommandsHaveGroup}}

其他命令:{{range $cmds}}{{if (and (eq .GroupID "") (or .IsAvailableCommand (eq .Name "help")))}}
  {{rpad .Name .NamePadding }} {{.Short}}{{end}}{{end}}{{end}}{{end}}{{end}}{{if .HasAvailableLocalFlags}}

选项:
{{.LocalFlags.FlagUsages | trimTrailingWhitespaces}}{{end}}{{if .HasAvailableInheritedFlags}}

全局选项:
{{.InheritedFlags.FlagUsages | trimTrailingWhitespaces}}{{end}}{{if .HasHelpSubCommands}}

其他帮助主题:{{range .Commands}}{{if .IsAdditionalHelpTopicCommand}}
  {{rpad .CommandPath .CommandPathPadding}} {{.Short}}{{end}}{{end}}{{end}}{{if .HasAvailableSubCommands}}

用 "{{.CommandPath}} [命令] --help" 查看子命令详情。{{end}}
`

func init() {
	cobra.AddTemplateFunc("riskLine", func(cmd *cobra.Command) string {
		if !cmd.Runnable() {
			return ""
		}
		switch cmdutil.RiskOf(cmd) {
		case cmdutil.RiskHighRisk:
			return "风险: high-risk-write — 不可逆，执行前必须获得用户确认，并加 --yes"
		case cmdutil.RiskWrite:
			return "风险: write — 会修改服务端数据"
		default:
			return "风险: read — 只读，不会改动任何数据"
		}
	})
}
