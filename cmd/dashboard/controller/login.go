package controller

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/railzen/nezha-zero/pkg/oidc/cloudflare"
	myOidc "github.com/railzen/nezha-zero/pkg/oidc/general"

	"code.gitea.io/sdk/gitea"
	"github.com/gin-gonic/gin"
	GitHubAPI "github.com/google/go-github/v75/github"
	"github.com/nicksnyder/go-i18n/v2/i18n"
	"github.com/patrickmn/go-cache"
	"github.com/railzen/nezha-zero/model"
	"github.com/railzen/nezha-zero/pkg/audit"
	"github.com/railzen/nezha-zero/pkg/mygin"
	"github.com/railzen/nezha-zero/pkg/totp"
	"github.com/railzen/nezha-zero/pkg/utils"
	"github.com/railzen/nezha-zero/service/singleton"
	"github.com/xanzy/go-gitlab"
	"golang.org/x/crypto/bcrypt"
	"golang.org/x/oauth2"
	GitHubOauth2 "golang.org/x/oauth2/github"
	GitlabOauth2 "golang.org/x/oauth2/gitlab"
)

var (
	rsaPrivateKey    *rsa.PrivateKey
	RSAPublicKeyNHex string
	RSAPublicKeyE    int
)

var (
	loginChallengeConsumeMu sync.Mutex
	twoFactorMu             sync.Mutex
)

const (
	loginChallengeCachePrefix = "login_challenge_"
	loginChallengeTTL         = 5 * time.Minute
	authRateLimit1sKey        = "authrate_r1s"
	authRateLimit30sKey       = "authrate_r30s"
	authRateLimit1sMax        = 9
	authRateLimit30sMax       = 75
	// 来源级认证限速：同一 IP 网段在 10 秒内超过 12 次请求记一次违规
	authSourceRequestPrefix = "auth_src_req_"
	authSourceTripPrefix    = "auth_src_trip_"
	authBlacklistPrefix     = "auth_black_"
	authSourceRequestWindow = 10 * time.Second
	authSourceRequestMax    = 18
	authSourceTripWindow    = 10 * time.Minute
	authSourceTripThreshold = 9
	// authBlacklistTTL 拉黑时长。
	authBlacklistTTL = 30 * time.Minute
	// authSourceStateMaxItems 来源请求、违规和黑名单状态的总条目硬上限。
	authSourceStateMaxItems = 3000
	oidcProviderTTL         = time.Hour
	// twoFactorCachePrefix 二次验证一次性 ticket 的缓存键前缀。
	// ticket 只存于服务端缓存，对应已通过密码或 OAuth 身份核验但尚未通过 2FA 的用户，
	// 通过 2FA 后立即删除（一次性消费），避免被重放。
	twoFactorCachePrefix = "login_2fa_"
	twoFactorTTL         = 5 * time.Minute
	// twoFactorMaxAttempts 同一 ticket 允许的 TOTP 试错次数，防 6 位码枚举。
	twoFactorMaxAttempts = 5
)

var (
	authSourceStateMu    sync.Mutex
	authSourceStateCache = cache.New(authBlacklistTTL, time.Minute)
)

type twoFactorLoginMethod uint8

const (
	twoFactorLoginMethodPassword twoFactorLoginMethod = iota
	twoFactorLoginMethodOAuth
)

// twoFactorTicket 二次验证中间态：已通过第一阶段身份核验、等待 TOTP 校验。
// 仅存于服务端缓存，不下发客户端。
type twoFactorTicket struct {
	User     model.User
	Method   twoFactorLoginMethod
	Attempts int
}

func init() {
	// 每次启动随机生成 RSA-2048 密钥对，私钥仅驻留内存
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		panic(fmt.Sprintf("RSA key generation failed: %v", err))
	}
	rsaPrivateKey = key
	RSAPublicKeyNHex = hex.EncodeToString(key.PublicKey.N.Bytes())
	RSAPublicKeyE = key.PublicKey.E
}

type oauth2controller struct {
	r                     gin.IRoutes
	oidcProviderMu        sync.Mutex
	oidcIssuer            string
	oidcProvider          *oidc.Provider
	oidcProviderFetchedAt time.Time
}

func (oa *oauth2controller) serve() {
	oa.r.GET("/oauth2/login", oa.login)
	oa.r.GET("/oauth2/callback", oa.callback)
	oa.r.POST("/auth/2fa", oa.twoFactorVerify)
	oa.r.POST("/passkeyinfo", oa.passkeyLoginOptions)
	oa.r.POST("/auth/passkey", oa.passkeyLogin)
	oa.r.POST("/auth", oa.passwordLogin)
	oa.r.GET("/authinfo", oa.newChallenge)
}

// newChallenge 为前端提供一个新的一次性登录挑战（供登录失败后无刷新重试使用）
func (oa *oauth2controller) newChallenge(c *gin.Context) {
	if !allowAuthRateLimitedCheck(c) {
		c.JSON(http.StatusTooManyRequests, gin.H{"error": "too many requests"})
		return
	}

	loginChallengeID, err := utils.GenerateRandomString(24)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to generate challenge"})
		return
	}
	loginChallenge, err := utils.GenerateRandomString(32)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to generate challenge"})
		return
	}
	singleton.Cache.Set(loginChallengeCachePrefix+loginChallengeID, loginChallenge, loginChallengeTTL)
	c.JSON(http.StatusOK, gin.H{
		"challengeID":      loginChallengeID,
		"challenge":        loginChallenge,
		"publicKeyN":       RSAPublicKeyNHex,
		"publicKeyE":       RSAPublicKeyE,
		"twoFactorEnabled": singleton.Conf.TwoFactorActive(),
	})
}

func (oa *oauth2controller) passwordLogin(c *gin.Context) {
	if !allowAuthRateLimitedCheck(c) {
		// 被全局限速器拒绝的请求不写审计，避免未认证写放大打满 SQLite 单写者。
		showLoginRuleFailed(c)
		return
	}

	type LoginForm struct {
		Username string `form:"username" binding:"required,max=64"`
		Password string `form:"password" binding:"required,min=6"`
	}

	var req LoginForm
	if err := c.ShouldBind(&req); err != nil {
		audit.Record(c, audit.TypeAuth, "Password login failed", "invalid request parameters")
		showLoginFailed(c)
		return
	}
	req.Username = strings.TrimSpace(req.Username)
	req.Password = strings.TrimSpace(req.Password)
	// 纯空白用户名可通过 required 校验，去空格后为空时与参数无效同样处理，
	// 避免管理员名单含空项（如 "admin,"）时空用户名匹配空项
	if req.Username == "" {
		audit.Record(c, audit.TypeAuth, "Password login failed", "invalid request parameters")
		showLoginFailed(c)
		return
	}

	ruleAllowed := true
	if !singleton.Conf.PasswordLoginActive() {
		audit.Record(c, audit.TypeAuth, "Password login failed", "password login is disabled")
		mygin.ShowErrorPage(c, mygin.ErrInfo{
			Code:  400,
			Title: "登录失败",
			Msg:   "不支持的登陆方式",
		}, true)
		return
	}

	// 获取客户端 IP 地址
	clientIP := c.ClientIP()

	// 1. 限制连续失败次数
	u := sha1.Sum([]byte(strings.ToLower(req.Username)))
	failKey := "passwd_fail_" + hex.EncodeToString(u[:])
	failCount, _ := singleton.Cache.Get(failKey)
	if failCountInt, ok := failCount.(int); ok && failCountInt >= 5 {
		ruleAllowed = false
	}

	// 2. IP 地址限制（IPv4 /24、IPv6 /48 网段共享计数）
	ipFailKey := passwordLoginIPFailKey(clientIP)
	ipFailCount, _ := singleton.Cache.Get(ipFailKey)
	if ipFailCountInt, ok := ipFailCount.(int); ok && ipFailCountInt >= 5 {
		ruleAllowed = false
	}

	if !ruleAllowed {
		// 已锁定时不再累加计数：否则 Cache.Set 会刷新 TTL，攻击者每 <10min 一次即可永久锁死
		audit.Record(c, audit.TypeAuth, "Password login failed", "too many failed attempts, temporarily blocked")
		showLoginRuleFailed(c)
		return
	}

	// 校验用户名是否在管理员列表
	allowed := false
	for _, admin := range strings.Split(singleton.Conf.Oauth2.Admin, ",") {
		if strings.EqualFold(req.Username, strings.TrimSpace(admin)) {
			allowed = true
			break
		}
	}

	// RSA-OAEP(SHA-256) 解密：前端提交 challengeID + password + challenge 的加密载荷
	ciphertext, decodeErr := base64.StdEncoding.DecodeString(req.Password)
	if decodeErr != nil {
		allowed = false
	}

	var plaintext []byte
	var passwordBytes []byte
	var challengeID string
	if decodeErr == nil && len(ciphertext) > 0 {
		var decryptErr error
		plaintext, decryptErr = rsa.DecryptOAEP(sha256.New(), rand.Reader, rsaPrivateKey, ciphertext, nil)
		if decryptErr != nil {
			allowed = false
		} else {
			parts := bytes.SplitN(plaintext, []byte{'\n'}, 3)
			if len(parts) != 3 || len(bytes.TrimSpace(parts[0])) == 0 || len(parts[1]) < 6 || len(bytes.TrimSpace(parts[2])) == 0 {
				allowed = false
			} else {
				challengeID = strings.TrimSpace(string(parts[0]))
				passwordBytes = parts[1]
				challenge := strings.TrimSpace(string(parts[2]))
				if !consumeLoginChallenge(challengeID, challenge) {
					allowed = false
				}
			}
		}
	}

	// 确保 bcrypt 比较始终执行（防止时序攻击）
	if len(passwordBytes) == 0 {
		passwordBytes = []byte("_nezha_invalid_password_placeholder_")
	}

	// 校验密码（bcrypt）；始终使用 AdminPassword，避免 fakeHash cost 不一致泄露用户名
	if err := bcrypt.CompareHashAndPassword([]byte(singleton.Conf.Site.AdminPassword), passwordBytes); err != nil {
		allowed = false
	}

	// 比较结束立即清零内存
	for i := range plaintext {
		plaintext[i] = 0
	}
	for i := range passwordBytes {
		passwordBytes[i] = 0
	}

	if !allowed {
		incrementFailCount(failKey)
		incrementFailCount(ipFailKey)
		audit.Record(c, audit.TypeAuth, "Password login failed", "invalid username or password")
		showLoginFailed(c)
		return
	}

	// 第一阶段身份核验成功，清除密码失败计数。TOTP 使用独立 ticket 次数限制。
	singleton.Cache.Delete(failKey)
	singleton.Cache.Delete(ipFailKey)

	// 构造管理员用户
	user := model.User{
		Login:      req.Username,
		Name:       "Admin",
		SuperAdmin: true,
	}

	if singleton.Conf.TwoFactorActive() {
		oa.beginTwoFactor(c, user, twoFactorLoginMethodPassword)
		return
	}

	oa.issuePasswordSession(c, &user)
}

// issuePasswordSession 为已通过全部校验的密码用户发放独立密码会话。
func (oa *oauth2controller) issuePasswordSession(c *gin.Context, user *model.User) {
	sessionToken, err := utils.NewSessionToken()
	if err != nil {
		mygin.ShowErrorPage(c, mygin.ErrInfo{
			Code: 400, Title: "登录失败", Msg: "系统错误",
		}, true)
		return
	}
	user.Token = sessionToken.Hash
	user.TokenExpired = time.Now().UTC().AddDate(0, 0, 3)

	if err := user.SavePasswordSession(singleton.DB); err != nil {
		mygin.ShowErrorPage(c, mygin.ErrInfo{
			Code: 400, Title: "登录失败", Msg: "系统错误",
		}, true)
		return
	}

	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie(singleton.Conf.Site.CookieName, sessionToken.Plain, 60*60*24*3, "/", "", mygin.CookieSecure(c), true)
	mygin.SetCSRFCookie(c)

	// 登录成功跳转
	c.HTML(200, "dashboard-"+singleton.Conf.Site.DashboardTheme+"/redirect", mygin.CommonEnvironment(c, gin.H{
		"URL": "/",
	}))
	audit.Record(c, audit.TypeAuth, "Password login succeeded", "user: "+user.Login)
}

// 显示统一登录失败页面
func showLoginFailed(c *gin.Context) {
	mygin.ShowErrorPage(c, mygin.ErrInfo{
		Code: 403, Title: "用户名或密码错误", Msg: "用户名或密码错误",
	}, true)
}

// 显示统一登录失败页面
func showLoginRuleFailed(c *gin.Context) {
	mygin.ShowErrorPage(c, mygin.ErrInfo{
		Code: 400, Title: "登陆失败", Msg: "非法请求，请稍后再试",
	}, true)
}

// consumeLoginChallenge 校验并一次性消费登录 challenge，防止并发重放。
func consumeLoginChallenge(challengeID, challenge string) bool {
	challengeID = strings.TrimSpace(challengeID)
	challenge = strings.TrimSpace(challenge)
	if challengeID == "" || challenge == "" {
		return false
	}
	loginChallengeConsumeMu.Lock()
	defer loginChallengeConsumeMu.Unlock()

	key := loginChallengeCachePrefix + challengeID
	cacheValue, found := singleton.Cache.Get(key)
	if !found {
		return false
	}
	cachedChallenge, ok := cacheValue.(string)
	if !ok || cachedChallenge != challenge {
		return false
	}
	singleton.Cache.Delete(key)
	return true
}

// allowAuthRateLimitedCheck 先执行来源级限速，再执行全站认证接口共用的全局安全阀。
// 来源级拒绝和黑名单命中均不消耗全局额度，避免单一来源长期占满全局限流。
func allowAuthRateLimitedCheck(c *gin.Context) bool {
	ipKey := passwordLoginIPFailKey(c.ClientIP())

	// ① 黑名单命中：最早短路，不计入任何限速、不写重复审计。
	if _, blocked := authSourceStateCache.Get(authBlacklistPrefix + ipKey); blocked {
		return false
	}

	// ② 来源短窗口。容量已满时不创建新来源状态，退回全局安全阀。
	requestCount, tracked := incrementAuthSourceState(authSourceRequestPrefix+ipKey, authSourceRequestWindow)
	if tracked && requestCount > authSourceRequestMax {
		// 每个短窗口只有第一个超限请求记录一次违规。
		if requestCount == authSourceRequestMax+1 {
			tripCount, tripTracked := incrementAuthSourceState(authSourceTripPrefix+ipKey, authSourceTripWindow)
			if tripTracked && tripCount >= authSourceTripThreshold {
				authBlacklistAdd(c, ipKey)
			}
		}
		return false
	}

	// ③ 全局安全阀仅统计通过来源级检查的请求。
	if incrementAuthRateLimit(authRateLimit1sKey, time.Second) > authRateLimit1sMax ||
		incrementAuthRateLimit(authRateLimit30sKey, 30*time.Second) > authRateLimit30sMax {
		return false
	}
	return true
}

// incrementAuthSourceState 原子递增独立的来源状态。新来源创建受硬容量限制；
// 容量满时返回 tracked=false，由调用方退回全局安全阀。
func incrementAuthSourceState(key string, window time.Duration) (count int, tracked bool) {
	for {
		if _, found := authSourceStateCache.Get(key); !found {
			authSourceStateMu.Lock()
			if _, found = authSourceStateCache.Get(key); !found {
				if authSourceStateCache.ItemCount() >= authSourceStateMaxItems {
					authSourceStateMu.Unlock()
					return 0, false
				}
				if err := authSourceStateCache.Add(key, 1, window); err == nil {
					authSourceStateMu.Unlock()
					return 1, true
				}
			}
			authSourceStateMu.Unlock()
		}
		if n, err := authSourceStateCache.IncrementInt(key, 1); err == nil {
			return n, true
		}
	}
}

// authBlacklistAdd 将某 IP 网段加入认证限速黑名单。仅在加入时写一次审计。
func authBlacklistAdd(c *gin.Context, ipKey string) {
	blackKey := authBlacklistPrefix + ipKey

	authSourceStateMu.Lock()
	// 转换为黑名单前释放该来源的请求和违规状态，容量不会净增长。
	authSourceStateCache.Delete(authSourceRequestPrefix + ipKey)
	authSourceStateCache.Delete(authSourceTripPrefix + ipKey)
	err := authSourceStateCache.Add(blackKey, 1, authBlacklistTTL)
	authSourceStateMu.Unlock()
	if err != nil {
		return
	}
	audit.Record(c, audit.TypeAuth, "Auth rate-limit blacklist", "IP segment blacklisted for repeated rate-limit hits: "+ipKey)
}

// incrementAuthRateLimit 原子地将窗口计数 +1 并返回新值。固定窗口语义：已有键的递增不刷新
// TTL，窗口随首个请求起点到期后重置。Add 与 IncrementInt 均为 go-cache 内部持锁的原子操作，
// 通过 Add 优先处理"窗口首个请求"的初始化，彻底消除并发首请求初始化/递增之间的竞争窗口。
func incrementAuthRateLimit(key string, window time.Duration) int {
	for {
		// 窗口首个请求：原子地把键初始化为 1 并设置 TTL。已存在（未过期）则返回 error。
		if err := singleton.Cache.Add(key, 1, window); err == nil {
			return 1
		}
		// 键已存在：原子递增并取新值。若键恰在 Add 与递增之间过期被清除，IncrementInt 报错，
		// 回到 Add 重新作为新窗口起点。
		if n, err := singleton.Cache.IncrementInt(key, 1); err == nil {
			return n
		}
	}
}

// passwordLoginIPFailKey 生成 IP 失败计数键。
// IPv4 按 /24、IPv6 按 /48 聚合，同网段共享封锁。
func passwordLoginIPFailKey(clientIP string) string {
	ip := net.ParseIP(clientIP)
	if ip == nil {
		return "ip_fail_" + clientIP
	}
	if ip4 := ip.To4(); ip4 != nil {
		return "ip_fail_" + (&net.IPNet{
			IP:   net.IP{ip4[0], ip4[1], ip4[2], 0},
			Mask: net.CIDRMask(24, 32),
		}).String()
	}
	ip16 := ip.To16()
	if ip16 == nil {
		return "ip_fail_" + clientIP
	}
	masked := make(net.IP, net.IPv6len)
	copy(masked, ip16)
	for i := 6; i < net.IPv6len; i++ {
		masked[i] = 0
	}
	return "ip_fail_" + (&net.IPNet{IP: masked, Mask: net.CIDRMask(48, 128)}).String()
}

// 增加失败计数。仅首次创建时设置 10 分钟 TTL，后续 IncrementInt 不刷新过期时间，
// 避免失败窗口被反复续期导致永久锁定。
func incrementFailCount(key string) {
	const window = 10 * time.Minute
	for {
		if err := singleton.Cache.Add(key, 1, window); err == nil {
			return
		}
		if _, err := singleton.Cache.IncrementInt(key, 1); err == nil {
			return
		}
	}
}

func (oa *oauth2controller) getCommonOauth2Config(c *gin.Context) *oauth2.Config {
	if singleton.Conf.Oauth2.Type == model.ConfigTypeGitee {
		return &oauth2.Config{
			ClientID:     singleton.Conf.Oauth2.ClientID,
			ClientSecret: singleton.Conf.Oauth2.ClientSecret,
			Scopes:       []string{},
			Endpoint: oauth2.Endpoint{
				AuthURL:  "https://gitee.com/oauth/authorize",
				TokenURL: "https://gitee.com/oauth/token",
			},
			RedirectURL: oa.getRedirectURL(c),
		}
	} else if singleton.Conf.Oauth2.Type == model.ConfigTypeGitlab {
		return &oauth2.Config{
			ClientID:     singleton.Conf.Oauth2.ClientID,
			ClientSecret: singleton.Conf.Oauth2.ClientSecret,
			Scopes:       []string{"read_user", "read_api"},
			Endpoint:     GitlabOauth2.Endpoint,
			RedirectURL:  oa.getRedirectURL(c),
		}
	} else if singleton.Conf.Oauth2.Type == model.ConfigTypeJihulab {
		return &oauth2.Config{
			ClientID:     singleton.Conf.Oauth2.ClientID,
			ClientSecret: singleton.Conf.Oauth2.ClientSecret,
			Scopes:       []string{"read_user", "read_api"},
			Endpoint: oauth2.Endpoint{
				AuthURL:  "https://jihulab.com/oauth/authorize",
				TokenURL: "https://jihulab.com/oauth/token",
			},
			RedirectURL: oa.getRedirectURL(c),
		}
	} else if singleton.Conf.Oauth2.Type == model.ConfigTypeGitea {
		return &oauth2.Config{
			ClientID:     singleton.Conf.Oauth2.ClientID,
			ClientSecret: singleton.Conf.Oauth2.ClientSecret,
			Endpoint: oauth2.Endpoint{
				AuthURL:  fmt.Sprintf("%s/login/oauth/authorize", singleton.Conf.Oauth2.Endpoint),
				TokenURL: fmt.Sprintf("%s/login/oauth/access_token", singleton.Conf.Oauth2.Endpoint),
			},
			RedirectURL: oa.getRedirectURL(c),
		}
	} else if singleton.Conf.Oauth2.Type == model.ConfigTypeCloudflare {
		return &oauth2.Config{
			ClientID:     singleton.Conf.Oauth2.ClientID,
			ClientSecret: singleton.Conf.Oauth2.ClientSecret,
			Scopes:       []string{"openid", "email", "profile", "groups"},
			Endpoint: oauth2.Endpoint{
				AuthURL:  fmt.Sprintf("%s/cdn-cgi/access/sso/oidc/%s/authorization", singleton.Conf.Oauth2.Endpoint, singleton.Conf.Oauth2.ClientID),
				TokenURL: fmt.Sprintf("%s/cdn-cgi/access/sso/oidc/%s/token", singleton.Conf.Oauth2.Endpoint, singleton.Conf.Oauth2.ClientID),
			},
			RedirectURL: oa.getRedirectURL(c),
		}
	} else if singleton.Conf.Oauth2.Type == model.ConfigTypeOidc {
		provider, err := oa.getOIDCProvider(c.Request.Context(), singleton.Conf.Oauth2.OidcIssuer)
		if err != nil {
			mygin.ShowErrorPage(c, mygin.ErrInfo{
				Code:  http.StatusBadRequest,
				Title: fmt.Sprintf("Cannot get OIDC infomaion from issuer from %s", singleton.Conf.Oauth2.OidcIssuer),
				Msg:   err.Error(),
			}, true)
			return nil
		}
		scopes := strings.Split(singleton.Conf.Oauth2.OidcScopes, ",")
		scopes = append(scopes, oidc.ScopeOpenID)
		uniqueScopes := removeDuplicates(scopes)
		return &oauth2.Config{
			ClientID:     singleton.Conf.Oauth2.ClientID,
			ClientSecret: singleton.Conf.Oauth2.ClientSecret,
			Scopes:       uniqueScopes,
			Endpoint:     provider.Endpoint(),
			RedirectURL:  oa.getRedirectURL(c),
		}
	} else {
		return &oauth2.Config{
			ClientID:     singleton.Conf.Oauth2.ClientID,
			ClientSecret: singleton.Conf.Oauth2.ClientSecret,
			Scopes:       []string{},
			Endpoint:     GitHubOauth2.Endpoint,
		}
	}
}

func (oa *oauth2controller) getOIDCProvider(ctx context.Context, issuer string) (*oidc.Provider, error) {
	oa.oidcProviderMu.Lock()
	defer oa.oidcProviderMu.Unlock()
	if oa.oidcProvider != nil && oa.oidcIssuer == issuer && time.Now().Before(oa.oidcProviderFetchedAt.Add(oidcProviderTTL)) {
		return oa.oidcProvider, nil
	}
	provider, err := oidc.NewProvider(ctx, issuer)
	if err != nil {
		return nil, err
	}
	oa.oidcIssuer = issuer
	oa.oidcProvider = provider
	oa.oidcProviderFetchedAt = time.Now()
	return provider, nil
}

func (oa *oauth2controller) getRedirectURL(c *gin.Context) string {
	scheme := "http://"
	referer := c.Request.Referer()
	if forwardedProto := c.Request.Header.Get("X-Forwarded-Proto"); forwardedProto == "https" || strings.HasPrefix(referer, "https://") {
		scheme = "https://"
	}
	return scheme + c.Request.Host + "/oauth2/callback"
}

func (oa *oauth2controller) login(c *gin.Context) {
	if singleton.Conf.Oauth2.DisableOauthLogin {
		mygin.ShowErrorPage(c, mygin.ErrInfo{
			Code:  http.StatusForbidden,
			Title: "登录失败",
			Msg:   "不支持的登陆方式",
		}, true)
		return
	}
	if !allowAuthRateLimitedCheck(c) {
		mygin.ShowErrorPage(c, mygin.ErrInfo{
			Code:  http.StatusTooManyRequests,
			Title: "登录失败",
			Msg:   "请求过于频繁，请稍后再试",
		}, true)
		return
	}

	randomString, err := utils.GenerateRandomString(32)
	if err != nil {
		mygin.ShowErrorPage(c, mygin.ErrInfo{
			Code:  http.StatusBadRequest,
			Title: "Something Wrong",
			Msg:   err.Error(),
		}, true)
		return
	}
	state, stateKey := randomString[:16], randomString[16:]
	// 将发起请求的 Host 与 state 绑定存储，供回调时校验两次 Host 一致。
	// state 在回调成功校验后即删除（一次性消费）。这是针对 CVE-2026-53523
	// （redirect_uri 可被 Host 头注入）的缓解：当受害者浏览器曾以真实域名访问过
	// 本服务、而攻击者回调时使用伪造 Host 时，两次 Host 失配会被拦截。
	// 注意：若攻击者全程自行发起并回调（受害者不经过本服务），两次 Host 可同为伪造值，
	// 此校验无法覆盖，仍需反代规范化 Host 或固定可信回调域名作为根本防御。
	singleton.Cache.Set(fmt.Sprintf("%s%s", model.CacheKeyOauth2State, stateKey), state+"|"+c.Request.Host, cache.DefaultExpiration)
	url := oa.getCommonOauth2Config(c).AuthCodeURL(state, oauth2.AccessTypeOnline)
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie(singleton.Conf.Site.CookieName+"-sk", stateKey, 60*5, "/", "", mygin.CookieSecure(c), true)
	c.HTML(http.StatusOK, "dashboard-"+singleton.Conf.Site.DashboardTheme+"/redirect", mygin.CommonEnvironment(c, gin.H{
		"URL": url,
	}))
}

func (oa *oauth2controller) callback(c *gin.Context) {
	if singleton.Conf.Oauth2.DisableOauthLogin {
		mygin.ShowErrorPage(c, mygin.ErrInfo{
			Code:  http.StatusForbidden,
			Title: "登录失败",
			Msg:   "不支持的登陆方式",
		}, true)
		return
	}
	if !allowAuthRateLimitedCheck(c) {
		mygin.ShowErrorPage(c, mygin.ErrInfo{
			Code:  http.StatusTooManyRequests,
			Title: "登录失败",
			Msg:   "请求过于频繁，请稍后再试",
		}, true)
		return
	}
	var err error
	// 验证登录跳转时的 State
	stateKey, err := c.Cookie(singleton.Conf.Site.CookieName + "-sk")
	stateKey = strings.TrimSpace(stateKey)
	stateParam := strings.TrimSpace(c.Query("state"))
	codeParam := strings.TrimSpace(c.Query("code"))
	if stateKey == "" || stateParam == "" || codeParam == "" {
		err = errors.New("非法的登录方式")
	}
	if err == nil {
		cacheKey := fmt.Sprintf("%s%s", model.CacheKeyOauth2State, stateKey)
		state, ok := singleton.Cache.Get(cacheKey)
		cachedValue, _ := state.(string)
		// 拆分为 state|host，校验 state 匹配且当前请求 Host 与发起时一致（见 login 处注释）。
		cachedState, cachedHost, _ := strings.Cut(cachedValue, "|")
		if !ok || cachedState == "" || cachedState != stateParam || cachedHost == "" || cachedHost != c.Request.Host {
			err = errors.New("非法的登录方式")
		} else {
			singleton.Cache.Delete(cacheKey)
			c.SetSameSite(http.SameSiteLaxMode)
			c.SetCookie(singleton.Conf.Site.CookieName+"-sk", "", -1, "/", "", mygin.CookieSecure(c), true)
		}
	}
	if err != nil {
		audit.Record(c, audit.TypeAuth, "OAuth login failed", "failed to complete OAuth authentication")
		mygin.ShowErrorPage(c, mygin.ErrInfo{
			Code:  http.StatusBadRequest,
			Title: "登录失败",
			Msg:   "登录过程中发生错误，请稍后重试",
		}, true)
		return
	}
	oauth2Config := oa.getCommonOauth2Config(c)
	ctx := context.Background()
	var otk *oauth2.Token
	if err == nil {
		otk, err = oauth2Config.Exchange(ctx, codeParam)
	}

	var user model.User

	if err == nil {
		if singleton.Conf.Oauth2.Type == model.ConfigTypeGitlab || singleton.Conf.Oauth2.Type == model.ConfigTypeJihulab {
			var gitlabApiClient *gitlab.Client
			if singleton.Conf.Oauth2.Type == model.ConfigTypeGitlab {
				gitlabApiClient, err = gitlab.NewOAuthClient(otk.AccessToken)
			} else {
				gitlabApiClient, err = gitlab.NewOAuthClient(otk.AccessToken, gitlab.WithBaseURL("https://jihulab.com/api/v4/"))
			}
			var u *gitlab.User
			if err == nil {
				u, _, err = gitlabApiClient.Users.CurrentUser()
			}
			if err == nil {
				user = model.NewUserFromGitlab(u)
			}
		} else if singleton.Conf.Oauth2.Type == model.ConfigTypeGitea {
			var giteaApiClient *gitea.Client
			giteaApiClient, err = gitea.NewClient(singleton.Conf.Oauth2.Endpoint, gitea.SetToken(otk.AccessToken))
			var u *gitea.User
			if err == nil {
				u, _, err = giteaApiClient.GetMyUserInfo()
			}
			if err == nil {
				user = model.NewUserFromGitea(u)
			}
		} else if singleton.Conf.Oauth2.Type == model.ConfigTypeCloudflare {
			client := oauth2Config.Client(context.Background(), otk)
			resp, err := client.Get(fmt.Sprintf("%s/cdn-cgi/access/sso/oidc/%s/userinfo", singleton.Conf.Oauth2.Endpoint, singleton.Conf.Oauth2.ClientID))
			if err == nil {
				defer resp.Body.Close()
				var cloudflareUserInfo *cloudflare.UserInfo
				if err := utils.Json.NewDecoder(resp.Body).Decode(&cloudflareUserInfo); err == nil {
					user = cloudflareUserInfo.MapToNezhaUser()
				}
			}
		} else if singleton.Conf.Oauth2.Type == model.ConfigTypeOidc {
			provider, providerErr := oa.getOIDCProvider(c.Request.Context(), singleton.Conf.Oauth2.OidcIssuer)
			if providerErr != nil {
				err = providerErr
			}
			var userInfo *oidc.UserInfo
			if err == nil {
				userInfo, err = provider.UserInfo(c.Request.Context(), oauth2.StaticTokenSource(otk))
			}
			if err == nil {
				loginClaim := singleton.Conf.Oauth2.OidcLoginClaim
				groupClain := singleton.Conf.Oauth2.OidcGroupClaim
				adminGroups := strings.Split(singleton.Conf.Oauth2.AdminGroups, ",")
				autoCreate := singleton.Conf.Oauth2.OidcAutoCreate
				var oidceUserInfo *myOidc.UserInfo
				if err := userInfo.Claims(&oidceUserInfo); err == nil {
					user = oidceUserInfo.MapToNezhaUser(loginClaim, groupClain, adminGroups, autoCreate)
				}
			}
		} else {
			var client *GitHubAPI.Client
			oc := oauth2Config.Client(ctx, otk)
			if singleton.Conf.Oauth2.Type == model.ConfigTypeGitee {
				baseURL, _ := url.Parse("https://gitee.com/api/v5/")
				uploadURL, _ := url.Parse("https://gitee.com/api/v5/uploads/")
				client = GitHubAPI.NewClient(oc)
				client.BaseURL = baseURL
				client.UploadURL = uploadURL
			} else {
				client = GitHubAPI.NewClient(oc)
			}
			var gu *GitHubAPI.User
			gu, _, err = client.Users.Get(ctx, "")
			if err == nil {
				user = model.NewUserFromGitHub(gu)
			}
		}
	}
	if err == nil && user.Login == "" {
		err = errors.New("获取用户信息失败")
	}

	if err != nil || user.Login == "" {
		audit.Record(c, audit.TypeAuth, "OAuth login failed", "failed to complete OAuth authentication")
		mygin.ShowErrorPage(c, mygin.ErrInfo{
			Code:  http.StatusBadRequest,
			Title: "登录失败",
			Msg:   "登录过程中发生错误，请稍后重试",
		}, true)
		return
	}
	var isAdmin bool

	if user.SuperAdmin {
		isAdmin = true
	} else {
		for _, admin := range strings.Split(singleton.Conf.Oauth2.Admin, ",") {
			if strings.EqualFold(user.Login, strings.TrimSpace(admin)) {
				isAdmin = true
				break
			}
		}
	}
	if !isAdmin {
		audit.Record(c, audit.TypeAuth, "OAuth login failed", "user is not an administrator: "+user.Login)
		mygin.ShowErrorPage(c, mygin.ErrInfo{
			Code:  http.StatusBadRequest,
			Title: "登录失败",
			Msg:   fmt.Sprintf("错误信息：%s", "该用户不是本站点管理员，无法登录"),
		}, true)
		return
	}
	user.SuperAdmin = true
	if singleton.Conf.TwoFactorActive() {
		oa.beginTwoFactor(c, user, twoFactorLoginMethodOAuth)
		return
	}
	oa.issueOAuthSession(c, &user)
}

// beginTwoFactor 为已通过第一阶段身份核验的用户签发一次性二次验证 ticket。
func (oa *oauth2controller) beginTwoFactor(c *gin.Context, user model.User, method twoFactorLoginMethod) {
	ticket, err := utils.GenerateRandomString(48)
	if err != nil {
		mygin.ShowErrorPage(c, mygin.ErrInfo{
			Code:  http.StatusBadRequest,
			Title: "Something wrong",
			Msg:   err.Error(),
		}, true)
		return
	}
	singleton.Cache.Set(twoFactorCachePrefix+ticket, &twoFactorTicket{
		User:   user,
		Method: method,
	}, twoFactorTTL)
	oa.renderTwoFactor(c, http.StatusOK, ticket, "")
}

// renderTwoFactor 使用默认登录页的第二阶段状态展示二次验证。
// 自定义主题不一定支持该状态，因此这里固定复用内置登录模板。
func (oa *oauth2controller) renderTwoFactor(c *gin.Context, status int, ticket, errorMessage string) {
	c.HTML(status, "dashboard-default/login", mygin.CommonEnvironment(c, gin.H{
		"Title": singleton.Localizer.MustLocalize(&i18n.LocalizeConfig{
			MessageID: "TwoFactorLoginTitle",
		}),
		"SecondFactorMode":  true,
		"SecondFactorError": errorMessage,
		"Ticket":            ticket,
	}))
}

// issueOAuthSession 为已通过全部校验的 OAuth 用户发放会话并跳转首页。
func (oa *oauth2controller) issueOAuthSession(c *gin.Context, user *model.User) {
	sessionToken, err := utils.NewSessionToken()
	if err != nil {
		mygin.ShowErrorPage(c, mygin.ErrInfo{
			Code:  http.StatusBadRequest,
			Title: "Something wrong",
			Msg:   err.Error(),
		}, true)
		return
	}
	user.Token = sessionToken.Hash
	user.TokenExpired = time.Now().UTC().AddDate(0, 0, 3)
	singleton.DB.Save(user)
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie(singleton.Conf.Site.CookieName, sessionToken.Plain, 60*60*24*3, "/", "", mygin.CookieSecure(c), true)
	mygin.SetCSRFCookie(c)
	c.HTML(http.StatusOK, "dashboard-"+singleton.Conf.Site.DashboardTheme+"/redirect", mygin.CommonEnvironment(c, gin.H{
		"URL": "/",
	}))
	audit.Record(c, audit.TypeAuth, "OAuth login succeeded", "user: "+user.Login)
}

// twoFactorVerify 校验密码和 OAuth 登录共用的二次验证 ticket。
func (oa *oauth2controller) twoFactorVerify(c *gin.Context) {
	if !singleton.Conf.TwoFactorActive() {
		mygin.ShowErrorPage(c, mygin.ErrInfo{
			Code:  http.StatusForbidden,
			Title: "登录失败",
			Msg:   "不支持的登陆方式",
		}, true)
		return
	}
	if !allowAuthRateLimitedCheck(c) {
		oa.renderTwoFactor(c, http.StatusTooManyRequests, strings.TrimSpace(c.PostForm("ticket")), "请求过于频繁，请稍后再试")
		return
	}

	ticket := strings.TrimSpace(c.PostForm("ticket"))
	code := strings.TrimSpace(c.PostForm("otp"))
	if ticket == "" || code == "" {
		oa.renderTwoFactor(c, http.StatusBadRequest, "", "登录会话无效，请重新登录")
		return
	}

	cacheKey := twoFactorCachePrefix + ticket
	twoFactorMu.Lock()
	value, ok := singleton.Cache.Get(cacheKey)
	t, isTicket := value.(*twoFactorTicket)
	if !ok || !isTicket {
		twoFactorMu.Unlock()
		audit.Record(c, audit.TypeAuth, "Two-factor login failed", "invalid or expired ticket")
		oa.renderTwoFactor(c, http.StatusBadRequest, "", "登录会话已过期，请重新登录")
		return
	}

	// TOTP 校验；同一 ticket 限制试错次数，防 6 位码枚举。
	if !totp.Validate(singleton.Conf.Site.TwoFactorSecret, code, 1) {
		t.Attempts++
		if t.Attempts >= twoFactorMaxAttempts {
			singleton.Cache.Delete(cacheKey)
			twoFactorMu.Unlock()
			audit.Record(c, audit.TypeAuth, "Two-factor login failed", "too many invalid codes, ticket revoked, user: "+t.User.Login)
			oa.renderTwoFactor(c, http.StatusBadRequest, "", "连续错误次数过多，请重新登录")
			return
		}
		singleton.Cache.Set(cacheKey, t, twoFactorTTL)
		twoFactorMu.Unlock()
		audit.Record(c, audit.TypeAuth, "Two-factor login failed", "invalid two-factor code, user: "+t.User.Login)
		oa.renderTwoFactor(c, http.StatusBadRequest, ticket, "动态验证码错误，请重新输入")
		return
	}

	// 校验通过：立即作废 ticket（一次性消费），发放会话。
	singleton.Cache.Delete(cacheKey)
	verifiedTicket := *t
	twoFactorMu.Unlock()
	audit.Record(c, audit.TypeAuth, "Two-factor login passed", "user: "+verifiedTicket.User.Login)
	switch verifiedTicket.Method {
	case twoFactorLoginMethodPassword:
		if !singleton.Conf.PasswordLoginActive() {
			oa.renderTwoFactor(c, http.StatusForbidden, "", "密码登录已被禁用，请重新登录")
			return
		}
		oa.issuePasswordSession(c, &verifiedTicket.User)
	case twoFactorLoginMethodOAuth:
		if singleton.Conf.Oauth2.DisableOauthLogin {
			oa.renderTwoFactor(c, http.StatusForbidden, "", "OAuth 登录已被禁用，请重新登录")
			return
		}
		oa.issueOAuthSession(c, &verifiedTicket.User)
	default:
		oa.renderTwoFactor(c, http.StatusBadRequest, "", "登录会话无效，请重新登录")
	}
}

func removeDuplicates(elements []string) []string {
	encountered := map[string]bool{}
	result := []string{}

	for _, v := range elements {
		if !encountered[v] {
			encountered[v] = true
			result = append(result, v)
		}
	}
	return result
}
