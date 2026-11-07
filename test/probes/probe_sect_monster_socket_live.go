//go:build probe

// 目标: 宗门灵兽 (Mod_SectMonster_Base, Module 576) 在仙盟城镇会话上的可达性
//
// 官方客户端经 DataBase.SOCKET_SYS (仙盟分组 socket) 调用:
//
//	egg_panel_info  (Act 0, 0x02400000) request=[Int sect_id]
//	get_monster_list(Act 1, 0x02400001) request=[Int sect_id]
//	feed            (Act 3, 0x02400003) request=[Int monster_type, Int feed_type]  (type 0=免费)
//
// 本探针建立仙盟城镇会话, 用 LocalSectID / SectID 分别尝试 get_monster_list, 只读。
//
// 运行命令: SXD_CONFIG=configs/s0.yaml go run -tags=probe test/probes/probe_sect_monster_socket_live.go
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
	feedType := flag.Int("feed-monster", -1, "喂养的 monster_type (>=0 时以 feed_type=0 免费喂养一次)")
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
	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
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

	cred, err := sess.FetchSectTownCredentials(ctx)
	if err != nil {
		slog.Error("读取仙盟凭据失败（可能未加入仙盟）", "err", err)
		return
	}
	slog.Info("仙盟凭据", "sect_id", cred.SectID, "local_sect_id", cred.LocalSectID,
		"sect_name", cred.SectName, "group", cred.Group)

	// 该 context 会作为仙盟城镇会话的内部生命周期 context, 必须在会话使用期间保持存活。
	townCtx, townCancel := context.WithTimeout(ctx, 120*time.Second)
	defer townCancel()
	town, err := sess.OpenSectTownSessionWithCredentials(townCtx, cred)
	if err != nil {
		slog.Error("建立仙盟城镇会话失败", "err", err)
		return
	}
	defer town.Close()
	slog.Info("仙盟城镇会话已建立", "role_id", town.RoleID())

	for _, act := range []uint32{0x02400001, 0x02400000} {
		for _, sectID := range []int32{cred.LocalSectID, cred.SectID} {
			if sectID <= 0 {
				continue
			}
			for _, withPrev := range []bool{true, false} {
				w := protocol.NewWriter()
				w.WriteInt32(sectID)
				pkt := protocol.NewPacket(act, w.Bytes())
				if withPrev {
					w.WriteUint32(0)
					pkt = protocol.NewPacket(act, w.Bytes())
					pkt.HasPrevAct = true
				}
				c, cc := context.WithTimeout(ctx, 6*time.Second)
				r, err := town.Call(c, pkt, act)
				cc()
				if err != nil {
					slog.Warn("仙盟城镇无回包", "action", fmt.Sprintf("0x%08X", act),
						"sect_id", sectID, "with_prev", withPrev, "err", err)
					continue
				}
				slog.Info("宗门灵兽回包", "action", fmt.Sprintf("0x%08X", act), "sect_id", sectID,
					"with_prev", withPrev, "len", len(r.Payload), "hex", hex.EncodeToString(r.Payload))
				if act == 0x02400001 && *feedType >= 0 {
					fw := protocol.NewWriter()
					fw.WriteInt32(int32(*feedType))
					fw.WriteInt32(0) // feed_type 0 = 免费
					fpkt := protocol.NewPacket(0x02400003, fw.Bytes())
					fc, fcc := context.WithTimeout(ctx, 10*time.Second)
					fr, ferr := town.Call(fc, fpkt, 0x02400003)
					fcc()
					if ferr != nil {
						slog.Warn("宗门灵兽喂养无回包", "err", ferr)
					} else {
						slog.Info("宗门灵兽喂养回包", "len", len(fr.Payload), "hex", hex.EncodeToString(fr.Payload))
					}
				}
				return
			}
		}
	}
	// 主服会话再试一次 (官方客户端走分组 socket, 但部分区服在主服也接受)。
	for _, sectID := range []int32{cred.LocalSectID, cred.SectID} {
		if sectID <= 0 {
			continue
		}
		w := protocol.NewWriter()
		w.WriteInt32(sectID)
		w.WriteUint32(0)
		pkt := protocol.NewPacket(0x02400001, w.Bytes())
		pkt.HasPrevAct = true
		c, cc := context.WithTimeout(ctx, 8*time.Second)
		r, err := sess.Call(c, pkt, 0x02400001)
		cc()
		if err != nil {
			slog.Warn("主服无回包", "sect_id", sectID, "err", err)
			continue
		}
		slog.Info("宗门灵兽列表(主服)", "sect_id", sectID, "len", len(r.Payload),
			"hex", hex.EncodeToString(r.Payload))
		break
	}
}
