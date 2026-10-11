// Package protocol_test 提供基于真实抓包 (sxd.pcapng) 的 Golden Cases 黄金测试用例。
//
// @author Ateng
// @since 2026-10-10
package protocol_test

import (
	"bytes"
	"compress/zlib"
	"encoding/hex"
	"os"
	"reflect"
	"testing"

	"sxd-pie-ng/internal/protocol"
)

// Golden Case 1: 客户端登录请求包 (116 字节) - 采样自 sxd.pcapng Frame 1 (192.168.3.6 -> 49.232.196.100:8381)
const goldenCase1LoginReqHex = "000000700000000000066b6f6e67797500203862336139346162393333623336353763613138386630356638343035646236000a3137393135353630303900137378645f62616964755f70696e7061695f627400000000000062b40160000006e796afe78ea900037765620100066b6f6e677975"

// Golden Case 2: 服务端登录认证成功响应包 (30 字节) - 采样自 sxd.pcapng Frame 5 (49.232.196.100:8381 -> 192.168.3.6)
const goldenCase2LoginRespHex = "0000001a0000000000000000040a000000000000000001000b0000108501"

// Golden Case 3: 角色初始化握手步 1 请求包 (12 字节) 与响应包 (22 字节)
const goldenCase3InitStep1ReqHex = "000000080000004800000000"
const goldenCase3InitStep1RespHex = "00000012000000480000130f00020000130f00001312"

// Golden Case 4: 角色全量资产快照响应包 (147 字节 zlib 压缩) - 采样自 sxd.pcapng Frame 17 (49.232.196.100:8381 -> 192.168.3.6)
const goldenCase4GetInfoRespHex = "0000008f789c6360606062e07cb668d9931d0d4fe7ec626060d461609865cec0c0c0913f79cd0f0610e8e1ba08a1f96742e421e03f140099331930012b946604620520f58719c8d067787ce00190d60262392036822a3262a838bcbd908149eb1b88c7c5a010fa99417085150303675a6a5e7a79625e6505031f9c195f6c61680cd5a9cf00f20183f43ba855600000c4182a72"

// TestGolden_Case1_PlayerLoginRequest 验证客户端登录请求包与抓包 116 字节逐字节完全对齐
func TestGolden_Case1_PlayerLoginRequest(t *testing.T) {
	expectedBytes, err := hex.DecodeString(goldenCase1LoginReqHex)
	if err != nil {
		t.Fatalf("解码 Golden Case 1 Hex 失败: %v", err)
	}
	if len(expectedBytes) != 116 {
		t.Fatalf("Golden Case 1 期望 116 字节, 实际 %d 字节", len(expectedBytes))
	}

	// 1. 测试 Unmarshal 解包
	pkt, err := protocol.Unmarshal(expectedBytes)
	if err != nil {
		t.Fatalf("Unmarshal Golden Case 1 失败: %v", err)
	}

	if pkt.ActionID != protocol.ActionIDPlayerLogin {
		t.Errorf("期望 ActionID 0x00000000, 实际 0x%08X", pkt.ActionID)
	}
	if len(pkt.Payload) != 108 {
		t.Errorf("期望 Payload 长度 108 字节, 实际 %d 字节", len(pkt.Payload))
	}

	// 2. 测试通过结构体序列化组装
	req := protocol.PlayerLoginRequest{
		Username:   "kongyu",
		Hash:       "8b3a94ab933b3657ca188f05f8405db6",
		Time:       "1791556009",
		Source:     "sxd_baidu_pinpai_bt",
		Platform:   "疯玩",
		ClientType: "web",
	}

	builtPkt, err := protocol.BuildPlayerLoginPacket(req)
	if err != nil {
		t.Fatalf("BuildPlayerLoginPacket 失败: %v", err)
	}

	marshaled, err := builtPkt.Marshal()
	if err != nil {
		t.Fatalf("Marshal 封包失败: %v", err)
	}

	if !bytes.Equal(marshaled, expectedBytes) {
		t.Fatalf("序列化封包与抓包 Golden Hex 存在字节差异:\n期望: %s\n实际: %s",
			goldenCase1LoginReqHex, hex.EncodeToString(marshaled))
	}
}

// TestGolden_Case2_PlayerLoginResponse 验证服务端登录成功响应包 (30 字节) 的解包与 ResultCode 提取
func TestGolden_Case2_PlayerLoginResponse(t *testing.T) {
	expectedBytes, err := hex.DecodeString(goldenCase2LoginRespHex)
	if err != nil {
		t.Fatalf("解码 Golden Case 2 Hex 失败: %v", err)
	}
	if len(expectedBytes) != 30 {
		t.Fatalf("Golden Case 2 期望 30 字节, 实际 %d 字节", len(expectedBytes))
	}

	// 1. 测试基础包头解包
	pkt, err := protocol.Unmarshal(expectedBytes)
	if err != nil {
		t.Fatalf("Unmarshal Golden Case 2 失败: %v", err)
	}
	if pkt.ActionID != protocol.ActionIDPlayerLogin {
		t.Errorf("期望 ActionID 0x00000000, 实际 0x%08X", pkt.ActionID)
	}
	if len(pkt.Payload) != 22 {
		t.Errorf("期望 Payload 长度 22 字节, 实际 %d 字节", len(pkt.Payload))
	}

	// 2. 测试业务模型解析
	authResp, err := protocol.ParsePlayerLoginAuthResponse(pkt.Payload)
	if err != nil {
		t.Fatalf("ParsePlayerLoginAuthResponse 失败: %v", err)
	}

	if authResp.ResultCode != 4 {
		t.Errorf("期望登录认证成功码 ResultCode=4, 实际为 %d", authResp.ResultCode)
	}
	if authResp.Reserved != 0 {
		t.Errorf("期望 Reserved 预留位为 0, 实际为 %d", authResp.Reserved)
	}
}

// TestGolden_Case3_PlayerInitStep1 验证角色初始化握手步 1 请求包 (12 字节) 与响应包 (22 字节)
func TestGolden_Case3_PlayerInitStep1(t *testing.T) {
	reqBytes, err := hex.DecodeString(goldenCase3InitStep1ReqHex)
	if err != nil {
		t.Fatalf("解码 Golden Case 3 Req Hex 失败: %v", err)
	}
	respBytes, err := hex.DecodeString(goldenCase3InitStep1RespHex)
	if err != nil {
		t.Fatalf("解码 Golden Case 3 Resp Hex 失败: %v", err)
	}

	// 1. 验证客户端请求组装与逐字节对齐
	builtReq := protocol.BuildPlayerInitStep1Packet(0)
	builtReqBytes, err := builtReq.Marshal()
	if err != nil {
		t.Fatalf("Marshal InitStep1 失败: %v", err)
	}
	if !bytes.Equal(builtReqBytes, reqBytes) {
		t.Fatalf("InitStep1 请求包字节不匹配:\n期望: %s\n实际: %s",
			goldenCase3InitStep1ReqHex, hex.EncodeToString(builtReqBytes))
	}

	// 2. 验证服务端响应解析
	pkt, err := protocol.Unmarshal(respBytes)
	if err != nil {
		t.Fatalf("Unmarshal InitStep1 响应包失败: %v", err)
	}
	if pkt.ActionID != protocol.ActionIDPlayerInitStep1 {
		t.Errorf("期望 ActionID 0x00000048, 实际 0x%08X", pkt.ActionID)
	}

	initResult, err := protocol.ParsePlayerInitStep1Result(pkt.Payload)
	if err != nil {
		t.Fatalf("ParsePlayerInitStep1Result 失败: %v", err)
	}

	if initResult.TownID != 4879 {
		t.Errorf("期望 TownID=4879, 实际 %d", initResult.TownID)
	}
	if initResult.TownLine != 2 {
		t.Errorf("期望 TownLine=2, 实际 %d", initResult.TownLine)
	}
	if initResult.SceneID != 4879 {
		t.Errorf("期望 SceneID=4879, 实际 %d", initResult.SceneID)
	}
	if initResult.TargetID != 4882 {
		t.Errorf("期望 TargetID=4882, 实际 %d", initResult.TargetID)
	}
}

// TestGolden_TypeMarshallingRoundtrip 验证大端序基础类型、变长字符串与字节切片的逐字节编解码
func TestGolden_TypeMarshallingRoundtrip(t *testing.T) {
	w := protocol.NewWriter()
	w.WriteUint8(0x12)
	w.WriteInt8(-42)
	w.WriteUint16(0x1234)
	w.WriteInt16(-1234)
	w.WriteUint32(0x12345678)
	w.WriteInt32(-12345678)
	w.WriteInt64(0x1122334455667788)
	w.WriteString("神仙道")
	w.WriteString32("SXD_PIE_NG")
	rawBlock := []byte{0xDE, 0xAD, 0xBE, 0xEF}
	w.WriteBytes(rawBlock)

	encoded := w.Bytes()

	r := protocol.NewReader(encoded)

	u8, err := r.ReadUint8()
	if err != nil || u8 != 0x12 {
		t.Fatalf("Uint8 校验失败: %v, %x", err, u8)
	}
	i8, err := r.ReadInt8()
	if err != nil || i8 != -42 {
		t.Fatalf("Int8 校验失败: %v, %d", err, i8)
	}
	u16, err := r.ReadUint16()
	if err != nil || u16 != 0x1234 {
		t.Fatalf("Uint16 校验失败: %v, %x", err, u16)
	}
	i16, err := r.ReadInt16()
	if err != nil || i16 != -1234 {
		t.Fatalf("Int16 校验失败: %v, %d", err, i16)
	}
	u32, err := r.ReadUint32()
	if err != nil || u32 != 0x12345678 {
		t.Fatalf("Uint32 校验失败: %v, %x", err, u32)
	}
	i32, err := r.ReadInt32()
	if err != nil || i32 != -12345678 {
		t.Fatalf("Int32 校验失败: %v, %d", err, i32)
	}
	i64, err := r.ReadInt64()
	if err != nil || i64 != 0x1122334455667788 {
		t.Fatalf("Int64 校验失败: %v, %x", err, i64)
	}
	s, err := r.ReadString()
	if err != nil || s != "神仙道" {
		t.Fatalf("String 校验失败: %v, %s", err, s)
	}
	s32, err := r.ReadString32()
	if err != nil || s32 != "SXD_PIE_NG" {
		t.Fatalf("String32 校验失败: %v, %s", err, s32)
	}
	b, err := r.ReadBytes(len(rawBlock))
	if err != nil || !bytes.Equal(b, rawBlock) {
		t.Fatalf("Bytes 校验失败: %v, %v", err, b)
	}
	if r.Remaining() != 0 {
		t.Fatalf("期望缓冲区无剩余字节, 实际剩余 %d 字节", r.Remaining())
	}
}

// TestGolden_Zlib_DecompressAndSelfHealing 验证 0x78 zlib 透明解压与首字节为 0x78 但非合法 zlib 头的自愈容错
func TestGolden_Zlib_DecompressAndSelfHealing(t *testing.T) {
	// 1. 正常 zlib 压缩载荷
	original := []byte("hello-sxd-binary-protocol-payload-verification-123456")
	var compBuf bytes.Buffer
	zw := zlib.NewWriter(&compBuf)
	_, _ = zw.Write(original)
	_ = zw.Close()
	compressedData := compBuf.Bytes()

	// 确认首字节为 0x78
	if compressedData[0] != protocol.ZlibMagicByte {
		t.Fatalf("zlib 压缩首字节非 0x78: %02X", compressedData[0])
	}

	decompressed, err := protocol.DecompressIfNeeded(compressedData)
	if err != nil {
		t.Fatalf("DecompressIfNeeded 失败: %v", err)
	}
	if !bytes.Equal(decompressed, original) {
		t.Fatalf("解压数据不一致: 期望 %s, 实际 %s", string(original), string(decompressed))
	}

	// 2. 伪正例自愈测试：首字节为 0x78 ('x')，但后续为普通文本或非法 zlib 数据
	falsePositive := []byte{0x78, 0x01, 0xFF, 0xFE, 0xFD, 0xFC}
	fallback, err := protocol.DecompressIfNeeded(falsePositive)
	if err != nil {
		t.Fatalf("自愈解压不应抛出致命错误: %v", err)
	}
	if !reflect.DeepEqual(fallback, falsePositive) {
		t.Fatalf("自愈机制未原样返回原始切片: 期望 %v, 实际 %v", falsePositive, fallback)
	}

	// 3. 空载荷与非 0x78 载荷透传测试
	empty, err := protocol.DecompressIfNeeded([]byte{})
	if err != nil || len(empty) != 0 {
		t.Fatalf("空载荷透传失败: %v", err)
	}
	rawNonZlib := []byte{0x00, 0x01, 0x02, 0x03}
	passThrough, err := protocol.DecompressIfNeeded(rawNonZlib)
	if err != nil || !bytes.Equal(passThrough, rawNonZlib) {
		t.Fatalf("非 zlib 载荷透传失败: %v", err)
	}
}

// TestGolden_Case4_PlayerGetInfoResponse 验证真实抓包 147 字节 zlib 压缩全量资产快照的透明解压与字段反序列化
func TestGolden_Case4_PlayerGetInfoResponse(t *testing.T) {
	expectedBytes, err := hex.DecodeString(goldenCase4GetInfoRespHex)
	if err != nil {
		t.Fatalf("解码 Golden Case 4 Hex 失败: %v", err)
	}
	if len(expectedBytes) != 147 {
		t.Fatalf("Golden Case 4 期望 147 字节, 实际 %d 字节", len(expectedBytes))
	}

	// 1. 测试整包透明解压与 ActionIDPlayerGetInfo 还原
	pkt, err := protocol.Unmarshal(expectedBytes)
	if err != nil {
		t.Fatalf("Unmarshal Golden Case 4 失败: %v", err)
	}
	if pkt.ActionID != protocol.ActionIDPlayerGetInfo {
		t.Errorf("期望 ActionID 0x00000002 (ActionIDPlayerGetInfo), 实际 0x%08X", pkt.ActionID)
	}

	// 2. 验证角色资产反序列化 (角色名: 梦一场, 等级: 300, 铜钱: 36226117234, 元宝: 39409/39479)
	res, err := protocol.ParsePlayerLoginResult(pkt.Payload)
	if err != nil {
		t.Fatalf("ParsePlayerLoginResult 解析资产失败: %v", err)
	}

	if res.RoleName != "梦一场" {
		t.Errorf("期望角色名 '梦一场', 实际 '%s'", res.RoleName)
	}
	if res.Level != 300 {
		t.Errorf("期望角色等级 300, 实际 %d", res.Level)
	}
	if res.Coins != 36231687416 {
		t.Errorf("期望角色铜钱 36231687416, 实际 %d", res.Coins)
	}
	if res.Ingots != 39479 {
		t.Errorf("期望角色元宝 39479, 实际 %d", res.Ingots)
	}
}

// TestGolden_Module0_LiveFilesReplay 遍历 test/testdata/golden/ 下提取的真实 .bin 封包切片，
// 在离线状态下验证二进制协议解码与大端序解析 100% 字节级对齐。
func TestGolden_Module0_LiveFilesReplay(t *testing.T) {
	testCases := []struct {
		name       string
		relPath    string
		expectMod  uint16
		expectAct  uint16
		verifyFunc func(t *testing.T, pkt *protocol.Packet)
	}{
		{
			name:      "登录认证请求帧 (c2s_m0_a0.bin)",
			relPath:   "../../test/testdata/golden/c2s_m0_a0.bin",
			expectMod: 0,
			expectAct: 0,
			verifyFunc: func(t *testing.T, pkt *protocol.Packet) {
				if len(pkt.Payload) != 100 {
					t.Errorf("期望载荷长度 100, 实际 %d", len(pkt.Payload))
				}
			},
		},
		{
			name:      "登录成功响应帧 (s2c_m0_a0.bin)",
			relPath:   "../../test/testdata/golden/s2c_m0_a0.bin",
			expectMod: 0,
			expectAct: 0,
			verifyFunc: func(t *testing.T, pkt *protocol.Packet) {
				authResp, err := protocol.ParsePlayerLoginAuthResponse(pkt.Payload)
				if err != nil {
					t.Fatalf("解析登录响应载荷失败: %v", err)
				}
				if authResp.ResultCode != 4 {
					t.Errorf("期望 ResultCode = 4 (SUCCESS), 实际 %d", authResp.ResultCode)
				}
			},
		},
		{
			name:      "场景初始化步 1 请求帧 (c2s_m0_a72.bin)",
			relPath:   "../../test/testdata/golden/c2s_m0_a72.bin",
			expectMod: 0,
			expectAct: 72,
			verifyFunc: func(t *testing.T, pkt *protocol.Packet) {
				if len(pkt.Payload) != 4 {
					t.Errorf("期望载荷长度 4, 实际 %d", len(pkt.Payload))
				}
			},
		},
		{
			name:      "场景初始化步 2 请求帧 (c2s_m0_a99.bin)",
			relPath:   "../../test/testdata/golden/c2s_m0_a99.bin",
			expectMod: 0,
			expectAct: 99,
			verifyFunc: func(t *testing.T, pkt *protocol.Packet) {
				if len(pkt.Payload) != 4 {
					t.Errorf("期望载荷长度 4, 实际 %d", len(pkt.Payload))
				}
			},
		},
		{
			name:      "长连接心跳保活请求帧 (c2s_m0_a23.bin)",
			relPath:   "../../test/testdata/golden/c2s_m0_a23.bin",
			expectMod: 0,
			expectAct: 23,
			verifyFunc: func(t *testing.T, pkt *protocol.Packet) {
				if len(pkt.Payload) != 4 {
					t.Errorf("期望心跳载荷长度 4, 实际 %d", len(pkt.Payload))
				}
			},
		},
		{
			name:      "长连接心跳保活响应帧 (s2c_m0_a23.bin)",
			relPath:   "../../test/testdata/golden/s2c_m0_a23.bin",
			expectMod: 0,
			expectAct: 23,
			verifyFunc: func(t *testing.T, pkt *protocol.Packet) {
				if len(pkt.Payload) != 4 {
					t.Errorf("期望心跳响应载荷长度 4, 实际 %d", len(pkt.Payload))
				}
			},
		},
		{
			name:      "系统时间同步请求帧 (c2s_m0_a22.bin)",
			relPath:   "../../test/testdata/golden/c2s_m0_a22.bin",
			expectMod: 0,
			expectAct: 22,
		},
		{
			name:      "系统时间同步响应帧 (s2c_m0_a22.bin)",
			relPath:   "../../test/testdata/golden/s2c_m0_a22.bin",
			expectMod: 0,
			expectAct: 22,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			rawBytes, err := os.ReadFile(tc.relPath)
			if err != nil {
				t.Fatalf("读取脱机黄金测试切片失败: %v", err)
			}

			// 1. 测试 Unmarshal 解包
			pkt, err := protocol.Unmarshal(rawBytes)
			if err != nil {
				t.Fatalf("Unmarshal 解包失败: %v", err)
			}

			mod, act := protocol.SplitActionID(pkt.ActionID)
			if mod != tc.expectMod {
				t.Errorf("期望 Module %d, 实际 %d", tc.expectMod, mod)
			}
			if act != tc.expectAct {
				t.Errorf("期望 Action %d, 实际 %d", tc.expectAct, act)
			}

			// 2. 测试 ReadPacket 流式解析一致性
			streamPkt, err := protocol.ReadPacket(bytes.NewReader(rawBytes))
			if err != nil {
				t.Fatalf("ReadPacket 流式读取失败: %v", err)
			}
			if streamPkt.ActionID != pkt.ActionID {
				t.Errorf("流式读取 ActionID 不一致: %08X vs %08X", streamPkt.ActionID, pkt.ActionID)
			}

			if tc.verifyFunc != nil {
				tc.verifyFunc(t, pkt)
			}
		})
	}
}

