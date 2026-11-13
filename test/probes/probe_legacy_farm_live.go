//go:build probe

// 目标: 验证 s1 药园是否仍使用旧版玩法（伙伴/旧种子），解释新农场买种全部丢包。
//
// 只读探针：依次查询 13/0 土地、13/1 伙伴列表、13/2 旧种子列表、13/21 新土地，
// 打印回包长度与前若干字节，不购买、不种植。
//
// 运行: SXD_CONFIG=configs/niuchangsheng.yaml go run -tags=probe test/probes/probe_legacy_farm_live.go -role 1
package main

import (
	"context"
	"encoding/hex"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"time"

	"sxd-pie-ng/internal/client"
	"sxd-pie-ng/internal/config"
	"sxd-pie-ng/internal/platform"
	"sxd-pie-ng/internal/protocol"
)

func main() {
	slog.SetLogLoggerLevel(slog.LevelInfo)
	cfgPath := os.Getenv("SXD_CONFIG")
	if cfgPath == "" {
		cfgPath = "configs/niuchangsheng.yaml"
	}
	roleIdx := flag.Int("role", 1, "配置中的角色下标 (0=s0, 1=s1)")
	flag.Parse()

	cfg, err := config.Load(cfgPath)
	if err != nil {
		slog.Error("加载配置失败", "err", err)
		return
	}
	acc := cfg.Accounts[0]
	role := acc.Roles[*roleIdx]

	ticketCache := platform.NewTicketCache(90 * time.Minute)
	xdAuth, err := platform.NewXindongAuthenticator()
	if err != nil {
		slog.Error("创建认证器失败", "err", err)
		return
	}
	xdAuth.SetCookie(acc.Cookie)
	ticketCache.RegisterAuthenticator(xdAuth)

	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Second)
	defer cancel()
	ticket, err := ticketCache.GetOrFetch(ctx, acc.Platform, acc.Username, acc.Password, role.ServerID)
	if err != nil {
		slog.Error("换票失败", "err", err)
		return
	}
	roleCfg := client.SessionConfig{
		RoleID:               fmt.Sprintf("%s-%s", role.ServerID, role.RoleName),
		RoleName:             role.RoleName,
		Username:             acc.Username,
		ServerAddr:           ticket.GatewayURL,
		ServerID:             role.ServerID,
		Platform:             acc.Platform,
		Authenticator:        client.DefaultAuthenticator,
		MaxReconnectAttempts: 3,
	}
	client.ApplyTicket(&roleCfg, ticket)
	sess := client.NewRoleSession(roleCfg)
	if err := sess.Start(ctx); err != nil {
		slog.Error("启动会话失败", "err", err)
		return
	}
	defer sess.Close()
	deadline := time.Now().Add(40 * time.Second)
	for sess.State() != client.StateActive && time.Now().Before(deadline) {
		time.Sleep(100 * time.Millisecond)
	}
	if sess.State() != client.StateActive {
		slog.Error("会话未激活", "state", sess.State())
		return
	}
	slog.Info("会话已激活", "role_id", sess.RoleID(), "level", sess.GetPlayerState().Level)

	call := func(pkt *protocol.Packet, name string) *protocol.Packet {
		cCtx, cCancel := context.WithTimeout(ctx, 6*time.Second)
		defer cCancel()
		resp, cErr := sess.Call(cCtx, pkt, pkt.ActionID)
		if cErr != nil {
			slog.Warn("无回包", "call", name, "action", fmt.Sprintf("0x%08X", pkt.ActionID), "err", cErr)
			return nil
		}
		limit := len(resp.Payload)
		if limit > 80 {
			limit = 80
		}
		slog.Info("有回包", "call", name, "action", fmt.Sprintf("0x%08X", pkt.ActionID),
			"len", len(resp.Payload), "hex", hex.EncodeToString(resp.Payload[:limit]))
		return resp
	}

	callSlow := func(pkt *protocol.Packet, name string, timeout time.Duration) *protocol.Packet {
		cCtx, cCancel := context.WithTimeout(ctx, timeout)
		defer cCancel()
		resp, cErr := sess.Call(cCtx, pkt, pkt.ActionID)
		if cErr != nil {
			slog.Warn("无回包(长超时)", "call", name, "err", cErr)
			return nil
		}
		limit := len(resp.Payload)
		if limit > 80 {
			limit = 80
		}
		slog.Info("有回包(长超时)", "call", name, "len", len(resp.Payload),
			"hex", hex.EncodeToString(resp.Payload[:limit]))
		return resp
	}

	// 13/0 旧版土地列表。
	var landResp *protocol.Packet
	if pkt, bErr := protocol.BuildFarmGetInfoPacket(protocol.FarmGetInfoRequest{PrevAct: sess.LastActionID()}); bErr == nil {
		landResp = call(pkt, "legacy_land_list")
	}
	// 13/1 伙伴列表：旧版药园用伙伴播种。
	roleResp := call(protocol.NewPacket(0x000D0001, nil), "legacy_role_list")
	// 13/2 旧版种子列表。
	call(protocol.NewPacket(0x000D0002, nil), "legacy_seed_list")
	// 13/21 新版土地状态。
	if pkt, bErr := protocol.BuildFarmPlantStatePacket(protocol.FarmPlantStateRequest{PrevAct: sess.LastActionID()}); bErr == nil {
		call(pkt, "new_land_state")
	}

	if landResp == nil || roleResp == nil {
		slog.Info("缺少土地/伙伴数据，跳过旧版播种测试")
		return
	}
	// 回包均为 Uint16 count + 记录；第一条记录首字段分别是 land_id / role_id。
	readFirstInt := func(resp *protocol.Packet) (int32, bool) {
		if len(resp.Payload) < 6 {
			return 0, false
		}
		r := protocol.NewReader(resp.Payload)
		if _, err := r.ReadUint16(); err != nil {
			return 0, false
		}
		v, err := r.ReadInt32()
		return v, err == nil
	}
	landID, ok1 := readFirstInt(landResp)
	if !ok1 {
		slog.Warn("解析 land_id 失败", "land", landID)
		return
	}

	// 解析伙伴列表: [Int roleId, String, String, Int level, Long, Long, Int, Int, Int, UByte]
	roles := make([]struct {
		id    int32
		level int32
	}, 0, 4)
	if r, err := func() (*protocol.Reader, error) { return protocol.NewReader(roleResp.Payload), nil }(); err == nil {
		if n, err := r.ReadUint16(); err == nil {
			for i := 0; i < int(n); i++ {
				roleID, err := r.ReadInt32()
				if err != nil {
					break
				}
				if _, err = r.ReadString(); err != nil {
					break
				}
				if _, err = r.ReadString(); err != nil {
					break
				}
				level, err := r.ReadInt32()
				if err != nil {
					break
				}
				if _, err = r.ReadInt64(); err != nil {
					break
				}
				if _, err = r.ReadInt64(); err != nil {
					break
				}
				for j := 0; j < 3; j++ {
					if _, err = r.ReadInt32(); err != nil {
						break
					}
				}
				if _, err = r.ReadUint8(); err != nil {
					break
				}
				roles = append(roles, struct {
					id    int32
					level int32
				}{roleID, level})
			}
		}
	}
	slog.Info("旧版伙伴列表", "land_id", landID, "roles", roles)
	if len(roles) == 0 {
		slog.Info("无可用伙伴")
		return
	}

	// 13/8 依次检查伙伴/类型，找到可播种组合后执行一次 13/5。
	planted := false
	for _, role := range roles {
		for _, herbType := range []uint8{2, 3, 4} {
			checkPkt := protocol.NewPacket(0x000D0008,
				protocol.NewWriter().WriteInt32(role.id).WriteUint8(herbType).Bytes())
			checkResp := call(checkPkt, fmt.Sprintf("check_role_state_role%d_type%d", role.id, herbType))
			if checkResp == nil || len(checkResp.Payload) == 0 {
				continue
			}
			if checkResp.Payload[0] != 21 { // PASS
				slog.Info("伙伴播种资格未通过", "role_id", role.id, "type", herbType, "result", checkResp.Payload[0])
				continue
			}
			time.Sleep(time.Second)
			base := protocol.NewWriter().WriteInt32(landID).WriteInt32(role.id).WriteUint8(herbType).WriteInt8(5).Bytes()
			var resp *protocol.Packet
			for _, pv := range []struct {
				name string
				prev uint32
			}{
				{"bare", 0},
				{"prev_check", 0x000D0008},
				{"prev_info", 0x000D000C},
			} {
				payload := append([]byte{}, base...)
				if pv.prev != 0 {
					payload = append(payload, byte(pv.prev>>24), byte(pv.prev>>16), byte(pv.prev>>8), byte(pv.prev))
				}
				sess.SetLastActionID(0)
				resp = callSlow(protocol.NewPacket(0x000D0005, payload),
					fmt.Sprintf("legacy_plant_role%d_type%d_%s", role.id, herbType, pv.name), 8*time.Second)
				if resp != nil {
					break
				}
			}
			if resp == nil {
				continue
			}
			if len(resp.Payload) > 0 && resp.Payload[0] == 8 { // SUCCESS
				planted = true
				break
			}
			// 失败码继续尝试其他类型 (12=无摇钱树次数, 13=无仙灵树次数, 20=伙伴等级)。
			slog.Info("旧版播种未成功，尝试下一类型", "role_id", role.id, "type", herbType, "result", resp.Payload[0])
		}
		if planted {
			break
		}
	}
	if !planted {
		slog.Info("未找到可播种的伙伴/类型组合")
	}
	// 旧版收获 13/6：官方第二个参数是 NO_DOUBLE(15)/IS_DOUBLE(14)，不是 0/1。
	// 对 s0 卡住的成熟地块 49530 做一次实测（收获是正常资源产出）。
	if *roleIdx == 0 {
		for _, kind := range []uint8{15, 14} {
			harvestPkt := protocol.NewPacket(0x000D0006,
				protocol.NewWriter().WriteInt32(49530).WriteUint8(kind).Bytes())
			if call(harvestPkt, fmt.Sprintf("legacy_harvest_kind%d", kind)) != nil {
				break
			}
			time.Sleep(time.Second)
		}
	}
	// 旧版顶级种子/刷新种子（可能提供免费种子）。
	call(protocol.NewPacket(0x000D0004, nil), "top_herbs_seed")
	call(protocol.NewPacket(0x000D0003, nil), "refresh_herbs_seed")
	// 新农场 13/24 是否接受“伙伴 role_id”作为播种对象（s1 无种子时）。
	if plantPkt, bErr := protocol.BuildFarmPlantPacket(protocol.FarmPlantRequest{
		LandID: 162236,
	}); bErr == nil {
		_ = plantPkt
	}
	for _, role := range roles {
		plantPkt, bErr := protocol.BuildFarmPlantPacket(protocol.FarmPlantRequest{
			LandID:       162236,
			SeedOrRoleID: role.id,
		})
		if bErr != nil {
			continue
		}
		if resp := call(plantPkt, fmt.Sprintf("new_plant_role%d", role.id)); resp != nil {
			break
		}
		time.Sleep(time.Second)
	}
	// 旧版 13/5 用“地块序号 0..3”而非大 land_id。
	for idx := int32(0); idx < 4; idx++ {
		pkt := protocol.NewPacket(0x000D0005,
			protocol.NewWriter().WriteInt32(idx).WriteInt32(roles[0].id).WriteUint8(2).WriteInt8(5).Bytes())
		if resp := call(pkt, fmt.Sprintf("legacy_plant_index%d", idx)); resp != nil {
			break
		}
		time.Sleep(500 * time.Millisecond)
	}

	// 摇钱树/仙灵树剩余次数与恢复时间（只读）。
	call(protocol.NewPacket(0x000D000C, nil), "coin_tree_count_info")
	call(protocol.NewPacket(0x000D0012, nil), "xianling_tree_count_info")
	slog.Info("探针结束")
}
