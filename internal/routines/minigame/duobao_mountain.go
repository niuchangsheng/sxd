// Package minigame 提供吉星高照、仙旅奇缘答题、喜从天降、刮刮卡、钓鱼等益智趣味活动。
//
// 本文件实现「多宝山」(Mod_MysticalTreasure_Base, Module 740) 自动化。
package minigame

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"sxd-pie-ng/internal/client"
	"sxd-pie-ng/internal/protocol"
	"sxd-pie-ng/internal/scheduler"
)

// maxMysticalTreasureDigs 单轮调度的最大免费挖掘次数。
const maxMysticalTreasureDigs = 40

// NewDuobaoMountainRoutine 构造每日多宝山挖掘任务。
//
// 官方流程 (com.views.mystical_treasure.MysticalTreasureView)：
//  1. get_panel_info 读取剩余免费挖掘次数 dig_num；
//  2. dig_treasure 使用一次免费挖掘次数开启一格；
//  3. 刷新面板继续，直到 dig_num 归零或服务端返回不可领取。
//
// 多宝山的元宝寻宝与高级寻宝入口不会被调用，自动化永不消耗元宝。
func NewDuobaoMountainRoutine() scheduler.ActivityRoutine {
	return scheduler.NewBaseRoutine(
		"duobao_mountain",
		"minigame",
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
			slog.Info("正在执行益智任务: 多宝山挖掘", "role_id", session.RoleID())

			call := func(pkt *protocol.Packet, act uint32) (*protocol.Packet, error) {
				cCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
				defer cancel()
				return session.Call(cCtx, pkt, act)
			}

			fetchPanel := func() (*protocol.MysticalTreasurePanel, error) {
				resp, err := call(protocol.BuildMysticalTreasurePanelPacket(), protocol.ActionIDMysticalTreasurePanel)
				if err != nil {
					return nil, err
				}
				if resp == nil {
					return nil, errors.New("多宝山面板无回包")
				}
				return protocol.ParseMysticalTreasurePanelResult(resp.Payload)
			}

			panel, err := fetchPanel()
			if err != nil {
				slog.Info("多宝山功能不可用或未开放，跳过", "role_id", session.RoleID(), "err", err)
				return nil
			}
			slog.Info("多宝山面板",
				"role_id", session.RoleID(),
				"activity", panel.Activity,
				"dig_num", panel.DigNum,
				"open_grids", len(panel.OpenGrids),
				"big_awards", len(panel.BigAwardIDs),
			)
			if panel.DigNum <= 0 {
				slog.Info("多宝山今日免费挖掘次数已用尽", "role_id", session.RoleID())
				return nil
			}

			digs := 0
			for digs < int(panel.DigNum) && digs < maxMysticalTreasureDigs {
				digs++
				if err := ctx.Err(); err != nil {
					return nil
				}
				resp, dErr := call(protocol.BuildMysticalTreasureDigPacket(), protocol.ActionIDMysticalTreasureDig)
				if dErr != nil || resp == nil {
					slog.Warn("多宝山挖掘无回包", "role_id", session.RoleID(), "err", dErr)
					return nil
				}
				res, pErr := protocol.ParseMysticalTreasureDigResult(resp.Payload)
				if pErr != nil {
					slog.Warn("解析多宝山挖掘回包失败", "role_id", session.RoleID(), "err", pErr)
					return nil
				}
				switch res.Result {
				case protocol.MysticalTreasureResultSuccess:
					slog.Info("多宝山挖掘成功",
						"role_id", session.RoleID(),
						"open_grid_id", res.OpenGridID,
						"award_id", res.AwardID,
						"big_award_ids", res.BigAwardIDs,
					)
				case protocol.MysticalTreasureResultPackageFull, protocol.MysticalTreasureResultFatePackageFull:
					return scheduler.ErrBagFull
				case protocol.MysticalTreasureResultCannotGet, protocol.MysticalTreasureResultNotEnoughTime:
					slog.Info("多宝山暂不可继续挖掘", "role_id", session.RoleID(), "result", res.Result)
					return nil
				case protocol.MysticalTreasureResultFuncNotOpen:
					slog.Info("多宝山功能未开放", "role_id", session.RoleID())
					return nil
				default:
					slog.Info("多宝山挖掘未成功", "role_id", session.RoleID(), "result", res.Result)
					return nil
				}
				select {
				case <-ctx.Done():
					return nil
				case <-time.After(200 * time.Millisecond):
				}
			}
			slog.Info("多宝山巡检完成", "role_id", session.RoleID(), "digs", digs)
			return nil
		},
	)
}
