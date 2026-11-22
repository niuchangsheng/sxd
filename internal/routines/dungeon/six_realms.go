// Package dungeon 提供副本推图、主线任务循环、精英扫荡、六道轮回、伏魔塔等关卡挑战自动化。
//
// 本文件实现六道轮回塔 (Mod_Tower_Base, Module 32) 自动爬塔与扫荡。
package dungeon

import (
	"context"
	"log/slog"
	"time"

	"sxd-pie-ng/internal/client"
	"sxd-pie-ng/internal/protocol"
	"sxd-pie-ng/internal/routines/kit"
	"sxd-pie-ng/internal/scheduler"
)

// SixRealmsConfig 六道轮回挑战与灵件处理策略配置。
type SixRealmsConfig struct {
	AutoReset       bool `yaml:"auto_reset" json:"auto_reset"`               // 通关后是否自动消耗每日免费重置次数
	AutoSellBlue    bool `yaml:"auto_sell_blue" json:"auto_sell_blue"`       // 挑战获胜是否自动出售蓝色灵件
	AutoSellPurple  bool `yaml:"auto_sell_purple" json:"auto_sell_purple"`   // 挑战获胜是否自动出售紫色灵件
	MaxFloorRetries int  `yaml:"max_floor_retries" json:"max_floor_retries"` // 每层挑战失败最大重试次数 (每层单独计算，失败达到上限后退出)
}

// DefaultSixRealmsConfig 返回六道轮回默认策略配置。
func DefaultSixRealmsConfig() SixRealmsConfig {
	return SixRealmsConfig{
		AutoReset:       true,
		AutoSellBlue:    true,
		AutoSellPurple:  false,
		MaxFloorRetries: 3,
	}
}

// NewSixRealmsRoutine 构造六道轮回挑战与灵件分解任务。
func NewSixRealmsRoutine(cfgs ...SixRealmsConfig) scheduler.ActivityRoutine {
	cfg := DefaultSixRealmsConfig()
	if len(cfgs) > 0 {
		cfg = cfgs[0]
	}
	return scheduler.NewBaseRoutine(
		"six_realms",
		"dungeon",
		scheduler.ScheduleCron,
		scheduler.PriorityNormal,
		0,
		func(ctx context.Context, session *client.RoleSession, jitter *scheduler.Jitter) error {
			if session == nil {
				return client.ErrNotConnected
			}
			if jitter != nil {
				_ = jitter.Wait(ctx)
			}
			slog.Info("正在执行副本任务: 六道轮回爬塔与灵件自动处理", "role_id", session.RoleID())

			// 1. 获取当前六道轮回塔面板与进度信息
			info, err := session.GetTowerInfo(ctx)
			if err != nil {
				slog.Warn("获取六道轮回塔信息失败或未开放", "role_id", session.RoleID(), "err", err)
				return nil
			}

			layerName := protocol.TowerLayerName(info.Layer)
			slog.Info("六道轮回塔当前状态",
				"role_id", session.RoleID(),
				"layer", info.Layer,
				"layer_name", layerName,
				"sequence_war", info.SequenceWar,
				"sequence_max", info.SequenceMax,
				"bag_num", info.BagNum,
				"reset_num", info.ResetNum,
				"reset_ingot", info.ResetIngot,
			)

			// 2. 检查等级门槛
			minLevel := protocol.TowerLayerLevelLimit(info.Layer)
			roleLevel := session.GetPlayerState().Level
			if roleLevel > 0 && roleLevel < minLevel {
				slog.Info("未达到当前六道挑战等级门槛，跳过挑战",
					"role_id", session.RoleID(),
					"role_level", roleLevel,
					"required_level", minLevel,
					"layer", info.Layer,
				)
				return nil
			}

			// 3. 灵件背包空间：get_tower_info 的 bag_num 字段语义未确认
			// （线上实测封灵背包为空时仍返回 20，疑为容量而非占用格数）。
			// 因此不再用 bag_num>=20 预判“已满”，直接发起挑战，
			// 由服务端 start_challenge 的 FULL_BAG(6) 结果码驱动暂停。
			slog.Debug("六道轮回面板背包字段（仅诊断，不作为门禁）",
				"role_id", session.RoleID(), "bag_num", info.BagNum)

			// 4. 判断是否需要重置: SequenceWar 表示当前待挑战层 (1..SequenceMax)，
			// 线上实测全通时返回 SequenceWar == SequenceMax（而非 >），因此必须用 >=。
			maxFloor := int(info.SequenceMax)
			if maxFloor <= 0 {
				maxFloor = 25
			}
			if int(info.SequenceWar) >= maxFloor {
				// 仙·天道(12) 为最终道：全通后服务端不再允许重置（2026-11-10 用户确认，
				// 且实测重置请求被静默丢弃）。直接结束，避免每 30 分钟无意义重试。
				if info.Layer >= protocol.TowerLayerXianHeaven {
					slog.Info("六道轮回已全通最终道且无法重置，挑战完成",
						"role_id", session.RoleID(),
						"layer", info.Layer,
						"layer_name", layerName,
					)
					return nil
				}
				if cfg.AutoReset && info.ResetNum > 0 {
					slog.Info("六道轮回当前道已全通，检测到今日剩余免费重置次数，执行重置",
						"role_id", session.RoleID(),
						"reset_num", info.ResetNum,
					)
					if jitter != nil {
						_ = jitter.Wait(ctx)
					}
					resetRes, resetErr := session.ResetTowerLayer(ctx)
					if resetErr != nil {
						slog.Warn("六道轮回塔重置无回包，稍后重试", "err", resetErr)
						return scheduler.NewCooldown(30*time.Minute, "六道轮回重置无回包")
					}
					if resetRes.Result != protocol.TowerResultSuccess {
						slog.Info("六道轮回塔重置未成功",
							"role_id", session.RoleID(), "result", resetRes.Result)
						return nil
					}
					slog.Info("六道轮回塔重置成功", "role_id", session.RoleID(), "layer", resetRes.Layer)
					// 刷新最新进度
					if newInfo, nErr := session.GetTowerInfo(ctx); nErr == nil {
						info = newInfo
					} else {
						info.SequenceWar = 1
					}
					if int(info.SequenceWar) >= maxFloor {
						slog.Info("六道轮回重置后仍显示全通，结束本轮",
							"role_id", session.RoleID(),
							"sequence_war", info.SequenceWar,
							"sequence_max", maxFloor)
						return nil
					}
				} else {
					slog.Info("六道轮回当前道今日已通关且无需重置或无免费重置次数，挑战完成",
						"role_id", session.RoleID(),
						"layer", info.Layer,
						"layer_name", layerName,
					)
					return nil
				}
			}

			// 5. 循环推进挑战未通关层数 (SequenceWar .. 25)
			// 注意: info.SequenceWar 是当前待挑战层数 (1~25)
			startFloor := int(info.SequenceWar)
			if startFloor <= 0 {
				startFloor = 1
			}

			maxRetries := cfg.MaxFloorRetries
			if maxRetries <= 0 {
				maxRetries = 3
			}

			for floor := startFloor; floor <= maxFloor; floor++ {
				floorRetries := 0
				floorWon := false

				for floorRetries < maxRetries {
					select {
					case <-ctx.Done():
						slog.Info("六道轮回挑战被外部上下文中断", "role_id", session.RoleID())
						return ctx.Err()
					default:
					}

					// 服务端连续战斗存在冷却保护 (对齐官方客户端 CommonConstant.DelayTimeToWar，需留足间隔)
					kit.Sleep(ctx, 1500*time.Millisecond)

					slog.Info("正在挑战六道轮回塔关卡",
						"role_id", session.RoleID(),
						"layer", info.Layer,
						"layer_name", protocol.TowerLayerName(info.Layer),
						"floor", floor,
						"retry", floorRetries,
						"max_retries", maxRetries,
					)

					// 自动卖出灵件策略
					res, chErr := session.ChallengeTower(ctx, uint8(floor), cfg.AutoSellBlue, cfg.AutoSellPurple)
					if chErr != nil {
						floorRetries++
						slog.Warn("六道轮回塔挑战请求失败",
							"role_id", session.RoleID(),
							"floor", floor,
							"retry", floorRetries,
							"max_retries", maxRetries,
							"err", chErr,
						)
						if floorRetries >= maxRetries {
							slog.Warn("六道轮回当前层重试次数已达上限，终止挑战",
								"role_id", session.RoleID(),
								"floor", floor,
								"retries", floorRetries,
							)
							return nil
						}
						continue
					}

					if res.Result == protocol.TowerResultSuccess {
						slog.Info("六道轮回塔关卡挑战胜利",
							"role_id", session.RoleID(),
							"floor", floor,
							"gain_exp", res.WarExp,
							"gain_fame", res.WarFame,
							"gain_coin", res.WarCoin,
							"soul_id", res.WarSoulID,
						)
						info.SequenceWar = uint8(floor + 1)
						floorWon = true
						if floor == maxFloor {
							slog.Info("恭喜通关六道轮回当前道全部层数！",
								"role_id", session.RoleID(),
								"layer", info.Layer,
								"layer_name", protocol.TowerLayerName(info.Layer),
							)
						}
						break
					}

					// 检查不可重试的硬终止条件
					if res.Result == protocol.TowerResultFullBag {
						slog.Info("灵件背包已满，终止六道轮回挑战", "role_id", session.RoleID())
						return nil
					}
					if res.Result == protocol.TowerResultFullTimes {
						slog.Info("今日六道挑战次数已达上限", "role_id", session.RoleID())
						return nil
					}
					if res.Result == protocol.TowerResultInvalidLayer {
						// 该层已通关/无效（线上全通后挑战最后一层返回 1），重试无意义。
						slog.Info("六道轮回该层已通关或无效，结束本轮",
							"role_id", session.RoleID(),
							"floor", floor,
							"sequence_war", info.SequenceWar,
							"sequence_max", maxFloor)
						return nil
					}

					// 挑战失败或者遭遇限制 (WAR_CD, 战力不足等)，每层单独计算重试次数
					floorRetries++
					slog.Warn("六道轮回关卡挑战未成功",
						"role_id", session.RoleID(),
						"floor", floor,
						"result_code", res.Result,
						"retry", floorRetries,
						"max_retries", maxRetries,
					)
					if floorRetries >= maxRetries {
						slog.Warn("六道轮回当前层挑战失败已达最大重试次数，终止爬塔",
							"role_id", session.RoleID(),
							"floor", floor,
							"retries", floorRetries,
						)
						return nil
					}
				}

				if !floorWon {
					break
				}

				if jitter != nil {
					_ = jitter.Wait(ctx)
				}
			}

			slog.Info("六道轮回爬塔任务执行完毕", "role_id", session.RoleID())
			return nil
		},
	)
}
