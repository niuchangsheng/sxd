// Package social 提供仙盟、圣盟、住宅与好友互动的帮派社交自动化玩法。
package social

import (
	"context"
	"errors"
	"net"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"sxd-pie-ng/internal/client"
	"sxd-pie-ng/internal/protocol"
	"sxd-pie-ng/internal/scheduler"
)

// chatRoomMockOptions 全网聊天室 mock 的可变行为。
type chatRoomMockOptions struct {
	loginResult uint8
	speakResult uint8
}

// setupChatRoomMock 搭建主服 (336/0、336/11) + 聊天服 (336/12、336/20) 双管道 mock。
// 返回会话、主服请求计数、聊天服收到的发言载荷。
func setupChatRoomMock(t *testing.T, opt chatRoomMockOptions) (*client.RoleSession, *atomic.Int32, <-chan []byte) {
	t.Helper()
	mainServer, mainClient := net.Pipe()
	chatServer, chatClient := net.Pipe()
	t.Cleanup(func() {
		_ = mainServer.Close()
		_ = mainClient.Close()
		_ = chatServer.Close()
		_ = chatClient.Close()
	})

	var mainCalls atomic.Int32
	speakCh := make(chan []byte, 1)

	go func() {
		for {
			pkt, err := protocol.ReadPacket(mainServer)
			if err != nil {
				return
			}
			switch pkt.ActionID {
			case protocol.ActionIDServerChatRoomRecentRooms:
				mainCalls.Add(1)
				_ = protocol.WritePacket(mainServer, protocol.NewPacket(
					protocol.ActionIDServerChatRoomRecentRooms, []byte{0x09, 0x00, 0x03, 0x01, 0x02, 0x08}))
			case protocol.ActionIDServerChatRoomLoginCode:
				mainCalls.Add(1)
				w := protocol.NewWriter()
				w.WriteUint8(1)
				w.WriteString("s0@chat.test")
				w.WriteString("s0")
				w.WriteString("verycd")
				w.WriteInt32(1794726490)
				w.WriteString("code123")
				w.WriteString("chat.test")
				w.WriteInt32(8205)
				_ = protocol.WritePacket(mainServer, protocol.NewPacket(protocol.ActionIDServerChatRoomLoginCode, w.Bytes()))
			}
		}
	}()

	go func() {
		for {
			pkt, err := protocol.ReadPacket(chatServer)
			if err != nil {
				return
			}
			switch pkt.ActionID {
			case protocol.ActionIDServerChatRoomLogin:
				loginResult := opt.loginResult
				if loginResult == 0 {
					loginResult = protocol.ServerChatRoomResultSuccess
				}
				w := protocol.NewWriter()
				w.WriteUint8(loginResult)
				w.WriteInt32(36047)
				_ = protocol.WritePacket(chatServer, protocol.NewPacket(protocol.ActionIDServerChatRoomLogin, w.Bytes()))
			case protocol.ActionIDServerChatRoomSpeak:
				select {
				case speakCh <- append([]byte(nil), pkt.Payload...):
				default:
				}
				result := opt.speakResult
				if result == 0 {
					result = protocol.ServerChatRoomResultSuccess
				}
				w := protocol.NewWriter()
				w.WriteInt32(0)
				w.WriteUint8(result)
				_ = protocol.WritePacket(chatServer, protocol.NewPacket(protocol.ActionIDServerChatRoomSpeak, w.Bytes()))
			}
		}
	}()

	sess := client.NewRoleSession(client.SessionConfig{
		RoleID:     "role_chat_room_test",
		RoleName:   "聊天室测试",
		ServerAddr: "main-chat-pipe",
		Dialer: func(ctx context.Context, network, addr string) (net.Conn, error) {
			if addr == "main-chat-pipe" {
				return mainClient, nil
			}
			return chatClient, nil
		},
		Authenticator: func(*client.RoleSession) error { return nil },
	})
	if err := sess.Start(context.Background()); err != nil {
		t.Fatalf("启动会话失败: %v", err)
	}
	t.Cleanup(sess.Close)
	deadline := time.Now().Add(2 * time.Second)
	for sess.State() != client.StateActive && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if sess.State() != client.StateActive {
		t.Fatalf("会话未激活: %s", sess.State())
	}
	return sess, &mainCalls, speakCh
}

func withTempChatRoomState(t *testing.T) {
	t.Helper()
	old := serverChatRoomStatePath
	serverChatRoomStatePath = filepath.Join(t.TempDir(), "chat_room_state.json")
	t.Cleanup(func() { serverChatRoomStatePath = old })
}

// TestServerChatRoomRoutine_SendsExpressionOncePerDay 验证成功发送表情后记账，
// 同一天第二次执行不再请求主服。
func TestServerChatRoomRoutine_SendsExpressionOncePerDay(t *testing.T) {
	withTempChatRoomState(t)
	sess, mainCalls, speakCh := setupChatRoomMock(t, chatRoomMockOptions{})

	execCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := NewServerChatRoomRoutine().Execute(execCtx, sess, nil); !errors.Is(err, scheduler.ErrAttemptsExhausted) {
		t.Fatalf("成功后应返回 ErrAttemptsExhausted, 实际 %v", err)
	}
	var speakPayload []byte
	select {
	case speakPayload = <-speakCh:
	case <-time.After(time.Second):
		t.Fatalf("未收到 336/20 发言请求")
	}
	want := []byte{0x00, 0x00, 0x00, 0x01, '1', 0x00, 0x02, '1', '6'}
	if string(speakPayload) != string(want) {
		t.Fatalf("发言载荷 = % X, want % X", speakPayload, want)
	}
	if got := mainCalls.Load(); got != 2 {
		t.Fatalf("主服应调用 336/0 与 336/11 各一次, 实际 %d", got)
	}

	// 同一天再次执行：命中记账，直接挂起且不再请求主服。
	before := mainCalls.Load()
	if err := NewServerChatRoomRoutine().Execute(execCtx, sess, nil); err != scheduler.ErrAttemptsExhausted {
		t.Fatalf("同一天再次执行应返回 ErrAttemptsExhausted, 实际 %v", err)
	}
	if after := mainCalls.Load(); after != before {
		t.Fatalf("同一天不应再请求主服, 前 %d 后 %d", before, after)
	}
}

// TestServerChatRoomRoutine_SpeakTimeDiffSuspends 验证结果码 25 (TIME_DIFF)
// 记账后挂起至次日，避免重复尝试。
func TestServerChatRoomRoutine_SpeakTimeDiffSuspends(t *testing.T) {
	withTempChatRoomState(t)
	sess, _, _ := setupChatRoomMock(t, chatRoomMockOptions{speakResult: protocol.ServerChatRoomResultTimeDiff})

	execCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := NewServerChatRoomRoutine().Execute(execCtx, sess, nil); !errors.Is(err, scheduler.ErrAttemptsExhausted) {
		t.Fatalf("TIME_DIFF 应返回 ErrAttemptsExhausted, 实际 %v", err)
	}
	if !serverChatRoomSentToday(sess.RoleID()) {
		t.Fatalf("TIME_DIFF 后应记账今日已发送")
	}
}

// TestServerChatRoomRoutine_LoginRejectedCooldown 验证聊天服登录被拒时返回冷却。
func TestServerChatRoomRoutine_LoginRejectedCooldown(t *testing.T) {
	withTempChatRoomState(t)
	sess, _, _ := setupChatRoomMock(t, chatRoomMockOptions{loginResult: protocol.ServerChatRoomResultFailed})

	execCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	err := NewServerChatRoomRoutine().Execute(execCtx, sess, nil)
	var cd *scheduler.CooldownError
	if !errors.As(err, &cd) {
		t.Fatalf("登录被拒应返回 CooldownError, 实际 %v", err)
	}
	if serverChatRoomSentToday(sess.RoleID()) {
		t.Fatalf("登录被拒不应记账已发送")
	}
}

func asCooldown(err error, target **scheduler.CooldownError) bool {
	for err != nil {
		if cd, ok := err.(*scheduler.CooldownError); ok {
			*target = cd
			return true
		}
		u, ok := err.(interface{ Unwrap() error })
		if !ok {
			return false
		}
		err = u.Unwrap()
	}
	return false
}
