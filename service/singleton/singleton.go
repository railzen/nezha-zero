package singleton

import (
	"log"
	"sync/atomic"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/patrickmn/go-cache"
	"gorm.io/gorm"

	"github.com/railzen/nezha-zero/model"
	"github.com/railzen/nezha-zero/pkg/audit"
	"github.com/railzen/nezha-zero/pkg/utils"
)

var Version = "debug"

const legacyMonitorHistoryIndex = "idx_server_id_created_at_monitor_id_avg_delay"

var (
	Conf        *model.Config
	Cache       *cache.Cache
	DB          *gorm.DB
	Loc         *time.Location
	OnlineUsers = new(atomic.Uint64)
)

func InitTimezoneAndCache() {
	var err error
	Loc, err = time.LoadLocation(Conf.Location)
	if err != nil {
		panic(err)
	}

	Cache = cache.New(5*time.Minute, 10*time.Minute)
}

// LoadSingleton 加载子服务并执行
func LoadSingleton() {
	loadNotifications() // 加载通知服务
	loadServers()       // 加载服务器列表
	loadCronTasks()     // 加载定时任务
	loadExpirationReminders()
	loadCurrencyRates()
	loadAPI()
	RefreshPasskeyCount()
	initNAT()
	initDDNS()
}

// InitConfigFromPath 从给出的文件路径中加载配置
func InitConfigFromPath(path string) {
	Conf = &model.Config{}
	err := Conf.Read(path)
	if err != nil {
		panic(err)
	}
}

// InitDBFromPath 从给出的文件路径中加载数据库
func InitDBFromPath(path string) {
	var err error
	// busy_timeout 让写冲突时自动等待重试
	dsn := path + "?_pragma=busy_timeout(5000)&_pragma=journal_mode(DELETE)"
	DB, err = gorm.Open(sqlite.Open(dsn), &gorm.Config{
		CreateBatchSize: 200,
	})
	if err != nil {
		panic(err)
	}
	if Conf.Debug {
		DB = DB.Debug()
	}
	err = DB.AutoMigrate(model.Server{}, model.User{},
		model.Notification{}, model.AlertRule{}, model.Monitor{},
		model.MonitorHistory{}, model.Cron{}, model.Transfer{},
		model.ApiToken{}, model.NAT{}, model.DDNSProfile{},
		model.AuditLog{}, model.Subscription{}, model.CurrencySnapshot{},
		model.Passkey{})
	if err != nil {
		panic(err)
	}
	if err = migrateAPITokens(DB); err != nil {
		panic(err)
	}
	if err = dropLegacyMonitorHistoryIndex(DB); err != nil {
		panic(err)
	}
	if err = createTransferCycleIndex(DB); err != nil {
		panic(err)
	}
	audit.Init(DB)
}

// CloseDB 关闭底层 SQLite 连接。应在所有依赖 DB 的落盘完成后再调用。
func CloseDB() error {
	if DB == nil {
		return nil
	}
	sqlDB, err := DB.DB()
	if err != nil {
		return err
	}
	audit.Init(nil)
	DB = nil
	return sqlDB.Close()
}

// createTransferCycleIndex 周期流量报警按 datetime(created_at) 过滤 transfers，
// 函数包列导致普通列索引失效，只能靠表达式索引命中，否则每轮检查都是全表扫描。
func createTransferCycleIndex(db *gorm.DB) error {
	return db.Exec(
		"CREATE INDEX IF NOT EXISTS idx_transfers_server_id_datetime_created_at " +
			"ON transfers(server_id, datetime(created_at))",
	).Error
}

func dropLegacyMonitorHistoryIndex(db *gorm.DB) error {
	// AutoMigrate creates the replacement first, so an upgrade never drops the
	// only usable monitor-history composite index before the new one exists.
	if !db.Migrator().HasIndex(&model.MonitorHistory{}, legacyMonitorHistoryIndex) {
		return nil
	}
	return db.Migrator().DropIndex(&model.MonitorHistory{}, legacyMonitorHistoryIndex)
}

// RecordTransferHourlyUsage 对流量记录进行打点
func RecordTransferHourlyUsage() {
	ServerLock.RLock()
	defer ServerLock.RUnlock()
	now := time.Now()
	nowTrimSeconds := time.Date(now.Year(), now.Month(), now.Day(), now.Hour(), 0, 0, 0, now.Location())
	var txs []model.Transfer
	for id, server := range ServerList {
		server.InitRuntimeLock()
		server.RuntimeLock.Lock()
		if server.State == nil {
			server.RuntimeLock.Unlock()
			continue
		}
		tx := model.Transfer{
			ServerID: id,
			In:       utils.SubUintChecked(server.State.NetInTransfer, server.PrevTransferInSnapshot),
			Out:      utils.SubUintChecked(server.State.NetOutTransfer, server.PrevTransferOutSnapshot),
		}
		server.PrevTransferInSnapshot = server.State.NetInTransfer
		server.PrevTransferOutSnapshot = server.State.NetOutTransfer
		server.RuntimeLock.Unlock()
		if tx.In == 0 && tx.Out == 0 {
			continue
		}
		tx.CreatedAt = nowTrimSeconds
		txs = append(txs, tx)
	}
	if len(txs) == 0 {
		return
	}
	log.Println("NEZHA>> Cron 流量统计入库", len(txs), DB.Create(txs).Error)
}

func PersistTransfer(transfer model.Transfer) error {
	if transfer.In == 0 && transfer.Out == 0 {
		return nil
	}
	return DB.Create(&transfer).Error
}

// CleanMonitorHistory 清理无效或过时的 监控记录 和 流量记录
func CleanMonitorHistory() {
	// 清理已被删除的服务器的监控记录与流量记录
	DB.Unscoped().Delete(&model.MonitorHistory{}, "created_at < ? OR monitor_id NOT IN (SELECT `id` FROM monitors)", time.Now().AddDate(0, 0, -30))
	// 由于网络监控记录的数据较多，并且前端仅使用了 1 天的数据
	// 考虑到 sqlite 数据量问题，仅保留一天数据，
	// server_id = 0 的数据会用于/service页面的可用性展示
	DB.Unscoped().Delete(&model.MonitorHistory{}, "(created_at < ? AND server_id != 0) OR monitor_id NOT IN (SELECT `id` FROM monitors)", time.Now().AddDate(0, 0, -1))
	DB.Unscoped().Delete(&model.Transfer{}, "server_id NOT IN (SELECT `id` FROM servers)")
	//清除token过期的用户
	DB.Unscoped().Where("token_expired < ?", time.Now().UTC()).Delete(&model.User{})
	// 计算可清理流量记录的时长
	var allServerKeep time.Time
	specialServerKeep := make(map[uint64]time.Time)
	var specialServerIDs []uint64
	var alerts []model.AlertRule
	if err := DB.Find(&alerts).Error; err != nil {
		log.Printf("NEZHA>> Failed to load alert rules while cleaning transfer history: %v", err)
		return
	}
	for _, alert := range alerts {
		for _, rule := range alert.Rules {
			// 是不是流量记录规则
			if !rule.IsTransferDurationRule() {
				continue
			}
			dataCouldRemoveBefore := rule.GetTransferDurationStart().UTC()
			// 判断规则影响的机器范围
			if rule.Cover == model.RuleCoverAll {
				// 更新全局可以清理的数据点
				if allServerKeep.IsZero() || allServerKeep.After(dataCouldRemoveBefore) {
					allServerKeep = dataCouldRemoveBefore
				}
			} else {
				// 更新特定机器可以清理数据点
				for id := range rule.Ignore {
					if specialServerKeep[id].IsZero() || specialServerKeep[id].After(dataCouldRemoveBefore) {
						specialServerKeep[id] = dataCouldRemoveBefore
						specialServerIDs = append(specialServerIDs, id)
					}
				}
			}
		}
	}
	// 无 CoverAll 周期规则时无全局保留期，避免 transfers 表无界增长
	if allServerKeep.IsZero() {
		allServerKeep = time.Now().AddDate(-1, 0, 0).UTC()
	}
	for id, couldRemove := range specialServerKeep {
		DB.Unscoped().Delete(&model.Transfer{}, "server_id = ? AND datetime(`created_at`) < datetime(?)", id, couldRemove)
	}
	// specialServerIDs 为空时，NOT IN (?) 会被 GORM 展开为 NOT IN (NULL) 导致匹配 0 行，
	// 清理静默失效；此时省略 NOT IN 子句，按全局保留期清理所有服务器。
	if len(specialServerIDs) == 0 {
		DB.Unscoped().Delete(&model.Transfer{}, "datetime(`created_at`) < datetime(?)", allServerKeep)
	} else {
		DB.Unscoped().Delete(&model.Transfer{}, "server_id NOT IN (?) AND datetime(`created_at`) < datetime(?)", specialServerIDs, allServerKeep)
	}
}

// IPDesensitize 根据设置选择是否对IP进行打码处理 返回处理后的IP(关闭打码则返回原IP)
func IPDesensitize(ip string) string {
	if Conf.EnablePlainIPInNotification {
		return ip
	}
	return utils.IPDesensitize(ip)
}
