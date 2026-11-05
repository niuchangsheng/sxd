// Package protocol_test 提供伙伴猜猜看协议 (Module 369) 的测试用例。
package protocol_test

import (
	"testing"

	"sxd-pie-ng/internal/protocol"
)

func TestGuessPartnerStartResult(t *testing.T) {
	rd, err := protocol.ParseGuessPartnerStartResult(protocol.BuildGuessPartnerStartResultPacket(protocol.GuessPartnerRound{
		Result: protocol.GuessPartnerResultSuccess, PictureID: 7,
		RemainScrapeTimes: 2, RemainGuessTimes: 1, RoleIDs: []int32{11, 22, 33},
	}).Payload)
	if err != nil || rd.PictureID != 7 || len(rd.RoleIDs) != 3 || rd.RoleIDs[2] != 33 {
		t.Fatalf("开局回包不一致: %+v err=%v", rd, err)
	}
}

func TestGuessPartnerGuessResult(t *testing.T) {
	res, err := protocol.ParseGuessPartnerGuessResult(
		protocol.BuildGuessPartnerGuessResultPacket(protocol.GuessPartnerResultRight, 0, 5715, 2).Payload)
	if err != nil || res.Result != protocol.GuessPartnerResultRight || res.Award.ItemID != 5715 {
		t.Fatalf("猜对回包不一致: %+v err=%v", res, err)
	}
	// 失败码: 猜错
	res, err = protocol.ParseGuessPartnerGuessResult(
		protocol.BuildGuessPartnerGuessResultPacket(protocol.GuessPartnerResultWrong, 1, 0, 0).Payload)
	if err != nil || res.Result != protocol.GuessPartnerResultWrong {
		t.Fatalf("期望猜错码 %d, got %+v err=%v", protocol.GuessPartnerResultWrong, res, err)
	}
}

func TestGuessPartnerScrapeResult(t *testing.T) {
	result, remain, err := protocol.ParseGuessPartnerScrapeResult(
		protocol.BuildGuessPartnerScrapeResultPacket(protocol.GuessPartnerResultSuccess, 1).Payload)
	if err != nil || result != protocol.GuessPartnerResultSuccess || remain != 1 {
		t.Fatalf("刮图回包不一致: result=%d remain=%d err=%v", result, remain, err)
	}
}
