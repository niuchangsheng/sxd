// Package farming_test 验证西天取经圣域通道的自动回退策略。
//
// 线上仅凭 force_saint_area 布尔开关需要按区服手工配置，容易漏配/错配。
// 现改为默认按圣域功能开放状态自动判定: 已开放圣域的角色只走圣域，
// 未开放的角色回退仙界跨服/主服取经。
package farming_test

import (
	"context"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"sxd-pie-ng/internal/client"
	"sxd-pie-ng/internal/protocol"
	"sxd-pie-ng/internal/routines/farming"
)

// runPilgrimageMock 以最小网关桩执行一次取经巡检，返回传统主服取经入口的调用次数。
//
// functions 为服务端下发的已开放功能列表，saStatus 为圣域开启状态码。
func runPilgrimageMock(t *testing.T, cfg farming.PilgrimageConfig, functions []int32, saStatus uint8) int32 {
	t.Helper()

	c1, c2 := net.Pipe()
	var takeBibleOpens atomic.Int32
	var wg sync.WaitGroup
	wg.Add(1)

	go func() {
		defer wg.Done()
		for {
			pkt, err := protocol.ReadPacket(c2)
			if err != nil {
				return
			}
			switch pkt.ActionID {
			case protocol.ActionIDPlayerLogin:
				respPayload := []byte{
					0x00, 0x00, 0x00, 0x00, 0x04, 0x0a, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
					0x00, 0x00, 0x01, 0x00, 0x0b, 0x00, 0x00, 0x10, 0x85, 0x01,
				}
				_ = protocol.WritePacket(c2, protocol.NewPacket(protocol.ActionIDPlayerLogin, respPayload))

			case protocol.ActionIDPlayerInitStep1:
				respPayload := []byte{0x00, 0x00, 0x13, 0x0f, 0x00, 0x02, 0x00, 0x00, 0x13, 0x0f, 0x00, 0x00, 0x13, 0x12}
				_ = protocol.WritePacket(c2, protocol.NewPacket(protocol.ActionIDPlayerInitStep1, respPayload))

			case protocol.ActionIDPlayerInitStep2:
				_ = protocol.WritePacket(c2, protocol.NewPacket(protocol.ActionIDPlayerInitStep2, []byte{0x00}))

			case protocol.ActionIDPlayerInitStep3:
				_ = protocol.WritePacket(c2, protocol.NewPacket(protocol.ActionIDPlayerInitStep3, make([]byte, 8)))

			case protocol.ActionIDPlayerGetInfo:
				_ = protocol.WritePacket(c2, protocol.NewPacket(protocol.ActionIDPlayerGetInfo, []byte{
					0x00, 0x09, 0xe6, 0xa2, 0xa6, 0xe4, 0xb8, 0x80, 0xe5, 0x9c, 0xba,
					0x00, 0x00, 0x01, 0x2c,
					0x00, 0x00, 0x00, 0x64,
					0x00, 0x00, 0x00, 0x00, 0x00, 0x01, 0x00, 0x00,
					0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
					0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
					0x00, 0x00, 0x01, 0x2c,
				}))

			case protocol.ActionHeartbeat:
				_ = protocol.WritePacket(c2, protocol.NewPacket(protocol.ActionHeartbeat, []byte{0x06, 0x00, 0x0a, 0x28}))

			case protocol.ActionIDSaintAreaStatus:
				w := protocol.NewWriter()
				w.WriteUint8(saStatus)
				w.WriteUint16(0)
				_ = protocol.WritePacket(c2, protocol.NewPacket(protocol.ActionIDSaintAreaStatus, w.Bytes()))

			case protocol.ActionIDPlayerGetFunctions:
				_ = protocol.WritePacket(c2, protocol.BuildPlayerFunctionsResultPacket(functions))

			case protocol.ActionIDTakeBibleOpen:
				takeBibleOpens.Add(1)
				// CanTakeTimes=0: 仅验证是否走到传统主服链路，随后立即安全关闭。
				w := protocol.NewWriter()
				w.WriteInt32(0)
				w.WriteUint8(0)
				_ = protocol.WritePacket(c2, protocol.NewPacket(protocol.ActionIDTakeBibleOpen, w.Bytes()))

			case protocol.ActionIDTakeBibleClose:
				_ = protocol.WritePacket(c2, protocol.NewPacket(protocol.ActionIDTakeBibleClose, []byte{}))
			}
		}
	}()

	session := client.NewRoleSession(client.SessionConfig{
		RoleID:     "role-pilgrim-sa-policy",
		RoleName:   "取经策略校验",
		Username:   "pilgrim_policy_user",
		Hash:       "dummy_pilgrim_policy_hash",
		ServerAddr: "pilgrim-sa-policy-pipe",
		Dialer: func(ctx context.Context, network, addr string) (net.Conn, error) {
			return c1, nil
		},
	})

	if err := session.Start(context.Background()); err != nil {
		t.Fatalf("session.Start 失败: %v", err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for session.State() != client.StateActive && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if session.State() != client.StateActive {
		t.Fatalf("会话未进入激活态: %s", session.State())
	}

	execCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := farming.NewPilgrimageRoutine(cfg).Execute(execCtx, session, nil); err != nil {
		t.Fatalf("取经 Routine 执行失败: %v", err)
	}

	session.Close()
	c1.Close()
	c2.Close()
	wg.Wait()
	return takeBibleOpens.Load()
}

// TestPilgrimageSaintAreaAuto_SkipsWhenFeatureOpen 验证圣域功能已开放的角色
// 在圣域关闭时跳过本轮，不回退仙界/主服。
func TestPilgrimageSaintAreaAuto_SkipsWhenFeatureOpen(t *testing.T) {
	cfg := farming.DefaultPilgrimageConfig()
	cfg.SaintAreaFallback = farming.SaintAreaAuto
	calls := runPilgrimageMock(t, cfg,
		[]int32{protocol.FunctionIDSaintArea, protocol.FunctionIDFaction},
		protocol.SaintAreaStatusClose)
	if calls != 0 {
		t.Fatalf("圣域功能已开放时不应回退主服取经, 实际调用 %d 次", calls)
	}
}

// TestPilgrimageSaintAreaAuto_FallsBackWhenFeatureClosed 验证未开放圣域功能的
// 角色 (取经仍在仙界/主服) 在圣域不可用时回退传统链路。
func TestPilgrimageSaintAreaAuto_FallsBackWhenFeatureClosed(t *testing.T) {
	cfg := farming.DefaultPilgrimageConfig()
	cfg.SaintAreaFallback = farming.SaintAreaAuto
	calls := runPilgrimageMock(t, cfg,
		[]int32{protocol.FunctionIDFaction},
		protocol.SaintAreaStatusClose)
	if calls != 1 {
		t.Fatalf("圣域功能未开放时应回退主服取经 1 次, 实际 %d 次", calls)
	}
}

// TestPilgrimageSaintAreaForceOnly_SkipsWithoutFeature 验证显式固定圣域
// (force_saint_area=true) 时，即使圣域功能未开放也绝不回退。
func TestPilgrimageSaintAreaForceOnly_SkipsWithoutFeature(t *testing.T) {
	cfg := farming.DefaultPilgrimageConfig()
	cfg.SaintAreaFallback = farming.SaintAreaForceOnly
	calls := runPilgrimageMock(t, cfg,
		[]int32{protocol.FunctionIDFaction},
		protocol.SaintAreaStatusClose)
	if calls != 0 {
		t.Fatalf("固定圣域模式不应回退主服取经, 实际调用 %d 次", calls)
	}
}

// TestPilgrimageSaintAreaAllowFallback_AlwaysFallsBack 验证显式允许回退
// (force_saint_area=false) 时，即使圣域功能已开放也回退传统链路。
func TestPilgrimageSaintAreaAllowFallback_AlwaysFallsBack(t *testing.T) {
	cfg := farming.DefaultPilgrimageConfig()
	cfg.SaintAreaFallback = farming.SaintAreaAllowFallback
	calls := runPilgrimageMock(t, cfg,
		[]int32{protocol.FunctionIDSaintArea},
		protocol.SaintAreaStatusClose)
	if calls != 1 {
		t.Fatalf("允许回退模式应回退主服取经 1 次, 实际 %d 次", calls)
	}
}