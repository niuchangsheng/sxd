//go:build probe

// 目标: 读取仙盟城镇登录凭据 (Mod 571 panel_info / Mod 575 get_player_sect_info / get_login_info)
//
//	panel_info     571,0  -> 0x023B0000
//	get_player_sect_info 575,10 -> 0x023F000A
//	get_login_info 575,1  -> 0x023F0001 (Int group)
//
// 运行命令: SXD_CONFIG=configs/s0.yaml go run -tags=probe test/probes/probe_sect_town_creds.go
//
// 预期表现: 打印 my_sect_id / my_sect_group 与城镇服务器 host/port/appId 等凭据。
package main

import (
	"context"
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
		cfgPath = "configs/config.yaml"
	}
	cfg, err := config.Load(cfgPath)
	if err != nil {
		slog.Error("加载配置失败", "path", cfgPath, "err", err)
		return
	}
	if len(cfg.Accounts) == 0 || len(cfg.Accounts[0].Roles) == 0 {
		slog.Error("配置文件缺少账号或角色")
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

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	ticketCache.Invalidate(acc.Platform, acc.Username, role.ServerID)
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
		MaxReconnectAttempts: 1,
	}
	client.ApplyTicket(&roleCfg, ticket)

	sess := client.NewRoleSession(roleCfg)
	if err := sess.Start(ctx); err != nil {
		slog.Error("启动会话失败", "err", err)
		return
	}
	defer sess.Close()

	deadline := time.Now().Add(20 * time.Second)
	for sess.State() != client.StateActive && time.Now().Before(deadline) {
		time.Sleep(100 * time.Millisecond)
	}
	if sess.State() != client.StateActive {
		slog.Error("会话未激活", "state", sess.State())
		return
	}
	slog.Info("会话已激活", "role_id", sess.RoleID(), "player_id", sess.PlayerID())

	call := func(pkt *protocol.Packet, act uint32, name string) *protocol.Packet {
		cCtx, cCancel := context.WithTimeout(ctx, 5*time.Second)
		resp, cErr := sess.Call(cCtx, pkt, act)
		cCancel()
		if cErr != nil {
			slog.Error("调用失败: "+name, "err", cErr)
			return nil
		}
		slog.Info("回包: "+name, "len", len(resp.Payload))
		return resp
	}

	panelResp := call(protocol.BuildSectPanelInfoPacket(), protocol.ActionIDSectPanelInfo, "sect panel_info")
	if panelResp != nil {
		summary, pErr := protocol.ParseSectPanelSummary(panelResp.Payload)
		if pErr != nil {
			slog.Error("解析仙盟面板失败", "err", pErr)
		} else {
			slog.Info("仙盟面板摘要",
				"my_appid", summary.MyAppID,
				"my_sect_id", summary.MySectID,
				"my_sect_group", summary.MySectGroup,
				"my_sect_lv", summary.MySectLevel,
				"my_sect_name", summary.MySectName,
			)
		}
	}

	group := int32(0)
	if r := call(protocol.NewPacket(protocol.ActionIDSectOpenStatus, nil), protocol.ActionIDSectOpenStatus, "sect open_status"); r != nil {
		// 仅打印
	}
	if r := call(protocol.NewPacket(0x023F000A, nil), 0x023F000A, "get_player_sect_info"); r != nil && len(r.Payload) >= 4 {
		group = int32(uint32(r.Payload[0])<<24 | uint32(r.Payload[1])<<16 | uint32(r.Payload[2])<<8 | uint32(r.Payload[3]))
		slog.Info("当前仙盟分组", "group", group)
	}

	if infoResp := call(protocol.BuildSectLoginInfoPacket(group), 0x023F0001, "get_login_info"); infoResp != nil {
		info, pErr := protocol.ParseSectLoginInfoResult(infoResp.Payload)
		if pErr != nil {
			slog.Error("解析仙盟城镇凭据失败", "err", pErr)
		} else {
			slog.Info("仙盟城镇凭据",
				"result", info.Result,
				"host", info.Host,
				"port", info.Port,
				"app_id", info.AppID,
				"node", info.Node,
				"server_name", info.ServerName,
				"time", info.Time,
				"group", info.Group,
			)
		}
	}
}
