// Package config 管理 new-api-cli 的配置与凭据。
//
// 磁盘布局（可用 NEW_API_CLI_HOME 改写根目录）：
//
//	~/.new-api-cli/config.yaml       多 profile 的连接配置（明文，可入版本库外备份）
//	~/.new-api-cli/credentials.json  令牌，权限 0600，永不打印
//
// 分成两个文件是刻意的：配置可以分享、贴进工单，凭据不行。
package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/huangxin8899/new-api-cli/errs"
	"gopkg.in/yaml.v3"
)

// 环境变量：无需写配置文件即可驱动 CLI，适合 CI 与容器。
const (
	EnvHome    = "NEW_API_CLI_HOME"
	EnvBaseURL = "NEW_API_BASE_URL"
	EnvToken   = "NEW_API_TOKEN"
	EnvProfile = "NEW_API_PROFILE"
	EnvUser    = "NEW_API_USER_ID"
)

// DefaultProfile 是未指定 profile 时使用的名字。
const DefaultProfile = "default"

// Profile 是一套指向某个 New API 实例的连接配置。
type Profile struct {
	// BaseURL 是 New API 站点根地址，例如 https://api.example.com。
	BaseURL string `yaml:"base_url"`
	// Username 仅用于 `auth login` 时回填，不含密码。
	Username string `yaml:"username,omitempty"`
	// Insecure 跳过 TLS 证书校验（自签名证书的私有部署）。
	Insecure bool `yaml:"insecure,omitempty"`
	// Timeout 单次请求超时秒数，0 表示用默认值。
	Timeout int `yaml:"timeout,omitempty"`
	// UserID 对应 New-API-User 请求头，管理员代表其他用户操作时使用。
	UserID int `yaml:"user_id,omitempty"`
}

// Config 是 config.yaml 的完整内容。
type Config struct {
	Current  string              `yaml:"current"`
	Profiles map[string]*Profile `yaml:"profiles"`

	path string
}

// Home 返回配置根目录，必要时创建。
func Home() (string, error) {
	if custom := os.Getenv(EnvHome); custom != "" {
		return custom, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", errs.NewConfigError(errs.SubtypeConfigCorrupt,
			"无法定位用户主目录: %v", err).
			WithHint("设置 %s 指定配置目录", EnvHome)
	}
	return filepath.Join(home, ".new-api-cli"), nil
}

// ConfigPath 返回 config.yaml 的绝对路径。
func ConfigPath() (string, error) {
	home, err := Home()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, "config.yaml"), nil
}

// Load 读取配置；文件不存在时返回一份空配置而非报错，
// 这样 `config init` 与纯环境变量用法都能跑。
func Load() (*Config, error) {
	path, err := ConfigPath()
	if err != nil {
		return nil, err
	}
	cfg := &Config{Current: DefaultProfile, Profiles: map[string]*Profile{}, path: path}

	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return cfg, nil
	}
	if err != nil {
		return nil, errs.NewConfigError(errs.SubtypeConfigCorrupt,
			"读取配置文件失败 %s: %v", path, err)
	}
	if err := yaml.Unmarshal(raw, cfg); err != nil {
		return nil, errs.NewConfigError(errs.SubtypeConfigCorrupt,
			"解析配置文件失败 %s: %v", path, err).
			WithHint("修复该 YAML 文件，或删除后重新执行 new-api-cli config init")
	}
	if cfg.Profiles == nil {
		cfg.Profiles = map[string]*Profile{}
	}
	if cfg.Current == "" {
		cfg.Current = DefaultProfile
	}
	cfg.path = path
	return cfg, nil
}

// Save 原子写回配置文件。
func (c *Config) Save() error {
	if c.path == "" {
		path, err := ConfigPath()
		if err != nil {
			return err
		}
		c.path = path
	}
	if err := os.MkdirAll(filepath.Dir(c.path), 0o700); err != nil {
		return errs.NewConfigError(errs.SubtypeConfigCorrupt,
			"创建配置目录失败: %v", err)
	}
	raw, err := yaml.Marshal(c)
	if err != nil {
		return errs.NewInternalError("序列化配置失败: %v", err)
	}
	return atomicWrite(c.path, raw, 0o600)
}

// Path 返回配置文件路径（可能尚未创建）。
func (c *Config) Path() string { return c.path }

// Resolve 按 profile 名取配置。传空则用 current（再被 NEW_API_PROFILE 覆盖）。
func (c *Config) Resolve(name string) (string, *Profile, error) {
	if name == "" {
		name = os.Getenv(EnvProfile)
	}
	if name == "" {
		name = c.Current
	}
	if name == "" {
		name = DefaultProfile
	}
	p, ok := c.Profiles[name]
	if !ok {
		// 环境变量足够独立驱动一次调用时，允许无配置文件运行。
		if os.Getenv(EnvBaseURL) != "" {
			return name, &Profile{}, nil
		}
		if len(c.Profiles) == 0 {
			return name, nil, errs.NewConfigError(errs.SubtypeNotConfigured,
				"尚未配置任何 New API 实例").
				WithHint("先执行 new-api-cli config init，或设置环境变量 %s", EnvBaseURL)
		}
		return name, nil, errs.NewConfigError(errs.SubtypeProfileNotFound,
			"profile %q 不存在", name).
			WithHint("已有 profile：%s；用 new-api-cli config use <name> 切换", strings.Join(c.ProfileNames(), ", ")).
			WithParams("--profile")
	}
	return name, p, nil
}

// ProfileNames 返回全部 profile 名（已排序由调用方保证不需要）。
func (c *Config) ProfileNames() []string {
	names := make([]string, 0, len(c.Profiles))
	for name := range c.Profiles {
		names = append(names, name)
	}
	return names
}

// Set 新增或覆盖一个 profile。
func (c *Config) Set(name string, p *Profile) {
	if c.Profiles == nil {
		c.Profiles = map[string]*Profile{}
	}
	c.Profiles[name] = p
}

// Remove 删除 profile 及其凭据。
func (c *Config) Remove(name string) error {
	if _, ok := c.Profiles[name]; !ok {
		return errs.NewConfigError(errs.SubtypeProfileNotFound, "profile %q 不存在", name)
	}
	delete(c.Profiles, name)
	if c.Current == name {
		c.Current = DefaultProfile
		for other := range c.Profiles {
			c.Current = other
			break
		}
	}
	if err := c.Save(); err != nil {
		return err
	}
	return DeleteCredential(name)
}

// Settings 是 flag、环境变量、配置文件三方合并后的最终连接参数。
type Settings struct {
	Profile  string
	BaseURL  string
	Token    string
	Insecure bool
	Timeout  int
	UserID   int
	// TokenSource 说明令牌来自哪里，出错时提示更精准。
	TokenSource string
}

// Overrides 是命令行 flag 传入的临时覆盖值。
type Overrides struct {
	Profile  string
	BaseURL  string
	Token    string
	Insecure bool
	Timeout  int
	UserID   int
}

// ResolveSettings 按 flag > 环境变量 > 配置文件 的优先级合并出最终配置。
// 只解析连接参数，不校验是否已登录 —— 那是 client 在真正发请求时的事。
func ResolveSettings(o Overrides) (*Settings, error) {
	cfg, err := Load()
	if err != nil {
		return nil, err
	}
	name, profile, err := cfg.Resolve(o.Profile)
	if err != nil {
		// 显式给了 base-url 就不需要 profile。
		if o.BaseURL == "" && os.Getenv(EnvBaseURL) == "" {
			return nil, err
		}
		profile = &Profile{}
	}

	s := &Settings{Profile: name}

	s.BaseURL = firstNonEmpty(o.BaseURL, os.Getenv(EnvBaseURL), profile.BaseURL)
	s.BaseURL = NormalizeBaseURL(s.BaseURL)

	s.Insecure = o.Insecure || profile.Insecure
	s.Timeout = firstPositive(o.Timeout, profile.Timeout, 60)
	s.UserID = firstPositive(o.UserID, envInt(EnvUser), profile.UserID)

	switch {
	case o.Token != "":
		s.Token, s.TokenSource = o.Token, "--token"
	case os.Getenv(EnvToken) != "":
		s.Token, s.TokenSource = os.Getenv(EnvToken), EnvToken
	default:
		cred, err := LoadCredential(name)
		if err != nil {
			return nil, err
		}
		if cred != nil {
			s.Token, s.TokenSource = cred.Token, "credentials.json"
		}
	}
	return s, nil
}

// NormalizeBaseURL 去掉末尾斜杠与误粘的 /api 后缀。
func NormalizeBaseURL(raw string) string {
	u := strings.TrimSpace(raw)
	u = strings.TrimRight(u, "/")
	u = strings.TrimSuffix(u, "/api")
	return u
}

// RequireBaseURL 在缺少站点地址时给出带修复建议的错误。
func (s *Settings) RequireBaseURL() error {
	if s.BaseURL != "" {
		return nil
	}
	return errs.NewConfigError(errs.SubtypeNotConfigured,
		"未设置 New API 站点地址").
		WithHint("执行 new-api-cli config init，或传 --base-url https://your-site.com，或设置 %s", EnvBaseURL).
		WithParams("--base-url")
}

// RequireToken 在缺少令牌时给出带修复建议的错误。
func (s *Settings) RequireToken() error {
	if s.Token != "" {
		return nil
	}
	return errs.NewAuthError(errs.SubtypeNotLoggedIn,
		"当前 profile %q 尚未登录", s.Profile).
		WithHint("执行 new-api-cli auth login，或传 --token <系统访问令牌>，或设置 %s", EnvToken)
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func firstPositive(values ...int) int {
	for _, v := range values {
		if v > 0 {
			return v
		}
	}
	return 0
}

func envInt(key string) int {
	v, err := strconv.Atoi(os.Getenv(key))
	if err != nil {
		return 0
	}
	return v
}

// atomicWrite 先写临时文件再 rename，避免写坏原文件。
func atomicWrite(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return errs.NewConfigError(errs.SubtypeConfigCorrupt, "创建临时文件失败: %v", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return errs.NewConfigError(errs.SubtypeConfigCorrupt, "写入临时文件失败: %v", err)
	}
	if err := tmp.Close(); err != nil {
		return errs.NewConfigError(errs.SubtypeConfigCorrupt, "关闭临时文件失败: %v", err)
	}
	if err := os.Chmod(tmpName, perm); err != nil {
		return errs.NewConfigError(errs.SubtypeConfigCorrupt, "设置文件权限失败: %v", err)
	}
	// Windows 上 rename 到已存在的文件会失败，先删除。
	_ = os.Remove(path)
	if err := os.Rename(tmpName, path); err != nil {
		return errs.NewConfigError(errs.SubtypeConfigCorrupt, "写入 %s 失败: %v", path, err)
	}
	return nil
}

// MaskToken 用于日志与 status 输出，永远不回显完整令牌。
func MaskToken(token string) string {
	n := len(token)
	if n == 0 {
		return ""
	}
	if n <= 8 {
		return strings.Repeat("*", n)
	}
	return fmt.Sprintf("%s...%s", token[:4], token[n-4:])
}
