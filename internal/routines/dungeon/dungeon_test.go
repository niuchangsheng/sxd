// Package dungeon_test 针对关卡扫荡与挑战玩法的端到端业务闭环进行单元测试。
//
// @author Ateng
// @since 2026-10-08
package dungeon_test

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"sxd-pie-ng/internal/client"
	"sxd-pie-ng/internal/dictionary"
	"sxd-pie-ng/internal/protocol"
	"sxd-pie-ng/internal/routines/dungeon"
	"sxd-pie-ng/internal/scheduler"
)

func createTestSession(t *testing.T, initialStamina int) (*client.RoleSession, net.Conn) {
	c1, c2 := net.Pipe()

	cfg := client.SessionConfig{
		RoleID:     "test-role-sweep",
		RoleName:   "梦一场",
		ServerAddr: "mock",
		Dialer: func(ctx context.Context, network, addr string) (net.Conn, error) {
			return c1, nil
		},
	}
	sess := client.NewRoleSession(cfg)
	sess.SetStamina(initialStamina)
	_ = sess.Start(context.Background())

	deadline := time.Now().Add(2 * time.Second)
	for sess.State() != client.StateActive && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}

	return sess, c2
}

func startMockDungeonServer(srvConn net.Conn, handler func(pkt *protocol.Packet) bool) {
	go func() {
		for {
			pkt, err := protocol.ReadPacket(srvConn)
			if err != nil {
				return
			}
			if handler != nil && handler(pkt) {
				continue
			}
			if pkt.ActionID == protocol.ActionTownMissionActive {
				_ = protocol.WritePacket(srvConn, protocol.NewPacket(protocol.ActionTownMissionActive, nil))
				continue
			}
			if pkt.ActionID == protocol.ActionIDItemGetPackList {
				// 默认返回空背包 (容量 30, 空格 30, 物品 0)
				buf := new(bytes.Buffer)
				_ = binary.Write(buf, binary.BigEndian, int16(30))
				_ = binary.Write(buf, binary.BigEndian, int32(30))
				_ = binary.Write(buf, binary.BigEndian, uint16(0))
				_ = binary.Write(buf, binary.BigEndian, int16(10))
				_ = binary.Write(buf, binary.BigEndian, uint16(0))
				_ = protocol.WritePacket(srvConn, protocol.NewPacket(protocol.ActionIDItemGetPackList, buf.Bytes()))
				continue
			}
			if pkt.ActionID == protocol.ActionIDItemUseGrid {
				// 默认使用成功
				_ = protocol.WritePacket(srvConn, protocol.NewPacket(protocol.ActionIDItemUseGrid, []byte{protocol.ItemActionSuccess}))
				continue
			}
			if pkt.ActionID == protocol.ActionHeroMissionList {
				w := protocol.NewWriter()
				w.WriteUint32(0)
				w.WriteUint8(0)
				w.WriteUint32(0)
				w.WriteUint16(0)
				w.WriteUint16(0)
				_ = protocol.WritePacket(srvConn, protocol.NewPacket(protocol.ActionHeroMissionList, w.Bytes()))
				continue
			}
			if pkt.ActionID == protocol.ActionIDPracticeGetStatus {
				w := protocol.NewWriter()
				w.WriteBytes([]byte{'F', 'P'})
				w.WriteUint16(0)
				w.WriteUint8(0)
				w.WriteUint32(0)
				w.WriteUint32(0)
				_ = protocol.WritePacket(srvConn, protocol.NewPacket(protocol.ActionIDPracticeGetStatus, w.Bytes()))
				continue
			}
			if pkt.ActionID == protocol.ActionIDPracticeStart {
				req, _ := protocol.ParseStartPracticeRequest(pkt.Payload)
				mid := int32(43)
				cnt := int16(1)
				if req != nil {
					mid = req.MissionID
					cnt = req.Count
				}
				resPkt, _ := protocol.BuildStartPracticeResultPacket(protocol.StartPracticeResult{
					Result:    protocol.PracticeResultSuccess,
					MissionID: mid,
					Count:     cnt,
				})
				_ = protocol.WritePacket(srvConn, resPkt)
				continue
			}
			if pkt.ActionID == protocol.ActionIDPracticeQuickly {
				resPkt, _ := protocol.BuildQuicklyResultPacket(protocol.QuicklyResult{
					Result: protocol.PracticeResultSuccess,
					Count:  1,
				})
				_ = protocol.WritePacket(srvConn, resPkt)
				continue
			}
			if pkt.ActionID == protocol.ActionMissionSweep {
				req, _ := protocol.ParseSweepRequest(pkt.Payload)
				resPkt, _ := protocol.BuildSweepResultPacket(protocol.SweepResult{
					Success:   true,
					MissionID: req.MissionID,
					Times:     req.Times,
					CostPower: int(req.Times) * 5,
					GainExp:   int64(req.Times) * 2500,
					GainCoins: int64(req.Times) * 12000,
					Message:   "扫荡完成",
				})
				_ = protocol.WritePacket(srvConn, resPkt)
			}
		}
	}()
}

func TestDungeonSweep_NormalExecution(t *testing.T) {
	sess, srvConn := createTestSession(t, 70) // 70 体力，扫 10 次消耗 50，剩余 20
	defer sess.Close()
	defer srvConn.Close()

	// 模拟服务端接收扫荡包并响应结算回包
	pktCh := make(chan *protocol.Packet, 10)
	startMockDungeonServer(srvConn, func(pkt *protocol.Packet) bool {
		if pkt.ActionID == protocol.ActionMissionSweep {
			select {
			case pktCh <- pkt:
			default:
			}
		}
		return false
	})

	dictRepo, err := dictionary.NewRepository("")
	if err != nil {
		t.Logf("本地未找到 Pieb.db，将使用降级关卡模式测试: %v", err)
	}

	routine := dungeon.NewDungeonSweepRoutine(dictRepo, dungeon.SweepConfig{
		Pipeline: []string{"sweep_highest_main"},
	})
	ctx := context.Background()

	err = routine.Execute(ctx, sess, nil)
	if err != nil {
		t.Fatalf("执行关卡扫荡失败: %v", err)
	}

	// 验证向服务器发送了 ActionMissionSweep 封包
	select {
	case pkt := <-pktCh:
		if pkt.ActionID != protocol.ActionMissionSweep {
			t.Errorf("期望 ActionID 0x%04X, 实际 0x%04X", protocol.ActionMissionSweep, pkt.ActionID)
		}
		req, err := protocol.ParseSweepRequest(pkt.Payload)
		if err != nil {
			t.Fatalf("解析服务端收到的请求包失败: %v", err)
		}
		if req.Times != 1 { // 对齐真实网关单次请求发 1
			t.Errorf("期望单次请求扫荡 1 次, 实际 %d 次", req.Times)
		}
	default:
		t.Error("未收到向服务端发送的扫荡封包")
	}

	// 验证体力扣除 (70 - 50 = 20)
	remStamina := sess.GetStamina()
	if remStamina != 20 {
		t.Errorf("期望剩余体力为 20, 实际为 %d", remStamina)
	}

	// 验证收益增加 (初始 1,000,000 + 10 * 12000 = 1,120,000)
	state := sess.GetPlayerState()
	if state.Coins <= 1000000 {
		t.Errorf("期望获得铜钱收益, 实际为 %d", state.Coins)
	}
}

func TestDungeonSweep_StaminaDepleted_SmartCooling(t *testing.T) {
	sess, srvConn := createTestSession(t, 2) // 体力仅有 2 点，不足 5 点
	defer sess.Close()
	defer srvConn.Close()

	startMockDungeonServer(srvConn, nil)

	routine := dungeon.NewDungeonSweepRoutine(nil, dungeon.SweepConfig{
		Pipeline: []string{"sweep_elite"},
	})
	ctx := context.Background()

	err := routine.Execute(ctx, sess, nil)
	if !errors.Is(err, scheduler.ErrStaminaDepleted) {
		t.Fatalf("期望返回 ErrStaminaDepleted 触发熔断, 实际返回: %v", err)
	}
}

func TestDungeonSweep_BagFull(t *testing.T) {
	sess, srvConn := createTestSession(t, 100)
	defer sess.Close()
	defer srvConn.Close()

	// 模拟背包已满
	sess.UpdatePlayerState(func(ps *client.PlayerState) {
		ps.BagCapacity = 0
	})

	routine := dungeon.NewDungeonSweepRoutine(nil, dungeon.SweepConfig{
		Pipeline: []string{"sweep_elite"},
	})
	ctx := context.Background()

	err := routine.Execute(ctx, sess, nil)
	if !errors.Is(err, scheduler.ErrBagFull) {
		t.Fatalf("期望返回 ErrBagFull, 实际返回: %v", err)
	}
}

// TestDungeonSweep_201Stamina_MultiBatchDepletion 验证角色拥有实机 201 点体力时，多轮批量扫荡消耗至耗尽熔断的完整闭环。
func TestDungeonSweep_201Stamina_MultiBatchDepletion(t *testing.T) {
	// 初始 201 点体力，单次最大 10 轮 (50 点体力)
	sess, srvConn := createTestSession(t, 201)
	defer sess.Close()
	defer srvConn.Close()

	// 服务端异步接收通道并响应扫荡结果
	startMockDungeonServer(srvConn, nil)

	routine := dungeon.NewDungeonSweepRoutine(nil, dungeon.SweepConfig{
		MaxBatchTimes: 10,
		Pipeline:      []string{"sweep_highest_main"},
	})
	ctx := context.Background()

	// 前 3 轮正常消耗 50 点体力，剩余体力充足 (>5 点)，返回 nil
	for round := 0; round < 3; round++ {
		err := routine.Execute(ctx, sess, nil)
		if err != nil {
			t.Fatalf("第 %d 轮扫荡执行失败: %v", round+1, err)
		}
	}
	if rem := sess.GetStamina(); rem != 51 {
		t.Fatalf("前 3 轮扫荡后期望剩余 51 点体力, 实际 %d", rem)
	}

	// 第 4 轮：从 51 点消耗 50 点后剩余 1 点，完成扫荡并即时触发 ErrStaminaDepleted 告知调度器进入冷却
	err := routine.Execute(ctx, sess, nil)
	if !errors.Is(err, scheduler.ErrStaminaDepleted) {
		t.Fatalf("第 4 轮扫荡消耗后期望返回 ErrStaminaDepleted, 实际返回: %v", err)
	}
	if rem := sess.GetStamina(); rem != 1 {
		t.Errorf("第 4 轮扫荡后期望剩余体力 1, 实际为 %d", rem)
	}

	// 第 5 轮：当剩余 1 点体力时再次调度，前置拦截直接拒绝并返回 ErrStaminaDepleted
	err = routine.Execute(ctx, sess, nil)
	if !errors.Is(err, scheduler.ErrStaminaDepleted) {
		t.Fatalf("第 5 轮前置拦截期望返回 ErrStaminaDepleted, 实际返回: %v", err)
	}

	// 最终体力仍应保持为 1 点
	if rem := sess.GetStamina(); rem != 1 {
		t.Errorf("熔断后期望体力仍为 1, 实际为 %d", rem)
	}

	// 验证 4 轮累计消耗体力 = 4 * 50 = 200 点
	totalCost := 201 - sess.GetStamina()
	if totalCost != 200 {
		t.Errorf("期望累计消耗体力 200, 实际消耗 %d", totalCost)
	}

	// 验证 4 轮累计经验收益 = 4 * 10 * 2500 = 100,000
	// 验证 4 轮累计铜钱收益 = 4 * 10 * 12000 = 480,000
	state := sess.GetPlayerState()
	expectedCoins := int64(36226117234 + 480000)
	if state.Coins != expectedCoins {
		t.Errorf("期望铜钱累计达到 %d, 实际为 %d", expectedCoins, state.Coins)
	}
}

// TestDungeonSweep_CustomBatchAndExactDeduction 验证自定义小批次与零头体力的精准扣除。
func TestDungeonSweep_CustomBatchAndExactDeduction(t *testing.T) {
	sess, srvConn := createTestSession(t, 23) // 23 点体力，配置最大批次为 3 次 (15 点)
	defer sess.Close()
	defer srvConn.Close()

	startMockDungeonServer(srvConn, nil)

	routine := dungeon.NewDungeonSweepRoutine(nil, dungeon.SweepConfig{
		MaxBatchTimes: 3,
		Pipeline:      []string{"sweep_highest_main"},
	})
	ctx := context.Background()

	// 第 1 轮：23 体力可扫 4 次，但被 MaxBatchTimes=3 限制，扫 3 次消耗 15 点，剩余 8 点 (>5)，返回 nil
	err := routine.Execute(ctx, sess, nil)
	if err != nil {
		t.Fatalf("第 1 轮执行失败: %v", err)
	}
	if rem := sess.GetStamina(); rem != 8 {
		t.Errorf("第 1 轮期望剩余 8 点体力, 实际 %d", rem)
	}

	// 第 2 轮：剩余 8 点体力，扫 1 次 (消耗 5 点)，剩余 3 点 (<5 点)，完成扫荡并即时触发 ErrStaminaDepleted
	err = routine.Execute(ctx, sess, nil)
	if !errors.Is(err, scheduler.ErrStaminaDepleted) {
		t.Fatalf("第 2 轮期望返回 ErrStaminaDepleted, 实际返回: %v", err)
	}
	if rem := sess.GetStamina(); rem != 3 {
		t.Errorf("第 2 轮期望剩余 3 点体力, 实际 %d", rem)
	}

	// 第 3 轮：剩余 3 点不足 5 点，前置熔断拦截
	err = routine.Execute(ctx, sess, nil)
	if !errors.Is(err, scheduler.ErrStaminaDepleted) {
		t.Fatalf("第 3 轮期望返回 ErrStaminaDepleted, 实际返回: %v", err)
	}
}

// TestDungeonSweep_ConsumeStamina_BoundaryAndConcurrency 验证体力扣除的边界条件与多协程并发安全。
func TestDungeonSweep_ConsumeStamina_BoundaryAndConcurrency(t *testing.T) {
	sess, srvConn := createTestSession(t, 100)
	defer sess.Close()
	defer srvConn.Close()

	// 1. 边界超扣测试: 当前 100 点体力，尝试扣除 105 点应被拒绝且体力不变
	_, err := sess.ConsumeStamina(105)
	if err == nil {
		t.Fatal("期望扣除超出体力时返回错误，实际成功")
	}
	if rem := sess.GetStamina(); rem != 100 {
		t.Fatalf("超扣失败后期望体力仍为 100, 实际为 %d", rem)
	}

	// 2. 并发扣除测试: 25 个并发协程，每个尝试扣除 5 点体力 (总需求 125 点)
	// 预期结果: 恰好 20 个协程成功扣除，5 个协程因体力不足失败，最终剩余体力精确为 0
	var wg sync.WaitGroup
	var successCount int64
	var failCount int64

	for i := 0; i < 25; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := sess.ConsumeStamina(5)
			if err == nil {
				atomic.AddInt64(&successCount, 1)
			} else {
				atomic.AddInt64(&failCount, 1)
			}
		}()
	}

	wg.Wait()

	if successCount != 20 {
		t.Errorf("期望成功扣除 20 次, 实际成功 %d 次", successCount)
	}
	if failCount != 5 {
		t.Errorf("期望失败 5 次, 实际失败 %d 次", failCount)
	}
	if rem := sess.GetStamina(); rem != 0 {
		t.Errorf("并发扣除后最终期望体力为 0, 实际为 %d", rem)
	}
}

// TestDungeonSweep_ServerPropUpdateSync 验证收到服务器 0x0300 广播推送时体力值的权威动态同步。
func TestDungeonSweep_ServerPropUpdateSync(t *testing.T) {
	sess, srvConn := createTestSession(t, 201)
	defer sess.Close()
	defer srvConn.Close()

	// 注册 0x0300 属性同步处理器
	sess.RegisterHandler(protocol.ActionIDPlayerUpdateData, func(p *protocol.Packet) {
		r := protocol.NewReader(p.Payload)
		for r.Remaining() >= 5 {
			prop, err := r.ReadUint8()
			if err != nil {
				break
			}
			val, err := r.ReadInt32()
			if err != nil {
				break
			}
			if prop == protocol.PlayerPropPower {
				sess.UpdatePlayerState(func(ps *client.PlayerState) {
					ps.Stamina = int(val)
				})
			}
		}
	})

	// 模拟服务端向客户端推送 0x0300 封包，通知体力更新为 146 点
	w := protocol.NewWriter()
	w.WriteUint8(protocol.PlayerPropPower)
	w.WriteInt32(146)
	pkt := protocol.NewPacket(protocol.ActionIDPlayerUpdateData, w.Bytes())

	raw, err := pkt.Marshal()
	if err != nil {
		t.Fatalf("Marshal 0x0300 失败: %v", err)
	}
	if _, err := srvConn.Write(raw); err != nil {
		t.Fatalf("服务端写入 0x0300 封包失败: %v", err)
	}

	// 等待客户端消费回包并同步
	deadline := time.Now().Add(2 * time.Second)
	for sess.GetStamina() != 146 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}

	if rem := sess.GetStamina(); rem != 146 {
		t.Errorf("期望体力同步为 146, 实际为 %d", rem)
	}
}

// TestDungeonSweep_ServerResponse_RewardSync 验证接收到服务端真实扫荡结算回包时的收益同步与精准扣减
func TestDungeonSweep_ServerResponse_RewardSync(t *testing.T) {
	sess, srvConn := createTestSession(t, 100)
	defer sess.Close()
	defer srvConn.Close()

	initialCoins := sess.GetPlayerState().Coins

	// 模拟服务端接收扫荡并回复真实结算包
	startMockDungeonServer(srvConn, func(pkt *protocol.Packet) bool {
		if pkt.ActionID == protocol.ActionMissionSweep {
			req, _ := protocol.ParseSweepRequest(pkt.Payload)
			res := protocol.SweepResult{
				Success:   true,
				MissionID: req.MissionID,
				Times:     req.Times,
				CostPower: int(req.Times) * 5,
				GainExp:   int64(req.Times) * 3500,
				GainCoins: int64(req.Times) * 18000,
				Message:   "扫荡完成",
			}
			respPkt, _ := protocol.BuildSweepResultPacket(res)
			_ = protocol.WritePacket(srvConn, respPkt)
			return true
		}
		return false
	})

	routine := dungeon.NewDungeonSweepRoutine(nil, dungeon.SweepConfig{
		Pipeline: []string{"sweep_highest_main"},
	})
	err := routine.Execute(context.Background(), sess, nil)
	if err != nil {
		t.Fatalf("扫荡执行失败: %v", err)
	}

	// 验证体力扣减
	if rem := sess.GetStamina(); rem != 50 {
		t.Errorf("期望剩余体力 50, 实际为 %d", rem)
	}

	// 验证铜钱收益按服务端回包增加 180,000
	if currentCoins := sess.GetPlayerState().Coins; currentCoins != initialCoins+180000 {
		t.Errorf("期望铜钱增加 180,000, 实际为 %d (初始: %d)", currentCoins, initialCoins)
	}
}

// TestDungeonSweep_ServerRejection_StaminaDepleted 验证当服务端返回“体力不足”时触发语义智能熔断
func TestDungeonSweep_ServerRejection_StaminaDepleted(t *testing.T) {
	sess, srvConn := createTestSession(t, 50)
	defer sess.Close()
	defer srvConn.Close()

	startMockDungeonServer(srvConn, func(pkt *protocol.Packet) bool {
		if pkt.ActionID == protocol.ActionMissionSweep {
			req, _ := protocol.ParseSweepRequest(pkt.Payload)
			res := protocol.SweepResult{
				Success:   false,
				MissionID: req.MissionID,
				Times:     req.Times,
				Message:   "体力不足，扫荡失败",
			}
			respPkt, _ := protocol.BuildSweepResultPacket(res)
			_ = protocol.WritePacket(srvConn, respPkt)
			return true
		}
		return false
	})

	routine := dungeon.NewDungeonSweepRoutine(nil, dungeon.SweepConfig{
		Pipeline: []string{"sweep_highest_main"},
	})
	err := routine.Execute(context.Background(), sess, nil)
	if !errors.Is(err, scheduler.ErrStaminaDepleted) {
		t.Fatalf("期望服务端拒绝时触发 ErrStaminaDepleted, 实际返回: %v", err)
	}
}

// TestDungeonSweep_ServerRejection_BagFull 验证当服务端返回“背包已满”时触发背包语义熔断
func TestDungeonSweep_ServerRejection_BagFull(t *testing.T) {
	sess, srvConn := createTestSession(t, 50)
	defer sess.Close()
	defer srvConn.Close()

	startMockDungeonServer(srvConn, func(pkt *protocol.Packet) bool {
		if pkt.ActionID == protocol.ActionMissionSweep {
			req, _ := protocol.ParseSweepRequest(pkt.Payload)
			res := protocol.SweepResult{
				Success:   false,
				MissionID: req.MissionID,
				Times:     req.Times,
				Message:   "背包已满，无法放入掉落物品",
			}
			respPkt, _ := protocol.BuildSweepResultPacket(res)
			_ = protocol.WritePacket(srvConn, respPkt)
			return true
		}
		return false
	})

	routine := dungeon.NewDungeonSweepRoutine(nil, dungeon.SweepConfig{
		Pipeline: []string{"sweep_highest_main"},
	})
	err := routine.Execute(context.Background(), sess, nil)
	if !errors.Is(err, scheduler.ErrBagFull) {
		t.Fatalf("期望服务端拒绝时触发 ErrBagFull, 实际返回: %v", err)
	}
}

// TestDungeonSweep_HeroMission_FullWorkflow 验证 Module 111 英雄副本扫荡全流程 (列表->选定->扫荡->快速完成->界面关闭) 与真实体力资产结算
func TestDungeonSweep_HeroMission_FullWorkflow(t *testing.T) {
	sess, srvConn := createTestSession(t, 25) // 初始 25 点体力，扫 2 个副本实例各 1 次，消耗 10 点，剩余 15 点
	defer sess.Close()
	defer srvConn.Close()

	var selectCount atomic.Int32
	var sweepCount atomic.Int32
	var quickFinishCount atomic.Int32
	var closedFlag atomic.Bool

	startMockDungeonServer(srvConn, func(pkt *protocol.Packet) bool {
		switch pkt.ActionID {
		case protocol.ActionHeroMissionList:
			// 响应包含 2 个英雄副本的列表
			w := protocol.NewWriter()
			w.WriteUint32(0) // 保留头
			w.WriteUint8(2)  // 总可扫荡次数 2
			w.WriteUint32(10) // 消耗体力
			w.WriteUint16(0)  // 倒计时
			w.WriteUint16(2)  // 条数 2
			// 副本 1
			w.WriteUint32(27983) // InstanceID
			w.WriteUint16(43)    // MissionID
			w.WriteUint8(1)      // Times
			// 副本 2
			w.WriteUint32(27984) // InstanceID
			w.WriteUint16(44)    // MissionID
			w.WriteUint8(1)      // Times
			_ = protocol.WritePacket(srvConn, protocol.NewPacket(protocol.ActionHeroMissionList, w.Bytes()))
			return true

		case protocol.ActionHeroMissionSelect:
			selectCount.Add(1)
			// 确认选定 (00 00 01)
			_ = protocol.WritePacket(srvConn, protocol.NewPacket(protocol.ActionHeroMissionSelect, []byte{0x00, 0x00, 0x01}))
			return true

		case protocol.ActionHeroMissionSweep:
			sweepCount.Add(1)
			// 回包结算 (3B status + 4B instanceID + 2B missionID + 1B times ...)
			w := protocol.NewWriter()
			w.WriteBytes([]byte{0x00, 0x00, 0x01})
			w.WriteUint32(27983)
			w.WriteUint16(43)
			w.WriteUint8(1)
			_ = protocol.WritePacket(srvConn, protocol.NewPacket(protocol.ActionHeroMissionSweep, w.Bytes()))
			return true

		case protocol.ActionHeroMissionQuickFinish:
			quickFinishCount.Add(1)
			return true

		case protocol.ActionHeroMissionClose:
			closedFlag.Store(true)
			return true
		}
		return false
	})

	routine := dungeon.NewDungeonSweepRoutine(nil, dungeon.SweepConfig{
		Pipeline: []string{"sweep_elite"},
	})
	err := routine.Execute(context.Background(), sess, nil)
	if err != nil {
		t.Fatalf("执行英雄副本扫荡失败: %v", err)
	}

	if selectCount.Load() != 2 {
		t.Errorf("期望选定 2 次副本, 实际 %d 次", selectCount.Load())
	}
	if sweepCount.Load() != 2 {
		t.Errorf("期望扫荡 2 次, 实际 %d 次", sweepCount.Load())
	}
	if quickFinishCount.Load() != 2 {
		t.Errorf("期望快速完成 2 次, 实际 %d 次", quickFinishCount.Load())
	}
	if !closedFlag.Load() {
		t.Errorf("期望退出时发送关闭英雄副本界面封包 ActionHeroMissionClose")
	}

	// 验证体力扣减 (25 - 2*5 = 15)
	if rem := sess.GetStamina(); rem != 15 {
		t.Errorf("期望剩余体力 15, 实际 %d", rem)
	}
}

// TestDungeonSweep_Module25_PracticeQuickly 验证当启用 PreferPracticeQuickly 时，
// 发送 Module 25 (0x00190001) 开始扫荡并紧跟 Action 3 (0x00190003) 极速秒完成的完整闭环。
func TestDungeonSweep_Module25_PracticeQuickly(t *testing.T) {
	sess, srvConn := createTestSession(t, 20) // 20 点体力
	defer sess.Close()
	defer srvConn.Close()

	var m25StartCount atomic.Int32
	var m25QuicklyCount atomic.Int32

	startMockDungeonServer(srvConn, func(pkt *protocol.Packet) bool {
		switch pkt.ActionID {
		case protocol.ActionIDPracticeStart:
			m25StartCount.Add(1)
			req, _ := protocol.ParseStartPracticeRequest(pkt.Payload)
			mid := int32(854)
			if req != nil {
				mid = req.MissionID
			}
			resPkt, _ := protocol.BuildStartPracticeResultPacket(protocol.StartPracticeResult{
				Result:    protocol.PracticeResultSuccess,
				MissionID: mid,
				Count:     1,
			})
			_ = protocol.WritePacket(srvConn, resPkt)
			return true

		case protocol.ActionIDPracticeQuickly:
			m25QuicklyCount.Add(1)
			resPkt, _ := protocol.BuildQuicklyResultPacket(protocol.QuicklyResult{
				Result: protocol.PracticeResultSuccess,
				Count:  1,
			})
			_ = protocol.WritePacket(srvConn, resPkt)
			return true
		}
		return false
	})

	cfg := dungeon.SweepConfig{
		MaxBatchTimes:         2,
		PreferPracticeQuickly: true,
		Pipeline:              []string{"sweep_highest_main"},
	}
	routine := dungeon.NewDungeonSweepRoutine(nil, cfg)
	err := routine.Execute(context.Background(), sess, nil)
	if err != nil {
		t.Fatalf("执行 Module 25 秒扫荡失败: %v", err)
	}

	deadline := time.Now().Add(500 * time.Millisecond)
	for (m25StartCount.Load() < 2 || m25QuicklyCount.Load() < 2) && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}

	if m25StartCount.Load() != 2 {
		t.Errorf("期望调用 Module 25 开始扫荡 2 次, 实际 %d 次", m25StartCount.Load())
	}
	if m25QuicklyCount.Load() != 2 {
		t.Errorf("期望调用 Module 25 极速秒完成 2 次, 实际 %d 次", m25QuicklyCount.Load())
	}
	if rem := sess.GetStamina(); rem != 10 {
		t.Errorf("期望剩余体力 10 (20 - 2*5), 实际 %d", rem)
	}
}

// TestDungeonSweep_Module25_FallbackToLegacy 验证当 Module 25 秒扫荡返回受限状态码时，
// 自动无缝降级为 Module 35 普通扫荡协议执行自愈。
func TestDungeonSweep_Module25_FallbackToLegacy(t *testing.T) {
	sess, srvConn := createTestSession(t, 10)
	defer sess.Close()
	defer srvConn.Close()

	var m25Attempted atomic.Bool
	var legacyExecuted atomic.Bool

	startMockDungeonServer(srvConn, func(pkt *protocol.Packet) bool {
		switch pkt.ActionID {
		case protocol.ActionIDPracticeStart:
			m25Attempted.Store(true)
			// 模拟元宝不足或其他非成功状态码
			resPkt, _ := protocol.BuildStartPracticeResultPacket(protocol.StartPracticeResult{
				Result:    protocol.PracticeResultNotEnoughIngot,
				MissionID: 854,
				Count:     1,
			})
			_ = protocol.WritePacket(srvConn, resPkt)
			return true

		case protocol.ActionMissionSweep:
			legacyExecuted.Store(true)
			req, _ := protocol.ParseSweepRequest(pkt.Payload)
			resPkt, _ := protocol.BuildSweepResultPacket(protocol.SweepResult{
				Success:   true,
				MissionID: req.MissionID,
				Times:     req.Times,
				CostPower: 5,
				GainExp:   2500,
				GainCoins: 12000,
				Message:   "普通扫荡成功",
			})
			_ = protocol.WritePacket(srvConn, resPkt)
			return true
		}
		return false
	})

	cfg := dungeon.SweepConfig{
		MaxBatchTimes:         1,
		PreferPracticeQuickly: true,
		Pipeline:              []string{"sweep_highest_main"},
	}
	routine := dungeon.NewDungeonSweepRoutine(nil, cfg)
	err := routine.Execute(context.Background(), sess, nil)
	if err != nil {
		t.Fatalf("降级扫荡失败: %v", err)
	}

	if !m25Attempted.Load() {
		t.Errorf("期望前置尝试 Module 25 秒扫荡")
	}
	if !legacyExecuted.Load() {
		t.Errorf("期望自愈降级执行 Module 35 普通扫荡")
	}
	if rem := sess.GetStamina(); rem != 5 {
		t.Errorf("期望扣减 5 点体力剩余 5, 实际 %d", rem)
	}
}

// TestDungeonSweep_SemanticCoolingOff_Module25 验证 Module 25 明确返回体力不足或背包满时，
// 立即触发 ErrStaminaDepleted 或 ErrBagFull 语义智能熔断，杜绝无意义降级重试。
func TestDungeonSweep_SemanticCoolingOff_Module25(t *testing.T) {
	t.Run("StaminaDepleted_CoolingOff", func(t *testing.T) {
		sess, srvConn := createTestSession(t, 20)
		defer sess.Close()
		defer srvConn.Close()

		startMockDungeonServer(srvConn, func(pkt *protocol.Packet) bool {
			if pkt.ActionID == protocol.ActionIDPracticeStart {
				resPkt, _ := protocol.BuildStartPracticeResultPacket(protocol.StartPracticeResult{
					Result:    protocol.PracticeResultNotEnoughPower,
					MissionID: 854,
					Count:     1,
				})
				_ = protocol.WritePacket(srvConn, resPkt)
				return true
			}
			return false
		})

		cfg := dungeon.SweepConfig{
			MaxBatchTimes:         1,
			PreferPracticeQuickly: true,
			Pipeline:              []string{"sweep_highest_main"},
		}
		routine := dungeon.NewDungeonSweepRoutine(nil, cfg)
		err := routine.Execute(context.Background(), sess, nil)
		if !errors.Is(err, scheduler.ErrStaminaDepleted) {
			t.Fatalf("期望触发 ErrStaminaDepleted 语义熔断, 实际返回: %v", err)
		}
	})

	t.Run("BagFull_CoolingOff", func(t *testing.T) {
		sess, srvConn := createTestSession(t, 20)
		defer sess.Close()
		defer srvConn.Close()

		startMockDungeonServer(srvConn, func(pkt *protocol.Packet) bool {
			if pkt.ActionID == protocol.ActionIDPracticeStart {
				resPkt, _ := protocol.BuildStartPracticeResultPacket(protocol.StartPracticeResult{
					Result:    protocol.PracticeResultBagFull,
					MissionID: 854,
					Count:     1,
				})
				_ = protocol.WritePacket(srvConn, resPkt)
				return true
			}
			return false
		})

		cfg := dungeon.SweepConfig{
			MaxBatchTimes:         1,
			PreferPracticeQuickly: true,
			Pipeline:              []string{"sweep_highest_main"},
		}
		routine := dungeon.NewDungeonSweepRoutine(nil, cfg)
		err := routine.Execute(context.Background(), sess, nil)
		if !errors.Is(err, scheduler.ErrBagFull) {
			t.Fatalf("期望触发 ErrBagFull 语义熔断, 实际返回: %v", err)
		}
	})
}

// TestDungeonSweep_AutoReplenishStamina_Priority 验证主线/支线副本无体力时自动检查背包，
// 且严格按照 [包子 > 粽子 > 茶叶蛋] 优先级使用道具补充体力并完成扫荡。
func TestDungeonSweep_AutoReplenishStamina_Priority(t *testing.T) {
	t.Run("Priority1_SteamedBun", func(t *testing.T) {
		// 初始体力为 0，背包里同时有包子(Grid 1)、粽子(Grid 2)、茶叶蛋(Grid 3)
		sess, srvConn := createTestSession(t, 0)
		defer sess.Close()
		defer srvConn.Close()

		usedGridCh := make(chan int16, 5)

		startMockDungeonServer(srvConn, func(pkt *protocol.Packet) bool {
			if pkt.ActionID == protocol.ActionIDItemGetPackList {
				buf := new(bytes.Buffer)
				_ = binary.Write(buf, binary.BigEndian, int16(30))
				_ = binary.Write(buf, binary.BigEndian, int32(27))
				_ = binary.Write(buf, binary.BigEndian, uint16(3))

				// 格位 1: 包子 (ID 1488)
				_ = protocol.WriteItemRecord(buf, protocol.ItemRecord{
					PlayerItemID: 101,
					ItemID:       protocol.ItemIDSteamedBun,
					GridID:       1,
				})
				// 格位 2: 粽子 (ID 1444)
				_ = protocol.WriteItemRecord(buf, protocol.ItemRecord{
					PlayerItemID: 102,
					ItemID:       protocol.ItemIDRiceDumpling,
					GridID:       2,
				})
				// 格位 3: 茶叶蛋 (ID 2016)
				_ = protocol.WriteItemRecord(buf, protocol.ItemRecord{
					PlayerItemID: 103,
					ItemID:       protocol.ItemIDTeaEgg,
					GridID:       3,
				})

				_ = binary.Write(buf, binary.BigEndian, int16(10))
				_ = binary.Write(buf, binary.BigEndian, uint16(0))
				_ = protocol.WritePacket(srvConn, protocol.NewPacket(protocol.ActionIDItemGetPackList, buf.Bytes()))
				return true
			}

			if pkt.ActionID == protocol.ActionIDItemUseGrid {
				var gridID int16
				_ = binary.Read(bytes.NewReader(pkt.Payload), binary.BigEndian, &gridID)
				usedGridCh <- gridID
				_ = protocol.WritePacket(srvConn, protocol.NewPacket(protocol.ActionIDItemUseGrid, []byte{protocol.ItemActionSuccess}))
				return true
			}

			return false
		})

		cfg := dungeon.SweepConfig{
			Category:             dungeon.CategoryMainline,
			MaxBatchTimes:        1,
			AutoReplenishStamina: true,
		}
		routine := dungeon.NewDungeonSweepRoutine(nil, cfg)
		err := routine.Execute(context.Background(), sess, nil)
		if err != nil && !errors.Is(err, scheduler.ErrStaminaDepleted) {
			t.Fatalf("扫荡执行失败: %v", err)
		}

		select {
		case gridID := <-usedGridCh:
			if gridID != 1 {
				t.Errorf("期望最高优先级优先使用包子 (Grid 1), 实际使用了格位 %d", gridID)
			}
		case <-time.After(1 * time.Second):
			t.Fatal("未收到使用道具协议调用")
		}
	})

	t.Run("Priority2_RiceDumpling_WhenNoBun", func(t *testing.T) {
		// 背包中无包子，仅有粽子(Grid 2)和茶叶蛋(Grid 3)
		sess, srvConn := createTestSession(t, 0)
		defer sess.Close()
		defer srvConn.Close()

		usedGridCh := make(chan int16, 5)

		startMockDungeonServer(srvConn, func(pkt *protocol.Packet) bool {
			if pkt.ActionID == protocol.ActionIDItemGetPackList {
				buf := new(bytes.Buffer)
				_ = binary.Write(buf, binary.BigEndian, int16(30))
				_ = binary.Write(buf, binary.BigEndian, int32(28))
				_ = binary.Write(buf, binary.BigEndian, uint16(2))

				// 格位 2: 粽子 (ID 1444)
				_ = protocol.WriteItemRecord(buf, protocol.ItemRecord{
					PlayerItemID: 102,
					ItemID:       protocol.ItemIDRiceDumpling,
					GridID:       2,
				})
				// 格位 3: 茶叶蛋 (ID 2016)
				_ = protocol.WriteItemRecord(buf, protocol.ItemRecord{
					PlayerItemID: 103,
					ItemID:       protocol.ItemIDTeaEgg,
					GridID:       3,
				})

				_ = binary.Write(buf, binary.BigEndian, int16(10))
				_ = binary.Write(buf, binary.BigEndian, uint16(0))
				_ = protocol.WritePacket(srvConn, protocol.NewPacket(protocol.ActionIDItemGetPackList, buf.Bytes()))
				return true
			}

			if pkt.ActionID == protocol.ActionIDItemUseGrid {
				var gridID int16
				_ = binary.Read(bytes.NewReader(pkt.Payload), binary.BigEndian, &gridID)
				usedGridCh <- gridID
				_ = protocol.WritePacket(srvConn, protocol.NewPacket(protocol.ActionIDItemUseGrid, []byte{protocol.ItemActionSuccess}))
				return true
			}

			return false
		})

		cfg := dungeon.SweepConfig{
			Category:             dungeon.CategorySide,
			MaxBatchTimes:        1,
			AutoReplenishStamina: true,
		}
		routine := dungeon.NewDungeonSweepRoutine(nil, cfg)
		err := routine.Execute(context.Background(), sess, nil)
		if err != nil && !errors.Is(err, scheduler.ErrStaminaDepleted) {
			t.Fatalf("扫荡执行失败: %v", err)
		}

		select {
		case gridID := <-usedGridCh:
			if gridID != 2 {
				t.Errorf("无包子时期望优先使用粽子 (Grid 2), 实际使用了格位 %d", gridID)
			}
		case <-time.After(1 * time.Second):
			t.Fatal("未收到使用道具协议调用")
		}
	})

	t.Run("Priority3_TeaEgg_WhenNoBunOrDumpling", func(t *testing.T) {
		// 背包中仅有茶叶蛋(Grid 3)
		sess, srvConn := createTestSession(t, 0)
		defer sess.Close()
		defer srvConn.Close()

		usedGridCh := make(chan int16, 5)

		startMockDungeonServer(srvConn, func(pkt *protocol.Packet) bool {
			if pkt.ActionID == protocol.ActionIDItemGetPackList {
				buf := new(bytes.Buffer)
				_ = binary.Write(buf, binary.BigEndian, int16(30))
				_ = binary.Write(buf, binary.BigEndian, int32(29))
				_ = binary.Write(buf, binary.BigEndian, uint16(1))

				// 格位 3: 茶叶蛋 (ID 2016)
				_ = protocol.WriteItemRecord(buf, protocol.ItemRecord{
					PlayerItemID: 103,
					ItemID:       protocol.ItemIDTeaEgg,
					GridID:       3,
				})

				_ = binary.Write(buf, binary.BigEndian, int16(10))
				_ = binary.Write(buf, binary.BigEndian, uint16(0))
				_ = protocol.WritePacket(srvConn, protocol.NewPacket(protocol.ActionIDItemGetPackList, buf.Bytes()))
				return true
			}

			if pkt.ActionID == protocol.ActionIDItemUseGrid {
				var gridID int16
				_ = binary.Read(bytes.NewReader(pkt.Payload), binary.BigEndian, &gridID)
				usedGridCh <- gridID
				_ = protocol.WritePacket(srvConn, protocol.NewPacket(protocol.ActionIDItemUseGrid, []byte{protocol.ItemActionSuccess}))
				return true
			}

			return false
		})

		cfg := dungeon.SweepConfig{
			Category:             dungeon.CategoryMainline,
			MaxBatchTimes:        1,
			AutoReplenishStamina: true,
		}
		routine := dungeon.NewDungeonSweepRoutine(nil, cfg)
		err := routine.Execute(context.Background(), sess, nil)
		if err != nil && !errors.Is(err, scheduler.ErrStaminaDepleted) {
			t.Fatalf("扫荡执行失败: %v", err)
		}

		select {
		case gridID := <-usedGridCh:
			if gridID != 3 {
				t.Errorf("无包子粽子时期望使用茶叶蛋 (Grid 3), 实际使用了格位 %d", gridID)
			}
		case <-time.After(1 * time.Second):
			t.Fatal("未收到使用道具协议调用")
		}
	})
}

// TestDungeonSweep_PipelineExecutionOrder 验证配置流水线严格按照:
// 优先推主线 -> 再推支线 -> 再扫荡精英 -> 最后兜底扫荡最高级主线 的时序流转。
func TestDungeonSweep_PipelineExecutionOrder(t *testing.T) {
	sess, srvConn := createTestSession(t, 20)
	defer sess.Close()
	defer srvConn.Close()

	var sequence []string
	var seqMu sync.Mutex
	record := func(action string) {
		seqMu.Lock()
		defer seqMu.Unlock()
		sequence = append(sequence, action)
	}

	startMockDungeonServer(srvConn, func(pkt *protocol.Packet) bool {
		switch pkt.ActionID {
		case protocol.ActionMissionSweep:
			// 普通关卡进入/扫荡
			record("mission_sweep")
			resPkt, _ := protocol.BuildSweepResultPacket(protocol.SweepResult{
				Success:   true,
				MissionID: 1,
				Times:     1,
				CostPower: 5,
				GainExp:   2500,
				GainCoins: 12000,
				Message:   "扫荡成功",
			})
			_ = protocol.WritePacket(srvConn, resPkt)
			return true

		case protocol.ActionHeroMissionList:
			record("hero_list")
			w := protocol.NewWriter()
			w.WriteUint32(0)
			w.WriteUint8(1)
			w.WriteUint32(10)
			w.WriteUint32(1)
			w.WriteUint32(1001)
			w.WriteUint16(43)
			w.WriteUint8(1)
			_ = protocol.WritePacket(srvConn, protocol.NewPacket(protocol.ActionHeroMissionList, w.Bytes()))
			return true

		case protocol.ActionHeroMissionSelect:
			record("hero_select")
			_ = protocol.WritePacket(srvConn, protocol.NewPacket(protocol.ActionHeroMissionSelect, []byte{0x00, 0x00, 0x01}))
			return true

		case protocol.ActionHeroMissionSweep:
			record("hero_sweep")
			w := protocol.NewWriter()
			w.WriteBytes([]byte{0x00, 0x00, 0x01})
			w.WriteUint32(1001)
			w.WriteUint16(43)
			w.WriteUint8(1)
			_ = protocol.WritePacket(srvConn, protocol.NewPacket(protocol.ActionHeroMissionSweep, w.Bytes()))
			return true

		case protocol.ActionHeroMissionClose:
			record("hero_close")
			return true
		}
		return false
	})

	cfg := dungeon.SweepConfig{
		MaxBatchTimes: 3,
		Pipeline: []string{
			"sweep_elite",
			"sweep_highest_main",
		},
	}
	routine := dungeon.NewDungeonSweepRoutine(nil, cfg)
	err := routine.Execute(context.Background(), sess, nil)
	if err != nil && !errors.Is(err, scheduler.ErrStaminaDepleted) {
		t.Fatalf("流水线执行失败: %v", err)
	}

	seqMu.Lock()
	defer seqMu.Unlock()
	// 期望先执行英雄/精英副本，再执行最高主线扫荡
	hasHeroFirst := false
	hasSweepAfter := false
	for _, act := range sequence {
		if act == "hero_sweep" {
			hasHeroFirst = true
		}
		if hasHeroFirst && act == "mission_sweep" {
			hasSweepAfter = true
			break
		}
	}
	if !hasHeroFirst || !hasSweepAfter {
		t.Errorf("期望流水线先扫荡精英再扫荡主线, 实际时序: %v", sequence)
	}
}

type mockQuestDungeonRepo struct {
	quests   map[int]*dictionary.Quest
	townNPCs map[int][]int
}

func (m *mockQuestDungeonRepo) GetItem(id int) (*dictionary.Item, error) {
	return nil, dictionary.ErrNotFound
}
func (m *mockQuestDungeonRepo) GetNPC(id int) (*dictionary.NPC, error) {
	return nil, dictionary.ErrNotFound
}
func (m *mockQuestDungeonRepo) GetRoleType(id int) (*dictionary.RoleType, error) {
	return nil, dictionary.ErrNotFound
}
func (m *mockQuestDungeonRepo) GetMission(id int) (*dictionary.Mission, error) {
	return nil, dictionary.ErrNotFound
}
func (m *mockQuestDungeonRepo) GetHighestMission(sweepElite bool) (*dictionary.Mission, error) {
	return &dictionary.Mission{ID: 43, Name: "万妖皇", MissionLock: 9999}, nil
}
func (m *mockQuestDungeonRepo) GetHighestMissionByLevel(level int, sweepElite bool) (*dictionary.Mission, error) {
	return &dictionary.Mission{ID: 43, Name: "万妖皇", MissionLock: 9999}, nil
}
func (m *mockQuestDungeonRepo) GetHighestMissionByType(missionType int) (*dictionary.Mission, error) {
	return &dictionary.Mission{ID: 43, Name: "万妖皇", MissionLock: 9999}, nil
}
func (m *mockQuestDungeonRepo) GetHighestMissionByTypeAndLevel(level int, missionType int) (*dictionary.Mission, error) {
	return &dictionary.Mission{ID: 43, Name: "万妖皇", MissionLock: 9999}, nil
}
func (m *mockQuestDungeonRepo) GetQuestMissions(questType int, playerLevel int) ([]int, error) {
	return nil, nil
}
func (m *mockQuestDungeonRepo) GetQuest(id int) (*dictionary.Quest, error) {
	if q, ok := m.quests[id]; ok {
		return q, nil
	}
	return nil, dictionary.ErrNotFound
}
func (m *mockQuestDungeonRepo) GetTownNPCs(townID int) ([]int, error) {
	return m.townNPCs[townID], nil
}
func (m *mockQuestDungeonRepo) GetQuestNPCs() (map[int]bool, error) {
	res := make(map[int]bool)
	for _, q := range m.quests {
		if q.BeginNPCID > 0 {
			res[q.BeginNPCID] = true
		}
	}
	return res, nil
}
func (m *mockQuestDungeonRepo) GetWishpoolAward(id int) (*dictionary.WishpoolAward, error) {
	return nil, dictionary.ErrNotFound
}
func (m *mockQuestDungeonRepo) GetWishpoolAwards() (map[int]*dictionary.WishpoolAward, error) {
	return nil, nil
}
func (m *mockQuestDungeonRepo) SearchItems(keyword string) ([]*dictionary.Item, error) {
	return nil, nil
}
func (m *mockQuestDungeonRepo) Close() error {
	return nil
}

// TestDungeonSweep_QuestCycleCompleteFlow 验证完整的任务闭环自动化驱动:
// 1. 扫描 NPC 发现可接任务 (Quest 145) ->
// 2. 接取任务 (accept_quest) ->
// 3. 挑战关卡推进任务 (EnterMission 88) ->
// 4. 达成并交付任务 (complete_quest) ->
// 5. 无更多任务时退出任务循环 (break) ->
// 6. 流转至后续流水线 (精英副本扫荡 -> 最高等级副本扫荡)。
func TestDungeonSweep_QuestCycleCompleteFlow(t *testing.T) {
	sess, srvConn := createTestSession(t, 50)
	defer sess.Close()
	defer srvConn.Close()

	// 初始化角色在扬州城 (TownID=22)
	sess.UpdatePlayerState(func(ps *client.PlayerState) {
		ps.TownID = 22
		ps.Level = 35
	})

	repo := &mockQuestDungeonRepo{
		townNPCs: map[int][]int{
			22: {48}, // 扬州城 NPC 48 (秀秀)
		},
		quests: map[int]*dictionary.Quest{
			145: {
				ID:         145,
				Type:       2, // 支线任务
				Title:      "蚕丝",
				BeginNPCID: 48,
				EndNPCID:   48,
				MissionID:  88, // 对应副本 88
				AwardExp:   5000,
				AwardCoins: 20000,
			},
		},
	}

	var events []string
	var eventsMu sync.Mutex
	recordEvent := func(evt string) {
		eventsMu.Lock()
		defer eventsMu.Unlock()
		events = append(events, evt)
	}

	var questOffered atomic.Bool
	questOffered.Store(true)

	startMockDungeonServer(srvConn, func(pkt *protocol.Packet) bool {
		switch pkt.ActionID {
		case protocol.ActionIDQuestGetNPCQuests:
			recordEvent("get_npc_quests")
			// 回复 NPC 48 的任务列表
			w := protocol.NewWriter()
			if questOffered.Load() {
				w.WriteUint16(1)          // 1 个任务
				w.WriteInt32(145)         // Quest 145
				w.WriteUint8(1)           // 1=可接取
			} else {
				w.WriteUint16(0)          // 无更多任务
			}
			_ = protocol.WritePacket(srvConn, protocol.NewPacket(protocol.ActionIDQuestGetNPCQuests, w.Bytes()))
			return true

		case protocol.ActionIDQuestAccept:
			recordEvent("accept_quest")
			// 接取后，NPC 处不再提供该任务
			questOffered.Store(false)
			_ = protocol.WritePacket(srvConn, protocol.NewPacket(protocol.ActionIDQuestAccept, []byte{0x00}))
			return true

		case protocol.ActionMissionEnter, protocol.ActionMissionSweep, protocol.ActionIDPracticeStart:
			recordEvent("mission_sweep")
			if pkt.ActionID == protocol.ActionIDPracticeStart {
				resPkt, _ := protocol.BuildStartPracticeResultPacket(protocol.StartPracticeResult{
					Result:    protocol.PracticeResultSuccess,
					MissionID: 88,
					Count:     1,
				})
				_ = protocol.WritePacket(srvConn, resPkt)
				return true
			}
			resPkt, _ := protocol.BuildSweepResultPacket(protocol.SweepResult{
				Success:   true,
				MissionID: 88,
				Times:     1,
				CostPower: 5,
				GainExp:   2500,
				GainCoins: 12000,
				Message:   "通关成功",
			})
			_ = protocol.WritePacket(srvConn, resPkt)
			return true

		case protocol.ActionIDQuestComplete:
			recordEvent("complete_quest")
			_ = protocol.WritePacket(srvConn, protocol.NewPacket(protocol.ActionIDQuestComplete, []byte{0x00}))
			return true

		case protocol.ActionHeroMissionList:
			recordEvent("hero_list")
			w := protocol.NewWriter()
			w.WriteUint32(0)
			w.WriteUint8(1)
			w.WriteUint32(10)
			w.WriteUint32(1)
			w.WriteUint32(1001)
			w.WriteUint16(43)
			w.WriteUint8(1)
			_ = protocol.WritePacket(srvConn, protocol.NewPacket(protocol.ActionHeroMissionList, w.Bytes()))
			return true

		case protocol.ActionHeroMissionSelect:
			recordEvent("hero_select")
			_ = protocol.WritePacket(srvConn, protocol.NewPacket(protocol.ActionHeroMissionSelect, []byte{0x00, 0x00, 0x01}))
			return true

		case protocol.ActionHeroMissionSweep:
			recordEvent("hero_sweep")
			w := protocol.NewWriter()
			w.WriteBytes([]byte{0x00, 0x00, 0x01})
			w.WriteUint32(1001)
			w.WriteUint16(43)
			w.WriteUint8(1)
			_ = protocol.WritePacket(srvConn, protocol.NewPacket(protocol.ActionHeroMissionSweep, w.Bytes()))
			return true

		case protocol.ActionHeroMissionClose:
			recordEvent("hero_close")
			return true
		}
		return false
	})

	cfg := dungeon.SweepConfig{
		MaxBatchTimes: 5,
		Pipeline: []string{
			"push_mainline",
			"push_side",
			"sweep_elite",
			"sweep_highest_main",
		},
	}
	routine := dungeon.NewDungeonSweepRoutine(repo, cfg)
	err := routine.Execute(context.Background(), sess, nil)
	if err != nil && !errors.Is(err, scheduler.ErrStaminaDepleted) {
		t.Fatalf("任务循环流水线执行失败: %v", err)
	}

	eventsMu.Lock()
	defer eventsMu.Unlock()

	// 验证关键时序: get_npc_quests -> accept_quest -> mission_sweep -> complete_quest -> hero_sweep -> mission_sweep (最高主线)
	expectedOrder := []string{
		"get_npc_quests",
		"accept_quest",
		"mission_sweep",
		"complete_quest",
		"hero_sweep",
		"mission_sweep",
	}

	currentIdx := 0
	for _, evt := range events {
		if currentIdx < len(expectedOrder) && evt == expectedOrder[currentIdx] {
			currentIdx++
		}
	}

	if currentIdx != len(expectedOrder) {
		t.Fatalf("实际执行事件时序未满足完整闭环期望, 完成进度 %d/%d. 实际事件列表: %v", currentIdx, len(expectedOrder), events)
	}
}

func TestSixRealmsRoutine_ExecutionFlow(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()
	defer serverConn.Close()

	var sequenceWar atomic.Int32
	sequenceWar.Store(24)
	var resetCount atomic.Int32
	resetCount.Store(1)

	go func() {
		for {
			pkt, err := protocol.ReadPacket(serverConn)
			if err != nil {
				return
			}
			switch pkt.ActionID {
			case protocol.ActionIDTowerGetInfo:
				w := protocol.NewWriter()
				w.WriteUint8(25)                     // SequenceMax
				w.WriteUint8(1)                      // Layer: 1 (地狱道)
				w.WriteUint8(uint8(sequenceWar.Load())) // SequenceWar
				w.WriteUint8(5)                      // BagNum
				w.WriteUint8(uint8(resetCount.Load())) // ResetNum
				w.WriteUint16(0)                     // ResetIngot
				w.WriteUint8(0)                      // HideID
				w.WriteInt32(20)                     // HighStone
				_ = protocol.WritePacket(serverConn, protocol.NewPacket(protocol.ActionIDTowerGetInfo, w.Bytes()))

			case protocol.ActionIDTowerStartChallenge:
				sequenceWar.Add(1)
				cw := protocol.NewWriter()
				cw.WriteUint8(protocol.TowerResultSuccess)
				cw.WriteInt32(1001)   // SoulID
				cw.WriteInt32(100000) // Exp
				cw.WriteInt32(200)    // Fame
				cw.WriteInt32(10000)  // Coin
				_ = protocol.WritePacket(serverConn, protocol.NewPacket(protocol.ActionIDTowerStartChallenge, cw.Bytes()))

			case protocol.ActionIDTowerResetLayer:
				resetCount.Store(0)
				sequenceWar.Store(1)
				rw := protocol.NewWriter()
				rw.WriteUint8(protocol.TowerResultSuccess)
				rw.WriteUint8(1)
				rw.WriteUint16(50)
				rw.WriteUint8(0)
				_ = protocol.WritePacket(serverConn, protocol.NewPacket(protocol.ActionIDTowerResetLayer, rw.Bytes()))
			}
		}
	}()

	session := client.NewRoleSession(client.SessionConfig{
		RoleID:            "role-six-realms-test",
		RoleName:          "六道测试玩家",
		ServerAddr:        "mock-pipe",
		HeartbeatInterval: 1 * time.Second,
		Dialer: func(ctx context.Context, network, addr string) (net.Conn, error) {
			return clientConn, nil
		},
		Authenticator: func(s *client.RoleSession) error {
			return nil
		},
	})
	_ = session.Start(context.Background())
	defer session.Close()

	deadline := time.Now().Add(2 * time.Second)
	for session.State() != client.StateActive && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}

	routine := dungeon.NewSixRealmsRoutine()
	if routine.Name() != "six_realms" {
		t.Fatalf("unexpected routine identity: name=%s", routine.Name())
	}

	// 执行六道轮回 Routine
	err := routine.Execute(context.Background(), session, nil)
	if err != nil {
		t.Fatalf("SixRealmsRoutine.Execute failed: %v", err)
	}

	if cur := sequenceWar.Load(); cur <= 25 {
		t.Errorf("expected sequenceWar to advance past 25, got %d", cur)
	}
}

func TestDungeonSweep_SweepElite_SellSweptItems(t *testing.T) {
	t.Run("SellSweptItems_Enabled", func(t *testing.T) {
		sess, srvConn := createTestSession(t, 25)
		defer sess.Close()
		defer srvConn.Close()

		var heroSweepCount atomic.Int32
		var soldGrids []int16
		var soldMu sync.Mutex
		var getPackCount atomic.Int32

		startMockDungeonServer(srvConn, func(pkt *protocol.Packet) bool {
			switch pkt.ActionID {
			case protocol.ActionHeroMissionList:
				w := protocol.NewWriter()
				w.WriteUint32(0)
				w.WriteUint8(1)
				w.WriteUint32(5)
				w.WriteUint16(0)
				w.WriteUint16(1)
				w.WriteUint32(1001)
				w.WriteUint16(43)
				w.WriteUint8(1)
				_ = protocol.WritePacket(srvConn, protocol.NewPacket(protocol.ActionHeroMissionList, w.Bytes()))
				return true

			case protocol.ActionHeroMissionSelect:
				_ = protocol.WritePacket(srvConn, protocol.NewPacket(protocol.ActionHeroMissionSelect, []byte{0x00, 0x00, 0x01}))
				return true

			case protocol.ActionHeroMissionSweep:
				heroSweepCount.Add(1)
				w := protocol.NewWriter()
				w.WriteBytes([]byte{0x00, 0x00, 0x01})
				w.WriteUint32(1001)
				w.WriteUint16(43)
				w.WriteUint8(1)
				_ = protocol.WritePacket(srvConn, protocol.NewPacket(protocol.ActionHeroMissionSweep, w.Bytes()))
				return true

			case protocol.ActionHeroMissionQuickFinish, protocol.ActionHeroMissionClose:
				return true

			case protocol.ActionIDItemGetPackList:
				c := getPackCount.Add(1)
				buf := new(bytes.Buffer)
				_ = binary.Write(buf, binary.BigEndian, int16(30))
				_ = binary.Write(buf, binary.BigEndian, int32(28))

				if c == 1 {
					// 初始背包：仅有格位 1 的初始包子
					_ = binary.Write(buf, binary.BigEndian, uint16(1))
					_ = protocol.WriteItemRecord(buf, protocol.ItemRecord{
						PlayerItemID: 10001,
						ItemID:       protocol.ItemIDSteamedBun,
						GridID:       1,
					})
				} else {
					// 扫荡后背包：出现格位 2 的降魔武器扫荡物 (ItemID 54)
					_ = binary.Write(buf, binary.BigEndian, uint16(2))
					_ = protocol.WriteItemRecord(buf, protocol.ItemRecord{
						PlayerItemID: 10001,
						ItemID:       protocol.ItemIDSteamedBun,
						GridID:       1,
					})
					_ = protocol.WriteItemRecord(buf, protocol.ItemRecord{
						PlayerItemID: 20002,
						ItemID:       54,
						GridID:       2,
					})
				}
				_ = binary.Write(buf, binary.BigEndian, int16(10))
				_ = binary.Write(buf, binary.BigEndian, uint16(0))
				_ = protocol.WritePacket(srvConn, protocol.NewPacket(protocol.ActionIDItemGetPackList, buf.Bytes()))
				return true

			case protocol.ActionIDItemSell:
				gridID := int16(binary.BigEndian.Uint16(pkt.Payload[:2]))
				soldMu.Lock()
				soldGrids = append(soldGrids, gridID)
				soldMu.Unlock()
				_ = protocol.WritePacket(srvConn, protocol.NewPacket(protocol.ActionIDItemSell, []byte{protocol.ItemActionSuccess}))
				return true
			}
			return false
		})

		cfg := dungeon.SweepConfig{
			MaxBatchTimes: 5,
			Sweep: dungeon.SweepFeatureConfig{
				Enabled:        true,
				Pipeline:       []string{"sweep_elite"},
				SellSweptItems: true,
			},
		}
		routine := dungeon.NewDungeonSweepRoutine(nil, cfg)
		err := routine.Execute(context.Background(), sess, nil)
		if err != nil {
			t.Fatalf("执行英雄副本扫荡失败: %v", err)
		}

		if heroSweepCount.Load() != 1 {
			t.Errorf("期望扫荡 1 次, 实际 %d 次", heroSweepCount.Load())
		}

		soldMu.Lock()
		defer soldMu.Unlock()
		if len(soldGrids) != 1 || soldGrids[0] != 2 {
			t.Errorf("期望自动售出格位 2 的扫荡物, 实际售出格位: %v", soldGrids)
		}
	})

	t.Run("SellSweptItems_Disabled", func(t *testing.T) {
		sess, srvConn := createTestSession(t, 25)
		defer sess.Close()
		defer srvConn.Close()

		var soldCount atomic.Int32

		startMockDungeonServer(srvConn, func(pkt *protocol.Packet) bool {
			switch pkt.ActionID {
			case protocol.ActionHeroMissionList:
				w := protocol.NewWriter()
				w.WriteUint32(0)
				w.WriteUint8(1)
				w.WriteUint32(5)
				w.WriteUint16(0)
				w.WriteUint16(1)
				w.WriteUint32(1001)
				w.WriteUint16(43)
				w.WriteUint8(1)
				_ = protocol.WritePacket(srvConn, protocol.NewPacket(protocol.ActionHeroMissionList, w.Bytes()))
				return true

			case protocol.ActionHeroMissionSelect:
				_ = protocol.WritePacket(srvConn, protocol.NewPacket(protocol.ActionHeroMissionSelect, []byte{0x00, 0x00, 0x01}))
				return true

			case protocol.ActionHeroMissionSweep:
				w := protocol.NewWriter()
				w.WriteBytes([]byte{0x00, 0x00, 0x01})
				w.WriteUint32(1001)
				w.WriteUint16(43)
				w.WriteUint8(1)
				_ = protocol.WritePacket(srvConn, protocol.NewPacket(protocol.ActionHeroMissionSweep, w.Bytes()))
				return true

			case protocol.ActionHeroMissionQuickFinish, protocol.ActionHeroMissionClose:
				return true

			case protocol.ActionIDItemSell:
				soldCount.Add(1)
				_ = protocol.WritePacket(srvConn, protocol.NewPacket(protocol.ActionIDItemSell, []byte{protocol.ItemActionSuccess}))
				return true
			}
			return false
		})

		cfg := dungeon.SweepConfig{
			MaxBatchTimes: 5,
			Sweep: dungeon.SweepFeatureConfig{
				Enabled:        true,
				Pipeline:       []string{"sweep_elite"},
				SellSweptItems: false,
			},
		}
		routine := dungeon.NewDungeonSweepRoutine(nil, cfg)
		err := routine.Execute(context.Background(), sess, nil)
		if err != nil {
			t.Fatalf("执行英雄副本扫荡失败: %v", err)
		}

		if soldCount.Load() != 0 {
			t.Errorf("未开启卖出扫荡物时, 期望售出 0 次, 实际售出 %d 次", soldCount.Load())
		}
	})
}

// TestDungeonSweep_MaxReplenishStamina_Limit 验证单次推图按体力点数上限熔断机制:
// 当累计补充体力达到 max_replenish_stamina (例如 10 点) 后，不再继续吃药。
func TestDungeonSweep_MaxReplenishStamina_Limit(t *testing.T) {
	sess, srvConn := createTestSession(t, 0)
	defer sess.Close()
	defer srvConn.Close()

	var usedCount int32
	startMockDungeonServer(srvConn, func(pkt *protocol.Packet) bool {
		if pkt.ActionID == protocol.ActionIDItemGetPackList {
			buf := new(bytes.Buffer)
			_ = binary.Write(buf, binary.BigEndian, int16(30))
			_ = binary.Write(buf, binary.BigEndian, int32(27))
			_ = binary.Write(buf, binary.BigEndian, uint16(2))

			// 格位 1: 粽子 (ID 1444, +10体力)
			_ = protocol.WriteItemRecord(buf, protocol.ItemRecord{
				PlayerItemID: 101,
				ItemID:       protocol.ItemIDRiceDumpling,
				GridID:       1,
			})
			// 格位 2: 茶叶蛋 (ID 2016, +50体力)
			_ = protocol.WriteItemRecord(buf, protocol.ItemRecord{
				PlayerItemID: 102,
				ItemID:       protocol.ItemIDTeaEgg,
				GridID:       2,
			})

			_ = binary.Write(buf, binary.BigEndian, int16(10))
			_ = binary.Write(buf, binary.BigEndian, uint16(0))
			_ = protocol.WritePacket(srvConn, protocol.NewPacket(protocol.ActionIDItemGetPackList, buf.Bytes()))
			return true
		}

		if pkt.ActionID == protocol.ActionIDItemUseGrid {
			atomic.AddInt32(&usedCount, 1)
			_ = protocol.WritePacket(srvConn, protocol.NewPacket(protocol.ActionIDItemUseGrid, []byte{protocol.ItemActionSuccess}))
			return true
		}

		return false
	})

	cfg := dungeon.SweepConfig{
		QuestCycle: dungeon.QuestCycleFeatureConfig{
			Enabled:              true,
			AutoReplenishStamina: true,
			MaxReplenishStamina:  10, // 上限 10 点，吃一个粽子(+10)后即达到上限，不能再吃茶叶蛋(+50)
		},
		Category: dungeon.CategoryMainline,
	}

	routine := dungeon.NewDungeonSweepRoutine(nil, cfg)
	_ = routine.Execute(context.Background(), sess, nil)

	// 验证最多只消耗了 1 次粽子 (+10 点体力)，没有继续消耗茶叶蛋
	if atomic.LoadInt32(&usedCount) > 1 {
		t.Errorf("期望达到体力点数上限后停止吃药，实际吃药次数: %d", atomic.LoadInt32(&usedCount))
	}
}






