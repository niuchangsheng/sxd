// Package daily 提供签到、邮件、在线奖励、绑定奖励与日常维护类玩法。
//
// 本文件实现每日免费礼包补齐：每日俸禄、每日灵石与 PC 微端礼包。
package daily

import (
	"context"
	"log/slog"
	"time"

	"sxd-pie-ng/internal/client"
	"sxd-pie-ng/internal/protocol"
	"sxd-pie-ng/internal/scheduler"
)

// NewDailyGiftsRoutine 构造每日免费礼包领取任务。
//
// 对齐原版助手「自动领取每日礼包」中的三项独立免费产出：
//   - 每日俸禄: Mod_Player_Base.get_player_camp_salary (Module 0 Act 20)
//   - 每日灵石: Mod_SealSoul_Base.get_day_stone (Module 34 Act 18, use_type=0 免费)
//   - PC 微端礼包: Mod_Player_Base.pc_client_gift_info / receive_client_gift (Module 0 Act 55/56)
//
// 极限挑战/阵营战等终结礼包由 online_gift 任务统一领取，此处不重复处理。
// 每日灵石只使用 use_type=0 的免费档，绝不消耗元宝。
func NewDailyGiftsRoutine() scheduler.ActivityRoutine {
	return scheduler.NewBaseRoutine(
		"daily_gifts",
		"daily",
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
			slog.Info("正在执行日常任务: 每日免费礼包", "role_id", session.RoleID())

			call := func(pkt *protocol.Packet, act uint32) (*protocol.Packet, error) {
				cCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
				defer cancel()
				return session.Call(cCtx, pkt, act)
			}

			// 1. 每日俸禄。
			if resp, err := call(protocol.BuildCampSalaryPacket(), protocol.ActionIDPlayerCampSalary); err != nil || resp == nil {
				slog.Info("领取每日俸禄无回包", "role_id", session.RoleID(), "err", err)
			} else if result, coin, pErr := protocol.ParseCampSalaryResult(resp.Payload); pErr != nil {
				slog.Warn("解析每日俸禄回包失败", "role_id", session.RoleID(), "err", pErr)
			} else {
				switch result {
				case protocol.PlayerCampSalarySuccess:
					slog.Info("每日俸禄领取成功", "role_id", session.RoleID(), "coin", coin)
				case protocol.PlayerCampSalaryAlreadyGet:
					slog.Info("每日俸禄今日已领取", "role_id", session.RoleID())
				case protocol.PlayerCampSalaryNotJoinCamp:
					slog.Info("未加入阵营，跳过每日俸禄", "role_id", session.RoleID())
				case protocol.PlayerCampSalaryPackFull:
					return scheduler.ErrBagFull
				default:
					slog.Info("每日俸禄未领取", "role_id", session.RoleID(), "result", result)
				}
			}

			// 2. 每日灵石 (免费档)。
			if resp, err := call(protocol.BuildSealSoulDayStonePacket(0), protocol.ActionIDSealSoulDayStone); err != nil || resp == nil {
				slog.Info("领取每日灵石无回包", "role_id", session.RoleID(), "err", err)
			} else if state, count, pErr := protocol.ParseSealSoulDayStoneResult(resp.Payload); pErr != nil {
				slog.Warn("解析每日灵石回包失败", "role_id", session.RoleID(), "err", pErr)
			} else {
				switch state {
				case protocol.SealSoulDayStoneSuccess:
					slog.Info("每日灵石领取成功", "role_id", session.RoleID(), "count", count)
				case protocol.SealSoulDayStoneFail:
					slog.Info("每日灵石领取失败或今日已领取", "role_id", session.RoleID())
				case protocol.SealSoulDayStoneNoVIP:
					slog.Info("每日灵石需要更高 VIP，跳过", "role_id", session.RoleID())
				case protocol.SealSoulDayStoneNoIngot:
					slog.Info("每日灵石元宝档不可用，仅使用免费档", "role_id", session.RoleID())
				default:
					slog.Info("每日灵石未领取", "role_id", session.RoleID(), "state", state)
				}
			}

			// 3. PC 微端礼包。
			if resp, err := call(protocol.BuildClientGiftInfoPacket(), protocol.ActionIDPlayerClientGiftInfo); err != nil || resp == nil {
				slog.Info("查询 PC 微端礼包无回包", "role_id", session.RoleID(), "err", err)
			} else if info, pErr := protocol.ParseClientGiftInfoResult(resp.Payload); pErr != nil {
				slog.Warn("解析 PC 微端礼包失败", "role_id", session.RoleID(), "err", pErr)
			} else if info.Status == 0 {
				slog.Info("PC 微端礼包未就绪", "role_id", session.RoleID(), "month_status", info.MonthStatus)
			} else {
				rResp, rErr := call(protocol.BuildClientGiftReceivePacket(), protocol.ActionIDPlayerReceiveClientGift)
				if rErr != nil || rResp == nil {
					slog.Info("领取 PC 微端礼包无回包", "role_id", session.RoleID(), "err", rErr)
				} else if result, rpErr := protocol.ParseClientGiftReceiveResult(rResp.Payload); rpErr != nil {
					slog.Warn("解析 PC 微端礼包领取回包失败", "role_id", session.RoleID(), "err", rpErr)
				} else {
					switch result {
					case protocol.PlayerClientGiftSuccess:
						slog.Info("PC 微端礼包领取成功", "role_id", session.RoleID())
					case protocol.PlayerClientGiftBagFull:
						return scheduler.ErrBagFull
					default:
						slog.Info("PC 微端礼包未领取", "role_id", session.RoleID(), "result", result)
					}
				}
			}

			slog.Info("每日免费礼包巡检完成", "role_id", session.RoleID())
			return nil
		},
	)
}
