// Package protocol_test 验证跨服/网页登录协议封包编解码与回包解析。
//
// @author Ateng
// @since 2026-10-08
package protocol_test

import (
	"encoding/hex"
	"testing"

	"sxd-pie-ng/internal/protocol"
)

func TestStLogin_Roundtrip(t *testing.T) {
	req := protocol.StLoginRequest{
		ServerName:     "fengwanyx_s813",
		PlayerID:       65536,
		OriginNickName: "梦一场.s813",
		Time:           1791469000,
		PassCode:       "5f4dcc3b5aa765d61d8327deb882cf99",
	}

	pkt, err := protocol.BuildStLoginPacket(req)
	if err != nil {
		t.Fatalf("BuildStLoginPacket failed: %v", err)
	}

	if pkt.ActionID != protocol.ActionIDStLogin {
		t.Errorf("expected ActionID 0x%04X, got 0x%04X", protocol.ActionIDStLogin, pkt.ActionID)
	}

	parsed, err := protocol.ParseStLoginRequest(pkt.Payload)
	if err != nil {
		t.Fatalf("ParseStLoginRequest failed: %v", err)
	}

	if *parsed != req {
		t.Errorf("expected %+v, got %+v", req, *parsed)
	}

	// 验证与官方 AS3 线格式逐字节对齐：
	// [writeUTF serverName][Int playerId][writeUTF originNickName][Int time][writeUTF passCode]
	// 官方 `com.Protocol.writeArray` 对 StringUtil 使用 ByteArray.writeUTF，
	// 长度前缀为 2 字节，而非 4 字节。
	const wantPayloadHex = "000e66656e6777616e79785f7338313300010000" +
		"000ee6a2a6e4b880e59cba2e73383133" +
		"6ac7a5c8" +
		"00203566346463633362356161373635643631643833323764656238383263663939"
	if got := hex.EncodeToString(pkt.Payload); got != wantPayloadHex {
		t.Errorf("wire payload mismatch:\n got=%s\nwant=%s", got, wantPayloadHex)
	}

	// 验证登录响应封包 Roundtrip
	res := protocol.StLoginResult{
		Result:     0,
		PlayerID:   65536,
		ServerTime: 1791469358,
	}

	resPkt, err := protocol.BuildStLoginResultPacket(res)
	if err != nil {
		t.Fatalf("BuildStLoginResultPacket failed: %v", err)
	}

	parsedRes, err := protocol.ParseStLoginResult(resPkt.Payload)
	if err != nil {
		t.Fatalf("ParseStLoginResult failed: %v", err)
	}

	if *parsedRes != res {
		t.Errorf("expected %+v, got %+v", res, *parsedRes)
	}
}
