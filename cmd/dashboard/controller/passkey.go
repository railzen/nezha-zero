package controller

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"

	"github.com/railzen/nezha-zero/model"
	"github.com/railzen/nezha-zero/pkg/audit"
	"github.com/railzen/nezha-zero/pkg/mygin"
	"github.com/railzen/nezha-zero/service/singleton"
)

const (
	passkeyCeremonyPrefix  = "passkey_ceremony_"
	passkeyCeremonyTimeout = 5 * time.Minute
	// passkeyMaxCount 全站通行密钥数量上限。
	passkeyMaxCount = 20
	// passkeyMaxNameRunes 通行密钥名称的最大字符数。
	passkeyMaxNameRunes = 32
	// passkeyMaxPayloadBytes 客户端回传的断言 / 证明 JSON 的最大字节数。
	passkeyMaxPayloadBytes = 64 << 10
	passkeyUserHandleBytes = 32
)

// passkeyPurpose 一次 WebAuthn 仪式的用途。challenge 只能用于签发时声明的用途和绑定对象。
type passkeyPurpose uint8

const (
	// passkeyPurposeLogin 免密码登录（可发现凭据），不绑定任何会话。
	passkeyPurposeLogin passkeyPurpose = iota + 1
	// passkeyPurposeLoginSecondFactor 密码 / OAuth 登录后的二次验证，绑定 2FA ticket。
	passkeyPurposeLoginSecondFactor
	// passkeyPurposeSessionVerify 已登录会话的二次验证（敏感操作授权、安全设置变更），绑定会话。
	passkeyPurposeSessionVerify
	// passkeyPurposeRegister 注册新通行密钥，绑定会话。
	passkeyPurposeRegister
)

// passkeyCeremony 服务端保存的仪式状态，以 challenge 为键，仅存于内存缓存且一次性消费。
type passkeyCeremony struct {
	Purpose   passkeyPurpose
	Binding   string
	Session   webauthn.SessionData
	Name      string // 仅注册：通行密钥名称
	UserLogin string // 仅注册：归属的管理员登录名
}

var passkeyCeremonyMu sync.Mutex

var (
	errPasskeyUnavailable = errors.New("通行密钥需要通过 HTTPS 域名访问面板")
	errPasskeyNotFound    = errors.New("当前域名下没有可用的通行密钥")
	errPasskeyExpired     = errors.New("通行密钥验证已过期，请重试")
	errPasskeyInvalid     = errors.New("通行密钥验证失败，请重试")
	// errPasskeyLoginDisabled 未开启密码登录时，通行密钥只能代替 TOTP 完成二次验证
	errPasskeyLoginDisabled = errors.New("未开启密码登录，通行密钥仅可用于二次验证")
)

// passkeyFailure 对外只展示 msg，cause 用于服务端日志排查（如 Origin 不匹配）。
type passkeyFailure struct {
	msg   string
	cause error
}

func (e *passkeyFailure) Error() string { return e.msg }
func (e *passkeyFailure) Unwrap() error { return e.cause }

func passkeyFail(public error, cause error) error {
	if cause != nil {
		detail := cause.Error()
		var pe *protocol.Error
		if errors.As(cause, &pe) {
			detail = pe.Details
			if pe.DevInfo != "" {
				detail += ": " + pe.DevInfo
			}
		}
		log.Printf("NEZHA>> passkey ceremony failed: %s", detail)
	}
	return &passkeyFailure{msg: public.Error(), cause: cause}
}

// passkeyRP 当前请求对应的 WebAuthn 依赖方：RPID 为访问域名，Origin 为浏览器所见来源。
// Host 头即使被伪造也无法绕过：签名由认证器按真实来源的 RPID 生成，与伪造值不会匹配。
type passkeyRP struct {
	ID     string
	Origin string
}

func passkeyRPFromRequest(c *gin.Context) (passkeyRP, error) {
	host := strings.ToLower(strings.TrimSpace(c.Request.Host))
	hostname, port := host, ""
	if h, p, err := net.SplitHostPort(host); err == nil {
		hostname, port = h, p
	}
	// WebAuthn 不允许以 IP 作为 RPID
	if hostname == "" || net.ParseIP(strings.Trim(hostname, "[]")) != nil || protocol.ValidateRPID(hostname) != nil {
		return passkeyRP{}, errPasskeyUnavailable
	}
	// 浏览器只在安全上下文中提供 WebAuthn：除 http://localhost 外均为 HTTPS
	scheme := "https"
	if hostname == "localhost" && !mygin.CookieSecure(c) {
		scheme = "http"
	}
	if (scheme == "https" && port == "443") || (scheme == "http" && port == "80") {
		port = ""
	}
	origin := scheme + "://" + hostname
	if port != "" {
		origin += ":" + port
	}
	return passkeyRP{ID: hostname, Origin: origin}, nil
}

func newPasskeyWebAuthn(rp passkeyRP) (*webauthn.WebAuthn, error) {
	name := singleton.Conf.Site.Brand
	if name == "" {
		name = "Nezha"
	}
	timeout := webauthn.TimeoutConfig{Enforce: true, Timeout: passkeyCeremonyTimeout, TimeoutUVD: passkeyCeremonyTimeout}
	return webauthn.New(&webauthn.Config{
		RPID:                  rp.ID,
		RPDisplayName:         name,
		RPOrigins:             []string{rp.Origin},
		AttestationPreference: protocol.PreferNoAttestation,
		// 通行密钥必须可发现（支持免用户名登录）且每次使用都需用户验证（指纹 / 面容 / PIN），
		// 因此单独使用即可视为多因素，免密码登录后不再要求二次验证。
		AuthenticatorSelection: protocol.AuthenticatorSelection{
			RequireResidentKey: protocol.ResidentKeyRequired(),
			ResidentKey:        protocol.ResidentKeyRequirementRequired,
			UserVerification:   protocol.VerificationRequired,
		},
		Timeouts: webauthn.TimeoutsConfig{Login: timeout, Registration: timeout},
		// 未请求任何扩展；部分密码管理器会附带未请求的扩展输出，忽略即可
		ExtensionsUnsolicitedOutputPolicy: protocol.UnsolicitedOutputPolicyIgnore,
	})
}

// passkeyAccount 实现 webauthn.User。面板只有"站点管理员"一个账号，全站通行密钥共用一个用户句柄。
type passkeyAccount struct {
	handle []byte
	login  string
	creds  []webauthn.Credential
}

func (a *passkeyAccount) WebAuthnID() []byte                         { return a.handle }
func (a *passkeyAccount) WebAuthnName() string                       { return a.login }
func (a *passkeyAccount) WebAuthnDisplayName() string                { return a.login }
func (a *passkeyAccount) WebAuthnCredentials() []webauthn.Credential { return a.creds }

func decodePasskeyCredential(row *model.Passkey) (webauthn.Credential, error) {
	var cred webauthn.Credential
	err := json.Unmarshal([]byte(row.Credential), &cred)
	return cred, err
}

// loadPasskeysForRP 读取某域名下的全部通行密钥，返回值 rows 与 creds 一一对应（无法解析的记录被跳过）。
func loadPasskeysForRP(rpID string) ([]model.Passkey, []webauthn.Credential, error) {
	var all []model.Passkey
	if err := singleton.DB.Where("rp_id = ?", rpID).Order("id").Find(&all).Error; err != nil {
		return nil, nil, err
	}
	rows := make([]model.Passkey, 0, len(all))
	creds := make([]webauthn.Credential, 0, len(all))
	for i := range all {
		cred, err := decodePasskeyCredential(&all[i])
		if err != nil {
			log.Printf("NEZHA>> passkey %d has invalid credential data: %v", all[i].ID, err)
			continue
		}
		rows = append(rows, all[i])
		creds = append(creds, cred)
	}
	return rows, creds, nil
}

// existingPasskeyHandle 已有通行密钥使用的用户句柄；没有通行密钥时返回 nil。
func existingPasskeyHandle() ([]byte, error) {
	var rows []model.Passkey
	if err := singleton.DB.Select("user_handle").Order("id").Limit(1).Find(&rows).Error; err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, nil
	}
	handle, err := base64.RawURLEncoding.DecodeString(rows[0].UserHandle)
	if err != nil || len(handle) == 0 {
		return nil, fmt.Errorf("invalid passkey user handle: %v", err)
	}
	return handle, nil
}

func storePasskeyCeremony(ce *passkeyCeremony) {
	singleton.Cache.Set(passkeyCeremonyPrefix+ce.Session.Challenge, ce, passkeyCeremonyTimeout)
}

// consumePasskeyCeremony 按客户端回传的 challenge 取出并立即作废对应仪式，再校验用途与绑定对象，防止重放与跨用途复用。
func consumePasskeyCeremony(challenge string, purpose passkeyPurpose, binding string) (*passkeyCeremony, bool) {
	if challenge == "" {
		return nil, false
	}
	key := passkeyCeremonyPrefix + challenge
	passkeyCeremonyMu.Lock()
	value, found := singleton.Cache.Get(key)
	if found {
		singleton.Cache.Delete(key)
	}
	passkeyCeremonyMu.Unlock()
	ce, ok := value.(*passkeyCeremony)
	if !found || !ok || ce.Purpose != purpose ||
		subtle.ConstantTimeCompare([]byte(ce.Binding), []byte(binding)) != 1 {
		return nil, false
	}
	return ce, true
}

// passkeySessionBinding 已登录超管会话的绑定键（会话 Token 摘要）；API Token 等身份没有会话，不可使用。
func passkeySessionBinding(c *gin.Context) (string, bool) {
	u, ok := c.Get(model.CtxKeyAuthorizedUser)
	if !ok {
		return "", false
	}
	user, ok := u.(*model.User)
	if !ok || !user.SuperAdmin || user.Token == "" {
		return "", false
	}
	return "session:" + user.Token, true
}

func passkeyTicketBinding(ticket string) string {
	return "ticket:" + ticket
}

// beginPasskeyAssertion 签发一次验证仪式。免密码登录走可发现凭据流程（不列出凭据），
// 其余用途列出当前域名下的全部通行密钥。
func beginPasskeyAssertion(c *gin.Context, purpose passkeyPurpose, binding string) (*protocol.CredentialAssertion, error) {
	rp, err := passkeyRPFromRequest(c)
	if err != nil {
		return nil, err
	}
	wa, err := newPasskeyWebAuthn(rp)
	if err != nil {
		return nil, passkeyFail(errPasskeyUnavailable, err)
	}
	rows, creds, err := loadPasskeysForRP(rp.ID)
	if err != nil {
		return nil, passkeyFail(errors.New("读取通行密钥失败"), err)
	}
	if len(creds) == 0 {
		return nil, errPasskeyNotFound
	}

	var (
		assertion *protocol.CredentialAssertion
		session   *webauthn.SessionData
	)
	if purpose == passkeyPurposeLogin {
		assertion, session, err = wa.BeginDiscoverableLogin()
	} else {
		handle, decodeErr := base64.RawURLEncoding.DecodeString(rows[0].UserHandle)
		if decodeErr != nil {
			return nil, passkeyFail(errPasskeyInvalid, decodeErr)
		}
		assertion, session, err = wa.BeginLogin(&passkeyAccount{handle: handle, creds: creds})
	}
	if err != nil {
		return nil, passkeyFail(errPasskeyUnavailable, err)
	}
	storePasskeyCeremony(&passkeyCeremony{Purpose: purpose, Binding: binding, Session: *session})
	return assertion, nil
}

// finishPasskeyAssertion 校验客户端回传的断言，成功时更新签名计数与最近使用时间并返回对应的通行密钥。
func finishPasskeyAssertion(c *gin.Context, payload string, purpose passkeyPurpose, binding string) (*model.Passkey, error) {
	if payload == "" || len(payload) > passkeyMaxPayloadBytes {
		return nil, errPasskeyInvalid
	}
	parsed, err := protocol.ParseCredentialRequestResponseBytes([]byte(payload))
	if err != nil {
		return nil, passkeyFail(errPasskeyInvalid, err)
	}
	ce, ok := consumePasskeyCeremony(parsed.Response.CollectedClientData.Challenge, purpose, binding)
	if !ok {
		return nil, errPasskeyExpired
	}
	rp, err := passkeyRPFromRequest(c)
	if err != nil {
		return nil, err
	}
	if ce.Session.RelyingPartyID != rp.ID {
		return nil, errPasskeyInvalid
	}
	wa, err := newPasskeyWebAuthn(rp)
	if err != nil {
		return nil, passkeyFail(errPasskeyUnavailable, err)
	}

	rows, creds, err := loadPasskeysForRP(rp.ID)
	if err != nil {
		return nil, passkeyFail(errors.New("读取通行密钥失败"), err)
	}
	credentialID := base64.RawURLEncoding.EncodeToString(parsed.RawID)
	var matched *model.Passkey
	for i := range rows {
		if subtle.ConstantTimeCompare([]byte(rows[i].CredentialID), []byte(credentialID)) == 1 {
			matched = &rows[i]
			break
		}
	}
	if matched == nil {
		return nil, passkeyFail(errPasskeyNotFound, errors.New("unknown credential id"))
	}

	var validated *webauthn.Credential
	if purpose == passkeyPurposeLogin {
		handler := func(rawID, userHandle []byte) (webauthn.User, error) {
			// 认证器回传的用户句柄必须与注册时保存的一致
			if base64.RawURLEncoding.EncodeToString(userHandle) != matched.UserHandle {
				return nil, errors.New("user handle mismatch")
			}
			cred, err := decodePasskeyCredential(matched)
			if err != nil {
				return nil, err
			}
			return &passkeyAccount{handle: userHandle, login: matched.UserLogin, creds: []webauthn.Credential{cred}}, nil
		}
		_, validated, err = wa.ValidatePasskeyLogin(handler, ce.Session, parsed)
	} else {
		validated, err = wa.ValidateLogin(&passkeyAccount{handle: ce.Session.UserID, creds: creds}, ce.Session, parsed)
	}
	if err != nil {
		return nil, passkeyFail(errPasskeyInvalid, err)
	}
	if validated.Authenticator.CloneWarning {
		return nil, passkeyFail(errors.New("通行密钥签名计数异常，可能已被复制，请删除后重新添加"), errors.New("sign count regressed"))
	}

	data, err := json.Marshal(validated)
	if err != nil {
		return nil, passkeyFail(errPasskeyInvalid, err)
	}
	now := time.Now()
	if err := singleton.DB.Model(&model.Passkey{}).Where("id = ?", matched.ID).Updates(map[string]any{
		"credential":   string(data),
		"last_used_at": now,
	}).Error; err != nil {
		// 签名计数用于发现被复制的认证器，写入失败时拒绝本次验证，避免计数回退
		return nil, passkeyFail(errors.New("保存通行密钥状态失败，请稍后重试"), err)
	}
	matched.LastUsedAt = &now
	return matched, nil
}

// beginPasskeyRegistration 为当前会话签发注册仪式。同一认证器在同一域名下不能重复注册。
func beginPasskeyRegistration(c *gin.Context, binding, login, name string) (*protocol.CredentialCreation, error) {
	rp, err := passkeyRPFromRequest(c)
	if err != nil {
		return nil, err
	}
	wa, err := newPasskeyWebAuthn(rp)
	if err != nil {
		return nil, passkeyFail(errPasskeyUnavailable, err)
	}
	var total int64
	if err := singleton.DB.Model(&model.Passkey{}).Count(&total).Error; err != nil {
		return nil, passkeyFail(errors.New("读取通行密钥失败"), err)
	}
	if total >= passkeyMaxCount {
		return nil, fmt.Errorf("最多只能添加 %d 个通行密钥", passkeyMaxCount)
	}
	_, creds, err := loadPasskeysForRP(rp.ID)
	if err != nil {
		return nil, passkeyFail(errors.New("读取通行密钥失败"), err)
	}
	handle, err := existingPasskeyHandle()
	if err != nil {
		return nil, passkeyFail(errors.New("读取通行密钥失败"), err)
	}
	if handle == nil {
		handle = make([]byte, passkeyUserHandleBytes)
		if _, err := rand.Read(handle); err != nil {
			return nil, passkeyFail(errors.New("生成通行密钥失败"), err)
		}
	}
	account := &passkeyAccount{handle: handle, login: login, creds: creds}
	creation, session, err := wa.BeginRegistration(account,
		webauthn.WithExclusions(webauthn.Credentials(creds).CredentialDescriptors()))
	if err != nil {
		return nil, passkeyFail(errPasskeyUnavailable, err)
	}
	storePasskeyCeremony(&passkeyCeremony{
		Purpose:   passkeyPurposeRegister,
		Binding:   binding,
		Session:   *session,
		Name:      name,
		UserLogin: login,
	})
	return creation, nil
}

// finishPasskeyRegistration 校验注册结果并保存通行密钥。
func finishPasskeyRegistration(c *gin.Context, binding string, payload []byte) (*model.Passkey, error) {
	if len(payload) == 0 || len(payload) > passkeyMaxPayloadBytes {
		return nil, errPasskeyInvalid
	}
	parsed, err := protocol.ParseCredentialCreationResponseBytes(payload)
	if err != nil {
		return nil, passkeyFail(errPasskeyInvalid, err)
	}
	ce, ok := consumePasskeyCeremony(parsed.Response.CollectedClientData.Challenge, passkeyPurposeRegister, binding)
	if !ok {
		return nil, errPasskeyExpired
	}
	rp, err := passkeyRPFromRequest(c)
	if err != nil {
		return nil, err
	}
	if ce.Session.RelyingPartyID != rp.ID {
		return nil, errPasskeyInvalid
	}
	wa, err := newPasskeyWebAuthn(rp)
	if err != nil {
		return nil, passkeyFail(errPasskeyUnavailable, err)
	}
	cred, err := wa.CreateCredential(&passkeyAccount{handle: ce.Session.UserID, login: ce.UserLogin}, ce.Session, parsed)
	if err != nil {
		return nil, passkeyFail(errors.New("通行密钥注册失败，请重试"), err)
	}

	// 全站共用一个用户句柄：注册期间若已有其他句柄的通行密钥写入，要求重试
	handle, err := existingPasskeyHandle()
	if err != nil {
		return nil, passkeyFail(errors.New("读取通行密钥失败"), err)
	}
	if handle != nil && subtle.ConstantTimeCompare(handle, ce.Session.UserID) != 1 {
		return nil, errors.New("通行密钥状态已变化，请重试")
	}
	var total int64
	if err := singleton.DB.Model(&model.Passkey{}).Count(&total).Error; err != nil {
		return nil, passkeyFail(errors.New("读取通行密钥失败"), err)
	}
	if total >= passkeyMaxCount {
		return nil, fmt.Errorf("最多只能添加 %d 个通行密钥", passkeyMaxCount)
	}

	data, err := json.Marshal(cred)
	if err != nil {
		return nil, passkeyFail(errors.New("通行密钥注册失败，请重试"), err)
	}
	row := &model.Passkey{
		Name:         ce.Name,
		UserLogin:    ce.UserLogin,
		UserHandle:   base64.RawURLEncoding.EncodeToString(ce.Session.UserID),
		CredentialID: base64.RawURLEncoding.EncodeToString(cred.ID),
		RPID:         rp.ID,
		Credential:   string(data),
	}
	var dup int64
	singleton.DB.Unscoped().Model(&model.Passkey{}).Where("credential_id = ?", row.CredentialID).Count(&dup)
	if dup > 0 {
		return nil, errors.New("该通行密钥已添加")
	}
	if err := singleton.DB.Create(row).Error; err != nil {
		return nil, passkeyFail(errors.New("保存通行密钥失败，请稍后重试"), err)
	}
	singleton.RefreshPasskeyCount()
	return row, nil
}

// isConfiguredAdmin 登录名是否在管理员名单内（忽略大小写与首尾空格，与密码 / OAuth 登录一致）。
func isConfiguredAdmin(login string) bool {
	login = strings.TrimSpace(login)
	if login == "" {
		return false
	}
	for _, admin := range strings.Split(singleton.Conf.Oauth2.Admin, ",") {
		if strings.EqualFold(login, strings.TrimSpace(admin)) {
			return true
		}
	}
	return false
}

// verifySessionPasskey 校验当前会话提交的通行密钥断言，用于代替动态验证码完成二次验证。
// 与 verifySessionTwoFactor 共用每会话试错计数。通过时返回空字符串。
func verifySessionPasskey(c *gin.Context, assertion string) string {
	const tooManyFailures = "验证码错误次数过多，请重新登录后再试"
	// 通行密钥只代替 TOTP：未绑定 TOTP（未开启双重验证）时一律拒绝
	if !singleton.Conf.TwoFactorActive() {
		return "双重验证未启用"
	}
	if mygin.TwoFactorGrantLocked(c) {
		return tooManyFailures
	}
	binding, ok := passkeySessionBinding(c)
	if !ok {
		return "当前登录会话不支持二次验证"
	}
	if _, err := finishPasskeyAssertion(c, assertion, passkeyPurposeSessionVerify, binding); err != nil {
		if mygin.RecordTwoFactorGrantFailure(c) <= 0 {
			return tooManyFailures
		}
		return err.Error()
	}
	return ""
}

// verifySessionSecondFactor 提交了通行密钥断言时校验通行密钥，否则按原逻辑校验动态验证码。
func verifySessionSecondFactor(c *gin.Context, code, passkeyAssertion string) string {
	if passkeyAssertion = strings.TrimSpace(passkeyAssertion); passkeyAssertion != "" {
		return verifySessionPasskey(c, passkeyAssertion)
	}
	return verifySessionTwoFactor(c, code)
}

// deletePasskeysOutsideAdmins 管理员名单变更后，删除归属者已不在名单（keep 为小写登录名集合）内的通行密钥，
// 不留可代替二次验证的残留凭据。
func deletePasskeysOutsideAdmins(c *gin.Context, keep map[string]struct{}) {
	var passkeys []model.Passkey
	if err := singleton.DB.Select("id", "name", "user_login").Find(&passkeys).Error; err != nil {
		log.Printf("NEZHA>> list passkeys failed: %v", err)
		return
	}
	var ids []uint64
	var desc []string
	for _, pk := range passkeys {
		if _, ok := keep[strings.ToLower(strings.TrimSpace(pk.UserLogin))]; !ok {
			ids = append(ids, pk.ID)
			desc = append(desc, fmt.Sprintf("%q (owner: %s)", pk.Name, pk.UserLogin))
		}
	}
	if len(ids) == 0 {
		return
	}
	if err := singleton.DB.Unscoped().Where("id IN ?", ids).Delete(&model.Passkey{}).Error; err != nil {
		log.Printf("NEZHA>> delete passkeys of removed admins failed: %v", err)
	} else {
		audit.Record(c, audit.TypeSecurity, "Passkey deleted", "owner removed from admin list: "+strings.Join(desc, ", "))
	}
	singleton.RefreshPasskeyCount()
}
