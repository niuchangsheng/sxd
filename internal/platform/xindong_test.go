// Package platform 提供多运营平台 Web 自动化认证、凭据换取与区服标识归一化驱动层。
//
// @author Ateng
// @since 2026-10-11
package platform

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestNormalizeServerID_Xindong(t *testing.T) {
	tests := []struct {
		platform string
		input    string
		expected string
	}{
		{"xindong", "s0", "s0"},
		{"xindong", "S0", "s0"},
		{"xd", "0", "s0"},
		{"xindong", "s10", "s10"},
		{"xd", "S10", "s10"},
	}

	for _, tt := range tests {
		got := NormalizeServerID(tt.platform, tt.input)
		if got != tt.expected {
			t.Errorf("NormalizeServerID(%q, %q) = %q; want %q", tt.platform, tt.input, got, tt.expected)
		}
	}
}

func TestParseFlashVarsMap(t *testing.T) {
	mockHTML := `
    <script type="text/javascript">
    var flashVars
        = "server_name=s0.sxd.xd.com"
        + "&player_name=6427992"
        + "&hash_code=d3781221b7495260fafda5a50114a4b0"
        + "&time=1791648403"
        + "&ip=9x050.sxdweb.xd.com"
        + "&port=8200"
        + "&server_id=s0"
        + "&server_url=http%3A%2F%2F9x050.sxdweb.xd.com%2Fs0%2F"
        + "&id_card=321281198705258097"
        + "&regdate=1333029233";
    </script>
    `

	m := ParseFlashVarsMap(mockHTML)
	if m["player_name"] != "6427992" {
		t.Errorf("expected player_name 6427992, got %q", m["player_name"])
	}
	if m["hash_code"] != "d3781221b7495260fafda5a50114a4b0" {
		t.Errorf("expected hash_code d3781221b7495260fafda5a50114a4b0, got %q", m["hash_code"])
	}
	if m["ip"] != "9x050.sxdweb.xd.com" {
		t.Errorf("expected ip 9x050.sxdweb.xd.com, got %q", m["ip"])
	}
	if m["port"] != "8200" {
		t.Errorf("expected port 8200, got %q", m["port"])
	}
	if m["id_card"] != "321281198705258097" {
		t.Errorf("expected id_card 321281198705258097, got %q", m["id_card"])
	}
	if m["regdate"] != "1333029233" {
		t.Errorf("expected regdate 1333029233, got %q", m["regdate"])
	}
}

func TestXindongAuthenticator_Login_Success(t *testing.T) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/games/play":
			if r.URL.Query().Get("server") != "S0" {
				http.Error(w, "invalid server parameter", http.StatusBadRequest)
				return
			}
			// 模拟 302 重定向到区服页面
			http.Redirect(w, r, "/game_page", http.StatusFound)
		case "/game_page":
			// 返回包含 flashVars 的游戏 HTML
			w.WriteHeader(http.StatusOK)
			fmt.Fprint(w, `
                var flashVars = "server_name=s0.sxd.xd.com"
                + "&player_name=6427992"
                + "&hash_code=test_hash_123456"
                + "&time=1791648403"
                + "&ip=9x050.sxdweb.xd.com"
                + "&port=8200"
                + "&server_id=s0"
                + "&id_card=321281198705258097";
            `)
		default:
			http.NotFound(w, r)
		}
	}))
	defer mockServer.Close()

	auth, err := NewXindongAuthenticator(
		WithXindongHTTPClient(mockServer.Client()),
		WithXindongPlayURL(mockServer.URL+"/games/play"),
		WithXindongCookie("A1=mock_a1; A2=mock_a2"),
	)
	if err != nil {
		t.Fatalf("failed to create xindong authenticator: %v", err)
	}

	ticket, err := auth.Login(context.Background(), "tester", "mock_pass", "s0")
	if err != nil {
		t.Fatalf("login failed: %v", err)
	}

	if ticket.Platform != "xindong" {
		t.Errorf("expected platform xindong, got %s", ticket.Platform)
	}
	if ticket.ServerID != "s0" {
		t.Errorf("expected server_id s0, got %s", ticket.ServerID)
	}
	if ticket.RoleName != "6427992" {
		t.Errorf("expected role_name 6427992, got %s", ticket.RoleName)
	}
	if ticket.GatewayURL != "9x050.sxdweb.xd.com:8200" {
		t.Errorf("expected gateway 9x050.sxdweb.xd.com:8200, got %s", ticket.GatewayURL)
	}
	if ticket.MainServer.Hash != "test_hash_123456" {
		t.Errorf("expected hash test_hash_123456, got %s", ticket.MainServer.Hash)
	}
	if ticket.MainServer.Time != 1791648403 {
		t.Errorf("expected time 1791648403, got %d", ticket.MainServer.Time)
	}
	if ticket.Extra["id_card"] != "321281198705258097" {
		t.Errorf("expected id_card in extra, got %v", ticket.Extra)
	}
}

func TestXindongAuthenticator_Login_Unauthenticated(t *testing.T) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 未登录时重定向到登录页
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, `<html><script src="/js/users/login_v5.js"></script></html>`)
	}))
	defer mockServer.Close()

	auth, err := NewXindongAuthenticator(
		WithXindongHTTPClient(mockServer.Client()),
		WithXindongPlayURL(mockServer.URL+"/games/play"),
	)
	if err != nil {
		t.Fatalf("failed to create xindong authenticator: %v", err)
	}

	_, err = auth.Login(context.Background(), "user", "pass", "s0")
	if err == nil {
		t.Fatal("expected error for unauthenticated page, got nil")
	}
}
