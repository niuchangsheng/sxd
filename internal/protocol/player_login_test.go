// Package protocol 提供神仙道客户端与服务器之间的二进制协议序列化、反序列化、大端序编解码与透明 zlib 流解压缩引擎。
//
// @author Ateng
// @since 2026-10-09
package protocol_test

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"encoding/hex"
	"testing"

	"sxd-pie-ng/internal/protocol"
)

func TestPlayerLogin_BuildAndParse(t *testing.T) {
	req := protocol.PlayerLoginRequest{
		Username:   "kongyu",
		Hash:       "4fea7a9e71b3f4da3be19eefa519b685",
		Time:       "1791503911",
		Source:     "sxd_baidu_pinpai_bt",
		Platform:   "疯玩",
		ClientType: "web",
	}

	pkt, err := protocol.BuildPlayerLoginPacket(req)
	if err != nil {
		t.Fatalf("BuildPlayerLoginPacket 失败: %v", err)
	}

	if pkt.ActionID != protocol.ActionIDPlayerLogin {
		t.Errorf("期望 ActionID 0x%04X, 实际 0x%04X", protocol.ActionIDPlayerLogin, pkt.ActionID)
	}

	// 模拟构造服务端回包 (未压缩格式)
	var rawBuf bytes.Buffer
	binary.Write(&rawBuf, binary.BigEndian, int16(0)) // Result
	binary.Write(&rawBuf, binary.BigEndian, int16(2)) // RoleID
	// RoleName
	name := "梦一场"
	binary.Write(&rawBuf, binary.BigEndian, uint16(len(name)))
	rawBuf.WriteString(name)
	// Level, Ingots, Coins
	binary.Write(&rawBuf, binary.BigEndian, int32(300))
	binary.Write(&rawBuf, binary.BigEndian, int32(39409))
	binary.Write(&rawBuf, binary.BigEndian, int64(36200000000))
	// 16 字节占位
	rawBuf.Write(make([]byte, 16))
	// Stamina
	binary.Write(&rawBuf, binary.BigEndian, int32(201))

	// 1. 测试未压缩直接解析
	res, err := protocol.ParsePlayerLoginResult(rawBuf.Bytes())
	if err != nil {
		t.Fatalf("ParsePlayerLoginResult 未压缩解析失败: %v", err)
	}
	if res.RoleName != "梦一场" {
		t.Errorf("期望角色名为 '梦一场', 实际为 %s", res.RoleName)
	}
	if res.Level != 300 {
		t.Errorf("期望等级为 300, 实际为 %d", res.Level)
	}
	if res.Stamina != 201 {
		t.Errorf("期望体力为 201, 实际为 %d", res.Stamina)
	}
	if res.MaxStamina != 300 {
		t.Errorf("期望最大体力为 300, 实际为 %d", res.MaxStamina)
	}
	if res.Ingots != 39409 {
		t.Errorf("期望元宝为 39409, 实际为 %d", res.Ingots)
	}

	// 2. 测试 zlib 压缩流自愈解压与解析
	var compBuf bytes.Buffer
	zw := zlib.NewWriter(&compBuf)
	_, _ = zw.Write(rawBuf.Bytes())
	_ = zw.Close()

	resComp, err := protocol.ParsePlayerLoginResult(compBuf.Bytes())
	if err != nil {
		t.Fatalf("ParsePlayerLoginResult 压缩流解析失败: %v", err)
	}
	if resComp.Stamina != 201 || resComp.Ingots != 39409 {
		t.Errorf("压缩解析数据不匹配: %+v", resComp)
	}
}

func TestPlayerLogin_LivePcapWholeCompressedPacket(t *testing.T) {
	// 真实抓包 EPB #357 封包载荷 (138 字节)
	rawHex := "00000086789c6360606062e07cb668d9931d0d4fe7ec626060d4616098f99181818123df6e5d110308f4705d44a24f3240c17f280032673260025628cd08c40a40ea0f3390a1cff0f8c00320ad05953482d31587b7173230697d03f1b81814426e3108aeb06260e04c4bcd4b2f4fccabac60e08333e38b2d0c8d91ac626260907e07b50a0c0022f02aba"
	data, err := hex.DecodeString(rawHex)
	if err != nil {
		t.Fatalf("hex decode failed: %v", err)
	}

	// 1. 测试 protocol.Unmarshal 能透明识别并整包解压缩
	pkt, err := protocol.Unmarshal(data)
	if err != nil {
		t.Fatalf("Unmarshal live packet failed: %v", err)
	}
	if pkt.ActionID != protocol.ActionIDPlayerGetInfo {
		t.Errorf("期望 ActionID 0x0002, 实际 0x%04X", pkt.ActionID)
	}

	// 2. 测试 protocol.ParsePlayerLoginResult 提取出的角色资产
	res, err := protocol.ParsePlayerLoginResult(pkt.Payload)
	if err != nil {
		t.Fatalf("ParsePlayerLoginResult failed: %v", err)
	}

	if res.RoleName != "梦一场" {
		t.Errorf("期望角色名 '梦一场', 实际: %s", res.RoleName)
	}
	if res.Level != 300 {
		t.Errorf("期望等级 300, 实际: %d", res.Level)
	}
	if res.Stamina != 201 {
		t.Errorf("期望体力 201, 实际: %d", res.Stamina)
	}
	if res.MaxStamina != 300 {
		t.Errorf("期望最大体力 300, 实际: %d", res.MaxStamina)
	}
	if res.Ingots != 39409 {
		t.Errorf("期望元宝 39409, 实际: %d", res.Ingots)
	}
	if res.Coins != 36226117234 {
		t.Errorf("期望铜钱 36226117234, 实际: %d", res.Coins)
	}

	// 3. 测试 protocol.ReadPacket 从流中读取
	streamPkt, err := protocol.ReadPacket(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("ReadPacket live packet failed: %v", err)
	}
	if streamPkt.ActionID != protocol.ActionIDPlayerGetInfo {
		t.Errorf("期望 ActionID 0x0002, 实际 0x%04X", streamPkt.ActionID)
	}
}

func TestPlayerLogin_HeartbeatAndServerTime(t *testing.T) {
	// 测试心跳包构造
	seq := int32(0x00000030)
	hbPkt := protocol.BuildPlayerHeartbeatPacket(seq)
	if hbPkt.ActionID != protocol.ActionIDPlayerHeartbeat {
		t.Fatalf("期望 ActionID 0x%04X, 实际 0x%04X", protocol.ActionIDPlayerHeartbeat, hbPkt.ActionID)
	}
	hbBytes, err := hbPkt.Marshal()
	if err != nil {
		t.Fatalf("Marshal 心跳包失败: %v", err)
	}
	// 期望长度 4 (header) + 4 (module/action) + 4 (payload) = 12 字节
	if len(hbBytes) != 12 {
		t.Errorf("心跳包长度期望 12 字节, 实际 %d 字节", len(hbBytes))
	}

	// 测试同步系统时间包构造
	stPkt := protocol.BuildPlayerServerTimePacket(0)
	if stPkt.ActionID != protocol.ActionIDPlayerServerTime {
		t.Fatalf("期望 ActionID 0x%04X, 实际 0x%04X", protocol.ActionIDPlayerServerTime, stPkt.ActionID)
	}
}

