package mygin

import (
	"fmt"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/nicksnyder/go-i18n/v2/i18n"

	"github.com/railzen/nezha-zero/model"
	"github.com/railzen/nezha-zero/service/singleton"
)

var adminPage = map[string]bool{
	"/server":       true,
	"/subscription": true,
	"/monitor":      true,
	"/setting":      true,
	"/notification": true,
	"/ddns":         true,
	"/nat":          true,
	"/cron":         true,
	"/api":          true,
	"/log":          true,
}

func CommonEnvironment(c *gin.Context, data map[string]interface{}) gin.H {
	data["MatchedPath"] = c.MustGet("MatchedPath")
	data["Version"] = singleton.Version
	data["Conf"] = singleton.Conf
	data["Themes"] = model.AvailableThemes(singleton.Conf.CompatAPIDisable, singleton.Conf.Site.Theme)
	data["CustomCode"] = singleton.Conf.Site.CustomCode
	data["CustomCodeDashboard"] = singleton.Conf.Site.CustomCodeDashboard
	// 已添加通行密钥：可用于免密码登录，并在需要二次验证时代替动态验证码
	data["PasskeyEnabled"] = singleton.PasskeyEnabled()
	// 是否是管理页面
	data["IsAdminPage"] = adminPage[data["MatchedPath"].(string)]
	// 站点标题
	if t, has := data["Title"]; !has {
		data["Title"] = singleton.Conf.Site.Brand
	} else {
		data["Title"] = fmt.Sprintf("%s - %s", t, singleton.Conf.Site.Brand)
	}
	u, ok := c.Get(model.CtxKeyAuthorizedUser)
	if ok {
		data["Admin"] = u
	}
	data["LANG"] = map[string]string{
		"Add":          singleton.Localizer.MustLocalize(&i18n.LocalizeConfig{MessageID: "Add"}),
		"Edit":         singleton.Localizer.MustLocalize(&i18n.LocalizeConfig{MessageID: "Edit"}),
		"AlarmRule":    singleton.Localizer.MustLocalize(&i18n.LocalizeConfig{MessageID: "AlarmRule"}),
		"Notification": singleton.Localizer.MustLocalize(&i18n.LocalizeConfig{MessageID: "NotificationMethod"}),
		"Server":       singleton.Localizer.MustLocalize(&i18n.LocalizeConfig{MessageID: "Server"}),
		"Monitor":      singleton.Localizer.MustLocalize(&i18n.LocalizeConfig{MessageID: "ServicesManagement"}),
		"Cron":         singleton.Localizer.MustLocalize(&i18n.LocalizeConfig{MessageID: "ScheduledTasks"}),
	}
	return data
}

func RecordPath(c *gin.Context) {
	url := c.Request.URL.Path
	for _, p := range c.Params {
		url = strings.Replace(url, p.Value, ":"+p.Key, 1)
	}
	c.Set("MatchedPath", url)
}
