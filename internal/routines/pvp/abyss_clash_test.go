package pvp

import (
	"context"
	"errors"
	"net"
	"sync/atomic"
	"testing"
	"time"

	"sxd-pie-ng/internal/client"
	"sxd-pie-ng/internal/protocol"
	"sxd-pie-ng/internal/routines/kit"
	"sxd-pie-ng/internal/scheduler"
)

// stubAbyssClashSession 让跨服会话建立返回主会话本身，便于单测。
func stubAbyssClashSession(t *testing.T) {
	t.Helper()
	prev := openAbyssClashSession
	openAbyssClashSession = func(ctx context.Context, s *client.RoleSession) (*client.RoleSession, error) {
		return s, nil
	}
	t.Cleanup(func() { openAbyssClashSession = prev })
}

// abyssPanel 构造第一阶段面板。
func abyssPanel(fightNum int32, order []int32) *protocol.Packet {
	monsters := make([]protocol.AbyssClashMonster, 0, len(order))
	for _, id := range order {
		monsters = append(monsters, protocol.AbyssClashMonster{
			StepMonsterID: id,
			MonsterTeamID: 12936 + id%10,
			WinNum:        10,
			LastHealth:    100,
			MaxHealth:     200,
		})
	}
	return protocol.BuildAbyssClashStep1PanelResultPacket(&protocol.AbyssClashPanel{
		Result: protocol.AbyssClashResultSuccess, Step: 1,
		Score: 3000, FightNum: fightNum, CostPower: 425,
		Monsters: monsters, ZoneID: 6, STTeamID: 1, TotalScore: 3000,
	})
}

func TestAbyssClashRoutine_FightsAllBossesUntilUsedUp(t *testing.T) {
	stubAbyssClashSession(t)
	var panelCalls, infoCalls, fightCalls atomic.Int32
	var fought []int32
	sess := newPvPMockSession(t, func(conn net.Conn, pkt *protocol.Packet) {
		switch pkt.ActionID {
		case protocol.ActionIDAbyssClashFuncOpen:
			_ = protocol.WritePacket(conn, protocol.BuildAbyssClashFuncOpenResultPacket(
				&protocol.AbyssClashFuncOpen{OpenFlag: 1, Step: 1}))
		case protocol.ActionIDAbyssClashStep1Panel:
			n := panelCalls.Add(1)
			switch n {
			case 1:
				_ = protocol.WritePacket(conn, abyssPanel(2, []int32{32, 31, 33}))
			case 2:
				_ = protocol.WritePacket(conn, abyssPanel(1, []int32{32, 31, 33}))
			default:
				_ = protocol.WritePacket(conn, abyssPanel(0, []int32{32, 31, 33}))
			}
		case protocol.ActionIDAbyssClashMonsterTeam:
			infoCalls.Add(1)
			_ = protocol.WritePacket(conn, protocol.BuildAbyssClashMonsterTeamResultPacket(
				protocol.AbyssClashResultSuccess))
		case protocol.ActionIDAbyssClashStartFight:
			fightCalls.Add(1)
			id := int32(pkt.Payload[0])<<24 | int32(pkt.Payload[1])<<16 |
				int32(pkt.Payload[2])<<8 | int32(pkt.Payload[3])
			fought = append(fought, id)
			_ = protocol.WritePacket(conn, protocol.BuildAbyssClashFightResultPacket(
				protocol.AbyssClashResultSuccess, 1000, 1))
		}
	})
	defer sess.Close()

	if err := NewAbyssClashRoutine().Execute(context.Background(), sess, nil); err != nil {
		t.Fatalf("abyss clash: %v", err)
	}
	if got := fightCalls.Load(); got != 2 {
		t.Fatalf("剩余 2 次应挑战 2 次, got %d", got)
	}
	if infoCalls.Load() != 2 {
		t.Fatalf("每次挑战前应查询怪物信息, got %d", infoCalls.Load())
	}
	// 目标按 step_monster_id 升序轮询：31、32。
	if len(fought) != 2 || fought[0] != 31 || fought[1] != 32 {
		t.Fatalf("fought = %v, want [31 32]", fought)
	}
	if panelCalls.Load() < 3 {
		t.Fatalf("每次挑战后应刷新面板, panel calls=%d", panelCalls.Load())
	}
}

func TestAbyssClashRoutine_NoFightNumStops(t *testing.T) {
	stubAbyssClashSession(t)
	var fightCalls atomic.Int32
	sess := newPvPMockSession(t, func(conn net.Conn, pkt *protocol.Packet) {
		switch pkt.ActionID {
		case protocol.ActionIDAbyssClashFuncOpen:
			_ = protocol.WritePacket(conn, protocol.BuildAbyssClashFuncOpenResultPacket(
				&protocol.AbyssClashFuncOpen{OpenFlag: 1, Step: 1}))
		case protocol.ActionIDAbyssClashStep1Panel:
			_ = protocol.WritePacket(conn, abyssPanel(3, []int32{31, 32, 33}))
		case protocol.ActionIDAbyssClashMonsterTeam:
			_ = protocol.WritePacket(conn, protocol.BuildAbyssClashMonsterTeamResultPacket(
				protocol.AbyssClashResultSuccess))
		case protocol.ActionIDAbyssClashStartFight:
			fightCalls.Add(1)
			_ = protocol.WritePacket(conn, protocol.BuildAbyssClashFightResultPacket(
				protocol.AbyssClashResultNoFightNum, 0, 0))
		}
	})
	defer sess.Close()

	if err := NewAbyssClashRoutine().Execute(context.Background(), sess, nil); err != nil {
		t.Fatalf("abyss clash: %v", err)
	}
	if got := fightCalls.Load(); got != 1 {
		t.Fatalf("次数不足后应立即停止, fights=%d", got)
	}
}

func TestAbyssClashRoutine_FuncNotOpenSkips(t *testing.T) {
	stubAbyssClashSession(t)
	var panelCalls atomic.Int32
	sess := newPvPMockSession(t, func(conn net.Conn, pkt *protocol.Packet) {
		switch pkt.ActionID {
		case protocol.ActionIDAbyssClashFuncOpen:
			_ = protocol.WritePacket(conn, protocol.BuildAbyssClashFuncOpenResultPacket(
				&protocol.AbyssClashFuncOpen{OpenFlag: 0, Step: 0}))
		case protocol.ActionIDAbyssClashStep1Panel:
			panelCalls.Add(1)
		}
	})
	defer sess.Close()

	err := NewAbyssClashRoutine().Execute(context.Background(), sess, nil)
	var cd *scheduler.CooldownError
	if !errors.As(err, &cd) {
		t.Fatalf("未开放应返回 CooldownError: %v", err)
	}
	if panelCalls.Load() != 0 {
		t.Fatalf("未开放不应请求面板, calls=%d", panelCalls.Load())
	}
}

func TestAbyssClashRoutine_PanelSilentDropBacksOff(t *testing.T) {
	stubAbyssClashSession(t)
	sess := newPvPMockSession(t, func(conn net.Conn, pkt *protocol.Packet) {
		if pkt.ActionID == protocol.ActionIDAbyssClashFuncOpen {
			_ = protocol.WritePacket(conn, protocol.BuildAbyssClashFuncOpenResultPacket(
				&protocol.AbyssClashFuncOpen{OpenFlag: 1, Step: 1}))
		}
		// 面板静默丢弃。
	})
	defer sess.Close()

	ctx := kit.WithTimeoutOverride(context.Background(), 150*time.Millisecond)
	err := NewAbyssClashRoutine().Execute(ctx, sess, nil)
	if err == nil {
		t.Fatal("面板无回包应返回退避错误")
	}
	var cd *scheduler.CooldownError
	if !errors.As(err, &cd) {
		t.Fatalf("want CooldownError, got %T %v", err, err)
	}
}

func TestPickAbyssClashTarget_SortedRoundRobin(t *testing.T) {
	panel := &protocol.AbyssClashPanel{Step: 1, Monsters: []protocol.AbyssClashMonster{
		{StepMonsterID: 33}, {StepMonsterID: 31}, {StepMonsterID: 32},
	}}
	order := []int32{31, 32, 33, 31}
	for i, want := range order {
		got, ok := pickAbyssClashTarget(panel, i, nil)
		if !ok || got.StepMonsterID != want {
			t.Fatalf("attempt %d: got %+v, want %d", i, got, want)
		}
	}
	// 第二阶段：Boss 未解锁时选普通怪物；解锁后 Boss 也进入候选。
	panel2 := &protocol.AbyssClashPanel{Step: 2, Monsters: []protocol.AbyssClashMonster{
		{StepMonsterID: 41, IsBoss: 1, BossCanFightNum: 0},
		{StepMonsterID: 42, IsBoss: 0},
		{StepMonsterID: 43, IsBoss: 0},
	}}
	got, ok := pickAbyssClashTarget(panel2, 0, nil)
	if !ok || got.StepMonsterID != 42 {
		t.Fatalf("step2 target = %+v ok=%v", got, ok)
	}
	panel3 := &protocol.AbyssClashPanel{Step: 2, Monsters: []protocol.AbyssClashMonster{
		{StepMonsterID: 41, IsBoss: 1, BossCanFightNum: 0},
		{StepMonsterID: 42, IsBoss: 1, BossCanFightNum: 2},
		{StepMonsterID: 43, IsBoss: 0},
	}}
	if got, ok := pickAbyssClashTarget(panel3, 0, nil); !ok || got.StepMonsterID != 42 {
		t.Fatalf("step2 unlocked target = %+v ok=%v", got, ok)
	}
}
