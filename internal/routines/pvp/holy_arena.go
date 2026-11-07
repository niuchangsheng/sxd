// Package pvp 提供竞技场、天梯、劫镖、世界 Boss 等对抗玩法自动化。
//
// 本文件实现圣域竞技场自动抢占席位 (Mod_SaArena_Base, Module 298)。
package pvp

import (
	"context"
	"errors"
	"log/slog"
	"sort"
	"time"

	"sxd-pie-ng/internal/client"
	"sxd-pie-ng/internal/protocol"
	"sxd-pie-ng/internal/scheduler"
)

// maxSaArenaRobs 单轮调度的最大抢占次数。
const maxSaArenaRobs = 10

// openSaintAreaSession 建立圣域独立长连接。抽成变量以便单测注入。
var openSaintAreaSession = func(ctx context.Context, s *client.RoleSession) (*client.RoleSession, error) {
	return s.OpenSaintAreaSession(ctx)
}

// NewHolyArenaRoutine 构造圣域竞技场自动抢占任务。
//
// 官方 com.datas.SaArenaData: get_info 返回 5 个擂台席位与剩余可抢占次数。
// 空席位 (sa_player_id=0) 可直接免费占据；有人的席位则发起抢占战斗。
// 自动化优先选择空席位，其次选择战力最低的对手，避免无意义消耗。
//
// 官方 SaArenaThroneView 将 get_info / rob_the_seat 全部路由到圣域连接
// (DataBase.SOCKETSAINTAREA)。线上实测：主服调用 get_info 返回全空席位，
// rob_the_seat 固定返回 ACTIVITY_CLOSE(3)；必须改用圣域会话才能看到真实席位。
//
// 圣域竞技场不消耗元宝，只消耗每日免费抢占次数。
func NewHolyArenaRoutine() scheduler.ActivityRoutine {
	return scheduler.NewBaseRoutine(
		"holy_arena",
		"pvp",
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
			slog.Info("正在执行对抗任务: 圣域竞技场", "role_id", session.RoleID())

			// 圣域竞技场隶属圣域玩法，先查功能开放列表；未开放时不发送业务请求。
			open, fErr := session.IsFunctionOpen(ctx, protocol.FunctionIDSaintArea)
			if fErr != nil {
				slog.Info("圣域竞技场功能开放状态查询失败，跳过", "role_id", session.RoleID(), "err", fErr)
				return nil
			}
			if !open {
				slog.Info("圣域竞技场功能未开放，跳过", "role_id", session.RoleID(), "function_id", protocol.FunctionIDSaintArea)
				return nil
			}

			// 圣域开启状态：关闭/维护时直接跳过，避免建立无意义的透传连接。
			sCtx, sCancel := context.WithTimeout(ctx, 5*time.Second)
			statusResp, sErr := session.Call(sCtx, protocol.BuildSaintAreaStatusPacket(), protocol.ActionIDSaintAreaStatus)
			sCancel()
			if sErr != nil || statusResp == nil {
				slog.Info("查询圣域状态无回包，跳过圣域竞技场", "role_id", session.RoleID(), "err", sErr)
				return nil
			}
			status, sErr := protocol.ParseSaintAreaStatusResult(statusResp.Payload)
			if sErr != nil {
				slog.Warn("解析圣域状态失败，跳过圣域竞技场", "role_id", session.RoleID(), "err", sErr)
				return nil
			}
			if status.Status != protocol.SaintAreaStatusOpen {
				slog.Info("圣域当前未开启，跳过圣域竞技场",
					"role_id", session.RoleID(), "status", status.Status)
				return nil
			}

			// 竞技场协议必须走圣域独立长连接。
			sa, oErr := openSaintAreaSession(ctx, session)
			if oErr != nil || sa == nil {
				slog.Info("建立圣域会话失败，跳过圣域竞技场", "role_id", session.RoleID(), "err", oErr)
				return nil
			}
			if sa != session {
				defer sa.Close()
			}

			call := func(pkt *protocol.Packet, act uint32) (*protocol.Packet, error) {
				cCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
				defer cancel()
				return sa.Call(cCtx, pkt, act)
			}

			fetchInfo := func() (*protocol.SaArenaInfo, error) {
				resp, err := call(protocol.BuildSaArenaGetInfoPacket(), protocol.ActionIDSaArenaGetInfo)
				if err != nil {
					return nil, err
				}
				if resp == nil {
					return nil, errors.New("圣域竞技场面板无回包")
				}
				return protocol.ParseSaArenaGetInfoResult(resp.Payload)
			}

			info, err := fetchInfo()
			if err != nil {
				slog.Info("圣域竞技场功能不可用或未开放，跳过", "role_id", session.RoleID(), "err", err)
				return nil
			}
			slog.Info("圣域竞技场面板",
				"role_id", session.RoleID(),
				"seats", len(info.Seats),
				"available_count", info.AvailableCount,
			)
			if info.AvailableCount <= 0 {
				slog.Info("圣域竞技场今日抢占次数已用尽", "role_id", session.RoleID())
				return nil
			}

			robs := 0
			for robs < int(info.AvailableCount) && robs < maxSaArenaRobs {
				robs++
				if err := ctx.Err(); err != nil {
					return nil
				}
				seat, ok := pickSaArenaSeat(info.Seats, session.PlayerID())
				if !ok {
					slog.Info("圣域竞技场没有可抢占的席位", "role_id", session.RoleID())
					return nil
				}
				resp, rErr := call(protocol.BuildSaArenaRobSeatPacket(seat.Seq), protocol.ActionIDSaArenaRobSeat)
				if rErr != nil || resp == nil {
					slog.Info("圣域竞技场抢占无回包", "role_id", session.RoleID(), "seq", seat.Seq, "err", rErr)
					return nil
				}
				res, pErr := protocol.ParseSaArenaRobResult(resp.Payload)
				if pErr != nil {
					slog.Warn("解析圣域竞技场抢占回包失败", "role_id", session.RoleID(), "err", pErr)
					return nil
				}
				switch res.Result {
				case protocol.SaArenaResultSuccess, protocol.SaArenaResultSuccessFree:
					slog.Info("圣域竞技场抢占成功",
						"role_id", session.RoleID(),
						"seq", seat.Seq,
						"target", seat.Nickname,
						"award_score", res.AwardScore,
						"win_player_id", res.WinPlayerID,
					)
				case protocol.SaArenaResultOnSeat:
					slog.Info("圣域竞技场已在席位上", "role_id", session.RoleID())
					return nil
				case protocol.SaArenaResultActivityClose:
					slog.Info("圣域竞技场活动已关闭", "role_id", session.RoleID())
					return nil
				case protocol.SaArenaResultCDTime:
					slog.Info("圣域竞技场抢占冷却中", "role_id", session.RoleID())
					return nil
				case protocol.SaArenaResultCountUsedUp:
					slog.Info("圣域竞技场抢占次数已用尽", "role_id", session.RoleID())
					return nil
				case protocol.SaArenaResultMyself:
					slog.Debug("跳过自己的席位", "role_id", session.RoleID(), "seq", seat.Seq)
				case protocol.SaArenaResultPackFull:
					return scheduler.ErrBagFull
				default:
					slog.Info("圣域竞技场抢占未成功", "role_id", session.RoleID(), "seq", seat.Seq, "result", res.Result)
					return nil
				}

				refreshed, fErr := fetchInfo()
				if fErr != nil {
					slog.Warn("刷新圣域竞技场面板失败", "role_id", session.RoleID(), "err", fErr)
					return nil
				}
				info = refreshed
				select {
				case <-ctx.Done():
					return nil
				case <-time.After(300 * time.Millisecond):
				}
			}
			slog.Info("圣域竞技场巡检完成", "role_id", session.RoleID(), "robs", robs)
			return nil
		},
	)
}

// pickSaArenaSeat 选择一个可抢占的席位：优先空席位，其次战力最低的对手。
func pickSaArenaSeat(seats []protocol.SaArenaSeat, selfPlayerID int32) (protocol.SaArenaSeat, bool) {
	empty := make([]protocol.SaArenaSeat, 0)
	occupied := make([]protocol.SaArenaSeat, 0)
	for _, s := range seats {
		if s.SaPlayerID == selfPlayerID {
			continue
		}
		if s.SaPlayerID == 0 {
			empty = append(empty, s)
			continue
		}
		occupied = append(occupied, s)
	}
	if len(empty) > 0 {
		sort.SliceStable(empty, func(i, j int) bool { return empty[i].Seq < empty[j].Seq })
		return empty[0], true
	}
	if len(occupied) == 0 {
		return protocol.SaArenaSeat{}, false
	}
	sort.SliceStable(occupied, func(i, j int) bool { return occupied[i].WarPower < occupied[j].WarPower })
	return occupied[0], true
}
