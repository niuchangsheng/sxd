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
	"fmt"
	"log/slog"
	"net"
	"strings"
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

// dungeonAutomationFunctionIDs Mock 环境视为已开放的玩法功能编号。
var dungeonAutomationFunctionIDs = []int32{
	protocol.FunctionIDZodiac, protocol.FunctionIDBeelzebubTrials,
	protocol.FunctionIDMiracleFighters, protocol.FunctionIDRepressDemonTower,
	protocol.FunctionIDShanhaiWorld, protocol.FunctionIDEudemons,
	protocol.FunctionIDDestinySword, protocol.FunctionIDLinkFate,
	protocol.FunctionIDPartnerLink, protocol.FunctionIDSaintArea,
	protocol.FunctionIDFaction, protocol.FunctionIDTreasureHouse,
	protocol.FunctionIDStTrials, protocol.FunctionIDStPracticeRoom,
	protocol.FunctionIDTravelEvent, protocol.FunctionIDRollCake,
	protocol.FunctionIDFindImmortal, protocol.FunctionIDFishing,
}

func startMockDungeonServer(srvConn net.Conn, handler func(pkt *protocol.Packet) bool) {
	startMockDungeonServerWithFunctions(srvConn, dungeonAutomationFunctionIDs, handler)
}

// startMockDungeonServerWithFunctions 允许指定已开放功能列表，用于验证功能门槛跳过路径。
func startMockDungeonServerWithFunctions(srvConn net.Conn, functionIDs []int32, handler func(pkt *protocol.Packet) bool) {
	go func() {
		for {
			pkt, err := protocol.ReadPacket(srvConn)
			if err != nil {
				return
			}
			// 参与自动化功能门槛的玩法在 Mock 下一律视为已开放。
			if pkt.ActionID == protocol.ActionIDPlayerGetFunctions {
				_ = protocol.WritePacket(srvConn, protocol.BuildPlayerFunctionsResultPacket(functionIDs))
				continue
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
				// 默认返回空英雄副本列表 (官方 Mod_HeroMission_Base 响应布局)
				w := protocol.NewWriter()
				w.WriteUint16(0) // 条目数
				w.WriteInt8(0)   // 当日总购买次数
				w.WriteInt8(0)   // 剩余购买次数
				w.WriteInt16(0)  // 购买价格
				w.WriteInt32(0)  // 已通关数量
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

// TestDungeonSweep_HeroMission_FullWorkflow 验证官方 Module 35 英雄副本扫荡全流程:
// get_hero_mission_list(town_id) -> start_practice -> quickly -> notify FINISH。
func TestDungeonSweep_HeroMission_FullWorkflow(t *testing.T) {
	sess, srvConn := createTestSession(t, 25)
	defer sess.Close()
	defer srvConn.Close()

	var startCount atomic.Int32
	var quickCount atomic.Int32
	var startedTown atomic.Int32
	var listCount atomic.Int32

	startMockDungeonServer(srvConn, func(pkt *protocol.Packet) bool {
		switch pkt.ActionID {
		case protocol.ActionHeroMissionList:
			listCount.Add(1)
			if len(pkt.Payload) < 2 {
				t.Errorf("英雄副本列表请求缺少 town_id: %X", pkt.Payload)
			}
			townID := binary.BigEndian.Uint16(pkt.Payload[:2])
			w := protocol.NewWriter()
			if townID == 4 {
				w.WriteUint16(2) // 条数
				// mission 1
				w.WriteInt32(166)
				w.WriteInt8(1) // 可挑战
				w.WriteUint8(0)
				w.WriteInt8(1) // 已首通
				// mission 2
				w.WriteInt32(167)
				w.WriteInt8(1)
				w.WriteUint8(1)
				w.WriteInt8(1)
				w.WriteInt8(1) // 当日总购买次数
				w.WriteInt8(1) // 剩余购买次数
				w.WriteInt16(10)
				w.WriteInt32(2)
			} else {
				w.WriteUint16(0)
				w.WriteInt8(0)
				w.WriteInt8(0)
				w.WriteInt16(0)
				w.WriteInt32(0)
			}
			_ = protocol.WritePacket(srvConn, protocol.NewPacket(protocol.ActionHeroMissionList, w.Bytes()))
			return true

		case protocol.ActionHeroMissionStartPractice:
			startCount.Add(1)
			if len(pkt.Payload) != 8 {
				t.Errorf("start_practice 载荷应为 4 字节业务 + 4 字节 PrevAct, 实际 %d: %X", len(pkt.Payload), pkt.Payload)
			}
			startedTown.Store(int32(binary.BigEndian.Uint16(pkt.Payload[:2])))
			if pkt.Payload[2] != 0 {
				t.Errorf("auto_num 必须为 0 (禁止自动购买翻牌), 实际 %d", pkt.Payload[2])
			}
			w := protocol.NewWriter()
			w.WriteUint8(protocol.HeroMissionResultSuccess)
			w.WriteInt32(30)
			_ = protocol.WritePacket(srvConn, protocol.NewPacket(protocol.ActionHeroMissionStartPractice, w.Bytes()))
			return true

		case protocol.ActionHeroMissionQuickly:
			quickCount.Add(1)
			if len(pkt.Payload) != 4 {
				t.Errorf("quickly 请求应仅携带 4 字节 PrevAct 占位, 实际 %X", pkt.Payload)
			}
			_ = protocol.WritePacket(srvConn, protocol.NewPacket(protocol.ActionHeroMissionQuickly, []byte{protocol.HeroMissionResultSuccess}))
			// 模拟服务端推送扫荡结束
			w := protocol.NewWriter()
			w.WriteUint8(protocol.HeroMissionNotifyFinish)
			w.WriteUint16(0) // 战报数
			w.WriteUint16(0) // 评价结算数
			w.WriteInt16(30)
			_ = protocol.WritePacket(srvConn, protocol.NewPacket(protocol.ActionHeroMissionNotify, w.Bytes()))
			return true
		}
		return false
	})

	routine := dungeon.NewDungeonSweepRoutine(nil, dungeon.SweepConfig{
		Pipeline: []string{"sweep_elite"},
	})
	if err := routine.Execute(context.Background(), sess, nil); err != nil {
		t.Fatalf("执行英雄副本扫荡失败: %v", err)
	}

	if listCount.Load() == 0 {
		t.Errorf("期望至少查询一次英雄副本城镇列表")
	}
	if startCount.Load() != 1 {
		t.Errorf("期望仅对含副本的城镇启动 1 次扫荡, 实际 %d 次", startCount.Load())
	}
	if got := startedTown.Load(); got != 4 {
		t.Errorf("期望扫荡城镇 4, 实际 %d", got)
	}
	if quickCount.Load() != 1 {
		t.Errorf("期望快速完成 1 次, 实际 %d 次", quickCount.Load())
	}
}

type mockQuestDungeonRepo struct {
	quests   map[int]*dictionary.Quest
	missions map[int]*dictionary.Mission
	townNPCs map[int][]int
	eliteIDs []int
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
	if mm, ok := m.missions[id]; ok {
		return mm, nil
	}
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
func (m *mockQuestDungeonRepo) GetEliteMissionIDs() ([]int, error) {
	return m.eliteIDs, nil
}

func (m *mockQuestDungeonRepo) GetEliteMissionTowns() ([]int, error) {
	seen := make(map[int]bool)
	towns := make([]int, 0)
	for _, ms := range m.missions {
		if ms.Type == 1 && ms.MapID > 0 && !seen[ms.MapID] {
			seen[ms.MapID] = true
			towns = append(towns, ms.MapID)
		}
	}
	return towns, nil
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
				w.WriteUint16(1)  // 1 个任务
				w.WriteInt32(145) // Quest 145
				w.WriteUint8(1)   // 1=可接取
			} else {
				w.WriteUint16(0) // 无更多任务
			}
			_ = protocol.WritePacket(srvConn, protocol.NewPacket(protocol.ActionIDQuestGetNPCQuests, w.Bytes()))
			return true

		case protocol.ActionIDQuestAccept:
			recordEvent("accept_quest")
			// 接取后，NPC 处不再提供该任务
			questOffered.Store(false)
			_ = protocol.WritePacket(srvConn, protocol.NewPacket(protocol.ActionIDQuestAccept, []byte{0x00}))
			return true

		case protocol.ActionIDPracticeStart:
			recordEvent("mission_sweep")
			resPkt, _ := protocol.BuildStartPracticeResultPacket(protocol.StartPracticeResult{
				Result:    protocol.PracticeResultSuccess,
				MissionID: 88,
				Count:     1,
			})
			_ = protocol.WritePacket(srvConn, resPkt)
			return true

		case protocol.ActionIDQuestComplete:
			recordEvent("complete_quest")
			_ = protocol.WritePacket(srvConn, protocol.NewPacket(protocol.ActionIDQuestComplete, []byte{0x00}))
			return true

		case protocol.ActionHeroMissionList:
			recordEvent("hero_list")
			townID := uint16(0)
			if len(pkt.Payload) >= 2 {
				townID = binary.BigEndian.Uint16(pkt.Payload[:2])
			}
			w := protocol.NewWriter()
			if townID == 4 {
				w.WriteUint16(1)
				w.WriteInt32(166)
				w.WriteInt8(1)
				w.WriteUint8(0)
				w.WriteInt8(1)
				w.WriteInt8(0)
				w.WriteInt8(0)
				w.WriteInt16(0)
				w.WriteInt32(1)
			} else {
				w.WriteUint16(0)
				w.WriteInt8(0)
				w.WriteInt8(0)
				w.WriteInt16(0)
				w.WriteInt32(0)
			}
			_ = protocol.WritePacket(srvConn, protocol.NewPacket(protocol.ActionHeroMissionList, w.Bytes()))
			return true

		case protocol.ActionHeroMissionStartPractice:
			recordEvent("hero_sweep")
			w := protocol.NewWriter()
			w.WriteUint8(protocol.HeroMissionResultSuccess)
			w.WriteInt32(30)
			_ = protocol.WritePacket(srvConn, protocol.NewPacket(protocol.ActionHeroMissionStartPractice, w.Bytes()))
			return true

		case protocol.ActionHeroMissionQuickly:
			recordEvent("hero_quickly")
			_ = protocol.WritePacket(srvConn, protocol.NewPacket(protocol.ActionHeroMissionQuickly, []byte{protocol.HeroMissionResultSuccess}))
			w := protocol.NewWriter()
			w.WriteUint8(protocol.HeroMissionNotifyFinish)
			w.WriteUint16(0)
			w.WriteUint16(0)
			w.WriteInt16(30)
			_ = protocol.WritePacket(srvConn, protocol.NewPacket(protocol.ActionHeroMissionNotify, w.Bytes()))
			return true
		}
		return false
	})

	cfg := dungeon.SweepConfig{
		Pipeline: []string{
			"push_mainline",
			"push_side",
			"sweep_elite",
		},
	}
	routine := dungeon.NewDungeonSweepRoutine(repo, cfg)
	err := routine.Execute(context.Background(), sess, nil)
	if err != nil && !errors.Is(err, scheduler.ErrStaminaDepleted) {
		t.Fatalf("任务循环流水线执行失败: %v", err)
	}

	eventsMu.Lock()
	defer eventsMu.Unlock()

	// 验证关键时序: get_npc_quests -> accept_quest -> mission_sweep -> complete_quest -> hero_sweep
	expectedOrder := []string{
		"get_npc_quests",
		"accept_quest",
		"mission_sweep",
		"complete_quest",
		"hero_sweep",
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

func TestDungeonSweep_EliteQuestCycle_Module4FirstClear(t *testing.T) {
	sess, srvConn := createTestSession(t, 100)
	defer sess.Close()
	defer srvConn.Close()

	sess.UpdatePlayerState(func(ps *client.PlayerState) {
		ps.Level = 63
	})

	repo := &mockQuestDungeonRepo{
		quests: map[int]*dictionary.Quest{
			336: {
				ID:        336,
				Type:      4, // 精英任务
				Lock:      10100,
				Level:     1,
				Title:     "精英罗虎王",
				MissionID: 456,
				AwardExp:  440000, AwardCoins: 88000,
			},
		},
		missions: map[int]*dictionary.Mission{
			456: {
				ID:          456,
				SectionID:   29,
				MissionLock: 22046,
				MapID:       13,
				Name:        "精英罗虎王",
				Type:        1, // 精英关卡
				IsBoss:      true,
				Monsters:    "1041",
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

	var cleared atomic.Bool
	var completed atomic.Bool

	startMockDungeonServer(srvConn, func(pkt *protocol.Packet) bool {
		switch pkt.ActionID {
		case protocol.ActionIDQuestList:
			recordEvent("quest_list")
			w := protocol.NewWriter()
			if completed.Load() {
				w.WriteUint16(0)
				_ = protocol.WritePacket(srvConn, protocol.NewPacket(protocol.ActionIDQuestList, w.Bytes()))
				return true
			}
			state, cur := int32(1), int32(0)
			if cleared.Load() {
				state, cur = 2, 1
			}
			w.WriteUint16(1)
			w.WriteInt32(336)
			w.WriteInt32(state)
			w.WriteUint8(0) // 击杀怪物类任务
			w.WriteUint16(1)
			w.WriteInt32(1041)
			w.WriteInt32(cur)
			w.WriteInt32(1)
			_ = protocol.WritePacket(srvConn, protocol.NewPacket(protocol.ActionIDQuestList, w.Bytes()))
			return true

		case protocol.ActionIDMissionGetSections:
			recordEvent("get_sections")
			w := protocol.NewWriter()
			w.WriteUint16(0) // 精英关卡不在章节 get_sections 列表中
			_ = protocol.WritePacket(srvConn, protocol.NewPacket(protocol.ActionIDMissionGetSections, w.Bytes()))
			return true

		case protocol.ActionIDMissionPlotEnter:
			mid := int32(0)
			if len(pkt.Payload) >= 4 {
				mid = int32(binary.BigEndian.Uint32(pkt.Payload[:4]))
			}
			recordEvent(fmt.Sprintf("plot_enter:%d", mid))
			_ = protocol.WritePacket(srvConn, protocol.NewPacket(protocol.ActionIDMissionPlotEnter, []byte{protocol.MissionResultSuccess}))
			return true

		case protocol.ActionIDMissionFightMonster:
			mt := int32(0)
			if len(pkt.Payload) >= 4 {
				mt = int32(binary.BigEndian.Uint32(pkt.Payload[:4]))
			}
			recordEvent(fmt.Sprintf("fight_monster:%d", mt))
			cleared.Store(true)
			_ = protocol.WritePacket(srvConn, protocol.NewPacket(protocol.ActionIDMissionFightMonster, []byte{0x00, protocol.MissionResultSuccess}))
			return true

		case protocol.ActionIDMissionRank:
			recordEvent("rank_mission")
			_ = protocol.WritePacket(srvConn, protocol.NewPacket(protocol.ActionIDMissionRank, []byte{0x00}))
			return true

		case protocol.ActionIDMissionOpenBox:
			recordEvent("open_box")
			_ = protocol.WritePacket(srvConn, protocol.NewPacket(protocol.ActionIDMissionOpenBox, []byte{0x00}))
			return true

		case protocol.ActionIDMissionPickupAward:
			recordEvent("pickup_award")
			_ = protocol.WritePacket(srvConn, protocol.NewPacket(protocol.ActionIDMissionPickupAward, []byte{0x00}))
			return true

		case protocol.ActionIDQuestSubmitComplete:
			recordEvent("complete_quest")
			completed.Store(true)
			_ = protocol.WritePacket(srvConn, protocol.NewPacket(protocol.ActionIDQuestSubmitComplete, []byte{0x01}))
			return true
		}
		return false
	})

	cfg := dungeon.SweepConfig{Pipeline: []string{"quest_cycle"}}
	routine := dungeon.NewDungeonSweepRoutine(repo, cfg)
	if err := routine.Execute(context.Background(), sess, nil); err != nil && !errors.Is(err, scheduler.ErrStaminaDepleted) {
		t.Fatalf("精英任务推图执行失败: %v", err)
	}

	eventsMu.Lock()
	defer eventsMu.Unlock()

	joined := strings.Join(events, ",")
	if !strings.Contains(joined, "plot_enter:456") {
		t.Fatalf("精英任务未进入 Module 4 plot_enter, events=%v", events)
	}
	if !strings.Contains(joined, "fight_monster:1041") {
		t.Fatalf("精英任务未挑战精英关卡怪物编队 1041, events=%v", events)
	}
	if !strings.Contains(joined, "complete_quest") {
		t.Fatalf("精英任务通关后未交付结算, events=%v", events)
	}
	for _, evt := range events {
		if strings.HasPrefix(evt, "practice") {
			t.Fatalf("精英任务不应走 Module 25 普通关卡扫荡, events=%v", events)
		}
	}
}

// TestDungeonSweep_UnlistedMission_SkipsModule25 验证目标关卡不在 get_sections 章节列表时，
// 不得再发起必然被网关静默丢弃的 Module 25 扫荡，而应直接走 Module 4 剧情副本首通。
func TestDungeonSweep_UnlistedMission_SkipsModule25(t *testing.T) {
	sess, srvConn := createTestSession(t, 100)
	defer sess.Close()
	defer srvConn.Close()

	sess.UpdatePlayerState(func(ps *client.PlayerState) {
		ps.Level = 67
	})

	repo := &mockQuestDungeonRepo{
		quests: map[int]*dictionary.Quest{
			257: {
				ID:         257,
				Type:       1,
				Lock:       0,
				Level:      1,
				Title:      "羽族仙子",
				MissionID:  364,
				AwardExp:   5000,
				AwardCoins: 20000,
			},
		},
		missions: map[int]*dictionary.Mission{
			364: {
				ID:          364,
				SectionID:   31,
				MissionLock: 24025,
				MapID:       13,
				Name:        "扶桑神树(3)",
				Type:        0,
				Monsters:    "1007",
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

	var cleared atomic.Bool
	var completed atomic.Bool

	startMockDungeonServer(srvConn, func(pkt *protocol.Packet) bool {
		switch pkt.ActionID {
		case protocol.ActionIDQuestList:
			w := protocol.NewWriter()
			if completed.Load() {
				w.WriteUint16(0)
				_ = protocol.WritePacket(srvConn, protocol.NewPacket(protocol.ActionIDQuestList, w.Bytes()))
				return true
			}
			state := int32(1)
			if cleared.Load() {
				state = 2
			}
			w.WriteUint16(1)
			w.WriteInt32(257)
			w.WriteInt32(state)
			w.WriteUint8(4) // NeedType NOONE: 仅需通关关联关卡
			w.WriteUint16(0)
			w.WriteInt32(0)
			w.WriteInt32(0)
			w.WriteInt32(0)
			_ = protocol.WritePacket(srvConn, protocol.NewPacket(protocol.ActionIDQuestList, w.Bytes()))
			return true

		case protocol.ActionIDMissionGetSections:
			recordEvent("get_sections")
			w := protocol.NewWriter()
			w.WriteUint16(0) // 目标关卡尚未解锁，不在章节列表中
			_ = protocol.WritePacket(srvConn, protocol.NewPacket(protocol.ActionIDMissionGetSections, w.Bytes()))
			return true

		case protocol.ActionIDPracticeStart, protocol.ActionIDPracticeGetStatus:
			recordEvent("practice_start")
			return true

		case protocol.ActionIDMissionPlotEnter:
			mid := int32(0)
			if len(pkt.Payload) >= 4 {
				mid = int32(binary.BigEndian.Uint32(pkt.Payload[:4]))
			}
			recordEvent(fmt.Sprintf("plot_enter:%d", mid))
			_ = protocol.WritePacket(srvConn, protocol.NewPacket(protocol.ActionIDMissionPlotEnter, []byte{protocol.MissionResultSuccess}))
			return true

		case protocol.ActionIDMissionFightMonster:
			mt := int32(0)
			if len(pkt.Payload) >= 4 {
				mt = int32(binary.BigEndian.Uint32(pkt.Payload[:4]))
			}
			recordEvent(fmt.Sprintf("fight_monster:%d", mt))
			cleared.Store(true)
			_ = protocol.WritePacket(srvConn, protocol.NewPacket(protocol.ActionIDMissionFightMonster, []byte{0x00, protocol.MissionResultSuccess}))
			return true

		case protocol.ActionIDMissionRank:
			_ = protocol.WritePacket(srvConn, protocol.NewPacket(protocol.ActionIDMissionRank, []byte{0x00}))
			return true
		case protocol.ActionIDMissionOpenBox:
			_ = protocol.WritePacket(srvConn, protocol.NewPacket(protocol.ActionIDMissionOpenBox, []byte{0x00}))
			return true
		case protocol.ActionIDMissionPickupAward:
			_ = protocol.WritePacket(srvConn, protocol.NewPacket(protocol.ActionIDMissionPickupAward, []byte{0x00}))
			return true
		case protocol.ActionIDQuestSubmitComplete:
			recordEvent("complete_quest")
			completed.Store(true)
			_ = protocol.WritePacket(srvConn, protocol.NewPacket(protocol.ActionIDQuestSubmitComplete, []byte{0x01}))
			return true
		}
		return false
	})

	cfg := dungeon.SweepConfig{Pipeline: []string{"quest_cycle"}}
	routine := dungeon.NewDungeonSweepRoutine(repo, cfg)
	if err := routine.Execute(context.Background(), sess, nil); err != nil && !errors.Is(err, scheduler.ErrStaminaDepleted) {
		t.Fatalf("推图执行失败: %v", err)
	}

	eventsMu.Lock()
	defer eventsMu.Unlock()

	joined := strings.Join(events, ",")
	if !strings.Contains(joined, "plot_enter:364") {
		t.Fatalf("未解锁关卡应直接走剧情副本 plot_enter, events=%v", events)
	}
	if !strings.Contains(joined, "fight_monster:1007") {
		t.Fatalf("未解锁关卡应直接挑战怪物编队, events=%v", events)
	}
	for _, evt := range events {
		if evt == "practice_start" {
			t.Fatalf("目标关卡不在章节列表时不应发起 Module 25 扫荡, events=%v", events)
		}
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
				w.WriteUint8(25)                        // SequenceMax
				w.WriteUint8(1)                         // Layer: 1 (地狱道)
				w.WriteUint8(uint8(sequenceWar.Load())) // SequenceWar
				w.WriteUint8(5)                         // BagNum
				w.WriteUint8(uint8(resetCount.Load()))  // ResetNum
				w.WriteUint16(0)                        // ResetIngot
				w.WriteUint8(0)                         // HideID
				w.WriteInt32(20)                        // HighStone
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

	if cur := sequenceWar.Load(); cur < 25 {
		t.Errorf("expected sequenceWar to advance to at least 25, got %d", cur)
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
				townID := uint16(0)
				if len(pkt.Payload) >= 2 {
					townID = binary.BigEndian.Uint16(pkt.Payload[:2])
				}
				w := protocol.NewWriter()
				if townID == 4 {
					w.WriteUint16(1)
					w.WriteInt32(166)
					w.WriteInt8(1)
					w.WriteUint8(0)
					w.WriteInt8(1)
					w.WriteInt8(1)
					w.WriteInt8(1)
					w.WriteInt16(10)
					w.WriteInt32(1)
				} else {
					w.WriteUint16(0)
					w.WriteInt8(0)
					w.WriteInt8(0)
					w.WriteInt16(0)
					w.WriteInt32(0)
				}
				_ = protocol.WritePacket(srvConn, protocol.NewPacket(protocol.ActionHeroMissionList, w.Bytes()))
				return true

			case protocol.ActionHeroMissionStartPractice:
				heroSweepCount.Add(1)
				w := protocol.NewWriter()
				w.WriteUint8(protocol.HeroMissionResultSuccess)
				w.WriteInt32(30)
				_ = protocol.WritePacket(srvConn, protocol.NewPacket(protocol.ActionHeroMissionStartPractice, w.Bytes()))
				return true

			case protocol.ActionHeroMissionQuickly:
				_ = protocol.WritePacket(srvConn, protocol.NewPacket(protocol.ActionHeroMissionQuickly, []byte{protocol.HeroMissionResultSuccess}))
				w := protocol.NewWriter()
				w.WriteUint8(protocol.HeroMissionNotifyFinish)
				w.WriteUint16(0)
				w.WriteUint16(0)
				w.WriteInt16(30)
				_ = protocol.WritePacket(srvConn, protocol.NewPacket(protocol.ActionHeroMissionNotify, w.Bytes()))
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
						Field4:       100, // 售价
						Field5:       1,   // 数量
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
				townID := uint16(0)
				if len(pkt.Payload) >= 2 {
					townID = binary.BigEndian.Uint16(pkt.Payload[:2])
				}
				w := protocol.NewWriter()
				if townID == 4 {
					w.WriteUint16(1)
					w.WriteInt32(166)
					w.WriteInt8(1)
					w.WriteUint8(0)
					w.WriteInt8(1)
					w.WriteInt8(1)
					w.WriteInt8(1)
					w.WriteInt16(10)
					w.WriteInt32(1)
				} else {
					w.WriteUint16(0)
					w.WriteInt8(0)
					w.WriteInt8(0)
					w.WriteInt16(0)
					w.WriteInt32(0)
				}
				_ = protocol.WritePacket(srvConn, protocol.NewPacket(protocol.ActionHeroMissionList, w.Bytes()))
				return true

			case protocol.ActionHeroMissionStartPractice:
				w := protocol.NewWriter()
				w.WriteUint8(protocol.HeroMissionResultSuccess)
				w.WriteInt32(30)
				_ = protocol.WritePacket(srvConn, protocol.NewPacket(protocol.ActionHeroMissionStartPractice, w.Bytes()))
				return true

			case protocol.ActionHeroMissionQuickly:
				_ = protocol.WritePacket(srvConn, protocol.NewPacket(protocol.ActionHeroMissionQuickly, []byte{protocol.HeroMissionResultSuccess}))
				w := protocol.NewWriter()
				w.WriteUint8(protocol.HeroMissionNotifyFinish)
				w.WriteUint16(0)
				w.WriteUint16(0)
				w.WriteInt16(30)
				_ = protocol.WritePacket(srvConn, protocol.NewPacket(protocol.ActionHeroMissionNotify, w.Bytes()))
				return true

			case protocol.ActionIDItemSell:
				soldCount.Add(1)
				_ = protocol.WritePacket(srvConn, protocol.NewPacket(protocol.ActionIDItemSell, []byte{protocol.ItemActionSuccess}))
				return true
			}
			return false
		})

		cfg := dungeon.SweepConfig{
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

	t.Run("SellSweptItems_ServerRejects", func(t *testing.T) {
		sess, srvConn := createTestSession(t, 25)
		defer sess.Close()
		defer srvConn.Close()

		var sellAttempts atomic.Int32
		var getPackCount atomic.Int32

		startMockDungeonServer(srvConn, func(pkt *protocol.Packet) bool {
			switch pkt.ActionID {
			case protocol.ActionHeroMissionList:
				townID := uint16(0)
				if len(pkt.Payload) >= 2 {
					townID = binary.BigEndian.Uint16(pkt.Payload[:2])
				}
				w := protocol.NewWriter()
				if townID == 4 {
					w.WriteUint16(1)
					w.WriteInt32(166)
					w.WriteInt8(1)
					w.WriteUint8(0)
					w.WriteInt8(1)
					w.WriteInt8(1)
					w.WriteInt8(1)
					w.WriteInt16(10)
					w.WriteInt32(1)
				} else {
					w.WriteUint16(0)
					w.WriteInt8(0)
					w.WriteInt8(0)
					w.WriteInt16(0)
					w.WriteInt32(0)
				}
				_ = protocol.WritePacket(srvConn, protocol.NewPacket(protocol.ActionHeroMissionList, w.Bytes()))
				return true

			case protocol.ActionHeroMissionStartPractice:
				w := protocol.NewWriter()
				w.WriteUint8(protocol.HeroMissionResultSuccess)
				w.WriteInt32(30)
				_ = protocol.WritePacket(srvConn, protocol.NewPacket(protocol.ActionHeroMissionStartPractice, w.Bytes()))
				return true

			case protocol.ActionHeroMissionQuickly:
				_ = protocol.WritePacket(srvConn, protocol.NewPacket(protocol.ActionHeroMissionQuickly, []byte{protocol.HeroMissionResultSuccess}))
				w := protocol.NewWriter()
				w.WriteUint8(protocol.HeroMissionNotifyFinish)
				w.WriteUint16(0)
				w.WriteUint16(0)
				w.WriteInt16(30)
				_ = protocol.WritePacket(srvConn, protocol.NewPacket(protocol.ActionHeroMissionNotify, w.Bytes()))
				return true

			case protocol.ActionIDItemGetPackList:
				c := getPackCount.Add(1)
				buf := new(bytes.Buffer)
				_ = binary.Write(buf, binary.BigEndian, int16(30))
				_ = binary.Write(buf, binary.BigEndian, int32(28))
				if c == 1 {
					_ = binary.Write(buf, binary.BigEndian, uint16(0))
				} else {
					_ = binary.Write(buf, binary.BigEndian, uint16(1))
					_ = protocol.WriteItemRecord(buf, protocol.ItemRecord{
						PlayerItemID: 20002,
						ItemID:       54,
						GridID:       2,
						Field4:       100,
						Field5:       1,
					})
				}
				_ = binary.Write(buf, binary.BigEndian, int16(10))
				_ = binary.Write(buf, binary.BigEndian, uint16(0))
				_ = protocol.WritePacket(srvConn, protocol.NewPacket(protocol.ActionIDItemGetPackList, buf.Bytes()))
				return true

			case protocol.ActionIDItemSell:
				sellAttempts.Add(1)
				// 非成功结果码: 自动化必须识别并继续, 不能误判为售出成功
				_ = protocol.WritePacket(srvConn, protocol.NewPacket(protocol.ActionIDItemSell, []byte{0x0B}))
				return true
			}
			return false
		})

		cfg := dungeon.SweepConfig{
			Sweep: dungeon.SweepFeatureConfig{
				Enabled:        true,
				Pipeline:       []string{"sweep_elite"},
				SellSweptItems: true,
			},
		}
		if err := dungeon.NewDungeonSweepRoutine(nil, cfg).Execute(context.Background(), sess, nil); err != nil {
			t.Fatalf("售出被拒绝时不应中断扫荡: %v", err)
		}
		if sellAttempts.Load() != 1 {
			t.Errorf("期望尝试售出 1 次, 实际 %d 次", sellAttempts.Load())
		}
	})

	t.Run("SellSweptItems_DoesNotSellExistingConsumableStack", func(t *testing.T) {
		sess, srvConn := createTestSession(t, 25)
		defer sess.Close()
		defer srvConn.Close()

		var sellAttempts atomic.Int32
		var getPackCount atomic.Int32

		startMockDungeonServer(srvConn, func(pkt *protocol.Packet) bool {
			switch pkt.ActionID {
			case protocol.ActionHeroMissionList:
				townID := uint16(0)
				if len(pkt.Payload) >= 2 {
					townID = binary.BigEndian.Uint16(pkt.Payload[:2])
				}
				w := protocol.NewWriter()
				if townID == 4 {
					w.WriteUint16(1)
					w.WriteInt32(166)
					w.WriteInt8(1)
					w.WriteUint8(0)
					w.WriteInt8(1)
					w.WriteInt8(1)
					w.WriteInt8(1)
					w.WriteInt16(10)
					w.WriteInt32(1)
				} else {
					w.WriteUint16(0)
					w.WriteInt8(0)
					w.WriteInt8(0)
					w.WriteInt16(0)
					w.WriteInt32(0)
				}
				_ = protocol.WritePacket(srvConn, protocol.NewPacket(protocol.ActionHeroMissionList, w.Bytes()))
				return true

			case protocol.ActionHeroMissionStartPractice:
				w := protocol.NewWriter()
				w.WriteUint8(protocol.HeroMissionResultSuccess)
				w.WriteInt32(30)
				_ = protocol.WritePacket(srvConn, protocol.NewPacket(protocol.ActionHeroMissionStartPractice, w.Bytes()))
				return true

			case protocol.ActionHeroMissionQuickly:
				_ = protocol.WritePacket(srvConn, protocol.NewPacket(protocol.ActionHeroMissionQuickly, []byte{protocol.HeroMissionResultSuccess}))
				w := protocol.NewWriter()
				w.WriteUint8(protocol.HeroMissionNotifyFinish)
				w.WriteUint16(0)
				w.WriteUint16(0)
				w.WriteInt16(30)
				_ = protocol.WritePacket(srvConn, protocol.NewPacket(protocol.ActionHeroMissionNotify, w.Bytes()))
				return true

			case protocol.ActionIDItemGetPackList:
				c := getPackCount.Add(1)
				qty := int32(10)
				if c > 1 {
					qty = 11
				}
				buf := new(bytes.Buffer)
				_ = binary.Write(buf, binary.BigEndian, int16(30))
				_ = binary.Write(buf, binary.BigEndian, int32(28))
				_ = binary.Write(buf, binary.BigEndian, uint16(1))
				_ = protocol.WriteItemRecord(buf, protocol.ItemRecord{
					PlayerItemID: 10001,
					ItemID:       protocol.ItemIDSteamedBun,
					GridID:       1,
					Field4:       1,
					Field5:       qty,
				})
				_ = binary.Write(buf, binary.BigEndian, int16(10))
				_ = binary.Write(buf, binary.BigEndian, uint16(0))
				_ = protocol.WritePacket(srvConn, protocol.NewPacket(protocol.ActionIDItemGetPackList, buf.Bytes()))
				return true

			case protocol.ActionIDItemSell:
				sellAttempts.Add(1)
				_ = protocol.WritePacket(srvConn, protocol.NewPacket(protocol.ActionIDItemSell, []byte{protocol.ItemActionSuccess}))
				return true
			}
			return false
		})

		cfg := dungeon.SweepConfig{
			Sweep: dungeon.SweepFeatureConfig{
				Enabled:        true,
				Pipeline:       []string{"sweep_elite"},
				SellSweptItems: true,
			},
		}
		if err := dungeon.NewDungeonSweepRoutine(nil, cfg).Execute(context.Background(), sess, nil); err != nil {
			t.Fatalf("执行扫荡失败: %v", err)
		}
		if sellAttempts.Load() != 0 {
			t.Errorf("已有消耗品堆叠数量增加时不得整格出售, 实际尝试 %d 次", sellAttempts.Load())
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
	}

	routine := dungeon.NewDungeonSweepRoutine(nil, cfg)
	_ = routine.Execute(context.Background(), sess, nil)

	// 验证最多只消耗了 1 次粽子 (+10 点体力)，没有继续消耗茶叶蛋
	if atomic.LoadInt32(&usedCount) > 1 {
		t.Errorf("期望达到体力点数上限后停止吃药，实际吃药次数: %d", atomic.LoadInt32(&usedCount))
	}
}

func TestSixRealmsRoutine_PerFloorRetry(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()
	defer serverConn.Close()

	var attempts atomic.Int32

	go func() {
		for {
			pkt, err := protocol.ReadPacket(serverConn)
			if err != nil {
				return
			}
			switch pkt.ActionID {
			case protocol.ActionIDTowerGetInfo:
				w := protocol.NewWriter()
				w.WriteUint8(25) // SequenceMax
				w.WriteUint8(2)  // Layer: 2 (饿鬼道)
				w.WriteUint8(20) // SequenceWar: 20
				w.WriteUint8(5)  // BagNum
				w.WriteUint8(1)  // ResetNum
				w.WriteUint16(0) // ResetIngot
				w.WriteUint8(0)  // HideID
				w.WriteInt32(20) // HighStone
				_ = protocol.WritePacket(serverConn, protocol.NewPacket(protocol.ActionIDTowerGetInfo, w.Bytes()))

			case protocol.ActionIDTowerStartChallenge:
				attempts.Add(1)
				// 模拟挑战失败 (非 0 状态码，如 99 战败)
				cw := protocol.NewWriter()
				cw.WriteUint8(99) // Failure
				cw.WriteInt32(0)
				cw.WriteInt32(0)
				cw.WriteInt32(0)
				cw.WriteInt32(0)
				_ = protocol.WritePacket(serverConn, protocol.NewPacket(protocol.ActionIDTowerStartChallenge, cw.Bytes()))
			}
		}
	}()

	session := client.NewRoleSession(client.SessionConfig{
		RoleID:            "role-retry-test",
		RoleName:          "重试测试玩家",
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

	cfg := dungeon.SixRealmsConfig{
		AutoReset:       false,
		AutoSellBlue:    true,
		AutoSellPurple:  false,
		MaxFloorRetries: 2, // 配置最大重试 2 次
	}
	routine := dungeon.NewSixRealmsRoutine(cfg)
	err := routine.Execute(context.Background(), session, nil)
	if err != nil {
		t.Fatalf("SixRealmsRoutine.Execute failed: %v", err)
	}

	if total := attempts.Load(); total != 2 {
		t.Errorf("expected exactly 2 challenge attempts for failed floor, got %d", total)
	}
}

// TestDungeonSweep_QuestAcceptLostResponseRecovery 验证接取任务回包丢失（网关偶发超时）时的自愈能力:
// 1. 服务端实际已受理 accept_quest，但第一个回包被网关丢弃 ->
// 2. 客户端复核 town_quest_show 发现任务已进入 Accepted(1) ->
// 3. 继续推进剧情副本并交付，而不是误判失败后跳过整轮接取。
func TestDungeonSweep_QuestAcceptLostResponseRecovery(t *testing.T) {
	sess, srvConn := createTestSession(t, 100)
	defer sess.Close()
	defer srvConn.Close()

	sess.UpdatePlayerState(func(ps *client.PlayerState) {
		ps.Level = 63
	})

	repo := &mockQuestDungeonRepo{
		quests: map[int]*dictionary.Quest{
			336: {
				ID:         336,
				Type:       4,
				Lock:       10100,
				Level:      1,
				Title:      "精英罗虎王",
				BeginNPCID: 53,
				EndNPCID:   53,
				MissionID:  456,
				AwardExp:   440000, AwardCoins: 88000,
			},
		},
		missions: map[int]*dictionary.Mission{
			456: {
				ID:          456,
				SectionID:   29,
				MissionLock: 22046,
				MapID:       13,
				Name:        "精英罗虎王",
				Type:        1,
				IsBoss:      true,
				Monsters:    "1041",
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

	var accepted, cleared, completed, acceptDropDone atomic.Bool

	startMockDungeonServer(srvConn, func(pkt *protocol.Packet) bool {
		switch pkt.ActionID {
		case protocol.ActionIDQuestList:
			recordEvent("quest_list")
			w := protocol.NewWriter()
			if completed.Load() {
				w.WriteUint16(0)
				_ = protocol.WritePacket(srvConn, protocol.NewPacket(protocol.ActionIDQuestList, w.Bytes()))
				return true
			}
			state, cur := int32(0), int32(0)
			if accepted.Load() {
				state, cur = 1, 0
			}
			if cleared.Load() {
				state, cur = 2, 1
			}
			w.WriteUint16(1)
			w.WriteInt32(336)
			w.WriteInt32(state)
			w.WriteUint8(0)
			w.WriteUint16(1)
			w.WriteInt32(1041)
			w.WriteInt32(cur)
			w.WriteInt32(1)
			_ = protocol.WritePacket(srvConn, protocol.NewPacket(protocol.ActionIDQuestList, w.Bytes()))
			return true

		case protocol.ActionIDQuestGetNPCQuests:
			recordEvent("get_npc_quests")
			w := protocol.NewWriter()
			w.WriteUint16(1)
			w.WriteInt32(336)
			if accepted.Load() {
				w.WriteUint8(1) // 1=已接取
			} else {
				w.WriteUint8(0) // 0=可接取
			}
			_ = protocol.WritePacket(srvConn, protocol.NewPacket(protocol.ActionIDQuestGetNPCQuests, w.Bytes()))
			return true

		case protocol.ActionIDQuestAccept:
			recordEvent("accept_quest")
			accepted.Store(true)
			// 首个接取回包模拟网关丢包：服务端已受理但不回包。
			if !acceptDropDone.Swap(true) {
				return true
			}
			_ = protocol.WritePacket(srvConn, protocol.NewPacket(protocol.ActionIDQuestAccept, []byte{0x05}))
			return true

		case protocol.ActionIDMissionGetSections:
			recordEvent("get_sections")
			w := protocol.NewWriter()
			w.WriteUint16(0)
			_ = protocol.WritePacket(srvConn, protocol.NewPacket(protocol.ActionIDMissionGetSections, w.Bytes()))
			return true

		case protocol.ActionIDMissionPlotEnter:
			mid := int32(0)
			if len(pkt.Payload) >= 4 {
				mid = int32(binary.BigEndian.Uint32(pkt.Payload[:4]))
			}
			recordEvent(fmt.Sprintf("plot_enter:%d", mid))
			_ = protocol.WritePacket(srvConn, protocol.NewPacket(protocol.ActionIDMissionPlotEnter, []byte{protocol.MissionResultSuccess}))
			return true

		case protocol.ActionIDMissionFightMonster:
			mt := int32(0)
			if len(pkt.Payload) >= 4 {
				mt = int32(binary.BigEndian.Uint32(pkt.Payload[:4]))
			}
			recordEvent(fmt.Sprintf("fight_monster:%d", mt))
			cleared.Store(true)
			_ = protocol.WritePacket(srvConn, protocol.NewPacket(protocol.ActionIDMissionFightMonster, []byte{0x00, protocol.MissionResultSuccess}))
			return true

		case protocol.ActionIDMissionRank:
			recordEvent("rank_mission")
			_ = protocol.WritePacket(srvConn, protocol.NewPacket(protocol.ActionIDMissionRank, []byte{0x00}))
			return true

		case protocol.ActionIDMissionOpenBox:
			recordEvent("open_box")
			_ = protocol.WritePacket(srvConn, protocol.NewPacket(protocol.ActionIDMissionOpenBox, []byte{0x00}))
			return true

		case protocol.ActionIDMissionPickupAward:
			recordEvent("pickup_award")
			_ = protocol.WritePacket(srvConn, protocol.NewPacket(protocol.ActionIDMissionPickupAward, []byte{0x00}))
			return true

		case protocol.ActionIDQuestSubmitComplete:
			recordEvent("complete_quest")
			completed.Store(true)
			_ = protocol.WritePacket(srvConn, protocol.NewPacket(protocol.ActionIDQuestSubmitComplete, []byte{0x01}))
			return true
		}
		return false
	})

	cfg := dungeon.SweepConfig{Pipeline: []string{"quest_cycle"}}
	routine := dungeon.NewDungeonSweepRoutine(repo, cfg)
	if err := routine.Execute(context.Background(), sess, nil); err != nil && !errors.Is(err, scheduler.ErrStaminaDepleted) {
		t.Fatalf("接取回包丢失后的任务推图执行失败: %v", err)
	}

	eventsMu.Lock()
	defer eventsMu.Unlock()

	joined := strings.Join(events, ",")
	if !strings.Contains(joined, "accept_quest") {
		t.Fatalf("未发送 accept_quest, events=%v", events)
	}
	// 直接接取优先：NPC 对话仅作为无回包时的兜底，不应出现在正常路径中。
	// 服务端已受理但回包丢失时，应通过任务列表复核确认接取生效。
	firstAcceptIdx := -1
	for i, evt := range events {
		if evt == "accept_quest" && firstAcceptIdx == -1 {
			firstAcceptIdx = i
		}
	}
	if firstAcceptIdx == -1 {
		t.Fatalf("未发送 accept_quest, events=%v", events)
	}
	if !strings.Contains(joined, "plot_enter:456") {
		t.Fatalf("接取回包丢失后未继续推进剧情副本, events=%v", events)
	}
	if !strings.Contains(joined, "fight_monster:1041") {
		t.Fatalf("接取回包丢失后未挑战关卡怪物, events=%v", events)
	}
	if !strings.Contains(joined, "complete_quest") {
		t.Fatalf("接取回包丢失后未继续交付任务, events=%v", events)
	}
}

// lockedLogBuffer 提供带锁的日志捕获缓冲，避免 Mock 服务端协程与测试主协程并发写入产生数据竞争。
type lockedLogBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedLogBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedLogBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// TestDungeonSweep_GracefulShutdownStopsQuestCycle 验证停机（上下文取消/会话关闭）时推图循环立即静默退出:
// 不应继续进入后续关卡、也不应把撤站导致的通信失败记录成业务告警。
func TestDungeonSweep_GracefulShutdownStopsQuestCycle(t *testing.T) {
	sess, srvConn := createTestSession(t, 100)
	defer sess.Close()
	defer srvConn.Close()

	sess.UpdatePlayerState(func(ps *client.PlayerState) {
		ps.Level = 63
	})

	repo := &mockQuestDungeonRepo{
		quests: map[int]*dictionary.Quest{
			336: {
				ID:        336,
				Type:      4,
				Lock:      10100,
				Level:     1,
				Title:     "精英罗虎王",
				MissionID: 456,
				AwardExp:  440000, AwardCoins: 88000,
			},
		},
		missions: map[int]*dictionary.Mission{
			456: {
				ID:          456,
				SectionID:   29,
				MissionLock: 22046,
				MapID:       13,
				Name:        "精英罗虎王",
				Type:        1,
				IsBoss:      true,
				Monsters:    "1041",
			},
		},
	}

	ctx, cancel := context.WithCancel(context.Background())
	var cancelOnce sync.Once
	startMockDungeonServer(srvConn, func(pkt *protocol.Packet) bool {
		switch pkt.ActionID {
		case protocol.ActionIDQuestList:
			w := protocol.NewWriter()
			w.WriteUint16(1)
			w.WriteInt32(336)
			w.WriteInt32(1) // 已接取进行中
			w.WriteUint8(0)
			w.WriteUint16(1)
			w.WriteInt32(1041)
			w.WriteInt32(0)
			w.WriteInt32(1)
			_ = protocol.WritePacket(srvConn, protocol.NewPacket(protocol.ActionIDQuestList, w.Bytes()))
			return true
		case protocol.ActionIDMissionGetSections:
			// 模拟停机：客户端上下文被取消，服务端不再回包。
			cancelOnce.Do(cancel)
			return true
		}
		return false
	})

	var logs lockedLogBuffer
	prevLogger := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug})))
	defer slog.SetDefault(prevLogger)

	routine := dungeon.NewDungeonSweepRoutine(repo, dungeon.SweepConfig{Pipeline: []string{"quest_cycle"}})
	start := time.Now()
	if err := routine.Execute(ctx, sess, nil); err != nil {
		t.Fatalf("停机时推图 Routine 不应返回错误: %v", err)
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("停机后推图 Routine 未及时退出, 耗时 %v", elapsed)
	}

	logText := logs.String()
	for _, banned := range []string{"进入剧情副本未收到有效响应", "激活章节关卡列表上下文失败"} {
		if strings.Contains(logText, banned) {
			t.Fatalf("停机期间不应产生业务告警 %q, 日志: %s", banned, logText)
		}
	}
}

// TestZodiacRoutine_ChallengeWithLevelCheck 覆盖生肖挑战等级前置校验与挑战成功路径。
func TestZodiacRoutine_ChallengeWithLevelCheck(t *testing.T) {
	session := client.NewRoleSession(client.SessionConfig{
		RoleID:     "role_test_zodiac",
		RoleName:   "测试生肖角色",
		ServerAddr: "mock",
	})
	if err := session.Start(context.Background()); err != nil {
		t.Fatalf("启动测试会话失败: %v", err)
	}
	defer session.Close()
	time.Sleep(100 * time.Millisecond)
	session.UpdatePlayerState(func(ps *client.PlayerState) { ps.Level = 300 })

	r := dungeon.NewZodiacRoutine()
	if err := r.Execute(context.Background(), session, nil); err != nil {
		t.Fatalf("生肖挑战任务执行失败: %v", err)
	}
}

// TestSpiritTrialRoutine_Execute 覆盖精灵试炼模式领奖与挑战路径。
func TestSpiritTrialRoutine_Execute(t *testing.T) {
	session := client.NewRoleSession(client.SessionConfig{
		RoleID: "role_test_spirit_trial", RoleName: "测试精灵试炼角色", ServerAddr: "mock",
	})
	if err := session.Start(context.Background()); err != nil {
		t.Fatalf("启动测试会话失败: %v", err)
	}
	defer session.Close()
	time.Sleep(100 * time.Millisecond)
	if err := dungeon.NewSpiritTrialRoutine().Execute(context.Background(), session, nil); err != nil {
		t.Fatalf("精灵试炼任务执行失败: %v", err)
	}
}

// TestDungeonSweep_EliteSweep_AllTowns 回归验证:
// 精英副本扫荡按城镇逐个执行，不设本地轮数上限。
// 数据字典不可用时使用 22 个内置精英城镇，仍应扫荡全部城镇。
func TestDungeonSweep_EliteSweep_AllTowns(t *testing.T) {
	sess, srvConn := createTestSession(t, 200)
	defer sess.Close()
	defer srvConn.Close()

	var heroSweepCount atomic.Int32
	startMockDungeonServer(srvConn, func(pkt *protocol.Packet) bool {
		switch pkt.ActionID {
		case protocol.ActionHeroMissionList:
			w := protocol.NewWriter()
			w.WriteUint16(1)
			w.WriteInt32(166)
			w.WriteInt8(1)
			w.WriteUint8(0)
			w.WriteInt8(1)
			w.WriteInt8(1)
			w.WriteInt8(1)
			w.WriteInt16(10)
			w.WriteInt32(1)
			_ = protocol.WritePacket(srvConn, protocol.NewPacket(protocol.ActionHeroMissionList, w.Bytes()))
			return true
		case protocol.ActionHeroMissionStartPractice:
			heroSweepCount.Add(1)
			w := protocol.NewWriter()
			w.WriteUint8(protocol.HeroMissionResultSuccess)
			w.WriteInt32(30)
			_ = protocol.WritePacket(srvConn, protocol.NewPacket(protocol.ActionHeroMissionStartPractice, w.Bytes()))
			return true
		case protocol.ActionHeroMissionQuickly:
			_ = protocol.WritePacket(srvConn, protocol.NewPacket(protocol.ActionHeroMissionQuickly, []byte{protocol.HeroMissionResultSuccess}))
			w := protocol.NewWriter()
			w.WriteUint8(protocol.HeroMissionNotifyFinish)
			w.WriteUint16(0)
			w.WriteUint16(0)
			w.WriteInt16(30)
			_ = protocol.WritePacket(srvConn, protocol.NewPacket(protocol.ActionHeroMissionNotify, w.Bytes()))
			return true
		}
		return false
	})

	routine := dungeon.NewDungeonSweepRoutine(nil, dungeon.SweepConfig{
		Sweep: dungeon.SweepFeatureConfig{
			Enabled:  true,
			Pipeline: []string{"sweep_elite"},
		},
	})
	if err := routine.Execute(context.Background(), sess, nil); err != nil {
		t.Fatalf("执行精英副本扫荡失败: %v", err)
	}
	if got := heroSweepCount.Load(); got != 22 {
		t.Fatalf("期望扫荡全部 22 个精英城镇, 实际 %d 个", got)
	}
}

// TestDungeonSweep_QuestAcceptFallsBackToNPCDialog 验证接取任务时优先直接发送
// accept_quest；仅当直接接取无回包且服务端任务列表仍未生效时，才补发
// npc_info_by_npcid 建立官方 NPC 对话前置链并重试。
//
// 背景（2026-11-06 实机）：任务 NPC 位于仙界时，主世界会话的
// npc_info_by_npcid 会被服务端静默丢弃；若仍坚持对话优先，
// 会连续超时并阻塞整轮任务循环。
func TestDungeonSweep_QuestAcceptFallsBackToNPCDialog(t *testing.T) {
	sess, srvConn := createTestSession(t, 100)
	defer sess.Close()
	defer srvConn.Close()

	sess.UpdatePlayerState(func(ps *client.PlayerState) {
		ps.Level = 80
	})

	repo := &mockQuestDungeonRepo{
		quests: map[int]*dictionary.Quest{
			145: {
				ID:         145,
				Type:       1,
				Level:      1,
				Title:      "蚕丝",
				BeginNPCID: 48,
				EndNPCID:   48,
				MissionID:  9999, // 字典中不存在，接取后即可结束循环
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

	var accepted atomic.Bool
	var acceptCalls atomic.Int32

	startMockDungeonServer(srvConn, func(pkt *protocol.Packet) bool {
		switch pkt.ActionID {
		case protocol.ActionIDQuestList:
			recordEvent("quest_list")
			w := protocol.NewWriter()
			w.WriteUint16(1)
			w.WriteInt32(145)
			if accepted.Load() {
				w.WriteInt32(1) // Accepted
			} else {
				w.WriteInt32(0) // Acceptable
			}
			w.WriteUint8(0)
			w.WriteUint16(1)
			w.WriteInt32(1)
			w.WriteInt32(0)
			w.WriteInt32(1)
			_ = protocol.WritePacket(srvConn, protocol.NewPacket(protocol.ActionIDQuestList, w.Bytes()))
			return true

		case protocol.ActionIDQuestAccept:
			recordEvent("accept_quest")
			// 第一次无回包且不置为已接取，逼迫走 NPC 对话兜底路径。
			if acceptCalls.Add(1) == 1 {
				return true
			}
			accepted.Store(true)
			_ = protocol.WritePacket(srvConn, protocol.NewPacket(protocol.ActionIDQuestAccept, []byte{0x05}))
			return true

		case protocol.ActionIDQuestGetNPCQuests:
			recordEvent("get_npc_quests")
			w := protocol.NewWriter()
			w.WriteUint16(1)
			w.WriteInt32(145)
			w.WriteUint8(0) // 可接取
			_ = protocol.WritePacket(srvConn, protocol.NewPacket(protocol.ActionIDQuestGetNPCQuests, w.Bytes()))
			return true
		}
		return false
	})

	routine := dungeon.NewDungeonSweepRoutine(repo, dungeon.SweepConfig{
		Pipeline: []string{"quest_cycle"},
	})
	if err := routine.Execute(context.Background(), sess, nil); err != nil &&
		!errors.Is(err, scheduler.ErrStaminaDepleted) {
		t.Fatalf("任务接取兜底流程执行失败: %v", err)
	}

	eventsMu.Lock()
	defer eventsMu.Unlock()
	joined := strings.Join(events, ",")
	firstAccept := strings.Index(joined, "accept_quest")
	dialog := strings.Index(joined, "get_npc_quests")
	secondAccept := -1
	if firstAccept >= 0 && dialog > firstAccept {
		if rel := strings.Index(joined[dialog:], "accept_quest"); rel >= 0 {
			secondAccept = dialog + rel
		}
	}
	if firstAccept < 0 || dialog < 0 || secondAccept < 0 || !(firstAccept < dialog && dialog < secondAccept) {
		t.Fatalf("期望顺序 accept_quest -> get_npc_quests -> accept_quest, 实际 events=%v", events)
	}
	if !accepted.Load() {
		t.Fatalf("兜底重试后服务端仍未收到 accept_quest, events=%v", events)
	}
}

// TestZodiacRoutine_SkipsWhenPanelEmpty 验证生肖面板全 0（功能未开放/未解锁）时
// 不再发送 challenge 请求，避免服务端静默丢弃后把任务标记为执行异常。
func TestZodiacRoutine_SkipsWhenPanelEmpty(t *testing.T) {
	sess, srvConn := createTestSession(t, 100)
	defer sess.Close()
	defer srvConn.Close()

	var challengeCalls atomic.Int32
	startMockDungeonServer(srvConn, func(pkt *protocol.Packet) bool {
		switch pkt.ActionID {
		case protocol.ActionIDZodiacInfo:
			payload := make([]byte, 13) // 全 0 面板
			_ = protocol.WritePacket(srvConn, protocol.NewPacket(protocol.ActionIDZodiacInfo, payload))
			return true
		case protocol.ActionIDZodiacChallenge:
			challengeCalls.Add(1)
			return true
		}
		return false
	})

	routine := dungeon.NewZodiacRoutine()
	if err := routine.Execute(context.Background(), sess, nil); err != nil {
		t.Fatalf("生肖面板为空时应安全跳过, got %v", err)
	}
	if got := challengeCalls.Load(); got != 0 {
		t.Fatalf("面板为空时不应发送 challenge, 实际 %d 次", got)
	}
}

// TestSellBagCraftScrolls 验证背包满时按格位出售装备制作卷。
func TestSellBagCraftScrolls(t *testing.T) {
	dictRepo, err := dictionary.NewRepository("")
	if err != nil {
		t.Fatalf("load dictionary: %v", err)
	}
	defer dictRepo.Close()

	sess, srvConn := createTestSession(t, 100)
	defer sess.Close()
	var soldGrid int32
	startMockDungeonServer(srvConn, func(pkt *protocol.Packet) bool {
		switch pkt.ActionID {
		case protocol.ActionIDItemGetPackList:
			buf := new(bytes.Buffer)
			_ = binary.Write(buf, binary.BigEndian, int16(30))
			_ = binary.Write(buf, binary.BigEndian, int32(0))
			_ = binary.Write(buf, binary.BigEndian, uint16(1))
			_ = protocol.WriteItemRecord(buf, protocol.ItemRecord{
				PlayerItemID: 1001,
				ItemID:       940, // 逍遥戟制作卷
				GridID:       5,
			})
			_ = binary.Write(buf, binary.BigEndian, int16(10))
			_ = binary.Write(buf, binary.BigEndian, uint16(0))
			_ = protocol.WritePacket(srvConn, protocol.NewPacket(protocol.ActionIDItemGetPackList, buf.Bytes()))
			return true
		case protocol.ActionIDItemSell:
			if len(pkt.Payload) >= 2 {
				soldGrid = int32(int16(binary.BigEndian.Uint16(pkt.Payload[:2])))
			}
			_ = protocol.WritePacket(srvConn, protocol.NewPacket(protocol.ActionIDItemSell, []byte{protocol.ItemActionSuccess}))
			return true
		}
		return false
	})

	sold := dungeon.SellBagCraftScrolls(context.Background(), sess, dictRepo, 5)
	if sold != 1 {
		t.Fatalf("sold = %d, want 1", sold)
	}
	if soldGrid != 5 {
		t.Fatalf("sold grid = %d, want 5", soldGrid)
	}
}

// TestSixRealmsRoutine_BagNum20DoesNotBlock 验证 bag_num=20 不再被误判为封灵背包已满，
// 仍会发起挑战（真正满包由服务端 FULL_BAG 结果码熔断）。
func TestSixRealmsRoutine_BagNum20DoesNotBlock(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()
	defer serverConn.Close()

	var challengeCalls atomic.Int32
	go func() {
		for {
			pkt, err := protocol.ReadPacket(serverConn)
			if err != nil {
				return
			}
			switch pkt.ActionID {
			case protocol.ActionIDTowerGetInfo:
				w := protocol.NewWriter()
				w.WriteUint8(25) // SequenceMax
				w.WriteUint8(1)  // Layer
				w.WriteUint8(1)  // SequenceWar
				w.WriteUint8(20) // BagNum (线上空背包也返回 20)
				w.WriteUint8(0)  // ResetNum
				w.WriteUint16(0) // ResetIngot
				w.WriteUint8(0)  // HideID
				w.WriteInt32(20) // HighStone
				_ = protocol.WritePacket(serverConn, protocol.NewPacket(protocol.ActionIDTowerGetInfo, w.Bytes()))
			case protocol.ActionIDTowerStartChallenge:
				challengeCalls.Add(1)
				cw := protocol.NewWriter()
				cw.WriteUint8(protocol.TowerResultSuccess)
				cw.WriteInt32(1001)
				cw.WriteInt32(100000)
				cw.WriteInt32(200)
				cw.WriteInt32(10000)
				_ = protocol.WritePacket(serverConn, protocol.NewPacket(protocol.ActionIDTowerStartChallenge, cw.Bytes()))
			}
		}
	}()

	session := client.NewRoleSession(client.SessionConfig{
		RoleID: "role-six-realms-bag20", RoleName: "六道测试", ServerAddr: "mock-pipe",
		HeartbeatInterval: time.Second,
		Dialer:            func(ctx context.Context, network, addr string) (net.Conn, error) { return clientConn, nil },
		Authenticator:     func(*client.RoleSession) error { return nil },
	})
	_ = session.Start(context.Background())
	defer session.Close()
	deadline := time.Now().Add(2 * time.Second)
	for session.State() != client.StateActive && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}

	if err := dungeon.NewSixRealmsRoutine().Execute(context.Background(), session, nil); err != nil {
		t.Fatalf("SixRealmsRoutine.Execute failed: %v", err)
	}
	if got := challengeCalls.Load(); got == 0 {
		t.Fatal("bag_num=20 不应阻止六道挑战, 实际挑战 0 次")
	}
}

// TestQiMenRoutine_UsesMiracleFightersModules 验证奇门遁甲改走 251/392，
// 执行一键普通修炼与突破，且不再请求旧的 486/485/584 动作。
func TestQiMenRoutine_UsesMiracleFightersModules(t *testing.T) {
	sess, srvConn := createTestSession(t, 100)
	defer sess.Close()
	defer srvConn.Close()

	var oneKeyCalls, breakCalls, oldModuleCalls atomic.Int32
	startMockDungeonServer(srvConn, func(pkt *protocol.Packet) bool {
		switch pkt.ActionID {
		case protocol.ActionIDMiracleFightersInfo:
			w := protocol.NewWriter()
			w.WriteUint8(protocol.MiracleFightersResultSuccess)
			w.WriteInt32(1)
			w.WriteUint8(0)
			w.WriteUint16(1)
			w.WriteInt32(1)
			_ = protocol.WritePacket(srvConn, protocol.NewPacket(pkt.ActionID, w.Bytes()))
			return true
		case protocol.ActionIDMiracleFightersOneKey:
			oneKeyCalls.Add(1)
			_ = protocol.WritePacket(srvConn, protocol.NewPacket(pkt.ActionID, []byte{protocol.MiracleFightersResultSuccess}))
			return true
		case protocol.ActionIDMiracleFightersBreakThru:
			breakCalls.Add(1)
			_ = protocol.WritePacket(srvConn, protocol.NewPacket(pkt.ActionID, []byte{protocol.MiracleFightersResultSuccess}))
			return true
		case protocol.ActionIDMiracleFightersTwoInfo:
			// 奇门遁甲二返回未开放，结束该模块。
			w := protocol.NewWriter()
			w.WriteUint8(protocol.MiracleFightersResultFuncUnopen)
			w.WriteInt32(0)
			w.WriteUint8(0)
			w.WriteUint16(0)
			_ = protocol.WritePacket(srvConn, protocol.NewPacket(pkt.ActionID, w.Bytes()))
			return true
		case 0x01E60007, 0x01E50000, 0x01E60001, 0x02480001, 0x02480003:
			oldModuleCalls.Add(1)
			return true
		}
		return false
	})

	if err := dungeon.NewQiMenRoutine().Execute(context.Background(), sess, nil); err != nil {
		t.Fatalf("qimen: %v", err)
	}
	if oneKeyCalls.Load() != 1 || breakCalls.Load() != 1 {
		t.Fatalf("期望 1 次一键修炼 + 1 次突破, 实际 oneKey=%d break=%d",
			oneKeyCalls.Load(), breakCalls.Load())
	}
	if oldModuleCalls.Load() != 0 {
		t.Fatalf("不应再使用旧模块 486/485/584, 实际请求 %d 次", oldModuleCalls.Load())
	}
}

// TestSixRealmsRoutine_ClearedAtSequenceMaxSkips 验证全通边界：
// SequenceWar 等于 SequenceMax（线上全通返回 25/25）时不应再挑战最后一层。
func TestSixRealmsRoutine_ClearedAtSequenceMaxSkips(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()
	defer serverConn.Close()

	var challenges atomic.Int32
	go func() {
		for {
			pkt, err := protocol.ReadPacket(serverConn)
			if err != nil {
				return
			}
			switch pkt.ActionID {
			case protocol.ActionIDTowerGetInfo:
				w := protocol.NewWriter()
				w.WriteUint8(25) // SequenceMax
				w.WriteUint8(12) // Layer
				w.WriteUint8(25) // SequenceWar == max -> 已全通
				w.WriteUint8(20) // BagNum
				w.WriteUint8(6)  // ResetNum
				w.WriteUint16(0) // ResetIngot
				w.WriteUint8(0)  // HideID
				w.WriteInt32(0)  // HighStone
				_ = protocol.WritePacket(serverConn, protocol.NewPacket(pkt.ActionID, w.Bytes()))
			case protocol.ActionIDTowerStartChallenge:
				challenges.Add(1)
				_ = protocol.WritePacket(serverConn, protocol.NewPacket(pkt.ActionID,
					[]byte{protocol.TowerResultSuccess}))
			}
		}
	}()

	session := client.NewRoleSession(client.SessionConfig{
		RoleID:     "role-six-realms-cleared",
		RoleName:   "六道全通测试",
		ServerAddr: "mock-pipe",
		Dialer: func(ctx context.Context, network, addr string) (net.Conn, error) {
			return clientConn, nil
		},
		Authenticator: func(s *client.RoleSession) error { return nil },
	})
	_ = session.Start(context.Background())
	defer session.Close()
	deadline := time.Now().Add(2 * time.Second)
	for session.State() != client.StateActive && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}

	cfg := dungeon.DefaultSixRealmsConfig()
	cfg.AutoReset = false
	if err := dungeon.NewSixRealmsRoutine(cfg).Execute(context.Background(), session, nil); err != nil {
		t.Fatalf("six realms: %v", err)
	}
	if got := challenges.Load(); got != 0 {
		t.Fatalf("全通后不应再挑战, challenge calls = %d", got)
	}
}

// TestSixRealmsRoutine_InvalidLayerStopsImmediately 验证最后一层返回 INVALID_LAYER(1)
// 时立即结束，不再重试 3 次。
func TestSixRealmsRoutine_InvalidLayerStopsImmediately(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()
	defer serverConn.Close()

	var challenges atomic.Int32
	go func() {
		for {
			pkt, err := protocol.ReadPacket(serverConn)
			if err != nil {
				return
			}
			switch pkt.ActionID {
			case protocol.ActionIDTowerGetInfo:
				w := protocol.NewWriter()
				w.WriteUint8(25) // SequenceMax
				w.WriteUint8(12) // Layer
				w.WriteUint8(24) // SequenceWar
				w.WriteUint8(5)  // BagNum
				w.WriteUint8(0)  // ResetNum
				w.WriteUint16(0) // ResetIngot
				w.WriteUint8(0)
				w.WriteInt32(0)
				_ = protocol.WritePacket(serverConn, protocol.NewPacket(pkt.ActionID, w.Bytes()))
			case protocol.ActionIDTowerStartChallenge:
				challenges.Add(1)
				_ = protocol.WritePacket(serverConn, protocol.NewPacket(pkt.ActionID,
					[]byte{protocol.TowerResultInvalidLayer}))
			}
		}
	}()

	session := client.NewRoleSession(client.SessionConfig{
		RoleID:     "role-six-realms-invalid",
		RoleName:   "六道无效层测试",
		ServerAddr: "mock-pipe",
		Dialer: func(ctx context.Context, network, addr string) (net.Conn, error) {
			return clientConn, nil
		},
		Authenticator: func(s *client.RoleSession) error { return nil },
	})
	_ = session.Start(context.Background())
	defer session.Close()
	deadline := time.Now().Add(2 * time.Second)
	for session.State() != client.StateActive && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}

	cfg := dungeon.DefaultSixRealmsConfig()
	cfg.AutoReset = false
	if err := dungeon.NewSixRealmsRoutine(cfg).Execute(context.Background(), session, nil); err != nil {
		t.Fatalf("six realms: %v", err)
	}
	if got := challenges.Load(); got != 1 {
		t.Fatalf("INVALID_LAYER 后应立即结束, challenge calls = %d", got)
	}
}

// TestDungeonSweep_GlobalElite 验证 global_elite 开关走 Module 25：
// status(精英副本ID) 取子关卡后 start(子关卡,1,自动出售)。
func TestDungeonSweep_GlobalElite(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()
	defer serverConn.Close()

	var statusCalls, startCalls atomic.Int32
	go func() {
		for {
			pkt, err := protocol.ReadPacket(serverConn)
			if err != nil {
				return
			}
			switch pkt.ActionID {
			case protocol.ActionIDPracticeGetStatus:
				statusCalls.Add(1)
				w := protocol.NewWriter()
				w.WriteBytes([]byte("FP"))
				w.WriteUint16(1)
				w.WriteInt32(448)
				w.WriteInt16(31)
				w.WriteInt8(1)
				w.WriteUint8(0)
				w.WriteUint32(100)
				w.WriteUint32(200)
				_ = protocol.WritePacket(serverConn, protocol.NewPacket(pkt.ActionID, w.Bytes()))
			case protocol.ActionIDPracticeStart:
				startCalls.Add(1)
				_ = protocol.WritePacket(serverConn, protocol.NewPacket(pkt.ActionID, []byte{protocol.PracticeResultSuccess}))
			}
		}
	}()

	sess := client.NewRoleSession(client.SessionConfig{
		RoleID:     "role-global-elite",
		RoleName:   "全局扫荡测试",
		ServerAddr: "mock-pipe",
		Dialer: func(ctx context.Context, network, addr string) (net.Conn, error) {
			return clientConn, nil
		},
		Authenticator: func(s *client.RoleSession) error { return nil },
	})
	_ = sess.Start(context.Background())
	defer sess.Close()
	deadline := time.Now().Add(2 * time.Second)
	for sess.State() != client.StateActive && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	sess.SetStamina(100)

	repo := &mockQuestDungeonRepo{eliteIDs: []int{166}}
	cfg := dungeon.DefaultSweepConfig()
	cfg.Sweep.Enabled = true
	cfg.Sweep.GlobalElite = true
	cfg.Sweep.SellSweptItems = false

	if err := dungeon.NewDungeonSweepRoutine(repo, cfg).Execute(context.Background(), sess, nil); err != nil {
		t.Fatalf("global elite sweep: %v", err)
	}
	if statusCalls.Load() != 1 || startCalls.Load() != 1 {
		t.Fatalf("expected 1 status + 1 start, got status=%d start=%d", statusCalls.Load(), startCalls.Load())
	}
}
