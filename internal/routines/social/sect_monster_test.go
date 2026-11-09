package social

import (
	"context"
	"errors"
	"net"
	"testing"

	"sxd-pie-ng/internal/protocol"
	"sxd-pie-ng/internal/scheduler"
)

func TestSectMonsterRoutine_FunctionCheckSilentDropCooldown(t *testing.T) {
	sess := newSocialMockSession(t, func(conn net.Conn, pkt *protocol.Packet) {
		if pkt.ActionID == protocol.ActionIDGameAssistantInfo {
			_ = protocol.WritePacket(conn, protocol.BuildGameAssistantInfoResultPacket([]protocol.DailyActivityTask{
				{TaskID: DailyTaskSectMonster, CurTimes: 0, MaxTimes: 1, PerActiveAdd: 5},
			}))
		}
		// 功能开放列表查询静默丢包。
	})
	defer sess.Close()

	err := NewSectMonsterRoutine().Execute(context.Background(), sess, nil)
	var cd *scheduler.CooldownError
	if !errors.As(err, &cd) {
		t.Fatalf("功能开放状态无回包应返回 CooldownError, 实际: %v", err)
	}
}
