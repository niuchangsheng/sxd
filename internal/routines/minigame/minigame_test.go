// Package minigame 提供吉星高照、仙旅奇缘答题、刮刮卡、钓鱼等益智趣味活动。
//
// @author Ateng
// @since 2026-10-11
package minigame

import (
	"bytes"
	"context"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"sxd-pie-ng/internal/client"
	"sxd-pie-ng/internal/dictionary"
	"sxd-pie-ng/internal/protocol"
	"sxd-pie-ng/internal/qa"
	"sxd-pie-ng/internal/scheduler"
)

func TestImmortalFantasyRoutine_Execute(t *testing.T) {
	session := client.NewRoleSession(client.SessionConfig{
		RoleID:     "role_test_minigame",
		RoleName:   "测试仙旅角色",
		ServerAddr: "mock",
	})
	if err := session.Start(context.Background()); err != nil {
		t.Fatalf("启动测试会话失败: %v", err)
	}
	defer session.Close()

	time.Sleep(100 * time.Millisecond)

	qaEngine, err := qa.NewEngine("")
	if err != nil {
		t.Fatalf("加载 QA 引擎失败: %v", err)
	}

	t.Run("仙旅奇缘全链路事件抉择闭环", func(t *testing.T) {
		routine := NewTravelEventRoutine(qaEngine)
		if routine.Name() != "travel_event" {
			t.Fatalf("任务名称不匹配: %s", routine.Name())
		}
		if dr, ok := routine.(scheduler.DetailedRoutine); ok {
			if dr.Domain() != "minigame" {
				t.Fatalf("领域不匹配: %s", dr.Domain())
			}
		}

		err := routine.Execute(context.Background(), session, nil)
		if err != nil {
			t.Fatalf("仙旅奇缘例程执行失败: %v", err)
		}
		// Mock 每日 3 次，执行后最后一次动作必须是答案提交
		if got := session.LastActionID(); got != protocol.ActionIDFateAnswer {
			t.Errorf("期望最后动作 0x%08X, 实际 0x%08X", protocol.ActionIDFateAnswer, got)
		}
	})

	t.Run("金榜题名会试答题", func(t *testing.T) {
		r2 := NewImperialExamRoutine(qaEngine)
		if err := r2.Execute(context.Background(), session, nil); err != nil {
			t.Fatalf("金榜题名执行失败: %v", err)
		}
	})

	t.Run("个人吉星高照摇骰子与领奖", func(t *testing.T) {
		r3 := NewLuckyStarRoutine()
		if r3.Name() != "lucky_star" {
			t.Fatalf("个人吉星高照任务名称不匹配: %s", r3.Name())
		}
		if err := r3.Execute(context.Background(), session, nil); err != nil {
			t.Fatalf("个人吉星高照执行失败: %v", err)
		}
		// Mock 服务端提供 2 次投掷机会；每次领奖后都会刷新剩余次数，最后动作应为次数查询
		if got := session.LastActionID(); got != protocol.ActionIDRollCakeGetCount {
			t.Errorf("期望最后动作 0x%08X, 实际 0x%08X", protocol.ActionIDRollCakeGetCount, got)
		}
	})

	t.Run("帮派吉星高照摇骰子", func(t *testing.T) {
		r4 := NewFactionLuckyStarRoutine()
		if r4.Name() != "faction_lucky_star" {
			t.Fatalf("帮派吉星高照任务名称不匹配: %s", r4.Name())
		}
		if err := r4.Execute(context.Background(), session, nil); err != nil {
			t.Fatalf("帮派吉星高照执行失败: %v", err)
		}
		// Mock 服务端提供 2 次投掷机会，执行后最后一次动作必须是摇骰结算
		if got := session.LastActionID(); got != protocol.ActionIDFactionRollCake {
			t.Errorf("期望最后动作 0x%08X, 实际 0x%08X", protocol.ActionIDFactionRollCake, got)
		}
	})

	t.Run("悟法领域面板解析与免费感悟", func(t *testing.T) {
		r := NewWuFaRoutine()
		if r.Name() != "wufa_field" {
			t.Fatalf("任务名称不匹配: %s", r.Name())
		}
		if err := r.Execute(context.Background(), session, nil); err != nil {
			t.Fatalf("悟法领域执行失败: %v", err)
		}
		// Mock 面板中秘馆 2 有免费十连，应优先执行十连抽卡。
		if got := session.LastActionID(); got != protocol.ActionIDWuFaDrawCard {
			t.Errorf("期望最后动作 0x%08X, 实际 0x%08X", protocol.ActionIDWuFaDrawCard, got)
		}
	})

	t.Run("空会话防御", func(t *testing.T) {
		routine := NewTravelEventRoutine(qaEngine)
		if err := routine.Execute(context.Background(), nil, nil); err != client.ErrNotConnected {
			t.Errorf("期望 ErrNotConnected, 实际: %v", err)
		}
	})

	t.Run("天帝宝库自选三档大奖配置", func(t *testing.T) {
		thCfg := DefaultTreasureHouseConfig()
		if len(thCfg.Awards) != 3 {
			t.Fatalf("期望默认三档大奖, 实际 %d 档", len(thCfg.Awards))
		}
		r := NewTreasureHouseRoutine(thCfg)
		if r.Name() != "treasure_house" {
			t.Fatalf("任务名称不匹配: %s", r.Name())
		}
		if err := r.Execute(context.Background(), session, nil); err != nil {
			t.Fatalf("天帝宝库执行异常: %v", err)
		}
	})
}

func TestTreasureHouse_InteractiveSelection(t *testing.T) {
	pool := &protocol.TreasureHousePool{
		TotalCount: 45,
		Tier1Items: []int32{50, 12, 9, 5, 2, 7, 1, 15, 11, 3, 14, 4, 6, 10, 8},
		Tier2Items: []int32{21, 30, 23, 18, 20, 51, 27, 29, 28, 22, 26, 17, 24, 19, 25},
		Tier3Items: []int32{42, 45, 33, 40, 31, 34, 41, 36, 37, 44, 32, 39, 43, 35, 38},
	}
	defaults := []protocol.TreasureAwardSelect{
		{TierID: 1, ItemID: 50},
		{TierID: 2, ItemID: 25},
		{TierID: 3, ItemID: 42},
	}

	t.Run("按序号交互选择", func(t *testing.T) {
		input := strings.NewReader("2\n1\n3\n")
		var out bytes.Buffer
		chosen := promptUserAwardSelection(context.Background(), input, &out, pool, nil, defaults)
		if len(chosen) != 3 {
			t.Fatalf("期望三档, 实际: %d", len(chosen))
		}
		if chosen[0].ItemID != 12 { // 序号 2 对应 12
			t.Errorf("Tier 1 期望 12, 实际: %d", chosen[0].ItemID)
		}
		if chosen[1].ItemID != 21 { // 序号 1 对应 21
			t.Errorf("Tier 2 期望 21, 实际: %d", chosen[1].ItemID)
		}
		if chosen[2].ItemID != 33 { // 序号 3 对应 33
			t.Errorf("Tier 3 期望 33, 实际: %d", chosen[2].ItemID)
		}
	})

	t.Run("按道具ID精确交互选择", func(t *testing.T) {
		input := strings.NewReader("id:14\n#25\nid:35\n")
		var out bytes.Buffer
		chosen := promptUserAwardSelection(context.Background(), input, &out, pool, nil, defaults)
		if chosen[0].ItemID != 14 || chosen[1].ItemID != 25 || chosen[2].ItemID != 35 {
			t.Errorf("道具ID匹配不符合预期: %+v", chosen)
		}
	})

	t.Run("按高位道具ID直接交互选择", func(t *testing.T) {
		input := strings.NewReader("50\n25\n42\n")
		var out bytes.Buffer
		chosen := promptUserAwardSelection(context.Background(), input, &out, pool, nil, defaults)
		if chosen[0].ItemID != 50 || chosen[1].ItemID != 25 || chosen[2].ItemID != 42 {
			t.Errorf("大号道具ID直接匹配不符合预期: %+v", chosen)
		}
	})

	t.Run("直接回车使用默认配置", func(t *testing.T) {
		input := strings.NewReader("\n\n\n")
		var out bytes.Buffer
		chosen := promptUserAwardSelection(context.Background(), input, &out, pool, nil, defaults)
		if chosen[0].ItemID != 50 || chosen[1].ItemID != 25 || chosen[2].ItemID != 42 {
			t.Errorf("默认回车项不符合预期: %+v", chosen)
		}
	})

	t.Run("非法输入优雅回退默认", func(t *testing.T) {
		input := strings.NewReader("not_a_number\n9999\n-1\n")
		var out bytes.Buffer
		chosen := promptUserAwardSelection(context.Background(), input, &out, pool, nil, defaults)
		if chosen[0].ItemID != 50 || chosen[1].ItemID != 25 || chosen[2].ItemID != 42 {
			t.Errorf("非法输入回退不符合预期: %+v", chosen)
		}
	})

	t.Run("自定义Selector配置", func(t *testing.T) {
		called := false
		cfg := TreasureHouseConfig{
			Selector: func(ctx context.Context, p *protocol.TreasureHousePool, dict dictionary.Repository) ([]protocol.TreasureAwardSelect, error) {
				called = true
				return []protocol.TreasureAwardSelect{
					{TierID: 1, ItemID: 12},
					{TierID: 2, ItemID: 25},
					{TierID: 3, ItemID: 42},
				}, nil
			},
		}
		r := NewTreasureHouseRoutine(cfg)
		if r == nil {
			t.Fatalf("NewTreasureHouseRoutine 返回 nil")
		}
		if cfg.Selector != nil {
			res, err := cfg.Selector(context.Background(), pool, nil)
			if err != nil || len(res) != 3 || !called {
				t.Fatalf("自定义选择器执行异常: res=%v, err=%v", res, err)
			}
		}
	})
}

func TestFindImmortalRoutine_Execute(t *testing.T) {
	session := client.NewRoleSession(client.SessionConfig{
		RoleID:     "role_test_find_immortal",
		RoleName:   "测试喜从天降角色",
		ServerAddr: "mock",
	})
	if err := session.Start(context.Background()); err != nil {
		t.Fatalf("启动测试会话失败: %v", err)
	}
	defer session.Close()

	time.Sleep(100 * time.Millisecond)

	t.Run("画龙鱼免费闭环", func(t *testing.T) {
		routine := NewFindImmortalRoutine()
		if routine.Name() != "find_immortal" {
			t.Fatalf("任务名称不匹配: %s", routine.Name())
		}
		if dr, ok := routine.(scheduler.DetailedRoutine); ok {
			if dr.Domain() != "minigame" {
				t.Fatalf("领域不匹配: %s", dr.Domain())
			}
		}
		// mock 初始存在一条遗留龙鱼: 首次召唤返回 GOADING，例程必须先收取清空
		// 再重试召唤；若 GOADING 恢复分支缺失，最后动作会停留在召唤而非收奖。
		if err := routine.Execute(context.Background(), session, nil); err != nil {
			t.Fatalf("喜从天降画龙鱼执行失败: %v", err)
		}
		if got := session.LastActionID(); got != protocol.ActionIDFindImmortalPickupAward {
			t.Errorf("期望最后动作为收取龙鱼奖励 0x%08X, 实际 0x%08X", protocol.ActionIDFindImmortalPickupAward, got)
		}
	})

	t.Run("空会话防御", func(t *testing.T) {
		routine := NewFindImmortalRoutine()
		if err := routine.Execute(context.Background(), nil, nil); err != client.ErrNotConnected {
			t.Errorf("期望 ErrNotConnected, 实际: %v", err)
		}
	})
}

func TestResolveFindImmortalRemain(t *testing.T) {
	cases := []struct {
		name        string
		before      int16
		refreshed   int16
		refreshedOK bool
		wantTokens  int16
		wantMore    bool
	}{
		{name: "回查确认递减", before: 4, refreshed: 3, refreshedOK: true, wantTokens: 3, wantMore: true},
		{name: "回查确认归零", before: 1, refreshed: 0, refreshedOK: true, wantTokens: 0, wantMore: true},
		{name: "回查余量未变化", before: 2, refreshed: 2, refreshedOK: true, wantTokens: 2, wantMore: false},
		{name: "回查余量反而增加", before: 2, refreshed: 4, refreshedOK: true, wantTokens: 4, wantMore: false},
		{name: "回查失败", before: 2, refreshed: 0, refreshedOK: false, wantTokens: 2, wantMore: false},
		{name: "回查返回负值", before: 2, refreshed: -1, refreshedOK: true, wantTokens: 2, wantMore: false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gotTokens, gotMore := resolveFindImmortalRemain(tc.before, tc.refreshed, tc.refreshedOK)
			if gotTokens != tc.wantTokens || gotMore != tc.wantMore {
				t.Errorf("resolveFindImmortalRemain() = (%d, %v), want (%d, %v)",
					gotTokens, gotMore, tc.wantTokens, tc.wantMore)
			}
		})
	}
}

func TestFiveBlessingsRoutine_Execute(t *testing.T) {
	session := client.NewRoleSession(client.SessionConfig{
		RoleID:     "role_test_five_blessings",
		RoleName:   "测试五福临门角色",
		ServerAddr: "mock",
	})
	if err := session.Start(context.Background()); err != nil {
		t.Fatalf("启动测试会话失败: %v", err)
	}
	defer session.Close()

	time.Sleep(100 * time.Millisecond)

	routine := NewFiveBlessingsRoutine()
	if routine.Name() != "five_blessings" {
		t.Fatalf("任务名称不匹配: %s", routine.Name())
	}
	if dr, ok := routine.(scheduler.DetailedRoutine); ok {
		if dr.Domain() != "minigame" {
			t.Fatalf("领域不匹配: %s", dr.Domain())
		}
	}

	if err := routine.Execute(context.Background(), session, nil); err != nil {
		t.Fatalf("五福临门免费祈福执行失败: %v", err)
	}
	if got := session.LastActionID(); got != protocol.ActionIDFindImmortalEndBless {
		t.Errorf("期望最后动作为结束祈福领奖 0x%08X, 实际 0x%08X", protocol.ActionIDFindImmortalEndBless, got)
	}

	if err := routine.Execute(context.Background(), nil, nil); err != client.ErrNotConnected {
		t.Errorf("空会话期望 ErrNotConnected, 实际: %v", err)
	}
}

func TestSelectWuFaFreeDraw(t *testing.T) {
	panel := &protocol.WuFaPanel{
		Fields: []protocol.WuFaFieldState{
			{FieldType: 1, LeftFreeOneCount: 2},
			{FieldType: 2, LeftFreeTenCount: 1},
			{FieldType: 3, LeftFreeTenCount: 1},
		},
	}
	fieldType, drawType, ok := selectWuFaFreeDraw(panel)
	if !ok || fieldType != 3 || drawType != protocol.WuFaDrawTen {
		t.Fatalf("应优先选择免费十连且同次数取高 field_type, 实际 field=%d draw=%d ok=%v", fieldType, drawType, ok)
	}

	panel = &protocol.WuFaPanel{
		Fields: []protocol.WuFaFieldState{
			{FieldType: 1, LeftFreeOneCount: 2},
			{FieldType: 2, LeftFreeOneCount: 1},
		},
	}
	fieldType, drawType, ok = selectWuFaFreeDraw(panel)
	if !ok || fieldType != 1 || drawType != protocol.WuFaDrawOne {
		t.Fatalf("无免费十连时应选择免费单抽, 实际 field=%d draw=%d ok=%v", fieldType, drawType, ok)
	}

	panel = &protocol.WuFaPanel{
		Fields: []protocol.WuFaFieldState{{FieldType: 1}},
	}
	if _, _, ok := selectWuFaFreeDraw(panel); ok {
		t.Fatal("无免费次数时不应选择抽卡")
	}
	if _, _, ok := selectWuFaFreeDraw(nil); ok {
		t.Fatal("nil 面板不应选择抽卡")
	}
}

func TestClassifyWuFaDrawResult(t *testing.T) {
	cases := []struct {
		code uint8
		want error
	}{
		{protocol.WuFaResultSuccess, nil},
		{protocol.WuFaResultOverGet, nil},
		{protocol.WuFaResultIsBuy, nil},
		{protocol.WuFaResultIsHave, nil},
		{protocol.WuFaResultFullGrade, nil},
		{protocol.WuFaResultCountLimit, scheduler.ErrAttemptsExhausted},
		{protocol.WuFaResultNotEnoughCount, scheduler.ErrAttemptsExhausted},
		{protocol.WuFaResultWeekNumLimit, scheduler.ErrAttemptsExhausted},
		{protocol.WuFaResultNoEnoughDrawTimes, scheduler.ErrAttemptsExhausted},
		{protocol.WuFaResultItemPackFull, scheduler.ErrBagFull},
		{protocol.WuFaResultNotEnoughIngot, scheduler.ErrConditionNotMet},
		{protocol.WuFaResultNotEnoughResourse, scheduler.ErrConditionNotMet},
		{protocol.WuFaResultNotEnoughResource, scheduler.ErrConditionNotMet},
		{protocol.WuFaResultNotEnoughRecharge, scheduler.ErrConditionNotMet},
		{protocol.WuFaResultFunctionNotOpen, scheduler.ErrConditionNotMet},
	}
	for _, tc := range cases {
		got := classifyWuFaDrawResult(tc.code)
		if tc.want == nil {
			if got != nil {
				t.Errorf("结果码 %d 应返回 nil, 实际 %v", tc.code, got)
			}
			continue
		}
		if got != tc.want {
			t.Errorf("结果码 %d 期望 %v, 实际 %v", tc.code, tc.want, got)
		}
	}

	if err := classifyWuFaDrawResult(protocol.WuFaResultFieldTypeError); err == nil {
		t.Error("秘馆类型错误应返回错误")
	}
	if err := classifyWuFaDrawResult(200); err == nil {
		t.Error("未知结果码应返回错误")
	}
}

// TestFactionLuckyStarRoutine_SkipsWhenRemainTimesZero 验证帮派吉星高照会先查询
// 剩余次数 (Mod 10 Act 44)：次数为 0 时直接结束本轮，不再请求会携带帮派成员列表、
// 在无帮派角色上被静默丢弃的战绩面板 (Act 45)。
func TestFactionLuckyStarRoutine_SkipsWhenRemainTimesZero(t *testing.T) {
	serverConn, clientConn := net.Pipe()
	defer serverConn.Close()
	defer clientConn.Close()

	var mu sync.Mutex
	infoRequested := false

	go func() {
		for {
			pkt, err := protocol.ReadPacket(serverConn)
			if err != nil {
				return
			}
			switch pkt.ActionID {
			case protocol.ActionIDFactionRollCakeRemainTimes:
				_ = protocol.WritePacket(serverConn, protocol.NewPacket(
					protocol.ActionIDFactionRollCakeRemainTimes, []byte{0},
				))
			case protocol.ActionIDFactionRollCakeInfo:
				mu.Lock()
				infoRequested = true
				mu.Unlock()
			}
		}
	}()

	session := client.NewRoleSession(client.SessionConfig{
		RoleID:     "role_test_faction_rollcake",
		ServerAddr: "mock-pipe",
		Dialer: func(ctx context.Context, network, addr string) (net.Conn, error) {
			return clientConn, nil
		},
		Authenticator: func(s *client.RoleSession) error {
			return nil
		},
	})
	if err := session.Start(context.Background()); err != nil {
		t.Fatalf("启动测试会话失败: %v", err)
	}
	defer session.Close()

	deadline := time.Now().Add(2 * time.Second)
	for session.State() != client.StateActive && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if session.State() != client.StateActive {
		t.Fatalf("测试会话未进入激活状态: %s", session.State())
	}

	if err := NewFactionLuckyStarRoutine().Execute(context.Background(), session, nil); err != nil {
		t.Fatalf("帮派吉星高照执行失败: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if infoRequested {
		t.Fatal("剩余次数为 0 时不应再请求战绩面板")
	}
	if session.IsFactionFeatureUnavailable() {
		t.Fatal("剩余次数为 0 不应判定帮派模块不可用")
	}
}

func TestTreasureHouseRoutine_ExecuteSuccessWithMock(t *testing.T) {
	session := client.NewRoleSession(client.SessionConfig{
		RoleID:     "role_test_treasure_house",
		RoleName:   "测试宝库角色",
		ServerAddr: "mock",
	})
	if err := session.Start(context.Background()); err != nil {
		t.Fatalf("启动测试会话失败: %v", err)
	}
	defer session.Close()
	time.Sleep(100 * time.Millisecond)

	routine := NewTreasureHouseRoutine(TreasureHouseConfig{MaxDrawTimes: 10})
	if err := routine.Execute(context.Background(), session, nil); err != nil {
		t.Fatalf("天帝宝库 Routine 执行失败: %v", err)
	}
	// 成功路径: 领免费钥匙 -> 设定自选 -> 设置免费优先 -> 抽奖 -> 结束刷新主面板
	if got := session.LastActionID(); got != protocol.ActionIDTreasureHouseMainPanel {
		t.Errorf("期望最后动作为主面板刷新 0x%08X, 实际 0x%08X",
			protocol.ActionIDTreasureHouseMainPanel, got)
	}
}

func TestTreasureHouseRoutine_SetAwardFailureStopsLottery(t *testing.T) {
	session := client.NewRoleSession(client.SessionConfig{
		RoleID:     "role_test_treasure_house_fail",
		RoleName:   "测试宝库失败角色",
		ServerAddr: "mock",
	})
	if err := session.Start(context.Background()); err != nil {
		t.Fatalf("启动测试会话失败: %v", err)
	}
	defer session.Close()
	time.Sleep(100 * time.Millisecond)

	// 注入一个不在服务端开放候选中的大奖: set_award 回包 FAILED，Routine 必须熔断且不再抽奖。
	cfg := TreasureHouseConfig{
		MaxDrawTimes: 10,
		Selector: func(ctx context.Context, pool *protocol.TreasureHousePool, dict dictionary.Repository) ([]protocol.TreasureAwardSelect, error) {
			return []protocol.TreasureAwardSelect{
				{TierID: 1, ItemID: 999},
				{TierID: 2, ItemID: 25},
				{TierID: 3, ItemID: 42},
			}, nil
		},
	}
	routine := NewTreasureHouseRoutine(cfg)
	if err := routine.Execute(context.Background(), session, nil); err != nil {
		t.Fatalf("天帝宝库 Routine 执行失败: %v", err)
	}
	if got := session.LastActionID(); got != protocol.ActionIDTreasureHouseSetAward {
		t.Errorf("set_award 失败后应立即熔断, 期望最后动作 0x%08X, 实际 0x%08X",
			protocol.ActionIDTreasureHouseSetAward, got)
	}
}

func TestTreasureHouseAwardResolution(t *testing.T) {
	panel := &protocol.TreasureHouseSelectPanel{
		Result: protocol.TreasureHouseResultSuccess,
		Qualities: []protocol.TreasureHouseQualityPanel{
			{Quality: 3, Awards: []protocol.TreasureHouseAwardCandidate{{AwardID: 42, OpenFlag: 1}, {AwardID: 43, OpenFlag: 0}}},
			{Quality: 2, Awards: []protocol.TreasureHouseAwardCandidate{{AwardID: 25, OpenFlag: 0}, {AwardID: 21, OpenFlag: 1}}},
			{Quality: 1, Awards: []protocol.TreasureHouseAwardCandidate{{AwardID: 50, OpenFlag: 0}, {AwardID: 12, OpenFlag: 1}}},
		},
	}
	pool := offeredAwardPool(panel)
	if pool.TotalCount != 3 {
		t.Fatalf("期望仅 3 个开放候选, 实际 %d", pool.TotalCount)
	}
	if !containsAwardID(pool.Tier1Items, 12) || containsAwardID(pool.Tier1Items, 50) {
		t.Errorf("一档开放候选解析异常: %+v", pool.Tier1Items)
	}

	configured := []protocol.TreasureAwardSelect{
		{TierID: 1, ItemID: 50}, // 未开放 -> 回退首个开放候选 12
		{TierID: 2, ItemID: 21}, // 已开放 -> 保留
		{TierID: 3, ItemID: 99}, // 未开放 -> 回退首个开放候选 42
	}
	current := []protocol.TreasureAwardSelect{
		{TierID: 1, ItemID: 12},
	}
	resolved := resolveTreasureAwards(configured, current, pool)
	want := []int32{12, 21, 42}
	for i, id := range want {
		if resolved[i].ItemID != id {
			t.Errorf("第 %d 档期望 %d, 实际 %d (%+v)", i+1, id, resolved[i].ItemID, resolved)
		}
	}
}

// TestScratchCardRoutine_Execute 覆盖刮刮卡免费次数刮卡成功路径 (mock 返回元宝)。
func TestScratchCardRoutine_Execute(t *testing.T) {
	session := client.NewRoleSession(client.SessionConfig{
		RoleID:     "role_test_scratch_card",
		RoleName:   "测试刮刮卡角色",
		ServerAddr: "mock",
	})
	if err := session.Start(context.Background()); err != nil {
		t.Fatalf("启动测试会话失败: %v", err)
	}
	defer session.Close()
	time.Sleep(100 * time.Millisecond)

	r := NewScratchCardRoutine()
	if err := r.Execute(context.Background(), session, nil); err != nil {
		t.Fatalf("刮刮卡任务执行失败: %v", err)
	}
}
