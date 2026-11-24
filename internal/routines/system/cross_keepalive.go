// 本文件实现「主服会话保活」维护任务。
//
// 2026-11-25 抓包 (/tmp/daily.pcapng) 与探针实测：
//   - 服务端每 5 分钟按墙钟扫描一次，回收「最近约 120 秒没有真实业务活动」
//     的主服会话；单发 0/75、0/22、enter_town 均不能续期；
//   - 常驻跨服连接可以续期，但若该连接进入仙界城镇，主服返回城镇/写请求
//     会被静默丢弃（2026-11-25 01:41 实机：五行 ReturnToTown 失败、
//     灵域猎妖/五行 453/3 连续无回包）；
//   - 不建立跨服连接、仅每 90 秒发送一次 0/6 功能列表查询，主服会话可
//     跨过扫描点并保持 6.5 分钟在线（探针 probe_main_keepalive_live）。
//
// 因此本任务在调度空隙发送一次无害的主服业务读，既刷新活动又不会让角色
// 停留在跨服地图。调度器单协程执行，不会插进其他玩法的 PrevAct 序列中间。
package system

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"sxd-pie-ng/internal/client"
	"sxd-pie-ng/internal/scheduler"
)

// mainKeepaliveInterval 主服保活读周期。
// 服务端按约 120 秒活动窗口扫描，90 秒留出安全余量。
const mainKeepaliveInterval = 90 * time.Second

// NewCrossKeepaliveRoutine 构造主服会话保活任务（保留历史 Routine ID）。
func NewCrossKeepaliveRoutine() scheduler.ActivityRoutine {
	return scheduler.NewBaseRoutine(
		"cross_keepalive",
		"system",
		scheduler.ScheduleLoop,
		// 与 session_refresh 同域，略高优先级：登录后尽快启动保活。
		scheduler.PhasePriority(scheduler.PhaseSystem, 10),
		mainKeepaliveInterval,
		func(ctx context.Context, session *client.RoleSession, jitter *scheduler.Jitter) error {
			if session == nil || session.State() != client.StateActive {
				return client.ErrNotConnected
			}
			if err := session.KeepaliveBusinessRead(ctx); err != nil {
				if errors.Is(err, client.ErrNotConnected) {
					return err
				}
				slog.Warn("主服会话保活读无回包，稍后重试",
					"role_id", session.RoleID(), "err", err)
				return scheduler.NewCooldown(30*time.Second, "会话保活读无回包")
			}
			return nil
		},
	)
}
