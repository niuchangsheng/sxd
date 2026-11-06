// Package web_test 验证 Web 控制台嵌入式前端托管、REST API 与日志缓冲区。
//
// @author Ateng
// @since 2026-10-08
package web_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"testing"
	"time"

	"sxd-pie-ng/internal/web"
)

type mockRoleProvider struct {
	roles []web.RoleInfo
}

func (m *mockRoleProvider) ListRoles() []web.RoleInfo {
	return m.roles
}

func TestWebServer_LifecycleAndAPIs(t *testing.T) {
	logBuf := web.NewLogBuffer(10)
	logger := slog.New(logBuf)
	logger.Info("系统初始化完成", "module", "main")
	logger.Warn("测试告警条目", "code", 101)

	provider := &mockRoleProvider{
		roles: []web.RoleInfo{
			{
				RoleID:   "role-101",
				RoleName: "测试剑灵角色",
				ServerID: "s1",
				State:    "Active",
			},
		},
	}

	srv := web.NewServer("127.0.0.1", 0, provider, logBuf, "v1.0.0-test")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := srv.Start(ctx); err != nil {
		t.Fatalf("Server.Start failed: %v", err)
	}
	defer func() {
		shutdownCtx, sCancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer sCancel()
		_ = srv.Shutdown(shutdownCtx)
	}()

	addr := srv.Addr()
	if addr == "" {
		t.Fatal("expected non-empty bound address")
	}

	baseURL := fmt.Sprintf("http://%s", addr)

	// 1. 验证静态 HTML 资源内嵌托管
	resp, err := http.Get(baseURL + "/")
	if err != nil {
		t.Fatalf("GET / failed: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200 OK for /, got %d", resp.StatusCode)
	}
	if !strings.Contains(string(body), "sxd-pie-ng") {
		t.Errorf("expected HTML body to contain 'sxd-pie-ng'")
	}

	// 2. 验证 /api/health 健康检查接口
	healthResp, err := http.Get(baseURL + "/api/health")
	if err != nil {
		t.Fatalf("GET /api/health failed: %v", err)
	}
	defer healthResp.Body.Close()
	var healthData map[string]any
	_ = json.NewDecoder(healthResp.Body).Decode(&healthData)
	if healthData["status"] != "ok" || healthData["version"] != "v1.0.0-test" {
		t.Errorf("unexpected health response: %+v", healthData)
	}

	// 3. 验证 /api/roles 角色列表接口
	rolesResp, err := http.Get(baseURL + "/api/roles")
	if err != nil {
		t.Fatalf("GET /api/roles failed: %v", err)
	}
	defer rolesResp.Body.Close()
	var rolesData []web.RoleInfo
	_ = json.NewDecoder(rolesResp.Body).Decode(&rolesData)
	if len(rolesData) != 1 || rolesData[0].RoleID != "role-101" {
		t.Errorf("unexpected roles response: %+v", rolesData)
	}

	// 4. 验证 /api/logs 实时日志接口
	logsResp, err := http.Get(baseURL + "/api/logs")
	if err != nil {
		t.Fatalf("GET /api/logs failed: %v", err)
	}
	defer logsResp.Body.Close()
	var logsData []web.LogEntry
	_ = json.NewDecoder(logsResp.Body).Decode(&logsData)
	if len(logsData) < 2 {
		t.Fatalf("expected at least 2 log entries, got %d", len(logsData))
	}
	if logsData[0].Level != "INFO" || logsData[1].Level != "WARN" {
		t.Errorf("unexpected logs levels: %+v", logsData)
	}
}

func TestLogBuffer_Capacity(t *testing.T) {
	buf := web.NewLogBuffer(3)
	logger := slog.New(buf)

	logger.Info("1")
	logger.Info("2")
	logger.Info("3")
	logger.Info("4")
	logger.Info("5")

	logs := buf.GetLogs()
	if len(logs) != 3 {
		t.Fatalf("expected 3 entries after overflow, got %d", len(logs))
	}
	if logs[0].Message != "3" || logs[1].Message != "4" || logs[2].Message != "5" {
		t.Errorf("expected [3, 4, 5], got %+v", logs)
	}
}

type mockRoutineProvider struct {
	routines   []web.RoutineStatus
	lastRoleID string
}

func (m *mockRoutineProvider) ListRoutines(roleID string) []web.RoutineStatus {
	return m.routines
}

func (m *mockRoutineProvider) ToggleRoutine(roleID, id string, enabled bool) error {
	m.lastRoleID = roleID
	for i := range m.routines {
		if m.routines[i].ID == id {
			m.routines[i].Enabled = enabled
			return nil
		}
	}
	return nil
}

func (m *mockRoutineProvider) ApplyPreset(roleID, preset string) error {
	m.lastRoleID = roleID
	for i := range m.routines {
		if preset == "all" {
			m.routines[i].Enabled = true
		} else if preset == "none" {
			m.routines[i].Enabled = false
		}
	}
	return nil
}

func TestWebServer_RoutinesAPI(t *testing.T) {
	mockP := &mockRoutineProvider{
		routines: []web.RoutineStatus{
			{ID: "herb_garden", Name: "药园种植", Domain: "farming", Enabled: true, State: "Active"},
			{ID: "arena", Name: "竞技场", Domain: "pvp", Enabled: false, State: "Disabled"},
		},
	}

	srv := web.NewServer("127.0.0.1", 0, nil, nil, "v1.0.0")
	srv.SetRoutineProvider(mockP)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := srv.Start(ctx); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer func() {
		_ = srv.Shutdown(context.Background())
	}()

	baseURL := fmt.Sprintf("http://%s", srv.Addr())

	// 1. GET /api/routines
	resp, err := http.Get(baseURL + "/api/routines")
	if err != nil {
		t.Fatalf("GET /api/routines failed: %v", err)
	}
	defer resp.Body.Close()

	var list []web.RoutineStatus
	_ = json.NewDecoder(resp.Body).Decode(&list)
	if len(list) != 2 {
		t.Fatalf("expected 2 routines, got %d", len(list))
	}

	// 2. POST /api/routines (Toggle)
	toggleBody := strings.NewReader(`{"id": "arena", "enabled": true}`)
	postResp, err := http.Post(baseURL+"/api/routines", "application/json", toggleBody)
	if err != nil {
		t.Fatalf("POST /api/routines failed: %v", err)
	}
	postResp.Body.Close()

	if !mockP.routines[1].Enabled {
		t.Error("expected arena to be enabled after toggle")
	}

	// 3. POST /api/routines (Preset)
	presetBody := strings.NewReader(`{"preset": "none"}`)
	presetResp, err := http.Post(baseURL+"/api/routines", "application/json", presetBody)
	if err != nil {
		t.Fatalf("POST /api/routines preset failed: %v", err)
	}
	presetResp.Body.Close()

	if mockP.routines[0].Enabled || mockP.routines[1].Enabled {
		t.Error("expected all routines to be disabled after preset none")
	}

	// 4. POST /api/routines 指定 role_id 时透传给按角色隔离的管理器
	roleToggleBody := strings.NewReader(`{"role_id": "s0-遁一", "id": "herb_garden", "enabled": true}`)
	roleResp, err := http.Post(baseURL+"/api/routines", "application/json", roleToggleBody)
	if err != nil {
		t.Fatalf("POST /api/routines with role_id failed: %v", err)
	}
	roleResp.Body.Close()
	if mockP.lastRoleID != "s0-遁一" {
		t.Errorf("expected role_id to be forwarded, got %q", mockP.lastRoleID)
	}

	// 5. GET /api/routines?role_id=... 也透传角色过滤
	roleListResp, err := http.Get(baseURL + "/api/routines?role_id=s0-%E9%81%81%E4%B8%80")
	if err != nil {
		t.Fatalf("GET /api/routines?role_id failed: %v", err)
	}
	defer roleListResp.Body.Close()
	var roleList []web.RoutineStatus
	_ = json.NewDecoder(roleListResp.Body).Decode(&roleList)
	if len(roleList) != 2 {
		t.Fatalf("expected 2 routines for role query, got %d", len(roleList))
	}
}
