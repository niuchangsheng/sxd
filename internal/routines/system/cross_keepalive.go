// 本文件实现「跨服辅助会话常驻」维护任务。
//
// 2026-11-25 抓包 (/tmp/daily.pcapng) 与探针实测：
//   - 服务端每 5 分钟按墙钟扫描一次（本网关在整点后约 23~29 秒），回收
//     「最近约 120 秒没有真实业务活动」的主服会话；
//   - 官方客户端空闲 10 分钟，主服上行只有每约 100 秒一次 get_server_time，
//     但其 8219/8251/8205/8252 辅助连接始终在线，主服会话不会被回收；
//   - 探针 cross 模式（主服零上行 + 常驻一条 8219 跨服连接）同样跨过两个
//     扫描点存活 10 分钟，纯空闲模式则在第一个扫描点被回收；
//   - 单发 0/75、0/22、enter_town 均不能续期。
//
// 因此本任务在调度空隙维护一条常驻跨服连接。调度器单协程执行，确保跨服
// 凭据请求不会插进其他玩法的 PrevAct 业务序列中间。
package system

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"sxd-pie-ng/internal/client"
	"sxd-pie-ng/internal/scheduler"
)

// crossKeepaliveInterval 跨服连接巡检周期。
// 跨服会话自带心跳与重连，正常情况下每次检查只是读一次状态。
const crossKeepaliveInterval = 60 * time.Second

// NewCrossKeepaliveRoutine 构造跨服辅助会话常驻任务。
func NewCrossKeepaliveRoutine() scheduler.ActivityRoutine {
	return scheduler.NewBaseRoutine(
		"cross_keepalive",
		"system",
		scheduler.ScheduleLoop,
		// 与 session_refresh 同域，略高优先级：登录后尽快建立辅助连接。
		scheduler.PhasePriority(scheduler.PhaseSystem, 10),
		crossKeepaliveInterval,
		func(ctx context.Context, session *client.RoleSession, jitter *scheduler.Jitter) error {
			if session == nil {
				return client.ErrNotConnected
			}
			if session.State() != client.StateActive {
				return client.ErrNotConnected
			}
			if err := session.EnsureCrossSessionAlive(ctx); err != nil {
				if errors.Is(err, client.ErrNotConnected) {
					return err
				}
				slog.Warn("跨服辅助会话不可用，稍后重试",
					"role_id", session.RoleID(), "err", err)
				// 跨服节点临时不可用不应拖垮主服玩法：5 分钟后再试。
				return scheduler.NewCooldown(5*time.Minute, "跨服辅助会话不可用")
			}
			return nil
		},
	)
}