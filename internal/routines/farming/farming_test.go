// Package farming_test 验证药园种植与巡检任务的土地查询、成熟采摘、空闲播种与奖励累加真实业务流。
//
// @author Ateng
// @since 2026-10-10
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
	"sxd-pie-ng/internal/scheduler"
)

// TestHerbGardenRoutine_Metadata 验证 Routine 元数据与调度属性
func TestHerbGardenRoutine_Metadata(t *testing.T) {
	routine := farming.NewHerbGardenRoutine()
	if routine.Name() != "herb_garden" {
		t.Fatalf("任务名称不匹配: %s", routine.Name())
	}
	if dr, ok := routine.(scheduler.DetailedRoutine); ok {
		if dr.Domain() != "farming" {
			t.Fatalf("领域不匹配: %s", dr.Domain())
		}
	}
}

// TestHerbGardenRoutine_UnconnectedGuard 验证未连接时前置卫语句拦截
func TestHerbGardenRoutine_UnconnectedGuard(t *testing.T) {
	routine := farming.NewHerbGardenRoutine()
	err := routine.Execute(context.Background(), nil, nil)
	if err != client.ErrNotConnected {
		t.Fatalf("期望返回 ErrNotConnected, 实际得到: %v", err)
	}
}

// TestHerbGardenRoutine_FullBusinessFlow 验证完整的土地查询、成熟收获、空闲播种与奖励累加业务流
func TestHerbGardenRoutine_FullBusinessFlow(t *testing.T) {
	c1, c2 := net.Pipe()
	defer c1.Close()
	defer c2.Close()

	var harvestCalls atomic.Int32
	var plantCalls atomic.Int32
	var queryCalls atomic.Int32

	var serverWg sync.WaitGroup
	serverWg.Add(1)

	// 模拟具备药园业务处理能力的真实游戏网关
	go func() {
		defer serverWg.Done()
		for {
			pkt, err := protocol.ReadPacket(c2)
			if err != nil {
				return
			}

			switch pkt.ActionID {
			// 网关登录与四步初始化流水线
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
				// 全量资产快照
				_ = protocol.WritePacket(c2, protocol.NewPacket(protocol.ActionIDPlayerGetInfo, []byte{
					0x00, 0x09, 0xe6, 0xa2, 0xa6, 0xe4, 0xb8, 0x80, 0xe5, 0x9c, 0xba,
					0x00, 0x00, 0x01, 0x2c, // Level 300
					0x00, 0x00, 0x9a, 0x37, // Ingots 39479
					0x00, 0x00, 0x00, 0x08, 0x6f, 0x93, 0xac, 0xf8, // Coins 36231687416
					0x00, 0x00, 0x00, 0x00, 0x00, 0x8c, 0x0a, 0xd1,
					0x00, 0x00, 0x00, 0x00, 0x00, 0x8c, 0x0f, 0x99,
					0x00, 0x00, 0x01, 0x2c,
				}))

			case protocol.ActionHeartbeat:
				_ = protocol.WritePacket(c2, protocol.NewPacket(protocol.ActionHeartbeat, []byte{0x06, 0x00, 0x0a, 0x28}))

			// 药园核心业务流响应
			case protocol.ActionIDFarmGetInfo:
				queryCalls.Add(1)
				// 返回 3 块土地: 10(空闲), 11(成熟可摘), 12(种植中)
				res := protocol.FarmGetInfoResult{
					Fields: []protocol.FarmField{
						{LandID: 10, State: 1, Cooldown: 0, SeedOrRoleID: 0},
						{LandID: 11, State: 3, Cooldown: 0, SeedOrRoleID: 176},
						{LandID: 12, State: 2, Cooldown: 3600, SeedOrRoleID: 176},
					},
				}
				resp, _ := protocol.BuildFarmGetInfoResultPacket(res)
				_ = protocol.WritePacket(c2, resp)

			case protocol.ActionIDFarmGetPlantState:
				w := protocol.NewWriter()
				w.WriteUint16(3)
				fields := []protocol.FarmField{
					{LandID: 10, State: 1, Cooldown: 0, SeedOrRoleID: 0},
					{LandID: 11, State: 3, Cooldown: 0, SeedOrRoleID: 176},
					{LandID: 12, State: 2, Cooldown: 3600, SeedOrRoleID: 176},
				}
				for _, f := range fields {
					w.WriteInt32(f.LandID)
					w.WriteInt32(f.State)
					w.WriteInt32(f.Cooldown)
					w.WriteInt32(f.SeedOrRoleID)
				}
				_ = protocol.WritePacket(c2, protocol.NewPacket(protocol.ActionIDFarmGetPlantState, w.Bytes()))

			case protocol.ActionIDFarmGetSeeds:
				w := protocol.NewWriter()
				w.WriteUint16(1)
				w.WriteInt32(176) // SeedID
				w.WriteInt32(1)   // OwnedCount
				w.WriteInt32(3)   // PriceType
				w.WriteInt32(1500000)
				w.WriteInt32(0)
				_ = protocol.WritePacket(c2, protocol.NewPacket(protocol.ActionIDFarmGetSeeds, w.Bytes()))

			case protocol.ActionIDFarmBuySeed:
				_ = protocol.WritePacket(c2, protocol.NewPacket(protocol.ActionIDFarmBuySeed, []byte{protocol.FarmStatusSuccess}))

			case protocol.ActionIDFarmHarvest:
				harvestCalls.Add(1)
				req, _ := protocol.ParseFarmHarvestRequest(pkt.Payload)
				// 模拟成熟采摘: 仅 Land 11 成熟采摘成功 (产出 150000 经验与 600000 铜钱)，播种后的例行采摘尝试返回未成熟
				success := false
				if req.LandID == 11 && harvestCalls.Load() == 2 {
					success = true
				}
				res := protocol.FarmHarvestResult{
					Success:   success,
					GainExp:   150000,
					GainCoins: 600000,
				}
				resp, _ := protocol.BuildFarmHarvestResultPacket(res)
				_ = protocol.WritePacket(c2, resp)

			case protocol.ActionIDFarmPlant:
				plantCalls.Add(1)
				req, _ := protocol.ParseFarmPlantRequest(pkt.Payload)
				res := protocol.FarmPlantResult{
					Success:  req.LandID == 10 || req.LandID == 11,
					LandID:   req.LandID,
					Cooldown: 28800,
				}
				resp, _ := protocol.BuildFarmPlantResultPacket(res)
				_ = protocol.WritePacket(c2, resp)
			}
		}
	}()

	session := client.NewRoleSession(client.SessionConfig{
		RoleID:     "role-farmer-01",
		RoleName:   "神仙老农",
		Username:   "farmer_user",
		Hash:       "dummy_farm_hash",
		ServerAddr: "farm-test-pipe",
		Dialer: func(ctx context.Context, network, addr string) (net.Conn, error) {
			return c1, nil
		},
	})

	if err := session.Start(context.Background()); err != nil {
		t.Fatalf("session.Start 失败: %v", err)
	}
	defer session.Close()

	// 等待会话完成握手进入激活态
	activeDeadline := time.Now().Add(2 * time.Second)
	for session.State() != client.StateActive && time.Now().Before(activeDeadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if session.State() != client.StateActive {
		t.Fatalf("会话未进入激活态: %s", session.State())
	}

	initialCoins := session.GetPlayerState().Coins

	// 执行药园例程
	routine := farming.NewHerbGardenRoutine()
	execCtx, execCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer execCancel()
	err := routine.Execute(execCtx, session, nil)
	if err != nil {
		t.Fatalf("药园 Routine 执行失败: %v", err)
	}

	// 1. 验证发起了土地查询
	if queryCalls.Load() != 1 {
		t.Errorf("期望发起 1 次土地查询, 实际发起 %d 次", queryCalls.Load())
	}

	// 2. 验证采摘调用总计 3 次 (Land 10 播种后采摘尝试 + Land 11 成熟采摘 + Land 11 播种后采摘尝试)
	if harvestCalls.Load() != 3 {
		t.Errorf("期望采摘调用 3 次, 实际采摘 %d 次", harvestCalls.Load())
	}

	// 3. 验证对 Land 10 与刚采摘的 Land 11 发起了播种 (共 2 块空闲土地)
	if plantCalls.Load() != 2 {
		t.Errorf("期望播种 2 块空闲土地, 实际播种 %d 次", plantCalls.Load())
	}

	// 4. 验证收获奖励累加至角色状态 (初始铜钱 + 600,000)
	currentCoins := session.GetPlayerState().Coins
	if currentCoins != initialCoins+600000 {
		t.Errorf("期望铜钱增加 600,000 (当前: %d, 初始: %d)", currentCoins, initialCoins)
	}

	session.Close()
	c1.Close()
	c2.Close()
	serverWg.Wait()
}
