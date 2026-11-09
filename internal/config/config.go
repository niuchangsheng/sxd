// Package config 提供系统配置读取、YAML 反序列化、环境变量展开与参数校验。
//
// @author Ateng
// @since 2026-10-08
package config

import (
	"fmt"
	"os"
	"regexp"
	"time"

	"gopkg.in/yaml.v3"

	"sxd-pie-ng/internal/protocol"
)

// Config 全局顶层配置结构体
type Config struct {
	Server    ServerConfig    `yaml:"server"`
	Log       LogConfig       `yaml:"log"`
	Accounts  []AccountConfig `yaml:"accounts"`
	Scheduler SchedulerConfig `yaml:"scheduler"`

	// schedulerNode 保留 scheduler 段的原始 YAML 节点，用于按区服做深度合并覆盖。
	// 未经过 Load 手工构造的 Config 该字段为空，EffectiveScheduler 会自动回退。
	schedulerNode *yaml.Node
}

// LogConfig 本地文件日志配置
type LogConfig struct {
	// Enabled 是否启用本地文件日志 (默认 true)
	Enabled *bool `yaml:"enabled"`
	// Dir 日志存放目录 (默认 logs)
	Dir string `yaml:"dir"`
	// File 当前活动日志文件名 (默认 sxd.log)
	File string `yaml:"file"`
	// Level 写入文件的最低日志级别: debug/info/warn/error (默认 info)
	Level string `yaml:"level"`
	// Format 输出格式: text/json (默认 text)
	Format string `yaml:"format"`
	// MaxSizeMB 单文件大小上限 (MB，默认 10)
	MaxSizeMB int `yaml:"max_size_mb"`
	// RotateInterval 时间轮转周期，如 24h/1h；设置为 "0" 可关闭时间轮转 (默认 24h)
	RotateInterval string `yaml:"rotate_interval"`
	// MaxBackups 最多保留的历史日志文件数量 (0 表示不限制，默认 10)
	MaxBackups int `yaml:"max_backups"`
	// MaxAgeDays 历史日志最长保留天数 (0 表示不限制，默认 30)
	MaxAgeDays int `yaml:"max_age_days"`
}

// IsEnabled 报告是否启用本地文件日志。
func (c LogConfig) IsEnabled() bool {
	return c.Enabled == nil || *c.Enabled
}

// DefaultLogConfig 返回本地文件日志的默认配置。
func DefaultLogConfig() LogConfig {
	enabled := true
	return LogConfig{
		Enabled:        &enabled,
		Dir:            "logs",
		File:           "sxd.log",
		Level:          "info",
		Format:         "text",
		MaxSizeMB:      10,
		RotateInterval: "24h",
		MaxBackups:     10,
		MaxAgeDays:     30,
	}
}

// ServerConfig Web 控制台服务配置
type ServerConfig struct {
	Host string `yaml:"host"`
	Port int    `yaml:"port"`
}

// AccountConfig 平台通行证账号配置
type AccountConfig struct {
	Platform string       `yaml:"platform"`
	Username string       `yaml:"username"`
	Password string       `yaml:"password"`
	Cookie   string       `yaml:"cookie,omitempty"`
	Roles    []RoleConfig `yaml:"roles"`
}

// RoleConfig 单个游戏角色配置
type RoleConfig struct {
	ServerID   string `yaml:"server_id"`
	RoleName   string `yaml:"role_name"`
	AutoLogin  bool   `yaml:"auto_login"`
	ServerAddr string `yaml:"server_addr"`
	// 本服主网关认证凭据 (Mod_Player_Base 0x0000)
	Code string `yaml:"code"`
	Time int32  `yaml:"time"`
	Hash string `yaml:"hash"`
	// 跨服网关认证凭据 (Mod_StLogin_Base 0x005E)
	Time1 int32  `yaml:"time1"`
	Hash1 string `yaml:"hash1"`
}

// SchedulerConfig 任务调度器与防封配置
type SchedulerConfig struct {
	Jitter    JitterConfig    `yaml:"jitter"`
	Preset    string          `yaml:"preset"`
	CronTimes []string        `yaml:"cron_times"`
	Routines  map[string]bool `yaml:"routines"`
	Startup   StartupConfig   `yaml:"startup"`
	// ServerOverrides 按区服覆盖全局调度策略。
	// 覆盖采用 YAML 深度合并语义：标量整体替换、映射逐键合并、列表整体替换。
	// 未列出的字段继承全局配置。
	ServerOverrides map[string]*ServerOverrideConfig `yaml:"server_overrides"`

	Dungeon         DungeonConfig          `yaml:"dungeon"`
	SixRealms       SixRealmsConfig        `yaml:"six_realms"`
	Pilgrimage      PilgrimageConfig       `yaml:"pilgrimage"`
	SpiritPool      SpiritPoolConfig       `yaml:"spirit_pool"`
	BeelzebubTrials BeelzebubTrialsConfig  `yaml:"beelzebub_trials"`
	TrainingRoom    TrainingRoomConfig     `yaml:"training_room"`
	JieYuanBox      JieYuanBoxConfig       `yaml:"jieyuan_box"`
	FactionBlessing FactionBlessingConfig  `yaml:"faction_blessing"`
	TreasureHouse   TreasureHouseConfig    `yaml:"treasure_house"`
	LuckyStar       LuckyStarConfig        `yaml:"lucky_star"`
	OnlineGift      OnlineGiftConfig       `yaml:"online_gift"`
	MasterPractice  MasterPracticeConfig   `yaml:"master_practice"`
	Expert          map[string]interface{} `yaml:"expert"`
}

// MasterPracticeConfig 宗师修行策略配置。
//
// 官方服务端要求宗师修行阵容中所有已上阵角色 (含不可下阵的主角) 都与挑战方向同 JobId。
// 主角可通过 Mod_Library_Base.use_stunt_skill (Module 85 Action 3) 切换神器绝技，
// 绝技与职业的映射来自 get_artifact_stunt_info (Module 85 Action 5)；
// 切换成功后服务端会改写主角当前 role_id/职业。PracticeOrder 指定要依次修行的职业顺序：
// 当前方向满星后，自动切到下一个职业的已激活绝技并重整阵容。
type MasterPracticeConfig struct {
	// PracticeOrder 期望修行的职业 JobId 顺序 (如 [15, 3, 1])。
	// 当前职业累计星数满后会切换到下一个职业的神器绝技继续修行。
	// 留空时默认只修行主角当前 JobId 对应的方向。
	PracticeOrder []int32 `yaml:"practice_order"`
	// MaxStarsPerJob 单方向累计星数上限 (默认 30)。
	// 服务端返回 MAX_STARS(10) 时会立即视为满星并切换到下一职业，因此该值只是本地快速判断上限。
	MaxStarsPerJob int32 `yaml:"max_stars_per_job"`
	// MaxChallengesPerDay 每日挑战次数上限 (默认 3，线上实测宗师修行每天 3 次)。
	// 用尽后必须停止挑战；继续发送会被服务端静默丢弃并表现为 context deadline exceeded。
	MaxChallengesPerDay int32 `yaml:"max_challenges_per_day"`
	// AutoNormalizeDeploy 是否自动调整宗师修行阵容：
	// 下阵异职业伙伴、上阵同职业伙伴，使整队满足服务端同职业要求 (默认 true)。
	AutoNormalizeDeploy *bool `yaml:"auto_normalize_deploy"`
	// AllowSwitchStunt 是否允许通过切换主角神器绝技来转换职业 (默认 true)。
	// 切换绝技会改变主角当前 role_id/职业，是官方支持的职业切换途径。
	AllowSwitchStunt *bool `yaml:"allow_switch_stunt"`
	// AllowStepUp 是否在挑战后自动升阶 (默认 true)。
	AllowStepUp *bool `yaml:"allow_step_up"`
	// MaxRetriesPerStar 单个星级挑战失败后的重试次数 (默认 2)。
	MaxRetriesPerStar int `yaml:"max_retries_per_star"`
	// SwitchJobOnFailure 当前职业重试耗尽仍打不过时，是否切换到其他可修行职业 (默认 true)。
	SwitchJobOnFailure *bool `yaml:"switch_job_on_failure"`
}

// StartupConfig 多区服启动编排配置。
type StartupConfig struct {
	// TicketMinIntervalSeconds 相邻两次换票之间的最小间隔 (秒)。
	// 平台 Web 登录对同一账号存在频控，多区服换票必须串行并拉开间隔。
	TicketMinIntervalSeconds *float64 `yaml:"ticket_min_interval_seconds"`
	// TicketMaxIntervalSeconds 相邻两次换票之间的最大间隔 (秒)。
	TicketMaxIntervalSeconds *float64 `yaml:"ticket_max_interval_seconds"`
}

// TicketDelayRange 返回换票间隔的上下限。未配置时默认 2~5 秒；
// 显式配置为 0 表示关闭延迟。
func (c StartupConfig) TicketDelayRange() (time.Duration, time.Duration) {
	minSec, maxSec := 2.0, 5.0
	if c.TicketMinIntervalSeconds != nil {
		minSec = *c.TicketMinIntervalSeconds
	}
	if c.TicketMaxIntervalSeconds != nil {
		maxSec = *c.TicketMaxIntervalSeconds
	}
	if minSec < 0 {
		minSec = 0
	}
	if maxSec < 0 {
		maxSec = 0
	}
	if minSec > maxSec {
		minSec, maxSec = maxSec, minSec
	}
	return time.Duration(minSec * float64(time.Second)), time.Duration(maxSec * float64(time.Second))
}

// ServerOverrideConfig 单个区服的局部策略覆盖。
//
// 该结构在 YAML 中与 scheduler 段同构（但不应再嵌套 server_overrides）：
//
//	server_overrides:
//	  s0:
//	    routines: { treasure_house: true }
//	    treasure_house: { selection_mode: auto }
//
// 为支持任意子项覆盖，这里保留原始 YAML 节点并在 EffectiveScheduler 中做深度合并。
type ServerOverrideConfig struct {
	// Routines 玩法开关覆盖，与全局 routines 逐键合并。
	Routines map[string]bool `yaml:"routines"`

	rawNode *yaml.Node
}

// UnmarshalYAML 捕获原始节点，同时解析 routines 便于配置自检。
func (o *ServerOverrideConfig) UnmarshalYAML(node *yaml.Node) error {
	type plain struct {
		Routines map[string]bool `yaml:"routines"`
	}
	var p plain
	if err := node.Decode(&p); err != nil {
		return err
	}
	o.Routines = p.Routines
	o.rawNode = cloneYAMLNode(node)
	return nil
}

// SixRealmsConfig 六道轮回挑战与灵件配置
type SixRealmsConfig struct {
	AutoReset       bool `yaml:"auto_reset"`        // 当前道通关后是否自动消耗免费次数重置 (默认 true)
	AutoSellBlue    bool `yaml:"auto_sell_blue"`    // 挑战获胜时是否自动卖出蓝色灵件 (默认 true)
	AutoSellPurple  bool `yaml:"auto_sell_purple"`  // 挑战获胜时是否自动卖出紫色灵件 (默认 false)
	MaxFloorRetries int  `yaml:"max_floor_retries"` // 每层挑战失败最大重试次数 (每层单独计算，失败达到上限后退出)
}

// BeelzebubTrialsConfig 魔王试炼自动化策略配置
type BeelzebubTrialsConfig struct {
	// PreferBossID 优先挑战的魔王编号 (0 表示自动选择最高难度魔王)
	PreferBossID int32 `yaml:"prefer_boss_id"`
	// AutoMoralAward 是否自动领取道行等级宝箱奖励 (默认 true)
	AutoMoralAward bool `yaml:"auto_moral_award"`
	// MinLevel 开启等级门槛 (默认 70 级开放魔王试炼)
	MinLevel int `yaml:"min_level"`
}

// TrainingRoomConfig 仙界练功房挂机收益维护策略配置
type TrainingRoomConfig struct {
	// AllowRobSeat 是否允许通过战斗抢夺已满员的高星席位 (默认 false，仅使用空闲席位)
	AllowRobSeat bool `yaml:"allow_rob_seat"`
	// MaxRobAttempts 单轮巡检最多发起的席位争夺次数 (默认 1)
	MaxRobAttempts int `yaml:"max_rob_attempts"`
}

// JieYuanBoxConfig 结缘宝箱配置
type JieYuanBoxConfig struct {
	// MaxBatchSize 每批最多开启数量 (每批最多10个，默认 10)
	MaxBatchSize int `yaml:"max_batch_size"`
}

// FactionBlessingConfig 帮派祭神上香配置
type FactionBlessingConfig struct {
	// OfferingID 上香供品编号 (1 白檀香免费 / 2 苏合香30元宝 / 3 天木香90元宝)
	OfferingID int32 `yaml:"offering_id"`
	// AllowCover 剩余祝福次数高于本次上香收益时是否仍允许覆盖
	AllowCover bool `yaml:"allow_cover"`
}

// LuckyStarConfig 个人吉星高照自动化策略配置
type LuckyStarConfig struct {
	// MaxRerollsPerRoll 每次投掷最多逆天改运次数 (默认 9)
	MaxRerollsPerRoll int `yaml:"max_rerolls_per_roll"`
}

// OnlineGiftConfig 在线礼包与功能终结礼包策略配置
type OnlineGiftConfig struct {
	// ClaimFunctionEndGifts 是否领取功能终结礼包 (默认 true)。
	// 功能终结礼包指吉星高照、喜从天降 (龙鱼仙令/境界点)、灵脉、拜关公、聚灵等
	// 玩法达到等级上限终结后转为每日领取的补偿礼包。
	ClaimFunctionEndGifts *bool `yaml:"claim_function_end_gifts"`
	// MaxClaimsPerGift 单个终结礼包连续领取次数上限 (默认 20)。
	MaxClaimsPerGift int `yaml:"max_claims_per_gift"`
}

// PilgrimageConfig 西天取经护送与打劫策略配置。
type PilgrimageConfig struct {
	// AutoRob 是否在取经巡检后自动打劫其他玩家队伍 (默认 true)
	AutoRob *bool `yaml:"auto_rob"`
	// MaxRobAttempts 单轮最多尝试打劫次数 (1-5，默认 5)
	MaxRobAttempts int `yaml:"max_rob_attempts"`
	// MaxProbeTargets 单轮最多探测的目标队伍数量 (1-50，默认 8)
	MaxProbeTargets int `yaml:"max_probe_targets"`
	// ForceSaintArea 圣域取经回退策略 (默认不配置，即自动判定):
	//   - 不配置: 自动。圣域功能已开放的角色只走圣域，圣域关闭/维护时跳过本轮；
	//     圣域功能未开放的角色回退仙界跨服/主服取经。
	//   - true: 固定圣域，圣域不可用时跳过本轮，绝不回退。
	//   - false: 总是允许回退仙界跨服/主服取经。
	ForceSaintArea *bool `yaml:"force_saint_area"`
}

// SpiritPoolConfig 粗铁淬炼池策略配置。
type SpiritPoolConfig struct {
	// RefineTarget 一键操作产出目标: xuantie=玄铁 (默认) / lingcai=灵材。
	// 两种目标均固定使用铜钱消耗, 永不消耗元宝。
	RefineTarget string `yaml:"refine_target"`
}

// TreasureAwardConfig 天帝宝库自选大奖条目
type TreasureAwardConfig struct {
	Tier   int32 `yaml:"tier"`
	ItemID int32 `yaml:"item_id"`
}

// TreasureHouseConfig 天帝宝库配置
type TreasureHouseConfig struct {
	// SelectionMode 非交互模式选奖策略:
	//   auto     - 采集账号养成进度 (法阵/龙珠/血脉/藏经阁/丹魂/圣器/兵法/混沌/幻兽) 动态计算需求分
	//   rarity   - 按道具稀有度/价值序 (默认)
	//   explicit - 使用下方指定奖励
	SelectionMode string `yaml:"selection_mode"`
	// PreferAwards 优先奖励条目 ID 列表 (按顺序匹配当前开放候选，命中即选)
	PreferAwards []int32 `yaml:"prefer_awards"`
	// Interactive 是否开启终端交互式输入自选大奖
	Interactive bool `yaml:"interactive"`
	// Awards 三档自选大奖列表
	Awards []TreasureAwardConfig `yaml:"awards"`
	// AwardIDs 简易道具ID列表，如 [50, 25, 42] 对应一、二、三档
	AwardIDs []int32 `yaml:"award_ids"`
	// 分档直接指定大奖道具ID
	Tier1ItemID int32 `yaml:"tier1_item_id"`
	Tier2ItemID int32 `yaml:"tier2_item_id"`
	Tier3ItemID int32 `yaml:"tier3_item_id"`
	// MaxDrawTimes 单次运行最大抽奖次数 (默认 10)
	MaxDrawTimes int `yaml:"max_draw_times"`
}

// ResolveSelections 解析为三档自选大奖模型
func (c TreasureHouseConfig) ResolveSelections() []protocol.TreasureAwardSelect {
	defaults := []protocol.TreasureAwardSelect{
		{TierID: 1, ItemID: 50},
		{TierID: 2, ItemID: 25},
		{TierID: 3, ItemID: 42},
	}
	if len(c.Awards) >= 3 {
		return []protocol.TreasureAwardSelect{
			{TierID: 1, ItemID: c.Awards[0].ItemID},
			{TierID: 2, ItemID: c.Awards[1].ItemID},
			{TierID: 3, ItemID: c.Awards[2].ItemID},
		}
	}
	if len(c.AwardIDs) >= 3 {
		return []protocol.TreasureAwardSelect{
			{TierID: 1, ItemID: c.AwardIDs[0]},
			{TierID: 2, ItemID: c.AwardIDs[1]},
			{TierID: 3, ItemID: c.AwardIDs[2]},
		}
	}
	if c.Tier1ItemID > 0 || c.Tier2ItemID > 0 || c.Tier3ItemID > 0 {
		t1 := c.Tier1ItemID
		if t1 <= 0 {
			t1 = 50
		}
		t2 := c.Tier2ItemID
		if t2 <= 0 {
			t2 = 25
		}
		t3 := c.Tier3ItemID
		if t3 <= 0 {
			t3 = 42
		}
		return []protocol.TreasureAwardSelect{
			{TierID: 1, ItemID: t1},
			{TierID: 2, ItemID: t2},
			{TierID: 3, ItemID: t3},
		}
	}
	return defaults
}

// QuestCycleConfig 剧情推图任务闭环配置
type QuestCycleConfig struct {
	Enabled              bool `yaml:"enabled"`
	AutoReplenishStamina bool `yaml:"auto_replenish_stamina"` // 自动吃药补充体力 (仅作用于推图)
	MaxReplenishStamina  int  `yaml:"max_replenish_stamina"`  // 单次推图最大补充体力上限 (点数，仅作用于推图；包子5/粽子10/茶叶蛋50)
	MaxReplenishTimes    int  `yaml:"max_replenish_times"`    // 兼容旧配置：单次推图最大自愈补充次数 (仅作用于推图)
}

// DungeonSweepFeatureConfig 副本关卡扫荡配置
type DungeonSweepFeatureConfig struct {
	Enabled               bool     `yaml:"enabled"`
	Pipeline              []string `yaml:"pipeline"`                // 扫荡优先级流水线: "quest_cycle" / "sweep_elite"
	PreferPracticeQuickly bool     `yaml:"prefer_practice_quickly"` // 优先使用英雄副本秒完成协议
	SellSweptItems        bool     `yaml:"sell_swept_items"`        // 扫荡精英副本后是否自动卖掉扫荡物
	AutoSellSweptItems    bool     `yaml:"auto_sell_swept_items"`   // 扫荡精英副本后是否自动卖掉扫荡物 (兼容别名)
	GlobalElite           bool     `yaml:"global_elite"`            // 全局扫荡所有精英副本 (无需进城)
}

// DungeonConfig 副本与推图自动化策略配置
type DungeonConfig struct {
	// 功能一: 剧情推图 (循环接任务/做任务/交任务直至无任务可做)
	QuestCycle QuestCycleConfig `yaml:"quest_cycle"`
	// 功能二: 关卡扫荡 (扫荡精英副本)
	Sweep DungeonSweepFeatureConfig `yaml:"sweep"`

	// 向下兼容既有顶层扁平配置字段
	Pipeline              []string `yaml:"pipeline"`
	PreferPracticeQuickly bool     `yaml:"prefer_practice_quickly"`
	AutoReplenishStamina  bool     `yaml:"auto_replenish_stamina"`
	MaxReplenishStamina   int      `yaml:"max_replenish_stamina"`
	MaxReplenishTimes     int      `yaml:"max_replenish_times"`
	SellSweptItems        bool     `yaml:"sell_swept_items"`
	AutoSellSweptItems    bool     `yaml:"auto_sell_swept_items"`
}

// JitterConfig 抖动上下限配置 (秒)
type JitterConfig struct {
	MinSeconds float64 `yaml:"min_seconds"`
	MaxSeconds float64 `yaml:"max_seconds"`
}

var envVarRegex = regexp.MustCompile(`\$\{([A-Za-z0-9_]+)(:-([^}]*))?\}`)

func expandEnv(content string) string {
	return envVarRegex.ReplaceAllStringFunc(content, func(match string) string {
		submatches := envVarRegex.FindStringSubmatch(match)
		if len(submatches) >= 2 {
			varName := submatches[1]
			val := os.Getenv(varName)
			if val != "" {
				return val
			}
			if len(submatches) >= 4 && submatches[2] != "" {
				return submatches[3]
			}
		}
		return match
	})
}

// Load 从指定路径加载并解析 YAML 配置文件，自动展开环境变量并填充默认值。
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("config: failed to read file: %w", err)
	}

	expanded := expandEnv(string(data))

	// 预先填充日志默认值，使配置文件中缺省字段保持默认、显式零值仍可生效。
	cfg := Config{Log: DefaultLogConfig()}
	if err := yaml.Unmarshal([]byte(expanded), &cfg); err != nil {
		return nil, fmt.Errorf("config: failed to parse YAML: %w", err)
	}

	// 保留 scheduler 原始节点：区服覆盖必须基于“配置原文”合并后再套默认值，
	// 否则默认值会掩盖按区服显式设置的零值 (例如 max_* 计算字段)。
	var rawDoc struct {
		Scheduler yaml.Node `yaml:"scheduler"`
	}
	if err := yaml.Unmarshal([]byte(expanded), &rawDoc); err != nil {
		return nil, fmt.Errorf("config: failed to parse scheduler node: %w", err)
	}
	if rawDoc.Scheduler.Kind != 0 {
		cfg.schedulerNode = cloneYAMLNode(&rawDoc.Scheduler)
	}

	// 补齐并校验默认值
	if cfg.Server.Host == "" {
		cfg.Server.Host = "127.0.0.1"
	}
	if cfg.Server.Port <= 0 {
		cfg.Server.Port = 8080
	}

	applySchedulerDefaults(&cfg.Scheduler)

	// 预校验所有区服覆盖，避免运行期才发现覆盖节点非法。
	for serverID := range cfg.Scheduler.ServerOverrides {
		if _, err := cfg.EffectiveScheduler(serverID); err != nil {
			return nil, fmt.Errorf("config: invalid server_overrides[%s]: %w", serverID, err)
		}
	}

	if cfg.Accounts == nil {
		cfg.Accounts = []AccountConfig{}
	}

	return &cfg, nil
}

// applySchedulerDefaults 为调度配置补齐默认值。
// Load 与 EffectiveScheduler 都会调用，因此必须保持幂等。
func applySchedulerDefaults(sc *SchedulerConfig) {
	if sc.Jitter.MinSeconds <= 0 {
		sc.Jitter.MinSeconds = 1.0
	}
	if sc.Jitter.MaxSeconds <= 0 {
		sc.Jitter.MaxSeconds = 3.0
	}
	if sc.Jitter.MinSeconds > sc.Jitter.MaxSeconds {
		sc.Jitter.MinSeconds, sc.Jitter.MaxSeconds =
			sc.Jitter.MaxSeconds, sc.Jitter.MinSeconds
	}

	if sc.Routines == nil {
		sc.Routines = make(map[string]bool)
	}

	if sc.Preset == "" {
		sc.Preset = "recommended"
	}

	if len(sc.CronTimes) == 0 {
		sc.CronTimes = []string{
			"00:00", "06:02", "08:02", "12:00", "16:05", "18:01", "20:01", "22:01",
		}
	}

	if sc.Expert == nil {
		sc.Expert = make(map[string]interface{})
	}

	hasSubDungeon := sc.Dungeon.QuestCycle.Enabled ||
		sc.Dungeon.QuestCycle.MaxReplenishStamina > 0 ||
		sc.Dungeon.QuestCycle.MaxReplenishTimes > 0 ||
		sc.Dungeon.Sweep.Enabled ||
		sc.Dungeon.Sweep.SellSweptItems ||
		sc.Dungeon.Sweep.AutoSellSweptItems ||
		sc.Dungeon.SellSweptItems ||
		sc.Dungeon.AutoSellSweptItems ||
		len(sc.Dungeon.Sweep.Pipeline) > 0
	if len(sc.Dungeon.Pipeline) == 0 && !hasSubDungeon {
		sc.Dungeon.Pipeline = []string{
			"push_mainline",
			"push_side",
			"sweep_elite",
		}
	}
	if sc.Dungeon.SellSweptItems || sc.Dungeon.AutoSellSweptItems {
		sc.Dungeon.Sweep.SellSweptItems = true
	}
	// 默认启用秒完成
	if !sc.Dungeon.PreferPracticeQuickly {
		sc.Dungeon.PreferPracticeQuickly = true
	}
	if sc.Dungeon.MaxReplenishStamina <= 0 {
		if sc.Dungeon.MaxReplenishTimes > 0 {
			sc.Dungeon.MaxReplenishStamina = sc.Dungeon.MaxReplenishTimes * 10
		} else {
			sc.Dungeon.MaxReplenishStamina = 200
		}
	}
	if sc.Dungeon.QuestCycle.MaxReplenishStamina <= 0 {
		if sc.Dungeon.QuestCycle.MaxReplenishTimes > 0 {
			sc.Dungeon.QuestCycle.MaxReplenishStamina = sc.Dungeon.QuestCycle.MaxReplenishTimes * 10
		} else {
			sc.Dungeon.QuestCycle.MaxReplenishStamina = 200
		}
	}
	if sc.Dungeon.MaxReplenishTimes <= 0 {
		sc.Dungeon.MaxReplenishTimes = 10
	}
	// AutoReplenishStamina 默认为 true
	if !sc.Dungeon.AutoReplenishStamina {
		sc.Dungeon.AutoReplenishStamina = true
	}

	// Pilgrimage 默认开启自动打劫；仅使用免费次数，不会消耗元宝。
	if sc.Pilgrimage.AutoRob == nil {
		autoRob := true
		sc.Pilgrimage.AutoRob = &autoRob
	}
	if sc.Pilgrimage.MaxRobAttempts <= 0 {
		sc.Pilgrimage.MaxRobAttempts = 5
	}
	if sc.Pilgrimage.MaxRobAttempts > 5 {
		sc.Pilgrimage.MaxRobAttempts = 5
	}
	if sc.Pilgrimage.MaxProbeTargets <= 0 {
		sc.Pilgrimage.MaxProbeTargets = 8
	}
	if sc.Pilgrimage.MaxProbeTargets > 50 {
		sc.Pilgrimage.MaxProbeTargets = 50
	}

	// 粗铁淬炼池默认淬炼玄铁; 可显式指定 lingcai 提炼灵材。
	if sc.SpiritPool.RefineTarget == "" {
		sc.SpiritPool.RefineTarget = "xuantie"
	}

	// BeelzebubTrials 默认等级门槛 0 (由服务端资格检查自动决定)
	if sc.BeelzebubTrials.MinLevel < 0 {
		sc.BeelzebubTrials.MinLevel = 0
	}

	// 宗师修行: 单方向星数上限 30；每天最多挑战 3 次；默认自动整理阵容并允许升阶。
	if sc.MasterPractice.MaxStarsPerJob <= 0 {
		sc.MasterPractice.MaxStarsPerJob = 30
	}
	if sc.MasterPractice.MaxChallengesPerDay <= 0 {
		sc.MasterPractice.MaxChallengesPerDay = 3
	}
	if sc.MasterPractice.AutoNormalizeDeploy == nil {
		autoNormalize := true
		sc.MasterPractice.AutoNormalizeDeploy = &autoNormalize
	}
	if sc.MasterPractice.AllowSwitchStunt == nil {
		allowSwitchStunt := true
		sc.MasterPractice.AllowSwitchStunt = &allowSwitchStunt
	}
	if sc.MasterPractice.AllowStepUp == nil {
		allowStepUp := true
		sc.MasterPractice.AllowStepUp = &allowStepUp
	}
	if sc.MasterPractice.MaxRetriesPerStar <= 0 {
		sc.MasterPractice.MaxRetriesPerStar = 2
	}
	if sc.MasterPractice.SwitchJobOnFailure == nil {
		switchJob := true
		sc.MasterPractice.SwitchJobOnFailure = &switchJob
	}

	// 多区服换票默认串行间隔 2~5 秒；显式设置为 0 可关闭。
	if sc.Startup.TicketMinIntervalSeconds == nil && sc.Startup.TicketMaxIntervalSeconds == nil {
		minSec, maxSec := 2.0, 5.0
		sc.Startup.TicketMinIntervalSeconds = &minSec
		sc.Startup.TicketMaxIntervalSeconds = &maxSec
	} else if sc.Startup.TicketMinIntervalSeconds == nil {
		v := *sc.Startup.TicketMaxIntervalSeconds
		sc.Startup.TicketMinIntervalSeconds = &v
	} else if sc.Startup.TicketMaxIntervalSeconds == nil {
		v := *sc.Startup.TicketMinIntervalSeconds
		sc.Startup.TicketMaxIntervalSeconds = &v
	}
	if *sc.Startup.TicketMinIntervalSeconds < 0 {
		*sc.Startup.TicketMinIntervalSeconds = 0
	}
	if *sc.Startup.TicketMaxIntervalSeconds < 0 {
		*sc.Startup.TicketMaxIntervalSeconds = 0
	}
	if *sc.Startup.TicketMinIntervalSeconds > *sc.Startup.TicketMaxIntervalSeconds {
		*sc.Startup.TicketMinIntervalSeconds, *sc.Startup.TicketMaxIntervalSeconds =
			*sc.Startup.TicketMaxIntervalSeconds, *sc.Startup.TicketMinIntervalSeconds
	}
}
