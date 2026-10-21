// Package platform 提供多运营平台 Web 自动化认证、凭据换取与区服标识归一化驱动层。
//
// @author Ateng
// @since 2026-10-11
package platform

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var (
	flashVarRegex = regexp.MustCompile(`(?:\+|var flashVars\s*=)?\s*["']&?([a-zA-Z0-9_]+)=([^"']*)["']`)
)

// XindongAuthenticator 提供心动游戏平台 (sxd.xd.com) 的原生 Web 凭据换取驱动。
type XindongAuthenticator struct {
	client     *http.Client
	playURL    string
	cookie     string
	defaultTTL time.Duration
}

// XindongOption 配置 XindongAuthenticator 的可选参数函数。
type XindongOption func(*XindongAuthenticator)

// WithXindongHTTPClient 允许外部注入自定义 HTTP 客户端 (例如单元测试 Mock Transport)。
func WithXindongHTTPClient(client *http.Client) XindongOption {
	return func(a *XindongAuthenticator) {
		a.client = client
	}
}

// WithXindongPlayURL 允许覆盖默认选服/入场 Web 端点。
func WithXindongPlayURL(playURL string) XindongOption {
	return func(a *XindongAuthenticator) {
		if playURL != "" {
			a.playURL = playURL
		}
	}
}

// WithXindongCookie 注入心动通行证会话 Cookie (例如 "A1=...; A2=...")。
func WithXindongCookie(cookie string) XindongOption {
	return func(a *XindongAuthenticator) {
		a.cookie = strings.TrimSpace(cookie)
	}
}

// NewXindongAuthenticator 创建并初始化心动平台认证驱动。
func NewXindongAuthenticator(opts ...XindongOption) (*XindongAuthenticator, error) {
	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, fmt.Errorf("xindong: failed to create cookie jar: %w", err)
	}

		auth := &XindongAuthenticator{
		client: &http.Client{
			Transport: &http.Transport{
				Proxy: nil, // 直连心动服务器，避免受本地代理 (如 7890) 干扰导致超时
			},
			Jar:     jar,
			Timeout: 15 * time.Second,
		},
		playURL:    "https://www.xd.com/games/play",
		defaultTTL: 90 * time.Minute,
	}

	for _, opt := range opts {
		opt(auth)
	}

	return auth, nil
}

// Platform 返回平台唯一标识 "xindong"。
func (a *XindongAuthenticator) Platform() string {
	return "xindong"
}

// SetCookie 设置/更新会话 Cookie 并同步注满 CookieJar。
func (a *XindongAuthenticator) SetCookie(cookie string) {
	a.cookie = strings.TrimSpace(cookie)
	if a.cookie != "" && a.client != nil && a.client.Jar != nil {
		targets := []string{
			"https://www.xd.com", "https://xd.com", "http://www.xd.com", "http://xd.com",
			"https://sxd.xd.com", "http://sxd.xd.com",
			"https://s0.sxd.xd.com", "http://s0.sxd.xd.com",
			"https://S0.sxd.xd.com", "http://S0.sxd.xd.com",
		}
		for _, tgt := range targets {
			if u, err := url.Parse(tgt); err == nil {
				a.injectCookies(u, a.cookie)
			}
		}
	}
}

// Login 执行心动平台入场跳转并换取目标区服的动态凭据。
func (a *XindongAuthenticator) Login(ctx context.Context, username, password, serverID string) (*Ticket, error) {
	// 0. 若未配置有效 Cookie 且提供了账号密码，优先尝试全自治登录换票流水线 (集成图形验证码 OCR)
	if a.playURL == "https://www.xd.com/games/play" && (a.cookie == "" || strings.Contains(a.cookie, "YOUR_A1_VALUE")) && username != "" && password != "" {
		if t, err := a.loginViaScript(ctx, username, password, serverID); err == nil && t != nil {
			return t, nil
		}
	}

	// 1. 规范化区服标识: 例如 "s0" 或 "0" -> "S0" (xd.com games/play 参数要求大写 S)
	serverNum := strings.TrimPrefix(serverID, "s")
	serverNum = strings.TrimPrefix(serverNum, "S")
	serverSlug := fmt.Sprintf("S%s", serverNum)
	if serverNum == "" {
		serverSlug = "S0"
	}

	// 2. 将注入的 Cookie 解析并写入 CookieJar (覆盖全子域与大小写区服)
	if a.cookie != "" && a.client.Jar != nil {
		targets := []string{
			"https://www.xd.com", "https://xd.com", "http://www.xd.com", "http://xd.com",
			"https://sxd.xd.com", "http://sxd.xd.com",
			fmt.Sprintf("https://%s.sxd.xd.com", strings.ToLower(serverSlug)),
			fmt.Sprintf("http://%s.sxd.xd.com", strings.ToLower(serverSlug)),
			fmt.Sprintf("https://%s.sxd.xd.com", serverSlug),
			fmt.Sprintf("http://%s.sxd.xd.com", serverSlug),
		}
		for _, tgt := range targets {
			if u, err := url.Parse(tgt); err == nil {
				a.injectCookies(u, a.cookie)
			}
		}
	}

	// 捕获跳转过程中 login_api.php 携带的凭据
	capturedParams := make(map[string]string)
	origCheckRedirect := a.client.CheckRedirect
	a.client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if a.cookie != "" {
			existing := req.Header.Get("Cookie")
			if existing == "" {
				req.Header.Set("Cookie", a.cookie)
			} else if !strings.Contains(existing, "A1=") {
				req.Header.Set("Cookie", existing+"; "+a.cookie)
			}
		}
		q := req.URL.Query()
		if q.Get("hash") != "" {
			capturedParams["hash_code"] = q.Get("hash")
			capturedParams["time"] = q.Get("time")
			capturedParams["player_name"] = q.Get("user")
			capturedParams["regdate"] = q.Get("regdate")
			capturedParams["id_card"] = q.Get("idcard")
			capturedParams["non_kid"] = q.Get("non_kid")
		}
		if len(via) >= 10 {
			return errors.New("stopped after 10 redirects")
		}
		return nil
	}
	defer func() {
		a.client.CheckRedirect = origCheckRedirect
	}()

	// 3. 构造进入游戏请求: https://www.xd.com/games/play?app=sxd&server=%s
	reqURL := fmt.Sprintf("%s?app=sxd&server=%s", a.playURL, serverSlug)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, fmt.Errorf("xindong: failed to create play request: %w", err)
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")
	req.Header.Set("Referer", "https://sxd.xd.com/")

	resp, err := a.client.Do(req)
	if err != nil {
		// 若因超时或断开而失败，但已在跳转链中捕获到了 hash_code，则以捕获的凭据自愈降级返回
		if capturedParams["hash_code"] != "" && capturedParams["player_name"] != "" {
			capturedParams["server_id"] = strings.ToLower(serverSlug)
			capturedParams["stage"] = "心动"
			capturedParams["client"] = "web"
			if strings.EqualFold(serverSlug, "s0") {
				capturedParams["ip"] = "9x050.sxdweb.xd.com"
				capturedParams["port"] = "8200"
			}
			return a.buildTicket(serverID, capturedParams), nil
		}
		return nil, fmt.Errorf("xindong: play request failed: %w", err)
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("xindong: failed to read response body: %w", err)
	}
	html := string(bodyBytes)

	// 4. 解析页面 FlashVars
	flashVars := ParseFlashVarsMap(html)
	if len(flashVars) == 0 || flashVars["hash_code"] == "" {
		// 若页面未含 flashVars，但重定向链已捕获凭据，合并降级
		if capturedParams["hash_code"] != "" {
			for k, v := range capturedParams {
				if flashVars[k] == "" {
					flashVars[k] = v
				}
			}
			if flashVars["ip"] == "" && strings.EqualFold(serverSlug, "s0") {
				flashVars["ip"] = "9x050.sxdweb.xd.com"
				flashVars["port"] = "8200"
			}
		}
	}

	// 5. 优先尝试换取官方微端 (pc_wd) 专属动态凭据
	if capturedParams["hash_code"] != "" && capturedParams["player_name"] != "" {
		wdURL := fmt.Sprintf("http://%s.sxd.xd.com/index_wd.php?idcard=%s&user=%s&non_kid=%s&time=%s&regdate=%s&hash=%s",
			strings.ToLower(serverSlug),
			url.QueryEscape(capturedParams["id_card"]),
			url.QueryEscape(capturedParams["player_name"]),
			url.QueryEscape(capturedParams["non_kid"]),
			url.QueryEscape(capturedParams["time"]),
			url.QueryEscape(capturedParams["regdate"]),
			url.QueryEscape(capturedParams["hash_code"]),
		)
		if wdReq, err := http.NewRequestWithContext(ctx, http.MethodGet, wdURL, nil); err == nil {
			wdReq.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")
			if wdResp, err := a.client.Do(wdReq); err == nil {
				defer wdResp.Body.Close()
				var wdRes struct {
					Code int                    `json:"code"`
					Data map[string]interface{} `json:"data"`
				}
				if json.NewDecoder(wdResp.Body).Decode(&wdRes) == nil && wdRes.Code == 1 && wdRes.Data != nil {
					for k, v := range wdRes.Data {
						if strVal, ok := v.(string); ok {
							flashVars[k] = strVal
						} else if numVal, ok := v.(float64); ok {
							flashVars[k] = fmt.Sprintf("%.0f", numVal)
						}
					}
					flashVars["client"] = "pc_wd"
				}
			}
		}
	}

	if len(flashVars) == 0 || flashVars["hash_code"] == "" {
		// 未能提取到凭据，若提供了账号密码，尝试全自治脚本换票
		if a.playURL == "https://www.xd.com/games/play" && username != "" && password != "" {
			if t, err := a.loginViaScript(ctx, username, password, serverID); err == nil && t != nil {
				return t, nil
			}
		}
		finalURL := resp.Request.URL.String()
		if strings.Contains(finalURL, "users/login") || strings.Contains(html, "login_v5.js") {
			return nil, errors.New("xindong: authentication required, please provide a valid cookie in config or verify credentials")
		}
		return nil, fmt.Errorf("xindong: failed to extract flashVars credentials from %s (length %d)", finalURL, len(html))
	}

	return a.buildTicket(serverID, flashVars), nil
}

type scriptLoginResult struct {
	Success   bool              `json:"success"`
	Cookies   string            `json:"cookies"`
	FlashVars map[string]string `json:"flashVars"`
	Error     string            `json:"error"`
}

func (a *XindongAuthenticator) loginViaScript(ctx context.Context, username, password, serverID string) (*Ticket, error) {
	scriptPaths := []string{
		"scripts/xd_login.py",
		"../scripts/xd_login.py",
		"../../scripts/xd_login.py",
	}
	scriptPath := ""
	for _, p := range scriptPaths {
		if _, err := os.Stat(p); err == nil {
			scriptPath = p
			break
		}
	}
	if scriptPath == "" {
		return nil, errors.New("xindong: auto-login script scripts/xd_login.py not found")
	}

	pyBins := []string{"python3", "python"}
	var lastErr error
	for _, py := range pyBins {
		cmd := exec.CommandContext(ctx, py, scriptPath,
			"--username", username,
			"--password", password,
			"--server", serverID,
			"--cookie", a.cookie,
		)
		out, err := cmd.Output()
		if err != nil {
			if ee, ok := err.(*exec.ExitError); ok {
				if len(ee.Stderr) > 0 {
					lastErr = fmt.Errorf("%s: %s", err, strings.TrimSpace(string(ee.Stderr)))
				} else {
					lastErr = err
				}
			} else {
				lastErr = err
				continue
			}
		}

		var res scriptLoginResult
		if err := json.Unmarshal(out, &res); err != nil {
			lastErr = err
			continue
		}

		if res.Success && len(res.FlashVars) > 0 && res.FlashVars["hash_code"] != "" {
			if res.Cookies != "" {
				a.cookie = res.Cookies
				if a.client.Jar != nil {
					if xdURL, err := url.Parse("https://www.xd.com"); err == nil {
						a.injectCookies(xdURL, a.cookie)
					}
					if sxdURL, err := url.Parse("http://sxd.xd.com"); err == nil {
						a.injectCookies(sxdURL, a.cookie)
					}
				}
			}
			return a.buildTicket(serverID, res.FlashVars), nil
		}
		if res.Error != "" {
			lastErr = errors.New(res.Error)
		}
	}
	if lastErr != nil {
		return nil, fmt.Errorf("xindong: script login failed: %w", lastErr)
	}
	return nil, errors.New("xindong: script login failed without explicit error")
}

func (a *XindongAuthenticator) buildTicket(serverID string, flashVars map[string]string) *Ticket {
	host := flashVars["ip"]
	port := flashVars["port"]
	if port == "" {
		port = "8200"
	}
	gatewayURL := ""
	if host != "" {
		gatewayURL = fmt.Sprintf("%s:%s", host, port)
	}

	playerName := flashVars["player_name"]
	hashCode := flashVars["hash_code"]
	timeStr := flashVars["time"]
	var timeVal int32
	if t, err := strconv.ParseInt(timeStr, 10, 32); err == nil {
		timeVal = int32(t)
	} else {
		timeVal = int32(time.Now().Unix())
	}

	return &Ticket{
		Platform:    "xindong",
		ServerID:    NormalizeServerID("xindong", serverID),
		RawServerID: serverID,
		RoleName:    playerName,
		GatewayURL:  gatewayURL,
		MainServer: MainServerTicket{
			Code: playerName,
			Time: timeVal,
			Hash: hashCode,
		},
		CrossServer: CrossServerTicket{
			Time1: timeVal,
			Hash1: hashCode,
		},
		CreatedAt: time.Now(),
		ExpiresAt: time.Now().Add(a.defaultTTL),
		Extra:     flashVars,
	}
}

// injectCookies 将字符串形式的 Cookie (如 "A1=xxx; A2=yyy") 注入到对应 URL 的 CookieJar 中。
func (a *XindongAuthenticator) injectCookies(u *url.URL, rawCookie string) {
	parts := strings.Split(rawCookie, ";")
	var cookies []*http.Cookie
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		kv := strings.SplitN(p, "=", 2)
		if len(kv) == 2 {
			cookies = append(cookies, &http.Cookie{
				Name:   strings.TrimSpace(kv[0]),
				Value:  strings.TrimSpace(kv[1]),
				Path:   "/",
				Domain: u.Host,
			})
		}
	}
	if len(cookies) > 0 {
		a.client.Jar.SetCookies(u, cookies)
	}
}

// ParseFlashVarsMap 从游戏页面 HTML 中提取所有的 FlashVars 键值对映射。
func ParseFlashVarsMap(html string) map[string]string {
	result := make(map[string]string)
	matches := flashVarRegex.FindAllStringSubmatch(html, -1)
	for _, m := range matches {
		if len(m) >= 3 {
			k := strings.TrimSpace(m[1])
			v := strings.TrimSpace(m[2])
			if unescaped, err := url.QueryUnescape(v); err == nil {
				v = unescaped
			}
			result[k] = v
		}
	}
	return result
}
