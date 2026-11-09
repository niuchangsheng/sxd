package minigame

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"sxd-pie-ng/internal/dictionary"
	"sxd-pie-ng/internal/protocol"
	"sxd-pie-ng/internal/scheduler"
)

func masterPracticeTestRepo(t *testing.T) dictionary.Repository {
	t.Helper()
	repo, err := dictionary.NewRepository("../../../data/Pieb.db")
	if err != nil {
		t.Skipf("Pieb.db unavailable: %v", err)
	}
	t.Cleanup(func() { repo.Close() })
	return repo
}

func masterPracticeTestPanel() *protocol.MasterPracticePanel {
	return &protocol.MasterPracticePanel{Jobs: []protocol.MasterPracticeJobInfo{
		{JobID: 1}, {JobID: 2}, {JobID: 3}, {JobID: 5}, {JobID: 6}, {JobID: 15},
	}}
}

// masterPracticeTestDeploy 返回主角 JobId=15、已上阵 JobId=1 伙伴的混编阵容。
func masterPracticeTestDeploy() *protocol.MasterPracticeDeployInfo {
	return &protocol.MasterPracticeDeployInfo{
		Roles: []protocol.MasterPracticeDeployRole{
			{PlayerRoleID: 1035439, RoleID: 606, RoleName: "魔林月如", IsDeploy: 1},
			{PlayerRoleID: 227943, IsMainRole: 1, RoleID: 511, RoleName: "飞羽男天师", IsDeploy: 1},
		},
		Grids: []protocol.MasterPracticeDeployGrid{
			{GridTypeID: 1, PlayerRoleID: 227943},
			{GridTypeID: 9, PlayerRoleID: 1035439},
			{GridTypeID: 2},
		},
	}
}

func TestMasterPracticeFindStunt_PrefersInUse(t *testing.T) {
	stunts := []protocol.LibraryStuntInfo{
		{RoleJobID: 1, RoleStuntID: 111, IsActive: 0},
		{RoleJobID: 1, RoleStuntID: 222, IsActive: 1},
		{RoleJobID: 1, RoleStuntID: 333, IsActive: 1, IsUse: 1},
		{RoleJobID: 3, RoleStuntID: 444, IsActive: 1},
		{RoleJobID: 2, RoleStuntID: 999, IsActive: 0}, // 线上常见: 未装备但 next/id 有值
	}
	if got, ok := masterPracticeFindStunt(stunts, 1); !ok || got != 333 {
		t.Fatalf("expected in-use stunt 333, got %d ok=%v", got, ok)
	}
	if got, ok := masterPracticeFindStunt(stunts, 3); !ok || got != 444 {
		t.Fatalf("expected active stunt 444, got %d ok=%v", got, ok)
	}
	if _, ok := masterPracticeFindStunt(stunts, 2); ok {
		t.Fatal("expected inactive stunt to be rejected for job 2")
	}
	if _, ok := masterPracticeFindStunt(stunts, 5); ok {
		t.Fatal("expected no stunt for unlisted job 5")
	}
}

func TestMasterPracticeFindStunt_FallsBackToNextStuntID(t *testing.T) {
	stunts := []protocol.LibraryStuntInfo{
		{RoleJobID: 15, NextRoleStuntID: 555, IsActive: 1},
	}
	if got, ok := masterPracticeFindStunt(stunts, 15); !ok || got != 555 {
		t.Fatalf("expected next stunt 555, got %d ok=%v", got, ok)
	}
}

func TestMasterPracticeEnsureStunt_Success(t *testing.T) {
	stunts := []protocol.LibraryStuntInfo{
		{RoleJobID: 1, RoleStuntID: 1234, IsActive: 1},
	}
	fake := &fakeMasterPracticeCall{
		stuntResultSet: true,
		stuntResult:    protocol.LibraryResultSuccess,
		stuntRoleID:    600,
		stuntStuntID:   1234,
	}
	switched, err := masterPracticeEnsureStunt(
		context.Background(), 15, 1, stunts,
		MasterPracticeConfig{AllowSwitchStunt: true}, "s0-test", fake.call,
	)
	if err != nil || !switched {
		t.Fatalf("expected switch success, switched=%v err=%v", switched, err)
	}
	want := []string{"use_stunt 1234"}
	if fmt.Sprint(fake.calls) != fmt.Sprint(want) {
		t.Fatalf("expected calls %v, got %v", want, fake.calls)
	}
}

func TestMasterPracticeEnsureStunt_NoStuntSkipsWithoutSending(t *testing.T) {
	fake := &fakeMasterPracticeCall{}
	switched, err := masterPracticeEnsureStunt(
		context.Background(), 15, 1, nil,
		MasterPracticeConfig{AllowSwitchStunt: true}, "s0-test", fake.call,
	)
	if switched || err == nil {
		t.Fatalf("expected skip with error, switched=%v err=%v", switched, err)
	}
	if len(fake.calls) != 0 {
		t.Fatalf("expected no packet sent, got %v", fake.calls)
	}
}

func TestMasterPracticeEnsureStunt_ResultCodeFailure(t *testing.T) {
	stunts := []protocol.LibraryStuntInfo{
		{RoleJobID: 1, RoleStuntID: 1234, IsActive: 1},
	}
	fake := &fakeMasterPracticeCall{
		stuntResultSet: true,
		stuntResult:    protocol.LibraryResultNoHave,
	}
	switched, err := masterPracticeEnsureStunt(
		context.Background(), 15, 1, stunts,
		MasterPracticeConfig{AllowSwitchStunt: true}, "s0-test", fake.call,
	)
	if switched || err == nil {
		t.Fatalf("expected result-code failure, switched=%v err=%v", switched, err)
	}
}

func TestMasterPracticeEnsureStunt_DisabledDoesNotSend(t *testing.T) {
	stunts := []protocol.LibraryStuntInfo{
		{RoleJobID: 1, RoleStuntID: 1234, IsActive: 1},
	}
	fake := &fakeMasterPracticeCall{}
	switched, err := masterPracticeEnsureStunt(
		context.Background(), 15, 1, stunts,
		MasterPracticeConfig{AllowSwitchStunt: false}, "s0-test", fake.call,
	)
	if switched || err == nil {
		t.Fatalf("expected disabled switch to fail, switched=%v err=%v", switched, err)
	}
	if len(fake.calls) != 0 {
		t.Fatalf("expected no packet sent, got %v", fake.calls)
	}
}

func TestMasterPracticeEnsureStunt_SameJobIsNoop(t *testing.T) {
	fake := &fakeMasterPracticeCall{}
	switched, err := masterPracticeEnsureStunt(
		context.Background(), 15, 15, nil,
		MasterPracticeConfig{AllowSwitchStunt: true}, "s0-test", fake.call,
	)
	if err != nil || switched {
		t.Fatalf("expected no-op, switched=%v err=%v", switched, err)
	}
}

func TestMasterPracticeMismatchedPartners_ExcludesMainRole(t *testing.T) {
	repo := masterPracticeTestRepo(t)
	got := masterPracticeMismatchedPartners(masterPracticeTestDeploy(), 15, repo)
	if len(got) != 1 || got[0] != 1035439 {
		t.Fatalf("expected only partner 1035439 to be removed, got %v", got)
	}
}

func TestMasterPracticeDeployCandidate_NeedsSameJobPartner(t *testing.T) {
	repo := masterPracticeTestRepo(t)
	// 现有阵容中的伙伴 JobId=1，没有 JobId=15 伙伴可上阵。
	if _, _, ok := masterPracticeDeployCandidate(masterPracticeTestDeploy(), 15, repo); ok {
		t.Fatal("expected no same-job partner candidate")
	}

	// 补充一名未上阵的 JobId=15 伙伴后应能选中 9 号空位。
	deploy := masterPracticeTestDeploy()
	deploy.Roles = append(deploy.Roles, protocol.MasterPracticeDeployRole{
		PlayerRoleID: 300001, RoleID: 511, RoleName: "天师伙伴", IsDeploy: 0,
	})
	deploy.Grids[1].PlayerRoleID = 0 // 9 号位空出
	playerRoleID, grid, ok := masterPracticeDeployCandidate(deploy, 15, repo)
	if !ok || playerRoleID != 300001 || grid != 9 {
		t.Fatalf("expected partner 300001 on grid 9, got id=%d grid=%d ok=%v", playerRoleID, grid, ok)
	}
}

func TestMasterPracticeDeployMatches(t *testing.T) {
	repo := masterPracticeTestRepo(t)
	if ok, reason := masterPracticeDeployMatches(masterPracticeTestDeploy(), 15, repo); ok {
		t.Fatalf("expected mixed lineup to fail, got %q", reason)
	}

	matching := masterPracticeTestDeploy()
	matching.Grids[1].PlayerRoleID = 0 // 下阵异职业伙伴
	if ok, reason := masterPracticeDeployMatches(matching, 15, repo); !ok {
		t.Fatalf("expected main-only lineup to match, got %q", reason)
	}
}

// fakeMasterPracticeCall 按预设回包序列模拟宗师修行协议交互。
type fakeMasterPracticeCall struct {
	infos          []*protocol.MasterPracticeDeployInfo
	nextInfo       int
	deployCodes    []uint8
	nextDeploy     int
	stuntResultSet bool
	stuntResult    uint8
	stuntRoleID    int16
	stuntStuntID   int16
	calls          []string
}

func (f *fakeMasterPracticeCall) call(pkt *protocol.Packet, act uint32) (*protocol.Packet, error) {
	switch act {
	case protocol.ActionIDMasterPracticeDeployInfo:
		if f.nextInfo >= len(f.infos) {
			return nil, fmt.Errorf("unexpected deploy info query")
		}
		info := f.infos[f.nextInfo]
		f.nextInfo++
		return protocol.BuildMasterPracticeDeployInfoResultPacket(*info), nil
	case protocol.ActionIDMasterPracticeDeploy:
		r := protocol.NewReader(pkt.Payload)
		playerRoleID, err := r.ReadInt32()
		if err != nil {
			return nil, err
		}
		grid, err := r.ReadUint8()
		if err != nil {
			return nil, err
		}
		f.calls = append(f.calls, fmt.Sprintf("deploy %d->%d", playerRoleID, grid))
		code := protocol.MasterPracticeResultSuccess
		if f.nextDeploy < len(f.deployCodes) {
			code = f.deployCodes[f.nextDeploy]
		}
		f.nextDeploy++
		return protocol.BuildMasterPracticeActionCodePacket(act, code), nil
	case protocol.ActionIDLibraryUseStuntSkill:
		if len(pkt.Payload) != 2 {
			return nil, fmt.Errorf("use_stunt_skill payload must be Short(2B), got %d bytes", len(pkt.Payload))
		}
		r := protocol.NewReader(pkt.Payload)
		stuntID, err := r.ReadInt16()
		if err != nil {
			return nil, err
		}
		f.calls = append(f.calls, fmt.Sprintf("use_stunt %d", stuntID))
		result := uint8(protocol.LibraryResultSuccess)
		if f.stuntResultSet {
			result = f.stuntResult
		}
		return protocol.BuildLibraryUseStuntSkillResultPacket(result, f.stuntRoleID, f.stuntStuntID), nil
	default:
		return nil, fmt.Errorf("unexpected action 0x%08X", act)
	}
}

func TestMasterPracticeNormalizeDeploy_RemovesMismatchedPartner(t *testing.T) {
	repo := masterPracticeTestRepo(t)
	afterDown := &protocol.MasterPracticeDeployInfo{
		Result: protocol.MasterPracticeResultSuccess,
		Roles: []protocol.MasterPracticeDeployRole{
			{PlayerRoleID: 1035439, RoleID: 606, RoleName: "魔林月如"},
			{PlayerRoleID: 227943, IsMainRole: 1, RoleID: 511, RoleName: "飞羽男天师", IsDeploy: 1},
		},
		Grids: []protocol.MasterPracticeDeployGrid{
			{GridTypeID: 1, PlayerRoleID: 227943},
			{GridTypeID: 9},
			{GridTypeID: 2},
		},
	}
	fake := &fakeMasterPracticeCall{infos: []*protocol.MasterPracticeDeployInfo{afterDown}}

	got, err := masterPracticeNormalizeDeploy(context.Background(), masterPracticeTestDeploy(), 15, repo, fake.call)
	if err != nil {
		t.Fatalf("normalize failed: %v", err)
	}
	if ok, reason := masterPracticeDeployMatches(got, 15, repo); !ok {
		t.Fatalf("expected normalized lineup to match, got %q", reason)
	}
	want := []string{"deploy 1035439->0"}
	if fmt.Sprint(fake.calls) != fmt.Sprint(want) {
		t.Fatalf("expected calls %v, got %v", want, fake.calls)
	}
}

func TestMasterPracticeNormalizeDeploy_DeploysSameJobPartner(t *testing.T) {
	repo := masterPracticeTestRepo(t)
	// 下阵异职业伙伴后：主角 JobId=15，另有未上阵的 JobId=15 伙伴 300001，9 号位空出。
	afterDown := &protocol.MasterPracticeDeployInfo{
		Result: protocol.MasterPracticeResultSuccess,
		Roles: []protocol.MasterPracticeDeployRole{
			{PlayerRoleID: 1035439, RoleID: 606, RoleName: "魔林月如"},
			{PlayerRoleID: 300001, RoleID: 511, RoleName: "天师伙伴"},
			{PlayerRoleID: 227943, IsMainRole: 1, RoleID: 511, RoleName: "飞羽男天师", IsDeploy: 1},
		},
		Grids: []protocol.MasterPracticeDeployGrid{
			{GridTypeID: 1, PlayerRoleID: 227943},
			{GridTypeID: 9},
			{GridTypeID: 2},
		},
	}
	afterUp := &protocol.MasterPracticeDeployInfo{
		Result: protocol.MasterPracticeResultSuccess,
		Roles:  afterDown.Roles,
		Grids: []protocol.MasterPracticeDeployGrid{
			{GridTypeID: 1, PlayerRoleID: 227943},
			{GridTypeID: 9, PlayerRoleID: 300001},
			{GridTypeID: 2},
		},
	}

	// 初始阵容中增加该同职业伙伴，但未上阵。
	initial := masterPracticeTestDeploy()
	initial.Roles = append(initial.Roles, protocol.MasterPracticeDeployRole{
		PlayerRoleID: 300001, RoleID: 511, RoleName: "天师伙伴",
	})
	fake := &fakeMasterPracticeCall{infos: []*protocol.MasterPracticeDeployInfo{afterDown, afterUp}}

	got, err := masterPracticeNormalizeDeploy(context.Background(), initial, 15, repo, fake.call)
	if err != nil {
		t.Fatalf("normalize failed: %v", err)
	}
	if ok, reason := masterPracticeDeployMatches(got, 15, repo); !ok {
		t.Fatalf("expected normalized lineup to match, got %q", reason)
	}
	want := []string{"deploy 1035439->0", "deploy 300001->9"}
	if fmt.Sprint(fake.calls) != fmt.Sprint(want) {
		t.Fatalf("expected calls %v, got %v", want, fake.calls)
	}
}

func TestMasterPracticeNormalizeDeploy_DeployFailureAborts(t *testing.T) {
	repo := masterPracticeTestRepo(t)
	fake := &fakeMasterPracticeCall{deployCodes: []uint8{protocol.MasterPracticeResultFailed}}

	if _, err := masterPracticeNormalizeDeploy(context.Background(), masterPracticeTestDeploy(), 15, repo, fake.call); err == nil {
		t.Fatal("expected deploy failure to abort normalization")
	}
}

func TestClassifyMasterPracticeChallengeResult(t *testing.T) {
	outcome, err := classifyMasterPracticeChallengeResult(protocol.MasterPracticeResultSuccess)
	if outcome != masterPracticeChallengeContinue || err != nil {
		t.Fatalf("success: outcome=%v err=%v", outcome, err)
	}

	outcome, err = classifyMasterPracticeChallengeResult(protocol.MasterPracticeResultJobError)
	if outcome != masterPracticeChallengeJobError || !errors.Is(err, scheduler.ErrConditionNotMet) {
		t.Fatalf("job error: outcome=%v err=%v", outcome, err)
	}

	outcome, err = classifyMasterPracticeChallengeResult(protocol.MasterPracticeResultNoChallengeTimes)
	if outcome != masterPracticeChallengeNoTimes || !errors.Is(err, scheduler.ErrAttemptsExhausted) {
		t.Fatalf("no times: outcome=%v err=%v", outcome, err)
	}

	outcome, err = classifyMasterPracticeChallengeResult(protocol.MasterPracticeResultLessStars)
	if outcome != masterPracticeChallengeStop || err != nil {
		t.Fatalf("less stars: outcome=%v err=%v", outcome, err)
	}

	outcome, err = classifyMasterPracticeChallengeResult(200)
	if outcome != masterPracticeChallengeStop || err == nil {
		t.Fatalf("unknown code must stop with error, got outcome=%v err=%v", outcome, err)
	}
}

// fakeMasterChallengeCall 按预设结果码序列模拟宗师修行挑战回包。
type fakeMasterChallengeCall struct {
	results []uint8
	next    int
	calls   int
}

func (f *fakeMasterChallengeCall) call(pkt *protocol.Packet, act uint32) (*protocol.Packet, error) {
	if act != protocol.ActionIDMasterPracticeChallenge {
		return nil, fmt.Errorf("unexpected action 0x%08X", act)
	}
	if f.next >= len(f.results) {
		return nil, fmt.Errorf("challenge result sequence exhausted")
	}
	code := f.results[f.next]
	f.next++
	f.calls++
	return protocol.BuildMasterPracticeActionCodePacket(act, code), nil
}

// TestMasterPracticeChallengeJob_RetriesFailedStarThenSucceeds 验证战斗失败后重试同星级。
func TestMasterPracticeChallengeJob_RetriesFailedStarThenSucceeds(t *testing.T) {
	job := protocol.MasterPracticeJobInfo{JobID: 15, TodayChallenge: []int32{2, 3}}
	fake := &fakeMasterChallengeCall{results: []uint8{
		protocol.MasterPracticeResultFailed,
		protocol.MasterPracticeResultSuccess,
	}}

	consumed, full, failed, err := masterPracticeChallengeJob(
		context.Background(), job, 3, 2, "s0-test", fake.call)
	if err != nil || full || failed {
		t.Fatalf("expected retry success, consumed=%d full=%v failed=%v err=%v", consumed, full, failed, err)
	}
	if consumed != 1 || fake.calls != 2 {
		t.Fatalf("失败不消耗次数，成功计 1 次, got consumed=%d calls=%d", consumed, fake.calls)
	}
}

// TestMasterPracticeChallengeJob_RetriesExhaustedSignalsJobSwitch 验证重试耗尽后
// 返回 failed=true，调用方可切换职业。
func TestMasterPracticeChallengeJob_RetriesExhaustedSignalsJobSwitch(t *testing.T) {
	job := protocol.MasterPracticeJobInfo{JobID: 15, TodayChallenge: []int32{2, 3}}
	fake := &fakeMasterChallengeCall{results: []uint8{
		protocol.MasterPracticeResultFailed,
		protocol.MasterPracticeResultFailed,
	}}

	consumed, full, failed, err := masterPracticeChallengeJob(
		context.Background(), job, 3, 1, "s0-test", fake.call)
	if err != nil || full {
		t.Fatalf("unexpected error/full: consumed=%d full=%v err=%v", consumed, full, err)
	}
	if !failed {
		t.Fatal("expected failed=true after retry exhaustion")
	}
	if consumed != 0 || fake.calls != 2 {
		t.Fatalf("失败不消耗次数, got consumed=%d calls=%d", consumed, fake.calls)
	}
}

// TestMasterPracticeChallengeJob_FailureWithoutRetrySwitchesImmediately 验证
// 开启失败切换时（maxRetries=0）首次失败即返回 failed=true，把剩余次数让给其他职业。
func TestMasterPracticeChallengeJob_FailureWithoutRetrySwitchesImmediately(t *testing.T) {
	job := protocol.MasterPracticeJobInfo{JobID: 15, TodayChallenge: []int32{2, 3}}
	fake := &fakeMasterChallengeCall{results: []uint8{
		protocol.MasterPracticeResultFailed,
	}}

	consumed, full, failed, err := masterPracticeChallengeJob(
		context.Background(), job, 3, 0, "s0-test", fake.call)
	if err != nil || full {
		t.Fatalf("unexpected error/full: consumed=%d full=%v err=%v", consumed, full, err)
	}
	if !failed {
		t.Fatal("首次失败应立即返回 failed=true")
	}
	if consumed != 0 || fake.calls != 1 {
		t.Fatalf("失败应立即停止重试且不消耗次数, got consumed=%d calls=%d", consumed, fake.calls)
	}
}

// TestMasterPracticeBuildOrder_FallbackJobsRequireStunt 验证失败切换候选：
// 优先当前职业，再追加未满星/今日未完成且已激活神器绝技的职业。
func TestMasterPracticeBuildOrder_FallbackJobsRequireStunt(t *testing.T) {
	panel := &protocol.MasterPracticePanel{Jobs: []protocol.MasterPracticeJobInfo{
		{JobID: 15},
		{JobID: 1},
		{JobID: 2},
		{JobID: 3, TotalStars: 30},
		{JobID: 5},
		{JobID: 6, TodayChallenge: []int32{1, 2, 3}},
	}}
	stunts := []protocol.LibraryStuntInfo{
		{RoleJobID: 1, RoleStuntID: 101, IsActive: 1},
		{RoleJobID: 2, RoleStuntID: 102, IsActive: 1},
		{RoleJobID: 3, RoleStuntID: 103, IsActive: 1},
		// Job 5 未提供已激活绝技，不能作为切换目标。
	}
	cfg := MasterPracticeConfig{
		MaxStarsPerJob:     30,
		AllowSwitchStunt:   true,
		SwitchJobOnFailure: true,
	}

	order := masterPracticeBuildOrder(cfg, panel, 15, stunts)
	want := []int32{15, 1, 2}
	if fmt.Sprint(order) != fmt.Sprint(want) {
		t.Fatalf("expected order %v, got %v", want, order)
	}

	cfg.SwitchJobOnFailure = false
	order = masterPracticeBuildOrder(cfg, panel, 15, stunts)
	if fmt.Sprint(order) != fmt.Sprint([]int32{15}) {
		t.Fatalf("switch disabled: expected only current job, got %v", order)
	}
}

func TestBuildMasterPracticeDeployPacketLayout(t *testing.T) {
	up := protocol.BuildMasterPracticeDeployPacket(768553, 9)
	if up.ActionID != protocol.ActionIDMasterPracticeDeploy {
		t.Fatalf("unexpected action id: 0x%08X", up.ActionID)
	}
	// Int32(768553=0x000BBA29) + Byte(9)
	if got := "000bba2909"; len(up.Payload) != 5 || hexString(up.Payload) != got {
		t.Fatalf("unexpected up payload: %x", up.Payload)
	}

	down := protocol.BuildMasterPracticeDeployPacket(1035439, 0)
	// Int32(1035439=0x000FCCAF) + Byte(0)
	if got := "000fccaf00"; len(down.Payload) != 5 || hexString(down.Payload) != got {
		t.Fatalf("unexpected down payload: %x", down.Payload)
	}
}

func hexString(b []byte) string {
	const digits = "0123456789abcdef"
	out := make([]byte, len(b)*2)
	for i, v := range b {
		out[i*2] = digits[v>>4]
		out[i*2+1] = digits[v&0x0f]
	}
	return string(out)
}
