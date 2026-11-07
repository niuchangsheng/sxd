//go:build probe

// 目标协议: 摘仙桃 (Mod_GetPeach_Base, Module 40) 与 叶公好龙培养
// (Mod_PetAnimal_Base, Module 48) 只读面板侦察。
//
// 本探针仅执行只读查询，不召唤、不培养、不消耗任何资源:
//
//	check_function_open(56/60)            功能开放状态
//	peach_info(40/3)                      摘仙桃面板 4 字节
//	player_info(40/4)                     仙桃玩家信息 [5B, Int coin_times, Int need_ingot]
//	pet_animal_info(48/0, [player_id])    叶公好龙面板 [String name, Byte, Byte, Int, Int]
//
// 运行命令: SXD_CONFIG=configs/s1.yaml go run -tags=probe test/probes/probe_peach_pet_animal_info_live.go
package main

import (
	"context"
	"encoding/hex"
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
		cfgPath = "configs/s1.yaml"
	}
	cfg, err := config.Load(cfgPath)
	if err != nil {
		slog.Error("加载配置失败", "path", cfgPath, "err", err)
		return
	}
	acc := cfg.Accounts[0]
	role := acc.Roles[0]

	ticketCache := platform.NewTicketCache(90 * time.Minute)
	xdAuth, err := platform.NewXindongAuthenticator()
	if err != nil {
		slog.Error("创建认证器失败", "err", err)
		return
	}
	xdAuth.SetCookie(acc.Cookie)
	ticketCache.RegisterAuthenticator(xdAuth)

	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
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
		slog.Error("启动主服会话失败", "err", err)
		return
	}
	defer sess.Close()
	deadline := time.Now().Add(40 * time.Second)
	for sess.State() != client.StateActive && time.Now().Before(deadline) {
		time.Sleep(100 * time.Millisecond)
	}
	if sess.State() != client.StateActive {
		slog.Error("主服会话未激活", "state", sess.State())
		return
	}

	call := func(pkt *protocol.Packet, act uint32, timeout time.Duration) *protocol.Packet {
		cCtx, cCancel := context.WithTimeout(ctx, timeout)
		defer cCancel()
		resp, cErr := sess.Call(cCtx, pkt, act)
		if cErr != nil {
			slog.Warn("调用无回包", "action", fmt.Sprintf("0x%08X", act), "err", cErr)
			return nil
		}
		return resp
	}

	dump := func(label string, resp *protocol.Packet) {
		if resp == nil {
			return
		}
		slog.Info(label, "len", len(resp.Payload), "hex", hex.EncodeToString(resp.Payload))
	}

	for _, fid := range []int32{56, 60} {
		resp := call(protocol.BuildCheckFunctionOpenPacket(fid, sess.LastActionID()), protocol.ActionIDPlayerCheckFunctionOpen, 6*time.Second)
		if resp != nil && len(resp.Payload) > 0 {
			slog.Info("功能开放", "function_id", fid, "flag", resp.Payload[0])
		}
	}

	infoPkt := protocol.NewPacket(0x00280003, []byte{0, 0, 0, 0})
	infoPkt.HasPrevAct = true
	if resp := call(infoPkt, 0x00280003, 6*time.Second); resp != nil {
		dump("摘仙桃 peach_info", resp)
	}

	playerPkt := protocol.NewPacket(0x00280004, []byte{0, 0, 0, 0})
	playerPkt.HasPrevAct = true
	if resp := call(playerPkt, 0x00280004, 6*time.Second); resp != nil {
		dump("摘仙桃 player_info", resp)
		r := protocol.NewReader(resp.Payload)
		for i := 0; i < 5; i++ {
			v, rErr := r.ReadUint8()
			slog.Info("player_info byte", "idx", i, "val", v, "err", rErr)
		}
		coin, _ := r.ReadInt32()
		ingot, _ := r.ReadInt32()
		slog.Info("player_info 尾字段", "call_coin_times", coin, "call_need_ingot", ingot)
	}

	playerID := sess.PlayerID()
	fetchPet := func() {
		w := protocol.NewWriter()
		w.WriteInt32(playerID)
		w.WriteUint32(sess.LastActionID())
		pkt := protocol.NewPacket(0x00300000, w.Bytes())
		pkt.HasPrevAct = true
		resp := call(pkt, 0x00300000, 8*time.Second)
		if resp == nil {
			return
		}
		name, rErr := protocol.NewReader(resp.Payload).ReadString()
		slog.Info("叶公好龙 pet_animal_info", "len", len(resp.Payload), "hex", hex.EncodeToString(resp.Payload),
			"name", name, "name_err", rErr, "player_id", playerID)
		r := protocol.NewReader(resp.Payload)
		_, _ = r.ReadString()
		for i := 0; i < 2; i++ {
			v, rErr2 := r.ReadUint8()
			slog.Info("pet_animal_info byte", "idx", i, "val", v, "err", rErr2)
		}
		for i := 0; i < 2; i++ {
			v, rErr2 := r.ReadInt32()
			slog.Info("pet_animal_info int", "idx", i, "val", v, "err", rErr2)
		}
	}
	fetchPet()

	if os.Getenv("SXD_PROBE_APPLY") == "1" {
		before := sess.GetPlayerState()
		fw := protocol.NewWriter()
		fw.WriteUint8(0)
		fw.WriteUint32(sess.LastActionID())
		feedPkt := protocol.NewPacket(0x00300002, fw.Bytes())
		feedPkt.HasPrevAct = true
		if resp := call(feedPkt, 0x00300002, 8*time.Second); resp != nil {
			dump("叶公好龙 普通培养 feed(0)", resp)
			r := protocol.NewReader(resp.Payload)
			f := make([]int64, 0, 7)
			if code, rErr := r.ReadUint8(); rErr == nil {
				slog.Info("feed result", "code", code)
			}
			for _, read := range []func() (int64, error){
				func() (int64, error) { v, e := r.ReadUint8(); return int64(v), e },
				func() (int64, error) { v, e := r.ReadUint8(); return int64(v), e },
				func() (int64, error) { v, e := r.ReadInt32(); return int64(v), e },
				func() (int64, error) { v, e := r.ReadInt32(); return int64(v), e },
				func() (int64, error) { v, e := r.ReadUint8(); return int64(v), e },
				func() (int64, error) { v, e := r.ReadInt32(); return int64(v), e },
				func() (int64, error) { v, e := r.ReadUint8(); return int64(v), e },
			} {
				v, rErr := read()
				f = append(f, v)
				if rErr != nil {
					break
				}
			}
			slog.Info("feed fields", "values", f)
		}
		after := sess.GetPlayerState()
		slog.Info("培养资产变化", "coin_delta", after.Coins-before.Coins, "ingot_delta", after.Ingots-before.Ingots)
		fetchPet()

		// 召唤仙桃（call_coin_times > 0 时为铜钱/免费档），观察 peach_info 变化。
		peachBefore := sess.GetPlayerState()
		if resp := call(protocol.NewPacket(0x00280006, nil), 0x00280006, 8*time.Second); resp != nil {
			dump("召唤仙桃 call_peach(no prevact)", resp)
		}
		peachAfterCall := sess.GetPlayerState()
		slog.Info("召唤仙桃资产变化", "coin_delta", peachAfterCall.Coins-peachBefore.Coins,
			"ingot_delta", peachAfterCall.Ingots-peachBefore.Ingots)
		info2 := protocol.NewPacket(0x00280003, []byte{0, 0, 0, 0})
		info2.HasPrevAct = true
		if resp := call(info2, 0x00280003, 6*time.Second); resp != nil {
			dump("召唤后 peach_info", resp)
		}

		// 摘取已召唤仙桃（免费战斗），并观察 peach_info 变化。
		warPkt := protocol.NewPacket(0x00280001, []byte{0, 0, 0, 0})
		warPkt.HasPrevAct = true
		if resp := call(warPkt, 0x00280001, 20*time.Second); resp != nil {
			dump("摘取仙桃 get_peach", resp)
		}
		info3 := protocol.NewPacket(0x00280003, []byte{0, 0, 0, 0})
		info3.HasPrevAct = true
		if resp := call(info3, 0x00280003, 6*time.Second); resp != nil {
			dump("摘取后 peach_info", resp)
		}
	}
}