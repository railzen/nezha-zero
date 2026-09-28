package audit

import (
	"fmt"
	"strconv"
	"strings"
	"sync/atomic"
	"unicode"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/railzen/nezha-zero/model"
)

const (
	TypeAuth     = "auth"
	TypeSecurity = "security"
	TypeConfig   = "config"
	TypeEvent    = "event"
	MaxLogs      = 1000
	PageSize     = 20
	maxDetailLen = 1024
)

var store atomic.Pointer[gorm.DB]

// Init 设置审计日志使用的数据库；传入 nil 可在数据库关闭前停用写入。
func Init(db *gorm.DB) {
	store.Store(db)
}

func currentDB() *gorm.DB {
	return store.Load()
}

type TypeOption struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}

var typeLabels = map[string]string{
	TypeAuth:     "认证",
	TypeSecurity: "安全",
	TypeConfig:   "配置",
	TypeEvent:    "事件",
}

// SettingChangeInput 设置保存后的目标值（用于生成变更详情，不含敏感内容）。
type SettingChangeInput struct {
	Title                           string
	Admin                           string
	Language                        string
	Theme                           string
	DashboardTheme                  string
	CustomCodeChanged               bool
	CustomCodeDashboardChanged      bool
	ViewPasswordChanged             bool
	CustomNameservers               string
	UseExternalGeoIP                bool
	EnableIPChangeNotification      bool
	EnablePlainIPInNotification     bool
	DisableSwitchTemplateInFrontend bool
	CompatAPIDisable                bool
	EnableSubscription              bool
	SyncServerSubscription          bool
	EnableCurrencyConversion        bool
	CurrencyProvider                string
	BaseCurrency                    string
	CurrencyAPIKeyChanged           bool
	UseTemplateHandleNoRoute        bool
	DisableOauthLogin               bool
	DisablePasswordLogin            bool
	GRPCHost                        string
	GRPCDiscoverKey                 string
	Cover                           uint8
	IgnoredIPNotification           string
	IPChangeNotificationTag         string
	PasswordChanged                 bool
}

// TypeOptions 供筛选使用
func TypeOptions() []TypeOption {
	return []TypeOption{
		{ID: "all", Label: "全部"},
		{ID: TypeAuth, Label: typeLabels[TypeAuth]},
		{ID: TypeSecurity, Label: typeLabels[TypeSecurity]},
		{ID: TypeConfig, Label: typeLabels[TypeConfig]},
		{ID: TypeEvent, Label: typeLabels[TypeEvent]},
	}
}

// BuildSecuritySettingDetail 安全相关配置变更（登录、权限、API 边界等）。
func BuildSecuritySettingDetail(before *model.Config, in SettingChangeInput) string {
	if before == nil {
		return ""
	}
	var changes []string
	if before.Oauth2.Admin != in.Admin {
		changes = append(changes, "Admin user list changed")
	}
	appendBoolChange(&changes, "OAuth login disabled", before.Oauth2.DisableOauthLogin, in.DisableOauthLogin)
	appendBoolChange(&changes, "Password login disabled", before.Site.DisablePasswordLogin, in.DisablePasswordLogin)
	appendBoolChange(&changes, "Compat API disabled", before.CompatAPIDisable, in.CompatAPIDisable)
	if in.ViewPasswordChanged {
		changes = append(changes, "Frontend view password changed")
	}
	if in.PasswordChanged {
		changes = append(changes, "Admin password changed")
	}
	return joinChanges(changes)
}

// BuildConfigSettingDetail 非安全类站点配置变更。
func BuildConfigSettingDetail(before *model.Config, in SettingChangeInput) string {
	if before == nil {
		return ""
	}
	var changes []string
	appendStrChange(&changes, "Site title", before.Site.Brand, in.Title)
	appendStrChange(&changes, "Language", before.Language, in.Language)
	appendStrChange(&changes, "Frontend theme", before.Site.Theme, in.Theme)
	appendStrChange(&changes, "Dashboard theme", before.Site.DashboardTheme, in.DashboardTheme)
	appendStrChange(&changes, "gRPC host", before.GRPCHost, in.GRPCHost)
	if before.GRPCDiscoverKey != in.GRPCDiscoverKey {
		changes = append(changes, "gRPC discover key changed")
	}
	appendStrChange(&changes, "DNS servers", before.DNSServers, in.CustomNameservers)
	appendStrChange(&changes, "Ignored IPs for notification", before.IgnoredIPNotification, in.IgnoredIPNotification)
	appendStrChange(&changes, "IP change notification tag", before.IPChangeNotificationTag, in.IPChangeNotificationTag)
	appendUint8Change(&changes, "Notification cover mode", before.Cover, in.Cover)
	appendBoolChange(&changes, "External GeoIP lookup", before.UseExternalGeoIP, in.UseExternalGeoIP)
	appendBoolChange(&changes, "IP change notification", before.EnableIPChangeNotification, in.EnableIPChangeNotification)
	appendBoolChange(&changes, "Plain IP in notification", before.EnablePlainIPInNotification, in.EnablePlainIPInNotification)
	appendBoolChange(&changes, "Disable frontend theme switch", before.DisableSwitchTemplateInFrontend, in.DisableSwitchTemplateInFrontend)
	appendBoolChange(&changes, "Template 404 handler", before.UseTemplateHandleNoRoute, in.UseTemplateHandleNoRoute)
	appendBoolChange(&changes, "Subscription management", before.EnableSubscription, in.EnableSubscription)
	appendBoolChange(&changes, "Show server billing", before.SyncServerSubscription, in.SyncServerSubscription)
	appendBoolChange(&changes, "Currency conversion", before.EnableCurrencyConversion, in.EnableCurrencyConversion)
	appendStrChange(&changes, "Currency provider", before.CurrencyProvider, in.CurrencyProvider)
	appendStrChange(&changes, "Base currency", before.BaseCurrency, in.BaseCurrency)
	if in.CurrencyAPIKeyChanged {
		changes = append(changes, "Currency API key changed")
	}
	if in.CustomCodeChanged {
		changes = append(changes, "Frontend custom code modified")
	}
	if in.CustomCodeDashboardChanged {
		changes = append(changes, "Dashboard custom code modified")
	}
	return joinChanges(changes)
}

func joinChanges(changes []string) string {
	if len(changes) == 0 {
		return ""
	}
	for i, c := range changes {
		changes[i] = capitalizeFirst(c)
	}
	return strings.Join(changes, "; ")
}

// Record 写入一条审计日志（忽略写入错误，避免影响主流程）。
func Record(c *gin.Context, typ, action, detail string) {
	db := currentDB()
	if db == nil {
		return
	}
	ip := ""
	if c != nil {
		ip = c.ClientIP()
	}
	if err := db.Create(&model.AuditLog{
		Type:   typ,
		Action: capitalizeLogText(action),
		Detail: trimDetail(capitalizeLogText(detail)),
		IP:     ip,
	}).Error; err != nil {
		return
	}
	pruneExcess(db)
}

// PruneExcess 删除超出上限的最旧日志。
func PruneExcess() {
	db := currentDB()
	if db == nil {
		return
	}
	pruneExcess(db)
}

func pruneExcess(db *gorm.DB) {
	var count int64
	db.Model(&model.AuditLog{}).Count(&count)
	if count <= MaxLogs {
		return
	}
	excess := int(count - MaxLogs)
	var oldest []model.AuditLog
	db.Order("created_at ASC").Limit(excess).Find(&oldest)
	if len(oldest) == 0 {
		return
	}
	ids := make([]uint64, len(oldest))
	for i, entry := range oldest {
		ids[i] = entry.ID
	}
	_ = db.Unscoped().Delete(&model.AuditLog{}, ids).Error
}

func appendStrChange(changes *[]string, name, oldVal, newVal string) {
	if oldVal == newVal {
		return
	}
	*changes = append(*changes, fmt.Sprintf("%s: %q -> %q", name, oldVal, newVal))
}

func appendBoolChange(changes *[]string, name string, oldVal, newVal bool) {
	if oldVal == newVal {
		return
	}
	*changes = append(*changes, fmt.Sprintf("%s: %s -> %s", name, boolWord(oldVal), boolWord(newVal)))
}

func appendUint8Change(changes *[]string, name string, oldVal, newVal uint8) {
	if oldVal == newVal {
		return
	}
	*changes = append(*changes, fmt.Sprintf("%s: %s -> %s", name, strconv.FormatUint(uint64(oldVal), 10), strconv.FormatUint(uint64(newVal), 10)))
}

func boolWord(v bool) string {
	if v {
		return "enabled"
	}
	return "disabled"
}

func capitalizeFirst(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return s
	}
	r := []rune(s)
	r[0] = unicode.ToUpper(r[0])
	return string(r)
}

// capitalizeLogText 保证整条日志及分号分隔的各条详情均以大写字母开头。
func capitalizeLogText(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return s
	}
	parts := strings.Split(s, "; ")
	for i, p := range parts {
		parts[i] = capitalizeFirst(p)
	}
	return strings.Join(parts, "; ")
}

func trimDetail(s string) string {
	if len(s) <= maxDetailLen {
		return s
	}
	return s[:maxDetailLen-3] + "..."
}
