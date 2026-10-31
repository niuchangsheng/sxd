// Package config 提供系统配置读取、YAML 反序列化、环境变量展开与参数校验。
//
// @author Ateng
// @since 2026-10-08
package config

import (
	"fmt"
	"os"
	"regexp"

	"gopkg.in/yaml.v3"

	"sxd-pie-ng/internal/protocol"
)

// Config 全局顶层配置结构体
type Config struct {
	Server    ServerConfig    `yaml:"server"`
	Accounts  []AccountConfig `yaml:"accounts"`
	Scheduler SchedulerConfig `yaml:"scheduler"`
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
	Jitter          JitterConfig           `yaml:"jitter"`
	Preset          string                 `yaml:"preset"`
	CronTimes       []string               `yaml:"cron_times"`
	Routines        map[string]bool        `yaml:"routines"`
	Dungeon         DungeonConfig          `yaml:"dungeon"`
	SixRealms       SixRealmsConfig        `yaml:"six_realms"`
	BeelzebubTrials BeelzebubTrialsConfig  `yaml:"beelzebub_trials"`
	JieYuanBox      JieYuanBoxConfig       `yaml:"jieyuan_box"`
	TreasureHouse   TreasureHouseConfig    `yaml:"treasure_house"`
	Expert          map[string]interface{} `yaml:"expert"`
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

// JieYuanBoxConfig 结缘宝箱配置
type JieYuanBoxConfig struct {
	// MaxBatchSize 每批最多开启数量 (每批最多10个，默认 10)
	MaxBatchSize int `yaml:"max_batch_size"`
}

// TreasureAwardConfig 天帝宝库自选大奖条目
type TreasureAwardConfig struct {
	Tier   int32 `yaml:"tier"`
	ItemID int32 `yaml:"item_id"`
}

// TreasureHouseConfig 天帝宝库配置
type TreasureHouseConfig struct {
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
	Pipeline              []string `yaml:"pipeline"`                // 扫荡优先级流水线: "sweep_elite"
	MaxBatchTimes         int      `yaml:"max_batch_times"`         // 扫荡批次单次最大执行次数 (仅作用于扫荡)
	PreferPracticeQuickly bool     `yaml:"prefer_practice_quickly"` // 优先使用 Module 25 秒完成协议 (仅作用于扫荡)
	SellSweptItems        bool     `yaml:"sell_swept_items"`        // 扫荡精英副本后是否自动卖掉扫荡物
	AutoSellSweptItems    bool     `yaml:"auto_sell_swept_items"`   // 扫荡精英副本后是否自动卖掉扫荡物 (兼容别名)
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
	MaxBatchTimes         int      `yaml:"max_batch_times"`
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

	var cfg Config
	if err := yaml.Unmarshal([]byte(expanded), &cfg); err != nil {
		return nil, fmt.Errorf("config: failed to parse YAML: %w", err)
	}

	// 补齐并校验默认值
	if cfg.Server.Host == "" {
		cfg.Server.Host = "127.0.0.1"
	}
	if cfg.Server.Port <= 0 {
		cfg.Server.Port = 8080
	}

	if cfg.Scheduler.Jitter.MinSeconds <= 0 {
		cfg.Scheduler.Jitter.MinSeconds = 1.0
	}
	if cfg.Scheduler.Jitter.MaxSeconds <= 0 {
		cfg.Scheduler.Jitter.MaxSeconds = 3.0
	}
	if cfg.Scheduler.Jitter.MinSeconds > cfg.Scheduler.Jitter.MaxSeconds {
		cfg.Scheduler.Jitter.MinSeconds, cfg.Scheduler.Jitter.MaxSeconds =
			cfg.Scheduler.Jitter.MaxSeconds, cfg.Scheduler.Jitter.MinSeconds
	}

	if cfg.Scheduler.Routines == nil {
		cfg.Scheduler.Routines = make(map[string]bool)
	}

	if cfg.Scheduler.Preset == "" {
		cfg.Scheduler.Preset = "recommended"
	}

	if len(cfg.Scheduler.CronTimes) == 0 {
		cfg.Scheduler.CronTimes = []string{
			"00:00", "06:02", "08:02", "12:00", "16:05", "18:01", "20:01", "22:01",
		}
	}

	if cfg.Scheduler.Expert == nil {
		cfg.Scheduler.Expert = make(map[string]interface{})
	}

	hasSubDungeon := cfg.Scheduler.Dungeon.QuestCycle.Enabled ||
		cfg.Scheduler.Dungeon.QuestCycle.MaxReplenishStamina > 0 ||
		cfg.Scheduler.Dungeon.QuestCycle.MaxReplenishTimes > 0 ||
		cfg.Scheduler.Dungeon.Sweep.Enabled ||
		cfg.Scheduler.Dungeon.Sweep.MaxBatchTimes > 0 ||
		cfg.Scheduler.Dungeon.Sweep.SellSweptItems ||
		cfg.Scheduler.Dungeon.Sweep.AutoSellSweptItems ||
		cfg.Scheduler.Dungeon.SellSweptItems ||
		cfg.Scheduler.Dungeon.AutoSellSweptItems ||
		len(cfg.Scheduler.Dungeon.Sweep.Pipeline) > 0
	if len(cfg.Scheduler.Dungeon.Pipeline) == 0 && !hasSubDungeon {
		cfg.Scheduler.Dungeon.Pipeline = []string{
			"push_mainline",
			"push_side",
			"sweep_elite",
		}
	}
	if cfg.Scheduler.Dungeon.SellSweptItems || cfg.Scheduler.Dungeon.AutoSellSweptItems {
		cfg.Scheduler.Dungeon.Sweep.SellSweptItems = true
	}
	// 默认启用秒完成
	if !cfg.Scheduler.Dungeon.PreferPracticeQuickly {
		cfg.Scheduler.Dungeon.PreferPracticeQuickly = true
	}
	if cfg.Scheduler.Dungeon.MaxBatchTimes <= 0 {
		cfg.Scheduler.Dungeon.MaxBatchTimes = 10
	}
	if cfg.Scheduler.Dungeon.MaxReplenishStamina <= 0 {
		if cfg.Scheduler.Dungeon.MaxReplenishTimes > 0 {
			cfg.Scheduler.Dungeon.MaxReplenishStamina = cfg.Scheduler.Dungeon.MaxReplenishTimes * 10
		} else {
			cfg.Scheduler.Dungeon.MaxReplenishStamina = 200
		}
	}
	if cfg.Scheduler.Dungeon.QuestCycle.MaxReplenishStamina <= 0 {
		if cfg.Scheduler.Dungeon.QuestCycle.MaxReplenishTimes > 0 {
			cfg.Scheduler.Dungeon.QuestCycle.MaxReplenishStamina = cfg.Scheduler.Dungeon.QuestCycle.MaxReplenishTimes * 10
		} else {
			cfg.Scheduler.Dungeon.QuestCycle.MaxReplenishStamina = 200
		}
	}
	if cfg.Scheduler.Dungeon.MaxReplenishTimes <= 0 {
		cfg.Scheduler.Dungeon.MaxReplenishTimes = 10
	}
	// AutoReplenishStamina 默认为 true
	if !cfg.Scheduler.Dungeon.AutoReplenishStamina {
		cfg.Scheduler.Dungeon.AutoReplenishStamina = true
	}

	// BeelzebubTrials 默认等级门槛 0 (由服务端资格检查自动决定)
	if cfg.Scheduler.BeelzebubTrials.MinLevel < 0 {
		cfg.Scheduler.BeelzebubTrials.MinLevel = 0
	}

	if cfg.Accounts == nil {
		cfg.Accounts = []AccountConfig{}
	}

	return &cfg, nil
}
