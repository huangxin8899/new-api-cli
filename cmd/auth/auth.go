// Package auth 实现 auth 命令域：登录、登出、查看身份。
package auth

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"time"

	"github.com/QuantumNous/new-api-cli/errs"
	"github.com/QuantumNous/new-api-cli/internal/client"
	"github.com/QuantumNous/new-api-cli/internal/cmdutil"
	cfg "github.com/QuantumNous/new-api-cli/internal/config"
	"github.com/QuantumNous/new-api-cli/internal/output"
	"github.com/spf13/cobra"
)

// NewCmd 构造 auth 命令组。
func NewCmd(f *cmdutil.Factory) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "auth",
		Short: "登录、登出与身份查看",
		Long: `管理 new-api-cli 的登录态。

两种凭据，按场景选：

  系统访问令牌（PAT）— 推荐。长期有效，无需续期，适合 CLI、CI 与 Agent。
    在 New API 「个人设置 → 系统访问令牌」生成后，用 auth login --token 保存。

  密码登录 — 换来的是短期 access token，过期后需要重新登录。
    适合临时排查；配合 --generate-token 可直接换成长期令牌。

注意区分：这里说的令牌是「管理后台令牌」，不是 token 命令管理的「API 调用令牌（sk-...）」。`,
		Example: `  # 用系统访问令牌登录（推荐）
  new-api-cli auth login --token 1234abcd...

  # 用户名密码登录（密码会交互式询问，不进 shell 历史）
  new-api-cli auth login -u admin

  # 查看当前身份与额度
  new-api-cli auth status`,
	}
	cmd.AddCommand(
		newLoginCmd(f),
		newLogoutCmd(f),
		newStatusCmd(f),
		newWhoamiCmd(f),
		newTokenCmd(f),
		newListCmd(f),
	)
	return cmd
}

func newLoginCmd(f *cmdutil.Factory) *cobra.Command {
	var (
		token         string
		username      string
		password      string
		twoFACode     string
		generateToken bool
	)
	cmd := &cobra.Command{
		Use:   "login",
		Short: "登录并保存凭据",
		Long: `登录 New API 并把凭据保存到 ~/.new-api-cli/credentials.json（权限 0600）。

三种方式：
  --token <PAT>            直接保存系统访问令牌，不发起登录请求（推荐）
  -u <用户名>              交互式询问密码后登录
  -u <用户名> -p <密码>    完全非交互（注意密码会进入 shell 历史与进程列表）

登录成功后会调用 /api/user/self 校验令牌可用并记录身份。`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runLogin(cmdutil.Context(cmd), f, loginOptions{
				Token: token, Username: username, Password: password,
				TwoFACode: twoFACode, GenerateToken: generateToken,
			})
		},
	}
	cmd.Flags().StringVar(&token, "token", "", "系统访问令牌（在个人设置页生成）")
	cmd.Flags().StringVarP(&username, "username", "u", "", "登录用户名")
	cmd.Flags().StringVarP(&password, "password", "p", "", "登录密码（不传则交互式询问）")
	cmd.Flags().StringVar(&twoFACode, "code", "", "两步验证码（账号开启 2FA 时需要）")
	cmd.Flags().BoolVar(&generateToken, "generate-token", false,
		"登录后生成长期系统访问令牌并保存（会使该账号已有的系统访问令牌失效）")
	cmdutil.SetRisk(cmd, cmdutil.RiskWrite)
	return cmd
}

type loginOptions struct {
	Token         string
	Username      string
	Password      string
	TwoFACode     string
	GenerateToken bool
}

func runLogin(ctx context.Context, f *cmdutil.Factory, opts loginOptions) error {
	settings, err := f.Settings()
	if err != nil {
		return err
	}
	if err := settings.RequireBaseURL(); err != nil {
		return err
	}

	// --token 覆盖 login 自身的全局 --token，避免歧义。
	if opts.Token == "" && f.Globals.Token != "" {
		opts.Token = f.Globals.Token
	}

	var cred *cfg.Credential
	if opts.Token != "" {
		cred = &cfg.Credential{Kind: cfg.KindPAT, Token: opts.Token, BaseURL: settings.BaseURL}
	} else {
		cred, err = passwordLogin(ctx, f, settings, &opts)
		if err != nil {
			return err
		}
	}

	// 用新凭据验证一次身份：既确认令牌可用，也拿到用户名/角色存进本地。
	probe := *settings
	probe.Token = cred.Token
	self, err := fetchSelf(ctx, &probe)
	if err != nil {
		return err
	}
	applySelf(cred, self)

	if opts.GenerateToken && cred.Kind != cfg.KindPAT {
		if err := f.Confirm("生成新的系统访问令牌（该账号已有的系统访问令牌会立即失效）"); err != nil {
			return err
		}
		pat, err := generatePAT(ctx, &probe)
		if err != nil {
			return err
		}
		cred.Kind = cfg.KindPAT
		cred.Token = pat
		cred.RefreshToken = ""
		cred.ExpiresAt = 0
	}

	if err := cfg.SaveCredential(settings.Profile, cred); err != nil {
		return err
	}
	// 记住用户名，下次 login 可直接回填。
	if cred.Username != "" {
		if store, err := cfg.Load(); err == nil {
			if name, p, err := store.Resolve(f.Globals.Profile); err == nil && p != nil {
				p.Username = cred.Username
				store.Set(name, p)
				_ = store.Save()
			}
		}
	}

	data := map[string]any{
		"profile":  settings.Profile,
		"base_url": settings.BaseURL,
		"kind":     string(cred.Kind),
		"username": cred.Username,
		"user_id":  cred.UserID,
		"role":     cfg.RoleName(cred.Role),
		"token":    cfg.MaskToken(cred.Token),
	}
	msg := fmt.Sprintf("已以 %s（%s）登录 profile %q", cred.Username, cfg.RoleName(cred.Role), settings.Profile)
	if cred.ExpiresAt > 0 {
		data["expires_at"] = cred.ExpiresAt
		data["expires_at_human"] = time.Unix(cred.ExpiresAt, 0).Format(time.RFC3339)
		msg += fmt.Sprintf("；令牌在 %s 过期，届时需重新登录（加 --generate-token 可换成长期令牌）",
			time.Unix(cred.ExpiresAt, 0).Format("2006-01-02 15:04"))
	}
	return f.EmitResult(output.Result{Data: data, Message: msg})
}

// passwordLogin 走 /api/user/login，必要时补一次 2FA 校验。
func passwordLogin(ctx context.Context, f *cmdutil.Factory, settings *cfg.Settings, opts *loginOptions) (*cfg.Credential, error) {
	p := f.NewPrompter()

	username := opts.Username
	if username == "" {
		store, err := cfg.Load()
		if err == nil {
			if _, profile, err := store.Resolve(f.Globals.Profile); err == nil && profile != nil {
				username = profile.Username
			}
		}
		if p.Interactive() {
			username, err = p.Line("用户名", username)
			if err != nil {
				return nil, err
			}
		}
	}
	if username == "" {
		return nil, errs.NewValidationError(errs.SubtypeMissingArgument,
			"缺少登录凭据").
			WithHint("用 --token 保存系统访问令牌，或用 -u <用户名> 登录").
			WithParams("--token", "--username")
	}

	password := opts.Password
	if password == "" {
		if !p.Interactive() {
			return nil, errs.NewValidationError(errs.SubtypeMissingArgument,
				"非交互环境下必须提供密码").
				WithHint("传 -p <密码>，或改用 --token <系统访问令牌>（更安全）").
				WithParams("--password")
		}
		var err error
		password, err = p.Secret("密码")
		if err != nil {
			return nil, err
		}
	}

	anon := *settings
	anon.Token = ""
	c := client.New(&anon)
	c.Verbose = f.Globals.Verbose
	c.Log = f.IOStreams.Err

	resp, err := c.Do(ctx, client.Request{
		Method: http.MethodPost,
		Path:   "/api/user/login",
		Body:   map[string]string{"username": username, "password": password},
		NoAuth: true,
	})
	if err != nil {
		return nil, wrapLoginError(err)
	}

	var bundle authBundle
	if err := resp.Decode(&bundle); err != nil {
		return nil, err
	}

	if bundle.Require2FA {
		code := opts.TwoFACode
		if code == "" {
			if !p.Interactive() {
				return nil, errs.NewAuthError(errs.Subtype2FARequired,
					"该账号开启了两步验证").
					WithHint("加 --code <6 位验证码> 重试").
					WithParams("--code")
			}
			code, err = p.Line("两步验证码", "")
			if err != nil {
				return nil, err
			}
		}
		resp, err = c.Do(ctx, client.Request{
			Method: http.MethodPost,
			Path:   "/api/user/login/2fa",
			Body:   map[string]string{"code": code, "flow_token": bundle.FlowToken},
			NoAuth: true,
		})
		if err != nil {
			return nil, wrapLoginError(err)
		}
		bundle = authBundle{}
		if err := resp.Decode(&bundle); err != nil {
			return nil, err
		}
	}

	if bundle.AccessToken == "" {
		return nil, errs.NewAuthError(errs.SubtypeLoginFailed,
			"登录响应中没有 access_token").
			WithHint("确认站点版本支持该登录方式，或改用 --token 保存系统访问令牌")
	}

	cred := &cfg.Credential{
		Kind:      cfg.KindSession,
		Token:     bundle.AccessToken,
		ExpiresAt: bundle.AccessExpiresAt,
		BaseURL:   settings.BaseURL,
	}
	// refresh token 走 Set-Cookie 下发，存下来供后续续期。
	for _, ck := range readSetCookies(resp.Header) {
		if ck.Name == refreshCookieName {
			cred.RefreshToken = ck.Value
		}
	}
	return cred, nil
}

// refreshCookieName 与 new-api service.RefreshCookieName 保持一致。
const refreshCookieName = "new_api_refresh"

type authBundle struct {
	AccessToken     string `json:"access_token"`
	TokenType       string `json:"token_type"`
	AccessExpiresAt int64  `json:"access_expires_at"`
	Require2FA      bool   `json:"require_2fa"`
	FlowToken       string `json:"flow_token"`
	Session         struct {
		SID string `json:"sid"`
	} `json:"session"`
	User map[string]any `json:"user"`
}

func readSetCookies(h http.Header) []*http.Cookie {
	resp := http.Response{Header: h}
	return resp.Cookies()
}

func wrapLoginError(err error) error {
	typed, ok := errs.Unwrap(err)
	if !ok {
		return err
	}
	if typed.Type == errs.TypeAPI && typed.Subtype == "" {
		return errs.NewAuthError(errs.SubtypeLoginFailed, "%s", typed.Message).
			WithHint("确认用户名与密码；站点若关闭了密码登录，请改用 --token")
	}
	return err
}

// fetchSelf 拉取当前身份，同时充当令牌可用性检查。
func fetchSelf(ctx context.Context, settings *cfg.Settings) (map[string]any, error) {
	c := client.New(settings)
	resp, err := c.Do(ctx, client.Request{Method: http.MethodGet, Path: "/api/user/self"})
	if err != nil {
		return nil, err
	}
	var self map[string]any
	if err := resp.Decode(&self); err != nil {
		return nil, err
	}
	return self, nil
}

func applySelf(cred *cfg.Credential, self map[string]any) {
	if self == nil {
		return
	}
	if v, ok := self["username"].(string); ok {
		cred.Username = v
	}
	if v, ok := self["id"].(float64); ok {
		cred.UserID = int(v)
	}
	if v, ok := self["role"].(float64); ok {
		cred.Role = int(v)
	}
}

// generatePAT 调用 /api/user/token 生成新的系统访问令牌。
// 服务端会覆盖账号上的旧令牌，所以调用方必须先确认。
func generatePAT(ctx context.Context, settings *cfg.Settings) (string, error) {
	c := client.New(settings)
	resp, err := c.Do(ctx, client.Request{Method: http.MethodGet, Path: "/api/user/token"})
	if err != nil {
		return "", err
	}
	var pat string
	if err := resp.Decode(&pat); err != nil {
		return "", err
	}
	if pat == "" {
		return "", errs.NewAPIError(errs.SubtypeBadResponse, "服务端未返回系统访问令牌")
	}
	return pat, nil
}

func newLogoutCmd(f *cmdutil.Factory) *cobra.Command {
	var allProfiles bool
	cmd := &cobra.Command{
		Use:   "logout",
		Short: "删除本地保存的登录凭据",
		Long: `删除本地保存的凭据。

密码登录产生的会话会同时通知服务端注销；系统访问令牌只删除本地副本，
令牌本身仍然有效（需要作废请到 New API 个人设置页重新生成）。`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmdutil.Context(cmd)
			if allProfiles {
				creds, err := cfg.ListCredentials()
				if err != nil {
					return err
				}
				names := make([]string, 0, len(creds))
				for name := range creds {
					names = append(names, name)
				}
				sort.Strings(names)
				if len(names) == 0 {
					return f.EmitResult(output.Result{
						Data: map[string]any{"removed": []string{}}, Message: "本地没有任何登录态"})
				}
				if err := f.Confirm(fmt.Sprintf("退出全部 %d 个 profile 的登录", len(names))); err != nil {
					return err
				}
				for _, name := range names {
					_ = cfg.DeleteCredential(name)
				}
				return f.EmitResult(output.Result{
					Data:    map[string]any{"removed": names},
					Message: fmt.Sprintf("已退出 %d 个 profile", len(names)),
				})
			}

			settings, err := f.Settings()
			if err != nil {
				return err
			}
			cred, err := cfg.LoadCredential(settings.Profile)
			if err != nil {
				return err
			}
			if cred == nil {
				return f.EmitResult(output.Result{
					Data:    map[string]any{"profile": settings.Profile, "removed": false},
					Message: "该 profile 本来就未登录",
				})
			}
			if cred.Kind == cfg.KindSession && cred.RefreshToken != "" {
				revokeSession(ctx, settings, cred)
			}
			if err := cfg.DeleteCredential(settings.Profile); err != nil {
				return err
			}
			return f.EmitResult(output.Result{
				Data:    map[string]any{"profile": settings.Profile, "removed": true},
				Message: fmt.Sprintf("已退出 profile %q", settings.Profile),
			})
		},
	}
	cmd.Flags().BoolVar(&allProfiles, "all", false, "退出全部 profile 的登录")
	cmdutil.SetRisk(cmd, cmdutil.RiskWrite)
	return cmd
}

// revokeSession 尽力通知服务端注销会话；失败不影响本地清理。
func revokeSession(ctx context.Context, settings *cfg.Settings, cred *cfg.Credential) {
	c := client.New(settings)
	_, _ = c.Do(ctx, client.Request{
		Method:  http.MethodPost,
		Path:    "/api/user/auth/logout",
		Cookies: []*http.Cookie{{Name: refreshCookieName, Value: cred.RefreshToken}},
	})
}

func newStatusCmd(f *cmdutil.Factory) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "status",
		Short: "查看当前登录状态与账号信息",
		Long: `校验当前凭据并展示账号信息：用户名、角色、额度、已用额度、分组。

退出码可直接用于脚本判断：0 表示凭据有效，4 表示未登录或令牌失效。`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			settings, err := f.Settings()
			if err != nil {
				return err
			}
			if err := settings.RequireBaseURL(); err != nil {
				return err
			}
			if err := settings.RequireToken(); err != nil {
				return err
			}
			self, err := fetchSelf(cmdutil.Context(cmd), settings)
			if err != nil {
				return err
			}
			cred, _ := cfg.LoadCredential(settings.Profile)

			data := map[string]any{
				"profile":      settings.Profile,
				"base_url":     settings.BaseURL,
				"token_source": settings.TokenSource,
				"username":     self["username"],
				"user_id":      self["id"],
				"display_name": self["display_name"],
				"email":        self["email"],
				"group":        self["group"],
				"quota":        self["quota"],
				"used_quota":   self["used_quota"],
				"request_count": self["request_count"],
			}
			if role, ok := self["role"].(float64); ok {
				data["role"] = int(role)
				data["role_name"] = cfg.RoleName(int(role))
			}
			if cred != nil {
				data["kind"] = string(cred.Kind)
				if cred.ExpiresAt > 0 {
					data["expires_at"] = cred.ExpiresAt
					data["expires_at_human"] = time.Unix(cred.ExpiresAt, 0).Format(time.RFC3339)
					data["expired"] = cred.Expired()
				}
			}
			msg := fmt.Sprintf("已登录：%v（%s）@ %s", self["username"], data["role_name"], settings.BaseURL)
			return f.EmitResult(output.Result{Data: data, Message: msg})
		},
	}
	cmdutil.SetRisk(cmd, cmdutil.RiskRead)
	return cmd
}

func newWhoamiCmd(f *cmdutil.Factory) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "whoami",
		Short: "只输出当前用户名（便于脚本取值）",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			settings, err := f.Settings()
			if err != nil {
				return err
			}
			if err := settings.RequireToken(); err != nil {
				return err
			}
			self, err := fetchSelf(cmdutil.Context(cmd), settings)
			if err != nil {
				return err
			}
			return f.EmitData(map[string]any{
				"username": self["username"],
				"user_id":  self["id"],
				"role":     self["role"],
			})
		},
	}
	cmdutil.SetRisk(cmd, cmdutil.RiskRead)
	return cmd
}

func newTokenCmd(f *cmdutil.Factory) *cobra.Command {
	var save bool
	cmd := &cobra.Command{
		Use:   "token",
		Short: "生成新的系统访问令牌（管理后台令牌）",
		Long: `调用 /api/user/token 生成新的系统访问令牌。

服务端每个账号只保留一个系统访问令牌，生成新的会立即让旧令牌失效 ——
包括其他机器、其他 CI 上正在使用的那一份。

需要的是给客户端调用模型用的 sk- 令牌？那是 new-api-cli token create。`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			settings, err := f.Settings()
			if err != nil {
				return err
			}
			if err := settings.RequireToken(); err != nil {
				return err
			}
			if err := f.Confirm("生成新的系统访问令牌（该账号已有的系统访问令牌会立即失效）"); err != nil {
				return err
			}
			pat, err := generatePAT(cmdutil.Context(cmd), settings)
			if err != nil {
				return err
			}
			data := map[string]any{"token": pat}
			msg := "已生成新的系统访问令牌，旧令牌已失效"
			if save {
				cred, _ := cfg.LoadCredential(settings.Profile)
				if cred == nil {
					cred = &cfg.Credential{}
				}
				cred.Kind = cfg.KindPAT
				cred.Token = pat
				cred.RefreshToken = ""
				cred.ExpiresAt = 0
				cred.BaseURL = settings.BaseURL
				if err := cfg.SaveCredential(settings.Profile, cred); err != nil {
					return err
				}
				data["saved_to_profile"] = settings.Profile
				msg += fmt.Sprintf("，并已保存到 profile %q", settings.Profile)
			}
			return f.EmitResult(output.Result{Data: data, Message: msg})
		},
	}
	cmd.Flags().BoolVar(&save, "save", true, "同时保存到当前 profile")
	cmdutil.SetRisk(cmd, cmdutil.RiskHighRisk)
	return cmd
}

func newListCmd(f *cmdutil.Factory) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "列出各 profile 的登录态（不含令牌明文）",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			creds, err := cfg.ListCredentials()
			if err != nil {
				return err
			}
			store, err := cfg.Load()
			if err != nil {
				return err
			}
			names := make([]string, 0, len(creds))
			for name := range creds {
				names = append(names, name)
			}
			sort.Strings(names)

			rows := make([]any, 0, len(names))
			for _, name := range names {
				c := creds[name]
				row := map[string]any{
					"profile":  name,
					"current":  name == store.Current,
					"kind":     string(c.Kind),
					"username": c.Username,
					"role":     cfg.RoleName(c.Role),
					"base_url": c.BaseURL,
					"token":    cfg.MaskToken(c.Token),
				}
				if c.ExpiresAt > 0 {
					row["expires_at"] = time.Unix(c.ExpiresAt, 0).Format(time.RFC3339)
					row["expired"] = c.Expired()
				}
				rows = append(rows, row)
			}
			return f.EmitResult(output.Result{
				Data:    rows,
				Meta:    &output.Meta{Count: len(rows)},
				Columns: []string{"profile", "current", "kind", "username", "role", "base_url", "expired"},
			})
		},
	}
	cmdutil.SetRisk(cmd, cmdutil.RiskRead)
	return cmd
}
