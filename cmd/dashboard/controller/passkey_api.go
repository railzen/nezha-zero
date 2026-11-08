package controller

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	"github.com/gin-gonic/gin/binding"
	"golang.org/x/crypto/bcrypt"

	"github.com/railzen/nezha-zero/model"
	"github.com/railzen/nezha-zero/pkg/audit"
	"github.com/railzen/nezha-zero/pkg/mygin"
	"github.com/railzen/nezha-zero/pkg/utils"
	"github.com/railzen/nezha-zero/service/singleton"
)

// passkey 管理通行密钥（仅超管）。通行密钥只能在设置页添加：
//   - verify-options：签发当前会话的通行密钥二次验证选项（敏感操作授权、安全设置、管理通行密钥等共用）
//   - grant：用通行密钥代替动态验证码，为当前会话授予敏感操作授权
//   - register-options：校验二次验证（已启用时）后签发注册选项
//   - register：保存注册结果
//   - delete：校验二次验证后删除，并注销其他会话
func (ma *memberAPI) passkey(c *gin.Context) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 2*passkeyMaxPayloadBytes)
	var req struct {
		Operation  string          `json:"operation"`
		Name       string          `json:"name"`
		ID         uint64          `json:"id"`
		Code       string          `json:"code"`
		Passkey    string          `json:"passkey"`
		Password   string          `json:"password"`
		Credential json.RawMessage `json:"credential"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusOK, model.Response{Code: http.StatusBadRequest, Message: "请求错误"})
		return
	}
	binding, ok := passkeySessionBinding(c)
	if !ok {
		c.JSON(http.StatusOK, model.Response{Code: http.StatusForbidden, Message: "当前登录会话不支持通行密钥"})
		return
	}
	user := c.MustGet(model.CtxKeyAuthorizedUser).(*model.User)

	switch strings.TrimSpace(req.Operation) {
	case "verify-options":
		if !singleton.PasskeyEnabled() {
			c.JSON(http.StatusOK, model.Response{Code: http.StatusBadRequest, Message: "未添加通行密钥"})
			return
		}
		assertion, err := beginPasskeyAssertion(c, passkeyPurposeSessionVerify, binding)
		if err != nil {
			c.JSON(http.StatusOK, model.Response{Code: http.StatusBadRequest, Message: err.Error()})
			return
		}
		c.JSON(http.StatusOK, model.Response{Code: http.StatusOK, Result: assertion})

	case "grant":
		ma.passkeyGrant(c, req.Passkey)

	case "register-options":
		name, message := normalizePasskeyName(req.Name)
		if message != "" {
			c.JSON(http.StatusOK, model.Response{Code: http.StatusBadRequest, Message: message})
			return
		}
		// 通行密钥可直接登录面板，属于持久凭据，且不会随改密码失效：
		// 已开启密码登录时先校验当前管理员密码，防止被盗会话无声地植入后门；已启用双重验证时再完成二次验证
		if !ma.passkeyRequirePassword(c, req.Password) {
			return
		}
		if !ma.passkeyRequireTwoFactor(c, req.Code, req.Passkey, "Passkey register failed") {
			return
		}
		creation, err := beginPasskeyRegistration(c, binding, user.Login, name)
		if err != nil {
			c.JSON(http.StatusOK, model.Response{Code: http.StatusBadRequest, Message: err.Error()})
			return
		}
		c.JSON(http.StatusOK, model.Response{Code: http.StatusOK, Result: creation})

	case "register":
		row, err := finishPasskeyRegistration(c, binding, req.Credential)
		if err != nil {
			audit.Record(c, audit.TypeSecurity, "Passkey register failed", err.Error())
			c.JSON(http.StatusOK, model.Response{Code: http.StatusBadRequest, Message: err.Error()})
			return
		}
		c.JSON(http.StatusOK, model.Response{Code: http.StatusOK, Message: "通行密钥已添加"})
		audit.Record(c, audit.TypeSecurity, "Passkey registered",
			fmt.Sprintf("user: %s, passkey: %q, domain: %s", row.UserLogin, row.Name, row.RPID))

	case "delete":
		if req.ID == 0 {
			c.JSON(http.StatusOK, model.Response{Code: http.StatusBadRequest, Message: "请求错误"})
			return
		}
		var row model.Passkey
		if err := singleton.DB.Where("id = ?", req.ID).Limit(1).Find(&row).Error; err != nil || row.ID == 0 {
			c.JSON(http.StatusOK, model.Response{Code: http.StatusBadRequest, Message: "通行密钥不存在"})
			return
		}
		if !ma.passkeyRequireTwoFactor(c, req.Code, req.Passkey, "Passkey delete failed") {
			return
		}
		if err := singleton.DB.Unscoped().Delete(&model.Passkey{}, "id = ?", row.ID).Error; err != nil {
			c.JSON(http.StatusOK, model.Response{Code: http.StatusInternalServerError, Message: "删除失败，请稍后重试"})
			return
		}
		singleton.RefreshPasskeyCount()
		// 删除通常意味着设备丢失或凭据不再可信：注销除当前会话外的全部会话，并清空敏感操作授权
		singleton.DB.Unscoped().Where("token <> ?", user.Token).Delete(&model.User{})
		mygin.ResetTwoFactorGrants()
		// 返回被删凭据，前端据此通过 Signal API 让当前浏览器同步移除
		c.JSON(http.StatusOK, model.Response{
			Code:    http.StatusOK,
			Message: "通行密钥已删除",
			Result:  gin.H{"rpId": row.RPID, "credentialId": row.CredentialID},
		})
		audit.Record(c, audit.TypeSecurity, "Passkey deleted",
			fmt.Sprintf("passkey: %q, owner: %s, domain: %s; other sessions revoked", row.Name, row.UserLogin, row.RPID))

	default:
		c.JSON(http.StatusOK, model.Response{Code: http.StatusBadRequest, Message: "未知操作"})
	}
}

// passkeyGrant 校验通行密钥，为当前会话授予 mygin.TwoFactorGrantTTL 的敏感操作授权；与 totpGrant 对应。
func (ma *memberAPI) passkeyGrant(c *gin.Context, assertion string) {
	if !mygin.TwoFactorGrantRequired() {
		c.JSON(http.StatusOK, model.Response{Code: http.StatusOK})
		return
	}
	if !allowAuthRateLimitedCheck(c) {
		c.JSON(http.StatusOK, model.Response{Code: http.StatusTooManyRequests, Message: "请求过于频繁，请稍后再试"})
		return
	}
	if !singleton.PasskeyEnabled() {
		c.JSON(http.StatusOK, model.Response{Code: http.StatusBadRequest, Message: "未添加通行密钥"})
		return
	}
	if strings.TrimSpace(assertion) == "" {
		c.JSON(http.StatusOK, model.Response{Code: http.StatusBadRequest, Message: "请完成通行密钥验证"})
		return
	}
	if message := verifySessionPasskey(c, assertion); message != "" {
		audit.Record(c, audit.TypeSecurity, "Two-factor grant verification failed", "invalid passkey")
		c.JSON(http.StatusOK, model.Response{Code: http.StatusBadRequest, Message: message})
		return
	}
	if !mygin.GrantTwoFactor(c) {
		c.JSON(http.StatusOK, model.Response{Code: http.StatusForbidden, Message: "当前登录会话不支持敏感操作授权"})
		return
	}
	audit.Record(c, audit.TypeSecurity, "Two-factor grant issued",
		fmt.Sprintf("sensitive operations unlocked for %d minutes, method: passkey", int(mygin.TwoFactorGrantTTL.Minutes())))
	c.JSON(http.StatusOK, model.Response{
		Code: http.StatusOK,
		Result: gin.H{
			"enabled":        true,
			"grantRemaining": int(mygin.TwoFactorGrantTTL.Seconds()),
		},
	})
}

// passkeyRequireTwoFactor 已启用双重验证（绑定 TOTP）时校验动态验证码或通行密钥；未通过时写入响应并返回 false。
func (ma *memberAPI) passkeyRequireTwoFactor(c *gin.Context, code, passkeyAssertion, auditAction string) bool {
	if !singleton.Conf.TwoFactorActive() {
		return true
	}
	if !allowAuthRateLimitedCheck(c) {
		c.JSON(http.StatusOK, model.Response{Code: http.StatusTooManyRequests, Message: "请求过于频繁，请稍后再试"})
		return false
	}
	if message := verifySessionSecondFactor(c, code, passkeyAssertion); message != "" {
		if strings.TrimSpace(code) != "" || strings.TrimSpace(passkeyAssertion) != "" {
			audit.Record(c, audit.TypeSecurity, auditAction, "invalid two-factor code or passkey")
		}
		c.JSON(http.StatusOK, model.Response{Code: http.StatusBadRequest, Message: message})
		return false
	}
	return true
}

// passkeyRequirePassword 已开启密码登录时校验当前管理员密码；未通过时写入响应并返回 false。
// 与登录页是否显示密码登录框的判断一致；未开启密码登录（仅 OAuth 登录）时直接放行。
// 密码错误与动态验证码共用每会话试错计数，防止持有被盗会话的人借此持续猜密码。
func (ma *memberAPI) passkeyRequirePassword(c *gin.Context, password string) bool {
	const tooManyFailures = "验证错误次数过多，请重新登录后再试"
	if !singleton.Conf.PasswordLoginActive() {
		return true
	}
	hash := singleton.Conf.Site.AdminPassword
	if !allowAuthRateLimitedCheck(c) {
		c.JSON(http.StatusOK, model.Response{Code: http.StatusTooManyRequests, Message: "请求过于频繁，请稍后再试"})
		return false
	}
	if mygin.TwoFactorGrantLocked(c) {
		c.JSON(http.StatusOK, model.Response{Code: http.StatusForbidden, Message: tooManyFailures})
		return false
	}
	if password == "" {
		c.JSON(http.StatusOK, model.Response{Code: http.StatusBadRequest, Message: "请输入当前管理员密码"})
		return false
	}
	if bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) != nil {
		message := "管理员密码错误"
		if mygin.RecordTwoFactorGrantFailure(c) <= 0 {
			message = tooManyFailures
		}
		audit.Record(c, audit.TypeSecurity, "Passkey register failed", "invalid admin password")
		c.JSON(http.StatusOK, model.Response{Code: http.StatusBadRequest, Message: message})
		return false
	}
	return true
}

// normalizePasskeyName 去除控制字符并限制长度；为空时按添加时间生成默认名称。
func normalizePasskeyName(name string) (string, string) {
	name = strings.TrimSpace(strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, name))
	if name == "" {
		return "通行密钥 " + time.Now().In(singleton.Loc).Format("2006-01-02 15:04"), ""
	}
	if utf8.RuneCountInString(name) > passkeyMaxNameRunes {
		return "", fmt.Sprintf("名称不能超过 %d 个字符", passkeyMaxNameRunes)
	}
	return name, ""
}

// passkeyView 设置页展示的通行密钥信息。
type passkeyView struct {
	ID         uint64
	Name       string
	UserLogin  string
	RPID       string
	CreatedAt  string
	LastUsedAt string
	CurrentRP  bool // 是否属于当前访问域名，只有当前域名下的通行密钥可在本页使用
}

func listPasskeyViews(c *gin.Context) []passkeyView {
	var rows []model.Passkey
	if err := singleton.DB.Order("id").Find(&rows).Error; err != nil {
		return nil
	}
	rp, rpErr := passkeyRPFromRequest(c)
	views := make([]passkeyView, 0, len(rows))
	for _, row := range rows {
		v := passkeyView{
			ID:        row.ID,
			Name:      row.Name,
			UserLogin: row.UserLogin,
			RPID:      row.RPID,
			CreatedAt: row.CreatedAt.In(singleton.Loc).Format("2006-01-02 15:04"),
			CurrentRP: rpErr == nil && row.RPID == rp.ID,
		}
		if row.LastUsedAt != nil {
			v.LastUsedAt = row.LastUsedAt.In(singleton.Loc).Format("2006-01-02 15:04")
		}
		views = append(views, v)
	}
	return views
}

// passkeyLoginOptions 签发通行密钥验证选项。请求体为空时用于免密码登录；
// 携带 ticket 时用于密码 / OAuth 登录后的二次验证，challenge 与该 ticket 绑定。
func (oa *oauth2controller) passkeyLoginOptions(c *gin.Context) {
	if !allowAuthRateLimitedCheck(c) {
		c.JSON(http.StatusOK, model.Response{Code: http.StatusTooManyRequests, Message: "请求过于频繁，请稍后再试"})
		return
	}
	if !singleton.PasskeyEnabled() {
		c.JSON(http.StatusOK, model.Response{Code: http.StatusForbidden, Message: "未添加通行密钥"})
		return
	}
	// 请求体只有可选的 ticket，未登录即可调用，单独限制大小
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, passkeyOptionsMaxBodyBytes)
	var req struct {
		Ticket string `json:"ticket"`
	}
	// 空请求体即免密码登录，忽略解析错误
	_ = c.ShouldBindJSON(&req)

	purpose, binding := passkeyPurposeLogin, ""
	if ticket := strings.TrimSpace(req.Ticket); ticket == "" {
		if !singleton.Conf.PasswordLoginActive() {
			c.JSON(http.StatusOK, model.Response{Code: http.StatusForbidden, Message: "未开启密码登录，通行密钥仅可用于二次验证"})
			return
		}
	} else {
		if !singleton.Conf.TwoFactorActive() {
			c.JSON(http.StatusOK, model.Response{Code: http.StatusForbidden, Message: "双重验证未启用"})
			return
		}
		if _, ok := singleton.Cache.Get(twoFactorCachePrefix + ticket); !ok {
			c.JSON(http.StatusOK, model.Response{Code: http.StatusBadRequest, Message: "登录会话已过期，请重新登录"})
			return
		}
		purpose, binding = passkeyPurposeLoginSecondFactor, passkeyTicketBinding(ticket)
	}
	assertion, err := beginPasskeyAssertion(c, purpose, binding)
	if err != nil {
		c.JSON(http.StatusOK, model.Response{Code: http.StatusBadRequest, Message: err.Error()})
		return
	}
	c.JSON(http.StatusOK, model.Response{Code: http.StatusOK, Result: assertion})
}

// passkeyLogin 通行密钥登录。与 /passkeyinfo 对应：
//   - JSON 请求体（通行密钥断言）：免密码登录，返回 JSON；
//   - 表单（ticket + passkey）：密码 / OAuth 登录后用通行密钥代替动态验证码完成二次验证，返回页面。
func (oa *oauth2controller) passkeyLogin(c *gin.Context) {
	if c.ContentType() == binding.MIMEPOSTForm {
		oa.passkeySecondFactor(c)
		return
	}
	oa.passkeyPasswordlessLogin(c)
}

// passkeyPasswordlessLogin 通行密钥免密码登录。通行密钥注册时要求用户验证（指纹 / 面容 / PIN），
// 单独即构成多因素，验证通过即视为已通过双重验证，直接发放会话，不再要求 TOTP。
// 仅在开启密码登录时可用：此时添加通行密钥必须验证管理员密码；未开启密码登录（仅 OAuth）时
// 添加不验证密码，通行密钥只能代替 TOTP 完成二次验证，避免被盗会话植入可直接登录的后门。
func (oa *oauth2controller) passkeyPasswordlessLogin(c *gin.Context) {
	if !allowAuthRateLimitedCheck(c) {
		c.JSON(http.StatusOK, model.Response{Code: http.StatusTooManyRequests, Message: "请求过于频繁，请稍后再试"})
		return
	}
	if !singleton.PasskeyEnabled() {
		c.JSON(http.StatusOK, model.Response{Code: http.StatusForbidden, Message: "未添加通行密钥"})
		return
	}
	payload, err := io.ReadAll(io.LimitReader(c.Request.Body, passkeyMaxPayloadBytes+1))
	if err != nil {
		c.JSON(http.StatusOK, model.Response{Code: http.StatusBadRequest, Message: "请求错误"})
		return
	}
	if len(bytes.TrimSpace(payload)) == 0 {
		c.JSON(http.StatusOK, model.Response{Code: http.StatusBadRequest, Message: "请完成通行密钥验证"})
		return
	}
	if !singleton.Conf.PasswordLoginActive() {
		c.JSON(http.StatusOK, model.Response{Code: http.StatusForbidden, Message: "未开启密码登录，通行密钥仅可用于二次验证"})
		return
	}
	row, err := finishPasskeyAssertion(c, string(payload), passkeyPurposeLogin, "")
	if err != nil {
		audit.Record(c, audit.TypeAuth, "Passkey login failed", err.Error())
		c.JSON(http.StatusOK, model.Response{Code: http.StatusForbidden, Message: err.Error()})
		return
	}
	// 管理员名单是超管身份的唯一来源：通行密钥的归属登录名被移出名单后不能再登录
	if !isConfiguredAdmin(row.UserLogin) {
		audit.Record(c, audit.TypeAuth, "Passkey login failed", "passkey owner is not an administrator: "+row.UserLogin)
		c.JSON(http.StatusOK, model.Response{Code: http.StatusForbidden, Message: "该通行密钥所属用户已不是本站管理员"})
		return
	}
	user := model.User{
		Login:      row.UserLogin,
		Name:       row.UserLogin,
		SuperAdmin: true,
	}
	// 与密码登录相同的独立会话：每次登录一条记录，3 天有效
	sessionToken, err := utils.NewSessionToken()
	if err != nil {
		c.JSON(http.StatusOK, model.Response{Code: http.StatusInternalServerError, Message: "系统错误"})
		return
	}
	user.Token = sessionToken.Hash
	user.TokenExpired = time.Now().UTC().AddDate(0, 0, 3)
	if err := user.SavePasswordSession(singleton.DB); err != nil {
		c.JSON(http.StatusOK, model.Response{Code: http.StatusInternalServerError, Message: "系统错误"})
		return
	}
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie(singleton.Conf.Site.CookieName, sessionToken.Plain, 60*60*24*3, "/", "", mygin.CookieSecure(c), true)
	mygin.SetCSRFCookie(c)

	c.JSON(http.StatusOK, model.Response{Code: http.StatusOK})
	audit.Record(c, audit.TypeAuth, "Passkey login succeeded", fmt.Sprintf("user: %s, passkey: %q", user.Login, row.Name))
}

// passkeySecondFactor 登录二次验证：用通行密钥代替动态验证码。流程与 twoFactorVerify 一致，
// 共用同一 ticket 的试错次数；通过后按 ticket 记录的登录方式发放会话。
func (oa *oauth2controller) passkeySecondFactor(c *gin.Context) {
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
	passkeyPayload := strings.TrimSpace(c.PostForm("passkey"))
	if ticket == "" || passkeyPayload == "" {
		oa.renderTwoFactor(c, http.StatusBadRequest, "", "登录会话无效，请重新登录")
		return
	}
	// 未添加通行密钥：直接返回并保留 ticket，可改用动态验证码，不计入试错
	if !singleton.PasskeyEnabled() {
		oa.renderTwoFactor(c, http.StatusBadRequest, ticket, "未添加通行密钥")
		return
	}

	// 通行密钥的 challenge 绑定本 ticket 且一次性消费；在加锁前完成校验，避免持全局锁访问数据库
	_, passkeyErr := finishPasskeyAssertion(c, passkeyPayload, passkeyPurposeLoginSecondFactor, passkeyTicketBinding(ticket))

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

	// 失败与动态验证码共用同一 ticket 的试错次数
	if passkeyErr != nil {
		t.Attempts++
		if t.Attempts >= twoFactorMaxAttempts {
			singleton.Cache.Delete(cacheKey)
			twoFactorMu.Unlock()
			audit.Record(c, audit.TypeAuth, "Two-factor login failed", "too many failed attempts, ticket revoked, user: "+t.User.Login)
			oa.renderTwoFactor(c, http.StatusBadRequest, "", "连续错误次数过多，请重新登录")
			return
		}
		singleton.Cache.Set(cacheKey, t, twoFactorTTL)
		twoFactorMu.Unlock()
		audit.Record(c, audit.TypeAuth, "Two-factor login failed", "passkey verification failed, user: "+t.User.Login)
		oa.renderTwoFactor(c, http.StatusBadRequest, ticket, passkeyErr.Error())
		return
	}

	// 校验通过：立即作废 ticket（一次性消费），发放会话。
	singleton.Cache.Delete(cacheKey)
	verifiedTicket := *t
	twoFactorMu.Unlock()
	audit.Record(c, audit.TypeAuth, "Two-factor login passed", "user: "+verifiedTicket.User.Login+", method: passkey")
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
