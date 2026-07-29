// Package config 实现 config 命令域：管理连接配置与 profile。
package config

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/huangxin8899/new-api-cli/errs"
	"github.com/huangxin8899/new-api-cli/internal/client"
	"github.com/huangxin8899/new-api-cli/internal/cmdutil"
	cfg "github.com/huangxin8899/new-api-cli/internal/config"
	"github.com/huangxin8899/new-api-cli/internal/output"
	"github.com/spf13/cobra"
)

// NewCmd 构造 config 命令组。
func NewCmd(f *cmdutil.Factory) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "管理站点配置与 profile",
		Long: `管理 new-api-cli 的连接配置。

配置存放在 ~/.new-api-cli/config.yaml（可用 NEW_API_CLI_HOME 改写），
令牌单独存放在同目录的 credentials.json（权限 0600）。

一个 profile 对应一个 New API 实例。管理多套环境时用 --profile 或 config use 切换。`,
		Example: `  # 交互式初始化（首次使用）
  new-api-cli config init

  # 非交互初始化（Agent / CI）
  new-api-cli config init --base-url https://api.example.com --profile prod --force

  # 查看与切换
  new-api-cli config list
  new-api-cli config use prod
  new-api-cli config show`,
	}
	cmd.AddCommand(
		newInitCmd(f),
		newShowCmd(f),
		newListCmd(f),
		newUseCmd(f),
		newSetCmd(f),
		newRemoveCmd(f),
	)
	return cmd
}

func newInitCmd(f *cmdutil.Factory) *cobra.Command {
	var (
		baseURL  string
		profile  string
		token    string
		insecure bool
		force    bool
		skipTest bool
	)
	cmd := &cobra.Command{
		Use:   "init",
		Short: "初始化站点配置",
		Long: `初始化一个 profile：填写站点地址并验证连通性。

不带参数时进入交互式引导；传了 --base-url 则直接写入，适合 Agent 与 CI。
写入后会调用站点的公开状态接口做一次连通性探测（--skip-test 可跳过）。`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runInit(cmdutil.Context(cmd), f, initOptions{
				BaseURL: baseURL, Profile: profile, Token: token,
				Insecure: insecure, Force: force, SkipTest: skipTest,
			})
		},
	}
	cmd.Flags().StringVar(&baseURL, "base-url", "", "New API 站点地址，如 https://api.example.com")
	cmd.Flags().StringVar(&profile, "profile-name", "", "profile 名称（默认 default）")
	cmd.Flags().StringVar(&token, "save-token", "", "同时保存系统访问令牌（等价于随后执行 auth login --token）")
	cmd.Flags().BoolVar(&insecure, "insecure-tls", false, "跳过 TLS 证书校验（自签名证书）")
	cmd.Flags().BoolVar(&force, "force", false, "覆盖已存在的同名 profile")
	cmd.Flags().BoolVar(&skipTest, "skip-test", false, "跳过连通性探测")
	cmdutil.SetRisk(cmd, cmdutil.RiskWrite)
	return cmd
}

type initOptions struct {
	BaseURL  string
	Profile  string
	Token    string
	Insecure bool
	Force    bool
	SkipTest bool
}

func runInit(ctx context.Context, f *cmdutil.Factory, opts initOptions) error {
	store, err := cfg.Load()
	if err != nil {
		return err
	}
	p := f.NewPrompter()

	name := opts.Profile
	if name == "" {
		name = f.Globals.Profile
	}
	baseURL := opts.BaseURL
	if baseURL == "" {
		baseURL = f.Globals.BaseURL
	}

	if baseURL == "" {
		if !p.Interactive() {
			return errs.NewValidationError(errs.SubtypeMissingArgument,
				"非交互环境下必须提供站点地址").
				WithHint("new-api-cli config init --base-url https://api.example.com").
				WithParams("--base-url")
		}
		fmt.Fprintln(f.IOStreams.Err, "配置 New API 站点连接（配置写入 ~/.new-api-cli/config.yaml）")
		if name == "" {
			name, err = p.Line("profile 名称", cfg.DefaultProfile)
			if err != nil {
				return err
			}
		}
		baseURL, err = p.Line("站点地址（如 https://api.example.com）", "")
		if err != nil {
			return err
		}
	}
	if name == "" {
		name = cfg.DefaultProfile
	}
	baseURL = cfg.NormalizeBaseURL(baseURL)
	if baseURL == "" {
		return errs.NewValidationError(errs.SubtypeMissingArgument, "站点地址不能为空").
			WithParams("--base-url")
	}
	if !strings.HasPrefix(baseURL, "http://") && !strings.HasPrefix(baseURL, "https://") {
		baseURL = "https://" + baseURL
	}

	if _, exists := store.Profiles[name]; exists && !opts.Force {
		if !p.Interactive() {
			return errs.NewValidationError(errs.SubtypeInvalidArgument,
				"profile %q 已存在", name).
				WithHint("加 --force 覆盖，或换一个 --profile-name").
				WithParams("--force")
		}
		if !p.YesNo(fmt.Sprintf("profile %q 已存在，覆盖它吗", name)) {
			return errs.NewValidationError(errs.SubtypeConfirmRequired, "已取消")
		}
	}

	profile := &cfg.Profile{BaseURL: baseURL, Insecure: opts.Insecure || f.Globals.Insecure}
	store.Set(name, profile)
	store.Current = name
	if err := store.Save(); err != nil {
		return err
	}

	result := map[string]any{
		"profile":     name,
		"base_url":    baseURL,
		"config_path": store.Path(),
	}

	// 探测：用公开状态接口验证地址可达，避免把打错的地址留到下一次调用。
	if !opts.SkipTest {
		status, err := probeSite(ctx, f, name)
		if err != nil {
			result["reachable"] = false
			result["probe_error"] = err.Error()
			if e, err2 := f.Emitter(); err2 == nil {
				e.Warn("站点探测失败：%v", err)
			}
		} else {
			result["reachable"] = true
			result["site"] = status
		}
	}

	if opts.Token != "" {
		cred := &cfg.Credential{Kind: cfg.KindPAT, Token: opts.Token, BaseURL: baseURL}
		if err := cfg.SaveCredential(name, cred); err != nil {
			return err
		}
		result["token_saved"] = true
	}

	msg := fmt.Sprintf("已保存 profile %q。下一步：new-api-cli auth login", name)
	if opts.Token != "" {
		msg = fmt.Sprintf("已保存 profile %q 与访问令牌。用 new-api-cli auth status 验证", name)
	}
	return f.EmitResult(output.Result{Data: result, Message: msg})
}

// probeSite 调用公开的 /api/status 验证地址指向的确实是 New API。
func probeSite(ctx context.Context, f *cmdutil.Factory, profile string) (any, error) {
	settings, err := cfg.ResolveSettings(cfg.Overrides{
		Profile:  profile,
		Insecure: f.Globals.Insecure,
		Timeout:  f.Globals.Timeout,
	})
	if err != nil {
		return nil, err
	}
	c := client.New(settings)
	resp, err := c.Do(ctx, client.Request{Method: "GET", Path: "/api/status", NoAuth: true})
	if err != nil {
		return nil, err
	}
	data, err := resp.Any()
	if err != nil {
		return nil, err
	}
	// 只回显有辨识度的少量字段，避免把整个站点配置刷屏。
	if m, ok := data.(map[string]any); ok {
		brief := map[string]any{}
		for _, k := range []string{"version", "system_name", "start_time", "chat_link"} {
			if v, ok := m[k]; ok {
				brief[k] = v
			}
		}
		if len(brief) > 0 {
			return brief, nil
		}
	}
	return data, nil
}

func newShowCmd(f *cmdutil.Factory) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "show",
		Short: "显示当前生效的配置（令牌脱敏）",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			settings, err := f.Settings()
			if err != nil {
				return err
			}
			store, err := cfg.Load()
			if err != nil {
				return err
			}
			data := map[string]any{
				"profile":      settings.Profile,
				"base_url":     settings.BaseURL,
				"insecure":     settings.Insecure,
				"timeout":      settings.Timeout,
				"token":        cfg.MaskToken(settings.Token),
				"token_source": settings.TokenSource,
				"logged_in":    settings.Token != "",
				"config_path":  store.Path(),
			}
			if settings.UserID > 0 {
				data["as_user"] = settings.UserID
			}
			return f.EmitData(data)
		},
	}
	cmdutil.SetRisk(cmd, cmdutil.RiskRead)
	return cmd
}

func newListCmd(f *cmdutil.Factory) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "列出全部 profile",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			store, err := cfg.Load()
			if err != nil {
				return err
			}
			creds, err := cfg.ListCredentials()
			if err != nil {
				return err
			}
			names := store.ProfileNames()
			sort.Strings(names)

			rows := make([]any, 0, len(names))
			for _, name := range names {
				p := store.Profiles[name]
				row := map[string]any{
					"name":     name,
					"current":  name == store.Current,
					"base_url": p.BaseURL,
					"logged_in": func() bool {
						_, ok := creds[name]
						return ok
					}(),
				}
				if c, ok := creds[name]; ok {
					row["username"] = c.Username
					row["role"] = cfg.RoleName(c.Role)
				}
				rows = append(rows, row)
			}
			return f.EmitResult(output.Result{
				Data:    rows,
				Meta:    &output.Meta{Count: len(rows)},
				Columns: []string{"name", "current", "base_url", "logged_in", "username", "role"},
			})
		},
	}
	cmdutil.SetRisk(cmd, cmdutil.RiskRead)
	return cmd
}

func newUseCmd(f *cmdutil.Factory) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "use <profile>",
		Short: "切换当前 profile",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			store, err := cfg.Load()
			if err != nil {
				return err
			}
			name := args[0]
			if _, ok := store.Profiles[name]; !ok {
				return errs.NewConfigError(errs.SubtypeProfileNotFound,
					"profile %q 不存在", name).
					WithHint("已有：%s", strings.Join(store.ProfileNames(), ", "))
			}
			store.Current = name
			if err := store.Save(); err != nil {
				return err
			}
			return f.EmitResult(output.Result{
				Data:    map[string]any{"current": name, "base_url": store.Profiles[name].BaseURL},
				Message: fmt.Sprintf("已切换到 profile %q", name),
			})
		},
		ValidArgsFunction: func(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective) {
			store, err := cfg.Load()
			if err != nil {
				return nil, cobra.ShellCompDirectiveNoFileComp
			}
			return store.ProfileNames(), cobra.ShellCompDirectiveNoFileComp
		},
	}
	cmdutil.SetRisk(cmd, cmdutil.RiskWrite)
	return cmd
}

func newSetCmd(f *cmdutil.Factory) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "set <key> <value>",
		Short: "修改当前 profile 的单个字段",
		Long: `修改当前 profile 的字段。可用 key：

  base_url   站点地址
  username   登录用户名（仅用于 auth login 回填）
  insecure   true|false，跳过 TLS 校验
  timeout    请求超时秒数
  user_id    默认以哪个用户身份调用（管理员专用）`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			store, err := cfg.Load()
			if err != nil {
				return err
			}
			name, profile, err := store.Resolve(f.Globals.Profile)
			if err != nil {
				return err
			}
			key, value := strings.ToLower(args[0]), args[1]
			switch key {
			case "base_url", "base-url":
				profile.BaseURL = cfg.NormalizeBaseURL(value)
			case "username":
				profile.Username = value
			case "insecure":
				b, err := strconv.ParseBool(value)
				if err != nil {
					return errs.NewValidationError(errs.SubtypeInvalidArgument,
						"insecure 需要 true 或 false，收到 %q", value)
				}
				profile.Insecure = b
			case "timeout":
				n, err := strconv.Atoi(value)
				if err != nil || n < 0 {
					return errs.NewValidationError(errs.SubtypeInvalidArgument,
						"timeout 需要非负整数秒，收到 %q", value)
				}
				profile.Timeout = n
			case "user_id", "user-id":
				n, err := strconv.Atoi(value)
				if err != nil || n < 0 {
					return errs.NewValidationError(errs.SubtypeInvalidArgument,
						"user_id 需要非负整数，收到 %q", value)
				}
				profile.UserID = n
			default:
				return errs.NewValidationError(errs.SubtypeInvalidArgument,
					"未知配置项 %q", key).
					WithHint("可用：base_url|username|insecure|timeout|user_id")
			}
			store.Set(name, profile)
			if err := store.Save(); err != nil {
				return err
			}
			return f.EmitResult(output.Result{
				Data:    map[string]any{"profile": name, key: value},
				Message: fmt.Sprintf("已更新 profile %q 的 %s", name, key),
			})
		},
		ValidArgsFunction: func(_ *cobra.Command, args []string, _ string) ([]string, cobra.ShellCompDirective) {
			if len(args) == 0 {
				return []string{"base_url", "username", "insecure", "timeout", "user_id"},
					cobra.ShellCompDirectiveNoFileComp
			}
			return nil, cobra.ShellCompDirectiveNoFileComp
		},
	}
	cmdutil.SetRisk(cmd, cmdutil.RiskWrite)
	return cmd
}

func newRemoveCmd(f *cmdutil.Factory) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "remove <profile>",
		Aliases: []string{"rm"},
		Short:   "删除 profile 及其保存的登录态",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			if err := f.Confirm(fmt.Sprintf("删除 profile %q 及其令牌", name)); err != nil {
				return err
			}
			store, err := cfg.Load()
			if err != nil {
				return err
			}
			if err := store.Remove(name); err != nil {
				return err
			}
			return f.EmitResult(output.Result{
				Data:    map[string]any{"removed": name, "current": store.Current},
				Message: fmt.Sprintf("已删除 profile %q", name),
			})
		},
	}
	cmdutil.SetRisk(cmd, cmdutil.RiskHighRisk)
	return cmd
}
