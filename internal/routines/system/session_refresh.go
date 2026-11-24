// 本文件实现「定期重建会话上下文」维护任务。
//
// 背景（2026-11-22 实测）：
//   - 同一账号登录后 0-8 分钟内，所有业务请求正常回包；
//   - 会话持续约 8 分钟后，即使 PrevAct 完全正确，成批业务请求也会被服务端
//     静默丢弃；重新登录后立即恢复。
//
// 该任务以固定间隔在调度空隙中主动断开并重连，把长会话切成多个「健康窗口」。
// 调度器单协程顺序执行，因此不会打断正在进行的玩法；重连期间其他任务会收到
// ErrNotConnected 并按既有 5 分钟策略重试。
package system

import (
	"context"
	"log/slog"
	"time"

	"sxd-pie-ng/internal/client"
	"sxd-pie-ng/internal/scheduler"
)

// sessionRefreshInterval 会话重建检查周期。
// 每次检查仅在连接存活超过 minSessionAge 时才真正重连。
//
// 检查必须足够密：服务端在会话年龄约 4.4~4.7 分钟时回收连接，30 秒检查
// 可确保在 3.5~3.7 分钟触发重建，留出约 1 分钟余量。
const sessionRefreshInterval = 30 * time.Second

// minSessionAge 触发主动重建的最小连接存活时长。
//
// 2026-11-24 实测服务端在会话年龄约 4.4~4.7 分钟时回收连接；取 3.5 分钟
// 触发重建，留出约 1 分钟安全余量。刚被外部顶号重连的会话不会被再次重建。
var minSessionAge = 210 * time.Second

// sessionRefreshTimeout 等待重连重新激活的上限。
const sessionRefreshTimeout = 90 * time.Second

// NewSessionRefreshRoutine 构造会话上下文刷新任务。
func NewSessionRefreshRoutine() scheduler.ActivityRoutine {
	return scheduler.NewBaseRoutine(
		"session_refresh",
		"system",
		scheduler.ScheduleLoop,
		// 高于推图/宝库：到期后优先在下一个调度空隙执行。
		scheduler.PhasePriority(scheduler.PhaseSystem, 0),
		sessionRefreshInterval,
		func(ctx context.Context, session *client.RoleSession, jitter *scheduler.Jitter) error {
			if session == nil {
				return client.ErrNotConnected
			}
			if session.State() != client.StateActive {
				// 已在重连中：交给会话状态机，不重复触发。
				return nil
			}
			if since := session.ConnectionSince(); !since.IsZero() &&
				time.Since(since) < minSessionAge {
				// 会话仍处在登录后的健康窗口，等待下一次检查。
				return nil
			}
			if jitter != nil {
				_ = jitter.Wait(ctx)
			}
			slog.Info("定期刷新会话上下文，规避长会话静默丢包",
				"role_id", session.RoleID(),
				"age", time.Since(session.ConnectionSince()).Round(time.Second),
				"last_business", session.LastBusinessActionID(),
				"last_rx", session.LastRxActionID())
			// 统一走 EnsureFreshConnection：重建前先换票（网关票据一次性），
			// 再等待新一代连接 Active；实测恢复时间由约 75 秒降到约 3 秒。
			if err := session.EnsureFreshConnection(ctx, 0); err != nil {
				slog.Warn("会话上下文刷新未完成，交给调度器稍后重试",
					"role_id", session.RoleID(), "state", session.State(), "err", err)
				return err
			}
			slog.Info("会话上下文刷新完成", "role_id", session.RoleID(),
				"generation", session.ConnectionGeneration())
			return nil
		},
	)
}
