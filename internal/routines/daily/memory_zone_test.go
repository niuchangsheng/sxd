package daily

import (
	"context"
	"encoding/hex"
	"errors"
	"net"
	"sync/atomic"
	"testing"

	"sxd-pie-ng/internal/protocol"
	"sxd-pie-ng/internal/scheduler"
)

func mustHex(t *testing.T, s string) []byte {
	t.Helper()
	b, err := hex.DecodeString(s)
	if err != nil {
		t.Fatalf("decode hex: %v", err)
	}
	return b
}

func encodeMemoryZoneBackPanel(
	res uint8,
	partners []protocol.MemoryZonePartner,
	todayTimes, freeTimes, ingotTimes, costIngot int32,
) []byte {
	w := protocol.NewWriter()
	w.WriteUint8(res)
	if res != protocol.MemoryZoneResultSuccess {
		return w.Bytes()
	}
	w.WriteUint16(uint16(len(partners)))
	for _, p := range partners {
		w.WriteInt32(p.RoleID)
		w.WriteInt32(p.HaveScrap)
		w.WriteInt32(p.NeedScrap)
	}
	w.WriteInt32(todayTimes)
	w.WriteInt32(freeTimes)
	w.WriteInt32(ingotTimes)
	w.WriteInt32(costIngot)
	return w.Bytes()
}

func encodeMemoryZoneMainPanel(res uint8, cats []protocol.MemoryZoneCategory, daoyuan int32) []byte {
	w := protocol.NewWriter()
	w.WriteUint8(res)
	if res != protocol.MemoryZoneResultSuccess {
		return w.Bytes()
	}
	w.WriteUint16(uint16(len(cats)))
	for _, c := range cats {
		w.WriteInt32(c.Type)
		w.WriteInt32(c.HaveNum)
		w.WriteInt32(c.MaxNum)
		w.WriteInt32(c.GetAward)
	}
	w.WriteInt32(daoyuan)
	return w.Bytes()
}

func TestMemoryZoneRoutine_SuccessPcapngFlow(t *testing.T) {
	// 完整复现 huiyizhijing.pcapng 真实交互：
	// type=1 (6/6), type=2 (5/5), type=3 (1/3) -> 进入 type=3 ->
	// 486 (500/500 已拥有), 487 (11/500 未拥有), 488 (0/500 未拥有) ->
	// 自动选择第一个未拥有伙伴 487 回忆成功，获得 8273×1，进度 11->12，今日次数 1/1，下次 20 元宝。
	mainGolden := mustHex(t, "000003000000010000000600000006000000010000000200000005000000050000000100000003000000010000000300000001000000c8")
	backGolden := mustHex(t, "000003000001e6000001f4000001f4000001e70000000b000001f4000001e800000000000001f400000000000000010000000900000000")
	btGolden := mustHex(t, "00000100002051000000010001000001e70000000c000001f40000000100000014")

	var requestedZone atomic.Int32
	var backtrackedRole atomic.Int32
	var backtrackCalls atomic.Int32

	sess := newDailyMockSession(t, func(conn net.Conn, pkt *protocol.Packet) {
		switch pkt.ActionID {
		case protocol.ActionIDPlayerCheckFunctionOpen:
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, []byte{protocol.PlayerFunctionOpenTrue}))
		case protocol.ActionIDMemoryZoneMainPanel:
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, mainGolden))
		case protocol.ActionIDMemoryZoneBackPanel:
			r := protocol.NewReader(pkt.Payload)
			zoneType, _ := r.ReadInt32()
			requestedZone.Store(zoneType)
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, backGolden))
		case protocol.ActionIDMemoryZoneBacktrack:
			r := protocol.NewReader(pkt.Payload)
			roleID, _ := r.ReadInt32()
			backtrackedRole.Store(roleID)
			backtrackCalls.Add(1)
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, btGolden))
		}
	})
	defer sess.Close()

	err := NewMemoryZoneRoutine().Execute(context.Background(), sess, nil)
	if !errors.Is(err, scheduler.ErrAttemptsExhausted) {
		t.Fatalf("完成每日唯一免费回溯后应返回 ErrAttemptsExhausted, got %v", err)
	}
	if requestedZone.Load() != 3 {
		t.Fatalf("应查询魔神专区 type=3, got %d", requestedZone.Load())
	}
	if backtrackedRole.Load() != 487 {
		t.Fatalf("应跳过已拥有的 486 并选择第一个未拥有伙伴 487, got %d", backtrackedRole.Load())
	}
	if backtrackCalls.Load() != 1 {
		t.Fatalf("每日仅应回溯 1 次, got %d", backtrackCalls.Load())
	}
}

func TestMemoryZoneRoutine_AlreadyUsedTodayOrCostsIngot(t *testing.T) {
	cases := []struct {
		name       string
		todayTimes int32
		freeTimes  int32
		costIngot  int32
	}{
		{name: "今日已回溯", todayTimes: 1, freeTimes: 1, costIngot: 0},
		{name: "需要消耗元宝", todayTimes: 0, freeTimes: 1, costIngot: 20},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var backtrackCalls atomic.Int32
			sess := newDailyMockSession(t, func(conn net.Conn, pkt *protocol.Packet) {
				switch pkt.ActionID {
				case protocol.ActionIDPlayerCheckFunctionOpen:
					_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, []byte{protocol.PlayerFunctionOpenTrue}))
				case protocol.ActionIDMemoryZoneMainPanel:
					_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, encodeMemoryZoneMainPanel(
						protocol.MemoryZoneResultSuccess,
						[]protocol.MemoryZoneCategory{{Type: 3, HaveNum: 1, MaxNum: 3, GetAward: 1}},
						200,
					)))
				case protocol.ActionIDMemoryZoneBackPanel:
					_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, encodeMemoryZoneBackPanel(
						protocol.MemoryZoneResultSuccess,
						[]protocol.MemoryZonePartner{
							{RoleID: 486, HaveScrap: 500, NeedScrap: 500},
							{RoleID: 487, HaveScrap: 11, NeedScrap: 500},
						},
						tc.todayTimes, tc.freeTimes, 9, tc.costIngot,
					)))
				case protocol.ActionIDMemoryZoneBacktrack:
					backtrackCalls.Add(1)
				}
			})
			defer sess.Close()

			err := NewMemoryZoneRoutine().Execute(context.Background(), sess, nil)
			if !errors.Is(err, scheduler.ErrAttemptsExhausted) {
				t.Fatalf("got %v, want ErrAttemptsExhausted", err)
			}
			if backtrackCalls.Load() != 0 {
				t.Fatalf("无免费次数或需消耗元宝时绝不能发送 526/4, calls = %d", backtrackCalls.Load())
			}
		})
	}
}

func TestMemoryZoneRoutine_AllPartnersOwned(t *testing.T) {
	var backtrackCalls atomic.Int32
	sess := newDailyMockSession(t, func(conn net.Conn, pkt *protocol.Packet) {
		switch pkt.ActionID {
		case protocol.ActionIDPlayerCheckFunctionOpen:
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, []byte{protocol.PlayerFunctionOpenTrue}))
		case protocol.ActionIDMemoryZoneMainPanel:
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, encodeMemoryZoneMainPanel(
				protocol.MemoryZoneResultSuccess,
				[]protocol.MemoryZoneCategory{
					{Type: 1, HaveNum: 6, MaxNum: 6, GetAward: 1},
					{Type: 2, HaveNum: 5, MaxNum: 5, GetAward: 1},
					{Type: 3, HaveNum: 3, MaxNum: 3, GetAward: 1},
				},
				200,
			)))
		case protocol.ActionIDMemoryZoneBacktrack:
			backtrackCalls.Add(1)
		}
	})
	defer sess.Close()

	err := NewMemoryZoneRoutine().Execute(context.Background(), sess, nil)
	if !errors.Is(err, scheduler.ErrConditionNotMet) {
		t.Fatalf("全部伙伴已拥有应返回 ErrConditionNotMet, got %v", err)
	}
	if backtrackCalls.Load() != 0 {
		t.Fatalf("不应发送回溯请求, calls = %d", backtrackCalls.Load())
	}
}

func TestMemoryZoneRoutine_FunctionNotOpen(t *testing.T) {
	var mainCalls atomic.Int32
	sess := newDailyMockSession(t, func(conn net.Conn, pkt *protocol.Packet) {
		switch pkt.ActionID {
		case protocol.ActionIDPlayerCheckFunctionOpen:
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, []byte{protocol.PlayerFunctionOpenFalse}))
		case protocol.ActionIDMemoryZoneMainPanel:
			mainCalls.Add(1)
		}
	})
	defer sess.Close()

	err := NewMemoryZoneRoutine().Execute(context.Background(), sess, nil)
	if !errors.Is(err, scheduler.ErrConditionNotMet) {
		t.Fatalf("功能未开放应返回 ErrConditionNotMet, got %v", err)
	}
	if mainCalls.Load() != 0 {
		t.Fatalf("功能未开放时不应发送 526/1, calls = %d", mainCalls.Load())
	}
}

func TestMemoryZoneRoutine_BacktrackResultCodes(t *testing.T) {
	cases := []struct {
		name  string
		code  uint8
		check func(error) bool
	}{
		{
			name:  "MAX_TIMES 熔断至次日",
			code:  protocol.MemoryZoneResultMaxTimes,
			check: func(err error) bool { return errors.Is(err, scheduler.ErrAttemptsExhausted) },
		},
		{
			name:  "NO_INGOT 停止不消耗元宝",
			code:  protocol.MemoryZoneResultNoIngot,
			check: func(err error) bool { return errors.Is(err, scheduler.ErrAttemptsExhausted) },
		},
		{
			name: "FAILED 进入冷却",
			code: protocol.MemoryZoneResultFailed,
			check: func(err error) bool {
				var cd *scheduler.CooldownError
				return errors.As(err, &cd)
			},
		},
		{
			name: "未知结果码进入冷却",
			code: 99,
			check: func(err error) bool {
				var cd *scheduler.CooldownError
				return errors.As(err, &cd)
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sess := newDailyMockSession(t, func(conn net.Conn, pkt *protocol.Packet) {
				switch pkt.ActionID {
				case protocol.ActionIDPlayerCheckFunctionOpen:
					_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, []byte{protocol.PlayerFunctionOpenTrue}))
				case protocol.ActionIDMemoryZoneMainPanel:
					_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, encodeMemoryZoneMainPanel(
						protocol.MemoryZoneResultSuccess,
						[]protocol.MemoryZoneCategory{{Type: 3, HaveNum: 0, MaxNum: 3, GetAward: 0}},
						200,
					)))
				case protocol.ActionIDMemoryZoneBackPanel:
					_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, encodeMemoryZoneBackPanel(
						protocol.MemoryZoneResultSuccess,
						[]protocol.MemoryZonePartner{
							{RoleID: 486, HaveScrap: 0, NeedScrap: 500},
							{RoleID: 487, HaveScrap: 0, NeedScrap: 500},
							{RoleID: 488, HaveScrap: 0, NeedScrap: 500},
						},
						0, 1, 9, 0,
					)))
				case protocol.ActionIDMemoryZoneBacktrack:
					_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, []byte{tc.code}))
				}
			})
			defer sess.Close()

			err := NewMemoryZoneRoutine().Execute(context.Background(), sess, nil)
			if err == nil || !tc.check(err) {
				t.Fatalf("result=%d got err=%v", tc.code, err)
			}
		})
	}
}
