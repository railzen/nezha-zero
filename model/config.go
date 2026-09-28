package model

import (
	"errors"
	"os"
	"strconv"
	"strings"

	kyaml "github.com/knadh/koanf/parsers/yaml"
	"github.com/knadh/koanf/providers/env"
	"github.com/knadh/koanf/providers/file"
	"github.com/knadh/koanf/v2"
	"github.com/railzen/nezha-zero/pkg/utils"
	"golang.org/x/crypto/bcrypt"
	"gopkg.in/yaml.v3"
)

var Languages = map[string]string{
	"zh-CN": "简体中文",
	"zh-TW": "繁體中文",
	"en-US": "English",
}

var Themes = map[string]string{
	"server-status":     "ServerStatus",
	"server-status-dev": "ServerStatusDev",
	"nazhua":            "Nazhua(V1)",
	"nezha-dash":        "Nezha Dash(V1)",
	"daynight":          "JackieSung DayNight",
	"mdui":              "Neko Mdui",
	"hotaru":            "Hotaru",
	"angel-kanade":      "AngelKanade",
	"default":           "Default",
	"custom":            "Custom(local)",
}

var DashboardThemes = map[string]string{
	"default": "Default",
	"custom":  "Custom(local)",
}

// AvailableThemes 返回可选前台主题。禁用 V1 兼容 API 时隐藏名称以 (V1) 结尾的主题，但保留 currentTheme 以便用户从当前 V1 主题切换离开。
func AvailableThemes(compatAPIDisabled bool, currentTheme string) map[string]string {
	if !compatAPIDisabled {
		return Themes
	}
	out := make(map[string]string, len(Themes))
	for k, v := range Themes {
		if !strings.HasSuffix(v, "(V1)") || k == currentTheme {
			out[k] = v
		}
	}
	return out
}

// IsV1Theme 判断主题是否为 V1 主题（显示名称以 (V1) 结尾）。
func IsV1Theme(themeKey string) bool {
	name, ok := Themes[themeKey]
	return ok && strings.HasSuffix(name, "(V1)")
}

const (
	ConfigTypeGitHub     = "github"
	ConfigTypeGitee      = "gitee"
	ConfigTypeGitlab     = "gitlab"
	ConfigTypeJihulab    = "jihulab"
	ConfigTypeGitea      = "gitea"
	ConfigTypeCloudflare = "cloudflare"
	ConfigTypeOidc       = "oidc"
)

const (
	ConfigCoverAll = iota
	ConfigCoverIgnoreAll
)

// Config 站点配置
type Config struct {
	Debug    bool   // debug模式开关
	Language string // 系统语言，默认 zh-CN
	Site     struct {
		Brand                string // 站点名称
		CookieName           string // 浏览器 Cookie 名称
		Theme                string
		DashboardTheme       string
		CustomCode           string
		CustomCodeDashboard  string
		ViewPassword         string // 前台查看密码
		AdminPassword        string // 管理员密码
		DisablePasswordLogin bool   // 禁用密码登录
		TwoFactorSecret      string // TOTP 密钥（Base32），非空即表示已启用双重验证
	}
	Oauth2 struct {
		Type              string
		Admin             string // 管理员用户名列表
		AdminGroups       string // 管理员用户组列表
		ClientID          string
		ClientSecret      string
		Endpoint          string
		OidcDisplayName   string // for OIDC Display Name
		OidcIssuer        string // for OIDC Issuer
		OidcLogoutURL     string // for OIDC Logout URL
		OidcRegisterURL   string // for OIDC Register URL
		OidcLoginClaim    string // for OIDC Claim
		OidcGroupClaim    string // for OIDC Group Claim
		OidcScopes        string // for OIDC Scopes
		OidcAutoCreate    bool   // for OIDC Auto Create
		OidcAutoLogin     bool   // for OIDC Auto Login
		DisableOauthLogin bool   // 禁用 OAuth 登录
	}
	HTTPPort        uint
	GRPCPort        uint
	GRPCHost        string
	GRPCDiscoverKey string
	ProxyGRPCPort   uint
	TLS             bool

	EnablePlainIPInNotification     bool // 通知信息IP不打码
	DisableSwitchTemplateInFrontend bool // 前台禁用切换模板功能
	DisableCommandExecuteInInstall  bool // 界面安装脚本默认附带 --disable-command-execute
	CompatAPIDisable                bool // 兼容API开关
	UseTemplateHandleNoRoute        bool // 用模板处理无路由情况
	EnableSubscription              bool // 启用订阅管理
	SyncServerSubscription          bool // 订阅页显示服务器账单
	EnableCurrencyConversion        bool // 启用订阅货币换算
	ShowMonthlyPrice                bool // 订阅列表按月度价格显示
	CurrencyProvider                string
	CurrencyAPIKey                  string
	BaseCurrency                    string

	// UseExternalGeoIP 为 true 时使用 data 目录下的外部 GeoLite2 库解析国家/地区；否则使用内置库。
	UseExternalGeoIP bool

	// IP变更提醒
	EnableIPChangeNotification bool
	IPChangeNotificationTag    string
	Cover                      uint8  // 覆盖范围（0:提醒未被 IgnoredIPNotification 包含的所有服务器; 1:仅提醒被 IgnoredIPNotification 包含的服务器;）
	IgnoredIPNotification      string // 特定服务器IP（多个服务器用逗号分隔）

	Location string // 时区，默认为 Asia/Shanghai

	IgnoredIPNotificationServerIDs map[uint64]bool // [ServerID] -> bool(值为true代表当前ServerID在特定服务器列表内）
	MaxTCPPingValue                int32
	AvgPingCount                   int

	DNSServers string

	k        *koanf.Koanf
	filePath string
}

// Read 读取配置文件并应用
func (c *Config) Read(path string) error {
	c.k = koanf.New(".")
	c.filePath = path

	// 先读取环境变量，然后读取配置文件；后者可以覆盖前者，因为哪吒支持在线修改配置
	haveParaChange := false

	err := c.k.Load(env.Provider("NZ_", ".", func(s string) string {
		return strings.Replace(strings.ToLower(strings.TrimPrefix(s, "NZ_")), "_", ".", -1)
	}), nil)
	if err != nil {
		return err
	}

	if _, err := os.Stat(path); err == nil {
		err = c.k.Load(file.Provider(path), kyaml.Parser())
		if err != nil {
			return err
		}
	}

	err = c.k.Unmarshal("", c)
	if err != nil {
		return err
	}

	// 未显式配置时默认禁用 V1 兼容 API
	if !c.k.Exists("compatapidisable") {
		c.CompatAPIDisable = true
	}
	// 未显式配置时默认显示服务器账单
	if !c.k.Exists("syncserversubscription") {
		c.SyncServerSubscription = true
	}

	if c.Oauth2.Admin == "" {
		return errors.New("missing admin user config")
	}

	oauthConfigIncomplete := c.Oauth2.Type == "" || c.Oauth2.ClientID == "" || c.Oauth2.ClientSecret == ""
	if oauthConfigIncomplete {
		if c.k.Exists("oauth2.disableoauthlogin") && !c.Oauth2.DisableOauthLogin {
			return errors.New("missing oauth2 config")
		}
		if !c.Oauth2.DisableOauthLogin {
			c.Oauth2.DisableOauthLogin = true
			haveParaChange = true
		}
	}

	if c.Site.Brand == "" {
		c.Site.Brand = "Nezha Monitoring"
	}

	if c.Site.CookieName == "" || c.Site.CookieName == "nezha-dashboard" {
		// 默认设置为 nz-jwt 以保证 v1 兼容性
		c.Site.CookieName = "nz-jwt"
	}

	if c.Site.Theme == "" {
		c.Site.Theme = "default"
	}

	if c.Site.DashboardTheme == "" {
		c.Site.DashboardTheme = "default"
	}

	if c.Site.AdminPassword != "" {
		// 已有密码，判断它是否已经是 bcrypt 哈希
		if _, err := bcrypt.Cost([]byte(c.Site.AdminPassword)); err != nil {
			hash, err := bcrypt.GenerateFromPassword([]byte(c.Site.AdminPassword), utils.BcryptCost)
			if err != nil {
				panic(err)
			}
			c.Site.AdminPassword = string(hash)
			haveParaChange = true
		}
	}

	if !c.k.Exists("grpcdiscoverkey") {
		// 生成 secret
		newKey, err := utils.GenerateRandomString(18)
		if err != nil {
			newKey = ""
		}
		c.GRPCDiscoverKey = newKey
		haveParaChange = true
	}

	if c.Language == "" {
		c.Language = "zh-CN"
	}
	if c.HTTPPort == 0 {
		c.HTTPPort = 80
	}
	if c.GRPCPort == 0 {
		c.GRPCPort = 5555
	}
	if c.EnableIPChangeNotification && c.IPChangeNotificationTag == "" {
		c.IPChangeNotificationTag = "default"
	}
	if c.Location == "" {
		c.Location = "Asia/Shanghai"
	}
	if c.MaxTCPPingValue == 0 {
		c.MaxTCPPingValue = 1000
	}
	if c.AvgPingCount == 0 {
		c.AvgPingCount = 2
	}
	if c.CurrencyProvider == "" {
		c.CurrencyProvider = "fixer"
	}
	if c.BaseCurrency == "" || !IsSupportedCurrency(c.BaseCurrency) {
		c.BaseCurrency = "USD"
	}
	if c.Oauth2.OidcScopes == "" {
		c.Oauth2.OidcScopes = "openid,profile,email"
	}
	if c.Oauth2.OidcLoginClaim == "" {
		c.Oauth2.OidcLoginClaim = "sub"
	}
	if c.Oauth2.OidcDisplayName == "" {
		c.Oauth2.OidcDisplayName = "OIDC"
	}
	if c.Oauth2.OidcGroupClaim == "" {
		c.Oauth2.OidcGroupClaim = "groups"
	}

	// 未设置密码时自动禁用密码登录
	if c.Site.AdminPassword == "" {
		c.Site.DisablePasswordLogin = true
	}

	if err := c.ValidateLoginConfig(); err != nil {
		return err
	}

	c.updateIgnoredIPNotificationID()

	if haveParaChange {
		c.Save()
	}

	return nil
}

func (c *Config) hasAdminPassword() bool {
	return c.Site.AdminPassword != "" && len(c.Site.AdminPassword) >= 10
}

func (c *Config) hasPasswordAdminList() bool {
	for _, admin := range strings.Split(c.Oauth2.Admin, ",") {
		if strings.TrimSpace(admin) != "" {
			return true
		}
	}
	return false
}

// PasswordLoginActive 密码登录已启用且密码、管理员用户名均已配置。
func (c *Config) PasswordLoginActive() bool {
	return !c.Site.DisablePasswordLogin && c.hasAdminPassword() && c.hasPasswordAdminList()
}

// LoginAvailable 是否至少有一种可用的登录方式。
func (c *Config) LoginAvailable() bool {
	return c.PasswordLoginActive() || !c.Oauth2.DisableOauthLogin
}

// ValidateLoginConfig 校验登录方式配置是否合法。
func (c *Config) ValidateLoginConfig() error {
	if !c.LoginAvailable() {
		return errors.New("至少需要启用一种登录方式")
	}
	if c.Oauth2.DisableOauthLogin && !c.PasswordLoginActive() {
		return errors.New("禁用 OAuth 登录前必须先启用并配置密码登录")
	}
	if !c.Site.DisablePasswordLogin {
		if !c.hasPasswordAdminList() {
			return errors.New("启用密码登录时必须配置至少一个管理员用户名")
		}
		if !c.hasAdminPassword() {
			return errors.New("启用密码登录时必须设置管理员密码")
		}
	}
	return nil
}

// TwoFactorActive 是否已启用登录双重验证（以密钥是否存在为准）。
func (c *Config) TwoFactorActive() bool {
	return c.Site.TwoFactorSecret != ""
}

// updateIgnoredIPNotificationID 更新用于判断服务器ID是否属于特定服务器的map。
// 必须在本地构建完成后一次性整体赋值：ReportSystemInfo 热路径（rpc/nezha.go）会
// 无锁并发读取该字段，若先赋值再逐项写入，会触发不可 recover 的
// concurrent map read and map write 致命错误。
func (c *Config) updateIgnoredIPNotificationID() {
	ids := make(map[uint64]bool)
	splitedIDs := strings.Split(c.IgnoredIPNotification, ",")
	for i := 0; i < len(splitedIDs); i++ {
		id, _ := strconv.ParseUint(splitedIDs[i], 10, 64)
		if id > 0 {
			ids[id] = true
		}
	}
	c.IgnoredIPNotificationServerIDs = ids
}

// Save 保存配置文件
func (c *Config) Save() error {
	if err := c.ValidateLoginConfig(); err != nil {
		return err
	}
	c.updateIgnoredIPNotificationID()
	data, err := yaml.Marshal(c)
	if err != nil {
		return err
	}
	return os.WriteFile(c.filePath, data, 0600)
}
