// Package daily 提供签到、邮件、在线奖励、绑定奖励与日常维护类玩法。
//
// 本文件实现「圣域修炼替身」(Mod_SaintAreaLogin_Base / Mod_SaPracticeRoom_Base)：
// 先查询圣域开启状态，再建立圣域独立长连接，进入免费修炼房入座并领取累积经验。
package daily

import (
	"context"
	"log/slog"
	"time"

	"sxd-pie-ng/internal/client"
	"sxd-pie-ng/internal/protocol"
	"sxd-pie-ng/internal/scheduler"
)

// saintPracticeRoomQuality 圣域免费修炼房品质 (官方 SaPracticeRoomController.freeQuality)。
const saintPracticeRoomQuality uint8 = 100

// maxSaintPracticeSeats 入座尝试的最大座位数。
const maxSaintPracticeSeats = 8

// NewSaintPracticeRoutine 构造圣域修炼替身任务。
//
// 官方流程:
//  1. 主服 Mod_SaintAreaLogin_Base.get_status (293/2) 确认圣域处于 OPEN；
//  2. get_login_info (293/1) 获取圣域网关凭据并建立独立长连接；
//  3. Mod_SaPracticeRoom_Base.get_free_room_info (407/16) 查询免费修炼房；
//  4. 未入座时 sit_down(quality=100, seat) 进入免费修炼房；
//  5. 有可领取经验时 get_room_practice_exp (407/6, use_type=0) 领取。
//
// 圣域关闭/维护或登录被拒时直接跳过，不消耗任何元宝。
func NewSaintPracticeRoutine() scheduler.ActivityRoutine {
	return scheduler.NewBaseRoutine(
		"saint_practice",
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
			slog.Info("正在执行日常任务: 圣域修炼替身", "role_id", session.RoleID())

			// 1. 圣域开启状态。
			sCtx, sCancel := context.WithTimeout(ctx, 5*time.Second)
			statusResp, err := session.Call(sCtx, protocol.BuildSaintAreaStatusPacket(), protocol.ActionIDSaintAreaStatus)
			sCancel()
			if err != nil || statusResp == nil {
				slog.Info("查询圣域状态无回包，跳过圣域修炼", "role_id", session.RoleID(), "err", err)
				return nil
			}
			status, err := protocol.ParseSaintAreaStatusResult(statusResp.Payload)
			if err != nil {
				slog.Warn("解析圣域状态失败，跳过圣域修炼", "role_id", session.RoleID(), "err", err)
				return nil
			}
			if status.Status != protocol.SaintAreaStatusOpen {
				slog.Info("圣域当前未开启，跳过圣域修炼",
					"role_id", session.RoleID(), "status", status.Status)
				return nil
			}

			// 2. 建立圣域独立会话。
			sa, err := session.OpenSaintAreaSession(ctx)
			if err != nil {
				slog.Info("建立圣域会话失败，跳过圣域修炼", "role_id", session.RoleID(), "err", err)
				return nil
			}
			defer sa.Close()

			call := func(pkt *protocol.Packet, act uint32) (*protocol.Packet, error) {
				cCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
				defer cancel()
				return sa.Call(cCtx, pkt, act)
			}

			// 3. 查询免费修炼房。
			freeResp, err := call(protocol.BuildSaPracticeRoomFreeInfoPacket(), protocol.ActionIDSaPracticeRoomGetFreeInfo)
			if err != nil || freeResp == nil {
				slog.Info("圣域修炼房不可用，跳过", "role_id", session.RoleID(), "err", err)
				return nil
			}
			room, err := protocol.ParseSaFreeRoomInfoResult(freeResp.Payload)
			if err != nil {
				slog.Warn("解析圣域修炼房信息失败，跳过", "role_id", session.RoleID(), "err", err)
				return nil
			}
			slog.Info("圣域修炼房状态",
				"role_id", session.RoleID(),
				"room_quality", room.RoomQuality,
				"exp", room.Exp,
				"practice_time", room.PracticeTime,
				"players", room.TotalPlayer,
			)

			// 4. 未入座则尝试进入免费修炼房。
			if room.RoomQuality <= 0 {
				seated := false
				for seat := 0; seat < maxSaintPracticeSeats; seat++ {
					if err := ctx.Err(); err != nil {
						return nil
					}
					sitResp, sErr := call(protocol.BuildSaPracticeRoomSitDownPacket(saintPracticeRoomQuality, uint8(seat)), protocol.ActionIDSaPracticeRoomSitDown)
					if sErr != nil || sitResp == nil {
						slog.Warn("圣域修炼房入座无回包", "role_id", session.RoleID(), "seat", seat, "err", sErr)
						break
					}
					code, pErr := protocol.ParseSaPracticeRoomActionResult(sitResp.Payload)
					if pErr != nil {
						slog.Warn("解析圣域修炼房入座回包失败", "role_id", session.RoleID(), "err", pErr)
						break
					}
					switch code {
					case protocol.SaPracticeRoomResultSuccess, protocol.SaPracticeRoomResultAlreadySatDown:
						seated = true
						slog.Info("圣域修炼房入座成功",
							"role_id", session.RoleID(), "quality", saintPracticeRoomQuality, "seat", seat)
					case protocol.SaPracticeRoomResultNoSeat:
						continue
					case protocol.SaPracticeRoomResultAlreadyInUse:
						seated = true
					case protocol.SaPracticeRoomResultFunctionNoOpen:
						slog.Info("圣域修炼房功能未开放", "role_id", session.RoleID())
						return nil
					default:
						slog.Info("圣域修炼房入座未成功", "role_id", session.RoleID(), "seat", seat, "result", code)
						continue
					}
					break
				}
				if !seated {
					slog.Info("圣域修炼房没有可用空位，跳过", "role_id", session.RoleID())
					return nil
				}
			}

			// 5. 有可领取经验则领取 (use_type=0 免费档)。
			flagResp, err := call(protocol.BuildSaPracticeRoomExpFlagPacket(), protocol.ActionIDSaPracticeRoomExpFlag)
			if err != nil || flagResp == nil {
				slog.Info("圣域修炼经验状态无回包，入座流程已完成", "role_id", session.RoleID())
				return nil
			}
			flag, err := protocol.ParseSaPracticeRoomActionResult(flagResp.Payload)
			if err != nil {
				slog.Warn("解析圣域修炼经验状态失败", "role_id", session.RoleID(), "err", err)
				return nil
			}
			if flag == 0 {
				slog.Info("圣域修炼暂无可领取经验", "role_id", session.RoleID())
				return nil
			}
			expResp, err := call(protocol.BuildSaPracticeRoomGetExpPacket(0), protocol.ActionIDSaPracticeRoomGetExp)
			if err != nil || expResp == nil {
				slog.Warn("领取圣域修炼经验无回包", "role_id", session.RoleID(), "err", err)
				return nil
			}
			code, exp, pErr := protocol.ParseSaPracticeRoomExpResult(expResp.Payload)
			if pErr != nil {
				slog.Warn("解析圣域修炼经验回包失败", "role_id", session.RoleID(), "err", pErr)
				return nil
			}
			switch code {
			case protocol.SaPracticeRoomResultSuccess:
				slog.Info("圣域修炼经验领取成功", "role_id", session.RoleID(), "exp", exp)
			case protocol.SaPracticeRoomResultNoExp:
				slog.Info("圣域修炼暂无可领取经验", "role_id", session.RoleID())
			case protocol.SaPracticeRoomResultNoEnoughDaoYuan:
				slog.Info("圣域修炼经验领取需要道缘，跳过", "role_id", session.RoleID())
			default:
				slog.Info("圣域修炼经验未领取", "role_id", session.RoleID(), "result", code)
			}
			return nil
		},
	)
}
