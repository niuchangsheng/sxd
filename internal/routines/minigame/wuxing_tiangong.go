// 本文件实现五行天宫 (Module 453 / 0x01C5) 自动化。
//
// 抓包流程 (/tmp/wuxingtiangong.pcapng):
//
//	453/10 分类面板 → 453/0 主面板 → 453/3 mode=1 一键挑战 → 453/0 刷新
//
// 用户要求（2026-11-14）：不做阵容/role_id 映射；一键挑战后只要存在失败关卡
// 或无回包，直接退出本轮，不再单独重试。
//
// 全程不消耗元宝。
package minigame

import (
	"context"
	"log/slog"
	"time"

	"sxd-pie-ng/internal/client"
	"sxd-pie-ng/internal/protocol"
	"sxd-pie-ng/internal/scheduler"
)

const wuxingCallTimeout = 15 * time.Second

// wuxingStateFailed 面板中失败/待重试状态（抓包中挑战后由 4 变为 3）。
const wuxingStateFailed uint8 = 3

// NewWuxingTiangongRoutine 构造五行天宫一键挑战任务。
func NewWuxingTiangongRoutine() scheduler.ActivityRoutine {
	return scheduler.NewBaseRoutine(
		"wuxing_tiangong",
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

			call := func(pkt *protocol.Packet, act uint32, timeout time.Duration) *protocol.Packet {
				cCtx, cancel := context.WithTimeout(ctx, timeout)
				defer cancel()
				resp, err := session.Call(cCtx, pkt, act)
				if err != nil {
					return nil
				}
				return resp
			}

			slog.Info("正在执行五行天宫挑战", "role_id", session.RoleID())

			// 1. 分类面板（诊断）。
			if resp := call(protocol.BuildWuxingPanel10Packet(session.LastActionID()),
				protocol.ActionIDWuxingPanel10, wuxingCallTimeout); resp != nil {
				slog.Info("五行天宫分类面板", "role_id", session.RoleID(), "payload_len", len(resp.Payload))
			}

			// 2. 主面板。
			panelResp := call(protocol.BuildWuxingPanelPacket(session.LastActionID()),
				protocol.ActionIDWuxingPanel, wuxingCallTimeout)
			if panelResp == nil {
				slog.Info("五行天宫主面板无回包，结束本轮", "role_id", session.RoleID())
				return nil
			}
			entries, pErr := protocol.ParseWuxingPanel(panelResp.Payload)
			if pErr != nil {
				slog.Warn("五行天宫面板解析失败，结束本轮", "role_id", session.RoleID(), "err", pErr)
				return nil
			}
			slog.Info("五行天宫面板状态",
				"role_id", session.RoleID(), "phase", "挑战前", "entries", entries)

			// 3. 一键挑战；无回包直接退出（不做单独重试）。
			fightResp := call(protocol.BuildWuxingFightPacket(protocol.WuxingFightModeAll, session.LastActionID()),
				protocol.ActionIDWuxingFight, 30*time.Second)
			if fightResp == nil {
				slog.Info("五行天宫一键挑战无回包，结束本轮", "role_id", session.RoleID())
				return nil
			}
			slog.Info("五行天宫一键挑战已执行", "role_id", session.RoleID(), "report_len", len(fightResp.Payload))

			// 4. 刷新面板；存在失败关卡即退出。
			afterResp := call(protocol.BuildWuxingPanelPacket(session.LastActionID()),
				protocol.ActionIDWuxingPanel, wuxingCallTimeout)
			if afterResp == nil {
				slog.Info("五行天宫挑战后面板无回包，结束本轮", "role_id", session.RoleID())
				return nil
			}
			after, aErr := protocol.ParseWuxingPanel(afterResp.Payload)
			if aErr != nil {
				slog.Warn("五行天宫挑战后面板解析失败，结束本轮", "role_id", session.RoleID(), "err", aErr)
				return nil
			}
			slog.Info("五行天宫面板状态",
				"role_id", session.RoleID(), "phase", "挑战后", "entries", after)

			for _, e := range after {
				if e.State == wuxingStateFailed {
					slog.Info("五行天宫存在失败关卡，按用户要求结束本轮",
						"role_id", session.RoleID(), "element", e.Element, "stage", e.Stage)
					return nil
				}
			}
			slog.Info("五行天宫本轮挑战完成", "role_id", session.RoleID())
			return nil
		},
	)
}
