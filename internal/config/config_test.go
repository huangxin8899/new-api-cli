package config

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/huangxin8899/new-api-cli/errs"
)

// isolate 把配置根目录指向临时目录，并清空所有会影响解析的环境变量，
// 让每个用例都从干净状态开始。
func isolate(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv(EnvHome, dir)
	for _, key := range []string{EnvBaseURL, EnvToken, EnvProfile, EnvUser} {
		t.Setenv(key, "")
		os.Unsetenv(key)
	}
	return dir
}

func TestLoadMissingFileYieldsEmptyConfig(t *testing.T) {
	isolate(t)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Current != DefaultProfile {
		t.Errorf("Current = %q, 期望 %q", cfg.Current, DefaultProfile)
	}
	if len(cfg.Profiles) != 0 {
		t.Errorf("期望空 profiles, 得到 %v", cfg.Profiles)
	}
}

func TestSaveThenLoadRoundTrips(t *testing.T) {
	isolate(t)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	cfg.Set("prod", &Profile{BaseURL: "https://prod.example.com", Timeout: 30, UserID: 7})
	cfg.Current = "prod"
	if err := cfg.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}

	reloaded, err := Load()
	if err != nil {
		t.Fatalf("重新 Load: %v", err)
	}
	if reloaded.Current != "prod" {
		t.Errorf("Current = %q", reloaded.Current)
	}
	p := reloaded.Profiles["prod"]
	if p == nil {
		t.Fatal("prod profile 丢失")
	}
	if p.BaseURL != "https://prod.example.com" || p.Timeout != 30 || p.UserID != 7 {
		t.Errorf("profile 未原样往返: %+v", p)
	}
}

func TestConfigFilePermissionsAreOwnerOnly(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows 不使用 POSIX 权限位")
	}
	dir := isolate(t)

	cfg, _ := Load()
	cfg.Set("default", &Profile{BaseURL: "https://a.example.com"})
	if err := cfg.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}
	info, err := os.Stat(filepath.Join(dir, "config.yaml"))
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("config.yaml 权限 = %o, 期望 600", perm)
	}
}

func TestCredentialFilePermissionsAreOwnerOnly(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows 不使用 POSIX 权限位")
	}
	dir := isolate(t)

	if err := SaveCredential("default", &Credential{Kind: KindPAT, Token: "secret-token"}); err != nil {
		t.Fatalf("SaveCredential: %v", err)
	}
	info, err := os.Stat(filepath.Join(dir, "credentials.json"))
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	// 令牌等同密码，必须 0600。
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("credentials.json 权限 = %o, 期望 600", perm)
	}
}

func TestCredentialRoundTripAndDelete(t *testing.T) {
	isolate(t)

	if got, err := LoadCredential("default"); err != nil || got != nil {
		t.Fatalf("空存储应返回 (nil, nil)，得到 (%v, %v)", got, err)
	}

	want := &Credential{Kind: KindPAT, Token: "tok-abc", UserID: 3, Username: "root", Role: 100}
	if err := SaveCredential("default", want); err != nil {
		t.Fatalf("SaveCredential: %v", err)
	}
	got, err := LoadCredential("default")
	if err != nil {
		t.Fatalf("LoadCredential: %v", err)
	}
	if got.Token != "tok-abc" || got.Username != "root" || got.Role != 100 {
		t.Errorf("凭据未原样往返: %+v", got)
	}
	if got.SavedAt == 0 {
		t.Error("SavedAt 应被自动填充")
	}

	if err := DeleteCredential("default"); err != nil {
		t.Fatalf("DeleteCredential: %v", err)
	}
	if got, _ := LoadCredential("default"); got != nil {
		t.Errorf("删除后仍能读到: %+v", got)
	}
	// 删除不存在的凭据是幂等的。
	if err := DeleteCredential("nope"); err != nil {
		t.Errorf("删除不存在的凭据应成功: %v", err)
	}
}

func TestCredentialsStoreIsolatesProfiles(t *testing.T) {
	isolate(t)

	if err := SaveCredential("prod", &Credential{Kind: KindPAT, Token: "prod-tok"}); err != nil {
		t.Fatalf("SaveCredential prod: %v", err)
	}
	if err := SaveCredential("staging", &Credential{Kind: KindPAT, Token: "staging-tok"}); err != nil {
		t.Fatalf("SaveCredential staging: %v", err)
	}
	// 写第二个 profile 不能覆盖第一个。
	prod, _ := LoadCredential("prod")
	if prod == nil || prod.Token != "prod-tok" {
		t.Errorf("prod 凭据被覆盖: %+v", prod)
	}
	staging, _ := LoadCredential("staging")
	if staging == nil || staging.Token != "staging-tok" {
		t.Errorf("staging 凭据错误: %+v", staging)
	}
}

func TestResolveSettingsPrecedence(t *testing.T) {
	isolate(t)

	cfg, _ := Load()
	cfg.Set("default", &Profile{BaseURL: "https://file.example.com", Timeout: 11, UserID: 1})
	cfg.Current = "default"
	if err := cfg.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if err := SaveCredential("default", &Credential{Kind: KindPAT, Token: "file-token"}); err != nil {
		t.Fatalf("SaveCredential: %v", err)
	}

	t.Run("仅配置文件", func(t *testing.T) {
		s, err := ResolveSettings(Overrides{})
		if err != nil {
			t.Fatalf("ResolveSettings: %v", err)
		}
		if s.BaseURL != "https://file.example.com" {
			t.Errorf("BaseURL = %q", s.BaseURL)
		}
		if s.Token != "file-token" {
			t.Errorf("Token = %q", s.Token)
		}
		if s.TokenSource != "credentials.json" {
			t.Errorf("TokenSource = %q", s.TokenSource)
		}
		if s.Timeout != 11 {
			t.Errorf("Timeout = %d", s.Timeout)
		}
	})

	t.Run("环境变量覆盖配置文件", func(t *testing.T) {
		t.Setenv(EnvBaseURL, "https://env.example.com")
		t.Setenv(EnvToken, "env-token")
		s, err := ResolveSettings(Overrides{})
		if err != nil {
			t.Fatalf("ResolveSettings: %v", err)
		}
		if s.BaseURL != "https://env.example.com" {
			t.Errorf("BaseURL = %q, 环境变量应胜过配置文件", s.BaseURL)
		}
		if s.Token != "env-token" || s.TokenSource != EnvToken {
			t.Errorf("Token = %q (来源 %q)", s.Token, s.TokenSource)
		}
	})

	t.Run("flag 覆盖环境变量", func(t *testing.T) {
		t.Setenv(EnvBaseURL, "https://env.example.com")
		t.Setenv(EnvToken, "env-token")
		s, err := ResolveSettings(Overrides{BaseURL: "https://flag.example.com", Token: "flag-token", Timeout: 99})
		if err != nil {
			t.Fatalf("ResolveSettings: %v", err)
		}
		if s.BaseURL != "https://flag.example.com" {
			t.Errorf("BaseURL = %q, flag 应胜过环境变量", s.BaseURL)
		}
		if s.Token != "flag-token" || s.TokenSource != "--token" {
			t.Errorf("Token = %q (来源 %q)", s.Token, s.TokenSource)
		}
		if s.Timeout != 99 {
			t.Errorf("Timeout = %d", s.Timeout)
		}
	})
}

func TestResolveSettingsWorksFromEnvAloneWithNoConfigFile(t *testing.T) {
	isolate(t)
	t.Setenv(EnvBaseURL, "https://only-env.example.com")
	t.Setenv(EnvToken, "only-env-token")

	// 纯环境变量驱动（容器 / CI）不应要求先跑 config init。
	s, err := ResolveSettings(Overrides{})
	if err != nil {
		t.Fatalf("ResolveSettings: %v", err)
	}
	if s.BaseURL != "https://only-env.example.com" || s.Token != "only-env-token" {
		t.Errorf("环境变量单独驱动失败: %+v", s)
	}
}

func TestResolveSettingsUnconfiguredIsTypedConfigError(t *testing.T) {
	isolate(t)

	_, err := ResolveSettings(Overrides{})
	if err == nil {
		t.Fatal("未配置时应报错")
	}
	typed, ok := errs.Unwrap(err)
	if !ok {
		t.Fatalf("应为类型化错误, 得到 %T", err)
	}
	if typed.Type != errs.TypeConfig || typed.Subtype != errs.SubtypeNotConfigured {
		t.Errorf("错误分类 = %s/%s", typed.Type, typed.Subtype)
	}
	if errs.ExitCodeOf(err) != errs.ExitConfig {
		t.Errorf("退出码 = %d, 期望 %d", errs.ExitCodeOf(err), errs.ExitConfig)
	}
}

func TestResolveSettingsUnknownProfileIsTypedError(t *testing.T) {
	isolate(t)

	cfg, _ := Load()
	cfg.Set("default", &Profile{BaseURL: "https://a.example.com"})
	if err := cfg.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}

	_, err := ResolveSettings(Overrides{Profile: "ghost"})
	if err == nil {
		t.Fatal("不存在的 profile 应报错")
	}
	typed, ok := errs.Unwrap(err)
	if !ok {
		t.Fatalf("应为类型化错误, 得到 %T", err)
	}
	if typed.Subtype != errs.SubtypeProfileNotFound {
		t.Errorf("subtype = %q, 期望 %q", typed.Subtype, errs.SubtypeProfileNotFound)
	}
}

func TestNormalizeBaseURLStripsTrailingSlashAndAPISuffix(t *testing.T) {
	cases := map[string]string{
		"https://a.example.com":      "https://a.example.com",
		"https://a.example.com/":     "https://a.example.com",
		"https://a.example.com/api":  "https://a.example.com",
		"https://a.example.com/api/": "https://a.example.com",
		"  https://a.example.com  ":  "https://a.example.com",
		"https://a.example.com:3000": "https://a.example.com:3000",
	}
	for in, want := range cases {
		if got := NormalizeBaseURL(in); got != want {
			t.Errorf("NormalizeBaseURL(%q) = %q, 期望 %q", in, got, want)
		}
	}
}

func TestRequireBaseURLAndTokenAreTypedErrors(t *testing.T) {
	s := &Settings{Profile: "default"}

	err := s.RequireBaseURL()
	if typed, ok := errs.Unwrap(err); !ok || typed.Subtype != errs.SubtypeNotConfigured {
		t.Errorf("RequireBaseURL 错误分类不符: %v", err)
	}

	err = s.RequireToken()
	typed, ok := errs.Unwrap(err)
	if !ok || typed.Subtype != errs.SubtypeNotLoggedIn {
		t.Errorf("RequireToken 错误分类不符: %v", err)
	}
	if errs.ExitCodeOf(err) != errs.ExitAuth {
		t.Errorf("未登录退出码 = %d, 期望 %d", errs.ExitCodeOf(err), errs.ExitAuth)
	}

	s.BaseURL = "https://a.example.com"
	s.Token = "tok"
	if err := s.RequireBaseURL(); err != nil {
		t.Errorf("配好后 RequireBaseURL 不应报错: %v", err)
	}
	if err := s.RequireToken(); err != nil {
		t.Errorf("配好后 RequireToken 不应报错: %v", err)
	}
}

func TestMaskTokenNeverRevealsMiddle(t *testing.T) {
	cases := []struct{ in, want string }{
		{"", ""},
		{"abc", "***"},
		{"sk-1234567890abcdef", "sk-1***********cdef"},
	}
	for _, c := range cases {
		got := MaskToken(c.in)
		if c.want != "" && got == c.in && len(c.in) > 4 {
			t.Errorf("MaskToken(%q) 未做掩码: %q", c.in, got)
		}
		if len(c.in) > 8 && (len(got) == 0 || got == c.in) {
			t.Errorf("MaskToken(%q) = %q", c.in, got)
		}
	}
	// 关键性质：掩码结果不得包含原串的中段。
	full := "sk-abcdefghijklmnopqrstuvwxyz"
	if masked := MaskToken(full); strings.Contains(masked, "defghijklmnopqrstuv") {
		t.Errorf("MaskToken 泄露了中段: %q", masked)
	}
}

func TestExpiredHonoursSkew(t *testing.T) {
	var none *Credential
	if none.Expired() {
		t.Error("nil 凭据不应判定为过期")
	}
	if (&Credential{ExpiresAt: 0}).Expired() {
		t.Error("ExpiresAt=0 表示不过期")
	}
	if !(&Credential{ExpiresAt: 1}).Expired() {
		t.Error("远古时间戳应判定为过期")
	}
}
