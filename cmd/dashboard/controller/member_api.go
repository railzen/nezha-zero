package controller

import (
	"bytes"
	"encoding/base64"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jinzhu/copier"
	"github.com/skip2/go-qrcode"
	"golang.org/x/crypto/bcrypt"
	"golang.org/x/net/idna"
	"gorm.io/gorm"

	"github.com/railzen/nezha-zero/model"
	"github.com/railzen/nezha-zero/pkg/audit"
	"github.com/railzen/nezha-zero/pkg/geoip"
	"github.com/railzen/nezha-zero/pkg/mygin"
	"github.com/railzen/nezha-zero/pkg/totp"
	"github.com/railzen/nezha-zero/pkg/utils"
	"github.com/railzen/nezha-zero/proto"
	"github.com/railzen/nezha-zero/resource"
	"github.com/railzen/nezha-zero/service/singleton"
)

type memberAPI struct {
	r gin.IRouter
}

func (ma *memberAPI) serve() {
	mr := ma.r.Group("")
	mr.Use(mygin.Authorize(mygin.AuthorizeOption{
		MemberOnly: true,
		IsPage:     false,
		Msg:        "访问此接口需要登录",
		Btn:        "点此登录",
		Redirect:   "/login",
	}))

	mr.GET("/search-server", ma.searchServer)
	mr.GET("/search-tasks", ma.searchTask)
	mr.GET("/search-ddns", ma.searchDDNS)
	mr.POST("/server", ma.addOrEditServer)
	mr.POST("/batch-update-server-group", ma.batchUpdateServerGroup)
	mr.POST("/batch-delete-server", ma.batchDeleteServer)
	mr.POST("/update-geoip", ma.updateGeoIP)
	mr.POST("/logout", ma.logout)

	// 仅超管可访问的接口,理论上能走到这里的时候一定有权限，敏感权限多加一层校验
	wr := mr.Group("")
	wr.Use(mygin.RequireSuperAdmin())
	wr.POST("/monitor", ma.addOrEditMonitor)
	wr.POST("/cron", ma.addOrEditCron)
	wr.POST("/cron/:id/manual", ma.manualTrigger)
	wr.POST("/force-update", ma.forceUpdate)
	wr.POST("/nat", ma.addOrEditNAT)
	wr.POST("/alert-rule", ma.addOrEditAlertRule)
	wr.POST("/subscription", ma.addOrEditSubscription)
	wr.POST("/batch-update-subscription-group", ma.batchUpdateSubscriptionGroup)
	wr.GET("/search-subscription", ma.searchSubscription)
	wr.POST("/currency/refresh", ma.refreshCurrencyRates)
	wr.POST("/notification", ma.addOrEditNotification)
	wr.POST("/notification/verify", ma.testNotification)
	wr.POST("/ddns", ma.addOrEditDDNS)
	wr.POST("/setting", ma.updateSetting)
	wr.POST("/totp", ma.totp)
	wr.POST("/token", ma.issueNewToken)
	wr.GET("/token", ma.getToken)
	wr.GET("/log", ma.listLogs)
	wr.DELETE("/token/:token", ma.deleteToken)
	wr.DELETE("/:model/:id", ma.delete)

	// API
	v1 := ma.r.Group("v1")
	{
		apiv1 := &apiV1{v1}
		apiv1.serve()
	}
}

type apiResult struct {
	Token string `json:"token"`
	Note  string `json:"note"`
}

// getToken 获取 Token
func (ma *memberAPI) getToken(c *gin.Context) {
	u := c.MustGet(model.CtxKeyAuthorizedUser).(*model.User)
	singleton.ApiLock.RLock()
	defer singleton.ApiLock.RUnlock()

	tokenList := singleton.UserIDToApiTokenList[u.ID]
	res := make([]*apiResult, len(tokenList))
	for i, token := range tokenList {
		res[i] = &apiResult{
			Token: token,
			Note:  singleton.ApiTokenList[token].Note,
		}
	}
	c.JSON(http.StatusOK, gin.H{
		"code":    0,
		"message": "success",
		"result":  res,
	})
}

type TokenForm struct {
	Note string
}

// issueNewToken 生成新的 token
func (ma *memberAPI) issueNewToken(c *gin.Context) {
	u := c.MustGet(model.CtxKeyAuthorizedUser).(*model.User)
	tf := &TokenForm{}
	err := c.ShouldBindJSON(tf)
	if err != nil {
		c.JSON(http.StatusOK, model.Response{
			Code:    http.StatusBadRequest,
			Message: fmt.Sprintf("请求错误：%s", err),
		})
		return
	}
	secureToken, err := utils.GenerateRandomString(32)
	if err != nil {
		c.JSON(http.StatusOK, model.Response{
			Code:    http.StatusBadRequest,
			Message: fmt.Sprintf("请求错误：%s", err),
		})
		return
	}
	token := &model.ApiToken{
		UserID: u.ID,
		Token:  utils.MaskAPIToken(secureToken),
		Hash:   utils.HashAPIToken(secureToken),
		Note:   tf.Note,
	}
	if err := singleton.DB.Create(token).Error; err != nil {
		c.JSON(http.StatusOK, model.Response{
			Code:    http.StatusInternalServerError,
			Message: fmt.Sprintf("database error: %s", err),
		})
		return
	}

	singleton.ApiLock.Lock()
	singleton.ApiTokenList[token.Token] = token
	singleton.UserIDToApiTokenList[u.ID] = append(singleton.UserIDToApiTokenList[u.ID], token.Token)
	singleton.ApiLock.Unlock()

	c.JSON(http.StatusOK, model.Response{
		Code:    http.StatusOK,
		Message: "success",
		Result: map[string]string{
			"token": secureToken,
			"note":  token.Note,
		},
	})
	audit.Record(c, audit.TypeSecurity, "API token issued", fmt.Sprintf("user: %s, note: %q", u.Login, tf.Note))
}

// deleteToken 删除 token
func (ma *memberAPI) deleteToken(c *gin.Context) {
	token := c.Param("token")
	if token == "" {
		c.JSON(http.StatusOK, model.Response{
			Code:    http.StatusBadRequest,
			Message: "token 不能为空",
		})
		return
	}
	singleton.ApiLock.Lock()
	defer singleton.ApiLock.Unlock()
	if _, ok := singleton.ApiTokenList[token]; !ok {
		c.JSON(http.StatusOK, model.Response{
			Code:    http.StatusBadRequest,
			Message: "token 不存在",
		})
		return
	}
	tokenNote := singleton.ApiTokenList[token].Note
	// 在数据库中删除该Token
	singleton.DB.Unscoped().Delete(&model.ApiToken{}, "token = ?", token)

	// 在UserIDToApiTokenList中删除该Token
	for i, t := range singleton.UserIDToApiTokenList[singleton.ApiTokenList[token].UserID] {
		if t == token {
			singleton.UserIDToApiTokenList[singleton.ApiTokenList[token].UserID] = append(singleton.UserIDToApiTokenList[singleton.ApiTokenList[token].UserID][:i], singleton.UserIDToApiTokenList[singleton.ApiTokenList[token].UserID][i+1:]...)
			break
		}
	}
	if len(singleton.UserIDToApiTokenList[singleton.ApiTokenList[token].UserID]) == 0 {
		delete(singleton.UserIDToApiTokenList, singleton.ApiTokenList[token].UserID)
	}
	// 在ApiTokenList中删除该Token
	delete(singleton.ApiTokenList, token)
	c.JSON(http.StatusOK, model.Response{
		Code:    http.StatusOK,
		Message: "success",
	})
	audit.Record(c, audit.TypeSecurity, "API token revoked", fmt.Sprintf("note: %q", tokenNote))
}

func (ma *memberAPI) delete(c *gin.Context) {
	id, _ := strconv.ParseUint(c.Param("id"), 10, 64)
	if id < 1 {
		c.JSON(http.StatusOK, model.Response{
			Code:    http.StatusBadRequest,
			Message: "错误的 Server ID",
		})
		return
	}

	var err error
	switch c.Param("model") {
	case "server":
		serverDetail := serverAuditLabel(id)
		err = singleton.DB.Transaction(func(tx *gorm.DB) error {
			if err := tx.Unscoped().Delete(&model.Server{}, "id = ?", id).Error; err != nil {
				return err
			}
			if err := tx.Unscoped().Delete(&model.MonitorHistory{}, "server_id = ?", id).Error; err != nil {
				return err
			}
			return nil
		})
		if err == nil {
			// 删除服务器
			onServerDeleteWithTaskClose(id)
			singleton.ReSortServer()
			audit.Record(c, audit.TypeConfig, "Server deleted", serverDetail)
		}
	case "notification":
		err = singleton.DB.Unscoped().Delete(&model.Notification{}, "id = ?", id).Error
		if err == nil {
			singleton.OnDeleteNotification(id)
		}
	case "ddns":
		err = singleton.DB.Unscoped().Delete(&model.DDNSProfile{}, "id = ?", id).Error
		if err == nil {
			singleton.OnDDNSUpdate()
		}
	case "nat":
		err = singleton.DB.Unscoped().Delete(&model.NAT{}, "id = ?", id).Error
		if err == nil {
			singleton.OnNATUpdate()
		}
	case "monitor":
		err = singleton.DB.Unscoped().Delete(&model.Monitor{}, "id = ?", id).Error
		if err == nil {
			singleton.ServiceSentinelShared.OnMonitorDelete(id)
			err = singleton.DB.Unscoped().Delete(&model.MonitorHistory{}, "monitor_id = ?", id).Error
		}
	case "cron":
		err = singleton.DB.Unscoped().Delete(&model.Cron{}, "id = ?", id).Error
		if err == nil {
			singleton.CronLock.Lock()
			defer singleton.CronLock.Unlock()
			cr := singleton.Crons[id]
			if cr != nil && cr.CronJobID != 0 {
				singleton.Cron.Remove(cr.CronJobID)
			}
			delete(singleton.Crons, id)
		}
	case "alert-rule":
		err = singleton.DB.Unscoped().Delete(&model.AlertRule{}, "id = ?", id).Error
		if err == nil {
			singleton.OnDeleteAlert(id)
		}
	case "subscription":
		if !singleton.Conf.EnableSubscription {
			c.JSON(http.StatusNotFound, model.Response{Code: http.StatusNotFound, Message: "订阅管理功能未启用"})
			return
		}
		var subscription model.Subscription
		if err = singleton.DB.First(&subscription, id).Error; err == nil {
			err = singleton.DB.Transaction(func(tx *gorm.DB) error {
				if err := tx.Unscoped().Delete(&subscription).Error; err != nil {
					return err
				}
				var rules []model.AlertRule
				if err := tx.Find(&rules).Error; err != nil {
					return err
				}
				for i := range rules {
					if !rules[i].IsSubscriptionExpirationRule() || !rules[i].Rules[0].Ignore[id] {
						continue
					}
					delete(rules[i].Rules[0].Ignore, id)
					if err := tx.Save(&rules[i]).Error; err != nil {
						return err
					}
				}
				return nil
			})
			if err == nil {
				audit.Record(c, audit.TypeConfig, "Subscription deleted", fmt.Sprintf("subscription: %s (ID %d)", subscription.Name, subscription.ID))
			}
		}
	}
	if err != nil {
		c.JSON(http.StatusOK, model.Response{
			Code:    http.StatusBadRequest,
			Message: fmt.Sprintf("数据库错误：%s", err),
		})
		return
	}
	c.JSON(http.StatusOK, model.Response{
		Code: http.StatusOK,
	})
}

type searchResult struct {
	Name  string `json:"name,omitempty"`
	Value uint64 `json:"value,omitempty"`
	Text  string `json:"text,omitempty"`
}

func (ma *memberAPI) searchServer(c *gin.Context) {
	var servers []model.Server
	likeWord := "%" + c.Query("word") + "%"
	singleton.DB.Select("id,name").Where("id = ? OR name LIKE ? OR tag LIKE ? OR note LIKE ?",
		c.Query("word"), likeWord, likeWord, likeWord).Find(&servers)

	var resp []searchResult
	for i := 0; i < len(servers); i++ {
		resp = append(resp, searchResult{
			Value: servers[i].ID,
			Name:  servers[i].Name,
			Text:  servers[i].Name,
		})
	}

	c.JSON(http.StatusOK, map[string]interface{}{
		"success": true,
		"results": resp,
	})
}

func (ma *memberAPI) searchSubscription(c *gin.Context) {
	if !singleton.Conf.EnableSubscription {
		c.JSON(http.StatusNotFound, gin.H{"success": false})
		return
	}
	var subscriptions []model.Subscription
	likeWord := "%" + c.Query("word") + "%"
	singleton.DB.Select("id,name").Where("id = ? OR name LIKE ? OR group_name LIKE ? OR note LIKE ?",
		c.Query("word"), likeWord, likeWord, likeWord).Find(&subscriptions)
	resp := make([]searchResult, 0, len(subscriptions))
	for i := range subscriptions {
		resp = append(resp, searchResult{Value: subscriptions[i].ID, Name: subscriptions[i].Name, Text: subscriptions[i].Name})
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "results": resp})
}

type subscriptionForm struct {
	ID           uint64
	Name         string
	StartDate    string
	EndDate      string
	Price        string
	PriceUnit    string
	Currency     string
	Link         string
	DisplayIndex int
	Note         string
	Group        string
	AutoRenewal  string
	Enable       string
	Server       bool
}

func parseOptionalSubscriptionDate(value string) (time.Time, error) {
	if strings.TrimSpace(value) == "" {
		return time.Time{}, nil
	}
	parsed, ok := model.ParseSubscriptionDate(value)
	if !ok {
		return time.Time{}, errors.New("日期格式无效")
	}
	return parsed, nil
}

func normalizeSubscriptionLink(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	lower := strings.ToLower(value)
	if strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://") {
		return value
	}
	return "https://" + value
}

func (ma *memberAPI) addOrEditSubscription(c *gin.Context) {
	if !singleton.Conf.EnableSubscription {
		c.JSON(http.StatusNotFound, model.Response{Code: http.StatusNotFound, Message: "订阅管理功能未启用"})
		return
	}
	var form subscriptionForm
	if err := c.ShouldBindJSON(&form); err != nil {
		c.JSON(http.StatusOK, model.Response{Code: http.StatusBadRequest, Message: fmt.Sprintf("请求错误：%s", err)})
		return
	}
	if form.Server {
		if !singleton.Conf.SyncServerSubscription {
			c.JSON(http.StatusOK, model.Response{Code: http.StatusBadRequest, Message: "未启用显示服务器账单"})
			return
		}
		ma.updateServerSubscriptionLink(c, form)
		return
	}
	form.Name = strings.TrimSpace(form.Name)
	form.Group = strings.TrimSpace(form.Group)
	form.Currency = strings.ToUpper(strings.TrimSpace(form.Currency))
	if form.Name == "" {
		c.JSON(http.StatusOK, model.Response{Code: http.StatusBadRequest, Message: "订阅名称不能为空"})
		return
	}
	if form.Group == "" {
		form.Group = "Default"
	}
	if form.Currency == "" {
		form.Currency = singleton.Conf.BaseCurrency
		if form.Currency == "" {
			form.Currency = "USD"
		}
	}
	if !model.IsSupportedCurrency(form.Currency) {
		c.JSON(http.StatusOK, model.Response{Code: http.StatusBadRequest, Message: "不支持的帐单货币"})
		return
	}
	startDate, err := parseOptionalSubscriptionDate(form.StartDate)
	if err == nil {
		var endDate time.Time
		endDate, err = parseOptionalSubscriptionDate(form.EndDate)
		if err == nil && !startDate.IsZero() && !endDate.IsZero() && endDate.Before(startDate) {
			err = errors.New("结束时间不能早于开始时间")
		}
		if err == nil {
			autoRenewal := form.AutoRenewal == "on"
			subscription := model.Subscription{
				Name:         form.Name,
				StartDate:    startDate,
				EndDate:      endDate,
				Price:        strings.TrimSpace(form.Price),
				PriceUnit:    strings.TrimSpace(form.PriceUnit),
				Currency:     form.Currency,
				Link:         normalizeSubscriptionLink(form.Link),
				Note:         strings.TrimSpace(form.Note),
				Group:        form.Group,
				AutoRenewal:  autoRenewal,
				Disabled:     form.Enable == "off",
				DisplayIndex: form.DisplayIndex,
			}
			if form.ID == 0 {
				err = singleton.DB.Create(&subscription).Error
			} else {
				var existing model.Subscription
				if err = singleton.DB.First(&existing, form.ID).Error; err == nil {
					subscription.ID = existing.ID
					err = singleton.DB.Model(&existing).Select("Name", "StartDate", "EndDate", "Price", "PriceUnit", "Currency", "Link", "Note", "Group", "AutoRenewal", "Disabled", "DisplayIndex").Updates(subscription).Error
				}
			}
			if err == nil {
				action := "Subscription created"
				if form.ID != 0 {
					action = "Subscription updated"
				}
				audit.Record(c, audit.TypeConfig, action, fmt.Sprintf("subscription: %s (ID %d)", subscription.Name, subscription.ID))
				c.JSON(http.StatusOK, model.Response{Code: http.StatusOK})
				return
			}
		}
	}
	c.JSON(http.StatusOK, model.Response{Code: http.StatusBadRequest, Message: fmt.Sprintf("请求错误：%s", err)})
}

func (ma *memberAPI) updateServerSubscriptionLink(c *gin.Context, form subscriptionForm) {
	if form.ID == 0 {
		c.JSON(http.StatusOK, model.Response{Code: http.StatusBadRequest, Message: "缺少服务器 ID"})
		return
	}
	var server model.Server
	if err := singleton.DB.First(&server, form.ID).Error; err != nil {
		c.JSON(http.StatusOK, model.Response{Code: http.StatusBadRequest, Message: "服务器不存在"})
		return
	}
	link := normalizeSubscriptionLink(form.Link)
	if err := singleton.DB.Model(&server).Updates(map[string]any{"link": link}).Error; err != nil {
		c.JSON(http.StatusOK, model.Response{Code: http.StatusBadRequest, Message: fmt.Sprintf("数据库错误：%s", err)})
		return
	}
	// 订阅页读 Link 时只持 SortedServerLock，写 Link 需同时持有两把锁；
	// 锁顺序与 ReSortServer 保持一致（先 ServerLock 后 SortedServerLock），避免死锁
	singleton.ServerLock.Lock()
	singleton.SortedServerLock.Lock()
	if running := singleton.ServerList[server.ID]; running != nil {
		running.Link = link
	}
	singleton.SortedServerLock.Unlock()
	singleton.ServerLock.Unlock()
	audit.Record(c, audit.TypeConfig, "Server link updated", fmt.Sprintf("server: %s (ID %d)", server.Name, server.ID))
	c.JSON(http.StatusOK, model.Response{Code: http.StatusOK})
}

func (ma *memberAPI) refreshCurrencyRates(c *gin.Context) {
	if err := singleton.RefreshCurrencyRates(); err != nil {
		c.JSON(http.StatusOK, model.Response{Code: http.StatusBadRequest, Message: "Refresh rates err"})
		return
	}
	audit.Record(c, audit.TypeConfig, "Currency rates refreshed", fmt.Sprintf("provider: %s", singleton.Conf.CurrencyProvider))
	c.JSON(http.StatusOK, model.Response{Code: http.StatusOK})
}

type batchUpdateSubscriptionGroupRequest struct {
	Subscriptions []uint64
	Group         string
}

func (ma *memberAPI) batchUpdateSubscriptionGroup(c *gin.Context) {
	if !singleton.Conf.EnableSubscription {
		c.JSON(http.StatusNotFound, model.Response{Code: http.StatusNotFound, Message: "订阅管理功能未启用"})
		return
	}
	var req batchUpdateSubscriptionGroupRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusOK, model.Response{Code: http.StatusBadRequest, Message: fmt.Sprintf("请求错误：%s", err)})
		return
	}
	if len(req.Subscriptions) == 0 {
		c.JSON(http.StatusOK, model.Response{Code: http.StatusBadRequest, Message: "未选择订阅"})
		return
	}
	req.Group = strings.TrimSpace(req.Group)
	if req.Group == "" {
		req.Group = "Default"
	}
	result := singleton.DB.Model(&model.Subscription{}).Where("id IN ?", req.Subscriptions).Update("group_name", req.Group)
	if result.Error != nil {
		c.JSON(http.StatusOK, model.Response{Code: http.StatusBadRequest, Message: fmt.Sprintf("数据库错误：%s", result.Error)})
		return
	}
	audit.Record(c, audit.TypeConfig, "Subscription groups updated",
		fmt.Sprintf("subscriptions: %v, group: %q", req.Subscriptions, req.Group))
	c.JSON(http.StatusOK, model.Response{Code: http.StatusOK})
}

func (ma *memberAPI) searchTask(c *gin.Context) {
	var tasks []model.Cron
	likeWord := "%" + c.Query("word") + "%"
	singleton.DB.Select("id,name").Where("id = ? OR name LIKE ?",
		c.Query("word"), likeWord).Find(&tasks)

	var resp []searchResult
	for i := 0; i < len(tasks); i++ {
		resp = append(resp, searchResult{
			Value: tasks[i].ID,
			Name:  tasks[i].Name,
			Text:  tasks[i].Name,
		})
	}

	c.JSON(http.StatusOK, map[string]interface{}{
		"success": true,
		"results": resp,
	})
}

func (ma *memberAPI) searchDDNS(c *gin.Context) {
	var ddns []model.DDNSProfile
	likeWord := "%" + c.Query("word") + "%"
	singleton.DB.Select("id,name").Where("id = ? OR name LIKE ?",
		c.Query("word"), likeWord).Find(&ddns)

	var resp []searchResult
	for i := 0; i < len(ddns); i++ {
		resp = append(resp, searchResult{
			Value: ddns[i].ID,
			Name:  ddns[i].Name,
			Text:  ddns[i].Name,
		})
	}

	c.JSON(http.StatusOK, map[string]interface{}{
		"success": true,
		"results": resp,
	})
}

type serverForm struct {
	ID              uint64
	Name            string `binding:"required"`
	DisplayIndex    int
	Secret          string
	Tag             string
	Note            string
	PublicNote      string
	Link            string
	HideForGuest    string
	EnableDDNS      string
	DDNSProfilesRaw string
}

func (ma *memberAPI) addOrEditServer(c *gin.Context) {
	var sf serverForm
	var s model.Server
	var isEdit bool
	err := c.ShouldBindJSON(&sf)
	if err == nil {
		if len([]rune(sf.Name)) > 256 {
			err = errors.New("服务器名称不能超过 256 个字符")
		}
	}
	if err == nil {
		s.Name = sf.Name
		s.Secret = strings.TrimSpace(sf.Secret)
		s.DisplayIndex = sf.DisplayIndex
		s.ID = sf.ID
		s.Tag = sf.Tag
		s.Note = sf.Note
		s.PublicNote = sf.PublicNote
		s.Link = normalizeSubscriptionLink(sf.Link)
		s.HideForGuest = sf.HideForGuest == "on"
		s.EnableDDNS = sf.EnableDDNS == "on"
		s.DDNSProfilesRaw = sf.DDNSProfilesRaw
		err = utils.Json.Unmarshal([]byte(sf.DDNSProfilesRaw), &s.DDNSProfiles)
		if err == nil {
			if s.ID == 0 {
				s.Secret, err = utils.GenerateRandomString(18)
				if err == nil {
					err = singleton.DB.Create(&s).Error
				}
			} else {
				if s.Secret == "" {
					err = errors.New("server secret cannot be empty")
				}
				isEdit = true
				if err == nil {
					err = singleton.DB.Save(&s).Error
				}
			}
		}
	}
	if err != nil {
		c.JSON(http.StatusOK, model.Response{
			Code:    http.StatusBadRequest,
			Message: fmt.Sprintf("请求错误：%s", err),
		})
		return
	}
	if isEdit {
		singleton.ServerLock.Lock()
		running := singleton.ServerList[s.ID]
		if running == nil {
			// DB 已 Save 成功，但内存中无此 server（启动后外部写入等罕见场景）；
			// 不做运行时状态拷贝与内存索引更新，避免 nil 解引用。下次重启会从 DB 重新加载。
			singleton.ServerLock.Unlock()
			audit.Record(c, audit.TypeConfig, "Server updated",
				fmt.Sprintf("server: %s (ID %d)", s.Name, s.ID))
			c.JSON(http.StatusOK, model.Response{
				Code: http.StatusOK,
			})
			return
		}
		s.CopyFromRunningServer(running)
		// 如果修改了 Secret
		if s.Secret != running.Secret {
			// 删除旧 Secret-ID 绑定关系
			singleton.SecretToID[s.Secret] = s.ID
			// 设置新的 Secret-ID 绑定关系
			delete(singleton.SecretToID, running.Secret)
		}
		// 如果修改了Tag
		oldTag := running.Tag
		newTag := s.Tag
		if newTag != oldTag {
			index := -1
			for i := 0; i < len(singleton.ServerTagToIDList[oldTag]); i++ {
				if singleton.ServerTagToIDList[oldTag][i] == s.ID {
					index = i
					break
				}
			}
			if index > -1 {
				// 删除旧 Tag-ID 绑定关系
				singleton.ServerTagToIDList[oldTag] = append(singleton.ServerTagToIDList[oldTag][:index], singleton.ServerTagToIDList[oldTag][index+1:]...)
				if len(singleton.ServerTagToIDList[oldTag]) == 0 {
					delete(singleton.ServerTagToIDList, oldTag)
				}
			}
			// 设置新的 Tag-ID 绑定关系
			singleton.ServerTagToIDList[newTag] = append(singleton.ServerTagToIDList[newTag], s.ID)
		}

		//只有修改了 PublicNote 才重新解析国家码
		if s.PublicNote != running.PublicNote {
			// 生效自定义国家码
			publicCountryCode := singleton.ParseCountryCodeFromJson([]byte(s.PublicNote))
			if publicCountryCode != nil {
				s.Host.CountryCode = *publicCountryCode
			}
		}

		singleton.ServerList[s.ID] = &s
		singleton.ServerLock.Unlock()
	} else {
		s.Host = &model.Host{}
		s.State = &model.HostState{}
		s.TaskCloseLock = new(sync.Mutex)
		s.TaskSendLock = new(sync.Mutex)
		s.TaskDispatchLock = new(sync.Mutex)
		s.TaskDispatchSem = make(chan struct{}, model.TaskDispatchSlotLimit)
		s.RuntimeLock = new(sync.RWMutex)
		singleton.ServerLock.Lock()
		singleton.SecretToID[s.Secret] = s.ID
		singleton.ServerList[s.ID] = &s
		singleton.ServerTagToIDList[s.Tag] = append(singleton.ServerTagToIDList[s.Tag], s.ID)
		singleton.ServerLock.Unlock()
	}
	singleton.ReSortServer()
	if isEdit {
		audit.Record(c, audit.TypeConfig, "Server updated",
			fmt.Sprintf("server: %s (ID %d)", s.Name, s.ID))
	} else {
		audit.Record(c, audit.TypeConfig, "Server created",
			fmt.Sprintf("server: %s (ID %d)", s.Name, s.ID))
	}
	c.JSON(http.StatusOK, model.Response{
		Code: http.StatusOK,
	})
}

type monitorForm struct {
	ID                     uint64
	Name                   string
	Target                 string
	Type                   uint8
	Cover                  uint8
	Notify                 string
	NotificationTag        string
	SkipServersRaw         string
	Duration               uint64
	MinLatency             float32
	MaxLatency             float32
	LatencyNotify          string
	EnableTriggerTask      string
	EnableShowInService    string
	FailTriggerTasksRaw    string
	RecoverTriggerTasksRaw string
}

func (ma *memberAPI) addOrEditMonitor(c *gin.Context) {
	var mf monitorForm
	var m model.Monitor
	err := c.ShouldBindJSON(&mf)
	if err == nil {
		m.Name = mf.Name
		m.Target = strings.TrimSpace(mf.Target)
		m.Type = mf.Type
		m.ID = mf.ID
		m.SkipServersRaw = mf.SkipServersRaw
		m.Cover = mf.Cover
		m.Notify = mf.Notify == "on"
		m.NotificationTag = mf.NotificationTag
		m.Duration = mf.Duration
		m.LatencyNotify = mf.LatencyNotify == "on"
		m.MinLatency = mf.MinLatency
		m.MaxLatency = mf.MaxLatency
		m.EnableShowInService = mf.EnableShowInService == "on"
		m.EnableTriggerTask = mf.EnableTriggerTask == "on"
		m.RecoverTriggerTasksRaw = mf.RecoverTriggerTasksRaw
		m.FailTriggerTasksRaw = mf.FailTriggerTasksRaw
		err = m.InitSkipServers()
	}
	if err == nil {
		// 保证NotificationTag不为空
		if m.NotificationTag == "" {
			m.NotificationTag = "default"
		}
		err = utils.Json.Unmarshal([]byte(mf.FailTriggerTasksRaw), &m.FailTriggerTasks)
	}
	if err == nil {
		err = utils.Json.Unmarshal([]byte(mf.RecoverTriggerTasksRaw), &m.RecoverTriggerTasks)
	}
	if err == nil {
		if m.ID == 0 {
			err = singleton.DB.Create(&m).Error
		} else {
			err = singleton.DB.Save(&m).Error
		}
	}
	if err == nil {
		if m.Cover == 0 {
			err = singleton.DB.Unscoped().Delete(&model.MonitorHistory{}, "monitor_id = ? and server_id in (?)", m.ID, strings.Split(m.SkipServersRaw[1:len(m.SkipServersRaw)-1], ",")).Error
		} else {
			err = singleton.DB.Unscoped().Delete(&model.MonitorHistory{}, "monitor_id = ? and server_id not in (?)", m.ID, strings.Split(m.SkipServersRaw[1:len(m.SkipServersRaw)-1], ",")).Error
		}
	}
	if err == nil {
		err = singleton.ServiceSentinelShared.OnMonitorUpdate(m)
	}
	if err != nil {
		c.JSON(http.StatusOK, model.Response{
			Code:    http.StatusBadRequest,
			Message: fmt.Sprintf("请求错误：%s", err),
		})
		return
	}
	c.JSON(http.StatusOK, model.Response{
		Code: http.StatusOK,
	})
}

type cronForm struct {
	ID              uint64
	TaskType        uint8 // 0:计划任务 1:触发任务
	Name            string
	Scheduler       string
	Command         string
	ServersRaw      string
	Cover           uint8
	PushSuccessful  string
	NotificationTag string
}

func (ma *memberAPI) addOrEditCron(c *gin.Context) {
	var cf cronForm
	var cr model.Cron
	err := c.ShouldBindJSON(&cf)
	if err == nil {
		cr.TaskType = cf.TaskType
		cr.Name = cf.Name
		cr.Scheduler = cf.Scheduler
		cr.Command = cf.Command
		cr.ServersRaw = cf.ServersRaw
		cr.PushSuccessful = cf.PushSuccessful == "on"
		cr.NotificationTag = cf.NotificationTag
		cr.ID = cf.ID
		cr.Cover = cf.Cover
		err = utils.Json.Unmarshal([]byte(cf.ServersRaw), &cr.Servers)
	}

	// 计划任务类型不得使用触发服务器执行方式
	if cr.TaskType == model.CronTypeCronTask && cr.Cover == model.CronCoverAlertTrigger {
		err = errors.New("计划任务类型不得使用触发服务器执行方式")
		c.JSON(http.StatusOK, model.Response{
			Code:    http.StatusBadRequest,
			Message: fmt.Sprintf("请求错误：%s", err),
		})
		return
	}

	tx := singleton.DB.Begin()
	if err == nil {
		// 保证NotificationTag不为空
		if cr.NotificationTag == "" {
			cr.NotificationTag = "default"
		}
		if cf.ID == 0 {
			err = tx.Create(&cr).Error
		} else {
			err = tx.Save(&cr).Error
		}
	}
	if err == nil {
		// 对于计划任务类型，需要更新CronJob
		if cf.TaskType == model.CronTypeCronTask {
			cr.CronJobID, err = singleton.Cron.AddFunc(cr.Scheduler, singleton.CronTrigger(cr))
		}
	}
	if err == nil {
		err = tx.Commit().Error
	} else {
		tx.Rollback()
	}
	if err != nil {
		c.JSON(http.StatusOK, model.Response{
			Code:    http.StatusBadRequest,
			Message: fmt.Sprintf("请求错误：%s", err),
		})
		return
	}

	singleton.CronLock.Lock()
	defer singleton.CronLock.Unlock()
	crOld := singleton.Crons[cr.ID]
	if crOld != nil && crOld.CronJobID != 0 {
		singleton.Cron.Remove(crOld.CronJobID)
	}

	delete(singleton.Crons, cr.ID)
	singleton.Crons[cr.ID] = &cr

	c.JSON(http.StatusOK, model.Response{
		Code: http.StatusOK,
	})
}

func (ma *memberAPI) manualTrigger(c *gin.Context) {
	var cr model.Cron
	if err := singleton.DB.First(&cr, "id = ?", c.Param("id")).Error; err != nil {
		c.JSON(http.StatusOK, model.Response{
			Code:    http.StatusBadRequest,
			Message: err.Error(),
		})
		return
	}

	singleton.ManualTrigger(cr)

	c.JSON(http.StatusOK, model.Response{
		Code: http.StatusOK,
	})
}

type BatchUpdateServerGroupRequest struct {
	Servers []uint64
	Group   string
}

func (ma *memberAPI) batchUpdateServerGroup(c *gin.Context) {
	var req BatchUpdateServerGroupRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusOK, model.Response{
			Code:    http.StatusBadRequest,
			Message: err.Error(),
		})
		return
	}

	if err := singleton.DB.Model(&model.Server{}).Where("id in (?)", req.Servers).Update("tag", req.Group).Error; err != nil {
		c.JSON(http.StatusOK, model.Response{
			Code:    http.StatusBadRequest,
			Message: err.Error(),
		})
		return
	}

	singleton.ServerLock.Lock()

	for i := 0; i < len(req.Servers); i++ {
		serverId := req.Servers[i]
		// 跳过不存在的 serverId，避免 nil 解引用（客户端可能传入已删除的 ID）
		running := singleton.ServerList[serverId]
		if running == nil {
			continue
		}
		var s model.Server
		copier.Copy(&s, running)
		s.CopyFromRunningServer(running)
		s.Tag = req.Group
		// 如果修改了Ta
		oldTag := running.Tag
		newTag := s.Tag
		if newTag != oldTag {
			index := -1
			for i := 0; i < len(singleton.ServerTagToIDList[oldTag]); i++ {
				if singleton.ServerTagToIDList[oldTag][i] == s.ID {
					index = i
					break
				}
			}
			if index > -1 {
				// 删除旧 Tag-ID 绑定关系
				singleton.ServerTagToIDList[oldTag] = append(singleton.ServerTagToIDList[oldTag][:index], singleton.ServerTagToIDList[oldTag][index+1:]...)
				if len(singleton.ServerTagToIDList[oldTag]) == 0 {
					delete(singleton.ServerTagToIDList, oldTag)
				}
			}
			// 设置新的 Tag-ID 绑定关系
			singleton.ServerTagToIDList[newTag] = append(singleton.ServerTagToIDList[newTag], s.ID)
		}
		singleton.ServerList[s.ID] = &s
	}

	singleton.ServerLock.Unlock()

	singleton.ReSortServer()

	c.JSON(http.StatusOK, model.Response{
		Code: http.StatusOK,
	})
}

func (ma *memberAPI) forceUpdate(c *gin.Context) {
	var forceUpdateServers []uint64
	if err := c.ShouldBindJSON(&forceUpdateServers); err != nil {
		c.JSON(http.StatusOK, model.Response{
			Code:    http.StatusBadRequest,
			Message: err.Error(),
		})
		return
	}

	var executeResult bytes.Buffer

	for i := 0; i < len(forceUpdateServers); i++ {
		singleton.ServerLock.RLock()
		server := singleton.ServerList[forceUpdateServers[i]]
		singleton.ServerLock.RUnlock()
		if server != nil && server.TaskStream != nil {
			if err := server.SendTask(&proto.Task{
				Type: model.TaskTypeUpgrade,
			}); err != nil {
				executeResult.WriteString(fmt.Sprintf("%d 下发指令失败 %+v<br/>", forceUpdateServers[i], err))
			} else {
				executeResult.WriteString(fmt.Sprintf("%d 下发指令成功<br/>", forceUpdateServers[i]))
			}
		} else {
			executeResult.WriteString(fmt.Sprintf("%d 离线<br/>", forceUpdateServers[i]))
		}
	}

	c.JSON(http.StatusOK, model.Response{
		Code:    http.StatusOK,
		Message: executeResult.String(),
	})
}

type notificationForm struct {
	ID             uint64
	Name           string
	Tag            string // 分组名
	Type           uint8
	URL            string
	RequestMethod  int
	RequestType    int
	RequestHeader  string
	RequestBody    string
	VerifySSL      string
	SkipCheck      string
	TelegramToken  string
	TelegramChatID string
	SMTPHost       string
	SMTPPort       int
	SMTPTLS        string
	SMTPUsername   string
	SMTPPassword   string
	EmailTo        string
}

func (ma *memberAPI) addOrEditNotification(c *gin.Context) {
	var nf notificationForm
	if err := c.ShouldBindJSON(&nf); err != nil {
		c.JSON(http.StatusOK, model.Response{Code: http.StatusBadRequest, Message: fmt.Sprintf("请求错误：%s", err)})
		return
	}
	n, err := notificationFromForm(nf)
	if err == nil {
		ns := model.NotificationServerBundle{
			Notification: &n,
			Server:       nil,
			Loc:          singleton.Loc,
		}
		if n.Type == model.NotificationTypeWebhook && nf.SkipCheck != "on" {
			err = ns.Send("这是测试消息")
		}
	}
	if err == nil {
		// 保证Tag不为空
		if n.Tag == "" {
			n.Tag = "default"
		}
		if n.ID == 0 {
			err = singleton.DB.Create(&n).Error
		} else {
			err = singleton.DB.Save(&n).Error
		}
	}
	if err != nil {
		c.JSON(http.StatusOK, model.Response{
			Code:    http.StatusBadRequest,
			Message: fmt.Sprintf("请求错误：%s", err),
		})
		return
	}
	singleton.OnRefreshOrAddNotification(&n)
	c.JSON(http.StatusOK, model.Response{
		Code: http.StatusOK,
	})
}

func (ma *memberAPI) testNotification(c *gin.Context) {
	var nf notificationForm
	if err := c.ShouldBindJSON(&nf); err != nil {
		c.JSON(http.StatusOK, model.Response{Code: http.StatusBadRequest, Message: fmt.Sprintf("请求错误：%s", err)})
		return
	}
	n, err := notificationFromForm(nf)
	if err == nil {
		err = (&model.NotificationServerBundle{Notification: &n, Loc: singleton.Loc}).Send("这是测试消息")
	}
	if err != nil {
		c.JSON(http.StatusOK, model.Response{Code: http.StatusBadRequest, Message: err.Error()})
		return
	}
	c.JSON(http.StatusOK, model.Response{Code: http.StatusOK})
}

func notificationFromForm(nf notificationForm) (model.Notification, error) {
	n := model.Notification{
		Name:           strings.TrimSpace(nf.Name),
		Tag:            strings.TrimSpace(nf.Tag),
		Type:           nf.Type,
		URL:            strings.TrimSpace(nf.URL),
		RequestMethod:  nf.RequestMethod,
		RequestType:    nf.RequestType,
		RequestHeader:  nf.RequestHeader,
		RequestBody:    nf.RequestBody,
		TelegramToken:  strings.TrimSpace(nf.TelegramToken),
		TelegramChatID: strings.TrimSpace(nf.TelegramChatID),
		SMTPHost:       strings.TrimSpace(nf.SMTPHost),
		SMTPPort:       nf.SMTPPort,
		SMTPTLS:        nf.SMTPTLS == "on",
		SMTPUsername:   strings.TrimSpace(nf.SMTPUsername),
		SMTPPassword:   nf.SMTPPassword,
		EmailTo:        strings.TrimSpace(nf.EmailTo),
	}
	n.ID = nf.ID
	verifySSL := nf.VerifySSL == "on"
	n.VerifySSL = &verifySSL
	if n.Tag == "" {
		n.Tag = "default"
	}
	if n.Type > model.NotificationTypeEmail {
		return n, errors.New("通知方式类型无效")
	}
	if n.ID != 0 && (n.TelegramToken == "********" || n.SMTPPassword == "********") {
		var old model.Notification
		if err := singleton.DB.First(&old, n.ID).Error; err != nil {
			return n, err
		}
		if n.TelegramToken == "********" {
			n.TelegramToken = old.TelegramToken
		}
		if n.SMTPPassword == "********" {
			n.SMTPPassword = old.SMTPPassword
		}
	}
	if n.SMTPPort == 0 {
		if n.SMTPTLS {
			n.SMTPPort = 465
		} else {
			n.SMTPPort = 25
		}
	}
	switch n.Type {
	case model.NotificationTypeWebhook:
		n.TelegramToken, n.TelegramChatID = "", ""
		n.SMTPHost, n.SMTPUsername, n.SMTPPassword, n.EmailTo = "", "", "", ""
		n.SMTPPort = 0
		n.SMTPTLS = false
	case model.NotificationTypeTelegram:
		if n.TelegramToken == "" || n.TelegramChatID == "" {
			return n, errors.New("Telegram Token 和会话 ID 不能为空")
		}
		n.SMTPHost, n.SMTPUsername, n.SMTPPassword, n.EmailTo = "", "", "", ""
		n.SMTPPort = 0
		n.SMTPTLS = false
	case model.NotificationTypeEmail:
		if n.SMTPHost == "" || n.SMTPUsername == "" || n.SMTPPassword == "" || n.EmailTo == "" {
			return n, errors.New("邮件通知配置不完整")
		}
		n.TelegramToken, n.TelegramChatID = "", ""
	}
	return n, nil
}

type ddnsForm struct {
	ID                 uint64
	MaxRetries         uint64
	EnableIPv4         string
	EnableIPv6         string
	Name               string
	Provider           uint8
	DomainsRaw         string
	AccessID           string
	AccessSecret       string
	WebhookURL         string
	WebhookMethod      uint8
	WebhookRequestType uint8
	WebhookRequestBody string
	WebhookHeaders     string
}

func (ma *memberAPI) addOrEditDDNS(c *gin.Context) {
	var df ddnsForm
	var p model.DDNSProfile
	err := c.ShouldBindJSON(&df)
	if err == nil {
		if df.MaxRetries < 1 || df.MaxRetries > 10 {
			err = errors.New("重试次数必须为大于 1 且不超过 10 的整数")
		}
	}
	if err == nil {
		p.Name = df.Name
		p.ID = df.ID
		enableIPv4 := df.EnableIPv4 == "on"
		enableIPv6 := df.EnableIPv6 == "on"
		p.EnableIPv4 = &enableIPv4
		p.EnableIPv6 = &enableIPv6
		p.MaxRetries = df.MaxRetries
		p.Provider = df.Provider
		p.DomainsRaw = df.DomainsRaw
		p.Domains = strings.Split(p.DomainsRaw, ",")
		p.AccessID = df.AccessID
		p.AccessSecret = df.AccessSecret
		p.WebhookURL = df.WebhookURL
		p.WebhookMethod = df.WebhookMethod
		p.WebhookRequestType = df.WebhookRequestType
		p.WebhookRequestBody = df.WebhookRequestBody
		p.WebhookHeaders = df.WebhookHeaders

		for n, domain := range p.Domains {
			// IDN to ASCII
			domainValid, domainErr := idna.Lookup.ToASCII(domain)
			if domainErr != nil {
				err = fmt.Errorf("域名 %s 解析错误: %v", domain, domainErr)
				break
			}
			p.Domains[n] = domainValid
		}
	}
	if err == nil {
		if p.ID == 0 {
			err = singleton.DB.Create(&p).Error
		} else {
			err = singleton.DB.Save(&p).Error
		}
	}
	if err != nil {
		c.JSON(http.StatusOK, model.Response{
			Code:    http.StatusBadRequest,
			Message: fmt.Sprintf("请求错误：%s", err),
		})
		return
	}
	singleton.OnDDNSUpdate()
	c.JSON(http.StatusOK, model.Response{
		Code: http.StatusOK,
	})
}

type natForm struct {
	ID       uint64
	Name     string
	ServerID uint64
	Host     string
	Domain   string
}

func (ma *memberAPI) addOrEditNAT(c *gin.Context) {
	var nf natForm
	var n model.NAT
	err := c.ShouldBindJSON(&nf)
	if err == nil {
		n.Name = nf.Name
		n.ID = nf.ID
		n.Domain = nf.Domain
		n.Host = nf.Host
		n.ServerID = nf.ServerID
	}
	if err == nil {
		if n.ID == 0 {
			err = singleton.DB.Create(&n).Error
		} else {
			err = singleton.DB.Save(&n).Error
		}
	}
	if err != nil {
		c.JSON(http.StatusOK, model.Response{
			Code:    http.StatusBadRequest,
			Message: fmt.Sprintf("请求错误：%s", err),
		})
		return
	}
	singleton.OnNATUpdate()
	c.JSON(http.StatusOK, model.Response{
		Code: http.StatusOK,
	})
}

type alertRuleForm struct {
	ID                     uint64
	AlertType              uint8
	Name                   string
	RulesRaw               string
	FailTriggerTasksRaw    string // 失败时触发的任务id
	RecoverTriggerTasksRaw string // 恢复时触发的任务id
	NotificationTag        string
	TriggerMode            int
	Enable                 string
	AdvanceDays            int
	DailyReminder          string
	Cover                  uint8
	SkipServersRaw         string
	SkipSubscriptionsRaw   string
}

func (ma *memberAPI) addOrEditAlertRule(c *gin.Context) {
	var arf alertRuleForm
	var r model.AlertRule
	err := c.ShouldBindJSON(&arf)
	if err == nil && (arf.AlertType == 1 || arf.AlertType == 2) {
		var selectedIDs []uint64
		selectedRaw := arf.SkipServersRaw
		ruleType := model.RuleTypeExpiration
		if arf.AlertType == 2 {
			if !singleton.Conf.EnableSubscription {
				err = errors.New("订阅管理功能未启用")
			}
			selectedRaw = arf.SkipSubscriptionsRaw
			ruleType = model.RuleTypeSubscriptionExpiration
		}
		if err != nil {
			// Keep the feature-gate error set above.
		} else if arf.AdvanceDays < 0 || arf.AdvanceDays > 365 {
			err = errors.New("提前天数必须在 0 到 365 之间")
		} else if arf.Cover > model.RuleCoverIgnoreAll {
			err = errors.New("覆盖范围无效")
		} else {
			err = utils.Json.Unmarshal([]byte(selectedRaw), &selectedIDs)
		}
		if err == nil {
			selected := make(map[uint64]bool, len(selectedIDs))
			for _, id := range selectedIDs {
				selected[id] = true
			}
			if arf.AlertType == 2 && len(selectedIDs) > 0 {
				var count int64
				err = singleton.DB.Model(&model.Subscription{}).Where("id IN ?", selectedIDs).Count(&count).Error
				if err == nil && count != int64(len(selected)) {
					err = errors.New("包含不存在的订阅 ID")
				}
			}
			if err != nil {
				c.JSON(http.StatusOK, model.Response{Code: http.StatusBadRequest, Message: fmt.Sprintf("请求错误：%s", err)})
				return
			}
			enable := arf.Enable == "on"
			r.ID = arf.ID
			r.Name = strings.TrimSpace(arf.Name)
			r.NotificationTag = strings.TrimSpace(arf.NotificationTag)
			if r.NotificationTag == "" {
				r.NotificationTag = "default"
			}
			r.Enable = &enable
			r.FailTriggerTasks = []uint64{}
			r.RecoverTriggerTasks = []uint64{}
			r.Rules = []model.Rule{{
				Type:          ruleType,
				AdvanceDays:   arf.AdvanceDays,
				DailyReminder: arf.DailyReminder == "on",
				Cover:         uint64(arf.Cover),
				Ignore:        selected,
			}}
			if r.ID == 0 {
				err = singleton.DB.Create(&r).Error
			} else {
				err = singleton.DB.Save(&r).Error
			}
		}
		if err != nil {
			c.JSON(http.StatusOK, model.Response{Code: http.StatusBadRequest, Message: fmt.Sprintf("请求错误：%s", err)})
			return
		}
		c.JSON(http.StatusOK, model.Response{Code: http.StatusOK})
		singleton.OnRefreshOrAddAlert(r)
		go singleton.CheckExpirationReminders()
		return
	}
	if err == nil && arf.AlertType > 2 {
		err = errors.New("告警类型无效")
	}
	if err == nil {
		err = utils.Json.Unmarshal([]byte(arf.RulesRaw), &r.Rules)
	}
	if err == nil {
		if len(r.Rules) == 0 {
			err = errors.New("至少定义一条规则")
		} else {
			for i := 0; i < len(r.Rules); i++ {
				if !r.Rules[i].IsTransferDurationRule() {
					if r.Rules[i].Duration < 3 {
						err = errors.New("错误：Duration 至少为 3")
						break
					}
				} else {
					if r.Rules[i].CycleInterval < 1 {
						err = errors.New("错误: cycle_interval 至少为 1")
						break
					}
					if r.Rules[i].CycleStart == nil {
						err = errors.New("错误: cycle_start 未设置")
						break
					}
					if r.Rules[i].CycleStart.After(time.Now()) {
						err = errors.New("错误: cycle_start 是个未来值")
						break
					}
				}
			}
		}
	}
	if err == nil {
		r.Name = arf.Name
		r.RulesRaw = arf.RulesRaw
		r.FailTriggerTasksRaw = arf.FailTriggerTasksRaw
		r.RecoverTriggerTasksRaw = arf.RecoverTriggerTasksRaw
		r.NotificationTag = arf.NotificationTag
		enable := arf.Enable == "on"
		r.TriggerMode = arf.TriggerMode
		r.Enable = &enable
		r.ID = arf.ID
	}
	if err == nil {
		err = utils.Json.Unmarshal([]byte(arf.FailTriggerTasksRaw), &r.FailTriggerTasks)
	}
	if err == nil {
		err = utils.Json.Unmarshal([]byte(arf.RecoverTriggerTasksRaw), &r.RecoverTriggerTasks)
	}
	//保证NotificationTag不为空
	if err == nil {
		if r.NotificationTag == "" {
			r.NotificationTag = "default"
		}
		if r.ID == 0 {
			err = singleton.DB.Create(&r).Error
		} else {
			err = singleton.DB.Save(&r).Error
		}
	}
	if err != nil {
		c.JSON(http.StatusOK, model.Response{
			Code:    http.StatusBadRequest,
			Message: fmt.Sprintf("请求错误：%s", err),
		})
		return
	}
	singleton.OnRefreshOrAddAlert(r)
	c.JSON(http.StatusOK, model.Response{
		Code: http.StatusOK,
	})
}

type logoutForm struct {
	ID uint64
}

func (ma *memberAPI) logout(c *gin.Context) {
	admin := c.MustGet(model.CtxKeyAuthorizedUser).(*model.User)
	var lf logoutForm
	if err := c.ShouldBindJSON(&lf); err != nil {
		c.JSON(http.StatusOK, model.Response{
			Code:    http.StatusBadRequest,
			Message: fmt.Sprintf("请求错误：%s", err),
		})
		return
	}
	if lf.ID != admin.ID {
		c.JSON(http.StatusOK, model.Response{
			Code:    http.StatusBadRequest,
			Message: fmt.Sprintf("请求错误：%s", "用户ID不匹配"),
		})
		return
	}
	/*
		singleton.DB.Model(admin).UpdateColumns(model.User{
			Token:        "",
			TokenExpired: time.Now().UTC(),
		})
	*/
	singleton.DB.Unscoped().Where("id = ? AND token = ?", admin.ID, admin.Token).Delete(&model.User{})
	mygin.ClearSessionCookies(c)
	c.JSON(http.StatusOK, model.Response{
		Code: http.StatusOK,
	})
	audit.Record(c, audit.TypeAuth, "Logout", fmt.Sprintf("session terminated, user: %s", admin.Login))

	if oidcLogoutUrl := singleton.Conf.Oauth2.OidcLogoutURL; oidcLogoutUrl != "" {
		// 重定向到 OIDC 退出登录地址。不知道为什么，这里的重定向不生效
		c.Redirect(http.StatusOK, oidcLogoutUrl)
	}
}

type settingForm struct {
	Title                           string
	Admin                           string
	Language                        string
	Theme                           string
	DashboardTheme                  string
	CustomCode                      string
	CustomCodeDashboard             string
	CustomNameservers               string
	ViewPassword                    string
	IgnoredIPNotification           string
	IPChangeNotificationTag         string // IP变更提醒的通知组
	GRPCHost                        string
	GRPCDiscoverKey                 string
	Cover                           uint8
	Password                        string
	TwoFactorCode                   string
	UseExternalGeoIP                string
	EnableIPChangeNotification      string
	EnablePlainIPInNotification     string
	DisableSwitchTemplateInFrontend string
	CompatAPIDisable                string
	UseTemplateHandleNoRoute        string
	EnableSubscription              string
	SyncServerSubscription          string
	EnableCurrencyConversion        string
	ShowMonthlyPrice                string
	CurrencyProvider                string
	CurrencyAPIKey                  string
	BaseCurrency                    string
	DisableOauthLogin               string
	DisablePasswordLogin            string
}

const (
	twoFactorSetupCachePrefix = "totp_setup_"
	twoFactorSetupTTL         = 10 * time.Minute
)

func validatePasswordChangeTwoFactor(passwordChanged bool, secret, code string) string {
	if !passwordChanged || secret == "" {
		return ""
	}
	if strings.TrimSpace(code) == "" {
		return "请输入当前双重验证码"
	}
	if !totp.Validate(secret, code, 1) {
		return "双重验证码错误，请重试"
	}
	return ""
}

func (ma *memberAPI) updateSetting(c *gin.Context) {
	var sf settingForm
	if err := c.ShouldBind(&sf); err != nil {
		c.JSON(http.StatusOK, model.Response{
			Code:    http.StatusBadRequest,
			Message: fmt.Sprintf("请求错误：%s", err),
		})
		return
	}

	if _, yes := model.Themes[sf.Theme]; !yes {
		c.JSON(http.StatusOK, model.Response{
			Code:    http.StatusBadRequest,
			Message: fmt.Sprintf("前台主题不存在：%s", sf.Theme),
		})
		return
	}
	if sf.CompatAPIDisable == "on" && model.IsV1Theme(sf.Theme) {
		c.JSON(http.StatusOK, model.Response{
			Code:    http.StatusBadRequest,
			Message: "禁用兼容 API 时无法使用 V1 主题",
		})
		return
	}

	if _, yes := model.DashboardThemes[sf.DashboardTheme]; !yes {
		c.JSON(http.StatusOK, model.Response{
			Code:    http.StatusBadRequest,
			Message: fmt.Sprintf("后台主题不存在：%s", sf.DashboardTheme),
		})
		return
	}

	if !utils.IsFileExists("resource/template/theme-"+sf.Theme+"/home.html") && !resource.IsTemplateFileExist("template/theme-"+sf.Theme+"/home.html") {
		c.JSON(http.StatusOK, model.Response{
			Code:    http.StatusBadRequest,
			Message: fmt.Sprintf("前台主题文件异常：%s", sf.Theme),
		})
		return
	}

	if !utils.IsFileExists("resource/template/dashboard-"+sf.DashboardTheme+"/setting.html") && !resource.IsTemplateFileExist("template/dashboard-"+sf.DashboardTheme+"/setting.html") {
		c.JSON(http.StatusOK, model.Response{
			Code:    http.StatusBadRequest,
			Message: fmt.Sprintf("后台主题文件异常：%s", sf.DashboardTheme),
		})
		return
	}

	disableOauthLogin := sf.DisableOauthLogin == "on"
	disablePasswordLogin := sf.DisablePasswordLogin == "on"

	// ===== 计算新的管理员密码 =====
	// 禁用密码登录或密码留空 → 清空；"********" 表示未修改；否则校验并生成新哈希
	adminPassword := singleton.Conf.Site.AdminPassword
	switch {
	case disablePasswordLogin || sf.Password == "":
		adminPassword = ""
	case sf.Password == "********":
		// 密码未修改，保持原哈希
	default:
		if err := utils.ValidateAdminPassword(sf.Password); err != nil {
			c.JSON(http.StatusOK, model.Response{
				Code:    http.StatusBadRequest,
				Message: err.Error(),
			})
			return
		}
		if bcrypt.CompareHashAndPassword([]byte(adminPassword), []byte(sf.Password)) != nil {
			hash, err := bcrypt.GenerateFromPassword([]byte(sf.Password), utils.BcryptCost)
			if err != nil {
				c.JSON(http.StatusOK, model.Response{
					Code:    http.StatusInternalServerError,
					Message: "密码加密失败，请稍后重试",
				})
				return
			}
			adminPassword = string(hash)
		}
	}
	passwordChanged := singleton.Conf.Site.AdminPassword != adminPassword
	if passwordChanged && singleton.Conf.TwoFactorActive() {
		if !allowAuthRateLimitedCheck(c) {
			c.JSON(http.StatusOK, model.Response{
				Code:    http.StatusTooManyRequests,
				Message: "请求过于频繁，请稍后再试",
			})
			return
		}
		if message := validatePasswordChangeTwoFactor(passwordChanged, singleton.Conf.Site.TwoFactorSecret, sf.TwoFactorCode); message != "" {
			audit.Record(c, audit.TypeSecurity, "Admin password change failed", "invalid two-factor code")
			c.JSON(http.StatusOK, model.Response{
				Code:    http.StatusBadRequest,
				Message: message,
			})
			return
		}
	}

	// ===== 校验登录方式 =====
	// 管理员用户名列表是密码登录与 OAuth 登录共用的白名单，任何模式下都必填
	hasAdminList := false
	for _, admin := range strings.Split(sf.Admin, ",") {
		if strings.TrimSpace(admin) != "" {
			hasAdminList = true
			break
		}
	}
	var loginErr string
	switch {
	case !hasAdminList:
		loginErr = "必须配置至少一个管理员用户名"
	case disableOauthLogin && disablePasswordLogin:
		loginErr = "至少需要启用一种登录方式"
	case !disablePasswordLogin && adminPassword == "":
		loginErr = "启用密码登录时必须设置管理员密码"
	}
	if loginErr != "" {
		c.JSON(http.StatusOK, model.Response{
			Code:    http.StatusBadRequest,
			Message: loginErr,
		})
		return
	}

	oldConf := *singleton.Conf
	enableCurrencyConversion := sf.EnableCurrencyConversion == "on"
	currencyProvider := strings.ToLower(strings.TrimSpace(sf.CurrencyProvider))
	if currencyProvider == "" {
		currencyProvider = oldConf.CurrencyProvider
		if currencyProvider == "" {
			currencyProvider = "fixer"
		}
	}
	if currencyProvider != "fixer" && currencyProvider != "apilayer" {
		c.JSON(http.StatusOK, model.Response{Code: http.StatusBadRequest, Message: "不支持的货币换算服务"})
		return
	}
	baseCurrency := strings.ToUpper(strings.TrimSpace(sf.BaseCurrency))
	if baseCurrency == "" {
		baseCurrency = oldConf.BaseCurrency
		if baseCurrency == "" {
			baseCurrency = "USD"
		}
	}
	if !model.IsSupportedCurrency(baseCurrency) {
		c.JSON(http.StatusOK, model.Response{Code: http.StatusBadRequest, Message: "不支持的基准货币"})
		return
	}
	currencyAPIKey := strings.TrimSpace(sf.CurrencyAPIKey)
	if currencyAPIKey == "********" {
		currencyAPIKey = oldConf.CurrencyAPIKey
	}
	if enableCurrencyConversion && currencyAPIKey == "" {
		c.JSON(http.StatusOK, model.Response{Code: http.StatusBadRequest, Message: "启用货币换算时必须填写 API Key"})
		return
	}

	singleton.Conf.Language = sf.Language
	singleton.Conf.UseExternalGeoIP = sf.UseExternalGeoIP == "on"
	singleton.Conf.EnableIPChangeNotification = sf.EnableIPChangeNotification == "on"
	singleton.Conf.EnablePlainIPInNotification = sf.EnablePlainIPInNotification == "on"
	singleton.Conf.DisableSwitchTemplateInFrontend = sf.DisableSwitchTemplateInFrontend == "on"
	singleton.Conf.UseTemplateHandleNoRoute = sf.UseTemplateHandleNoRoute == "on"
	singleton.Conf.CompatAPIDisable = sf.CompatAPIDisable == "on"
	singleton.Conf.EnableSubscription = sf.EnableSubscription == "on"
	singleton.Conf.SyncServerSubscription = sf.SyncServerSubscription == "on"
	singleton.Conf.EnableCurrencyConversion = enableCurrencyConversion
	singleton.Conf.ShowMonthlyPrice = sf.ShowMonthlyPrice == "on"
	singleton.Conf.CurrencyProvider = currencyProvider
	singleton.Conf.CurrencyAPIKey = currencyAPIKey
	singleton.Conf.BaseCurrency = baseCurrency
	singleton.Conf.Oauth2.DisableOauthLogin = disableOauthLogin
	singleton.Conf.Site.DisablePasswordLogin = disablePasswordLogin
	singleton.Conf.Cover = sf.Cover
	singleton.Conf.GRPCHost = sf.GRPCHost
	singleton.Conf.GRPCDiscoverKey = sf.GRPCDiscoverKey
	singleton.Conf.IgnoredIPNotification = sf.IgnoredIPNotification
	singleton.Conf.IPChangeNotificationTag = sf.IPChangeNotificationTag
	singleton.Conf.Site.Brand = sf.Title
	singleton.Conf.Site.Theme = sf.Theme
	singleton.Conf.Site.DashboardTheme = sf.DashboardTheme
	singleton.Conf.Site.CustomCode = sf.CustomCode
	singleton.Conf.Site.CustomCodeDashboard = sf.CustomCodeDashboard
	singleton.Conf.DNSServers = sf.CustomNameservers
	singleton.Conf.Site.ViewPassword = sf.ViewPassword
	singleton.Conf.Oauth2.Admin = sf.Admin
	// 保证NotificationTag不为空
	if singleton.Conf.IPChangeNotificationTag == "" {
		singleton.Conf.IPChangeNotificationTag = "default"
	}

	if passwordChanged {
		singleton.Conf.Site.AdminPassword = adminPassword
	}

	if err := singleton.Conf.Save(); err != nil {
		*singleton.Conf = oldConf
		c.JSON(http.StatusOK, model.Response{
			Code:    http.StatusBadRequest,
			Message: fmt.Sprintf("请求错误：%s", err),
		})
		return
	}
	if oldConf.CurrencyProvider != singleton.Conf.CurrencyProvider || oldConf.CurrencyAPIKey != singleton.Conf.CurrencyAPIKey {
		if err := singleton.DB.Delete(&model.CurrencySnapshot{}, model.CurrentCurrencySnapshotID).Error; err != nil {
			log.Printf("NEZHA>> clear currency snapshot after settings update failed: %v", err)
		}
	}
	if !oldConf.EnableSubscription && singleton.Conf.EnableSubscription {
		go singleton.CheckExpirationReminders()
	}
	if singleton.Conf.EnableSubscription && singleton.Conf.EnableCurrencyConversion &&
		(!oldConf.EnableCurrencyConversion || oldConf.CurrencyProvider != singleton.Conf.CurrencyProvider ||
			oldConf.CurrencyAPIKey != singleton.Conf.CurrencyAPIKey || oldConf.BaseCurrency != singleton.Conf.BaseCurrency) {
		go func() {
			if err := singleton.RefreshCurrencyRates(); err != nil {
				log.Printf("NEZHA>> refresh currency rates after settings update failed: %v", err)
			}
		}()
	}

	if passwordChanged {
		singleton.DB.Unscoped().Where("1 = 1").Delete(&model.User{})
		mygin.ClearSessionCookies(c)
		audit.Record(c, audit.TypeAuth, "Logout", "all sessions revoked due to admin password change")
	}
	// 更新系统语言
	singleton.InitLocalizer()
	// 更新DNS服务器
	singleton.OnNameserverUpdate()
	if err := geoip.ReloadExternal(singleton.Conf.UseExternalGeoIP); err != nil {
		log.Printf("NEZHA>> GeoIP reload failed: %v", err)
	}
	c.JSON(http.StatusOK, model.Response{
		Code: http.StatusOK,
	})
	settingIn := audit.SettingChangeInput{
		Title:                           sf.Title,
		Admin:                           sf.Admin,
		Language:                        sf.Language,
		Theme:                           sf.Theme,
		DashboardTheme:                  sf.DashboardTheme,
		CustomCodeChanged:               oldConf.Site.CustomCode != sf.CustomCode,
		CustomCodeDashboardChanged:      oldConf.Site.CustomCodeDashboard != sf.CustomCodeDashboard,
		ViewPasswordChanged:             oldConf.Site.ViewPassword != sf.ViewPassword,
		CustomNameservers:               sf.CustomNameservers,
		UseExternalGeoIP:                sf.UseExternalGeoIP == "on",
		EnableIPChangeNotification:      sf.EnableIPChangeNotification == "on",
		EnablePlainIPInNotification:     sf.EnablePlainIPInNotification == "on",
		DisableSwitchTemplateInFrontend: sf.DisableSwitchTemplateInFrontend == "on",
		CompatAPIDisable:                sf.CompatAPIDisable == "on",
		EnableSubscription:              sf.EnableSubscription == "on",
		SyncServerSubscription:          sf.SyncServerSubscription == "on",
		EnableCurrencyConversion:        enableCurrencyConversion,
		CurrencyProvider:                currencyProvider,
		BaseCurrency:                    baseCurrency,
		CurrencyAPIKeyChanged:           oldConf.CurrencyAPIKey != currencyAPIKey,
		UseTemplateHandleNoRoute:        sf.UseTemplateHandleNoRoute == "on",
		DisableOauthLogin:               disableOauthLogin,
		DisablePasswordLogin:            disablePasswordLogin,
		GRPCHost:                        sf.GRPCHost,
		GRPCDiscoverKey:                 sf.GRPCDiscoverKey,
		Cover:                           sf.Cover,
		IgnoredIPNotification:           sf.IgnoredIPNotification,
		IPChangeNotificationTag:         singleton.Conf.IPChangeNotificationTag,
		PasswordChanged:                 passwordChanged,
	}
	if detail := audit.BuildSecuritySettingDetail(&oldConf, settingIn); detail != "" {
		audit.Record(c, audit.TypeSecurity, "Security settings updated", detail)
	}
	if detail := audit.BuildConfigSettingDetail(&oldConf, settingIn); detail != "" {
		audit.Record(c, audit.TypeConfig, "Settings updated", detail)
	}
}

func (ma *memberAPI) updateGeoIP(c *gin.Context) {
	if err := geoip.Update(); err != nil {
		c.JSON(http.StatusOK, model.Response{
			Code:    http.StatusBadRequest,
			Message: err.Error(),
		})
		return
	}
	if err := geoip.ReloadExternal(singleton.Conf.UseExternalGeoIP); err != nil {
		log.Printf("NEZHA>> GeoIP reload failed: %v", err)
	}
	audit.Record(c, audit.TypeConfig, "GeoIP database updated", "GeoIP database updated")
	c.JSON(http.StatusOK, model.Response{
		Code: http.StatusOK,
	})
}

func (ma *memberAPI) listLogs(c *gin.Context) {
	audit.PruneExcess()
	typ := c.Query("type")
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "50"))
	if page < 1 {
		page = 1
	}
	if limit < 1 || limit > 200 {
		limit = 50
	}

	q := singleton.DB.Model(&model.AuditLog{})
	if typ != "" && typ != "all" {
		q = q.Where("type = ?", typ)
	}
	var total int64
	q.Count(&total)

	var logs []model.AuditLog
	q.Order("created_at DESC").Offset((page - 1) * limit).Limit(limit).Find(&logs)

	c.JSON(http.StatusOK, model.Response{
		Code: http.StatusOK,
		Result: gin.H{
			"logs":  logs,
			"total": total,
			"page":  page,
			"limit": limit,
			"types": audit.TypeOptions(),
		},
	})
}

func (ma *memberAPI) totp(c *gin.Context) {
	var req struct {
		Operation string `json:"operation"`
		SetupID   string `json:"setupId"`
		Code      string `json:"code"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusOK, model.Response{
			Code:    http.StatusBadRequest,
			Message: err.Error(),
		})
		return
	}
	req.Operation = strings.TrimSpace(req.Operation)
	req.SetupID = strings.TrimSpace(req.SetupID)
	req.Code = strings.TrimSpace(req.Code)

	switch req.Operation {
	case "bind":
		if !singleton.Conf.LoginAvailable() {
			c.JSON(http.StatusOK, model.Response{
				Code:    http.StatusBadRequest,
				Message: "请先启用至少一种登录方式",
			})
			return
		}
		if singleton.Conf.TwoFactorActive() {
			c.JSON(http.StatusOK, model.Response{
				Code:    http.StatusBadRequest,
				Message: "双重验证已启用",
			})
			return
		}
		secret, err := totp.GenerateSecret()
		if err != nil {
			c.JSON(http.StatusOK, model.Response{
				Code:    http.StatusInternalServerError,
				Message: "生成密钥失败，请稍后重试",
			})
			return
		}
		setupID, err := utils.GenerateRandomString(24)
		if err != nil {
			c.JSON(http.StatusOK, model.Response{
				Code:    http.StatusInternalServerError,
				Message: "生成绑定信息失败，请稍后重试",
			})
			return
		}
		singleton.Cache.Set(twoFactorSetupCachePrefix+setupID, secret, twoFactorSetupTTL)
		issuer := singleton.Conf.Site.Brand
		if issuer == "" {
			issuer = "Nezha"
		}
		otpauth := totp.BuildOTPAuthURL(issuer, "Nezha", secret)
		png, err := qrcode.Encode(otpauth, qrcode.Medium, 220)
		if err != nil {
			singleton.Cache.Delete(twoFactorSetupCachePrefix + setupID)
			c.JSON(http.StatusOK, model.Response{
				Code:    http.StatusInternalServerError,
				Message: "生成二维码失败，请稍后重试",
			})
			return
		}
		c.JSON(http.StatusOK, model.Response{
			Code: http.StatusOK,
			Result: gin.H{
				"setupId": setupID,
				"qr":      "data:image/png;base64," + base64.StdEncoding.EncodeToString(png),
			},
		})

	case "confirm":
		if !singleton.Conf.LoginAvailable() {
			c.JSON(http.StatusOK, model.Response{
				Code:    http.StatusBadRequest,
				Message: "请先启用至少一种登录方式",
			})
			return
		}
		if singleton.Conf.TwoFactorActive() {
			c.JSON(http.StatusOK, model.Response{
				Code:    http.StatusBadRequest,
				Message: "双重验证已启用",
			})
			return
		}
		if req.SetupID == "" || req.Code == "" {
			c.JSON(http.StatusOK, model.Response{
				Code:    http.StatusBadRequest,
				Message: "请扫描二维码并输入验证码",
			})
			return
		}
		secretVal, ok := singleton.Cache.Get(twoFactorSetupCachePrefix + req.SetupID)
		if !ok {
			c.JSON(http.StatusOK, model.Response{
				Code:    http.StatusBadRequest,
				Message: "绑定信息已过期，请重新打开弹窗",
			})
			return
		}
		secret, ok := secretVal.(string)
		if !ok || secret == "" {
			c.JSON(http.StatusOK, model.Response{
				Code:    http.StatusBadRequest,
				Message: "绑定信息无效，请重新打开弹窗",
			})
			return
		}
		if !totp.Validate(secret, req.Code, 1) {
			c.JSON(http.StatusOK, model.Response{
				Code:    http.StatusBadRequest,
				Message: "双重验证码错误，请重试",
			})
			return
		}
		singleton.Cache.Delete(twoFactorSetupCachePrefix + req.SetupID)
		singleton.Conf.Site.TwoFactorSecret = secret
		if err := singleton.Conf.Save(); err != nil {
			singleton.Conf.Site.TwoFactorSecret = ""
			c.JSON(http.StatusOK, model.Response{
				Code:    http.StatusBadRequest,
				Message: fmt.Sprintf("保存失败：%s", err),
			})
			return
		}
		c.JSON(http.StatusOK, model.Response{
			Code:    http.StatusOK,
			Message: "双重验证已启用",
		})
		audit.Record(c, audit.TypeSecurity, "Two-factor enabled", "TOTP binding confirmed and saved to site config")

	case "unbind":
		if !singleton.Conf.TwoFactorActive() {
			c.JSON(http.StatusOK, model.Response{
				Code:    http.StatusBadRequest,
				Message: "双重验证未启用",
			})
			return
		}
		// 关闭二次验证需先用当前 TOTP 码二次确认（修复会话被盗即可直接关闭 2FA 的缺口）。
		// 与开启时的 confirm 一致，要求输入当前有效的双重验证码。
		if req.Code == "" {
			c.JSON(http.StatusOK, model.Response{
				Code:    http.StatusBadRequest,
				Message: "请输入当前双重验证码",
			})
			return
		}
		// 限速防止持有被盗会话暴力猜 6 位码关闭 2FA；被限速的请求不写审计。
		if !allowAuthRateLimitedCheck(c) {
			c.JSON(http.StatusOK, model.Response{
				Code:    http.StatusTooManyRequests,
				Message: "请求过于频繁，请稍后再试",
			})
			return
		}
		if !totp.Validate(singleton.Conf.Site.TwoFactorSecret, req.Code, 1) {
			audit.Record(c, audit.TypeSecurity, "Two-factor disable failed", "invalid two-factor code on unbind")
			c.JSON(http.StatusOK, model.Response{
				Code:    http.StatusBadRequest,
				Message: "双重验证码错误，请重试",
			})
			return
		}
		oldSecret := singleton.Conf.Site.TwoFactorSecret
		singleton.Conf.Site.TwoFactorSecret = ""
		if err := singleton.Conf.Save(); err != nil {
			singleton.Conf.Site.TwoFactorSecret = oldSecret
			c.JSON(http.StatusOK, model.Response{
				Code:    http.StatusBadRequest,
				Message: fmt.Sprintf("保存失败：%s", err),
			})
			return
		}
		c.JSON(http.StatusOK, model.Response{
			Code:    http.StatusOK,
			Message: "双重验证已关闭",
		})
		audit.Record(c, audit.TypeSecurity, "Two-factor disabled", "TOTP secret removed from site config")

	default:
		c.JSON(http.StatusOK, model.Response{
			Code:    http.StatusBadRequest,
			Message: "未知操作",
		})
	}
}

func (ma *memberAPI) batchDeleteServer(c *gin.Context) {
	var servers []uint64
	if err := c.ShouldBindJSON(&servers); err != nil {
		c.JSON(http.StatusOK, model.Response{
			Code:    http.StatusBadRequest,
			Message: err.Error(),
		})
		return
	}
	serverDetails := make([]string, len(servers))
	for i, id := range servers {
		serverDetails[i] = serverAuditLabel(id)
	}
	if err := singleton.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Unscoped().Delete(&model.Server{}, "id in (?)", servers).Error; err != nil {
			return err
		}
		if err := tx.Unscoped().Delete(&model.MonitorHistory{}, "server_id in (?)", servers).Error; err != nil {
			return err
		}
		return nil
	}); err != nil {
		c.JSON(http.StatusOK, model.Response{
			Code:    http.StatusBadRequest,
			Message: err.Error(),
		})
		return
	}
	for i := 0; i < len(servers); i++ {
		id := servers[i]
		onServerDeleteWithTaskClose(id)
	}
	singleton.ReSortServer()
	for _, detail := range serverDetails {
		audit.Record(c, audit.TypeConfig, "Server deleted", detail)
	}
	c.JSON(http.StatusOK, model.Response{
		Code: http.StatusOK,
	})
}

func serverAuditLabel(id uint64) string {
	singleton.ServerLock.RLock()
	s := singleton.ServerList[id]
	singleton.ServerLock.RUnlock()
	if s != nil {
		return fmt.Sprintf("server: %s (ID %d)", s.Name, id)
	}
	var row model.Server
	if err := singleton.DB.Select("name").First(&row, id).Error; err == nil && row.Name != "" {
		return fmt.Sprintf("server: %s (ID %d)", row.Name, id)
	}
	return fmt.Sprintf("server ID %d", id)
}

func onServerDeleteWithTaskClose(id uint64) {
	singleton.ServerLock.RLock()
	server := singleton.ServerList[id]
	singleton.ServerLock.RUnlock()
	if server == nil {
		return
	}
	if server.TaskCloseLock != nil {
		server.TaskCloseLock.Lock()
		defer server.TaskCloseLock.Unlock()
	}

	singleton.ServerLock.Lock()
	defer singleton.ServerLock.Unlock()
	if singleton.ServerList[id] != server {
		return
	}
	if server.TaskClose != nil {
		close(server.TaskClose)
	}
	server.TaskClose = nil
	server.TaskStream = nil
	onServerDelete(id)
}

func onServerDelete(id uint64) {
	server := singleton.ServerList[id]
	if server == nil {
		return
	}
	tag := server.Tag
	delete(singleton.SecretToID, server.Secret)
	delete(singleton.ServerList, id)
	index := -1
	for i := 0; i < len(singleton.ServerTagToIDList[tag]); i++ {
		if singleton.ServerTagToIDList[tag][i] == id {
			index = i
			break
		}
	}
	if index > -1 {

		singleton.ServerTagToIDList[tag] = append(singleton.ServerTagToIDList[tag][:index], singleton.ServerTagToIDList[tag][index+1:]...)
		if len(singleton.ServerTagToIDList[tag]) == 0 {
			delete(singleton.ServerTagToIDList, tag)
		}
	}

	singleton.AlertsLock.Lock()
	for i := 0; i < len(singleton.Alerts); i++ {
		if singleton.AlertsCycleTransferStatsStore[singleton.Alerts[i].ID] != nil {
			delete(singleton.AlertsCycleTransferStatsStore[singleton.Alerts[i].ID].ServerName, id)
			delete(singleton.AlertsCycleTransferStatsStore[singleton.Alerts[i].ID].Transfer, id)
			delete(singleton.AlertsCycleTransferStatsStore[singleton.Alerts[i].ID].NextUpdate, id)
		}
	}
	singleton.AlertsLock.Unlock()

	singleton.DB.Unscoped().Delete(&model.Transfer{}, "server_id = ?", id)
}
