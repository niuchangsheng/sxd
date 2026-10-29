// Package config_test 验证 YAML 配置加载、环境变量替换与校验机制。
//
// @author Ateng
// @since 2026-10-08
package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"sxd-pie-ng/internal/config"
)

func TestConfig_LoadExample(t *testing.T) {
	// 设置测试环境变量
	_ = os.Setenv("PLATFORM_PASSWORD", "secret123456")
	defer os.Unsetenv("PLATFORM_PASSWORD")

	examplePath := filepath.Join("..", "..", "configs", "config.example.yaml")
	cfg, err := config.Load(examplePath)
	if err != nil {
		t.Fatalf("Load example config failed: %v", err)
	}

	if cfg.Server.Port != 8080 {
		t.Errorf("expected port 8080, got %d", cfg.Server.Port)
	}
	if cfg.Server.Host != "127.0.0.1" {
		t.Errorf("expected host 127.0.0.1, got %s", cfg.Server.Host)
	}

	if len(cfg.Accounts) != 1 {
		t.Fatalf("expected 1 account, got %d", len(cfg.Accounts))
	}
	acc := cfg.Accounts[0]
	if acc.Platform != "xindong" {
		t.Errorf("expected platform xindong, got %s", acc.Platform)
	}
	if acc.Password != "secret123456" {
		t.Errorf("expected password to be expanded from env, got %s", acc.Password)
	}
	if len(acc.Roles) != 1 {
		t.Fatalf("expected 1 role, got %d", len(acc.Roles))
	}

	if cfg.Scheduler.Jitter.MinSeconds != 1.0 || cfg.Scheduler.Jitter.MaxSeconds != 3.0 {
		t.Errorf("unexpected jitter bounds: %v - %v", cfg.Scheduler.Jitter.MinSeconds, cfg.Scheduler.Jitter.MaxSeconds)
	}
	if !cfg.Scheduler.Routines["herb_garden"] {
		t.Errorf("expected herb_garden to be enabled")
	}

	// 验证魔王试炼配置
	if !cfg.Scheduler.Routines["beelzebub_trials"] {
		t.Errorf("expected beelzebub_trials to be enabled")
	}
	if cfg.Scheduler.BeelzebubTrials.MinLevel != 0 || !cfg.Scheduler.BeelzebubTrials.AutoMoralAward {
		t.Errorf("unexpected beelzebub_trials config: %+v", cfg.Scheduler.BeelzebubTrials)
	}

	// 验证六道轮回配置
	if !cfg.Scheduler.Routines["six_realms"] {
		t.Errorf("expected six_realms to be enabled")
	}
	if !cfg.Scheduler.SixRealms.AutoReset || !cfg.Scheduler.SixRealms.AutoSellBlue || cfg.Scheduler.SixRealms.AutoSellPurple {
		t.Errorf("unexpected six_realms config: %+v", cfg.Scheduler.SixRealms)
	}

	// 验证结缘宝箱每批最多10个配置
	if cfg.Scheduler.JieYuanBox.MaxBatchSize != 10 {
		t.Errorf("expected jieyuan_box max_batch_size to be 10, got %d", cfg.Scheduler.JieYuanBox.MaxBatchSize)
	}

	// 验证天帝宝库三档大奖配置
	awards := cfg.Scheduler.TreasureHouse.ResolveSelections()
	if len(awards) != 3 {
		t.Fatalf("expected 3 awards, got %d", len(awards))
	}
	if awards[0].ItemID != 50 || awards[1].ItemID != 25 || awards[2].ItemID != 42 {
		t.Errorf("unexpected treasure awards: %+v", awards)
	}

	// 验证副本扫荡配置: 去掉 sweep_highest_main, 仅保留 sweep_elite, 开启 sell_swept_items
	if !cfg.Scheduler.Dungeon.Sweep.Enabled {
		t.Errorf("expected dungeon.sweep to be enabled")
	}
	if !cfg.Scheduler.Dungeon.Sweep.SellSweptItems {
		t.Errorf("expected dungeon.sweep.sell_swept_items to be true")
	}
	if cfg.Scheduler.Dungeon.QuestCycle.MaxReplenishStamina != 200 {
		t.Errorf("expected quest_cycle.max_replenish_stamina to be 200, got %d", cfg.Scheduler.Dungeon.QuestCycle.MaxReplenishStamina)
	}
	pipeline := cfg.Scheduler.Dungeon.Sweep.Pipeline
	if len(pipeline) != 1 || pipeline[0] != "sweep_elite" {
		t.Errorf("expected sweep pipeline to be [sweep_elite], got %v", pipeline)
	}
}

func TestConfig_ValidationDefaults(t *testing.T) {
	// 测试默认值补齐
	minimalYaml := `
server:
  port: 9090
`
	tmpFile := filepath.Join(t.TempDir(), "test_config.yaml")
	if err := os.WriteFile(tmpFile, []byte(minimalYaml), 0644); err != nil {
		t.Fatalf("write tmpFile failed: %v", err)
	}

	cfg, err := config.Load(tmpFile)
	if err != nil {
		t.Fatalf("Load minimal config failed: %v", err)
	}

	if cfg.Server.Host != "127.0.0.1" {
		t.Errorf("expected default host 127.0.0.1, got %s", cfg.Server.Host)
	}
	if cfg.Scheduler.Jitter.MinSeconds != 1.0 || cfg.Scheduler.Jitter.MaxSeconds != 3.0 {
		t.Errorf("expected default jitter [1.0, 3.0], got [%v, %v]",
			cfg.Scheduler.Jitter.MinSeconds, cfg.Scheduler.Jitter.MaxSeconds)
	}
	for _, p := range cfg.Scheduler.Dungeon.Pipeline {
		if p == "sweep_highest_main" {
			t.Errorf("expected default dungeon pipeline not to contain sweep_highest_main, got %v", cfg.Scheduler.Dungeon.Pipeline)
		}
	}
}

func TestConfig_LoadConfigYamlIfExists(t *testing.T) {
	configPath := filepath.Join("..", "..", "configs", "config.yaml")
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		t.Skip("configs/config.yaml does not exist, skipping")
	}

	cfg, err := config.Load(configPath)
	if err != nil {
		t.Fatalf("Load configs/config.yaml failed: %v", err)
	}

	if !cfg.Scheduler.Routines["beelzebub_trials"] {
		t.Errorf("expected beelzebub_trials routine to be enabled in configs/config.yaml")
	}
	if cfg.Scheduler.BeelzebubTrials.MinLevel != 0 {
		t.Errorf("expected min_level 0, got %d", cfg.Scheduler.BeelzebubTrials.MinLevel)
	}
	if !cfg.Scheduler.BeelzebubTrials.AutoMoralAward {
		t.Errorf("expected auto_moral_award to be true")
	}
}

func TestConfig_InvalidFile(t *testing.T) {
	_, err := config.Load("non_existent_file.yaml")
	if err == nil {
		t.Error("expected error for non-existent file")
	}
}
