//go:build probe

// 目标: 建立仙盟城镇独立会话并验证可用动作
//
//	主服: panel_info (571,0) 取 my_sect_id/group → get_login_info (575,1) 取 host/port/passCode
//	城镇: login (575,2) → enter_town (575,3, 尝试 townID 候选) → sect_main_info (572,1)
//
// 运行命令: SXD_CONFIG=configs/s0.yaml go run -tags=probe test/probes/probe_sect_town_session.go
package main

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"os"
	"strconv"
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

	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Second)
	defer cancel()

	ticketCache.Invalidate(acc.Platform, acc.Username, role.ServerID)
	ticket, err := ticketCache.GetOrFetch(ctx, acc.Platform, acc.Username, acc.Password, role.ServerID)
	if err != nil {
		slog.Error("换票失败", "err", err)
		return
	}

	mainCfg := client.SessionConfig{
		RoleID:               fmt.Sprintf("%s-%s", role.ServerID, role.RoleName),
		RoleName:             role.RoleName,
		Username:             acc.Username,
		ServerAddr:           ticket.GatewayURL,
		ServerID:             role.ServerID,
		Platform:             acc.Platform,
		Authenticator:        client.DefaultAuthenticator,
		MaxReconnectAttempts: 1,
	}
	client.ApplyTicket(&mainCfg, ticket)
	mainSess := client.NewRoleSession(mainCfg)
	if err := mainSess.Start(ctx); err != nil {
		slog.Error("主服会话启动失败", "err", err)
		return
	}
	defer mainSess.Close()
	for i := 0; i < 200 && mainSess.State() != client.StateActive; i++ {
		time.Sleep(100 * time.Millisecond)
	}
	if mainSess.State() != client.StateActive {
		slog.Error("主服会话未激活", "state", mainSess.State())
		return
	}

	callMain := func(pkt *protocol.Packet, act uint32, name string) *protocol.Packet {
		cCtx, cCancel := context.WithTimeout(ctx, 5*time.Second)
		resp, cErr := mainSess.Call(cCtx, pkt, act)
		cCancel()
		if cErr != nil {
			slog.Error("主服调用失败: "+name, "err", cErr)
			return nil
		}
		return resp
	}

	panelResp := callMain(protocol.BuildSectPanelInfoPacket(), protocol.ActionIDSectPanelInfo, "panel_info")
	if panelResp == nil {
		return
	}
	summary, err := protocol.ParseSectPanelSummary(panelResp.Payload)
	if err != nil {
		slog.Error("解析仙盟面板失败", "err", err)
		return
	}
	slog.Info("仙盟摘要", "sect_id", summary.MySectID, "group", summary.MySectGroup, "name", summary.MySectName, "locations", summary.Locations)

	infoResp := callMain(protocol.BuildSectLoginInfoPacket(summary.MySectGroup), 0x023F0001, "get_login_info")
	if infoResp == nil {
		return
	}
	loginInfo, err := protocol.ParseSectLoginInfoResult(infoResp.Payload)
	if err != nil || loginInfo.Result != 0 {
		slog.Error("获取仙盟城镇凭据失败", "err", err, "result", loginInfo)
		return
	}
	slog.Info("仙盟城镇凭据",
		"host", loginInfo.Host, "port", loginInfo.Port, "app_id", loginInfo.AppID,
		"server", loginInfo.ServerName, "node", loginInfo.Node, "group", loginInfo.Group,
	)

	localSectID := summary.MySectID % 10000000
	candidates := make([]int32, 0, 10)
	for _, loc := range summary.Locations {
		if loc.SectID == summary.MySectID || loc.SectID == localSectID {
			candidates = append(candidates, loc.LocationID)
		}
	}
	candidates = append(candidates, 1, 2, 0, 1001, summary.MySectID)
	cfg2 := client.SessionConfig{
		RoleID:               mainSess.RoleID() + "-sect",
		RoleName:             role.RoleName,
		ServerAddr:           net.JoinHostPort(loginInfo.Host, strconv.Itoa(int(loginInfo.Port))),
		ServerID:             loginInfo.ServerName,
		Platform:             acc.Platform,
		MaxReconnectAttempts: 1,
		HeartbeatInterval:    5 * time.Second,
	}
	var enteredTown int32 = -1
	cfg2.Authenticator = func(cs *client.RoleSession) error {
		aCtx, aCancel := context.WithTimeout(context.Background(), 120*time.Second)
		defer aCancel()
		loginResp, lErr := cs.Call(aCtx, protocol.BuildSectLoginPacket(*loginInfo, mainSess.PlayerID()), 0x023F0002)
		if lErr != nil {
			return fmt.Errorf("仙盟城镇登录失败: %w", lErr)
		}
		result, pErr := protocol.ParseSectActionResult(loginResp.Payload)
		if pErr != nil || result != 0 {
			return fmt.Errorf("仙盟城镇登录被拒绝: result=%d err=%v", result, pErr)
		}
		slog.Info("仙盟城镇登录成功")
		probes := []int32{146, 148}
		second := summary.MySectID % 10000000 // 地点列表中的本服宗门 ID (7)
		for idx, townID := range probes {
			eResp, eErr := cs.Call(aCtx, protocol.BuildSectEnterTownPacket(townID, second), 0x023F0003)
			if eErr != nil {
				continue
			}
			eResult, epErr := protocol.ParseSectActionResult(eResp.Payload)
			if epErr == nil && eResult == 0 {
				slog.Info("enter_town 成功", "town_id", townID, "sect_id", second)
				enteredTown = townID
				return nil
			}
			if idx%50 == 0 {
				slog.Info("enter_town 探测进度", "tried", idx, "current_town", townID, "result", eResult)
			}
		}
		return fmt.Errorf("所有 townID 候选均进入失败")
	}
	sectSess := client.NewRoleSession(cfg2)
	if err := sectSess.Start(ctx); err != nil {
		slog.Error("仙盟城镇会话启动失败", "err", err)
		return
	}
	defer sectSess.Close()
	for i := 0; i < 200 && sectSess.State() != client.StateActive; i++ {
		time.Sleep(100 * time.Millisecond)
	}
	if sectSess.State() != client.StateActive {
		slog.Error("仙盟城镇会话未激活", "state", sectSess.State(), "entered_town", enteredTown)
		return
	}
	slog.Info("仙盟城镇会话已激活", "town_id", enteredTown)

	// 验证城镇上的仙盟动作
	for _, c := range []struct {
		act  uint32
		name string
		body []byte
	}{
		{0x023C0001, "sect_main_info", protocol.BuildSectMainInfoPacket(summary.MySectID).Payload},
		{0x024F0001, "sect_bonus.open_panel", protocol.BuildSectBonusPanelPacket(summary.MySectID).Payload},
		{0x023C000D, "sect_worship", protocol.BuildSectWorshipPacket(summary.MySectID).Payload},
	} {
		cCtx, cCancel := context.WithTimeout(ctx, 5*time.Second)
		resp, cErr := sectSess.Call(cCtx, protocol.NewPacket(c.act, c.body), c.act)
		cCancel()
		if cErr != nil {
			slog.Warn("仙盟城镇动作无应答", "name", c.name, "err", cErr)
			continue
		}
		slog.Info("仙盟城镇动作回包", "name", c.name, "len", len(resp.Payload), "first", resp.Payload[0])
	}
}
