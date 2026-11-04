package dungeon

import (
	"context"
	"fmt"
	"reflect"
	"testing"
	"time"

	"sxd-pie-ng/internal/client"
	"sxd-pie-ng/internal/protocol"
	"sxd-pie-ng/internal/scheduler"
)

func TestTrainingRoomRoutine_Identity(t *testing.T) {
	rt := NewTrainingRoomRoutine(DefaultTrainingRoomConfig())
	if rt.Name() != "training_room" {
		t.Fatalf("期望任务名称 training_room, 实际 %s", rt.Name())
	}
	if rt.Interval() != 60*time.Minute {
		t.Errorf("期望巡检间隔 60m, 实际 %v", rt.Interval())
	}
	if rt.Priority() != scheduler.PriorityLow {
		t.Errorf("期望低优先级, 实际 %v", rt.Priority())
	}
	detailed, ok := rt.(scheduler.DetailedRoutine)
	if !ok {
		t.Fatal("任务应实现 DetailedRoutine")
	}
	if detailed.Domain() != "dungeon" {
		t.Errorf("期望领域 dungeon, 实际 %s", detailed.Domain())
	}
	if detailed.ScheduleType() != scheduler.ScheduleLoop {
		t.Errorf("期望 ScheduleLoop, 实际 %v", detailed.ScheduleType())
	}
}

func TestTrainingRoomRoutine_NilSession(t *testing.T) {
	err := NewTrainingRoomRoutine(DefaultTrainingRoomConfig()).Execute(context.Background(), nil, nil)
	if err != client.ErrNotConnected {
		t.Fatalf("期望 ErrNotConnected, 实际 %v", err)
	}
}

func TestTrainingRoomConfig_Normalize(t *testing.T) {
	cfg := normalizeTrainingRoomConfig(TrainingRoomConfig{AllowRobSeat: true, MaxRobAttempts: 0})
	if cfg.MaxRobAttempts != 1 {
		t.Errorf("争夺次数默认值应为 1, 实际 %d", cfg.MaxRobAttempts)
	}
}

// TestRunStPracticeRoom_MovesToBestEmptyRoom 验证从普通练功场迁移到收益最高的空闲星级房间。
func TestRunStPracticeRoom_MovesToBestEmptyRoom(t *testing.T) {
	api := newFakeStPracticeRoomAPI()
	api.lists = []*protocol.StPracticeRoomList{{
		Rooms: []protocol.StPracticeRoomEntry{
			{Quality: 1, Count: 0},
			{Quality: 5, Count: 12}, // 满员, 不应发起 get_room_info
			{Quality: 2, Count: 0},
			{Quality: 3, Count: 4},
			{Quality: 4, Count: 8},
			{Quality: 100, Count: 1},
		},
		InPracticeFlag: protocol.StPracticeRoomYes,
		Quality:        100,
	}}
	api.infos[4] = &protocol.StPracticeRoomInfo{
		Seats: []protocol.StPracticeRoomSeat{
			{SeatSeq: 0}, {SeatSeq: 1}, {SeatSeq: 2}, {SeatSeq: 3},
			{SeatSeq: 4}, {SeatSeq: 5}, {SeatSeq: 6}, {SeatSeq: 7},
		},
		RoomQuality: 4,
	}
	api.claimResults = []*protocol.StPracticeRoomExpResult{{Result: protocol.StPracticeRoomSuccess, Exp: 89562}}
	api.leaveResults = []uint8{protocol.StPracticeRoomSuccess}
	api.sitResults[seatKey{4, 8}] = protocol.StPracticeRoomSuccess

	if err := runStPracticeRoomMaintenance(context.Background(), api, DefaultTrainingRoomConfig()); err != nil {
		t.Fatalf("练功房维护失败: %v", err)
	}

	if api.hasCall("info:5") {
		t.Error("满员房间不应再发起 get_room_info")
	}
	if !api.hasCall("sit:4:8") {
		t.Errorf("应坐上 4 星 8 号空闲席位, 调用序列: %v", api.calls)
	}
	if api.hasCall("sit:100:0") {
		t.Error("存在高星空闲席位时不应回落普通练功场")
	}
	if api.indexOf("leave") > api.indexOf("claim") {
		t.Errorf("换房顺序应为先离开席位再领取经验, 调用序列: %v", api.calls)
	}
	if api.indexOf("leave") > api.indexOf("sit:4:8") {
		t.Errorf("换房顺序应为先离开席位再入座, 调用序列: %v", api.calls)
	}
}

// TestRunStPracticeRoom_StaysWhenAlreadyBest 验证当前房间已是最佳时只领取经验, 不反复换座。
func TestRunStPracticeRoom_StaysWhenAlreadyBest(t *testing.T) {
	api := newFakeStPracticeRoomAPI()
	api.lists = []*protocol.StPracticeRoomList{{
		Rooms: []protocol.StPracticeRoomEntry{
			{Quality: 5, Count: 12},
			{Quality: 4, Count: 8},
			{Quality: 100, Count: 1},
		},
		InPracticeFlag: protocol.StPracticeRoomYes,
		Quality:        4,
	}}
	api.infos[4] = &protocol.StPracticeRoomInfo{
		Seats:       []protocol.StPracticeRoomSeat{{SeatSeq: 0}, {SeatSeq: 1}},
		RoomQuality: 4,
	}
	api.claimResults = []*protocol.StPracticeRoomExpResult{{Result: protocol.StPracticeRoomSuccess, Exp: 1200}}

	if err := runStPracticeRoomMaintenance(context.Background(), api, DefaultTrainingRoomConfig()); err != nil {
		t.Fatalf("练功房维护失败: %v", err)
	}
	if api.hasCall("leave") || api.hasCall("sit:4:2") || api.hasCall("sit:100:0") {
		t.Errorf("已处于最佳席位时不应换座, 调用序列: %v", api.calls)
	}
	if api.countCall("claim") != 1 {
		t.Errorf("保持挂机时应领取一次经验, 实际 %d 次", api.countCall("claim"))
	}
}

// TestRunStPracticeRoom_FallsBackToFreeRoom 验证所有星级房间满员时回落普通练功场。
func TestRunStPracticeRoom_FallsBackToFreeRoom(t *testing.T) {
	api := newFakeStPracticeRoomAPI()
	api.lists = []*protocol.StPracticeRoomList{{
		Rooms: []protocol.StPracticeRoomEntry{
			{Quality: 1, Count: 6},
			{Quality: 2, Count: 7},
			{Quality: 3, Count: 8},
			{Quality: 4, Count: 10},
			{Quality: 5, Count: 12},
			{Quality: 100, Count: 1},
		},
		InPracticeFlag: protocol.StPracticeRoomNo,
		Quality:        0,
	}}
	api.claimResults = []*protocol.StPracticeRoomExpResult{{Result: protocol.StPracticeRoomNoExpToGet}}
	api.sitResults[seatKey{100, 0}] = protocol.StPracticeRoomSuccess

	if err := runStPracticeRoomMaintenance(context.Background(), api, DefaultTrainingRoomConfig()); err != nil {
		t.Fatalf("练功房维护失败: %v", err)
	}
	if !api.hasCall("sit:100:0") {
		t.Errorf("应回落普通练功场, 调用序列: %v", api.calls)
	}
}

func TestRunStPracticeRoom_FunctionNotOpen(t *testing.T) {
	api := newFakeStPracticeRoomAPI()
	api.enterResult = protocol.StPracticeRoomFunctionNoOpen

	err := runStPracticeRoomMaintenance(context.Background(), api, DefaultTrainingRoomConfig())
	if err != scheduler.ErrConditionNotMet {
		t.Fatalf("功能未开放应返回 ErrConditionNotMet, 实际 %v", err)
	}
	if api.hasCall("list") {
		t.Error("功能未开放时不应继续查询房间列表")
	}
}

// TestRunStPracticeRoom_SeatRaceRetriesNext 验证席位在查询与入座之间被抢占后继续尝试下一个。
func TestRunStPracticeRoom_SeatRaceRetriesNext(t *testing.T) {
	api := newFakeStPracticeRoomAPI()
	api.lists = []*protocol.StPracticeRoomList{{
		Rooms: []protocol.StPracticeRoomEntry{
			{Quality: 5, Count: 0},
			{Quality: 100, Count: 1},
		},
		InPracticeFlag: protocol.StPracticeRoomYes,
		Quality:        100,
	}}
	api.infos[5] = &protocol.StPracticeRoomInfo{RoomQuality: 5}
	api.claimResults = []*protocol.StPracticeRoomExpResult{{Result: protocol.StPracticeRoomSuccess}}
	api.leaveResults = []uint8{protocol.StPracticeRoomSuccess}
	api.seatStatus[seatKey{5, 0}] = protocol.StPracticeRoomSomebody
	api.seatStatus[seatKey{5, 1}] = protocol.StPracticeRoomNobody
	api.sitResults[seatKey{5, 1}] = protocol.StPracticeRoomSuccess

	if err := runStPracticeRoomMaintenance(context.Background(), api, DefaultTrainingRoomConfig()); err != nil {
		t.Fatalf("练功房维护失败: %v", err)
	}
	if !api.hasCall("seat:5:0") || !api.hasCall("seat:5:1") {
		t.Errorf("应依次检查空闲席位, 调用序列: %v", api.calls)
	}
	if !api.hasCall("sit:5:1") {
		t.Errorf("应改坐第 2 个空闲席位, 调用序列: %v", api.calls)
	}
}

// TestRunStPracticeRoom_LeaveExpNotGottenRetries 验证离开席位被经验阻塞时先领取再重试离开。
func TestRunStPracticeRoom_LeaveExpNotGottenRetries(t *testing.T) {
	api := newFakeStPracticeRoomAPI()
	api.lists = []*protocol.StPracticeRoomList{{
		Rooms: []protocol.StPracticeRoomEntry{
			{Quality: 5, Count: 0},
			{Quality: 3, Count: 1},
			{Quality: 100, Count: 1},
		},
		InPracticeFlag: protocol.StPracticeRoomYes,
		Quality:        3,
	}}
	api.infos[5] = &protocol.StPracticeRoomInfo{RoomQuality: 5}
	api.leaveResults = []uint8{protocol.StPracticeRoomExpNotGotten, protocol.StPracticeRoomSuccess}
	api.claimResults = []*protocol.StPracticeRoomExpResult{
		{Result: protocol.StPracticeRoomSuccess, Exp: 100},
		{Result: protocol.StPracticeRoomNoExpToGet},
	}
	api.sitResults[seatKey{5, 0}] = protocol.StPracticeRoomSuccess

	if err := runStPracticeRoomMaintenance(context.Background(), api, DefaultTrainingRoomConfig()); err != nil {
		t.Fatalf("练功房维护失败: %v", err)
	}
	if api.countCall("leave") != 2 {
		t.Errorf("期望离开席位重试一次, 实际 %d 次", api.countCall("leave"))
	}
	if api.countCall("claim") < 2 {
		t.Errorf("期望离开被阻塞时补领经验, 实际领取 %d 次", api.countCall("claim"))
	}
	if !api.hasCall("sit:5:0") {
		t.Errorf("补领经验后应完成入座, 调用序列: %v", api.calls)
	}
}

// TestRunStPracticeRoom_RobSeat 验证允许争夺时会选择战力最低的对手并确认席位归属。
func TestRunStPracticeRoom_RobSeat(t *testing.T) {
	api := newFakeStPracticeRoomAPI()
	api.lists = []*protocol.StPracticeRoomList{
		{
			Rooms: []protocol.StPracticeRoomEntry{
				{Quality: 1, Count: 6},
				{Quality: 2, Count: 7},
				{Quality: 3, Count: 8},
				{Quality: 4, Count: 10},
				{Quality: 5, Count: 12},
				{Quality: 100, Count: 1},
			},
			InPracticeFlag: protocol.StPracticeRoomYes,
			Quality:        100,
		},
		{
			Rooms:          []protocol.StPracticeRoomEntry{{Quality: 5, Count: 12}},
			InPracticeFlag: protocol.StPracticeRoomYes,
			Quality:        5,
		},
	}
	api.infos[5] = &protocol.StPracticeRoomInfo{
		Seats: []protocol.StPracticeRoomSeat{
			{SeatSeq: 0, AttackPower: 500000, Level: 100},
			{SeatSeq: 1, AttackPower: 100000, Level: 80},
		},
		RoomQuality: 5,
	}
	api.leaveResults = []uint8{protocol.StPracticeRoomSuccess}
	api.claimResults = []*protocol.StPracticeRoomExpResult{{Result: protocol.StPracticeRoomSuccess}}
	api.robResults[seatKey{5, 1}] = protocol.StPracticeRoomSuccess

	cfg := TrainingRoomConfig{AllowRobSeat: true, MaxRobAttempts: 1}
	if err := runStPracticeRoomMaintenance(context.Background(), api, cfg); err != nil {
		t.Fatalf("练功房维护失败: %v", err)
	}
	if !api.hasCall("rob:5:1") {
		t.Errorf("应优先争夺战力最低的 1 号席位, 调用序列: %v", api.calls)
	}
	if api.hasCall("sit:100:0") {
		t.Error("争夺成功后不应再回落普通练功场")
	}
}

// TestRunStPracticeRoom_RobFallsBackToFree 验证争夺次数耗尽后安全回落普通练功场。
func TestRunStPracticeRoom_RobFallsBackToFree(t *testing.T) {
	api := newFakeStPracticeRoomAPI()
	api.lists = []*protocol.StPracticeRoomList{{
		Rooms: []protocol.StPracticeRoomEntry{
			{Quality: 1, Count: 6},
			{Quality: 2, Count: 7},
			{Quality: 3, Count: 8},
			{Quality: 4, Count: 10},
			{Quality: 5, Count: 12},
			{Quality: 100, Count: 1},
		},
		InPracticeFlag: protocol.StPracticeRoomYes,
		Quality:        100,
	}}
	api.infos[5] = &protocol.StPracticeRoomInfo{
		Seats:       []protocol.StPracticeRoomSeat{{SeatSeq: 0, AttackPower: 100}},
		RoomQuality: 5,
	}
	api.leaveResults = []uint8{protocol.StPracticeRoomSuccess}
	api.claimResults = []*protocol.StPracticeRoomExpResult{{Result: protocol.StPracticeRoomSuccess}}
	api.robResults[seatKey{5, 0}] = protocol.StPracticeRoomRobTimeUsedUp
	api.sitResults[seatKey{100, 0}] = protocol.StPracticeRoomSuccess

	cfg := TrainingRoomConfig{AllowRobSeat: true, MaxRobAttempts: 1}
	if err := runStPracticeRoomMaintenance(context.Background(), api, cfg); err != nil {
		t.Fatalf("练功房维护失败: %v", err)
	}
	if !api.hasCall("rob:5:0") {
		t.Errorf("应尝试争夺席位, 调用序列: %v", api.calls)
	}
	if !api.hasCall("sit:100:0") {
		t.Errorf("争夺失败后应回落普通练功场, 调用序列: %v", api.calls)
	}
}

func TestStPracticeRoomFreeSeats(t *testing.T) {
	seats := []protocol.StPracticeRoomSeat{
		{SeatSeq: 0},
		{SeatSeq: 2},
		{SeatSeq: 9}, // 超出 4 席房间, 应被忽略
	}
	got := stPracticeRoomFreeSeats(seats, 4)
	want := []uint8{1, 3}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("空闲席位计算错误: 期望 %v, 实际 %v", want, got)
	}
	if got := stPracticeRoomFreeSeats(nil, 0); got != nil {
		t.Errorf("零席位房间应返回 nil, 实际 %v", got)
	}
}

func TestStPracticeRoomOccupiedSeats_SortsWeakestFirst(t *testing.T) {
	seats := []protocol.StPracticeRoomSeat{
		{SeatSeq: 0, AttackPower: 500, Level: 90},
		{SeatSeq: 1, AttackPower: 100, Level: 80},
		{SeatSeq: 2, AttackPower: 100, Level: 70},
	}
	got := stPracticeRoomOccupiedSeats(seats)
	want := []uint8{2, 1, 0}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("争夺席位排序错误: 期望 %v, 实际 %v", want, got)
	}
}

type seatKey struct {
	quality uint8
	seat    uint8
}

// fakeStPracticeRoomAPI 提供可编排的练功房跨服调用桩。
type fakeStPracticeRoomAPI struct {
	lists     []*protocol.StPracticeRoomList
	listCalls int

	infos   map[uint8]*protocol.StPracticeRoomInfo
	infoErr map[uint8]error

	enterResult uint8
	enterErr    error
	exitResult  uint8

	leaveResults []uint8
	leaveCalls   int

	claimResults []*protocol.StPracticeRoomExpResult
	claimCalls   int

	seatStatus map[seatKey]uint8
	sitResults map[seatKey]uint8
	robResults map[seatKey]uint8

	calls []string
}

func newFakeStPracticeRoomAPI() *fakeStPracticeRoomAPI {
	return &fakeStPracticeRoomAPI{
		enterResult: protocol.StPracticeRoomSuccess,
		exitResult:  protocol.StPracticeRoomSuccess,
		infos:       make(map[uint8]*protocol.StPracticeRoomInfo),
		infoErr:     make(map[uint8]error),
		seatStatus:  make(map[seatKey]uint8),
		sitResults:  make(map[seatKey]uint8),
		robResults:  make(map[seatKey]uint8),
	}
}

func (f *fakeStPracticeRoomAPI) record(call string) {
	f.calls = append(f.calls, call)
}

func (f *fakeStPracticeRoomAPI) hasCall(call string) bool {
	return f.indexOf(call) >= 0
}

func (f *fakeStPracticeRoomAPI) indexOf(call string) int {
	for i, c := range f.calls {
		if c == call {
			return i
		}
	}
	return -1
}

func (f *fakeStPracticeRoomAPI) countCall(call string) int {
	n := 0
	for _, c := range f.calls {
		if c == call {
			n++
		}
	}
	return n
}

func (f *fakeStPracticeRoomAPI) EnterStPracticeRoom(context.Context) (uint8, error) {
	f.record("enter")
	return f.enterResult, f.enterErr
}

func (f *fakeStPracticeRoomAPI) ExitStPracticeRoom(context.Context) (uint8, error) {
	f.record("exit")
	return f.exitResult, nil
}

func (f *fakeStPracticeRoomAPI) GetStPracticeRoomList(context.Context) (*protocol.StPracticeRoomList, error) {
	f.record("list")
	if len(f.lists) == 0 {
		return nil, fmt.Errorf("no fake room list")
	}
	idx := f.listCalls
	if idx >= len(f.lists) {
		idx = len(f.lists) - 1
	}
	f.listCalls++
	return f.lists[idx], nil
}

func (f *fakeStPracticeRoomAPI) GetStPracticeRoomInfo(_ context.Context, quality uint8) (*protocol.StPracticeRoomInfo, error) {
	f.record(fmt.Sprintf("info:%d", quality))
	if err := f.infoErr[quality]; err != nil {
		return nil, err
	}
	info, ok := f.infos[quality]
	if !ok {
		return nil, fmt.Errorf("no fake info for quality %d", quality)
	}
	return info, nil
}

func (f *fakeStPracticeRoomAPI) ClaimStPracticeRoomExp(context.Context) (*protocol.StPracticeRoomExpResult, error) {
	f.record("claim")
	if len(f.claimResults) == 0 {
		return &protocol.StPracticeRoomExpResult{Result: protocol.StPracticeRoomNoExpToGet}, nil
	}
	idx := f.claimCalls
	if idx >= len(f.claimResults) {
		idx = len(f.claimResults) - 1
	}
	f.claimCalls++
	return f.claimResults[idx], nil
}

func (f *fakeStPracticeRoomAPI) LeaveStPracticeRoomSeat(context.Context) (uint8, error) {
	f.record("leave")
	if len(f.leaveResults) == 0 {
		return protocol.StPracticeRoomSuccess, nil
	}
	idx := f.leaveCalls
	if idx >= len(f.leaveResults) {
		idx = len(f.leaveResults) - 1
	}
	f.leaveCalls++
	return f.leaveResults[idx], nil
}

func (f *fakeStPracticeRoomAPI) GetStPracticeRoomSeatStatus(_ context.Context, quality, seat uint8) (uint8, error) {
	f.record(fmt.Sprintf("seat:%d:%d", quality, seat))
	if status, ok := f.seatStatus[seatKey{quality, seat}]; ok {
		return status, nil
	}
	return protocol.StPracticeRoomNobody, nil
}

func (f *fakeStPracticeRoomAPI) SitDownStPracticeRoom(_ context.Context, quality, seat uint8) (uint8, error) {
	f.record(fmt.Sprintf("sit:%d:%d", quality, seat))
	if result, ok := f.sitResults[seatKey{quality, seat}]; ok {
		return result, nil
	}
	return protocol.StPracticeRoomSuccess, nil
}

func (f *fakeStPracticeRoomAPI) RobStPracticeRoomSeat(_ context.Context, quality, seat uint8) (uint8, error) {
	f.record(fmt.Sprintf("rob:%d:%d", quality, seat))
	if result, ok := f.robResults[seatKey{quality, seat}]; ok {
		return result, nil
	}
	return protocol.StPracticeRoomSuccess, nil
}
