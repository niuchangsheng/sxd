// 本文件实现五行天宫 (Module 453 / 0x01C5) 自动化。
//
// 抓包流程 (/tmp/wuxingtiangong.pcapng):
//
//	453/10 分类面板 → 453/0 主面板 → 453/3 mode=1 一键挑战
//	→ 453/0 刷新 → 对失败关卡 453/1 阵容 → 453/2 上阵特殊号位 → 453/3 mode=3 单独挑战
//
// 用户提供的特殊关卡规则：
//   - 袖手旁观：主角 一号位(pos 0) + 李逍遥 7 号位(pos 7)
//   - 镜像：玲珑 8 号位(pos 8) + 主角 9 号位(pos 9)
//
// 全程不消耗元宝；结果以面板状态为准，失败关卡最多重试 wuxingMaxRetries 次。
package minigame

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"sxd-pie-ng/internal/client"
	"sxd-pie-ng/internal/protocol"
	"sxd-pie-ng/internal/scheduler"
)

const wuxingCallTimeout = 15 * time.Second

// wuxingMaxRetries 单轮最多单独重试的失败关卡数（抓包中为 2）。
const wuxingMaxRetries = 2

// wuxingStateFailed 面板中失败/待重试状态（抓包中挑战后由 4 变为 3）。
const wuxingStateFailed uint8 = 3

// NewWuxingTiangongRoutine 构造五行天宫自动挑战任务。
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
				slog.Info("五行天宫主面板无回包，稍后重试", "role_id", session.RoleID())
				return scheduler.NewCooldown(30*time.Minute, "五行天宫面板无回包")
			}
			entries, pErr := protocol.ParseWuxingPanel(panelResp.Payload)
			if pErr != nil {
				slog.Warn("五行天宫面板解析失败", "role_id", session.RoleID(), "err", pErr,
					"payload_hex", respHex(panelResp.Payload))
				return nil
			}
			logWuxingEntries(session, "挑战前", entries)

			// 3. 一键挑战。
			fightResp := call(protocol.BuildWuxingFightPacket(protocol.WuxingFightModeAll, session.LastActionID()),
				protocol.ActionIDWuxingFight, 30*time.Second)
			if fightResp == nil {
				// 一键挑战在“已有失败关卡待单独重试”的状态下会被服务端静默丢弃；
				// 不熔断，继续走失败关卡的特殊阵容重试流程。
				slog.Info("五行天宫一键挑战无回包，改为逐关重试", "role_id", session.RoleID())
			} else {
				slog.Info("五行天宫一键挑战已执行", "role_id", session.RoleID(), "report_len", len(fightResp.Payload))
			}

			// 4. 刷新面板，定位失败关卡。
			afterResp := call(protocol.BuildWuxingPanelPacket(session.LastActionID()),
				protocol.ActionIDWuxingPanel, wuxingCallTimeout)
			if afterResp == nil {
				return nil
			}
			after, aErr := protocol.ParseWuxingPanel(afterResp.Payload)
			if aErr != nil {
				slog.Warn("五行天宫挑战后面板解析失败", "role_id", session.RoleID(), "err", aErr)
				return nil
			}
			logWuxingEntries(session, "挑战后", after)

			failed := make([]protocol.WuxingEntry, 0, 1)
			for _, e := range after {
				if e.State == wuxingStateFailed {
					failed = append(failed, e)
				}
			}
			if len(failed) == 0 {
				slog.Info("五行天宫本轮无失败关卡", "role_id", session.RoleID())
				return nil
			}

			// 5. 失败关卡按特殊阵容单独挑战。
			lineupResp := call(protocol.BuildWuxingLineupPacket(session.LastActionID()),
				protocol.ActionIDWuxingLineup, wuxingCallTimeout)
			var roles []protocol.WuxingRole
			if lineupResp != nil {
				roles, _ = protocol.ParseWuxingLineup(lineupResp.Payload)
				slog.Info("五行天宫阵容", "role_id", session.RoleID(), "roles", len(roles))
			}
			mainRoleID, liXiaoyaoID, linglongID := matchWuxingRoles(session, roles)

			for i := 0; i < len(failed) && i < wuxingMaxRetries; i++ {
				if ctx.Err() != nil {
					return nil
				}
				// 抓包中两次失败分别使用「镜像」与「袖手旁观」阵容；这里交替尝试。
				if i%2 == 0 {
					deployWuxingRole(session, linglongID, 8, ctx)
					deployWuxingRole(session, mainRoleID, 9, ctx)
				} else {
					deployWuxingRole(session, mainRoleID, 0, ctx)
					deployWuxingRole(session, liXiaoyaoID, 7, ctx)
				}
				retryResp := call(protocol.BuildWuxingFightPacket(protocol.WuxingFightModeRetry, session.LastActionID()),
					protocol.ActionIDWuxingFight, 30*time.Second)
				if retryResp == nil {
					slog.Info("五行天宫失败关卡重试无回包", "role_id", session.RoleID(),
						"element", failed[i].Element, "stage", failed[i].Stage)
					return scheduler.NewCooldown(30*time.Minute, "五行天宫重试无回包")
				}
				slog.Info("五行天宫失败关卡已重试", "role_id", session.RoleID(),
					"element", failed[i].Element, "stage", failed[i].Stage,
					"report_len", len(retryResp.Payload))
			}

			// 6. 最终状态。
			if finalResp := call(protocol.BuildWuxingPanelPacket(session.LastActionID()),
				protocol.ActionIDWuxingPanel, wuxingCallTimeout); finalResp != nil {
				if final, fErr := protocol.ParseWuxingPanel(finalResp.Payload); fErr == nil {
					logWuxingEntries(session, "重试后", final)
				}
			}
			return nil
		},
	)
}

// deployWuxingRole 发送 453/2 上阵；roleID<=0 或会话不可用时跳过。
func deployWuxingRole(session *client.RoleSession, roleID int32, pos uint8, ctx context.Context) {
	if roleID <= 0 || session == nil {
		return
	}
	pkt := protocol.BuildWuxingDeployPacket(roleID, pos, session.LastActionID())
	cCtx, cancel := context.WithTimeout(ctx, wuxingCallTimeout)
	defer cancel()
	resp, err := session.Call(cCtx, pkt, protocol.ActionIDWuxingDeploy)
	if err != nil || resp == nil {
		slog.Info("五行天宫上阵无回包", "role_id", session.RoleID(), "role_id_target", roleID, "pos", pos, "err", err)
		return
	}
	if len(resp.Payload) > 0 && resp.Payload[0] != 0 {
		slog.Info("五行天宫上阵未成功", "role_id", session.RoleID(), "role_id_target", roleID, "pos", pos, "result", resp.Payload[0])
		return
	}
	slog.Info("五行天宫上阵成功", "role_id", session.RoleID(), "role_id_target", roleID, "pos", pos)
}

// matchWuxingRoles 从阵容列表按名称匹配主角/李逍遥/玲珑。
func matchWuxingRoles(session *client.RoleSession, roles []protocol.WuxingRole) (main, liXiaoyao, linglong int32) {
	name := ""
	if session != nil {
		name = session.RoleName()
	}
	for _, r := range roles {
		switch {
		case name != "" && strings.Contains(r.Name, name):
			main = r.RoleID
		case strings.Contains(r.Name, "李逍遥"):
			liXiaoyao = r.RoleID
		case strings.Contains(r.Name, "玲珑"):
			linglong = r.RoleID
		}
	}
	// 主角未按名字命中时，取第一个非李逍遥/玲珑的条目兜底。
	if main == 0 {
		for _, r := range roles {
			if !strings.Contains(r.Name, "李逍遥") && !strings.Contains(r.Name, "玲珑") {
				main = r.RoleID
				break
			}
		}
	}
	return main, liXiaoyao, linglong
}

func logWuxingEntries(session *client.RoleSession, phase string, entries []protocol.WuxingEntry) {
	slog.Info("五行天宫面板状态",
		"role_id", session.RoleID(), "phase", phase, "entries", entries)
}

func respHex(p []byte) string {
	const max = 48
	if len(p) > max {
		p = p[:max]
	}
	return strings.ToLower(hexEncode(p))
}

func hexEncode(p []byte) string {
	const digits = "0123456789abcdef"
	out := make([]byte, 0, len(p)*2)
	for _, b := range p {
		out = append(out, digits[b>>4], digits[b&0xf])
	}
	return string(out)
}
