package model

import "time"

// Passkey 管理员通行密钥（WebAuthn 凭据）。
// 凭据按注册时的域名（RPID）隔离，只能在同一域名下使用；
// 全站所有通行密钥共用同一个 WebAuthn 用户句柄（UserHandle），登录身份以 UserLogin 为准。
type Passkey struct {
	Common
	Name         string     `json:"name"`                    // 用户自定义名称
	UserLogin    string     `gorm:"index" json:"user_login"` // 注册时的管理员登录名，通行密钥登录后的会话身份
	UserHandle   string     `json:"-"`                       // WebAuthn 用户句柄（base64url）
	CredentialID string     `gorm:"uniqueIndex" json:"-"`    // 凭据 ID（base64url）
	RPID         string     `gorm:"index" json:"rp_id"`      // 注册时的域名
	Credential   string     `json:"-"`                       // webauthn.Credential 的 JSON，含公钥与签名计数
	LastUsedAt   *time.Time `json:"last_used_at,omitempty"`  // 最近一次验证成功的时间
}
