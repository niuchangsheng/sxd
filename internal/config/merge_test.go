// Package config_test 验证按区服覆盖调度策略的深度合并语义。
//
// @author Ateng
// @since 2026-11-06
package config_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"sxd-pie-ng/internal/config"
)

func writeTempConfig(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write temp config failed: %v", err)
	}
	return path
}

func TestEffectiveScheduler_MergesRoutinesAndStrategyPerServer(t *testing.T) {
	path := writeTempConfig(t, `
server:
  port: 9090
scheduler:
  routines:
    herb_garden: true
    arena: false
  treasure_house:
    selection_mode: rarity
    max_draw_times: 10
  server_overrides:
    s0:
      routines:
        arena: true
        treasure_house: true
      treasure_house:
        selection_mode: auto
    s1:
      treasure_house:
        selection_mode: explicit
        award_ids: [4, 21, 33]
`)

	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	s0, err := cfg.EffectiveScheduler("s0")
	if err != nil {
		t.Fatalf("EffectiveScheduler(s0) failed: %v", err)
	}
	if !s0.Routines["arena"] {
		t.Error("expected s0 arena override true")
	}
	if !s0.Routines["treasure_house"] {
		t.Error("expected s0 treasure_house override true")
	}
	if !s0.Routines["herb_garden"] {
		t.Error("expected s0 to inherit global herb_garden true")
	}
	if s0.TreasureHouse.SelectionMode != "auto" {
		t.Errorf("expected s0 selection_mode auto, got %q", s0.TreasureHouse.SelectionMode)
	}
	if s0.TreasureHouse.MaxDrawTimes != 10 {
		t.Errorf("expected s0 to inherit max_draw_times 10, got %d", s0.TreasureHouse.MaxDrawTimes)
	}

	s1, err := cfg.EffectiveScheduler("s1")
	if err != nil {
		t.Fatalf("EffectiveScheduler(s1) failed: %v", err)
	}
	if s1.Routines["arena"] {
		t.Error("expected s1 arena to inherit global false")
	}
	if s1.TreasureHouse.SelectionMode != "explicit" {
		t.Errorf("expected s1 selection_mode explicit, got %q", s1.TreasureHouse.SelectionMode)
	}
	awards := s1.TreasureHouse.ResolveSelections()
	if len(awards) != 3 || awards[0].ItemID != 4 || awards[1].ItemID != 21 || awards[2].ItemID != 33 {
		t.Errorf("unexpected s1 award selections: %+v", awards)
	}

	// 全局配置不得被区服覆盖污染。
	if cfg.Scheduler.Routines["arena"] {
		t.Error("global arena must stay false after computing server overrides")
	}
	if cfg.Scheduler.TreasureHouse.SelectionMode != "rarity" {
		t.Errorf("global selection_mode must stay rarity, got %q", cfg.Scheduler.TreasureHouse.SelectionMode)
	}
}

func TestEffectiveScheduler_DeepMergesNestedConfigAndReplacesLists(t *testing.T) {
	path := writeTempConfig(t, `
server:
  port: 9090
scheduler:
  dungeon:
    sweep:
      enabled: true
      pipeline: ["sweep_elite"]
      max_batch_times: 10
  server_overrides:
    s0:
      dungeon:
        sweep:
          pipeline: ["sweep_elite", "sweep_highest_main"]
          max_batch_times: 3
`)

	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	s0, err := cfg.EffectiveScheduler("s0")
	if err != nil {
		t.Fatalf("EffectiveScheduler failed: %v", err)
	}

	if !s0.Dungeon.Sweep.Enabled {
		t.Error("expected nested enabled to be inherited")
	}
	if s0.Dungeon.Sweep.MaxBatchTimes != 3 {
		t.Errorf("expected max_batch_times 3, got %d", s0.Dungeon.Sweep.MaxBatchTimes)
	}
	pipeline := s0.Dungeon.Sweep.Pipeline
	if len(pipeline) != 2 || pipeline[0] != "sweep_elite" || pipeline[1] != "sweep_highest_main" {
		t.Errorf("expected pipeline to be replaced wholesale, got %v", pipeline)
	}
	if cfg.Scheduler.Dungeon.Sweep.MaxBatchTimes != 10 {
		t.Error("global max_batch_times must not be mutated")
	}
}

func TestEffectiveScheduler_CaseInsensitiveServerID(t *testing.T) {
	path := writeTempConfig(t, `
server:
  port: 9090
scheduler:
  server_overrides:
    s0:
      preset: "all"
`)
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	eff, err := cfg.EffectiveScheduler("S0")
	if err != nil {
		t.Fatalf("EffectiveScheduler failed: %v", err)
	}
	if eff.Preset != "all" {
		t.Errorf("expected case-insensitive override to apply, got preset %q", eff.Preset)
	}
	if cfg.Scheduler.Preset != "recommended" {
		t.Errorf("global preset must stay recommended, got %q", cfg.Scheduler.Preset)
	}
}

func TestStartupTicketDelayDefaultsAndDisable(t *testing.T) {
	defPath := writeTempConfig(t, `
server:
  port: 9090
`)
	defCfg, err := config.Load(defPath)
	if err != nil {
		t.Fatalf("Load default failed: %v", err)
	}
	minDelay, maxDelay := defCfg.Scheduler.Startup.TicketDelayRange()
	if minDelay != 2*time.Second || maxDelay != 5*time.Second {
		t.Errorf("expected default ticket delay 2s~5s, got %s~%s", minDelay, maxDelay)
	}

	offPath := writeTempConfig(t, `
server:
  port: 9090
scheduler:
  startup:
    ticket_min_interval_seconds: 0
    ticket_max_interval_seconds: 0
`)
	offCfg, err := config.Load(offPath)
	if err != nil {
		t.Fatalf("Load disable failed: %v", err)
	}
	minDelay, maxDelay = offCfg.Scheduler.Startup.TicketDelayRange()
	if minDelay != 0 || maxDelay != 0 {
		t.Errorf("expected disabled ticket delay, got %s~%s", minDelay, maxDelay)
	}
}

func TestMasterPracticeDefaultsAndPerServerOverride(t *testing.T) {
	defPath := writeTempConfig(t, `
server:
  port: 9090
`)
	cfg, err := config.Load(defPath)
	if err != nil {
		t.Fatalf("Load defaults failed: %v", err)
	}
	mp := cfg.Scheduler.MasterPractice
	if mp.MaxStarsPerJob != 30 {
		t.Errorf("expected default max_stars_per_job 30, got %d", mp.MaxStarsPerJob)
	}
	if mp.AutoNormalizeDeploy == nil || !*mp.AutoNormalizeDeploy {
		t.Error("expected auto_normalize_deploy default true")
	}
	if mp.AllowSwitchStunt == nil || !*mp.AllowSwitchStunt {
		t.Error("expected allow_switch_stunt default true")
	}
	if mp.AllowStepUp == nil || !*mp.AllowStepUp {
		t.Error("expected allow_step_up default true")
	}

	path := writeTempConfig(t, `
server:
  port: 9090
scheduler:
  master_practice:
    practice_order: [15, 3]
    max_stars_per_job: 30
  server_overrides:
    s0:
      master_practice:
        practice_order: [3, 1]
        auto_normalize_deploy: false
        allow_switch_stunt: false
`)
	cfg, err = config.Load(path)
	if err != nil {
		t.Fatalf("Load override failed: %v", err)
	}
	s0, err := cfg.EffectiveScheduler("s0")
	if err != nil {
		t.Fatalf("EffectiveScheduler(s0) failed: %v", err)
	}
	if len(s0.MasterPractice.PracticeOrder) != 2 || s0.MasterPractice.PracticeOrder[0] != 3 || s0.MasterPractice.PracticeOrder[1] != 1 {
		t.Errorf("unexpected s0 practice_order: %v", s0.MasterPractice.PracticeOrder)
	}
	if s0.MasterPractice.AutoNormalizeDeploy == nil || *s0.MasterPractice.AutoNormalizeDeploy {
		t.Error("expected s0 auto_normalize_deploy false")
	}
	if s0.MasterPractice.AllowSwitchStunt == nil || *s0.MasterPractice.AllowSwitchStunt {
		t.Error("expected s0 allow_switch_stunt false")
	}
	if s0.MasterPractice.MaxStarsPerJob != 30 {
		t.Errorf("expected s0 to inherit max_stars_per_job 30, got %d", s0.MasterPractice.MaxStarsPerJob)
	}
	if len(cfg.Scheduler.MasterPractice.PracticeOrder) != 2 || cfg.Scheduler.MasterPractice.PracticeOrder[0] != 15 {
		t.Errorf("global practice_order must stay [15 3], got %v", cfg.Scheduler.MasterPractice.PracticeOrder)
	}
}

func TestLoad_InvalidServerOverrideFails(t *testing.T) {
	path := writeTempConfig(t, `
server:
  port: 9090
scheduler:
  server_overrides:
    s0:
      routines: "not-a-map"
`)
	if _, err := config.Load(path); err == nil {
		t.Fatal("expected invalid server_overrides to fail Load")
	}
}
