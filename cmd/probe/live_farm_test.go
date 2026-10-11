//go:build live_test

package main

import (
	"context"
	"encoding/hex"
	"testing"
	"time"

	"sxd-pie-ng/internal/client"
	"sxd-pie-ng/internal/config"
	"sxd-pie-ng/internal/platform"
	"sxd-pie-ng/internal/protocol"
)

func TestLiveFarmInteraction(t *testing.T) {
	cfg, err := config.Load("../../configs/config.yaml")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	acc := cfg.Accounts[0]
	role := acc.Roles[0]

	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()

	ticketCache := platform.NewTicketCache(90 * time.Minute)
	if fwAuth, err := platform.NewFengwanAuthenticator(); err == nil {
		ticketCache.RegisterAuthenticator(fwAuth)
	}

	ticket, err := ticketCache.GetOrFetch(ctx, acc.Platform, acc.Username, acc.Password, role.ServerID)
	if err != nil {
		t.Fatalf("GetOrFetch ticket: %v", err)
	}

	roleID := role.ServerID + "-" + role.RoleName
	serverAddr := ticket.GatewayURL
	if serverAddr == "" {
		serverAddr = role.ServerAddr
	}

	roleCfg := client.SessionConfig{
		RoleID:            roleID,
		RoleName:          role.RoleName,
		Username:          acc.Username,
		ServerAddr:        serverAddr,
		ServerID:          role.ServerID,
		Platform:          acc.Platform,
		HeartbeatInterval: 60 * time.Second,
		Authenticator:     client.DefaultAuthenticator,
	}
	client.ApplyTicket(&roleCfg, ticket)

	sess := client.NewRoleSession(roleCfg)
	if err := sess.Start(ctx); err != nil {
		t.Fatalf("sess.Start: %v", err)
	}
	defer sess.Close()

	deadline := time.Now().Add(10 * time.Second)
	for sess.State() != client.StateActive && time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
	}
	if sess.State() != client.StateActive {
		t.Fatalf("Session state: %s", sess.State())
	}
	sess.RegisterHandler(0x00000017, func(p *protocol.Packet) {
		t.Logf("[SNIFFER] Got 0x00000017 len=%d hex=%s", len(p.Payload), hex.EncodeToString(p.Payload))
	})
	sess.RegisterHandler(0x000D0017, func(p *protocol.Packet) {
		t.Logf("[SNIFFER] Got 0x000D0017 len=%d hex=%s", len(p.Payload), hex.EncodeToString(p.Payload))
	})
	sess.RegisterHandler(0x000D0018, func(p *protocol.Packet) {
		t.Logf("[SNIFFER] Got 0x000D0018 len=%d hex=%s", len(p.Payload), hex.EncodeToString(p.Payload))
	})
	sess.RegisterHandler(0x000D0019, func(p *protocol.Packet) {
		t.Logf("[SNIFFER] Got 0x000D0019 len=%d hex=%s", len(p.Payload), hex.EncodeToString(p.Payload))
	})

	callWithTimeout := func(pkt *protocol.Packet, expectedAct uint32) (*protocol.Packet, error) {
		cCtx, cCancel := context.WithTimeout(ctx, 3*time.Second)
		defer cCancel()
		return sess.Call(cCtx, pkt, expectedAct)
	}

	// 0. Complete Town Scene Entry Handshake (Mod 216 Act 2 -> Mod 1 Act 0 TownEnter -> Mod 0 Act 37)
	t.Logf("[PROBE] Entering Town scene (Mod 1 Act 0)...")
	// Step A: Mod 216 Act 2 (0x00D80002)
	p216 := protocol.NewPacket(0x00D80002, []byte{0x00, 0x00, 0x00, 0x02})
	resp216, err216 := callWithTimeout(p216, 0x00D80002)
	if err216 != nil {
		t.Logf("[PROBE] Mod 216 Act 2 err: %v", err216)
	} else {
		t.Logf("[PROBE] Mod 216 Act 2 response len=%d", len(resp216.Payload))
	}

	// Step B: Mod 1 Act 0 (ActionIDTownEnter 0x00010000), Payload: [0xFF, 0xFF, 0xFF, 0xFF, 0x00, 0xD8, 0x00, 0x02]
	pTown := protocol.NewPacket(protocol.ActionIDTownEnter, []byte{0xFF, 0xFF, 0xFF, 0xFF, 0x00, 0xD8, 0x00, 0x02})
	if err := sess.Send(pTown); err != nil {
		t.Fatalf("[PROBE] Send TownEnter: %v", err)
	}
	t.Logf("[PROBE] Sent TownEnter!")

	// Step C: Mod 0 Act 37 (0x00000025), Payload: [0x00, 0x01, 0x00, 0x00]
	p37 := protocol.NewPacket(0x00000025, []byte{0x00, 0x01, 0x00, 0x00})
	_ = sess.Send(p37)
	time.Sleep(300 * time.Millisecond)

	// 1. Act 0 (GetInfo) Step 1
	infoPkt, _ := protocol.BuildFarmGetInfoPacket(protocol.FarmGetInfoRequest{PrevAct: protocol.ActionIDTownEnter})
	infoResp, err := callWithTimeout(infoPkt, protocol.ActionIDFarmGetInfo)
	if err != nil {
		t.Fatalf("Act 0 error: %v", err)
	}
	t.Logf("[PROBE] Act 0 response len=%d", len(infoResp.Payload))
	time.Sleep(400 * time.Millisecond)

	// 2. Act 1 (GetRoles)
	rolesPkt, _ := protocol.NewPacket(0x000D0001, []byte{0x00, 0x0D, 0x00, 0x00}), error(nil)
	rolesResp, err := callWithTimeout(rolesPkt, 0x000D0001)
	if err != nil {
		t.Fatalf("Act 1 error: %v", err)
	}
	t.Logf("[PROBE] Act 1 response len=%d", len(rolesResp.Payload))
	time.Sleep(400 * time.Millisecond)

	// 3. Act 12 (GetBlessing)
	blessPkt, _ := protocol.NewPacket(0x000D000C, []byte{0x00, 0x0D, 0x00, 0x01}), error(nil)
	blessResp, err := callWithTimeout(blessPkt, 0x000D000C)
	if err != nil {
		t.Fatalf("Act 12 error: %v", err)
	}
	t.Logf("[PROBE] Act 12 response len=%d", len(blessResp.Payload))
	time.Sleep(400 * time.Millisecond)

	// 4. Act 0 (GetInfo) Step 2
	info2Pkt, _ := protocol.BuildFarmGetInfoPacket(protocol.FarmGetInfoRequest{PrevAct: 0x000D000C})
	info2Resp, err := callWithTimeout(info2Pkt, protocol.ActionIDFarmGetInfo)
	if err != nil {
		t.Fatalf("Act 0 step 2 error: %v", err)
	}
	t.Logf("[PROBE] Act 0 step 2 response len=%d", len(info2Resp.Payload))
	time.Sleep(500 * time.Millisecond)

	// 5. Act 21 (PlantState)
	pStatePkt, _ := protocol.BuildFarmPlantStatePacket(protocol.FarmPlantStateRequest{PrevAct: protocol.ActionIDFarmGetInfo})
	pStateResp, err := callWithTimeout(pStatePkt, protocol.ActionIDFarmGetPlantState)
	if err != nil {
		t.Fatalf("Act 21 error: %v", err)
	}
	pStateRes, _ := protocol.ParseFarmPlantStateResult(pStateResp.Payload)
	for _, f := range pStateRes.Fields {
		t.Logf("  Land %d: State=%d Cooldown=%d SeedID=%d", f.LandID, f.State, f.Cooldown, f.SeedOrRoleID)
	}
	time.Sleep(400 * time.Millisecond)

	// 6. Act 22 (Seeds)
	seedsPkt, _ := protocol.BuildFarmSeedsPacket(protocol.FarmSeedsRequest{PrevAct: protocol.ActionIDFarmGetPlantState})
	seedsResp, err := callWithTimeout(seedsPkt, protocol.ActionIDFarmGetSeeds)
	if err != nil {
		t.Fatalf("Act 22 error: %v", err)
	}
	t.Logf("[PROBE] Act 22 response len=%d", len(seedsResp.Payload))
	seedsRes, _ := protocol.ParseFarmSeedsResult(seedsResp.Payload)
	for _, s := range seedsRes.Seeds {
		t.Logf("  Seed %d: Owned=%d Price=%d", s.SeedID, s.OwnedCount, s.Price)
	}
	time.Sleep(500 * time.Millisecond)

	// 7. Step 1: Buy Seed 168 (PrevAct = Act 22 0x000D0016), expect 0x21
	t.Logf("[PROBE] Buying Seed 168 (PrevAct=0x000D0016)...")
	buy168Pkt, _ := protocol.BuildFarmBuySeedPacket(protocol.FarmBuySeedRequest{
		SeedID:  168,
		PrevAct: protocol.ActionIDFarmGetSeeds,
	})
	buy168Resp, buy168Err := callWithTimeout(buy168Pkt, protocol.ActionIDFarmBuySeed)
	if buy168Err != nil {
		t.Fatalf("[PROBE] Buy Seed 168 failed: %v", buy168Err)
	}
	t.Logf("[PROBE] Buy Seed 168 response hex=%s", hex.EncodeToString(buy168Resp.Payload))
	time.Sleep(300 * time.Millisecond)

	// 8. Step 2: Buy Seed 169 (PrevAct = Act 23 0x000D0017), expect 0x08!
	t.Logf("[PROBE] Buying Seed 169 (PrevAct=0x000D0017)...")
	buy169Pkt, _ := protocol.BuildFarmBuySeedPacket(protocol.FarmBuySeedRequest{
		SeedID:  169,
		PrevAct: protocol.ActionIDFarmBuySeed,
	})
	buy169Resp, buy169Err := callWithTimeout(buy169Pkt, protocol.ActionIDFarmBuySeed)
	if buy169Err != nil {
		t.Fatalf("[PROBE] Buy Seed 169 failed: %v", buy169Err)
	}
	t.Logf("[PROBE] Buy Seed 169 SUCCESS! hex=%s", hex.EncodeToString(buy169Resp.Payload))
	buy169Res, _ := protocol.ParseFarmBuySeedResult(buy169Resp.Payload)
	t.Logf("     Parsed Buy Result: Success=%v", buy169Res.Success)
	time.Sleep(400 * time.Millisecond)

	// 9. Step 3: Refresh Seeds (Act 22, PrevAct = Act 23 0x000D0017)
	t.Logf("[PROBE] Refreshing Seeds (Act 22, PrevAct=0x000D0017)...")
	seedsRefPkt, _ := protocol.BuildFarmSeedsPacket(protocol.FarmSeedsRequest{PrevAct: protocol.ActionIDFarmBuySeed})
	seedsRefResp, seedsRefErr := callWithTimeout(seedsRefPkt, protocol.ActionIDFarmGetSeeds)
	if seedsRefErr != nil {
		t.Fatalf("[PROBE] Refresh Seeds failed: %v", seedsRefErr)
	}
	t.Logf("[PROBE] Refresh Seeds SUCCESS! len=%d", len(seedsRefResp.Payload))
	time.Sleep(400 * time.Millisecond)

	// 10. Step 4: Plant Land 10 with Seed 169 (PrevAct = Act 22 0x000D0016)
	t.Logf("[PROBE] Planting Land 10 with Seed 169 (PrevAct=0x000D0016)...")
	plant10Pkt, _ := protocol.BuildFarmPlantPacket(protocol.FarmPlantRequest{
		LandID:       10,
		SeedOrRoleID: 169,
		PrevAct:      protocol.ActionIDFarmGetSeeds,
	})
	p10Resp, p10Err := callWithTimeout(plant10Pkt, protocol.ActionIDFarmPlant)
	if p10Err != nil {
		t.Fatalf("[PROBE] Plant Land 10 failed: %v", p10Err)
	}
	t.Logf("[PROBE] Plant Land 10 SUCCESS! hex=%s", hex.EncodeToString(p10Resp.Payload))
	res10, _ := protocol.ParseFarmPlantResult(p10Resp.Payload)
	t.Logf("     Parsed Land 10: Success=%v Cooldown=%d", res10.Success, res10.Cooldown)
	time.Sleep(500 * time.Millisecond)

	// 11. Step 5: Harvest Land 10 (PrevAct = Act 24 0x000D0018)
	t.Logf("[PROBE] Harvesting Land 10 (PrevAct=0x000D0018)...")
	h10Pkt, _ := protocol.BuildFarmHarvestPacket(protocol.FarmHarvestRequest{
		LandID:  10,
		PrevAct: protocol.ActionIDFarmPlant,
	})
	h10Resp, h10Err := callWithTimeout(h10Pkt, protocol.ActionIDFarmHarvest)
	if h10Err != nil {
		t.Fatalf("[PROBE] Harvest Land 10 failed: %v", h10Err)
	}
	t.Logf("[PROBE] Harvest Land 10 SUCCESS! hex=%s", hex.EncodeToString(h10Resp.Payload))
	hRes10, _ := protocol.ParseFarmHarvestResult(h10Resp.Payload)
	t.Logf("     Harvest Land 10: Success=%v Exp=%d Coins=%d Cooldown=%d", hRes10.Success, hRes10.GainExp, hRes10.GainCoins, hRes10.Cooldown)
	time.Sleep(500 * time.Millisecond)

	// 10. Step 4: Plant Land 12 (Red Land) with owned Fruit Tree Seed 176 (PrevAct = Act 25 0x000D0019)
	t.Logf("[PROBE] Planting Land 12 with Seed 176 (PrevAct=0x000D0019)...")
	plant12Pkt, _ := protocol.BuildFarmPlantPacket(protocol.FarmPlantRequest{
		LandID:       12,
		SeedOrRoleID: 176,
		PrevAct:      protocol.ActionIDFarmHarvest,
	})
	p12Resp, p12Err := callWithTimeout(plant12Pkt, protocol.ActionIDFarmPlant)
	if p12Err != nil {
		t.Fatalf("[PROBE] Plant Land 12 failed: %v", p12Err)
	}
	t.Logf("[PROBE] Plant Land 12 SUCCESS! hex=%s", hex.EncodeToString(p12Resp.Payload))
	res12, _ := protocol.ParseFarmPlantResult(p12Resp.Payload)
	t.Logf("     Parsed Land 12: Success=%v Cooldown=%d", res12.Success, res12.Cooldown)
	time.Sleep(500 * time.Millisecond)

	// 11. Step 5: Harvest Land 12 (PrevAct = Act 24 0x000D0018)
	t.Logf("[PROBE] Harvesting Land 12 (PrevAct=0x000D0018)...")
	h12Pkt, _ := protocol.BuildFarmHarvestPacket(protocol.FarmHarvestRequest{
		LandID:  12,
		PrevAct: protocol.ActionIDFarmPlant,
	})
	h12Resp, h12Err := callWithTimeout(h12Pkt, protocol.ActionIDFarmHarvest)
	if h12Err != nil {
		t.Fatalf("[PROBE] Harvest Land 12 failed: %v", h12Err)
	}
	t.Logf("[PROBE] Harvest Land 12 SUCCESS! hex=%s", hex.EncodeToString(h12Resp.Payload))
	hRes12, _ := protocol.ParseFarmHarvestResult(h12Resp.Payload)
	t.Logf("     Harvest Land 12: Success=%v Exp=%d Coins=%d Cooldown=%d", hRes12.Success, hRes12.GainExp, hRes12.GainCoins, hRes12.Cooldown)
}
