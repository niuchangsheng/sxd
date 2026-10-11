// Package protocol 提供神仙道客户端与服务器之间的二进制协议序列化、反序列化、大端序编解码与透明 zlib 流解压缩引擎。
//
// @author Ateng
// @since 2026-10-09
package protocol

import (
	"encoding/hex"
	"testing"
)

func TestArenaCodec_BuildAndParse(t *testing.T) {
	// 1. 测试 ArenaGetTimesRequest
	getTimesReq := ArenaGetTimesRequest{PrevAct: 0x00200000}
	pktTimes, err := BuildArenaGetTimesPacket(getTimesReq)
	if err != nil {
		t.Fatalf("BuildArenaGetTimesPacket 失败: %v", err)
	}
	if pktTimes.ActionID != ActionIDArenaGetTimes {
		t.Fatalf("ActionID 不匹配: 期望 0x%08X, 实际 0x%08X", ActionIDArenaGetTimes, pktTimes.ActionID)
	}

	parsedTimes, err := ParseArenaGetTimesRequest(pktTimes.Payload)
	if err != nil {
		t.Fatalf("ParseArenaGetTimesRequest 失败: %v", err)
	}
	if parsedTimes.PrevAct != 0x00200000 {
		t.Errorf("PrevAct 不匹配: 期望 0x00200000, 实际 0x%08X", parsedTimes.PrevAct)
	}

	// 2. 测试 ArenaGetTimesResult 对齐真实抓包 Hex
	// 真实抓包: 00000005 (5 次剩余)
	resPkt, err := BuildArenaGetTimesResultPacket(5)
	if err != nil {
		t.Fatalf("BuildArenaGetTimesResultPacket 失败: %v", err)
	}
	if hex.EncodeToString(resPkt.Payload) != "00000005" {
		t.Errorf("剩余次数 Hex 不匹配: %s", hex.EncodeToString(resPkt.Payload))
	}
	parsedRes, err := ParseArenaGetTimesResult(resPkt.Payload)
	if err != nil {
		t.Fatalf("ParseArenaGetTimesResult 失败: %v", err)
	}
	if parsedRes.RemainingTimes != 5 {
		t.Errorf("RemainingTimes 不匹配: 期望 5, 实际 %d", parsedRes.RemainingTimes)
	}

	// 3. 测试 ArenaChallengeRequest 对齐真实抓包 Hex
	// 真实抓包: 00000005001c0001 (TargetRank=5, PrevAct=0x001C0001)
	challengeReq := ArenaChallengeRequest{
		TargetRank: 5,
		PrevAct:    0x001C0001,
	}
	pktChallenge, err := BuildArenaChallengePacket(challengeReq)
	if err != nil {
		t.Fatalf("BuildArenaChallengePacket 失败: %v", err)
	}
	if pktChallenge.ActionID != ActionIDArenaChallenge {
		t.Fatalf("ActionID 不匹配: 期望 0x%08X, 实际 0x%08X", ActionIDArenaChallenge, pktChallenge.ActionID)
	}

	expectedHex := "00000005001c0001"
	actualHex := hex.EncodeToString(pktChallenge.Payload)
	if actualHex != expectedHex {
		t.Errorf("挑战载荷 Hex 不匹配: 期望 %s, 实际 %s", expectedHex, actualHex)
	}

	parsedChallenge, err := ParseArenaChallengeRequest(pktChallenge.Payload)
	if err != nil {
		t.Fatalf("ParseArenaChallengeRequest 失败: %v", err)
	}
	if parsedChallenge.TargetRank != 5 || parsedChallenge.PrevAct != 0x001C0001 {
		t.Errorf("挑战解析结果不符: %+v", parsedChallenge)
	}

	// 4. 测试 ArenaGetOpponentsResult 编解码
	opponents := []ArenaOpponent{
		{Rank: 5, RoleID: 1001, Name: "初级剑仙", FightValue: 35000},
		{Rank: 4, RoleID: 1002, Name: "天罗神将", FightValue: 68000},
	}
	oppPkt, err := BuildArenaGetOpponentsResultPacket(opponents)
	if err != nil {
		t.Fatalf("BuildArenaGetOpponentsResultPacket 失败: %v", err)
	}
	parsedOppRes, err := ParseArenaGetOpponentsResult(oppPkt.Payload)
	if err != nil {
		t.Fatalf("ParseArenaGetOpponentsResult 失败: %v", err)
	}
	if len(parsedOppRes.Opponents) != 2 || parsedOppRes.Opponents[0].Name != "初级剑仙" {
		t.Errorf("对手列表解析不匹配: %+v", parsedOppRes)
	}

	// 5. 测试 ArenaChallengeResult 编解码
	chRes := ArenaChallengeResult{
		Success: true,
		NewRank: 5,
	}
	chPkt, err := BuildArenaChallengeResultPacket(chRes)
	if err != nil {
		t.Fatalf("BuildArenaChallengeResultPacket 失败: %v", err)
	}
	parsedChRes, err := ParseArenaChallengeResult(chPkt.Payload)
	if err != nil {
		t.Fatalf("ParseArenaChallengeResult 失败: %v", err)
	}
	if !parsedChRes.Success || parsedChRes.NewRank != 5 {
		t.Errorf("挑战结算结果不匹配: %+v", parsedChRes)
	}
}
