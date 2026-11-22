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

// sessionRefreshInterval 会话重建周期。
//
// 线上静默丢弃约在登录后 8 分钟出现；取 5 分钟留出安全余量。
const sessionRefreshInterval = 5 * time.Minute

// sessionRefreshTimeout 等待重连重新激活的上限。
const sessionRefreshTimeout = 90 * time.Second

// NewSessionRefreshRoutine 构造会话上下文刷新任务。
func NewSessionRefreshRoutine() scheduler.ActivityRoutine {
	// 注册后调度器会立即执行一次；此时会话刚登录，属于健康窗口，
	// 跳过首次执行，从下一个 5 分钟周期开始真正重建。
	firstRun := true
	return scheduler.NewBaseRoutine(
		"session_refresh",
		"system",
		scheduler.ScheduleLoop,
		// 高于推图/宝库：到期后优先在下一个调度空隙执行。
		scheduler.RoutinePriority(500),
		sessionRefreshInterval,
		func(ctx context.Context, session *client.RoleSession, jitter *scheduler.Jitter) error {
			if session == nil {
				return client.ErrNotConnected
			}
			if firstRun {
				firstRun = false
				return nil
			}
			if session.State() != client.StateActive {
				// 已在重连中：交给会话状态机，不重复触发。
				return nil
			}
			if jitter != nil {
				_ = jitter.Wait(ctx)
			}
			slog.Info("定期刷新会话上下文，规避长会话静默丢包",
				"role_id", session.RoleID(),
				"last_business", session.LastBusinessActionID(),
				"last_rx", session.LastRxActionID())
			gen := session.ConnectionGeneration()
			session.ReconnectNow()

			deadline := time.Now().Add(sessionRefreshTimeout)
			for {
				// 必须等到「新一代连接」建立（仅看 State 会命中关闭前的瞬时 Active）。
				if session.ConnectionGeneration() > gen && session.State() == client.StateActive {
					slog.Info("会话上下文刷新完成", "role_id", session.RoleID(),
						"generation", session.ConnectionGeneration())
					return nil
				}
				if session.State() == client.StateClosed {
					return client.ErrNotConnected
				}
				if time.Now().After(deadline) {
					slog.Warn("会话上下文刷新超时，交给调度器稍后重试",
						"role_id", session.RoleID(), "state", session.State())
					return client.ErrNotConnected
				}
				select {
				case <-ctx.Done():
					return ctx.Err()
				case <-time.After(200 * time.Millisecond):
				}
			}
		},
	)
}
