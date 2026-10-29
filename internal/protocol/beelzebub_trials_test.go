package protocol

import (
	"testing"
)

func TestBeelzebubTrialsBossName(t *testing.T) {
	cases := []struct {
		id   int32
		want string
	}{
		{1, "魔化大魔王"},
		{2, "魔化东皇"},
		{3, "魔神蚩尤"},
		{71, "魔掌门元识"},
		{72, "魔掌门神识"},
		{159, "龙之魔女"},
		{9999, "试炼魔王(9999)"},
	}

	for _, c := range cases {
		name := BeelzebubTrialsBossName(c.id)
		if name != c.want {
			t.Errorf("boss %d: got %s, want %s", c.id, name, c.want)
		}
	}
}

func TestBeelzebubTrialsBossLevel(t *testing.T) {
	cases := []struct {
		id   int32
		want int32
	}{
		{7, 15},   // 幽鬼 (Level 15)
		{8, 15},   // 将臣 (Level 15)
		{66, 20},  // 魔化小蝶 (Level 20)
		{71, 30},  // 魔掌门元识 (Level 30)
		{72, 30},  // 魔掌门神识 (Level 30)
		{13, 40},  // 魔化刑天 (Level 40)
		{15, 50},  // 鬼眼 (Level 50)
		{17, 60},  // 魔化天将 (Level 60)
		{1, 100},  // 魔化大魔王 (Level 100)
		{3, 150},  // 魔神蚩尤 (Level 150)
		{159, 185}, // 龙之魔女 (Level 185)
		{9999, 0}, // 未知 Boss
	}

	for _, c := range cases {
		lv := BeelzebubTrialsBossLevel(c.id)
		if lv != c.want {
			t.Errorf("boss %d: got level %d, want %d", c.id, lv, c.want)
		}
	}
}

func TestBuildAndParseBeelzebubTrialsCheckCanChallenge(t *testing.T) {
	pkt := BuildBeelzebubTrialsCheckCanChallengePacket(0x12345678)
	if pkt.ActionID != ActionIDBeelzebubTrialsCheckCanChallenge {
		t.Fatalf("expected ActionID 0x%08X, got 0x%08X", ActionIDBeelzebubTrialsCheckCanChallenge, pkt.ActionID)
	}

	// Case 1: Can challenge (SUCCESS = 5)
	canChallenge, err := ParseBeelzebubTrialsCheckCanChallengeResponse([]byte{BeelzebubTrialsSuccess})
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	if !canChallenge {
		t.Fatalf("expected true, got false")
	}

	// Case 2: Cannot challenge (FAILED = 14)
	canChallenge, err = ParseBeelzebubTrialsCheckCanChallengeResponse([]byte{BeelzebubTrialsFailed})
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	if canChallenge {
		t.Fatalf("expected false, got true")
	}

	// Case 3: Empty error
	if _, err := ParseBeelzebubTrialsCheckCanChallengeResponse([]byte{}); err == nil {
		t.Fatal("expected error on empty payload")
	}
}

func TestBuildAndParseBeelzebubTrialsCanChallengeList(t *testing.T) {
	pkt := BuildBeelzebubTrialsGetCanChallengeListPacket(0)
	if pkt.ActionID != ActionIDBeelzebubTrialsGetCanChallengeList {
		t.Fatalf("expected ActionID 0x%08X, got 0x%08X", ActionIDBeelzebubTrialsGetCanChallengeList, pkt.ActionID)
	}

	w := NewWriter()
	w.WriteUint16(3) // 3 bosses
	w.WriteInt32(1)
	w.WriteInt32(71)
	w.WriteInt32(72)

	list, err := ParseBeelzebubTrialsGetCanChallengeListResponse(w.Bytes())
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	if len(list) != 3 || list[0] != 1 || list[1] != 71 || list[2] != 72 {
		t.Fatalf("unexpected list: %v", list)
	}
}

func TestBuildAndParseBeelzebubTrialsCreateAndJoinTeam(t *testing.T) {
	createPkt := BuildBeelzebubTrialsCreateTeamPacket(72, BeelzebubTrialsNotLimit, 0x1111)
	if createPkt.ActionID != ActionIDBeelzebubTrialsCreateTeam {
		t.Fatalf("expected ActionID 0x%08X, got 0x%08X", ActionIDBeelzebubTrialsCreateTeam, createPkt.ActionID)
	}

	res, err := ParseBeelzebubTrialsCreateTeamResponse([]byte{BeelzebubTrialsSuccess})
	if err != nil || res != BeelzebubTrialsSuccess {
		t.Fatalf("create team parse failed: %v, res: %d", err, res)
	}

	joinPkt := BuildBeelzebubTrialsJoinTeamPacket(10086, 0x2222)
	if joinPkt.ActionID != ActionIDBeelzebubTrialsJoinTeam {
		t.Fatalf("expected ActionID 0x%08X, got 0x%08X", ActionIDBeelzebubTrialsJoinTeam, joinPkt.ActionID)
	}

	jRes, err := ParseBeelzebubTrialsJoinTeamResponse([]byte{BeelzebubTrialsSuccess})
	if err != nil || jRes != BeelzebubTrialsSuccess {
		t.Fatalf("join team parse failed: %v, res: %d", err, jRes)
	}
}

func TestBuildAndParseBeelzebubTrialsTeamInfo(t *testing.T) {
	pkt := BuildBeelzebubTrialsGetPlayerTeamInfoPacket(0)
	if pkt.ActionID != ActionIDBeelzebubTrialsGetPlayerTeamInfo {
		t.Fatalf("expected ActionID 0x%08X, got 0x%08X", ActionIDBeelzebubTrialsGetPlayerTeamInfo, pkt.ActionID)
	}

	w := NewWriter()
	w.WriteInt32(1001) // leader ID
	w.WriteUint16(2)   // 2 members

	// Member 1: Leader
	w.WriteInt32(1001)
	w.WriteString("逍遥大侠")
	w.WriteString("主角")
	w.WriteInt16(120)
	w.WriteInt32(0)
	w.WriteInt32(0)
	w.WriteInt32(0)
	w.WriteString("")
	w.WriteInt16(0)

	// Member 2: Helper
	w.WriteInt32(2002)
	w.WriteString("飞羽小仙")
	w.WriteString("飞羽")
	w.WriteInt16(115)
	w.WriteInt32(0)
	w.WriteInt32(0)
	w.WriteInt32(0)
	w.WriteString("")
	w.WriteInt16(0)

	leaderID, members, err := ParseBeelzebubTrialsPlayerTeamInfoResponse(w.Bytes())
	if err != nil {
		t.Fatalf("Parse team info failed: %v", err)
	}
	if leaderID != 1001 || len(members) != 2 {
		t.Fatalf("unexpected members: leader=%d, count=%d", leaderID, len(members))
	}
	if !members[0].IsLeader || members[1].IsLeader {
		t.Fatalf("expected member 0 leader and member 1 helper: %+v", members)
	}
}

func TestBuildAndParseBeelzebubTrialsHelperRoles(t *testing.T) {
	pkt := BuildBeelzebubTrialsGetHelperRoleListPacket(0)
	if pkt.ActionID != ActionIDBeelzebubTrialsGetHelperRoleList {
		t.Fatalf("expected ActionID 0x%08X, got 0x%08X", ActionIDBeelzebubTrialsGetHelperRoleList, pkt.ActionID)
	}

	w := NewWriter()
	w.WriteUint16(1)
	w.WriteInt32(501)
	w.WriteString("巨灵神镜像")
	w.WriteInt32(110)
	w.WriteInt32(1)
	w.WriteUint8(BeelzebubTrialsDeploy)
	w.WriteInt16(200)

	roles, err := ParseBeelzebubTrialsHelperRoleListResponse(w.Bytes())
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	if len(roles) != 1 || roles[0].RoleID != 501 || !roles[0].InBody || roles[0].Name != "巨灵神镜像" {
		t.Fatalf("unexpected roles: %+v", roles)
	}

	choosePkt := BuildBeelzebubTrialsChooseHelperRolePacket(501, 1, 0)
	if choosePkt.ActionID != ActionIDBeelzebubTrialsChooseHelperRole {
		t.Fatalf("expected ActionID 0x%08X, got 0x%08X", ActionIDBeelzebubTrialsChooseHelperRole, choosePkt.ActionID)
	}

	chooseRes, err := ParseBeelzebubTrialsChooseHelperRoleResponse([]byte{BeelzebubTrialsSuccess})
	if err != nil || chooseRes != BeelzebubTrialsSuccess {
		t.Fatalf("choose helper response error: %v, res: %d", err, chooseRes)
	}
}

func TestBuildAndParseBeelzebubTrialsFightAndResult(t *testing.T) {
	fightPkt := BuildBeelzebubTrialsFightPacket(0)
	if fightPkt.ActionID != ActionIDBeelzebubTrialsFight {
		t.Fatalf("expected ActionID 0x%08X, got 0x%08X", ActionIDBeelzebubTrialsFight, fightPkt.ActionID)
	}

	fRes, err := ParseBeelzebubTrialsFightResponse([]byte{BeelzebubTrialsSuccess})
	if err != nil || fRes != BeelzebubTrialsSuccess {
		t.Fatalf("fight response error: %v, res: %d", err, fRes)
	}

	warResultPkt := BuildBeelzebubTrialsGetWarResultPacket(0)
	if warResultPkt.ActionID != ActionIDBeelzebubTrialsGetWarResult {
		t.Fatalf("expected ActionID 0x%08X, got 0x%08X", ActionIDBeelzebubTrialsGetWarResult, warResultPkt.ActionID)
	}

	// 模拟战报结果
	w := NewWriter()
	w.WriteUint8(BeelzebubTrialsSuccess)
	w.WriteInt32(100) // extra
	w.WriteInt32(200)
	w.WriteUint16(0)   // 0 war data

	res, err := ParseBeelzebubTrialsWarResultResponse(w.Bytes())
	if err != nil {
		t.Fatalf("war result parse failed: %v", err)
	}
	if res.Result != BeelzebubTrialsSuccess {
		t.Fatalf("expected success result, got %d", res.Result)
	}

	leavePkt := BuildBeelzebubTrialsLeaveWarPacket(1, 0)
	if leavePkt.ActionID != ActionIDBeelzebubTrialsLeaveWar {
		t.Fatalf("expected ActionID 0x%08X, got 0x%08X", ActionIDBeelzebubTrialsLeaveWar, leavePkt.ActionID)
	}

	exitPkt := BuildBeelzebubTrialsExitTeamPacket(0)
	if exitPkt.ActionID != ActionIDBeelzebubTrialsExitTeam {
		t.Fatalf("expected ActionID 0x%08X, got 0x%08X", ActionIDBeelzebubTrialsExitTeam, exitPkt.ActionID)
	}
}

func TestBuildAndParseBeelzebubTrialsMoral(t *testing.T) {
	openPkt := BuildBeelzebubTrialsOpenMoralPacket(0)
	if openPkt.ActionID != ActionIDBeelzebubTrialsOpenMoral {
		t.Fatalf("expected ActionID 0x%08X, got 0x%08X", ActionIDBeelzebubTrialsOpenMoral, openPkt.ActionID)
	}

	w := NewWriter()
	w.WriteInt32(12500) // exp
	w.WriteInt32(5)     // lv
	w.WriteInt32(1)     // bGet = 1

	moral, err := ParseBeelzebubTrialsOpenMoralResponse(w.Bytes())
	if err != nil {
		t.Fatalf("moral parse failed: %v", err)
	}
	if moral.Exp != 12500 || moral.Level != 5 || !moral.CanClaim {
		t.Fatalf("unexpected moral: %+v", moral)
	}

	awardPkt := BuildBeelzebubTrialsGetMoralAwardPacket(5, 0)
	if awardPkt.ActionID != ActionIDBeelzebubTrialsGetMoralAward {
		t.Fatalf("expected ActionID 0x%08X, got 0x%08X", ActionIDBeelzebubTrialsGetMoralAward, awardPkt.ActionID)
	}

	aRes, err := ParseBeelzebubTrialsGetMoralAwardResponse([]byte{BeelzebubTrialsSuccess})
	if err != nil || aRes != BeelzebubTrialsSuccess {
		t.Fatalf("moral award parse failed: %v, res: %d", err, aRes)
	}
}
