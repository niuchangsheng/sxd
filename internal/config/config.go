// Package config 提供系统配置读取、YAML 反序列化、环境变量展开与参数校验。
//
// @author Ateng
// @since 2026-10-08
package config

import (
	"fmt"
	"os"
	"regexp"

	"gopkg.in/yaml.v3"
)

// Config 全局顶层配置结构体
type Config struct {
	Server    ServerConfig    `yaml:"server"`
	Accounts  []AccountConfig `yaml:"accounts"`
	Scheduler SchedulerConfig `yaml:"scheduler"`
}

// ServerConfig Web 控制台服务配置
type ServerConfig struct {
	Host string `yaml:"host"`
	Port int    `yaml:"port"`
}

// AccountConfig 平台通行证账号配置
type AccountConfig struct {
	Platform string       `yaml:"platform"`
	Username string       `yaml:"username"`
	Password string       `yaml:"password"`
	Cookie   string       `yaml:"cookie,omitempty"`
	Roles    []RoleConfig `yaml:"roles"`
}

// RoleConfig 单个游戏角色配置
type RoleConfig struct {
	ServerID   string `yaml:"server_id"`
	RoleName   string `yaml:"role_name"`
	AutoLogin  bool   `yaml:"auto_login"`
	ServerAddr string `yaml:"server_addr"`
	// 本服主网关认证凭据 (Mod_Player_Base 0x0000)
	Code string `yaml:"code"`
	Time int32  `yaml:"time"`
	Hash string `yaml:"hash"`
	// 跨服网关认证凭据 (Mod_StLogin_Base 0x005E)
	Time1 int32  `yaml:"time1"`
	Hash1 string `yaml:"hash1"`
}

// SchedulerConfig 任务调度器与防封配置
type SchedulerConfig struct {
	Jitter    JitterConfig           `yaml:"jitter"`
	Preset    string                 `yaml:"preset"`
	CronTimes []string               `yaml:"cron_times"`
	Routines  map[string]bool        `yaml:"routines"`
	Expert    map[string]interface{} `yaml:"expert"`
}

// JitterConfig 抖动上下限配置 (秒)
type JitterConfig struct {
	MinSeconds float64 `yaml:"min_seconds"`
	MaxSeconds float64 `yaml:"max_seconds"`
}

var envVarRegex = regexp.MustCompile(`\$\{([A-Za-z0-9_]+)(:-([^}]*))?\}`)

func expandEnv(content string) string {
	return envVarRegex.ReplaceAllStringFunc(content, func(match string) string {
		submatches := envVarRegex.FindStringSubmatch(match)
		if len(submatches) >= 2 {
			varName := submatches[1]
			val := os.Getenv(varName)
			if val != "" {
				return val
			}
			if len(submatches) >= 4 && submatches[2] != "" {
				return submatches[3]
			}
		}
		return match
	})
}

// Load 从指定路径加载并解析 YAML 配置文件，自动展开环境变量并填充默认值。
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("config: failed to read file: %w", err)
	}

	expanded := expandEnv(string(data))

	var cfg Config
	if err := yaml.Unmarshal([]byte(expanded), &cfg); err != nil {
		return nil, fmt.Errorf("config: failed to parse YAML: %w", err)
	}

	// 补齐并校验默认值
	if cfg.Server.Host == "" {
		cfg.Server.Host = "127.0.0.1"
	}
	if cfg.Server.Port <= 0 {
		cfg.Server.Port = 8080
	}

	if cfg.Scheduler.Jitter.MinSeconds <= 0 {
		cfg.Scheduler.Jitter.MinSeconds = 1.0
	}
	if cfg.Scheduler.Jitter.MaxSeconds <= 0 {
		cfg.Scheduler.Jitter.MaxSeconds = 3.0
	}
	if cfg.Scheduler.Jitter.MinSeconds > cfg.Scheduler.Jitter.MaxSeconds {
		cfg.Scheduler.Jitter.MinSeconds, cfg.Scheduler.Jitter.MaxSeconds =
			cfg.Scheduler.Jitter.MaxSeconds, cfg.Scheduler.Jitter.MinSeconds
	}

	if cfg.Scheduler.Routines == nil {
		cfg.Scheduler.Routines = make(map[string]bool)
	}

	if cfg.Scheduler.Preset == "" {
		cfg.Scheduler.Preset = "recommended"
	}

	if len(cfg.Scheduler.CronTimes) == 0 {
		cfg.Scheduler.CronTimes = []string{
			"00:00", "06:02", "08:02", "12:00", "16:05", "18:01", "20:01", "22:01",
		}
	}

	if cfg.Scheduler.Expert == nil {
		cfg.Scheduler.Expert = make(map[string]interface{})
	}

	if cfg.Accounts == nil {
		cfg.Accounts = []AccountConfig{}
	}

	return &cfg, nil
}
