// 本文件实现飞升页面「伙伴灌注经验」(Mod_Levitate_Base, Module 357)。
//
// 抓包基准: /tmp/feisheng.pcapng（2026-11-15，qq s0）：
//
//	357/0 get_levitate_info [UByte type]
//	357/1 perfusion         [UByte type] → [UByte result, [Int,Int]]
//
// 伙伴 type=1（官方 ROLE 页签），主角 type=0（MAINROLE，默认页签）。结果码 2=成功继续、3=已灌注完、4=功能未开放、5/7/8/9/10 等=停止。
// 不消耗元宝。
package daily

import (
	"context"
	"log/slog"
	"time"

	"sxd-pie-ng/internal/client"
	"sxd-pie-ng/internal/protocol"
	"sxd-pie-ng/internal/scheduler"
)

// maxLevitatePerfusions 单轮最多灌注次数。
const maxLevitatePerfusions = 20

// NewLevitatePerfusionRoutine 构造伙伴灌注经验任务。
func NewLevitatePerfusionRoutine() scheduler.ActivityRoutine {
	const callTimeout = 8 * time.Second
	return scheduler.NewBaseRoutine(
		"levitate_perfusion",
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
				resp, err := session.CallWithPrevActResync(ctx, callTimeout, pkt, act)
				if err != nil {
					slog.Info("飞升灌注协议无回包", "role_id", session.RoleID(),
						"action", act, "err", err)
					return nil
				}
				return resp
			}

			infoResp := call(protocol.BuildLevitateInfoPacket(protocol.LevitateTypeRole),
				protocol.ActionIDLevitateInfo)
			if infoResp == nil {
				// 伙伴页签的 357/0 会被服务端静默丢弃（2026-11-17 实测），
				// 而官方默认页签是主角 (MAINROLE=0)。面板本身只作为存活探针
				// （载荷未解析），因此改用主角页签再探一次，再把结果用于决定是否继续灌注。
				slog.Info("飞升信息（伙伴页签）无回包，改试主角页签", "role_id", session.RoleID())
				infoResp = call(protocol.BuildLevitateInfoPacket(protocol.LevitateTypeMainRole),
					protocol.ActionIDLevitateInfo)
			}
			if infoResp == nil {
				// 伙伴灌注达到当日/阶段上限后服务端会静默丢弃 357/0；
				// 连续 3 次挂起到次日重置，避免每 30 分钟空等。
				return scheduler.HandleSilentDrop(session, "levitate_perfusion", 3,
					30*time.Minute, "飞升信息无回包")
			}
			scheduler.ResetSilentDrops(session, "levitate_perfusion")

			perfused := 0
			for perfused < maxLevitatePerfusions && ctx.Err() == nil {
				resp := call(protocol.BuildLevitatePerfusionPacket(protocol.LevitateTypeRole),
					protocol.ActionIDLevitatePerfusion)
				if resp == nil {
					return scheduler.HandleSilentDrop(session, "levitate_perfusion", 3,
						30*time.Minute, "飞升灌注无回包")
				}
				scheduler.ResetSilentDrops(session, "levitate_perfusion")
				res, err := protocol.ParseLevitatePerfusionResult(resp.Payload)
				if err != nil {
					return scheduler.NewCooldown(10*time.Minute, "飞升灌注回包解析失败")
				}
				switch res.Result {
				case protocol.LevitateSuccess:
					perfused++
					slog.Info("飞升伙伴灌注经验成功", "role_id", session.RoleID(),
						"count", perfused, "gains", res.Gains)
				case protocol.LevitateUsedUp:
					slog.Info("飞升伙伴经验已灌注完", "role_id", session.RoleID(), "count", perfused)
					return nil
				case protocol.LevitateFuncNoOpen:
					slog.Info("飞升功能未开放，稍后重试", "role_id", session.RoleID())
					return scheduler.NewCooldown(2*time.Hour, "飞升功能未开放")
				default:
					slog.Info("飞升伙伴灌注结束", "role_id", session.RoleID(),
						"count", perfused, "result", res.Result,
						"result_text", protocol.LevitateResultText(res.Result))
					return nil
				}
			}
			slog.Info("飞升伙伴灌注本轮完成", "role_id", session.RoleID(), "count", perfused)
			return nil
		},
	)
}
