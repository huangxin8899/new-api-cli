package config

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"

	"github.com/huangxin8899/new-api-cli/errs"
)

// CredentialKind 区分令牌的来源，决定过期与刷新策略。
type CredentialKind string

const (
	// KindPAT 是「系统访问令牌」，在 New API 个人设置页或 `auth token` 生成，
	// 长期有效，是 CLI 与 CI 的推荐凭据。
	KindPAT CredentialKind = "pat"
	// KindSession 是密码登录换来的 dashboard access token，有效期短，
	// 可用 refresh token 续期。
	KindSession CredentialKind = "session"
)

// Credential 是单个 profile 的登录态。
type Credential struct {
	Kind CredentialKind `json:"kind"`
	// Token 写入 Authorization: Bearer <token>。
	Token string `json:"token"`
	// RefreshToken 仅 session 类型有，用于续期。
	RefreshToken string `json:"refresh_token,omitempty"`
	// ExpiresAt 是 Token 的过期时间戳（秒）；0 表示不过期。
	ExpiresAt int64 `json:"expires_at,omitempty"`
	// 登录时快照的身份信息，供 auth status 免网展示。
	UserID   int    `json:"user_id,omitempty"`
	Username string `json:"username,omitempty"`
	Role     int    `json:"role,omitempty"`
	BaseURL  string `json:"base_url,omitempty"`
	SavedAt  int64  `json:"saved_at,omitempty"`
}

// Expired 判断 access token 是否已过期（留 30 秒余量）。
func (c *Credential) Expired() bool {
	if c == nil || c.ExpiresAt == 0 {
		return false
	}
	return time.Now().Unix() >= c.ExpiresAt-30
}

// CredentialsPath 返回凭据文件路径。
func CredentialsPath() (string, error) {
	home, err := Home()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, "credentials.json"), nil
}

type credentialStore map[string]*Credential

func loadStore() (credentialStore, error) {
	path, err := CredentialsPath()
	if err != nil {
		return nil, err
	}
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return credentialStore{}, nil
	}
	if err != nil {
		return nil, errs.NewAuthError(errs.SubtypeCredentialStore,
			"读取凭据文件失败 %s: %v", path, err)
	}
	store := credentialStore{}
	if err := json.Unmarshal(raw, &store); err != nil {
		return nil, errs.NewAuthError(errs.SubtypeCredentialStore,
			"凭据文件已损坏 %s: %v", path, err).
			WithHint("删除该文件后重新执行 new-api-cli auth login")
	}
	return store, nil
}

func saveStore(store credentialStore) error {
	path, err := CredentialsPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return errs.NewAuthError(errs.SubtypeCredentialStore, "创建配置目录失败: %v", err)
	}
	raw, err := json.MarshalIndent(store, "", "  ")
	if err != nil {
		return errs.NewInternalError("序列化凭据失败: %v", err)
	}
	// 0600：同机其他用户读不到令牌。
	return atomicWrite(path, raw, 0o600)
}

// LoadCredential 读取某个 profile 的凭据；不存在返回 (nil, nil)。
func LoadCredential(profile string) (*Credential, error) {
	store, err := loadStore()
	if err != nil {
		return nil, err
	}
	return store[profile], nil
}

// SaveCredential 写入某个 profile 的凭据。
func SaveCredential(profile string, cred *Credential) error {
	store, err := loadStore()
	if err != nil {
		return err
	}
	cred.SavedAt = time.Now().Unix()
	store[profile] = cred
	return saveStore(store)
}

// DeleteCredential 删除某个 profile 的凭据；不存在也算成功。
func DeleteCredential(profile string) error {
	store, err := loadStore()
	if err != nil {
		return err
	}
	if _, ok := store[profile]; !ok {
		return nil
	}
	delete(store, profile)
	return saveStore(store)
}

// ListCredentials 返回全部已保存的登录态，用于 auth list。
func ListCredentials() (map[string]*Credential, error) {
	store, err := loadStore()
	if err != nil {
		return nil, err
	}
	return store, nil
}

// RoleName 把 New API 的角色数值翻译成人类可读名称。
// 数值取自 new-api common/constants.go（1/10/100）。
func RoleName(role int) string {
	switch {
	case role >= 100:
		return "root"
	case role >= 10:
		return "admin"
	case role >= 1:
		return "user"
	default:
		return "guest"
	}
}
