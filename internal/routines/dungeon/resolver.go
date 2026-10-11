// Package dungeon 提供副本关卡扫荡、关卡智能寻址引擎与双模配置策略。
//
// @author Ateng
// @since 2026-10-11
package dungeon

import (
	"errors"
	"fmt"

	"sxd-pie-ng/internal/dictionary"
)

var (
	// ErrInvalidMission 表示指定的关卡 ID 非法或不在数据字典中。
	ErrInvalidMission = errors.New("dungeon: invalid mission id")
	// ErrInsufficientStamina 表示体力不足以支撑一次扫荡。
	ErrInsufficientStamina = errors.New("dungeon: insufficient stamina for sweep")
	// ErrLevelTooLow 表示角色等级低于该关卡解锁限制。
	ErrLevelTooLow = errors.New("dungeon: player level too low for mission")
)

// SweepMode 扫荡配置模式枚举。
type SweepMode string

const (
	// ModeAutoRecommend 开箱即用模式：自动根据角色等级和元数据字典推荐最高收益关卡。
	ModeAutoRecommend SweepMode = "auto_recommend"
	// ModeExpertOverride 专家模式：显式指定目标关卡 ID 与自定义轮次策略。
	ModeExpertOverride SweepMode = "expert_override"
)

// DungeonSweepConfig 代表双模关卡扫荡策略配置。
type DungeonSweepConfig struct {
	// Mode 扫荡模式 (ModeAutoRecommend 或 ModeExpertOverride)
	Mode SweepMode `json:"mode"`
	// TargetMissionID 专家模式下指定的关卡 ID (当 Mode=ModeExpertOverride 时生效)
	TargetMissionID int `json:"target_mission_id"`
	// SweepElite 是否扫荡精英副本关卡
	SweepElite bool `json:"sweep_elite"`
	// MaxBatchTimes 单次执行批处理最大扫荡次数 (默认 10，上限 30)
	MaxBatchTimes int `json:"max_batch_times"`
	// RequiredPowerPerSweep 单次扫荡体力消耗基线 (默认 5 点)
	RequiredPowerPerSweep int `json:"required_power_per_sweep"`
	// PreferPracticeQuickly 是否优先使用 Module 25 秒扫荡协议
	PreferPracticeQuickly bool `json:"prefer_practice_quickly"`
}

// DefaultDungeonSweepConfig 返回开箱即用的默认推荐配置。
func DefaultDungeonSweepConfig() DungeonSweepConfig {
	return DungeonSweepConfig{
		Mode:                  ModeAutoRecommend,
		TargetMissionID:       0,
		SweepElite:            false,
		MaxBatchTimes:         10,
		RequiredPowerPerSweep: 5,
		PreferPracticeQuickly: false,
	}
}

// ExpertDungeonSweepConfig 返回专家指定关卡的覆盖配置。
func ExpertDungeonSweepConfig(missionID int, maxBatchTimes int) DungeonSweepConfig {
	if maxBatchTimes <= 0 {
		maxBatchTimes = 10
	}
	return DungeonSweepConfig{
		Mode:                  ModeExpertOverride,
		TargetMissionID:       missionID,
		SweepElite:            false,
		MaxBatchTimes:         maxBatchTimes,
		RequiredPowerPerSweep: 5,
	}
}

// MissionResolutionResult 代表关卡智能寻址引擎的解析结果。
type MissionResolutionResult struct {
	// MissionID 最终决定的目标关卡数字标识
	MissionID int
	// MissionName 目标关卡友好名称
	MissionName string
	// SectionID 关卡所属章节/地图段标识
	SectionID int
	// PowerCost 单次扫荡体力消耗 (通常为 5)
	PowerCost int
	// Mode 命中的配置模式
	Mode SweepMode
	// IsElite 是否为精英关卡
	IsElite bool
}

// MissionResolver 关卡智能寻址引擎。
type MissionResolver struct {
	dictRepo dictionary.Repository
}

// NewMissionResolver 构造关卡智能寻址引擎实例。
func NewMissionResolver(dictRepo dictionary.Repository) *MissionResolver {
	return &MissionResolver{dictRepo: dictRepo}
}

// Resolve 结合角色当前等级、当前体力与配置策略，确定最终扫荡关卡与批次。
func (r *MissionResolver) Resolve(cfg DungeonSweepConfig, playerLevel int, currentStamina int) (*MissionResolutionResult, error) {
	powerCost := cfg.RequiredPowerPerSweep
	if powerCost <= 0 {
		powerCost = 5
	}

	// 1. 基础体力卫语句判定
	if currentStamina < powerCost {
		return nil, fmt.Errorf("%w: current %d, required %d", ErrInsufficientStamina, currentStamina, powerCost)
	}

	// 2. 专家模式优先：显式指定 TargetMissionID
	if cfg.Mode == ModeExpertOverride && cfg.TargetMissionID > 0 {
		if r.dictRepo == nil {
			return &MissionResolutionResult{
				MissionID:   cfg.TargetMissionID,
				MissionName: fmt.Sprintf("专家指定关卡-%d", cfg.TargetMissionID),
				PowerCost:   powerCost,
				Mode:        ModeExpertOverride,
				IsElite:     cfg.SweepElite,
			}, nil
		}

		m, err := r.dictRepo.GetMission(cfg.TargetMissionID)
		if err != nil {
			return nil, fmt.Errorf("%w: mission_id %d not found in dictionary: %v", ErrInvalidMission, cfg.TargetMissionID, err)
		}

		// 等级与锁校验：若关卡需要的 Lock 显著高于角色当前等级，则拦截非法越级挑战
		if playerLevel > 0 {
			maxAllowedLock := playerLevel * 1000
			if playerLevel <= 1 {
				maxAllowedLock = 1020
			}
			if m.MissionLock > maxAllowedLock {
				return nil, fmt.Errorf("%w: player level %d (max lock %d) cannot challenge mission %d (lock %d)",
					ErrLevelTooLow, playerLevel, maxAllowedLock, m.ID, m.MissionLock)
			}
		}

		return &MissionResolutionResult{
			MissionID:   m.ID,
			MissionName: m.Name,
			SectionID:   m.SectionID,
			PowerCost:   powerCost,
			Mode:        ModeExpertOverride,
			IsElite:     m.Type == 1,
		}, nil
	}

	// 3. 自动推荐模式：结合数据字典与角色等级推荐最高收益关卡
	if r.dictRepo != nil {
		lvl := playerLevel
		if lvl <= 0 {
			lvl = 1
		}

		m, err := r.dictRepo.GetHighestMissionByLevel(lvl, cfg.SweepElite)
		if err == nil && m != nil {
			return &MissionResolutionResult{
				MissionID:   m.ID,
				MissionName: m.Name,
				SectionID:   m.SectionID,
				PowerCost:   powerCost,
				Mode:        ModeAutoRecommend,
				IsElite:     m.Type == 1,
			}, nil
		}

		// 降级兜底：全局最高
		highest, hErr := r.dictRepo.GetHighestMission(cfg.SweepElite)
		if hErr == nil && highest != nil {
			return &MissionResolutionResult{
				MissionID:   highest.ID,
				MissionName: highest.Name,
				SectionID:   highest.SectionID,
				PowerCost:   powerCost,
				Mode:        ModeAutoRecommend,
				IsElite:     highest.Type == 1,
			}, nil
		}
	}

	// 4. 无字典时的缺省安全关卡 (扬州城-万妖皇 / 沉没之船)
	fallbackID := 43
	if playerLevel >= 60 {
		fallbackID = 854 // 幽冥地府 / 抓包实机关卡
	}
	return &MissionResolutionResult{
		MissionID:   fallbackID,
		MissionName: "默认推荐关卡",
		SectionID:   1,
		PowerCost:   powerCost,
		Mode:        ModeAutoRecommend,
		IsElite:     cfg.SweepElite,
	}, nil
}
