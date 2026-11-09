// Package minigame 提供吉星高照、仙旅奇缘答题、刮刮卡、钓鱼等益智趣味活动。
//
// @author Ateng
// @since 2026-10-11
package minigame

import (
	"bytes"
	"context"
	"errors"
	"net"
	"strings"
	"sync"
	"sync/atomic"
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
		// Mock 面板中秘馆 2 有免费十连：应优先执行十连抽卡，
		// 抽卡后刷新面板发现免费次数归零且无冥想书卷，随即停止。
		if got := session.LastActionID(); got != protocol.ActionIDWuFaFieldInfo {
			t.Errorf("期望最后动作 0x%08X, 实际 0x%08X", protocol.ActionIDWuFaFieldInfo, got)
		}
	})

	t.Run("法力星图免费占星", func(t *testing.T) {
		r := NewStarPictureRoutine()
		if r.Name() != "star_picture" {
			t.Fatalf("任务名称不匹配: %s", r.Name())
		}
		if err := r.Execute(context.Background(), session, nil); err != nil {
			t.Fatalf("法力星图执行失败: %v", err)
		}
		// Mock 提供 2 次免费占星，执行后最后动作必须是占星结算。
		if got := session.LastActionID(); got != protocol.ActionIDStarPictureDivination {
			t.Errorf("期望最后动作 0x%08X, 实际 0x%08X", protocol.ActionIDStarPictureDivination, got)
		}
	})

	t.Run("纪元湮劫每日战斗", func(t *testing.T) {
		r := NewEraYanjieRoutine()
		if r.Name() != "era_yanjie" {
			t.Fatalf("任务名称不匹配: %s", r.Name())
		}
		if err := r.Execute(context.Background(), session, nil); err != nil {
			t.Fatalf("纪元湮劫执行失败: %v", err)
		}
		// Mock 关卡可战且战斗胜利，最后动作必须是 fight。
		if got := session.LastActionID(); got != protocol.ActionIDEraYanjieFight {
			t.Errorf("期望最后动作 0x%08X, 实际 0x%08X", protocol.ActionIDEraYanjieFight, got)
		}
	})

	t.Run("仙幽猎境每日进入", func(t *testing.T) {
		r := NewMysticHuntRoutine()
		if r.Name() != "mystic_hunt" {
			t.Fatalf("任务名称不匹配: %s", r.Name())
		}
		if err := r.Execute(context.Background(), session, nil); err != nil {
			t.Fatalf("仙幽猎境执行失败: %v", err)
		}
		if got := session.LastActionID(); got != protocol.ActionIDMysticHuntEnter {
			t.Errorf("期望最后动作 0x%08X, 实际 0x%08X", protocol.ActionIDMysticHuntEnter, got)
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

// TestWuFaRoutine_FunctionClosedSkips 验证功能列表为空 (悟法未开放) 时，
// 不再发送悟法领域面板等业务请求。
func TestWuFaRoutine_FunctionClosedSkips(t *testing.T) {
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
				// 功能列表为空：悟法领域未开放。
				w := protocol.NewWriter()
				w.WriteUint16(0)
				_ = protocol.WritePacket(c2, protocol.NewPacket(
					protocol.ActionIDPlayerGetFunctions, w.Bytes(),
				))
			}
		}
	}()

	session := client.NewRoleSession(client.SessionConfig{
		RoleID:   "role_test_wufa_closed",
		RoleName: "测试悟法未开放角色",
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

	if err := NewWuFaRoutine().Execute(context.Background(), session, nil); err != nil {
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

// TestFiveBlessingsRoutine_OpenFlagNoStillBlesses 验证线上回归：
// 不再调用 Act 14（其返回 NO 会污染动作链），直接以 Act 10 面板 bless_number
// 为准执行祈福。
func TestFiveBlessingsRoutine_OpenFlagNoStillBlesses(t *testing.T) {
	var startBlessCalls, openFlagCalls int32
	session := newMinigameMockSession(t, func(conn net.Conn, pkt *protocol.Packet) {
		switch pkt.ActionID {
		case protocol.ActionIDFindImmortalIsFiveBlessingsOpen:
			atomic.AddInt32(&openFlagCalls, 1)
			_ = protocol.WritePacket(conn, protocol.NewPacket(
				pkt.ActionID, []byte{protocol.FindImmortalResultNo}))
		case protocol.ActionIDFindImmortalOpenFiveBlessings:
			w := protocol.NewWriter()
			w.WriteUint16(1) // bless_number
			w.WriteInt8(1)   // stage
			w.WriteUint16(0) // award count
			w.WriteInt8(0)   // total_count
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, w.Bytes()))
		case protocol.ActionIDFindImmortalStartBless:
			atomic.AddInt32(&startBlessCalls, 1)
			w := protocol.NewWriter()
			w.WriteUint8(protocol.FindImmortalResultSuccess)
			w.WriteInt8(2)
			w.WriteUint16(0)
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, w.Bytes()))
		case protocol.ActionIDFindImmortalEndBless:
			w := protocol.NewWriter()
			w.WriteUint8(protocol.FindImmortalResultSuccess)
			w.WriteUint16(0)
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, w.Bytes()))
		}
	})

	if err := NewFiveBlessingsRoutine().Execute(context.Background(), session, nil); err != nil {
		t.Fatalf("五福临门任务执行失败: %v", err)
	}
	if got := atomic.LoadInt32(&openFlagCalls); got != 0 {
		t.Fatalf("不应再调用 Act14 开启标志, 实际 %d 次", got)
	}
	if got := atomic.LoadInt32(&startBlessCalls); got != 1 {
		t.Fatalf("面板有免费次数时应执行 1 次 start_bless, 实际 %d 次", got)
	}
}

// TestFiveBlessingsRoutine_NoBlessSkips 验证面板 bless_number=0 时不发送祈福请求。
func TestFiveBlessingsRoutine_NoBlessSkips(t *testing.T) {
	var startBlessCalls int32
	session := newMinigameMockSession(t, func(conn net.Conn, pkt *protocol.Packet) {
		switch pkt.ActionID {
		case protocol.ActionIDFindImmortalIsFiveBlessingsOpen:
			_ = protocol.WritePacket(conn, protocol.NewPacket(
				pkt.ActionID, []byte{protocol.FindImmortalResultNo}))
		case protocol.ActionIDFindImmortalOpenFiveBlessings:
			w := protocol.NewWriter()
			w.WriteUint16(0) // bless_number
			w.WriteInt8(0)
			w.WriteUint16(0)
			w.WriteInt8(0)
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, w.Bytes()))
		case protocol.ActionIDFindImmortalStartBless:
			atomic.AddInt32(&startBlessCalls, 1)
		}
	})

	if err := NewFiveBlessingsRoutine().Execute(context.Background(), session, nil); err != nil {
		t.Fatalf("五福临门任务执行失败: %v", err)
	}
	if got := atomic.LoadInt32(&startBlessCalls); got != 0 {
		t.Fatalf("面板无免费次数时不应发送 start_bless, 实际 %d 次", got)
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
	var cdErr *scheduler.CooldownError
	if err := classifyWuFaDrawResult(protocol.WuFaResultFunctionNotOpen); !errors.As(err, &cdErr) {
		t.Errorf("功能未开启应返回 CooldownError, 实际 %v", err)
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

// TestAskFairyRoutine_Execute 覆盖每日问灵成功并优先选择元宝奖励的路径。
func TestAskFairyRoutine_Execute(t *testing.T) {
	session := client.NewRoleSession(client.SessionConfig{
		RoleID:     "role_test_ask_fairy",
		RoleName:   "测试问灵角色",
		ServerAddr: "mock",
	})
	if err := session.Start(context.Background()); err != nil {
		t.Fatalf("启动测试会话失败: %v", err)
	}
	defer session.Close()
	time.Sleep(100 * time.Millisecond)

	r := NewAskFairyRoutine()
	if err := r.Execute(context.Background(), session, nil); err != nil {
		t.Fatalf("问灵任务执行失败: %v", err)
	}
}

// TestFishingRoutine_Execute 覆盖免费鱼钩自动钓鱼成功路径。
func TestFishingRoutine_Execute(t *testing.T) {
	session := client.NewRoleSession(client.SessionConfig{
		RoleID:     "role_test_fishing",
		RoleName:   "测试钓鱼角色",
		ServerAddr: "mock",
	})
	if err := session.Start(context.Background()); err != nil {
		t.Fatalf("启动测试会话失败: %v", err)
	}
	defer session.Close()
	time.Sleep(100 * time.Millisecond)

	r := NewFishingRoutine()
	if err := r.Execute(context.Background(), session, nil); err != nil {
		t.Fatalf("钓鱼任务执行失败: %v", err)
	}
}

// TestIceCaveRoutine_Execute 覆盖冰窟免费锤子敲击与兑换路径。
func TestIceCaveRoutine_Execute(t *testing.T) {
	session := client.NewRoleSession(client.SessionConfig{
		RoleID:     "role_test_ice_cave",
		RoleName:   "测试冰窟角色",
		ServerAddr: "mock",
	})
	if err := session.Start(context.Background()); err != nil {
		t.Fatalf("启动测试会话失败: %v", err)
	}
	defer session.Close()
	time.Sleep(100 * time.Millisecond)

	r := NewIceCaveRoutine()
	if err := r.Execute(context.Background(), session, nil); err != nil {
		t.Fatalf("冰窟任务执行失败: %v", err)
	}
}

// TestPartnerGuessRoutine_Execute 覆盖伙伴猜猜看开局、刮图与作答路径。
func TestPartnerGuessRoutine_Execute(t *testing.T) {
	session := client.NewRoleSession(client.SessionConfig{
		RoleID: "role_test_partner_guess", RoleName: "测试猜伙伴角色", ServerAddr: "mock",
	})
	if err := session.Start(context.Background()); err != nil {
		t.Fatalf("启动测试会话失败: %v", err)
	}
	defer session.Close()
	time.Sleep(100 * time.Millisecond)
	if err := NewPartnerGuessRoutine().Execute(context.Background(), session, nil); err != nil {
		t.Fatalf("伙伴猜猜看任务执行失败: %v", err)
	}
}

// TestImperialExamRoutine_Execute 覆盖金榜题名活动阶段检测路径。
func TestImperialExamRoutine_Execute(t *testing.T) {
	session := client.NewRoleSession(client.SessionConfig{
		RoleID: "role_test_exam", RoleName: "测试金榜角色", ServerAddr: "mock",
	})
	if err := session.Start(context.Background()); err != nil {
		t.Fatalf("启动测试会话失败: %v", err)
	}
	defer session.Close()
	time.Sleep(100 * time.Millisecond)
	if err := NewImperialExamRoutine(nil).Execute(context.Background(), session, nil); err != nil {
		t.Fatalf("金榜题名检测任务执行失败: %v", err)
	}
}

// TestGoldenTouchStoneRoutine_Execute 覆盖点金石状态检测与点化路径。
func TestGoldenTouchStoneRoutine_Execute(t *testing.T) {
	session := client.NewRoleSession(client.SessionConfig{
		RoleID: "role_test_golden_stone", RoleName: "测试点金石角色", ServerAddr: "mock",
	})
	if err := session.Start(context.Background()); err != nil {
		t.Fatalf("启动测试会话失败: %v", err)
	}
	defer session.Close()
	time.Sleep(100 * time.Millisecond)
	if err := NewGoldenTouchStoneRoutine().Execute(context.Background(), session, nil); err != nil {
		t.Fatalf("点金石任务执行失败: %v", err)
	}
}

// TestForbiddenBookRoutine_Execute 覆盖禁地之书免费探索与铜钱兑换路径。
func TestForbiddenBookRoutine_Execute(t *testing.T) {
	session := client.NewRoleSession(client.SessionConfig{
		RoleID: "role_test_forbidden_book", RoleName: "测试禁地之书角色", ServerAddr: "mock",
	})
	if err := session.Start(context.Background()); err != nil {
		t.Fatalf("启动测试会话失败: %v", err)
	}
	defer session.Close()
	time.Sleep(100 * time.Millisecond)
	if err := NewForbiddenBookRoutine().Execute(context.Background(), session, nil); err != nil {
		t.Fatalf("禁地之书任务执行失败: %v", err)
	}
}

// TestSelectTreasureAwardsByRarity 验证方案 A:
// 道具稀有度 > 自选包/材料/货币/宝石包/令牌 价值序 > 数量 > 官方 sort > 奖励 id。
func TestSelectTreasureAwardsByRarity(t *testing.T) {
	pool := &protocol.TreasureHousePool{
		Tier1Items: []int32{4, 6, 10, 12, 15, 8},
		Tier2Items: []int32{23, 51, 27, 21, 20},
		Tier3Items: []int32{33, 43, 31, 40, 42, 34},
	}
	items := map[int32]*dictionary.Item{
		6651: {ID: 6651, Name: "伙伴碎片自选包", TypeID: 42100, Quality: 5},
		7697: {ID: 7697, Name: "修为资源自选包", TypeID: 42100, Quality: 5},
		5183: {ID: 5183, Name: "命宫碎片", TypeID: 31000, Quality: 5},
		4954: {ID: 4954, Name: "血魂石", TypeID: 31000, Quality: 5},
		5117: {ID: 5117, Name: "兽魂碎片", TypeID: 31000, Quality: 5},
		6910: {ID: 6910, Name: "八卦阵碎片", TypeID: 31000, Quality: 5},
		6909: {ID: 6909, Name: "大法力石", TypeID: 31000, Quality: 5},
		9212: {ID: 9212, Name: "1阶宝石随机包", TypeID: 10011, Quality: 5},
		5822: {ID: 5822, Name: "炼器宝箱", TypeID: 42100, Quality: 5},
		347:  {ID: 347, Name: "黄玉牌", TypeID: 10006, Quality: 5},
		2343: {ID: 2343, Name: "橙装碎片", TypeID: 31000, Quality: 5},
		2471: {ID: 2471, Name: "高级灵石", TypeID: 31000, Quality: 4},
		4975: {ID: 4975, Name: "黄金圣水", TypeID: 31000, Quality: 5},
		2395: {ID: 2395, Name: "内丹", TypeID: 31000, Quality: 4},
		2403: {ID: 2403, Name: "道缘", TypeID: 31000, Quality: 3},
		1740: {ID: 1740, Name: "境界点", TypeID: 28000, Quality: 5},
		5397: {ID: 5397, Name: "普通进阶丹", TypeID: 31000, Quality: 4},
	}
	lookup := func(id int32) (*dictionary.Item, error) {
		if it, ok := items[id]; ok {
			return it, nil
		}
		return nil, errors.New("not found")
	}

	got := SelectTreasureAwardsByRarity(pool, lookup, nil, nil)
	want := []int32{6, 27, 43}
	if len(got) != len(want) {
		t.Fatalf("期望 %d 档, 实际 %d", len(want), len(got))
	}
	for i, id := range want {
		if got[i].TierID != int32(i+1) || got[i].ItemID != id {
			t.Errorf("第 %d 档期望 award=%d, 实际 %+v", i+1, id, got[i])
		}
	}
}

// TestSelectTreasureAwardsByRarity_TieBreakAndFallback 验证同级候选的稳定排序与官方表缺失兜底。
func TestSelectTreasureAwardsByRarity_TieBreakAndFallback(t *testing.T) {
	// 8 与 46 同为品质5/类型209对应材料，数量 3 vs 3，按 sort 升序: 8(sort=1) 优先。
	pool := &protocol.TreasureHousePool{Tier1Items: []int32{46, 8, 999}}
	lookup := func(id int32) (*dictionary.Item, error) {
		return &dictionary.Item{ID: int(id), TypeID: 31000, Quality: 5}, nil
	}
	got := SelectTreasureAwardsByRarity(pool, lookup, nil, nil)
	if len(got) != 1 || got[0].ItemID != 8 {
		t.Fatalf("期望按官方 sort 选中 award=8, 实际 %+v", got)
	}

	// 全部候选都不在官方表时，退回服务端首个开放候选。
	fallback := SelectTreasureAwardsByRarity(&protocol.TreasureHousePool{Tier2Items: []int32{999, 998}}, lookup, nil, nil)
	if len(fallback) != 1 || fallback[0].ItemID != 999 {
		t.Fatalf("官方表缺失时应退回首个候选, 实际 %+v", fallback)
	}
}

// TestSelectTreasureAwardsByRarity_FiltersClosedFunctions 验证所属功能未开放的候选会被跳过，
// 以及整档候选都被过滤时按稀有度兜底。
func TestSelectTreasureAwardsByRarity_FiltersClosedFunctions(t *testing.T) {
	pool := &protocol.TreasureHousePool{
		// 51 属山海幻兽(256, 未开放)，21 为通用黄玉牌；即使 51 道具更稀有也应选 21。
		Tier2Items: []int32{51, 21},
		// 整档都指向未开放功能，应兜底选稀有度最高者。
		Tier3Items: []int32{40, 33},
	}
	items := map[int32]*dictionary.Item{
		9212: {ID: 9212, Name: "1阶宝石随机包", TypeID: 10011, Quality: 7},
		347:  {ID: 347, Name: "黄玉牌", TypeID: 10006, Quality: 5},
		2403: {ID: 2403, Name: "道缘", TypeID: 31000, Quality: 3},
		2471: {ID: 2471, Name: "高级灵石", TypeID: 31000, Quality: 5},
	}
	lookup := func(id int32) (*dictionary.Item, error) {
		if it, ok := items[id]; ok {
			return it, nil
		}
		return nil, errors.New("not found")
	}
	open := map[int32]bool{ // 仅通用与封灵开放
		protocol.FunctionIDTravelEvent: true,
		48:                             true,
	}
	isOpen := func(id int32) bool { return open[id] }

	got := SelectTreasureAwardsByRarity(pool, lookup, isOpen, nil)
	// 第1档无候选不产出；第2档跳过未开放的 51 选 21；第3档全部未开放时兜底选品质更高的 33。
	want := map[int32]int32{2: 21, 3: 33}
	if len(got) != len(want) {
		t.Fatalf("期望 %d 档结果, 实际 %+v", len(want), got)
	}
	for _, sel := range got {
		if wantID, ok := want[sel.TierID]; !ok || sel.ItemID != wantID {
			t.Errorf("第 %d 档期望 award=%d, 实际 %+v", sel.TierID, wantID, sel)
		}
	}
}

// TestSelectTreasureAwardsByRarity_PreferenceWins 验证优先清单命中时覆盖稀有度选择，
// 未命中时回退到常规稀有度/开放过滤逻辑。
func TestSelectTreasureAwardsByRarity_PreferenceWins(t *testing.T) {
	pool := &protocol.TreasureHousePool{
		Tier1Items: []int32{4},
		Tier2Items: []int32{51, 21},
		Tier3Items: []int32{33},
	}
	items := map[int32]*dictionary.Item{
		6651: {ID: 6651, Name: "伙伴碎片自选包", TypeID: 42100, Quality: 5},
		9212: {ID: 9212, Name: "1阶宝石随机包", TypeID: 10011, Quality: 5},
		347:  {ID: 347, Name: "黄玉牌", TypeID: 10006, Quality: 5},
		2471: {ID: 2471, Name: "高级灵石", TypeID: 31000, Quality: 5},
		2397: {ID: 2397, Name: "鸿蒙紫气", TypeID: 31000, Quality: 5},
	}
	lookup := func(id int32) (*dictionary.Item, error) {
		if it, ok := items[id]; ok {
			return it, nil
		}
		return nil, errors.New("not found")
	}
	isOpen := func(id int32) bool { return true }

	// 26(鸿蒙紫气) 不在当前候选，命中 51(幻兽宝石包)，优先于通用黄玉牌。
	got := SelectTreasureAwardsByRarity(pool, lookup, isOpen, []int32{26, 51})
	if got[1].TierID != 2 || got[1].ItemID != 51 {
		t.Fatalf("期望第二档命中优先清单 award=51, 实际 %+v", got[1])
	}

	// 优先清单全部不在候选时回退稀有度: 第二档选 51(类型10011) 而非 21(类型10006)。
	got = SelectTreasureAwardsByRarity(pool, lookup, isOpen, []int32{26})
	if got[1].ItemID != 51 {
		t.Fatalf("优先清单未命中时应回退稀有度选择, 实际 %+v", got[1])
	}
}

// TestEvaluateAwardNeed_Dynamic 验证账号养成快照驱动的需求分计算。
func TestEvaluateAwardNeed_Dynamic(t *testing.T) {
	snap := &NeedSnapshot{
		Items: map[int32]int32{
			itemHongMengZiQi:      17,
			itemDaFaLiShi:         6,
			itemYongHengLingBao:   2,
			itemXueHunShi:         70,
			itemHuangJinShengShui: 1441,
			itemLingZhu:           4977,
			itemJingJieDian:       41304,
		},
		MagicArray: &protocol.MagicArrayPanel{Level: 16, Exp: 9340, MaxExp: 41000},
		Bloodline: &protocol.BloodlinePanel{Bloodlines: []protocol.Bloodline{
			{ID: 1, Level: 80}, {ID: 2, Level: 80}, {ID: 3, Level: 80},
			{ID: 4, Level: 60}, {ID: 5, Level: 31}, {ID: 6, Level: 1},
		}},
		Library: &protocol.LibraryPanel{Jobs: []protocol.LibraryJob{
			{JobID: 1, Level: 270}, {JobID: 2, Level: 270}, {JobID: 3, Level: 270},
		}},
		Tactics:  &protocol.TacticsPanel{Level: 60, Exp: 308500, NextLevelExp: 400000},
		Eudemons: &protocol.EudemonsPanel{StoneMaxLevel: 0, TotalStoneStep: 0},
		ElixirSoul: &protocol.ElixirSoulPanel{Slots: []protocol.ElixirSoulSlot{
			{ID: 1, Level: 2}, {ID: 2, Level: 3}, {ID: 3, Level: 2},
		}},
	}

	cases := []struct {
		awardID int32
		min     float64
		max     float64
	}{
		{26, 90, 100}, // 鸿蒙紫气最缺
		{23, 70, 90},  // 法阵缺口大
		{9, 40, 80},   // 永恒灵宝库存 2
		{34, 50, 60},  // 丹魂魂位全低
		{51, 60, 65},  // 幻兽宝石阶数 0
		{12, 0, 0.01}, // 血魂石 70 已覆盖缺口
		{43, 0, 0.01}, // 黄金圣水 1441 已覆盖兵法缺口
		{36, 0, 0.01}, // 藏经阁满级
		{42, 0, 5},    // 境界点充足
		{32, 0, 0.01}, // 灵珠充足
	}
	for _, c := range cases {
		got := EvaluateAwardNeed(c.awardID, snap)
		if got.Score < c.min || got.Score > c.max {
			t.Errorf("award %d score=%.2f, want [%.2f, %.2f] (%s)", c.awardID, got.Score, c.min, c.max, got.Reason)
		}
	}
}

// TestSelectTreasureAwardsByNeed 验证需求分最高的候选优先，同分回退稀有度。
func TestSelectTreasureAwardsByNeed(t *testing.T) {
	pool := &protocol.TreasureHousePool{
		Tier1Items: []int32{9, 12, 4},
		Tier2Items: []int32{23, 18},
		Tier3Items: []int32{34, 42},
	}
	items := map[int32]*dictionary.Item{
		2010: {ID: 2010, Name: "永恒灵宝", TypeID: 30100, Quality: 7},
		4954: {ID: 4954, Name: "血魂石", TypeID: 31000, Quality: 5},
		6651: {ID: 6651, Name: "伙伴碎片自选包", TypeID: 42100, Quality: 5},
		6909: {ID: 6909, Name: "大法力石", TypeID: 31000, Quality: 5},
		2407: {ID: 2407, Name: "超级火龙珠", TypeID: 31000, Quality: 7},
		5397: {ID: 5397, Name: "普通进阶丹", TypeID: 31000, Quality: 4},
		1740: {ID: 1740, Name: "境界点", TypeID: 28000, Quality: 5},
	}
	lookup := func(id int32) (*dictionary.Item, error) {
		if it, ok := items[id]; ok {
			return it, nil
		}
		return nil, errors.New("not found")
	}
	needs := map[int32]AwardNeed{
		9:  {Score: 40, Reason: "永恒灵宝"},
		12: {Score: 10, Reason: "血魂石"},
		4:  {Score: 30, Reason: "伙伴碎片"},
		23: {Score: 80, Reason: "法阵"},
		18: {Score: 70, Reason: "龙珠"},
		34: {Score: 50, Reason: "丹魂"},
		42: {Score: 2, Reason: "境界点"},
	}
	got := SelectTreasureAwardsByNeed(pool, lookup, func(int32) bool { return true }, needs)
	want := []int32{9, 23, 34}
	if len(got) != len(want) {
		t.Fatalf("expected %d tiers, got %+v", len(want), got)
	}
	for i, id := range want {
		if got[i].TierID != int32(i+1) || got[i].ItemID != id {
			t.Errorf("tier %d expected award %d, got %+v", i+1, id, got[i])
		}
	}
}

func TestHasWuFaItem(t *testing.T) {
	panel := &protocol.WuFaPanel{Items: []protocol.WuFaItemState{
		{ItemID: 8359, ItemNum: 0},
		{ItemID: 8641, ItemNum: 8},
	}}
	if hasWuFaItem(panel, 8359) {
		t.Fatal("8359 数量为 0，应判定为无余量")
	}
	if !hasWuFaItem(panel, 8641) {
		t.Fatal("8641 数量为 8，应判定有余量")
	}
	if hasWuFaItem(panel, 8700) {
		t.Fatal("不存在的道具应判定为无余量")
	}
	if hasWuFaItem(nil, 8641) {
		t.Fatal("空面板应判定为无余量")
	}
}

func TestSelectWuFaItemDraw(t *testing.T) {
	panel := &protocol.WuFaPanel{Items: []protocol.WuFaItemState{
		{ItemID: 8358, ItemNum: 5},
		{ItemID: 8700, ItemNum: 0},
	}}
	if field, drawType, ok := selectWuFaItemDraw(panel); !ok || field != 1 || drawType != protocol.WuFaDrawOne {
		t.Fatalf("expected field 1 via bamboo, got field=%d type=%d ok=%v", field, drawType, ok)
	}
	panel.Items = []protocol.WuFaItemState{{ItemID: 8700, ItemNum: 2}}
	if field, _, ok := selectWuFaItemDraw(panel); !ok || field != 7 {
		t.Fatalf("expected field 7 via xuan, got field=%d ok=%v", field, ok)
	}
	panel.Items = []protocol.WuFaItemState{
		{ItemID: 8641, ItemNum: 1}, {ItemID: 8358, ItemNum: 5},
	}
	if field, _, ok := selectWuFaItemDraw(panel); !ok || field != 3 {
		t.Fatalf("expected book priority field 3, got field=%d ok=%v", field, ok)
	}
	panel.Items = nil
	if _, _, ok := selectWuFaItemDraw(panel); ok {
		t.Fatal("expected no item draw")
	}
}
