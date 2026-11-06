// Package web 提供基于 Go 内嵌文件系统（go:embed）的 Web 控制台服务，支持本地配置与实时日志监视。
//
// @author Ateng
// @since 2026-10-08
package web

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"sync"
	"time"
)

//go:embed static/*
var staticFS embed.FS

// RoleInfo 角色状态展示结构体
type RoleInfo struct {
	RoleID   string `json:"role_id"`
	RoleName string `json:"role_name"`
	ServerID string `json:"server_id"`
	State    string `json:"state"`
}

// RoleProvider 角色信息提供器接口
type RoleProvider interface {
	ListRoles() []RoleInfo
}

// RoleRoutineStatus 单个角色下某个玩法的运行状态 (多区服控制台展示用)。
type RoleRoutineStatus struct {
	RoleID       string `json:"role_id"`
	ServerID     string `json:"server_id"`
	Enabled      bool   `json:"enabled"`
	State        string `json:"state"` // "Active", "Cooling", "Disabled"
	CoolingUntil string `json:"cooling_until,omitempty"`
}

// RoutineStatus 玩法运行与配置状态
type RoutineStatus struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	Domain       string `json:"domain"`
	Schedule     string `json:"schedule"`
	Description  string `json:"description"`
	Enabled      bool   `json:"enabled"`
	State        string `json:"state"` // "Active", "Cooling", "Disabled"
	CoolingUntil string `json:"cooling_until,omitempty"`
	// Roles 按角色拆分的明细；仅聚合查询 (role_id 为空) 时返回。
	Roles []RoleRoutineStatus `json:"roles,omitempty"`
}

// RoutineProvider 玩法管理器接口。
//
// roleID 为空表示对所有角色生效 (全局开关)；指定 roleID 时只影响该角色，
// 用于多区服/多角色场景下互不干扰的独立开关。
type RoutineProvider interface {
	ListRoutines(roleID string) []RoutineStatus
	ToggleRoutine(roleID, id string, enabled bool) error
	ApplyPreset(roleID, preset string) error
}

// Server Web 控制台与管理 API 服务
type Server struct {
	host            string
	port            int
	roleProvider    RoleProvider
	routineProvider RoutineProvider
	logBuffer       *LogBuffer
	version         string

	server   *http.Server
	listener net.Listener
	mu       sync.Mutex
	addr     string
}

// NewServer 创建新的 Web 控制台服务实例。
func NewServer(
	host string,
	port int,
	roleProvider RoleProvider,
	logBuffer *LogBuffer,
	version string,
) *Server {
	if host == "" {
		host = "127.0.0.1"
	}
	if logBuffer == nil {
		logBuffer = NewLogBuffer(200)
	}
	if version == "" {
		version = "dev"
	}

	return &Server{
		host:         host,
		port:         port,
		roleProvider: roleProvider,
		logBuffer:    logBuffer,
		version:      version,
	}
}

// Addr 返回服务器绑定的实际网络监听地址。
func (s *Server) Addr() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.addr
}

// SetRoutineProvider 动态注入玩法管理器。
func (s *Server) SetRoutineProvider(p RoutineProvider) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.routineProvider = p
}

// Start 启动 Web 控制台 HTTP 监听。
func (s *Server) Start(ctx context.Context) error {
	mux := http.NewServeMux()

	// 1. 挂载静态前端资源
	subFS, err := fs.Sub(staticFS, "static")
	if err != nil {
		return fmt.Errorf("web: failed to load embedded static fs: %w", err)
	}
	mux.Handle("/", http.FileServer(http.FS(subFS)))

	// 2. 挂载 REST API
	mux.HandleFunc("/api/health", s.handleHealth)
	mux.HandleFunc("/api/roles", s.handleRoles)
	mux.HandleFunc("/api/logs", s.handleLogs)
	mux.HandleFunc("/api/routines", s.handleRoutines)

	listenAddr := fmt.Sprintf("%s:%d", s.host, s.port)
	ln, err := net.Listen("tcp", listenAddr)
	if err != nil {
		return fmt.Errorf("web: failed to listen on %s: %w", listenAddr, err)
	}

	s.mu.Lock()
	s.listener = ln
	s.addr = ln.Addr().String()
	s.server = &http.Server{
		Handler:      mux,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
	}
	s.mu.Unlock()

	slog.Info("Web 控制台已启动", "url", fmt.Sprintf("http://%s", s.addr))

	go func() {
		if err := s.server.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("Web 控制台服务异常", "error", err)
		}
	}()

	return nil
}

// Shutdown 优雅关闭 Web 控制台 HTTP 服务。
func (s *Server) Shutdown(ctx context.Context) error {
	s.mu.Lock()
	srv := s.server
	s.mu.Unlock()

	if srv == nil {
		return nil
	}
	return srv.Shutdown(ctx)
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	resp := map[string]any{
		"status":  "ok",
		"version": s.version,
		"time":    time.Now().Format(time.RFC3339),
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleRoles(w http.ResponseWriter, r *http.Request) {
	var roles []RoleInfo
	if s.roleProvider != nil {
		roles = s.roleProvider.ListRoles()
	}
	if roles == nil {
		roles = []RoleInfo{}
	}
	writeJSON(w, http.StatusOK, roles)
}

func (s *Server) handleLogs(w http.ResponseWriter, r *http.Request) {
	logs := s.logBuffer.GetLogs()
	writeJSON(w, http.StatusOK, logs)
}

func (s *Server) handleRoutines(w http.ResponseWriter, r *http.Request) {
	if s.routineProvider == nil {
		writeJSON(w, http.StatusOK, []RoutineStatus{})
		return
	}

	if r.Method == http.MethodGet {
		routines := s.routineProvider.ListRoutines(r.URL.Query().Get("role_id"))
		if routines == nil {
			routines = []RoutineStatus{}
		}
		writeJSON(w, http.StatusOK, routines)
		return
	}

	if r.Method == http.MethodPost {
		var req struct {
			RoleID  string `json:"role_id"`
			ID      string `json:"id"`
			Enabled *bool  `json:"enabled"`
			Preset  string `json:"preset"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid json payload"})
			return
		}

		if req.Preset != "" {
			if err := s.routineProvider.ApplyPreset(req.RoleID, req.Preset); err != nil {
				writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
				return
			}
		} else if req.ID != "" && req.Enabled != nil {
			if err := s.routineProvider.ToggleRoutine(req.RoleID, req.ID, *req.Enabled); err != nil {
				writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
				return
			}
		}

		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
		return
	}

	w.WriteHeader(http.StatusMethodNotAllowed)
}

func writeJSON(w http.ResponseWriter, code int, data any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(data)
}
