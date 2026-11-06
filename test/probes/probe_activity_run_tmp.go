//go:build probe

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
	"sxd-pie-ng/internal/routines/dungeon"
	"sxd-pie-ng/internal/routines/farming"
	"sxd-pie-ng/internal/routines/minigame"
	"sxd-pie-ng/internal/routines/pvp"
	"sxd-pie-ng/internal/routines/social"
	"sxd-pie-ng/internal/scheduler"
)

func main() {
	slog.SetLogLoggerLevel(slog.LevelInfo)
	cfg, _ := config.Load("configs/s0.yaml")
	acc := cfg.Accounts[0]
	role := acc.Roles[0]
	tc := platform.NewTicketCache(90 * time.Minute)
	xd, _ := platform.NewXindongAuthenticator()
	xd.SetCookie(acc.Cookie)
	tc.RegisterAuthenticator(xd)
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Second)
	defer cancel()
	tc.Invalidate(acc.Platform, acc.Username, role.ServerID)
	ticket, err := tc.GetOrFetch(ctx, acc.Platform, acc.Username, acc.Password, role.ServerID)
	if err != nil {
		panic(err)
	}
	rc := client.SessionConfig{RoleID: fmt.Sprintf("%s-%s", role.ServerID, role.RoleName), RoleName: role.RoleName, Username: acc.Username, ServerAddr: ticket.GatewayURL, ServerID: role.ServerID, Platform: acc.Platform, Authenticator: client.DefaultAuthenticator, MaxReconnectAttempts: 2}
	client.ApplyTicket(&rc, ticket)
	s := client.NewRoleSession(rc)
	if err := s.Start(ctx); err != nil {
		panic(err)
	}
	defer s.Close()
	for s.State() != client.StateActive {
		time.Sleep(100 * time.Millisecond)
	}
	before := s.GetPlayerState()
	slog.Info("start", "ingots", before.Ingots, "coins", before.Coins)

	list := []struct {
		name string
		r    scheduler.ActivityRoutine
	}{
		{"duobao_mountain", minigame.NewDuobaoMountainRoutine()},
		{"pilgrimage", farming.NewPilgrimageRoutine()},
		{"caravan_hijack", pvp.NewCaravanHijackRoutine()},
		{"faction_blessing", social.NewFactionBlessingRoutine(social.DefaultFactionBlessingConfig())},
		{"qimen_dunjia", dungeon.NewQiMenRoutine()},
		{"homestead", social.NewHomesteadRoutine()},
		{"wufa_field", minigame.NewWuFaRoutine()},
		{"jieyuan_box", social.NewJieYuanBoxRoutine(social.DefaultJieYuanBoxConfig())},
	}
	for _, it := range list {
		if err := ctx.Err(); err != nil {
			break
		}
		if err := it.r.Execute(ctx, s, nil); err != nil {
			slog.Warn("routine error", "name", it.name, "err", err)
		}
	}
	after := s.GetPlayerState()
	slog.Info("done", "ingots", after.Ingots, "coins", after.Coins,
		"ingot_delta", after.Ingots-before.Ingots)

	c, cc := context.WithTimeout(ctx, 8*time.Second)
	r, e := s.Call(c, protocol.NewPacket(0x0000002A, nil), 0x0000002A)
	cc()
	if e != nil {
		slog.Warn("assistant info", "err", e)
		return
	}
	_ = os.WriteFile("/tmp/assistant_after_hex.txt", []byte(hex.EncodeToString(r.Payload)), 0o644)
	slog.Info("activity saved", "len", len(r.Payload))
}