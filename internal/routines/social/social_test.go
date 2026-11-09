// Package social 提供仙盟、圣盟、住宅与好友互动的帮派社交自动化玩法。
//
// @author Ateng
// @since 2026-10-09
package social

import (
	"context"
	"net"
	"sync/atomic"
	"testing"
	"time"

	"sxd-pie-ng/internal/client"
	"sxd-pie-ng/internal/protocol"
	"sxd-pie-ng/internal/scheduler"
)

func TestSocialRoutines_Execute(t *testing.T) {
	c1, c2 := net.Pipe()
	defer c1.Close()
	defer c2.Close()

	go func() {
		buf := make([]byte, 1024)
		for {
			_, err := c2.Read(buf)
			if err != nil {
				return
			}
		}
	}()

	session := client.NewRoleSession(client.SessionConfig{
		RoleID:   "role_test_social",
		RoleName: "测试社交角色",
		Dialer: func(ctx context.Context, network, addr string) (net.Conn, error) {
			return c1, nil
		},
	})
	_ = session.Start(context.Background())
	defer session.Close()

	time.Sleep(50 * time.Millisecond)

	r1 := NewGuildActivitiesRoutine()
	if dr, ok := r1.(scheduler.DetailedRoutine); ok {
		if dr.Domain() != "social" {
			t.Fatalf("领域不匹配: %s", dr.Domain())
		}
	}
	if err := r1.Execute(context.Background(), session, nil); err != nil {
		t.Fatalf("仙盟任务执行失败: %v", err)
	}

	r2 := NewHomesteadRoutine()
	if err := r2.Execute(context.Background(), session, nil); err != nil {
		t.Fatalf("住宅任务执行失败: %v", err)
	}

	r2b := NewMarryHomeRoutine()
	if r2b.Name() != "marry_home" {
		t.Fatalf("家园奖励任务名称不匹配: %s", r2b.Name())
	}
	// 该测试会话为无响应的管道，只验证不 panic/不返回错误。
	if err := r2b.Execute(context.Background(), session, nil); err != nil {
		t.Fatalf("家园奖励任务执行失败: %v", err)
	}

	t.Run("结缘宝箱每批最多10个配置", func(t *testing.T) {
		cfg := JieYuanBoxConfig{MaxBatchSize: 10}
		r3 := NewJieYuanBoxRoutine(cfg)
		if r3.Name() != "jieyuan_box" {
			t.Fatalf("结缘宝箱名称不匹配: %s", r3.Name())
		}
		if err := r3.Execute(context.Background(), session, nil); err != nil {
			t.Fatalf("结缘宝箱任务执行异常: %v", err)
		}
	})
}

// TestFameBlessingRoutine_Execute 覆盖免费白檀香膜拜成功路径。
func TestFameBlessingRoutine_Execute(t *testing.T) {
	session := client.NewRoleSession(client.SessionConfig{
		RoleID:     "role_test_fame_blessing",
		RoleName:   "测试膜拜角色",
		ServerAddr: "mock",
	})
	if err := session.Start(context.Background()); err != nil {
		t.Fatalf("启动测试会话失败: %v", err)
	}
	defer session.Close()
	time.Sleep(100 * time.Millisecond)

	r := NewFameBlessingRoutine()
	if err := r.Execute(context.Background(), session, nil); err != nil {
		t.Fatalf("膜拜任务执行失败: %v", err)
	}
}

// TestFameBlessingRoutine_FunctionClosedSkips 验证功能列表为空 (膜拜未开放) 时，
// 不再发送任何膜拜业务请求。
func TestFameBlessingRoutine_FunctionClosedSkips(t *testing.T) {
	c1, c2 := net.Pipe()
	defer c1.Close()
	defer c2.Close()

	actions := make(chan uint32, 8)
	go func() {
		for {
			pkt, err := protocol.ReadPacket(c2)
			if err != nil {
				return
			}
			actions <- pkt.ActionID
			if pkt.ActionID == protocol.ActionIDPlayerGetFunctions {
				// 功能列表为空：膜拜功能未开放。
				w := protocol.NewWriter()
				w.WriteUint16(0)
				_ = protocol.WritePacket(c2, protocol.NewPacket(
					protocol.ActionIDPlayerGetFunctions, w.Bytes(),
				))
			}
		}
	}()

	session := client.NewRoleSession(client.SessionConfig{
		RoleID:   "role_test_fame_blessing_closed",
		RoleName: "测试膜拜未开放角色",
		Dialer: func(ctx context.Context, network, addr string) (net.Conn, error) {
			return c1, nil
		},
		Authenticator: func(*client.RoleSession) error { return nil },
	})
	if err := session.Start(context.Background()); err != nil {
		t.Fatalf("启动测试会话失败: %v", err)
	}
	defer session.Close()
	time.Sleep(50 * time.Millisecond)

	if err := NewFameBlessingRoutine().Execute(context.Background(), session, nil); err != nil {
		t.Fatalf("功能未开放时执行不应返回错误: %v", err)
	}

	select {
	case got := <-actions:
		if got != protocol.ActionIDPlayerGetFunctions {
			t.Fatalf("期望先发送功能列表查询, 实际 0x%08X", got)
		}
	case <-time.After(time.Second):
		t.Fatal("未收到功能列表查询请求")
	}
	select {
	case got := <-actions:
		t.Fatalf("功能未开放时仍发送了业务请求 0x%08X", got)
	case <-time.After(100 * time.Millisecond):
	}
}

// TestFriendshipRoutine_Execute 覆盖好友来信自动阅读并领取奖励路径。
func TestFriendshipRoutine_Execute(t *testing.T) {
	session := client.NewRoleSession(client.SessionConfig{
		RoleID:     "role_test_friendship",
		RoleName:   "测试好友来信角色",
		ServerAddr: "mock",
	})
	if err := session.Start(context.Background()); err != nil {
		t.Fatalf("启动测试会话失败: %v", err)
	}
	defer session.Close()
	time.Sleep(100 * time.Millisecond)

	r := NewFriendshipRoutine()
	if err := r.Execute(context.Background(), session, nil); err != nil {
		t.Fatalf("好友来信任务执行失败: %v", err)
	}
}

// TestGuildActivitiesRoutine_Execute 覆盖仙盟开放状态与入盟检测路径。
func TestGuildActivitiesRoutine_Execute(t *testing.T) {
	session := client.NewRoleSession(client.SessionConfig{
		RoleID: "role_test_guild", RoleName: "测试仙盟角色", ServerAddr: "mock",
	})
	if err := session.Start(context.Background()); err != nil {
		t.Fatalf("启动测试会话失败: %v", err)
	}
	defer session.Close()
	time.Sleep(100 * time.Millisecond)
	if err := NewGuildActivitiesRoutine().Execute(context.Background(), session, nil); err != nil {
		t.Fatalf("仙盟任务执行失败: %v", err)
	}
}

// TestSacredAllianceRoutine_Execute 覆盖圣盟赛季探测路径。
func TestSacredAllianceRoutine_Execute(t *testing.T) {
	session := client.NewRoleSession(client.SessionConfig{
		RoleID: "role_test_alliance", RoleName: "测试圣盟角色", ServerAddr: "mock",
	})
	if err := session.Start(context.Background()); err != nil {
		t.Fatalf("启动测试会话失败: %v", err)
	}
	defer session.Close()
	time.Sleep(100 * time.Millisecond)
	if err := NewSacredAllianceRoutine().Execute(context.Background(), session, nil); err != nil {
		t.Fatalf("圣盟任务执行失败: %v", err)
	}
}

// TestHomesteadRoutine_Execute 覆盖住宅状态探测路径。
func TestHomesteadRoutine_Execute(t *testing.T) {
	session := client.NewRoleSession(client.SessionConfig{
		RoleID: "role_test_homestead", RoleName: "测试住宅角色", ServerAddr: "mock",
	})
	if err := session.Start(context.Background()); err != nil {
		t.Fatalf("启动测试会话失败: %v", err)
	}
	defer session.Close()
	time.Sleep(100 * time.Millisecond)
	if err := NewHomesteadRoutine().Execute(context.Background(), session, nil); err != nil {
		t.Fatalf("住宅任务执行失败: %v", err)
	}
}

// TestMarryHomeRoutine_SkipsWhenFunctionClosed 验证家园功能未开放时
// 不再发送会被服务端静默丢弃的 Module 210 请求。
func TestMarryHomeRoutine_SkipsWhenFunctionClosed(t *testing.T) {
	serverConn, clientConn := net.Pipe()
	t.Cleanup(func() {
		_ = serverConn.Close()
		_ = clientConn.Close()
	})

	var cakeCalls int32
	go func() {
		for {
			pkt, err := protocol.ReadPacket(serverConn)
			if err != nil {
				return
			}
			switch pkt.ActionID {
			case protocol.ActionIDPlayerGetFunctions:
				// 开放列表中不含家园 131。
				_ = protocol.WritePacket(serverConn,
					protocol.BuildPlayerFunctionsResultPacket([]int32{protocol.FunctionIDFaction}))
			case protocol.ActionIDMarryHomeGetCake:
				atomic.AddInt32(&cakeCalls, 1)
			}
		}
	}()

	session := client.NewRoleSession(client.SessionConfig{
		RoleID:     "role_test_marry_home_closed",
		RoleName:   "测试家园角色",
		ServerAddr: "mock-pipe",
		Dialer: func(ctx context.Context, network, addr string) (net.Conn, error) {
			return clientConn, nil
		},
		Authenticator: func(*client.RoleSession) error { return nil },
	})
	if err := session.Start(context.Background()); err != nil {
		t.Fatalf("启动测试会话失败: %v", err)
	}
	defer session.Close()
	deadline := time.Now().Add(2 * time.Second)
	for session.State() != client.StateActive && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}

	if err := NewMarryHomeRoutine().Execute(context.Background(), session, nil); err != scheduler.ErrAttemptsExhausted {
		t.Fatalf("家园未开放应挂起至次日, 实际: %v", err)
	}
	if got := atomic.LoadInt32(&cakeCalls); got != 0 {
		t.Fatalf("家园未开放时不应发送蛋糕领取请求, 实际 %d 次", got)
	}
}

// TestMarryHomeRoutine_SkipsWhenNotMarried 验证未结婚角色在发出会被静默丢弃的
// get_cake 之前就由住宅状态查询拦截，避免固定 8s 空等。
func TestMarryHomeRoutine_SkipsWhenNotMarried(t *testing.T) {
	serverConn, clientConn := net.Pipe()
	t.Cleanup(func() {
		_ = serverConn.Close()
		_ = clientConn.Close()
	})

	var cakeCalls int32
	go func() {
		for {
			pkt, err := protocol.ReadPacket(serverConn)
			if err != nil {
				return
			}
			switch pkt.ActionID {
			case protocol.ActionIDPlayerGetFunctions:
				_ = protocol.WritePacket(serverConn,
					protocol.BuildPlayerFunctionsResultPacket([]int32{protocol.FunctionIDMarryHome}))
			case protocol.ActionIDMarryHomeSelfInfo:
				_ = protocol.WritePacket(serverConn,
					protocol.BuildMarryHomeSelfInfoResultPacket(protocol.MarryHomeResultNonMarry))
			case protocol.ActionIDMarryHomeGetCake:
				atomic.AddInt32(&cakeCalls, 1)
			}
		}
	}()

	session := client.NewRoleSession(client.SessionConfig{
		RoleID:     "role_test_marry_home_unmarried",
		RoleName:   "测试未结婚角色",
		ServerAddr: "mock-pipe",
		Dialer: func(ctx context.Context, network, addr string) (net.Conn, error) {
			return clientConn, nil
		},
		Authenticator: func(*client.RoleSession) error { return nil },
	})
	if err := session.Start(context.Background()); err != nil {
		t.Fatalf("启动测试会话失败: %v", err)
	}
	defer session.Close()
	deadline := time.Now().Add(2 * time.Second)
	for session.State() != client.StateActive && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}

	if err := NewMarryHomeRoutine().Execute(context.Background(), session, nil); err != scheduler.ErrAttemptsExhausted {
		t.Fatalf("未结婚应挂起至次日, 实际: %v", err)
	}
	if got := atomic.LoadInt32(&cakeCalls); got != 0 {
		t.Fatalf("未结婚时不应发送蛋糕领取请求, 实际 %d 次", got)
	}
}

// TestMarryHomeRoutine_Execute 使用内置 mock 验证家园奖励领取链路。
func TestMarryHomeRoutine_Execute(t *testing.T) {
	session := client.NewRoleSession(client.SessionConfig{
		RoleID:     "role_test_marry_home",
		RoleName:   "测试家园角色",
		ServerAddr: "mock",
	})
	if err := session.Start(context.Background()); err != nil {
		t.Fatalf("启动测试会话失败: %v", err)
	}
	defer session.Close()
	time.Sleep(100 * time.Millisecond)

	if err := NewMarryHomeRoutine().Execute(context.Background(), session, nil); err != scheduler.ErrAttemptsExhausted {
		t.Fatalf("领取成功后应挂起至次日, 实际: %v", err)
	}
	if got := session.LastActionID(); got != protocol.ActionIDMarryHomeGetCake {
		t.Errorf("期望最后动作 0x%08X, 实际 0x%08X", protocol.ActionIDMarryHomeGetCake, got)
	}
}

// TestSectMonsterRoutine_NoSectSession 验证无仙盟城镇会话时安全跳过。
func TestSectMonsterRoutine_NoSectSession(t *testing.T) {
	session := client.NewRoleSession(client.SessionConfig{
		RoleID:     "role_test_sect_monster",
		RoleName:   "测试宗门灵兽角色",
		ServerAddr: "mock",
	})
	if err := session.Start(context.Background()); err != nil {
		t.Fatalf("启动测试会话失败: %v", err)
	}
	defer session.Close()
	time.Sleep(100 * time.Millisecond)

	r := NewSectMonsterRoutine()
	if r.Name() != "sect_monster" {
		t.Fatalf("任务名称不匹配: %s", r.Name())
	}
	if err := r.Execute(context.Background(), session, nil); err != nil {
		t.Fatalf("宗门灵兽任务应安全跳过, 实际: %v", err)
	}
}
