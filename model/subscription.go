package model

import (
	"encoding/json"
	"errors"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

const ServerSubscriptionGroup = "Servers"

type Currency struct {
	Code   string
	Name   string
	Symbol string
}

// CurrentCurrencySnapshotID keeps the runtime currency data as a single row.
const CurrentCurrencySnapshotID uint64 = 1

// CurrencySnapshot stores the latest provider response. Exchange rates and the
// provider quota are written together because they describe the same fetch.
type CurrencySnapshot struct {
	ID           uint64 `gorm:"primaryKey"`
	BaseCode     string `gorm:"size:3;not null"`
	Provider     string `gorm:"size:16;not null"`
	RatesRaw     string `gorm:"not null"`
	MonthlyUsed  *int64
	MonthlyLimit *int64
	FetchedAt    time.Time `gorm:"index;not null"`
}

// CurrencyUsageStatus is the read-model returned to the settings page.
type CurrencyUsageStatus struct {
	MonthlyUsed  int64
	MonthlyLimit int64
}

// Currencies contains current ISO 4217 currencies used for ordinary billing.
// Fund, precious-metal, testing and no-currency codes are intentionally omitted.
var Currencies = []Currency{
	{Code: "AED", Name: "UAE Dirham"},
	{Code: "AFN", Name: "Afghani"},
	{Code: "ALL", Name: "Lek"},
	{Code: "AMD", Name: "Armenian Dram"},
	{Code: "AOA", Name: "Kwanza"},
	{Code: "ARS", Name: "Argentine Peso"},
	{Code: "AUD", Name: "Australian Dollar"},
	{Code: "AWG", Name: "Aruban Florin"},
	{Code: "AZN", Name: "Azerbaijan Manat"},
	{Code: "BAM", Name: "Convertible Mark"},
	{Code: "BBD", Name: "Barbados Dollar"},
	{Code: "BDT", Name: "Taka"},
	{Code: "BHD", Name: "Bahraini Dinar"},
	{Code: "BIF", Name: "Burundi Franc"},
	{Code: "BMD", Name: "Bermudian Dollar"},
	{Code: "BND", Name: "Brunei Dollar"},
	{Code: "BOB", Name: "Boliviano"},
	{Code: "BRL", Name: "Brazilian Real"},
	{Code: "BSD", Name: "Bahamian Dollar"},
	{Code: "BTN", Name: "Ngultrum"},
	{Code: "BWP", Name: "Pula"},
	{Code: "BYN", Name: "Belarusian Ruble"},
	{Code: "BZD", Name: "Belize Dollar"},
	{Code: "CAD", Name: "Canadian Dollar"},
	{Code: "CDF", Name: "Congolese Franc"},
	{Code: "CHF", Name: "Swiss Franc"},
	{Code: "CLP", Name: "Chilean Peso"},
	{Code: "CNY", Name: "Chinese Yuan"},
	{Code: "COP", Name: "Colombian Peso"},
	{Code: "CRC", Name: "Costa Rican Colon"},
	{Code: "CUP", Name: "Cuban Peso"},
	{Code: "CVE", Name: "Cabo Verde Escudo"},
	{Code: "CZK", Name: "Czech Koruna"},
	{Code: "DJF", Name: "Djibouti Franc"},
	{Code: "DKK", Name: "Danish Krone"},
	{Code: "DOP", Name: "Dominican Peso"},
	{Code: "DZD", Name: "Algerian Dinar"},
	{Code: "EGP", Name: "Egyptian Pound"},
	{Code: "ERN", Name: "Nakfa"},
	{Code: "ETB", Name: "Ethiopian Birr"},
	{Code: "EUR", Name: "Euro"},
	{Code: "FJD", Name: "Fiji Dollar"},
	{Code: "FKP", Name: "Falkland Islands Pound"},
	{Code: "GBP", Name: "Pound Sterling"},
	{Code: "GEL", Name: "Lari"},
	{Code: "GHS", Name: "Ghana Cedi"},
	{Code: "GIP", Name: "Gibraltar Pound"},
	{Code: "GMD", Name: "Dalasi"},
	{Code: "GNF", Name: "Guinean Franc"},
	{Code: "GTQ", Name: "Quetzal"},
	{Code: "GYD", Name: "Guyana Dollar"},
	{Code: "HKD", Name: "Hong Kong Dollar"},
	{Code: "HNL", Name: "Lempira"},
	{Code: "HTG", Name: "Gourde"},
	{Code: "HUF", Name: "Forint"},
	{Code: "IDR", Name: "Rupiah"},
	{Code: "ILS", Name: "New Israeli Sheqel"},
	{Code: "INR", Name: "Indian Rupee"},
	{Code: "IQD", Name: "Iraqi Dinar"},
	{Code: "IRR", Name: "Iranian Rial"},
	{Code: "ISK", Name: "Iceland Krona"},
	{Code: "JMD", Name: "Jamaican Dollar"},
	{Code: "JOD", Name: "Jordanian Dinar"},
	{Code: "JPY", Name: "Yen"},
	{Code: "KES", Name: "Kenyan Shilling"},
	{Code: "KGS", Name: "Som"},
	{Code: "KHR", Name: "Riel"},
	{Code: "KMF", Name: "Comorian Franc"},
	{Code: "KPW", Name: "North Korean Won"},
	{Code: "KRW", Name: "Won"},
	{Code: "KWD", Name: "Kuwaiti Dinar"},
	{Code: "KYD", Name: "Cayman Islands Dollar"},
	{Code: "KZT", Name: "Tenge"},
	{Code: "LAK", Name: "Lao Kip"},
	{Code: "LBP", Name: "Lebanese Pound"},
	{Code: "LKR", Name: "Sri Lanka Rupee"},
	{Code: "LRD", Name: "Liberian Dollar"},
	{Code: "LSL", Name: "Loti"},
	{Code: "LYD", Name: "Libyan Dinar"},
	{Code: "MAD", Name: "Moroccan Dirham"},
	{Code: "MDL", Name: "Moldovan Leu"},
	{Code: "MGA", Name: "Malagasy Ariary"},
	{Code: "MKD", Name: "Denar"},
	{Code: "MMK", Name: "Kyat"},
	{Code: "MNT", Name: "Tugrik"},
	{Code: "MOP", Name: "Pataca"},
	{Code: "MRU", Name: "Ouguiya"},
	{Code: "MUR", Name: "Mauritius Rupee"},
	{Code: "MVR", Name: "Rufiyaa"},
	{Code: "MWK", Name: "Malawi Kwacha"},
	{Code: "MXN", Name: "Mexican Peso"},
	{Code: "MYR", Name: "Malaysian Ringgit"},
	{Code: "MZN", Name: "Mozambique Metical"},
	{Code: "NAD", Name: "Namibia Dollar"},
	{Code: "NGN", Name: "Naira"},
	{Code: "NIO", Name: "Cordoba Oro"},
	{Code: "NOK", Name: "Norwegian Krone"},
	{Code: "NPR", Name: "Nepalese Rupee"},
	{Code: "NZD", Name: "New Zealand Dollar"},
	{Code: "OMR", Name: "Rial Omani"},
	{Code: "PAB", Name: "Balboa"},
	{Code: "PEN", Name: "Sol"},
	{Code: "PGK", Name: "Kina"},
	{Code: "PHP", Name: "Philippine Peso"},
	{Code: "PKR", Name: "Pakistan Rupee"},
	{Code: "PLN", Name: "Zloty"},
	{Code: "PYG", Name: "Guarani"},
	{Code: "QAR", Name: "Qatari Rial"},
	{Code: "RON", Name: "Romanian Leu"},
	{Code: "RSD", Name: "Serbian Dinar"},
	{Code: "RUB", Name: "Russian Ruble"},
	{Code: "RWF", Name: "Rwanda Franc"},
	{Code: "SAR", Name: "Saudi Riyal"},
	{Code: "SBD", Name: "Solomon Islands Dollar"},
	{Code: "SCR", Name: "Seychelles Rupee"},
	{Code: "SDG", Name: "Sudanese Pound"},
	{Code: "SEK", Name: "Swedish Krona"},
	{Code: "SGD", Name: "Singapore Dollar"},
	{Code: "SHP", Name: "Saint Helena Pound"},
	{Code: "SLE", Name: "Leone"},
	{Code: "SOS", Name: "Somali Shilling"},
	{Code: "SRD", Name: "Surinam Dollar"},
	{Code: "SSP", Name: "South Sudanese Pound"},
	{Code: "STN", Name: "Dobra"},
	{Code: "SVC", Name: "El Salvador Colon"},
	{Code: "SYP", Name: "Syrian Pound"},
	{Code: "SZL", Name: "Lilangeni"},
	{Code: "THB", Name: "Baht"},
	{Code: "TJS", Name: "Somoni"},
	{Code: "TMT", Name: "Turkmenistan New Manat"},
	{Code: "TND", Name: "Tunisian Dinar"},
	{Code: "TOP", Name: "Pa’anga"},
	{Code: "TRY", Name: "Turkish Lira"},
	{Code: "TTD", Name: "Trinidad and Tobago Dollar"},
	{Code: "TWD", Name: "New Taiwan Dollar"},
	{Code: "TZS", Name: "Tanzanian Shilling"},
	{Code: "UAH", Name: "Hryvnia"},
	{Code: "UGX", Name: "Uganda Shilling"},
	{Code: "USD", Name: "US Dollar"},
	{Code: "UYU", Name: "Peso Uruguayo"},
	{Code: "UZS", Name: "Uzbekistan Sum"},
	{Code: "VED", Name: "Bolívar Soberano"},
	{Code: "VES", Name: "Bolívar Soberano"},
	{Code: "VND", Name: "Dong"},
	{Code: "VUV", Name: "Vatu"},
	{Code: "WST", Name: "Tala"},
	{Code: "XAF", Name: "CFA Franc BEAC"},
	{Code: "XCD", Name: "East Caribbean Dollar"},
	{Code: "XCG", Name: "Caribbean Guilder"},
	{Code: "XOF", Name: "CFA Franc BCEAO"},
	{Code: "XPF", Name: "CFP Franc"},
	{Code: "YER", Name: "Yemeni Rial"},
	{Code: "ZAR", Name: "Rand"},
	{Code: "ZMW", Name: "Zambian Kwacha"},
	{Code: "ZWG", Name: "Zimbabwe Gold"},
}

var currencyCodePattern = regexp.MustCompile(`(?i)(?:^|[^A-Z])([A-Z]{3})(?:$|[^A-Z])`)
var priceNumberPattern = regexp.MustCompile(`[-+]?\d[\d.,]*`)

func CurrencyCodes() []string {
	codes := make([]string, 0, len(Currencies))
	for _, currency := range Currencies {
		codes = append(codes, currency.Code)
	}
	return codes
}

func IsSupportedCurrency(code string) bool {
	code = strings.ToUpper(strings.TrimSpace(code))
	for _, currency := range Currencies {
		if currency.Code == code {
			return true
		}
	}
	return false
}

func DetectCurrency(value string) string {
	upper := strings.ToUpper(value)
	for _, match := range currencyCodePattern.FindAllStringSubmatch(upper, -1) {
		if len(match) > 1 && IsSupportedCurrency(match[1]) {
			return match[1]
		}
		if len(match) > 1 && match[1] == "RMB" { // RMB 不是 ISO 4217 代码，识别为 CNY
			return "CNY"
		}
	}
	symbols := []struct{ Symbol, Code string }{
		{"NT$", "TWD"}, {"HK$", "HKD"}, {"NZ$", "NZD"}, {"Mex$", "MXN"},
		{"A$", "AUD"}, {"C$", "CAD"}, {"S$", "SGD"}, {"R$", "BRL"},
		{"￥", "CNY"}, {"¥", "CNY"}, {"円", "JPY"}, {"€", "EUR"}, {"£", "GBP"},
		{"₹", "INR"}, {"₩", "KRW"}, {"₽", "RUB"}, {"₺", "TRY"}, {"฿", "THB"},
		{"₱", "PHP"}, {"₫", "VND"}, {"₪", "ILS"}, {"zł", "PLN"}, {"Kč", "CZK"},
		{"лв", "BGN"}, {"$", "USD"},
	}
	for _, item := range symbols {
		if strings.Contains(value, item.Symbol) {
			return item.Code
		}
	}
	return ""
}

func ParsePriceAmount(value string) (float64, bool) {
	number := priceNumberPattern.FindString(value)
	if number == "" {
		return 0, false
	}
	lastComma, lastDot := strings.LastIndex(number, ","), strings.LastIndex(number, ".")
	switch {
	case lastComma >= 0 && lastDot >= 0:
		if lastComma > lastDot {
			number = strings.ReplaceAll(strings.ReplaceAll(number, ".", ""), ",", ".")
		} else {
			number = strings.ReplaceAll(number, ",", "")
		}
	case lastComma >= 0:
		digitsAfter := len(number) - lastComma - 1
		if digitsAfter == 1 || digitsAfter == 2 {
			number = strings.ReplaceAll(number, ",", ".")
		} else {
			number = strings.ReplaceAll(number, ",", "")
		}
	}
	amount, err := strconv.ParseFloat(number, 64)
	return amount, err == nil
}

func ConvertCurrency(amount float64, from, to string, rates map[string]float64) (float64, error) {
	from, to = strings.ToUpper(strings.TrimSpace(from)), strings.ToUpper(strings.TrimSpace(to))
	if from == to {
		return amount, nil
	}
	fromRate, fromOK := rates[from]
	toRate, toOK := rates[to]
	if !fromOK || !toOK || fromRate <= 0 || toRate <= 0 {
		return 0, errors.New("exchange rate unavailable")
	}
	return amount / fromRate * toRate, nil
}

func SortedCurrencies() []Currency {
	items := append([]Currency(nil), Currencies...)
	sort.Slice(items, func(i, j int) bool { return items[i].Code < items[j].Code })
	return items
}

// Subscription stores subscriptions entered manually. Server subscriptions are
// read directly from Server.PublicNote and are never copied into this table.
type Subscription struct {
	Common
	Name         string
	StartDate    time.Time
	EndDate      time.Time `gorm:"index"`
	Price        string
	PriceUnit    string
	Currency     string `gorm:"size:3"`
	Link         string
	DisplayIndex int
	Note         string
	Group        string `gorm:"column:group_name"`
	AutoRenewal  bool   `gorm:"not null"`
	Disabled     bool   `gorm:"default:false"`
}

type ServerSubscription struct {
	ServerID    uint64
	Name        string
	StartDate   time.Time
	EndDate     time.Time
	Lifetime    bool
	Price       string
	PriceUnit   string
	Currency    string
	Group       string
	AutoRenewal bool
}

// ParseServerSubscription reads billing data from the existing server row.
func ParseServerSubscription(server *Server, now time.Time) ServerSubscription {
	result := ServerSubscription{Group: ServerSubscriptionGroup}
	if server == nil {
		return result
	}
	result.ServerID = server.ID
	result.Name = server.Name

	var note struct {
		BillingDataMod struct {
			StartDate   string          `json:"startDate"`
			EndDate     string          `json:"endDate"`
			Cycle       string          `json:"cycle"`
			Amount      json.RawMessage `json:"amount"`
			AutoRenewal json.RawMessage `json:"autoRenewal"`
		} `json:"billingDataMod"`
	}
	if json.Unmarshal([]byte(server.PublicNote), &note) != nil {
		return result
	}
	result.StartDate, _ = ParseSubscriptionDate(note.BillingDataMod.StartDate)
	result.EndDate, _ = ParseSubscriptionDate(note.BillingDataMod.EndDate)
	result.Lifetime = strings.Contains(note.BillingDataMod.EndDate, "0000-00-00")
	result.Price = rawSubscriptionValue(note.BillingDataMod.Amount)
	result.PriceUnit = strings.TrimSpace(note.BillingDataMod.Cycle)
	result.Currency = DetectCurrency(result.Price)
	result.AutoRenewal = subscriptionAutoRenewal(note.BillingDataMod.AutoRenewal)

	if result.EndDate.IsZero() || !subscriptionAutoRenewal(note.BillingDataMod.AutoRenewal) {
		return result
	}
	months := SubscriptionCycleMonths(note.BillingDataMod.Cycle)
	if months == 0 {
		return result
	}
	for i := 0; SubscriptionRemainingDays(now, result.EndDate) < 0 && i < 2400; i++ {
		result.EndDate = result.EndDate.AddDate(0, months, 0)
	}
	return result
}

func ParseSubscriptionDate(value string) (time.Time, bool) {
	value = strings.TrimSpace(value)
	if value == "" || strings.Contains(value, "0000-00-00") {
		return time.Time{}, false
	}
	for _, layout := range []string{time.RFC3339Nano, "2006-01-02 15:04:05Z07:00"} {
		if parsed, err := time.Parse(layout, value); err == nil {
			return parsed, true
		}
	}
	location := time.FixedZone("UTC+8", 8*60*60)
	for _, layout := range []string{"2006-01-02", "2006-01-02T15:04:05", "2006-01-02 15:04:05"} {
		if parsed, err := time.ParseInLocation(layout, value, location); err == nil {
			return parsed, true
		}
	}
	return time.Time{}, false
}

func SubscriptionRemainingDays(now, end time.Time) int {
	if end.IsZero() {
		return 0
	}
	from := now.In(end.Location())
	fromDate := time.Date(from.Year(), from.Month(), from.Day(), 0, 0, 0, 0, time.UTC)
	toDate := time.Date(end.Year(), end.Month(), end.Day(), 0, 0, 0, 0, time.UTC)
	return int(toDate.Sub(fromDate) / (24 * time.Hour))
}

func RenewSubscriptionEndDate(end time.Time, cycle string, now time.Time) (time.Time, bool) {
	if end.IsZero() || SubscriptionRemainingDays(now, end) >= 0 {
		return end, false
	}
	months := SubscriptionCycleMonths(cycle)
	if months == 0 {
		return end, false
	}
	for i := 0; SubscriptionRemainingDays(now, end) < 0 && i < 2400; i++ {
		end = end.AddDate(0, months, 0)
	}
	if SubscriptionRemainingDays(now, end) < 0 {
		return end, false
	}
	return end, true
}

func rawSubscriptionValue(raw json.RawMessage) string {
	value := strings.TrimSpace(string(raw))
	if value == "" || value == "null" {
		return ""
	}
	if len(value) >= 2 && value[0] == '"' && value[len(value)-1] == '"' {
		var decoded string
		if json.Unmarshal(raw, &decoded) == nil {
			return decoded
		}
	}
	return value
}

func subscriptionAutoRenewal(raw json.RawMessage) bool {
	value := strings.Trim(strings.TrimSpace(string(raw)), `"`)
	return value == "1" || strings.EqualFold(value, "true")
}

// IsLifetimeSubscriptionCycle reports whether the cycle labels the subscription
// as lifetime (永续), meaning no expiration reminder should fire regardless of
// any leftover EndDate value.
func IsLifetimeSubscriptionCycle(value string) bool {
	value = strings.ToLower(strings.TrimSpace(value))
	return value == "永续" || value == "永久" || value == "lifetime"
}

// CanonicalSubscriptionCycle maps a billing cycle onto the subscription form's option values.
func CanonicalSubscriptionCycle(cycle string) string {
	switch SubscriptionCycleMonths(cycle) {
	case 1:
		return "月"
	case 3:
		return "季度"
	case 6:
		return "半年"
	case 12:
		return "年"
	case 24:
		return "两年"
	case 36:
		return "三年"
	case 60:
		return "五年"
	}
	if IsLifetimeSubscriptionCycle(cycle) {
		return "永续"
	}
	return strings.TrimSpace(cycle)
}

func SubscriptionCycleMonths(cycle string) int {
	switch strings.ToLower(strings.TrimSpace(cycle)) {
	case "月", "mo", "month", "monthly", "m":
		return 1
	case "季", "季度", "quarterly", "q":
		return 3
	case "半", "半年", "half", "semi-annually", "h":
		return 6
	case "年", "yr", "year", "annually", "y":
		return 12
	case "两年", "2 years", "2-year", "biennially":
		return 24
	case "三年", "3 years", "3-year", "triennially":
		return 36
	case "五年", "5 years", "5-year":
		return 60
	default:
		return 0
	}
}
