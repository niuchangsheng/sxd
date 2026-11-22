package daily

import (
	"context"
	"errors"
	"net"
	"sync/atomic"
	"testing"
	"time"

	"sxd-pie-ng/internal/protocol"
	"sxd-pie-ng/internal/routines/kit"
	"sxd-pie-ng/internal/scheduler"
)

// okLevitateInfoPacket 返回可继续灌注的面板：阶段 1、经验未满、
// 经验 (3425) 与铜钱 (1747) 都充足。
func okLevitateInfoPacket() *protocol.Packet {
	return protocol.BuildLevitateInfoResultPacket(&protocol.LevitateInfo{
		Step:             1,
		PerfusionableExp: 100,
		Materials: []protocol.LevitateMaterial{
			{ItemID: protocol.LevitateMaterialExpItemID, Available: 10, Need: 100},
			{ItemID: protocol.LevitateMaterialCoinItemID, Available: 1000, Need: 100},
		},
	})
}

func TestLevitatePerfusionRoutine_PerfusesUntilUsedUp(t *testing.T) {
	var perfCalls atomic.Int32
	sess := newDailyMockSession(t, func(conn net.Conn, pkt *protocol.Packet) {
		switch pkt.ActionID {
		case protocol.ActionIDLevitateInfo:
			_ = protocol.WritePacket(conn, okLevitateInfoPacket())
		case protocol.ActionIDLevitatePerfusion:
			n := perfCalls.Add(1)
			result := uint8(protocol.LevitateSuccess)
			if n >= 3 {
				result = protocol.LevitateUsedUp
			}
			_ = protocol.WritePacket(conn, protocol.BuildLevitatePerfusionResultPacket(result, nil))
		}
	})
	defer sess.Close()

	if err := NewLevitatePerfusionRoutine().Execute(context.Background(), sess, nil); err != nil {
		t.Fatalf("levitate: %v", err)
	}
	if perfCalls.Load() != 3 {
		t.Fatalf("应在第 3 次 USED_UP 时停止, calls = %d", perfCalls.Load())
	}
}

func TestLevitatePerfusionRoutine_NotOpenCooldown(t *testing.T) {
	var perfCalls atomic.Int32
	sess := newDailyMockSession(t, func(conn net.Conn, pkt *protocol.Packet) {
		switch pkt.ActionID {
		case protocol.ActionIDLevitateInfo:
			_ = protocol.WritePacket(conn, okLevitateInfoPacket())
		case protocol.ActionIDLevitatePerfusion:
			perfCalls.Add(1)
			_ = protocol.WritePacket(conn, protocol.BuildLevitatePerfusionResultPacket(protocol.LevitateFuncNoOpen, nil))
		}
	})
	defer sess.Close()

	err := NewLevitatePerfusionRoutine().Execute(context.Background(), sess, nil)
	var cd *scheduler.CooldownError
	if !errors.As(err, &cd) {
		t.Fatalf("功能未开放应返回 CooldownError, got %v", err)
	}
	if perfCalls.Load() != 1 {
		t.Fatalf("功能未开放后应停止, calls = %d", perfCalls.Load())
	}
}

func TestLevitatePerfusionRoutine_InfoNoResponse(t *testing.T) {
	var perfCalls atomic.Int32
	sess := newDailyMockSession(t, func(conn net.Conn, pkt *protocol.Packet) {
		if pkt.ActionID == protocol.ActionIDLevitatePerfusion {
			perfCalls.Add(1)
		}
	})
	defer sess.Close()

	ctx := kit.WithTimeoutOverride(context.Background(), 150*time.Millisecond)
	err := NewLevitatePerfusionRoutine().Execute(ctx, sess, nil)
	if err == nil {
		t.Fatal("灌注也无回包时应进入冷却")
	}
	// 面板 357/0 无回包不再阻断链路：仍要尝试一轮 357/1（含最多 1 次 PrevAct 重同步重试）。
	if got := perfCalls.Load(); got < 1 || got > 2 {
		t.Fatalf("面板无回包时仍应尝试一次灌注(含重同步最多2包), calls = %d", got)
	}
}

// TestLevitatePerfusionRoutine_InfoOKButPerfusionDroppedBacksOff 验证面板 357/0 正常回包、
// 但灌注 357/1 被服务端静默丢弃时，连续丢包计数不会被面板成功清零：
// 达到阈值后必须挂起到每日重置，而不是每 30 分钟无限空转。
func TestLevitatePerfusionRoutine_InfoOKButPerfusionDroppedBacksOff(t *testing.T) {
	var perfCalls atomic.Int32
	sess := newDailyMockSession(t, func(conn net.Conn, pkt *protocol.Packet) {
		switch pkt.ActionID {
		case protocol.ActionIDLevitateInfo:
			_ = protocol.WritePacket(conn, okLevitateInfoPacket())
		case protocol.ActionIDLevitatePerfusion:
			perfCalls.Add(1)
			// 静默丢弃：不回包。
		}
	})
	defer sess.Close()

	routine := NewLevitatePerfusionRoutine()
	ctx := kit.WithTimeoutOverride(context.Background(), 150*time.Millisecond)
	var lastErr error
	for i := 0; i < 3; i++ {
		lastErr = routine.Execute(ctx, sess, nil)
		if lastErr == nil {
			t.Fatalf("第 %d 轮灌注无回包应返回冷却错误", i+1)
		}
	}
	if got := scheduler.SilentDropCount(sess, "levitate_perfusion"); got < 3 {
		t.Fatalf("面板成功不应清零灌注丢包计数, got %d", got)
	}
	var cd *scheduler.CooldownError
	if !errors.As(lastErr, &cd) {
		t.Fatalf("达到阈值后应返回 CooldownError, got %v", lastErr)
	}
	// HandleSilentDrop 到阈值后挂起至 min(次日 00:05, 6 小时)。
	exp := scheduler.NextDailyReset(time.Now())
	if cap := time.Now().Add(6 * time.Hour); exp.After(cap) {
		exp = cap
	}
	if d := cd.Until.Sub(exp); d > time.Minute || d < -time.Minute {
		t.Fatalf("熔断唤醒时间 = %s, want %s", cd.Until, exp)
	}
	// 每轮 Execute 内部可能因 PrevAct 重同步重放一次 357/1，
	// 因此只校验没有出现额外轮次的请求（3 轮 × 最多 2 次）。
	if got := perfCalls.Load(); got < 3 || got > 6 {
		t.Fatalf("达到阈值后不应继续无限请求, perf calls = %d", got)
	}
}

// TestLevitatePerfusionRoutine_InfoFallsBackToMainRole 验证伙伴页签 (type=1) 的
// 357/0 被服务端静默丢弃时，改用主角页签 (type=0) 探测后仍会继续灌注。
func TestLevitatePerfusionRoutine_InfoFallsBackToMainRole(t *testing.T) {
	var perfCalls, mainInfoCalls atomic.Int32
	sess := newDailyMockSession(t, func(conn net.Conn, pkt *protocol.Packet) {
		switch pkt.ActionID {
		case protocol.ActionIDLevitateInfo:
			if len(pkt.Payload) > 0 && pkt.Payload[0] == protocol.LevitateTypeMainRole {
				mainInfoCalls.Add(1)
				_ = protocol.WritePacket(conn, okLevitateInfoPacket())
			}
			// type=1 伙伴页签：静默丢弃，不回包。
		case protocol.ActionIDLevitatePerfusion:
			perfCalls.Add(1)
			_ = protocol.WritePacket(conn, protocol.BuildLevitatePerfusionResultPacket(
				protocol.LevitateUsedUp, nil))
		}
	})
	defer sess.Close()

	if err := NewLevitatePerfusionRoutine().Execute(context.Background(), sess, nil); err != nil {
		t.Fatalf("levitate: %v", err)
	}
	// 首次探测回退到主角，且在灌注结束后会复用同一页签复核飞升条件，
	// 因此主角页签调用次数 >= 1。
	if mainInfoCalls.Load() < 1 {
		t.Fatalf("应回退到主角页签探测, calls = %d", mainInfoCalls.Load())
	}
	if perfCalls.Load() != 1 {
		t.Fatalf("主角页签探活后应继续灌注, calls = %d", perfCalls.Load())
	}
}

// TestLevitatePerfusionRoutine_PerfusionPrechecks 覆盖灌注前置检查：
// 阶段经验已满或没有可灌注经验/经验丹时不再发 357/1；
// 阶段 5 但经验未满时仍可继续灌注。
func TestLevitatePerfusionRoutine_PerfusionPrechecks(t *testing.T) {
	infoWith := func(mut func(*protocol.LevitateInfo)) *protocol.Packet {
		info := &protocol.LevitateInfo{
			Step:             2,
			PerfusionableExp: 100,
			Materials: []protocol.LevitateMaterial{
				{ItemID: protocol.LevitateMaterialExpItemID, Available: 10, Need: 100},
				{ItemID: protocol.LevitateMaterialCoinItemID, Available: 1000, Need: 100},
			},
		}
		mut(info)
		return protocol.BuildLevitateInfoResultPacket(info)
	}

	cases := []struct {
		name      string
		info      *protocol.Packet
		wantCalls int32
	}{
		{
			name: "阶段经验已满",
			info: infoWith(func(i *protocol.LevitateInfo) {
				i.Materials[0].Available = 100
			}),
			wantCalls: 0,
		},
		{
			name: "无可灌注经验且无经验丹",
			info: infoWith(func(i *protocol.LevitateInfo) {
				i.PerfusionableExp = 0
				i.ExpItems = nil
			}),
			wantCalls: 0,
		},
		{
			name: "阶段5经验未满仍可灌注",
			info: infoWith(func(i *protocol.LevitateInfo) {
				i.Step = protocol.LevitateMaxStep
			}),
			wantCalls: 1,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var perfCalls atomic.Int32
			sess := newDailyMockSession(t, func(conn net.Conn, pkt *protocol.Packet) {
				switch pkt.ActionID {
				case protocol.ActionIDLevitateInfo:
					_ = protocol.WritePacket(conn, tc.info)
				case protocol.ActionIDLevitatePerfusion:
					perfCalls.Add(1)
					_ = protocol.WritePacket(conn, protocol.BuildLevitatePerfusionResultPacket(
						protocol.LevitateUsedUp, nil))
				}
			})
			defer sess.Close()

			if err := NewLevitatePerfusionRoutine().Execute(context.Background(), sess, nil); err != nil {
				t.Fatalf("levitate: %v", err)
			}
			if got := perfCalls.Load(); got != tc.wantCalls {
				t.Fatalf("灌注次数 = %d, want %d", got, tc.wantCalls)
			}
		})
	}
}

// TestLevitatePerfusionRoutine_Ascend 覆盖飞升 (357/2) 前置检查：
// 阶段 5 + 经验满 + 灵智已用完 + 材料/铜钱充足（材料不足可用神炼石替代）。
func TestLevitatePerfusionRoutine_Ascend(t *testing.T) {
	ascendInfo := func(mut func(*protocol.LevitateInfo)) *protocol.Packet {
		info := &protocol.LevitateInfo{
			Step:             protocol.LevitateMaxStep,
			AvailableLingZhi: 0,
			Materials: []protocol.LevitateMaterial{
				{ItemID: protocol.LevitateMaterialExpItemID, Available: 100, Need: 100},
				{ItemID: protocol.LevitateMaterialCoinItemID, Available: 1000, Need: 100},
				{ItemID: 9001, Available: 5, Need: 5},
			},
		}
		mut(info)
		return protocol.BuildLevitateInfoResultPacket(info)
	}

	cases := []struct {
		name      string
		info      *protocol.Packet
		wantCalls int32
		wantFlag  int32
	}{
		{
			name:      "条件满足",
			info:      ascendInfo(func(*protocol.LevitateInfo) {}),
			wantCalls: 1,
			wantFlag:  0,
		},
		{
			name: "阶段未到5",
			info: ascendInfo(func(i *protocol.LevitateInfo) {
				i.Step = protocol.LevitateMaxStep - 1
			}),
			wantCalls: 0,
		},
		{
			name: "阶段经验未满",
			info: ascendInfo(func(i *protocol.LevitateInfo) {
				i.Materials[0].Available = 10
			}),
			wantCalls: 0,
		},
		{
			name: "仍有可用灵智",
			info: ascendInfo(func(i *protocol.LevitateInfo) {
				i.AvailableLingZhi = 3
			}),
			wantCalls: 0,
		},
		{
			name: "铜钱不足",
			info: ascendInfo(func(i *protocol.LevitateInfo) {
				i.Materials[1].Available = 50
			}),
			wantCalls: 0,
		},
		{
			name: "材料不足且无神炼石",
			info: ascendInfo(func(i *protocol.LevitateInfo) {
				i.Materials[2].Available = 1
			}),
			wantCalls: 0,
		},
		{
			name: "材料不足但勾选神炼石替代",
			info: ascendInfo(func(i *protocol.LevitateInfo) {
				i.Materials[2].Available = 1
				i.ShenLianShiNum = 2
			}),
			wantCalls: 1,
			wantFlag:  1,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var ascendCalls, perfCalls atomic.Int32
			var gotFlag atomic.Int32
			sess := newDailyMockSession(t, func(conn net.Conn, pkt *protocol.Packet) {
				switch pkt.ActionID {
				case protocol.ActionIDLevitateInfo:
					_ = protocol.WritePacket(conn, tc.info)
				case protocol.ActionIDLevitatePerfusion:
					perfCalls.Add(1)
					_ = protocol.WritePacket(conn, protocol.BuildLevitatePerfusionResultPacket(
						protocol.LevitateUsedUp, nil))
				case protocol.ActionIDLevitate:
					ascendCalls.Add(1)
					if len(pkt.Payload) >= 5 {
						gotFlag.Store(int32(uint32(pkt.Payload[1])<<24 |
							uint32(pkt.Payload[2])<<16 |
							uint32(pkt.Payload[3])<<8 |
							uint32(pkt.Payload[4])))
					}
					_ = protocol.WritePacket(conn, protocol.BuildLevitateActionResultPacket(
						protocol.LevitateSuccess, 1))
				}
			})
			defer sess.Close()

			if err := NewLevitatePerfusionRoutine().Execute(context.Background(), sess, nil); err != nil {
				t.Fatalf("levitate: %v", err)
			}
			if got := perfCalls.Load(); got != 0 {
				t.Fatalf("经验已满时不应继续灌注, calls = %d", got)
			}
			if got := ascendCalls.Load(); got != tc.wantCalls {
				t.Fatalf("飞升次数 = %d, want %d", got, tc.wantCalls)
			}
			if tc.wantCalls > 0 && gotFlag.Load() != tc.wantFlag {
				t.Fatalf("is_use_shen_lian_zhu = %d, want %d", gotFlag.Load(), tc.wantFlag)
			}
		})
	}
}

// TestLevitatePerfusionRoutine_AscendNoEnoughMaterialCooldown 验证服务端返回
// 材料不足 (7) 时进入冷却，而不是无限重试。
func TestLevitatePerfusionRoutine_AscendNoEnoughMaterialCooldown(t *testing.T) {
	info := protocol.BuildLevitateInfoResultPacket(&protocol.LevitateInfo{
		Step: protocol.LevitateMaxStep,
		Materials: []protocol.LevitateMaterial{
			{ItemID: protocol.LevitateMaterialExpItemID, Available: 100, Need: 100},
			{ItemID: protocol.LevitateMaterialCoinItemID, Available: 1000, Need: 100},
		},
	})
	sess := newDailyMockSession(t, func(conn net.Conn, pkt *protocol.Packet) {
		switch pkt.ActionID {
		case protocol.ActionIDLevitateInfo:
			_ = protocol.WritePacket(conn, info)
		case protocol.ActionIDLevitate:
			_ = protocol.WritePacket(conn, protocol.BuildLevitateActionResultPacket(
				protocol.LevitateNoEnoughMaterial, 0))
		}
	})
	defer sess.Close()

	err := NewLevitatePerfusionRoutine().Execute(context.Background(), sess, nil)
	var cd *scheduler.CooldownError
	if !errors.As(err, &cd) {
		t.Fatalf("飞升材料不足应返回 CooldownError, got %v", err)
	}
}
