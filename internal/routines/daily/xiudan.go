// Package daily 提供签到、邮件、在线奖励、绑定奖励与日常维护类玩法。
//
// 本文件实现「服用修为丹」(通天道场)：
// 每日活跃度任务 79「每日服用修为丹」每次 +10，上限 15 次。
// 修为丹为背包道具（一~四阶：8308~8311），通过 Mod 2 player_use_grid_item 按格位服用。
package daily

import (
	"context"
	"log/slog"
	"time"

	"sxd-pie-ng/internal/client"
	"sxd-pie-ng/internal/protocol"
	"sxd-pie-ng/internal/scheduler"
)

// DailyTaskXiuDan 每日服用修为丹任务编号。
const DailyTaskXiuDan int32 = 79

// xiuDanItemIDs 一~四阶修为丹道具编号。
var xiuDanItemIDs = map[int32]bool{
	8308: true, // 一阶修为丹
	8309: true, // 二阶修为丹
	8310: true, // 三阶修为丹
	8311: true, // 四阶修为丹
}

// NewXiuDanRoutine 构造服用修为丹任务。
//
// 仅使用背包内已有的修为丹；没有道具时直接跳过，不购买、不消耗元宝。
func NewXiuDanRoutine() scheduler.ActivityRoutine {
	const callTimeout = 8 * time.Second
	return scheduler.NewBaseRoutine(
		"xiudan",
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

			call := func(pkt *protocol.Packet, act uint32) *protocol.Packet {
				cCtx, cancel := context.WithTimeout(ctx, callTimeout)
				defer cancel()
				resp, err := session.Call(cCtx, pkt, act)
				if err != nil {
					slog.Warn("修为丹协议无回包", "role_id", session.RoleID(),
						"action", act, "err", err)
					return nil
				}
				return resp
			}

			actResp := call(protocol.BuildGameAssistantInfoPacket(), protocol.ActionIDGameAssistantInfo)
			if actResp == nil {
				slog.Warn("查询每日活跃度失败，跳过服用修为丹", "role_id", session.RoleID())
				return nil
			}
			panel, err := protocol.ParseGameAssistantInfo(actResp.Payload)
			if err != nil {
				return err
			}
			remain := panel.RemainingTimesFor(DailyTaskXiuDan)
			if remain <= 0 {
				slog.Info("服用修为丹活跃度已完成，跳过", "role_id", session.RoleID())
				return nil
			}

			packResp := call(protocol.BuildGetPackItemListPacket(), protocol.ActionIDItemGetPackList)
			if packResp == nil {
				slog.Info("查询背包失败，跳过服用修为丹", "role_id", session.RoleID())
				return nil
			}
			pack, pErr := protocol.ParsePackItemListResponse(packResp.Payload)
			if pErr != nil || pack == nil {
				slog.Warn("解析背包失败，跳过服用修为丹", "role_id", session.RoleID(), "err", pErr)
				return nil
			}

			used := 0
			for _, it := range pack.PackItems {
				if used >= remain || ctx.Err() != nil {
					break
				}
				if !xiuDanItemIDs[it.ItemID] {
					continue
				}
				usePkt, bErr := protocol.BuildUseGridItemPacket(int16(it.GridID))
				if bErr != nil {
					continue
				}
				resp := call(usePkt, protocol.ActionIDItemUseGrid)
				if resp == nil {
					break
				}
				ok, code, uErr := protocol.ParseUseGridItemResponse(resp.Payload)
				if uErr != nil {
					break
				}
				if !ok {
					slog.Info("服用修为丹未成功",
						"role_id", session.RoleID(), "grid_id", it.GridID,
						"item_id", it.ItemID, "result", code)
					break
				}
				used++
				slog.Info("服用修为丹成功",
					"role_id", session.RoleID(), "grid_id", it.GridID, "item_id", it.ItemID, "used", used)
				select {
				case <-ctx.Done():
					return nil
				case <-time.After(150 * time.Millisecond):
				}
			}
			if used == 0 {
				slog.Info("背包内没有可服用的修为丹", "role_id", session.RoleID())
			}
			return nil
		},
	)
}
