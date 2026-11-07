//go:build probe

// 目标: 定位琳琅阁 NPC id 并验证 lucky_shop_item_list (Mod_Item_Base, Module 100, Act 80)
//
// 官方客户端 LuckyShopView: 请求参数为当前城镇 NPC id
//
//	npcID = TownType.getTownNPCIdByFunc(player.mapId, NPCType.LuckyShopNPC)
//
// 本探针先从 NPC 结交面板 (436/1) 读取全部 TownNpcID, 再逐个以
// [Int32 npcID] + PrevAct 调用 lucky_shop_item_list, 找到真正回包的 NPC。
// 只读, 不购买任何商品。
//
// 运行命令: SXD_CONFIG=configs/s0.yaml go run -tags=probe test/probes/probe_lucky_shop_npc_live.go
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

const (
	actionLuckyShopItemList uint32 = 0x00640050 // 100/80
	actionLuckyShopBuy      uint32 = 0x00640051 // 100/81
)

func main() {
	slog.SetLogLoggerLevel(slog.LevelInfo)
	timeout := flag.Duration("timeout", 3*time.Second, "单个 NPC 的等待上限")
	npcFlag := flag.Int("npc", 0, "直接指定 NPC id (0=从 NPC 结交面板遍历)")
	enterTown := flag.Int("enter-town", -1, "先 enter_town 到指定城镇 (-1 不切换)")
	flag.Parse()
	cfgPath := os.Getenv("SXD_CONFIG")
	if cfgPath == "" {
		cfgPath = "configs/s0.yaml"
	}
	cfg, err := config.Load(cfgPath)
	if err != nil {
		slog.Error("加载配置失败", "err", err)
		return
	}
	acc := cfg.Accounts[0]
	role := acc.Roles[0]
	tc := platform.NewTicketCache(90 * time.Minute)
	xd, _ := platform.NewXindongAuthenticator()
	xd.SetCookie(acc.Cookie)
	tc.RegisterAuthenticator(xd)
	ctx, cancel := context.WithTimeout(context.Background(), 240*time.Second)
	defer cancel()
	ticket, err := tc.GetOrFetch(ctx, acc.Platform, acc.Username, acc.Password, role.ServerID)
	if err != nil {
		slog.Error("换票失败", "err", err)
		return
	}
	rc := client.SessionConfig{RoleID: fmt.Sprintf("%s-%s", role.ServerID, role.RoleName), RoleName: role.RoleName,
		Username: acc.Username, ServerAddr: ticket.GatewayURL, ServerID: role.ServerID,
		Platform: acc.Platform, Authenticator: client.DefaultAuthenticator, MaxReconnectAttempts: 3}
	client.ApplyTicket(&rc, ticket)
	sess := client.NewRoleSession(rc)
	if err := sess.Start(ctx); err != nil {
		slog.Error("启动会话失败", "err", err)
		return
	}
	defer sess.Close()
	for sess.State() != client.StateActive && ctx.Err() == nil {
		time.Sleep(100 * time.Millisecond)
	}

	ps := sess.GetPlayerState()
	slog.Info("当前场景", "town_id", ps.TownID, "town_map_id", ps.TownMapID)
	if *enterTown >= 0 {
		if code, eErr := sess.EnterTown(ctx, int32(*enterTown)); eErr != nil {
			slog.Warn("enter_town 失败", "town", *enterTown, "err", eErr)
		} else {
			slog.Info("enter_town 完成", "town", *enterTown, "result", code)
		}
		ps = sess.GetPlayerState()
		slog.Info("切换后场景", "town_id", ps.TownID, "town_map_id", ps.TownMapID)
	}

	infoPkt := protocol.BuildNpcFriendshipInfoPacket()
	infoPkt.HasPrevAct = true
	infoPkt.Payload = []byte{0, 0, 0, 0}
	c, cc := context.WithTimeout(ctx, 8*time.Second)
	infoResp, err := sess.Call(c, infoPkt, protocol.ActionIDNpcFriendshipInfo)
	cc()
	if err != nil || infoResp == nil {
		slog.Error("查询 NPC 结交信息失败", "err", err)
		return
	}
	snap, err := protocol.ParseNpcFriendshipInfoResult(infoResp.Payload)
	if err != nil {
		slog.Error("解析 NPC 结交信息失败", "err", err)
		return
	}
	ids := make([]int32, 0, len(snap.Npcs))
	if *npcFlag > 0 {
		ids = append(ids, int32(*npcFlag))
	} else {
		for _, n := range snap.Npcs {
			if n.TownNpcID > 0 {
				ids = append(ids, n.TownNpcID)
			}
		}
	}
	slog.Info("城镇 NPC 列表", "count", len(ids), "town_npc_ids", ids)

	// 两个候选实现 × 带/不带 PrevAct 编码。
	for _, act := range []uint32{actionLuckyShopItemList, 0x0002004C} {
		for _, npcID := range ids {
			for _, withPrev := range []bool{true, false} {
				w := protocol.NewWriter()
				w.WriteInt32(npcID)
				if withPrev {
					w.WriteUint32(0)
				}
				pkt := protocol.NewPacket(act, w.Bytes())
				pkt.HasPrevAct = withPrev
				c, cc := context.WithTimeout(ctx, *timeout)
				resp, err := sess.Call(c, pkt, act)
				cc()
				if err != nil {
					continue
				}
				slog.Info("琳琅阁回包命中", "action", fmt.Sprintf("0x%08X", act), "npc_id", npcID,
					"with_prev", withPrev, "len", len(resp.Payload), "hex", hex.EncodeToString(resp.Payload))
				return
			}
		}
	}
	slog.Warn("未找到可响应的琳琅阁 NPC", "tried", len(ids))
}
