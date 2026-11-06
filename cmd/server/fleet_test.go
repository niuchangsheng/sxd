// Package main 验证多区服启动计划构建与区服覆盖生效。
//
// @author Ateng
// @since 2026-11-06
package main

import (
	"os"
	"path/filepath"
	"testing"

	"sxd-pie-ng/internal/config"
)

func loadConfigForTest(t *testing.T, content string) *config.Config {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write config failed: %v", err)
	}
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	return cfg
}

func TestBuildRolePlans_AppliesPerServerOverrides(t *testing.T) {
	cfg := loadConfigForTest(t, `
server:
  port: 9090
accounts:
  - platform: "xindong"
    username: "demo"
    password: "secret"
    roles:
      - server_id: "s0"
        role_name: "大号"
        auto_login: true
      - server_id: "s1"
        role_name: "小号"
        auto_login: true
scheduler:
  routines:
    herb_garden: true
    treasure_house: false
  server_overrides:
    s0:
      routines:
        treasure_house: true
      treasure_house:
        selection_mode: auto
    s1:
      treasure_house:
        selection_mode: explicit
        award_ids: [4, 21, 33]
`)

	plans := buildRolePlans(cfg, nil, nil)
	if len(plans) != 2 {
		t.Fatalf("expected 2 role plans, got %d", len(plans))
	}

	byServer := make(map[string]*rolePlan)
	for _, p := range plans {
		byServer[p.role.ServerID] = p
	}

	s0 := byServer["s0"]
	if s0 == nil || !s0.enabled["treasure_house"] {
		t.Fatal("expected s0 treasure_house enabled by override")
	}
	if !s0.enabled["herb_garden"] {
		t.Error("expected s0 to inherit global herb_garden")
	}
	if s0.eff.TreasureHouse.SelectionMode != "auto" {
		t.Errorf("expected s0 selection_mode auto, got %q", s0.eff.TreasureHouse.SelectionMode)
	}

	s1 := byServer["s1"]
	if s1 == nil || s1.enabled["treasure_house"] {
		t.Fatal("expected s1 to inherit global treasure_house disabled")
	}
	if s1.eff.TreasureHouse.SelectionMode != "explicit" {
		t.Errorf("expected s1 selection_mode explicit, got %q", s1.eff.TreasureHouse.SelectionMode)
	}
	if s0.factory == s1.factory {
		t.Error("expected different servers to use independent registry factories")
	}
	if s0.cfg.RoleID != "s0-大号" || s1.cfg.RoleID != "s1-小号" {
		t.Errorf("unexpected role ids: %q / %q", s0.cfg.RoleID, s1.cfg.RoleID)
	}
}

func TestBuildRolePlans_DeduplicatesIdenticalRoles(t *testing.T) {
	cfg := loadConfigForTest(t, `
server:
  port: 9090
accounts:
  - platform: "xindong"
    username: "demo"
    password: "secret"
    roles:
      - server_id: "s0"
        role_name: "大号"
      - server_id: "s0"
        role_name: "大号"
      - server_id: "s1"
        role_name: "小号"
`)
	plans := buildRolePlans(cfg, nil, nil)
	if len(plans) != 2 {
		t.Fatalf("expected duplicate role to be skipped, got %d plans", len(plans))
	}
}

func TestResolveServerAddr(t *testing.T) {
	if got := resolveServerAddr(config.RoleConfig{ServerID: "s0"}); got != "9x050.sxdweb.xd.com:8200" {
		t.Errorf("unexpected s0 gateway: %q", got)
	}
	if got := resolveServerAddr(config.RoleConfig{ServerID: "fengwanyx_s813"}); got != "49.232.196.100:8381" {
		t.Errorf("unexpected 813 gateway: %q", got)
	}
	if got := resolveServerAddr(config.RoleConfig{ServerID: "s9", ServerAddr: "1.2.3.4:9000"}); got != "1.2.3.4:9000" {
		t.Errorf("explicit gateway should win, got %q", got)
	}
	if got := resolveServerAddr(config.RoleConfig{ServerID: "s9", ServerAddr: "sandbox"}); got != "sandbox" {
		t.Errorf("unknown sandbox server should stay sandbox, got %q", got)
	}
}
