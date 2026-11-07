// Package protocol 提供叶公好龙培养协议编解码测试。
//
// @author Ateng
// @since 2026-11-07
package protocol

import (
	"encoding/hex"
	"testing"
)

func TestPetAnimalInfo_LiveSample(t *testing.T) {
	// 2026-11-07 s1 线上样本:
	// name="", lv=2, star=9, exp=180228, feed_num=0。
	payload, err := hex.DecodeString("000002090002c00400000000")
	if err != nil {
		t.Fatalf("decode sample: %v", err)
	}
	info, err := ParsePetAnimalInfoResult(payload)
	if err != nil {
		t.Fatalf("parse info: %v", err)
	}
	want := PetAnimalInfo{Name: "", Lv: 2, Star: 9, Exp: 180228, FeedNum: 0}
	if *info != want {
		t.Fatalf("叶公好龙面板字段不符: want=%+v got=%+v", want, *info)
	}
}

func TestPetAnimalInfo_RoundTrip(t *testing.T) {
	want := PetAnimalInfo{Name: "叶公好龙", Lv: 3, Star: 1, Exp: 999, FeedNum: 20}
	got, err := ParsePetAnimalInfoResult(BuildPetAnimalInfoResultPacket(want).Payload)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if *got != want {
		t.Fatalf("面板回包不幂等: want=%+v got=%+v", want, *got)
	}
}

func TestPetAnimalFeedResult_Success(t *testing.T) {
	want := PetAnimalFeedResult{
		Result: PetAnimalResultSuccess, Lv: 2, Star: 10, Exp: 200000,
		ExpAdd: 19772, FeedType: PetAnimalFeedTypeCoin, FeedNum: 8, Hit: 2,
	}
	got, err := ParsePetAnimalFeedResult(BuildPetAnimalFeedResultPacket(want).Payload)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if *got != want {
		t.Fatalf("培养回包不幂等: want=%+v got=%+v", want, *got)
	}
}

func TestPetAnimalFeedResult_FailureOnlyResultCode(t *testing.T) {
	// 失败回包只含结果码，不应继续读取后续字段。
	got, err := ParsePetAnimalFeedResult(BuildPetAnimalFeedResultPacket(PetAnimalFeedResult{
		Result: PetAnimalResultNoEnoughCoin,
	}).Payload)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if got.Result != PetAnimalResultNoEnoughCoin {
		t.Fatalf("结果码不符: %+v", got)
	}
	if text := PetAnimalResultText(PetAnimalResultTooMuchExp); text != "经验已满，需先升阶" {
		t.Fatalf("结果码文本不符: %s", text)
	}
}

func TestPetAnimalBuilders(t *testing.T) {
	info := BuildPetAnimalInfoPacket(372164, 0)
	if info.ActionID != ActionIDPetAnimalInfo || !info.HasPrevAct {
		t.Fatalf("面板封包类型不符: %+v", info)
	}
	// Int32 player_id + Uint32 prev_act (默认 TownEnter)。
	if got := hex.EncodeToString(info.Payload); got != "0005adc400010000" {
		t.Fatalf("面板封包载荷不符: %s", got)
	}

	feed := BuildPetAnimalFeedPacket(PetAnimalFeedTypeCoin, 0x00300000)
	if feed.ActionID != ActionIDPetAnimalFeed || !feed.HasPrevAct {
		t.Fatalf("培养封包类型不符: %+v", feed)
	}
	if got := hex.EncodeToString(feed.Payload); got != "0000300000" {
		t.Fatalf("培养封包载荷不符: %s", got)
	}
	if feed.Payload[0] != PetAnimalFeedTypeCoin {
		t.Fatalf("培养方式必须为铜钱普通培养: %d", feed.Payload[0])
	}
}
