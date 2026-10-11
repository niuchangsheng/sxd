//go:build probe

// 目标协议: [Module 0, Action 0] player_login & [Module 0, Action 23] heartbeat
// 基准对齐: 对齐官方微端与 ADR 0014，支持通过本地环境变量或凭据直注验证网关长连接握手与场景同步
// 运行命令: go run -tags=probe test/probes/gateway_handshake.go
// 预期表现: TCP 连接成功后自动发送 Module 0 握手，断言 ResultCode=4 成功并输出角色资产，维持周期性心跳保活
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"sxd-pie-ng/internal/client"
	"sxd-pie-ng/internal/protocol"
)

func main() {
	serverAddr := os.Getenv("SXD_PROBE_SERVER")
	if serverAddr == "" {
		serverAddr = "127.0.0.1:8381"
	}
	username := os.Getenv("SXD_PROBE_USER")
	if username == "" {
		username = "TEST_USER"
	}
	hash := os.Getenv("SXD_PROBE_HASH")
	if hash == "" {
		hash = "TEST_HASH_TOKEN"
	}

	fmt.Println("==================================================")
	fmt.Println("神仙道重构工程 - 网关长连接与 Module 0 协议探针")
	fmt.Printf("目标网关: %s\n", serverAddr)
	fmt.Printf("测试账号: %s\n", username)
	fmt.Println("==================================================")

	cfg := client.SessionConfig{
		RoleID:            "probe-role-01",
		RoleName:          "协议逆向探针角色",
		ServerAddr:        serverAddr,
		Username:          username,
		Hash:              hash,
		Platform:          "疯玩",
		HeartbeatInterval: 2 * time.Second,
	}

	session := client.NewRoleSession(cfg)

	// 注册主动推送与场景监听
	session.RegisterHandler(protocol.ActionIDPlayerUpdateData, func(p *protocol.Packet) {
		slog.Info("[探针] 收到服务器属性推送 (ActionIDPlayerUpdateData)", "len", len(p.Payload))
	})
	session.RegisterHandler(protocol.ActionIDPlayerHeartbeat, func(p *protocol.Packet) {
		slog.Info("[探针] 收到服务器心跳应答 (ActionIDPlayerHeartbeat)", "hex", fmt.Sprintf("%x", p.Payload))
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := session.Start(ctx); err != nil {
		slog.Error("会话启动失败", "error", err)
		return
	}

	// 捕获 Ctrl+C 优雅退出
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)

	fmt.Println("探针正在运行中 (按 Ctrl+C 退出)...")
	select {
	case <-sigCh:
		fmt.Println("\n正在停止探针...")
	case <-time.After(10 * time.Second):
		fmt.Println("探针默认演练周期 (10s) 结束，正常退出。")
	}

	session.Close()
	fmt.Println("探针已安全退出。")
}
