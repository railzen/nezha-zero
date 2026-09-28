package controller

import (
	"encoding/json"
	"fmt"
	"html/template"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/nicksnyder/go-i18n/v2/i18n"
	"github.com/railzen/nezha-zero/model"
	"github.com/railzen/nezha-zero/pkg/audit"
	"github.com/railzen/nezha-zero/pkg/geoip"
	"github.com/railzen/nezha-zero/pkg/mygin"
	"github.com/railzen/nezha-zero/service/singleton"
)

type memberPage struct {
	r *gin.Engine
}

func (mp *memberPage) serve() {
	mr := mp.r.Group("")
	mr.Use(mygin.Authorize(mygin.AuthorizeOption{
		MemberOnly: true,
		IsPage:     true,
		Msg:        singleton.Localizer.MustLocalize(&i18n.LocalizeConfig{MessageID: "YouAreNotAuthorized"}),
		Btn:        singleton.Localizer.MustLocalize(&i18n.LocalizeConfig{MessageID: "Login"}),
		Redirect:   "/login",
	}))
	mr.GET("/server", mp.server)
	mr.GET("/subscription", mp.subscription)
	mr.GET("/monitor", mp.monitor)
	mr.GET("/cron", mp.cron)
	mr.GET("/notification", mp.notification)
	mr.GET("/ddns", mp.ddns)
	mr.GET("/nat", mp.nat)
	mr.GET("/setting", mp.setting)
	mr.GET("/log", mp.log)
	mr.GET("/api", mp.api)
}

type subscriptionView struct {
	ID             uint64
	Name           string
	StartDate      string
	EndDate        string
	RemainingDays  int
	HasEndDate     bool
	Lifetime       bool
	Price          string // 原始存储价格，用于编辑表单回填
	Currency       string // 原始存储货币码
	DisplayAmount  string // 列表展示的金额数值（可能已换算/月度化，无货币码）
	DisplayUnit    string // 列表展示金额对应的货币码
	OriginalAmount string // 换算后括号内附带的原始金额数值（无货币码）
	OriginalUnit   string // 原始金额对应的货币码
	PriceUnit      string
	PriceUnitLabel string
	DisplayIndex   int
	MonthlyCost    float64
	YearlyCost     float64
	CostCurrency   string
	HasCost        bool
	CostReason     string
	Link           string
	Note           string
	Group          string
	Manual         bool
	Server         bool
	AutoRenewal    bool
	Enabled        bool
}

func subscriptionDate(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.Format("2006-01-02")
}

func subscriptionFormCycle(cycle string, lifetime bool) string {
	if lifetime || model.IsLifetimeSubscriptionCycle(cycle) {
		return "永续"
	}
	if canonical := model.CanonicalSubscriptionCycle(cycle); canonical != "" {
		return canonical
	}
	return strings.TrimSpace(cycle)
}

func (row subscriptionView) FormData() template.JS {
	data, _ := json.Marshal(struct {
		ID           uint64 `json:"ID"`
		Server       bool   `json:"Server"`
		Name         string `json:"Name"`
		StartDate    string `json:"StartDate"`
		EndDate      string `json:"EndDate"`
		Price        string `json:"Price"`
		PriceUnit    string `json:"PriceUnit"`
		Currency     string `json:"Currency"`
		Link         string `json:"Link"`
		DisplayIndex int    `json:"DisplayIndex"`
		Note         string `json:"Note"`
		Group        string `json:"Group"`
		AutoRenewal  bool   `json:"AutoRenewal"`
		Enable       bool   `json:"Enable"`
	}{
		ID: row.ID, Server: row.Server, Name: row.Name,
		StartDate: row.StartDate, EndDate: row.EndDate,
		Price: row.Price, PriceUnit: row.PriceUnit, Currency: row.Currency,
		Link: row.Link, Note: row.Note, Group: row.Group,
		AutoRenewal: row.AutoRenewal, Enable: row.Enabled,
		DisplayIndex: row.DisplayIndex,
	})
	return template.JS(data)
}

func (mp *memberPage) subscription(c *gin.Context) {
	if !singleton.Conf.EnableSubscription {
		c.AbortWithStatus(http.StatusNotFound)
		return
	}
	now := time.Now()
	rates, _, _ := singleton.CurrencyRateSnapshot()
	// convertPrice 对单条订阅计算列表展示价格：
	// 括号外（主价格）由"按照月度价格显示"和"启用货币换算"决定；
	// 括号内显示原始价格与原货币：换算后货币不同，或按月折算后金额不同时出现
	convertPrice := func(price, priceUnit, currency string) (displayAmount, originalAmount, displayUnit string) {
		currency = strings.ToUpper(strings.TrimSpace(currency))
		amount, ok := model.ParsePriceAmount(price)
		if !ok || amount == 0 {
			return "", "", ""
		}
		divisor := float64(subscriptionPriceDivisor(singleton.Conf.ShowMonthlyPrice, priceUnit))
		if singleton.Conf.EnableCurrencyConversion && currency != "" {
			converted, err := model.ConvertCurrency(amount, currency, singleton.Conf.BaseCurrency, rates)
			if err == nil {
				displayAmount = formatSubscriptionAmount(converted / divisor)
				displayUnit = singleton.Conf.BaseCurrency
			}
		}
		if displayAmount == "" {
			displayAmount = formatSubscriptionAmount(amount / divisor)
			displayUnit = currency
		}
		originalAmount = formatSubscriptionAmount(amount)
		if displayUnit == currency && displayAmount == originalAmount {
			originalAmount = ""
		}
		return displayAmount, originalAmount, displayUnit
	}
	recurringCost := func(price, priceUnit, currency string) (float64, float64, string, bool, string) {
		cycle := strings.ToLower(strings.TrimSpace(priceUnit))
		if model.IsLifetimeSubscriptionCycle(cycle) {
			return 0, 0, "", false, ""
		}
		amount, ok := model.ParsePriceAmount(price)
		if strings.TrimSpace(price) == "" {
			return 0, 0, "", false, ""
		}
		if !ok {
			return 0, 0, "", false, "SubscriptionCostInvalidPrice"
		}
		if amount == 0 {
			return 0, 0, "", false, ""
		}
		currency = strings.ToUpper(strings.TrimSpace(currency))
		if currency == "" {
			return 0, 0, "", false, "SubscriptionCostMissingCurrency"
		}
		if cycle == "" {
			return 0, 0, "", false, "SubscriptionCostMissingCycle"
		}
		months := model.SubscriptionCycleMonths(cycle)
		if months == 0 {
			return 0, 0, "", false, "SubscriptionCostInvalidCycle"
		}
		if singleton.Conf.EnableCurrencyConversion {
			var err error
			amount, err = model.ConvertCurrency(amount, currency, singleton.Conf.BaseCurrency, rates)
			if err != nil {
				return 0, 0, "", false, "SubscriptionCostMissingRate"
			}
			currency = singleton.Conf.BaseCurrency
		}
		monthly := amount / float64(months)
		return monthly, monthly * 12, currency, true, ""
	}
	formatPriceUnit := func(value string, lifetime bool) string {
		if lifetime || model.IsLifetimeSubscriptionCycle(value) {
			return singleton.Localizer.MustLocalize(&i18n.LocalizeConfig{MessageID: "PNELifetime"})
		}
		messageID := ""
		switch model.SubscriptionCycleMonths(value) {
		case 1:
			messageID = "PNECycleMonthly"
		case 3:
			messageID = "PNECycleQuarterly"
		case 6:
			messageID = "PNECycleHalfYear"
		case 12:
			messageID = "PNECycleYearly"
		case 24:
			messageID = "BillingCycleTwoYears"
		case 36:
			messageID = "BillingCycleThreeYears"
		case 60:
			messageID = "BillingCycleFiveYears"
		}
		if messageID == "" {
			return value
		}
		return singleton.Localizer.MustLocalize(&i18n.LocalizeConfig{MessageID: messageID})
	}
	rows := make([]subscriptionView, 0)
	if singleton.Conf.SyncServerSubscription {
		singleton.SortedServerLock.RLock()
		for _, server := range singleton.SortedServerList {
			item := model.ParseServerSubscription(server, now)
			lifetime := item.Lifetime || model.IsLifetimeSubscriptionCycle(item.PriceUnit)
			costCycle := item.PriceUnit
			if lifetime {
				costCycle = "永续"
			}
			monthlyCost, yearlyCost, costCurrency, hasCost, costReason := recurringCost(item.Price, costCycle, item.Currency)
			remainingDays := 0
			hasEndDate := false
			if !lifetime {
				remainingDays = model.SubscriptionRemainingDays(now, item.EndDate)
				hasEndDate = !item.EndDate.IsZero()
			}
			if hasEndDate && remainingDays < 0 {
				costReason = ""
			}
			displayAmount, originalAmount, displayUnit := convertPrice(item.Price, costCycle, item.Currency)
			if displayAmount == "" {
				// 无法解析价格时按原文本展示
				displayAmount = item.Price
				displayUnit = item.Currency
				originalAmount = ""
			}
			rows = append(rows, subscriptionView{
				ID:             item.ServerID,
				Server:         true,
				Name:           item.Name,
				StartDate:      subscriptionDate(item.StartDate),
				EndDate:        subscriptionDate(item.EndDate),
				RemainingDays:  remainingDays,
				HasEndDate:     hasEndDate,
				Lifetime:       lifetime,
				Price:          item.Price,
				Currency:       item.Currency,
				DisplayAmount:  displayAmount,
				DisplayUnit:    displayUnit,
				OriginalAmount: originalAmount,
				OriginalUnit:   item.Currency,
				PriceUnit:      subscriptionFormCycle(item.PriceUnit, lifetime),
				PriceUnitLabel: formatPriceUnit(item.PriceUnit, lifetime),
				MonthlyCost:    monthlyCost,
				YearlyCost:     yearlyCost,
				CostCurrency:   costCurrency,
				HasCost:        hasCost,
				CostReason:     costReason,
				Link:           normalizeSubscriptionLink(server.Link),
				Note:           server.Note,
				Group:          item.Group,
				AutoRenewal:    item.AutoRenewal,
				Enabled:        true,
			})
		}
		singleton.SortedServerLock.RUnlock()
	}

	var subscriptions []model.Subscription
	singleton.DB.Order("id").Find(&subscriptions)
	// 手动订阅按 DisplayIndex 降序（越大越靠前），DisplayIndex 相同时按 ID 升序
	sort.SliceStable(subscriptions, func(i, j int) bool {
		if subscriptions[i].DisplayIndex == subscriptions[j].DisplayIndex {
			return subscriptions[i].ID < subscriptions[j].ID
		}
		return subscriptions[i].DisplayIndex > subscriptions[j].DisplayIndex
	})
	for i := range subscriptions {
		item := subscriptions[i]
		currency := item.Currency
		if currency == "" {
			currency = model.DetectCurrency(item.Price)
		}
		lifetime := model.IsLifetimeSubscriptionCycle(item.PriceUnit)
		monthlyCost, yearlyCost, costCurrency, hasCost, costReason := recurringCost(item.Price, item.PriceUnit, currency)
		remainingDays := 0
		hasEndDate := false
		if !lifetime {
			remainingDays = model.SubscriptionRemainingDays(now, item.EndDate)
			hasEndDate = !item.EndDate.IsZero()
		}
		if item.Disabled || (hasEndDate && remainingDays < 0) {
			costReason = ""
		}
		displayAmount, originalAmount, displayUnit := convertPrice(item.Price, item.PriceUnit, currency)
		if displayAmount == "" {
			displayAmount = item.Price
			displayUnit = currency
			originalAmount = ""
		}
		rows = append(rows, subscriptionView{
			ID:             item.ID,
			Name:           item.Name,
			StartDate:      subscriptionDate(item.StartDate),
			EndDate:        subscriptionDate(item.EndDate),
			RemainingDays:  remainingDays,
			HasEndDate:     hasEndDate,
			Lifetime:       lifetime,
			Price:          item.Price,
			Currency:       currency,
			DisplayAmount:  displayAmount,
			DisplayUnit:    displayUnit,
			OriginalAmount: originalAmount,
			OriginalUnit:   currency,
			PriceUnit:      subscriptionFormCycle(item.PriceUnit, lifetime),
			PriceUnitLabel: formatPriceUnit(item.PriceUnit, lifetime),
			MonthlyCost:    monthlyCost,
			YearlyCost:     yearlyCost,
			CostCurrency:   costCurrency,
			HasCost:        hasCost,
			CostReason:     costReason,
			Link:           normalizeSubscriptionLink(item.Link),
			Note:           item.Note,
			Group:          item.Group,
			Manual:         true,
			AutoRenewal:    item.AutoRenewal,
			Enabled:        !item.Disabled,
			DisplayIndex:   item.DisplayIndex,
		})
	}
	c.HTML(http.StatusOK, "dashboard-"+singleton.Conf.Site.DashboardTheme+"/subscription", mygin.CommonEnvironment(c, gin.H{
		"Title":         singleton.Localizer.MustLocalize(&i18n.LocalizeConfig{MessageID: "SubscriptionManagement"}),
		"Subscriptions": rows,
		"Currencies":    model.SortedCurrencies(),
	}))
}

func subscriptionDateTime(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.Format("2006-01-02 15:04:05")
}

// subscriptionPriceDivisor 返回月度折算除数：开启"按照月度价格显示"后，
// 价格按付费周期月数摊销为每月价格；永续与无法识别的周期不折算
// （与 Wallos 的 getPricePerMonth 行为一致）
func subscriptionPriceDivisor(showMonthlyPrice bool, priceUnit string) int {
	if !showMonthlyPrice {
		return 1
	}
	cycle := strings.ToLower(strings.TrimSpace(priceUnit))
	if cycle == "" || model.IsLifetimeSubscriptionCycle(cycle) {
		return 1
	}
	if months := model.SubscriptionCycleMonths(cycle); months > 0 {
		return months
	}
	return 1
}

// formatSubscriptionAmount 把金额按千分位格式化（如 "1,200.00"），不附带货币码
func formatSubscriptionAmount(amount float64) string {
	amountText := fmt.Sprintf("%.2f", amount)
	whole, fraction, _ := strings.Cut(amountText, ".")
	negative := strings.HasPrefix(whole, "-")
	whole = strings.TrimPrefix(whole, "-")
	grouped := ""
	for i, digit := range whole {
		if i > 0 && (len(whole)-i)%3 == 0 {
			grouped += ","
		}
		grouped += string(digit)
	}
	if negative {
		grouped = "-" + grouped
	}
	return grouped + "." + fraction
}

func (mp *memberPage) api(c *gin.Context) {
	singleton.ApiLock.RLock()
	defer singleton.ApiLock.RUnlock()
	c.HTML(http.StatusOK, "dashboard-"+singleton.Conf.Site.DashboardTheme+"/api", mygin.CommonEnvironment(c, gin.H{
		"title":  singleton.Localizer.MustLocalize(&i18n.LocalizeConfig{MessageID: "ApiManagement"}),
		"Tokens": singleton.ApiTokenList,
	}))
}

func (mp *memberPage) server(c *gin.Context) {
	singleton.SortedServerLock.RLock()
	defer singleton.SortedServerLock.RUnlock()
	c.HTML(http.StatusOK, "dashboard-"+singleton.Conf.Site.DashboardTheme+"/server", mygin.CommonEnvironment(c, gin.H{
		"Title":   singleton.Localizer.MustLocalize(&i18n.LocalizeConfig{MessageID: "ServersManagement"}),
		"Servers": singleton.SortedServerList,
	}))
}

func (mp *memberPage) monitor(c *gin.Context) {
	c.HTML(http.StatusOK, "dashboard-"+singleton.Conf.Site.DashboardTheme+"/monitor", mygin.CommonEnvironment(c, gin.H{
		"Title":    singleton.Localizer.MustLocalize(&i18n.LocalizeConfig{MessageID: "ServicesManagement"}),
		"Monitors": singleton.ServiceSentinelShared.Monitors(),
	}))
}

func (mp *memberPage) cron(c *gin.Context) {
	var crons []model.Cron
	singleton.DB.Find(&crons)
	c.HTML(http.StatusOK, "dashboard-"+singleton.Conf.Site.DashboardTheme+"/cron", mygin.CommonEnvironment(c, gin.H{
		"Title": singleton.Localizer.MustLocalize(&i18n.LocalizeConfig{MessageID: "ScheduledTasks"}),
		"Crons": crons,
	}))
}

func (mp *memberPage) notification(c *gin.Context) {
	var nf []model.Notification
	singleton.DB.Find(&nf)
	for i := range nf {
		if nf[i].TelegramToken != "" {
			nf[i].TelegramToken = "********"
		}
		if nf[i].SMTPPassword != "" {
			nf[i].SMTPPassword = "********"
		}
	}
	var ar []model.AlertRule
	singleton.DB.Find(&ar)
	standardRules := make([]model.AlertRule, 0, len(ar))
	expirationRules := make([]model.AlertRule, 0)
	subscriptionExpirationRules := make([]model.AlertRule, 0)
	for _, rule := range ar {
		if rule.IsSubscriptionExpirationRule() {
			if singleton.Conf.EnableSubscription {
				subscriptionExpirationRules = append(subscriptionExpirationRules, rule)
			}
		} else if rule.IsExpirationRule() {
			expirationRules = append(expirationRules, rule)
		} else {
			standardRules = append(standardRules, rule)
		}
	}
	c.HTML(http.StatusOK, "dashboard-"+singleton.Conf.Site.DashboardTheme+"/notification", mygin.CommonEnvironment(c, gin.H{
		"Title":                       singleton.Localizer.MustLocalize(&i18n.LocalizeConfig{MessageID: "Notification"}),
		"Notifications":               nf,
		"AlertRules":                  standardRules,
		"ExpirationRules":             expirationRules,
		"SubscriptionExpirationRules": subscriptionExpirationRules,
	}))
}

func (mp *memberPage) ddns(c *gin.Context) {
	var data []model.DDNSProfile
	singleton.DB.Find(&data)
	c.HTML(http.StatusOK, "dashboard-"+singleton.Conf.Site.DashboardTheme+"/ddns", mygin.CommonEnvironment(c, gin.H{
		"Title":        singleton.Localizer.MustLocalize(&i18n.LocalizeConfig{MessageID: "DDNS"}),
		"DDNS":         data,
		"ProviderMap":  model.ProviderMap,
		"ProviderList": model.ProviderList,
	}))
}

func (mp *memberPage) nat(c *gin.Context) {
	var data []model.NAT
	singleton.DB.Find(&data)
	c.HTML(http.StatusOK, "dashboard-"+singleton.Conf.Site.DashboardTheme+"/nat", mygin.CommonEnvironment(c, gin.H{
		"Title": singleton.Localizer.MustLocalize(&i18n.LocalizeConfig{MessageID: "NAT"}),
		"NAT":   data,
	}))
}

func (mp *memberPage) setting(c *gin.Context) {
	geoIPUpdatedAt := ""
	if geoip.Downloaded() {
		loc, err := time.LoadLocation(singleton.Conf.Location)
		if err != nil {
			loc = time.Local
		}
		geoIPUpdatedAt = geoip.UpdatedAt().In(loc).Format("2006-01-02 15:04:05")
	}
	_, currencyRatesUpdatedAt, _ := singleton.CurrencyRateSnapshot()
	currencyUsage, currencyUsageAvailable, _ := singleton.CurrencyUsageSnapshot()
	c.HTML(http.StatusOK, "dashboard-"+singleton.Conf.Site.DashboardTheme+"/setting", mygin.CommonEnvironment(c, gin.H{
		"Title":                  singleton.Localizer.MustLocalize(&i18n.LocalizeConfig{MessageID: "Settings"}),
		"Languages":              model.Languages,
		"DashboardThemes":        model.DashboardThemes,
		"GeoIPDownloaded":        geoip.Downloaded(),
		"GeoIPUpdatedAt":         geoIPUpdatedAt,
		"Currencies":             model.SortedCurrencies(),
		"CurrencyRatesUpdatedAt": subscriptionDateTime(currencyRatesUpdatedAt),
		"CurrencyUsageAvailable": currencyUsageAvailable,
		"CurrencyUsageUsed":      currencyUsage.MonthlyUsed,
		"CurrencyUsageLimit":     currencyUsage.MonthlyLimit,
	}))
}

func (mp *memberPage) log(c *gin.Context) {
	audit.PruneExcess()
	filterType := c.Query("type")
	if filterType == "" {
		filterType = "all"
	}
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	if page < 1 {
		page = 1
	}

	q := singleton.DB.Model(&model.AuditLog{})
	if filterType != "all" {
		q = q.Where("type = ?", filterType)
	}
	var total int64
	q.Count(&total)

	totalPages := int((total + int64(audit.PageSize) - 1) / int64(audit.PageSize))
	if totalPages < 1 {
		totalPages = 1
	}
	if page > totalPages {
		page = totalPages
	}

	var logs []model.AuditLog
	q.Order("created_at DESC").Offset((page - 1) * audit.PageSize).Limit(audit.PageSize).Find(&logs)

	pageInfo := singleton.Localizer.MustLocalize(&i18n.LocalizeConfig{
		MessageID: "LogPageInfo",
		TemplateData: map[string]interface{}{
			"Page":       page,
			"TotalPages": totalPages,
		},
	})

	c.HTML(http.StatusOK, "dashboard-"+singleton.Conf.Site.DashboardTheme+"/log", mygin.CommonEnvironment(c, gin.H{
		"Title":      singleton.Localizer.MustLocalize(&i18n.LocalizeConfig{MessageID: "Log"}),
		"Logs":       logs,
		"Total":      total,
		"FilterType": filterType,
		"Page":       page,
		"TotalPages": totalPages,
		"PageInfo":   pageInfo,
		"HasPrev":    page > 1,
		"HasNext":    page < totalPages,
		"PrevPage":   page - 1,
		"NextPage":   page + 1,
	}))
}
