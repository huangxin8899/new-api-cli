// Package api 实现通用 api 命令：直接按 HTTP 方法与路径调用任意接口。
//
// 这是三层命令的最后一层 —— 当某个接口还没有对应的资源命令时用它兜底。
package api

import (
	"context"
	"net/url"
	"os"
	"strings"

	"github.com/huangxin8899/new-api-cli/errs"
	"github.com/huangxin8899/new-api-cli/internal/client"
	"github.com/huangxin8899/new-api-cli/internal/cmdutil"
	"github.com/huangxin8899/new-api-cli/internal/output"

	"github.com/spf13/cobra"
)

var httpMethods = []string{"GET", "POST", "PUT", "PATCH", "DELETE", "HEAD"}

// NewCmd 构建 api 命令。
func NewCmd(f *cmdutil.Factory) *cobra.Command {
	var data, params, outFile string
	var raw, pageAll bool
	var limit int

	cmd := &cobra.Command{
		Use:   "api <method> <path>",
		Short: "通用调用：按 HTTP 方法与路径访问任意接口",
		Long: `直接调用 New API 的任意接口。

优先用资源命令（` + "`new-api-cli channel list`" + ` 等）—— 它们做了参数校验、
风险标注与输出投影。只有在接口尚无对应命令时才用 ` + "`api`" + `。

路径可以省略 /api 前缀：` + "`/channel/`" + ` 与 ` + "`/api/channel/`" + ` 等价。
以 /v1、/mj、/suno、/pg 开头的路径保持原样，便于访问 relay 侧接口。

--data 与 --params 都支持三种写法：内联 JSON、@文件、-（读标准输入）。`,
		Args: cobra.ExactArgs(2),
		Example: "  new-api-cli api GET /api/channel/ --params '{\"p\":1,\"page_size\":10}'\n" +
			"  new-api-cli api GET /channel/ --page-all --jq '.[].name'\n" +
			"  new-api-cli api POST /api/channel/ --data @channel.json\n" +
			"  new-api-cli api PUT /api/option/ --data '{\"key\":\"AutomaticDisableChannelEnabled\",\"value\":\"true\"}'\n" +
			"  new-api-cli api GET /api/status --raw",
		RunE: func(cmd *cobra.Command, args []string) error {
			method := strings.ToUpper(strings.TrimSpace(args[0]))
			if !isKnownMethod(method) {
				return errs.NewValidationError(errs.SubtypeInvalidArgument,
					"不支持的 HTTP 方法 %q", args[0]).
					WithHint("可选：%s", strings.Join(httpMethods, "、")).
					WithParams("<method>")
			}
			if pageAll && method != "GET" {
				return errs.NewValidationError(errs.SubtypeInvalidArgument,
					"--page-all 只能用于 GET").
					WithParams("--page-all")
			}
			if raw && pageAll {
				return errs.NewValidationError(errs.SubtypeInvalidArgument,
					"--raw 与 --page-all 不能同时使用").
					WithHint("--raw 直接透传原始响应，无法解析分页").
					WithParams("--raw", "--page-all")
			}
			if data == "-" && params == "-" {
				return errs.NewValidationError(errs.SubtypeInvalidArgument,
					"--data 与 --params 不能同时从标准输入读取").
					WithParams("--data", "--params")
			}

			req := client.Request{Method: method, Path: args[1]}

			if params != "" {
				m, err := f.ReadJSONInput(params, "--params")
				if err != nil {
					return err
				}
				req.Query = queryFromMap(m)
			}
			if data != "" {
				body, err := f.ReadTextInput(data, "--data")
				if err != nil {
					return err
				}
				if strings.TrimSpace(body) != "" {
					// 先校验是合法 JSON，避免把明显错误的载荷发到服务端。
					if _, err := f.ReadJSONInput(body, "--data"); err != nil {
						return err
					}
					req.RawBody = []byte(body)
				}
			}

			ctx := cmdutil.Context(cmd)
			if f.Globals.DryRun {
				return f.DryRunResult(req)
			}

			if pageAll {
				var lf cmdutil.ListFlags
				lf.All = true
				lf.Limit = limit
				return f.RunList(ctx, req, &lf, nil)
			}
			if raw || outFile != "" {
				return runRaw(f, ctx, req, outFile)
			}
			return f.RunSingle(ctx, req)
		},
	}

	fl := cmd.Flags()
	fl.StringVar(&data, "data", "", "请求体 JSON：内联 | @文件 | -（标准输入）")
	fl.StringVar(&params, "params", "", "查询参数 JSON：内联 | @文件 | -（标准输入）")
	fl.BoolVar(&raw, "raw", false, "透传原始响应，不解析 success/data 信封")
	fl.BoolVar(&pageAll, "page-all", false, "自动翻页取回全部 items（仅 GET）")
	fl.IntVar(&limit, "limit", 0, "配合 --page-all，最多取回多少条（0 = 不限）")
	fl.StringVarP(&outFile, "output", "o", "", "把原始响应写入文件（隐含 --raw）")

	cmd.ValidArgsFunction = func(_ *cobra.Command, args []string, _ string) ([]string, cobra.ShellCompDirective) {
		if len(args) == 0 {
			return httpMethods, cobra.ShellCompDirectiveNoFileComp
		}
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	// 方法由用户决定，可能是 DELETE，因此按最高风险标注。
	cmdutil.SetRisk(cmd, cmdutil.RiskHighRisk)
	return cmd
}

func isKnownMethod(m string) bool {
	for _, known := range httpMethods {
		if m == known {
			return true
		}
	}
	return false
}

// queryFromMap 把 --params 的 JSON 对象摊平成查询参数。
// 数组值展开成重复键，布尔与数字按字面量序列化，null 直接跳过。
func queryFromMap(m map[string]any) url.Values {
	q := url.Values{}
	for k, v := range m {
		switch val := v.(type) {
		case nil:
			continue
		case []any:
			for _, item := range val {
				q.Add(k, output.ScalarString(item))
			}
		default:
			q.Add(k, output.ScalarString(val))
		}
	}
	return q
}

func runRaw(f *cmdutil.Factory, ctx context.Context, req client.Request, outFile string) error {
	c, err := f.Client()
	if err != nil {
		return err
	}
	resp, err := c.DoRaw(ctx, req)
	if err != nil {
		return err
	}
	if outFile != "" {
		if err := os.WriteFile(outFile, resp.Body, 0o600); err != nil {
			return errs.NewValidationError(errs.SubtypeFileIO,
				"写入 %s 失败: %v", outFile, err).WithParams("--output")
		}
		return f.EmitResult(output.Result{
			Data: map[string]any{
				"path":        outFile,
				"bytes":       len(resp.Body),
				"http_status": resp.HTTPStatus,
			},
			Message: "响应已写入 " + outFile,
		})
	}
	// --raw 下 stdout 承载原始字节，不加信封，便于管道处理。
	_, err = f.IOStreams.Out.Write(resp.Body)
	return err
}
