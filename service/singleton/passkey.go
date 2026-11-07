package singleton

import (
	"log"
	"sync/atomic"

	"github.com/railzen/nezha-zero/model"
)

// passkeyCount 已注册的通行密钥数量缓存，供页面判断是否展示通行密钥入口，避免频繁查库。
var passkeyCount atomic.Int64

// RefreshPasskeyCount 从数据库重新统计通行密钥数量。查询失败时保留原值。
func RefreshPasskeyCount() {
	var n int64
	if err := DB.Model(&model.Passkey{}).Count(&n).Error; err != nil {
		log.Printf("NEZHA>> count passkeys failed: %v", err)
		return
	}
	passkeyCount.Store(n)
}

// PasskeyEnabled 是否已注册至少一个通行密钥。
func PasskeyEnabled() bool {
	return passkeyCount.Load() > 0
}
