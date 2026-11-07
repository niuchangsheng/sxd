// Package protocol 提供法力星图协议编解码测试。
//
// @author Ateng
// @since 2026-11-07
package protocol

import (
	"encoding/hex"
	"testing"
)

func TestStarPicture_InfoRoundTrip(t *testing.T) {
	info := StarPictureInfo{
		HasDivination:   0,
		FreeDivination:  2,
		IngotDivination: 2,
		ChipCount:       30,
		ResetCount:      1,
		CurDeployLevel:  3,
	}
	pkt := BuildStarPictureInfoResultPacket(info)
	parsed, err := ParseStarPictureInfoResult(pkt.Payload)
	if err != nil {
		t.Fatalf("ParseStarPictureInfoResult 失败: %v", err)
	}
	if *parsed != info {
		t.Fatalf("面板往返不符: got=%+v want=%+v", parsed, info)
	}

	if _, err := ParseStarPictureInfoResult(nil); err == nil {
		t.Fatal("空载荷应返回错误")
	}
	if _, err := ParseStarPictureInfoResult(make([]byte, 12)); err == nil {
		t.Fatal("过短载荷应返回错误")
	}
}

func TestStarPicture_RequestsAndDivinationResult(t *testing.T) {
	infoPkt, err := BuildStarPictureInfoPacket(0)
	if err != nil {
		t.Fatalf("BuildStarPictureInfoPacket 失败: %v", err)
	}
	if infoPkt.ActionID != ActionIDStarPictureInfo || !infoPkt.HasPrevAct {
		t.Fatalf("面板请求封包不符: %+v", infoPkt)
	}

	divPkt, err := BuildStarPictureDivinationPacket(0)
	if err != nil {
		t.Fatalf("BuildStarPictureDivinationPacket 失败: %v", err)
	}
	if divPkt.ActionID != ActionIDStarPictureDivination || !divPkt.HasPrevAct {
		t.Fatalf("占星请求封包不符: %+v", divPkt)
	}
	// 缺省 PrevAct 为面板查询动作 (0x01E50000)
	if got := hex.EncodeToString(divPkt.Payload); got != "01e50000" {
		t.Fatalf("占星请求载荷不符: %s", got)
	}

	// 成功
	ok, err := ParseStarPictureDivinationResult([]byte{StarPictureResultSuccess})
	if err != nil || ok != StarPictureResultSuccess {
		t.Fatalf("成功结果解析不符: code=%d err=%v", ok, err)
	}
	// 失败码透传，调用方据此熔断
	fail, err := ParseStarPictureDivinationResult([]byte{7})
	if err != nil || fail != 7 {
		t.Fatalf("失败结果解析不符: code=%d err=%v", fail, err)
	}
	if _, err := ParseStarPictureDivinationResult(nil); err == nil {
		t.Fatal("空载荷应返回错误")
	}
}