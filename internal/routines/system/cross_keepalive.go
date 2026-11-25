// 本文件实现「跨服辅助常驻保活」维护任务。
//
// 2026-11-25 抓包 (/tmp/daily.pcapng, 12m32s 0 断连) + 原版客户端逆向 + 线上控制变量探针
// (test/probes/probe_isolate_keepalive_live.go) 实测结论：
//   - 服务端网关每 5 分钟按墙钟（如 :00:33 / :05:33 / :10:33）扫描回收无跨服辅助连接且
//     建连满约 120 秒的主服会话；纯主服发送 0/75、0/22、1/0 enter_town 乃至每 60 秒
//     发送 0/6 (get_player_function) 均无法阻止被踢 EOF（实测 14:40:14 发 0/6 成功后 19 秒
//     仍在 14:40:33 被踢）；
//   - 原版浏览器客户端在登录主城后立即调用 View.as:16639 chatConnect()，建立一条常驻
//     SOCKETSERVERTOWN (8219/8480) 连接，仅执行 94/0 Mod_StLogin_Base.login 与
//     6/12 Mod_Chat_Base.enter_st_channel，**不调用** 95/0 Mod_StTown_Base.enter_town；
//   - 该 login-only 常驻跨服连接既使主服持续免于 5 分钟扫描回收（实测 s0+s1 连续 10m33s
//     跨过多个扫描点 0 断连），又不会把角色切入仙界城镇场景污染主服上下文；当临时仙界玩法
//     需要进城时由 OpenCrossServerSessionWithInfo 自动挂起并在 95/2 leave_town 后恢复。
package system

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"sxd-pie-ng/internal/client"
	"sxd-pie-ng/internal/scheduler"
)

// crossKeepaliveInterval 常驻跨服辅助连接巡检周期。
const crossKeepaliveInterval = 60 * time.Second

// NewCrossKeepaliveRoutine 构造跨服辅助常驻保活任务。
func NewCrossKeepaliveRoutine() scheduler.ActivityRoutine {
	return scheduler.NewBaseRoutine(
		"cross_keepalive",
		"system",
		scheduler.ScheduleLoop,
		// 与 session_refresh 同域，高优先级：登录后第一时间建立常驻跨服辅助连接。
		scheduler.PhasePriority(scheduler.PhaseSystem, 10),
		crossKeepaliveInterval,
		func(ctx context.Context, session *client.RoleSession, jitter *scheduler.Jitter) error {
			if session == nil || session.State() != client.StateActive {
				return client.ErrNotConnected
			}
			if err := session.EnsureCrossSessionAlive(ctx); err != nil {
				if errors.Is(err, client.ErrNotConnected) {
					return err
				}
				slog.Warn("常驻跨服辅助会话保活失败，稍后重试",
					"role_id", session.RoleID(), "err", err)
				return scheduler.NewCooldown(30*time.Second, "跨服辅助会话保活失败")
			}
			return nil
		},
	)
}
