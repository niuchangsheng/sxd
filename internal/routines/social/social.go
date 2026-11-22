// Package social 提供壶中界、仙盟、圣盟、好友祝福与帮派社交自动化任务。
package social

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"sxd-pie-ng/internal/client"
	"sxd-pie-ng/internal/protocol"
	"sxd-pie-ng/internal/scheduler"
)

// NewGuildActivitiesRoutine 构造仙盟神兽与捐献任务。
// NewGuildActivitiesRoutine 构造仙盟日常任务 (Mod 571/591/575)。
//
// 流程：主服读取仙盟分组与城镇凭据 → 建立仙盟城镇会话 (Mod 575 登录+enter_town)
// → 读取仙盟主页 → 领取仙盟福利 → 每日膜拜。
// 未加入仙盟/城镇登录失败时安全跳过；不消耗元宝。
func NewGuildActivitiesRoutine() scheduler.ActivityRoutine {
	const townCtxTimeout = 30 * time.Second
	const callTimeout = 8 * time.Second
	return scheduler.NewBaseRoutine(
		"guild_activities",
		"social",
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

			// 先查功能开放列表；仙盟未开放时不再申请跨服城镇会话并等待超时。
			open, fErr := session.IsFunctionOpen(ctx, protocol.FunctionIDFaction)
			if fErr != nil {
				slog.Info("仙盟功能开放状态查询失败，稍后重试仙盟日常", "role_id", session.RoleID(), "err", fErr)
				return scheduler.NewCooldown(30*time.Minute, "仙盟功能开放状态无回包")
			}
			if !open {
				slog.Info("仙盟功能未开放，跳过仙盟日常", "role_id", session.RoleID(), "function_id", protocol.FunctionIDFaction)
				return nil
			}

			townCtx, townCancel := context.WithTimeout(ctx, townCtxTimeout)
			defer townCancel()
			town, err := session.OpenSectTownSession(townCtx)
			if err != nil {
				if errors.Is(err, client.ErrNotInSect) {
					slog.Info("账号未加入仙盟，跳过仙盟日常", "role_id", session.RoleID())
					return nil
				}
				if errors.Is(err, context.DeadlineExceeded) {
					slog.Info("仙盟面板无回包，稍后重试仙盟日常", "role_id", session.RoleID())
				} else {
					slog.Info("仙盟城镇会话不可用，稍后重试仙盟日常", "role_id", session.RoleID(), "err", err)
				}
				return scheduler.NewCooldown(30*time.Minute, "仙盟城镇会话无回包")
			}
			defer town.Close()

			cred, err := session.FetchSectTownCredentials(townCtx)
			if err != nil {
				slog.Warn("读取仙盟凭据失败，稍后重试", "role_id", session.RoleID(), "err", err)
				return scheduler.NewCooldown(30*time.Minute, "仙盟凭据无回包")
			}

			// 1. 仙盟主页
			mCtx, mCancel := context.WithTimeout(townCtx, callTimeout)
			mainResp, err := town.Call(mCtx, protocol.BuildSectMainInfoPacket(cred.SectID), 0x023C0001)
			mCancel()
			if err != nil {
				slog.Warn("读取仙盟主页失败", "role_id", session.RoleID(), "err", err)
			} else if mainInfo, pErr := protocol.ParseSectMainInfoResult(mainResp.Payload); pErr != nil {
				slog.Warn("解析仙盟主页失败", "role_id", session.RoleID(), "err", pErr)
			} else {
				slog.Info("仙盟主页",
					"role_id", session.RoleID(),
					"result", mainInfo.Result,
					"sect_id", mainInfo.SelfSectID,
					"sect_name", mainInfo.SectName,
					"sect_level", mainInfo.SectLevel,
					"alliance_id", mainInfo.AllianceID,
				)
			}

			// 2. 仙盟福利领取
			bCtx, bCancel := context.WithTimeout(townCtx, callTimeout)
			bonusResp, err := town.Call(bCtx, protocol.BuildSectBonusPanelPacket(cred.SectID), 0x024F0001)
			bCancel()
			if err != nil {
				slog.Warn("读取仙盟福利面板失败", "role_id", session.RoleID(), "err", err)
			} else if panel, pErr := protocol.ParseSectBonusPanelResult(bonusResp.Payload); pErr != nil {
				slog.Warn("解析仙盟福利面板失败", "role_id", session.RoleID(), "err", pErr)
			} else {
				slog.Info("仙盟福利面板",
					"role_id", session.RoleID(),
					"result", panel.Result,
					"gifts", panel.Gifts,
					"left_get_times", panel.LeftGetTimes,
					"sect_cont", panel.SectCont,
				)
				if panel.Result == 0 && panel.LeftGetTimes > 0 {
					// 官方客户端 get_gift 的参数是宗门 ID（不是 gift_id），每次领取一份。
					claimed := int32(0)
					for claimed < panel.LeftGetTimes {
						gCtx, gCancel := context.WithTimeout(townCtx, callTimeout)
						gResp, gErr := town.Call(gCtx, protocol.BuildSectBonusGetGiftPacket(cred.SectID), 0x024F0003)
						gCancel()
						if gErr != nil {
							slog.Info("领取仙盟福利无应答", "role_id", session.RoleID(), "err", gErr)
							break
						}
						result, rErr := protocol.ParseSectActionResult(gResp.Payload)
						if rErr != nil {
							slog.Warn("解析仙盟福利回包失败", "role_id", session.RoleID(), "err", rErr)
							break
						}
						switch result {
						case 0:
							claimed++
							slog.Info("仙盟福利领取成功", "role_id", session.RoleID(), "index", claimed)
						case 3: // LESS_TIMES
							slog.Info("仙盟福利领取次数已用尽", "role_id", session.RoleID(), "claimed", claimed)
							claimed = panel.LeftGetTimes
						case 4: // ITEM_PACK_FULL
							return scheduler.ErrBagFull
						case 5: // NO_GIFT
							slog.Info("仙盟福利暂无可领礼包", "role_id", session.RoleID(), "claimed", claimed)
							claimed = panel.LeftGetTimes
						case 1: // FUNC_NOT_OPEN
							slog.Info("仙盟福利功能未开放/未满足贡献条件", "role_id", session.RoleID(), "claimed", claimed)
							claimed = panel.LeftGetTimes
						case 2: // FAILED
							slog.Info("仙盟福利领取失败", "role_id", session.RoleID(), "claimed", claimed)
							claimed = panel.LeftGetTimes
						default:
							slog.Info("仙盟福利未领取", "role_id", session.RoleID(), "result", result, "claimed", claimed)
							claimed = panel.LeftGetTimes
						}
					}
				}
			}

			// 3. 每日膜拜
			wCtx, wCancel := context.WithTimeout(townCtx, callTimeout)
			wResp, err := town.Call(wCtx, protocol.BuildSectWorshipPacket(cred.SectID), 0x023C000D)
			wCancel()
			if err != nil {
				slog.Info("仙盟膜拜无应答", "role_id", session.RoleID(), "err", err)
			} else if result, rErr := protocol.ParseSectActionResult(wResp.Payload); rErr != nil {
				slog.Warn("解析仙盟膜拜回包失败", "role_id", session.RoleID(), "err", rErr)
			} else if result == 0 {
				slog.Info("仙盟膜拜成功", "role_id", session.RoleID())
			} else if result == 16 { // IS_WORSHIP
				slog.Info("今日已膜拜", "role_id", session.RoleID())
			} else {
				slog.Info("仙盟膜拜未成功", "role_id", session.RoleID(), "result", result)
			}
			return nil
		},
	)
}

// NewSacredAllianceRoutine 构造圣盟祭祀与幻魔塔任务。
// NewSacredAllianceRoutine 构造圣盟日常任务 (Module 884)。
//
// 先查询圣盟当前赛季；无应答/赛季数据缺失时视为功能未开放并跳过。
func NewSacredAllianceRoutine() scheduler.ActivityRoutine {
	const callTimeout = 8 * time.Second
	return scheduler.NewBaseRoutine(
		"sacred_alliance",
		"social",
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

			cCtx, cCancel := context.WithTimeout(ctx, callTimeout)
			resp, err := session.Call(cCtx, protocol.BuildSectAllianceSeasonPacket(), protocol.ActionIDSectAllianceSeason)
			cCancel()
			if err != nil {
				slog.Info("圣盟功能不可用/未开放，跳过", "role_id", session.RoleID(), "err", err)
				return nil
			}
			season, endTime, pErr := protocol.ParseSectAllianceSeasonResult(resp.Payload)
			if pErr != nil {
				slog.Warn("解析圣盟赛季失败", "role_id", session.RoleID(), "err", pErr)
				return nil
			}
			if season <= 0 {
				slog.Info("圣盟当前无有效赛季，跳过", "role_id", session.RoleID())
				return nil
			}
			slog.Info("圣盟赛季开放；祭祀/奖励领取需仙盟城镇会话与 alliance_id，待下一批接入",
				"role_id", session.RoleID(),
				"season", season,
				"end_time", endTime,
			)
			return nil
		},
	)
}

// NewHomesteadRoutine 构造住宅家具祝福与夫妻宝箱任务。
// NewHomesteadRoutine 构造住宅日常任务 (Module 210)。
//
// 先查询自身住宅状态：未处于婚姻状态或功能未开放时跳过。
func NewHomesteadRoutine() scheduler.ActivityRoutine {
	const callTimeout = 8 * time.Second
	return scheduler.NewBaseRoutine(
		"homestead",
		"social",
		scheduler.ScheduleCron,
		scheduler.PriorityLow,
		0,
		func(ctx context.Context, session *client.RoleSession, jitter *scheduler.Jitter) error {
			if session == nil {
				return client.ErrNotConnected
			}
			if jitter != nil {
				_ = jitter.Wait(ctx)
			}

			// 功能开放原生探测：未开通家园的账号连 210/0 都可能被静默丢弃
			// （2026-11-17 qq 账号实测），先按功能门槛跳过，避免每轮白等超时。
			if open, fErr := session.IsFunctionOpen(ctx, protocol.FunctionIDMarryHome); fErr != nil {
				slog.Info("家园功能开放状态查询失败，本轮跳过",
					"role_id", session.RoleID(), "err", fErr)
				return scheduler.NewCooldown(30*time.Minute, "家园功能开放状态查询失败")
			} else if !open {
				slog.Info("家园功能未开放，跳过",
					"role_id", session.RoleID(), "function_id", protocol.FunctionIDMarryHome)
				return nil
			}

			// 官方小助手"家园奖励"直接调用 210/18，不发 210/0。
			// 但未结婚/无家园角色发送 210/18 会被服务端静默丢弃（qq 实测），
			// 因此先用只读的 210/0 判定婚姻状态：未结婚直接挂起至次日，
			// 已结婚才发送 210/18，避免每轮固定超时 8 秒。
			sCtx, sCancel := context.WithTimeout(ctx, callTimeout)
			selfResp, sErr := session.Call(sCtx,
				protocol.BuildMarryHomeSelfInfoPacket(session.PlayerID()), protocol.ActionIDMarryHomeSelfInfo)
			sCancel()
			if sErr != nil || selfResp == nil {
				slog.Info("住宅状态查询无回包，稍后重试", "role_id", session.RoleID(), "err", sErr)
				return scheduler.NewCooldown(30*time.Minute, "住宅状态查询无回包")
			}
			selfResult, pErr := protocol.ParseMarryHomeSelfInfoResult(selfResp.Payload)
			if pErr != nil {
				return scheduler.NewCooldown(10*time.Minute, "住宅状态解析失败")
			}
			// 2026-11-14 实测（qq s0 已婚）：210/0 的返回值 3 并非可靠的婚姻标志，
			// 更像住宅状态。此处只记录，不做硬门禁；由领取结果码决定后续。
			slog.Info("住宅状态", "role_id", session.RoleID(), "self_result", selfResult,
				"payload_hex", hex.EncodeToString(selfResp.Payload))

			// 210/0 的返回值无法判定婚姻状态；用免费家园蛋糕 (210/19) 的结果码
			// 探测：3=未结婚/无家园，此时不再请求 210/18（会静默丢弃）。
			pCtx, pCancel := context.WithTimeout(ctx, callTimeout)
			cakeResp, pErr := session.Call(pCtx,
				protocol.BuildMarryHomeGetCakePacket(session.PlayerID()),
				protocol.ActionIDMarryHomeGetCake)
			pCancel()
			if pErr != nil || cakeResp == nil {
				slog.Info("住宅婚姻状态探测无回包，稍后重试", "role_id", session.RoleID(), "err", pErr)
				return scheduler.NewCooldown(2*time.Hour, "住宅婚姻状态探测无回包")
			}
			cakeRes, cErr := protocol.ParseMarryHomeCakeResult(cakeResp.Payload)
			if cErr != nil {
				slog.Warn("解析住宅蛋糕结果失败，稍后重试", "role_id", session.RoleID(), "err", cErr)
				return scheduler.NewCooldown(30*time.Minute, "住宅蛋糕结果解析失败")
			}
			switch cakeRes.Result {
			case protocol.MarryHomeResultNonMarryStatus:
				slog.Info("家园蛋糕返回未结婚/无家园，跳过家具效果", "role_id", session.RoleID())
				return scheduler.ErrAttemptsExhausted
			case protocol.MarryHomeResultFunctionNoOpen:
				slog.Info("家园功能未开放，跳过家具效果", "role_id", session.RoleID())
				return scheduler.ErrAttemptsExhausted
			case protocol.MarryHomeResultNonActivityTime:
				slog.Info("家园活动未开放，稍后重试", "role_id", session.RoleID())
				return scheduler.NewCooldown(2*time.Hour, "家园活动未开放")
			}

			bCtx, bCancel := context.WithTimeout(ctx, callTimeout)
			effectResp, bErr := session.Call(bCtx, protocol.BuildMarryHomeBatchFurnitureEffectPacket(session.LastActionID()), protocol.ActionIDMarryHomeBatchFurnitureEffect)
			bCancel()
			if bErr != nil || effectResp == nil {
				slog.Info("住宅家具效果领取无回包，稍后重试", "role_id", session.RoleID(), "err", bErr)
				return scheduler.NewCooldown(2*time.Hour, "住宅家具效果无回包")
			}
			if len(effectResp.Payload) == 0 {
				slog.Info("住宅暂无可领取家具效果", "role_id", session.RoleID())
				return nil
			}
			effects, eErr := protocol.ParseMarryHomeBatchFurnitureEffectResult(effectResp.Payload)
			if eErr != nil {
				slog.Warn("解析住宅家具效果失败，稍后重试", "role_id", session.RoleID(), "err", eErr)
				return scheduler.NewCooldown(10*time.Minute, "住宅家具效果解析失败")
			}
			slog.Info("住宅家具效果领取完成",
				"role_id", session.RoleID(), "count", len(effects), "effects", effects)
			return nil
		},
	)
}

// NewFriendshipRoutine 构造好友来信领取任务 (Module 684, Mod_FriendsSent_Base)。
//
// 拉取好友来信列表，仅处理已解锁 (时间/任务条件均满足) 的来信：
// 未读则标记已读，未领奖则领取附件奖励。不消耗元宝。
func NewFriendshipRoutine() scheduler.ActivityRoutine {
	const callTimeout = 8 * time.Second
	return scheduler.NewBaseRoutine(
		"friendship",
		"social",
		scheduler.ScheduleCron,
		scheduler.PriorityLow,
		0,
		func(ctx context.Context, session *client.RoleSession, jitter *scheduler.Jitter) error {
			if session == nil {
				return client.ErrNotConnected
			}
			if jitter != nil {
				_ = jitter.Wait(ctx)
			}

			pCtx, pCancel := context.WithTimeout(ctx, callTimeout)
			panelResp, err := session.Call(pCtx, protocol.BuildFriendsSentPanelPacket(), protocol.ActionIDFriendsSentPanel)
			pCancel()
			if err != nil {
				slog.Info("好友来信功能不可用，稍后重试", "role_id", session.RoleID(), "err", err)
				return scheduler.NewCooldown(30*time.Minute, "好友来信面板无回包")
			}
			letters, err := protocol.ParseFriendsSentPanelResult(panelResp.Payload)
			if err != nil {
				return fmt.Errorf("解析好友来信列表失败: %w", err)
			}
			unlocked := 0
			for _, l := range letters {
				if l.IsTimeLock != 0 || l.IsMissionLock != 0 {
					continue
				}
				unlocked++
				octx, ocancel := context.WithTimeout(ctx, callTimeout)
				openResp, oErr := session.Call(octx, protocol.BuildFriendsSentOpenPacket(l.ID), protocol.ActionIDFriendsSentOpen)
				ocancel()
				if oErr != nil {
					slog.Info("打开好友来信无应答，稍后重试", "role_id", session.RoleID(), "letter_id", l.ID, "err", oErr)
					return scheduler.NewCooldown(10*time.Minute, "打开好友来信无回包")
				}
				detail, pErr := protocol.ParseFriendsSentOpenResult(openResp.Payload)
				if pErr != nil {
					slog.Warn("解析好友来信内容失败", "role_id", session.RoleID(), "letter_id", l.ID, "err", pErr)
					continue
				}
				// 领取前总是补发一次 read：部分区服要求领奖请求前本会话内
				// 有 read 动作，即使账号维度的 is_read 已经是 1。
				if detail.IsGetAward == 0 {
					rctx, rcancel := context.WithTimeout(ctx, callTimeout)
					rResp, rErr := session.Call(rctx, protocol.BuildFriendsSentReadPacket(l.ID), protocol.ActionIDFriendsSentRead)
					rcancel()
					if rErr != nil || rResp == nil {
						slog.Info("标记好友来信已读无回包，稍后重试",
							"role_id", session.RoleID(), "letter_id", l.ID, "err", rErr)
						return scheduler.NewCooldown(10*time.Minute, "好友来信已读请求无回包")
					}
					if result, e := protocol.ParseFriendsSentActionResult(rResp.Payload); e == nil &&
						result != protocol.FriendsSentResultSuccess {
						slog.Info("标记好友来信已读未成功", "role_id", session.RoleID(),
							"letter_id", l.ID, "result", result)
					}
				}
				if detail.IsGetAward == 0 && len(detail.Awards) > 0 {
					actx, acancel := context.WithTimeout(ctx, callTimeout)
					aResp, aErr := session.Call(actx, protocol.BuildFriendsSentAwardPacket(l.ID), protocol.ActionIDFriendsSentAward)
					acancel()
					if aErr != nil {
						slog.Info("领取好友来信奖励无应答", "role_id", session.RoleID(), "letter_id", l.ID, "err", aErr)
						continue
					}
					result, e := protocol.ParseFriendsSentActionResult(aResp.Payload)
					if e != nil {
						slog.Warn("解析好友来信奖励回包失败", "role_id", session.RoleID(), "letter_id", l.ID, "err", e)
						continue
					}
					switch result {
					case protocol.FriendsSentResultSuccess:
						slog.Info("好友来信奖励领取成功", "role_id", session.RoleID(), "letter_id", l.ID, "awards", detail.Awards)
					case protocol.FriendsSentResultItemPackFull:
						return scheduler.ErrBagFull
					case protocol.FriendsSentResultUnRead:
						slog.Info("好友来信未阅读，跳过领奖", "role_id", session.RoleID(), "letter_id", l.ID)
					default:
						// 线上两套账号实测: 均已读、is_get_award=0 且列出附件，
						// get_award 仍固定返回 FAILED(1)，通常表示历史信件已领/不可领。
						slog.Info("好友来信奖励不可领取", "role_id", session.RoleID(),
							"letter_id", l.ID, "result", result)
					}
				}
			}
			slog.Info("好友来信处理完成", "role_id", session.RoleID(), "letters", len(letters), "unlocked", unlocked)
			return nil
		},
	)
}

// NewFameBlessingRoutine 构造仙界膜拜大神与声望祝福任务。
// NewFameBlessingRoutine 构造尊像膜拜任务 (Module 41, Mod_WorshipMars_Base)。
//
// 查询膜拜信息与剩余次数后，只使用免费供品 (白檀香, id=1) 上香；
// 需要元宝的苏合香/天木香一律不调用。
func NewFameBlessingRoutine() scheduler.ActivityRoutine {
	const callTimeout = 8 * time.Second
	return scheduler.NewBaseRoutine(
		"fame_blessing",
		"social",
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

			// 0. 功能开放检查：使用 Mod 0 Act 6 的会话缓存。
			// 逐功能的 check_function_open 会被服务端静默丢弃，探测失败时继续走面板查询，
			// 由面板结果判定玩法可用性 (线上 s0 已实测面板可正常返回)。
			open, chkErr := session.IsFunctionOpen(ctx, protocol.FunctionIDWorshipMars)
			if chkErr != nil {
				slog.Info("查询膜拜功能开放状态失败，继续尝试面板查询",
					"role_id", session.RoleID(), "err", chkErr)
			} else if !open {
				slog.Info("膜拜功能未开启，跳过", "role_id", session.RoleID())
				return nil
			}

			// 1. 膜拜信息：无数据时跳过。
			iCtx, iCancel := context.WithTimeout(ctx, callTimeout)
			infoResp, err := session.Call(iCtx, protocol.BuildWorshipMarsInfoPacket(), protocol.ActionIDWorshipMarsInfo)
			iCancel()
			if err != nil {
				slog.Info("查询膜拜信息无回包，稍后重试", "role_id", session.RoleID(), "err", err)
				return scheduler.NewCooldown(30*time.Minute, "膜拜信息无回包")
			}
			info, err := protocol.ParseWorshipMarsInfoResult(infoResp.Payload)
			if err != nil {
				slog.Warn("解析膜拜信息失败，稍后重试", "role_id", session.RoleID(), "err", err)
				return scheduler.NewCooldown(10*time.Minute, "膜拜信息解析失败")
			}
			slog.Info("膜拜信息",
				"role_id", session.RoleID(),
				"mars_level", info.MarsLevel,
				"incense_count", info.IncenseCount,
				"total_count", info.TotalCount,
				"logs", info.LogEntryCount,
			)

			// 2. 剩余免费次数
			rCtx, rCancel := context.WithTimeout(ctx, callTimeout)
			remainResp, err := session.Call(rCtx, protocol.BuildWorshipMarsRemainPacket(), protocol.ActionIDWorshipMarsRemain)
			rCancel()
			if err != nil {
				slog.Info("查询剩余膜拜次数失败，稍后重试", "role_id", session.RoleID(), "err", err)
				return scheduler.NewCooldown(30*time.Minute, "膜拜剩余次数无回包")
			}
			remain, err := protocol.ParseWorshipMarsRemainResult(remainResp.Payload)
			if err != nil {
				slog.Warn("解析剩余膜拜次数失败，稍后重试", "role_id", session.RoleID(), "err", err)
				return scheduler.NewCooldown(10*time.Minute, "膜拜剩余次数解析失败")
			}
			if remain <= 0 {
				slog.Info("膜拜免费次数已用尽", "role_id", session.RoleID())
				return nil
			}

			// 3. 使用免费供品 (白檀香) 上香，直到次数耗尽或失败。
			done := int32(0)
			dropped := false
			for done < remain {
				cCtx, cCancel := context.WithTimeout(ctx, callTimeout)
				resp, cErr := session.Call(cCtx, protocol.BuildWorshipMarsIncensePacket(protocol.WorshipMarsOfferingFree), protocol.ActionIDWorshipMarsIncense)
				cCancel()
				if cErr != nil {
					slog.Info("上香无应答", "role_id", session.RoleID(), "done", done, "err", cErr)
					dropped = true
					break
				}
				result, pErr := protocol.ParseWorshipMarsIncenseResult(resp.Payload)
				if pErr != nil {
					slog.Warn("解析上香回包失败", "role_id", session.RoleID(), "err", pErr)
					break
				}
				switch result {
				case protocol.WorshipMarsResultSuccess:
					done++
					slog.Info("免费上香成功", "role_id", session.RoleID(), "index", done)
				case protocol.WorshipMarsResultCountUsed:
					slog.Info("膜拜次数已用尽", "role_id", session.RoleID(), "done", done)
					done = remain
				case protocol.WorshipMarsResultBlessingTimes, protocol.WorshipMarsResultExpAdd:
					slog.Info("膜拜祝福/经验加成已满", "role_id", session.RoleID(), "result", result)
					done = remain
				case protocol.WorshipMarsResultInsufficientIngot:
					slog.Warn("上香提示元宝不足 (免费供品不应出现)", "role_id", session.RoleID())
					done = remain
				default:
					slog.Warn("上香返回未知结果", "role_id", session.RoleID(), "result", result)
					done = remain
				}
				if done < remain {
					select {
					case <-ctx.Done():
						return nil
					case <-time.After(200 * time.Millisecond):
					}
				}
			}
			slog.Info("膜拜完成", "role_id", session.RoleID(), "free_incense_done", done)
			if done == 0 && dropped {
				return scheduler.NewCooldown(10*time.Minute, "膜拜上香无回包")
			}
			return nil
		},
	)
}
