// 本文件实现全服伏魔塔房间流程 (Module 341 / 0x0155) 自动化。
//
// 协议基准: 2026-11-10 官方客户端抓包 (qq s0「同尘」)：
//
//	341/1  入口/面板
//	341/4  创建房间  -> 回包 [UByte result=4 SUCCESS, Int room_id, ...成员快照]
//	341/12 解除封印  -> 回包 [UByte result]；解除后客户端自动进入战斗
//	341/13 进入战斗  -> 回包 [UByte result]；抓包中在解除封印约 5 秒后自动发送
//
// 所有请求载荷仅 4 字节 PrevAct，由会话在发送时填充最新动作号。
// 本实现不调用任何元宝购买类动作；结果码非 4 时一律记录并终止本轮。
//
// 待补齐: 后续楼层“进入 + 手动挑战”的挑战动作尚未抓包，本轮只完成
// 创建房间 → 解除封印 → 进入战斗 的首场流程。
package social

import (
	"context"
	"log/slog"
	"time"

	"sxd-pie-ng/internal/client"
	"sxd-pie-ng/internal/protocol"
	"sxd-pie-ng/internal/scheduler"
)

const towerRoomCallTimeout = 10 * time.Second

// towerRoomMaxBattles 单轮最多连续战斗场次（抓包中为 3 场）。
const towerRoomMaxBattles = 3

// 步骤间隔与进入战斗等待，测试中可缩短。
var (
	towerRoomStepGap  = 2 * time.Second
	towerRoomEnterGap = 5 * time.Second
	// towerRoomNow 便于单测注入固定日期。
	towerRoomNow = time.Now
)

// towerRoomOpenHour 全服伏魔塔开放时段起点（14:00–23:59）。
const towerRoomOpenHour = 14

// towerRoomOpenDay 全服伏魔塔仅在周二、周四、周六开放。
func towerRoomOpenDay(t time.Time) bool {
	switch t.Weekday() {
	case time.Tuesday, time.Thursday, time.Saturday:
		return true
	default:
		return false
	}
}

// towerRoomOpenNow 全服伏魔塔仅在周二/四/六的 14:00–23:59 开放。
func towerRoomOpenNow(t time.Time) bool {
	return towerRoomOpenDay(t) && t.Hour() >= towerRoomOpenHour
}

// towerRoomNextOpenDelay 返回距离下一个开放时段（开放日 14:00）的等待时长。
func towerRoomNextOpenDelay(now time.Time) time.Duration {
	todayOpen := time.Date(now.Year(), now.Month(), now.Day(), towerRoomOpenHour, 0, 0, 0, now.Location())
	if towerRoomOpenDay(todayOpen) && now.Before(todayOpen) {
		return todayOpen.Sub(now)
	}
	next := todayOpen
	for i := 0; i < 8; i++ {
		next = next.AddDate(0, 0, 1)
		if towerRoomOpenDay(next) {
			break
		}
	}
	if d := next.Sub(now); d > 0 {
		return d
	}
	return 24 * time.Hour
}

// NewTowerRoomRoutine 构造全服伏魔塔房间流程任务。
func NewTowerRoomRoutine() scheduler.ActivityRoutine {
	return scheduler.NewBaseRoutine(
		"tower_room",
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
			if session.State() != client.StateActive {
				return nil
			}
			// 全服伏魔塔每周二、四、六开放（用户确认，2026-11-11）。
			if !towerRoomOpenNow(towerRoomNow()) {
				delay := towerRoomNextOpenDelay(towerRoomNow())
				slog.Info("全服伏魔塔仅周二/四/六 14:00-23:59 开放，等待开放时段",
					"role_id", session.RoleID(), "retry_in", delay)
				return scheduler.NewCooldown(delay, "全服伏魔塔非开放日")
			}

			call := func(pkt *protocol.Packet) *protocol.Packet {
				cCtx, cancel := context.WithTimeout(ctx, towerRoomCallTimeout)
				defer cancel()
				resp, err := session.Call(cCtx, pkt, pkt.ActionID)
				if err != nil {
					slog.Info("全服伏魔塔请求无回包", "role_id", session.RoleID(),
						"action", pkt.ActionID, "err", err)
					return nil
				}
				return resp
			}

			// 1. 入口/面板（官方客户端先发 341/1，再创建房间）
			openResp := call(protocol.BuildTowerRoomOpenPacket(session.LastActionID()))
			if openResp != nil {
				slog.Info("全服伏魔塔入口面板", "role_id", session.RoleID(),
					"payload_len", len(openResp.Payload))
			}

			if ctx.Err() != nil {
				return nil
			}

			// 2. 创建房间
			createResp := call(protocol.BuildTowerRoomCreatePacket(session.LastActionID()))
			if createResp == nil {
				return scheduler.NewCooldown(15*time.Minute, "全服伏魔塔创建房间无回包")
			}
			createRes, err := protocol.ParseTowerRoomCreateResult(createResp.Payload)
			if err != nil {
				slog.Warn("解析全服伏魔塔创建房间回包失败", "role_id", session.RoleID(), "err", err)
				return nil
			}
			if createRes.Result != protocol.TowerRoomResultSuccess {
				slog.Info("全服伏魔塔创建房间未成功", "role_id", session.RoleID(),
					"result", createRes.Result,
					"result_text", protocol.TowerRoomResultText(createRes.Result))
				if createRes.Result == protocol.TowerRoomResultNotOpen {
					// 非活动时间（实测 01:44 四个动作均返回 5）：定时重试以覆盖晚间开放时段。
					return scheduler.NewCooldown(time.Hour, "全服伏魔塔未开放")
				}
				return nil
			}
			slog.Info("全服伏魔塔房间创建成功", "role_id", session.RoleID(), "room_id", createRes.RoomID)

			if err := waitGap(ctx, towerRoomStepGap); err != nil {
				return nil
			}

			// 3. 解除封印（成功后服务端自动进入战斗）
			unsealResp := call(protocol.BuildTowerRoomUnsealPacket(session.LastActionID()))
			if unsealResp == nil {
				return scheduler.NewCooldown(15*time.Minute, "全服伏魔塔解除封印无回包")
			}
			if code, ok := firstResult(unsealResp); !ok {
				slog.Warn("全服伏魔塔解除封印回包为空", "role_id", session.RoleID())
				return nil
			} else if code != protocol.TowerRoomResultSuccess {
				slog.Info("全服伏魔塔解除封印未成功", "role_id", session.RoleID(),
					"result", code, "result_text", protocol.TowerRoomResultText(code))
				return nil
			}
			slog.Info("全服伏魔塔解除封印成功", "role_id", session.RoleID())

			if err := waitGap(ctx, towerRoomEnterGap); err != nil {
				return nil
			}

			// 4. 进入战斗（客户端在解除封印后自动发送）
			enterResp := call(protocol.BuildTowerRoomNextPacket(session.LastActionID()))
			if enterResp == nil {
				return scheduler.NewCooldown(15*time.Minute, "全服伏魔塔进入战斗无回包")
			}
			if code, ok := firstResult(enterResp); !ok {
				slog.Warn("全服伏魔塔进入战斗回包为空", "role_id", session.RoleID())
				return nil
			} else if code != protocol.TowerRoomResultSuccess {
				slog.Info("全服伏魔塔进入战斗未成功", "role_id", session.RoleID(),
					"result", code, "result_text", protocol.TowerRoomResultText(code))
				return nil
			}
			slog.Info("全服伏魔塔首场战斗已进入",
				"role_id", session.RoleID(), "room_id", createRes.RoomID)

			// 5. 战斗循环：341/15 开始战斗 -> 服务端推送 341/14 战报与
			// 341/17 结果 -> 341/13 进入下一场。抓包中单轮连续 3 场。
			battles := 0
			for battles < towerRoomMaxBattles {
				if ctx.Err() != nil {
					return nil
				}
				startResp := call(protocol.BuildTowerRoomStartBattlePacket(session.LastActionID()))
				if startResp == nil {
					return scheduler.NewCooldown(15*time.Minute, "全服伏魔塔开始战斗无回包")
				}
				if code, ok := firstResult(startResp); !ok {
					slog.Warn("全服伏魔塔开始战斗回包为空", "role_id", session.RoleID())
					return nil
				} else if code != protocol.TowerRoomResultSuccess {
					slog.Info("全服伏魔塔开始战斗未成功", "role_id", session.RoleID(),
						"result", code, "result_text", protocol.TowerRoomResultText(code))
					return nil
				}
				battles++
				slog.Info("全服伏魔塔战斗已开始", "role_id", session.RoleID(), "battles", battles)

				if err := waitGap(ctx, towerRoomStepGap); err != nil {
					return nil
				}
				if battles >= towerRoomMaxBattles {
					break
				}
				nextResp := call(protocol.BuildTowerRoomNextPacket(session.LastActionID()))
				if nextResp == nil {
					return scheduler.NewCooldown(15*time.Minute, "全服伏魔塔进入下一场无回包")
				}
				if code, ok := firstResult(nextResp); !ok {
					slog.Warn("全服伏魔塔进入下一场回包为空", "role_id", session.RoleID())
					return nil
				} else if code != protocol.TowerRoomResultSuccess {
					slog.Info("全服伏魔塔进入下一场未成功，结束本轮",
						"role_id", session.RoleID(), "result", code,
						"result_text", protocol.TowerRoomResultText(code))
					break
				}
			}
			slog.Info("全服伏魔塔本轮战斗完成",
				"role_id", session.RoleID(), "room_id", createRes.RoomID, "battles", battles)
			return nil
		},
	)
}

func firstResult(resp *protocol.Packet) (uint8, bool) {
	if resp == nil || len(resp.Payload) == 0 {
		return 0, false
	}
	return resp.Payload[0], true
}

func waitGap(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return nil
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(d):
		return nil
	}
}
