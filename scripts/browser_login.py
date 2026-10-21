#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""
神仙道助手 (sxd-pie-ng) 浏览器交互式辅助登录与 Cookie 自动捕获工具 (方案 B)。

工作机制:
1. 启动本地轻量临时认证服务 (默认 http://127.0.0.1:8989)。
2. 自动弹出系统默认浏览器 (Chrome/Firefox/Edge 等) 渲染心动官方登录界面。
3. 支持 ddddocr 自动预填验证码，用户可直观确认或手动修正，完全免疫风控与滑块限制。
4. 验证成功后自动截获完整通行证 Cookie，并安全回写至 configs/config.yaml。
"""

import os
import sys
import re
import json
import random
import argparse
import webbrowser
from http.server import HTTPServer, BaseHTTPRequestHandler
from urllib.parse import urlparse, parse_qs
import requests

try:
    import ddddocr
    HAS_OCR = True
except Exception:
    HAS_OCR = False

SESSION = requests.Session()
SESSION.headers.update({
    "User-Agent": "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36",
    "Referer": "https://www.xd.com/users/login"
})

CONFIG_PATH = "configs/config.yaml"
TARGET_SERVER = "s0"
SERVER_INSTANCE = None
CAPTCHA_IDENTIFIER = ""

HTML_TEMPLATE = """<!DOCTYPE html>
<html lang="zh-CN">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>神仙道助手 - 账号登录与 Cookie 自动捕获</title>
    <style>
        * { box-sizing: border-box; margin: 0; padding: 0; }
        body {
            font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, "Helvetica Neue", Arial, sans-serif;
            background: linear-gradient(135deg, #1e293b 0%, #0f172a 100%);
            color: #f8fafc;
            min-height: 100vh;
            display: flex;
            align-items: center;
            justify-content: center;
            padding: 20px;
        }
        .card {
            background: #1e293b;
            border: 1px solid #334155;
            border-radius: 16px;
            box-shadow: 0 20px 25px -5px rgba(0, 0, 0, 0.5), 0 8px 10px -6px rgba(0, 0, 0, 0.5);
            width: 100%;
            max-width: 440px;
            padding: 32px;
        }
        .header { text-align: center; margin-bottom: 24px; }
        .header h1 { font-size: 20px; font-weight: 700; color: #38bdf8; margin-bottom: 8px; }
        .header p { font-size: 13px; color: #94a3b8; line-height: 1.5; }
        .form-group { margin-bottom: 18px; }
        label { display: block; font-size: 13px; font-weight: 500; color: #cbd5e1; margin-bottom: 6px; }
        input[type="text"], input[type="password"] {
            width: 100%;
            padding: 10px 14px;
            background: #0f172a;
            border: 1px solid #334155;
            border-radius: 8px;
            color: #f8fafc;
            font-size: 14px;
            transition: border-color 0.2s;
        }
        input:focus { outline: none; border-color: #38bdf8; }
        .captcha-row { display: flex; gap: 10px; align-items: center; }
        .captcha-row input { flex: 1; }
        .captcha-img-box {
            background: #fff;
            border-radius: 8px;
            padding: 2px 6px;
            cursor: pointer;
            display: flex;
            align-items: center;
            height: 40px;
            border: 1px solid #475569;
            user-select: none;
        }
        .captcha-img-box img { height: 34px; display: block; }
        .btn-submit {
            width: 100%;
            padding: 12px;
            background: #0284c7;
            color: white;
            border: none;
            border-radius: 8px;
            font-size: 15px;
            font-weight: 600;
            cursor: pointer;
            margin-top: 8px;
            transition: background 0.2s;
        }
        .btn-submit:hover { background: #0369a1; }
        .btn-submit:disabled { background: #475569; cursor: not-allowed; }
        .tip { font-size: 12px; color: #64748b; margin-top: 14px; text-align: center; }
        .status-box {
            margin-top: 16px;
            padding: 12px;
            border-radius: 8px;
            font-size: 13px;
            display: none;
            line-height: 1.4;
        }
        .status-box.error { background: rgba(239, 68, 68, 0.15); border: 1px solid #ef4444; color: #fca5a5; }
        .status-box.success { background: rgba(34, 197, 94, 0.15); border: 1px solid #22c55e; color: #86efac; }
    </style>
</head>
<body>
    <div class="card">
        <div class="header">
            <h1>神仙道助手 账号授权</h1>
            <p>通过浏览器交互登录心动平台，自动截获凭据并注入配置</p>
        </div>
        <form id="loginForm">
            <div class="form-group">
                <label for="username">心动通行证账号</label>
                <input type="text" id="username" name="username" value="{{USERNAME}}" placeholder="请输入用户名/邮箱/手机" required autocomplete="username">
            </div>
            <div class="form-group">
                <label for="password">登录密码</label>
                <input type="password" id="password" name="password" value="{{PASSWORD}}" placeholder="请输入密码" required autocomplete="current-password">
            </div>
            <div class="form-group">
                <label for="captcha">图形验证码 (点击图片可换一张)</label>
                <div class="captcha-row">
                    <input type="text" id="captcha" name="captcha" placeholder="4位字符" required maxlength="6" autocomplete="off">
                    <div class="captcha-img-box" id="captchaContainer" title="点击刷新验证码">
                        <img id="captchaImg" src="/api/captcha" alt="验证码">
                    </div>
                </div>
            </div>
            <button type="submit" class="btn-submit" id="submitBtn">登录并自动同步 Cookie</button>
        </form>
        <div class="status-box" id="statusBox"></div>
        <div class="tip">提示: 登录成功后，Cookie 将自动写入 configs/config.yaml，本页面将自动关闭。</div>
    </div>

    <script>
        const captchaImg = document.getElementById('captchaImg');
        const captchaContainer = document.getElementById('captchaContainer');
        const captchaInput = document.getElementById('captcha');
        const form = document.getElementById('loginForm');
        const submitBtn = document.getElementById('submitBtn');
        const statusBox = document.getElementById('statusBox');

        function refreshCaptcha() {
            captchaImg.src = '/api/captcha?r=' + Math.random();
            fetch('/api/ocr_hint').then(r => r.json()).then(data => {
                if (data && data.code) {
                    captchaInput.value = data.code;
                }
            }).catch(() => {});
        }

        captchaContainer.addEventListener('click', refreshCaptcha);

        // 初始页面加载自动拉取 OCR 预填建议
        refreshCaptcha();

        form.addEventListener('submit', async (e) => {
            e.preventDefault();
            submitBtn.disabled = true;
            submitBtn.innerText = '正在验证并换取 Cookie...';
            statusBox.style.display = 'none';

            const payload = {
                username: document.getElementById('username').value.trim(),
                password: document.getElementById('password').value,
                captcha: captchaInput.value.trim()
            };

            try {
                const resp = await fetch('/api/submit', {
                    method: 'POST',
                    headers: { 'Content-Type': 'application/json' },
                    body: JSON.stringify(payload)
                });
                const res = await resp.json();

                if (res.success) {
                    statusBox.className = 'status-box success';
                    statusBox.innerHTML = '🎉 <b>登录成功！</b><br>Cookie 已自动写入配置文件并生效。<br>窗口将在 2 秒后自动退出...';
                    statusBox.style.display = 'block';
                    submitBtn.innerText = '授权完成';
                    setTimeout(() => {
                        window.close();
                    }, 2000);
                } else {
                    statusBox.className = 'status-box error';
                    statusBox.innerText = res.error || '登录失败，请检查账号、密码或验证码。';
                    statusBox.style.display = 'block';
                    submitBtn.disabled = false;
                    submitBtn.innerText = '重新登录';
                    refreshCaptcha();
                    captchaInput.focus();
                }
            } catch (err) {
                statusBox.className = 'status-box error';
                statusBox.innerText = '网络请求异常: ' + err.message;
                statusBox.style.display = 'block';
                submitBtn.disabled = false;
                submitBtn.innerText = '重新尝试';
            }
        });
    </script>
</body>
</html>
"""

def extract_credentials_from_config(config_path):
    username = ""
    password = ""
    if not os.path.exists(config_path):
        return username, password
    try:
        with open(config_path, "r", encoding="utf-8") as f:
            content = f.read()
        m_user = re.search(r'username:\s*["\']?([^"\'\n]+)["\']?', content)
        if m_user:
            username = m_user.group(1).strip()
        m_pwd = re.search(r'password:\s*["\']?(?:(?:\$\{.*?:-)?([^"\'\n}]+)\}?|([^"\'\n]+))["\']?', content)
        if m_pwd:
            password = (m_pwd.group(1) or m_pwd.group(2) or "").strip()
    except Exception:
        pass
    return username, password

def update_cookie_in_config(config_path, new_cookie, username=None):
    if not os.path.exists(config_path):
        return False
    try:
        with open(config_path, "r", encoding="utf-8") as f:
            lines = f.readlines()
        
        updated = False
        in_xindong = False
        new_lines = []
        for line in lines:
            if 'platform: "xindong"' in line or "platform: 'xindong'" in line or 'platform: xindong' in line:
                in_xindong = True
            elif in_xindong and line.strip().startswith("- platform:"):
                in_xindong = False
            
            if in_xindong and re.match(r'^\s*cookie:\s*.*', line):
                indent = re.match(r'^(\s*)', line).group(1)
                new_lines.append(f'{indent}cookie: "{new_cookie}"\n')
                updated = True
                continue
            
            if in_xindong and username and re.match(r'^\s*username:\s*.*', line):
                indent = re.match(r'^(\s*)', line).group(1)
                new_lines.append(f'{indent}username: "{username}"\n')
                continue

            new_lines.append(line)

        if not updated:
            # 如果原先没有 cookie 行，在 platform/username 之后插入
            final_lines = []
            for line in new_lines:
                final_lines.append(line)
                if 'platform: "xindong"' in line or 'platform: xindong' in line:
                    final_lines.append(f'    cookie: "{new_cookie}"\n')
                    updated = True
            new_lines = final_lines

        with open(config_path, "w", encoding="utf-8") as f:
            f.writelines(new_lines)
        return True
    except Exception as e:
        sys.stderr.write(f"写入配置文件失败: {e}\n")
        return False

class LoginServerHandler(BaseHTTPRequestHandler):
    last_captcha_bytes = None
    last_ocr_code = ""

    def log_message(self, format, *args):
        # 静默常规请求日志
        pass

    def do_GET(self):
        global CAPTCHA_IDENTIFIER
        parsed = urlparse(self.path)

        if parsed.path == "/" or parsed.path == "/index.html":
            username, password = extract_credentials_from_config(CONFIG_PATH)
            html = HTML_TEMPLATE.replace("{{USERNAME}}", username).replace("{{PASSWORD}}", password)
            self.send_response(200)
            self.send_header("Content-Type", "text/html; charset=utf-8")
            self.end_headers()
            self.wfile.write(html.encode("utf-8"))
            return

        if parsed.path == "/api/captcha":
            CAPTCHA_IDENTIFIER = str(random.random())
            captcha_url = f"https://www.xd.com/security/captcha/{CAPTCHA_IDENTIFIER}"
            try:
                resp = SESSION.get(captcha_url, timeout=10)
                LoginServerHandler.last_captcha_bytes = resp.content
                if HAS_OCR:
                    try:
                        ocr = ddddocr.DdddOcr(show_ad=False)
                        LoginServerHandler.last_ocr_code = ocr.classification(resp.content).strip()
                    except Exception:
                        LoginServerHandler.last_ocr_code = ""
                else:
                    LoginServerHandler.last_ocr_code = ""

                self.send_response(200)
                self.send_header("Content-Type", resp.headers.get("Content-Type", "image/jpeg"))
                self.end_headers()
                self.wfile.write(resp.content)
            except Exception as e:
                self.send_response(500)
                self.end_headers()
                self.wfile.write(str(e).encode())
            return

        if parsed.path == "/api/ocr_hint":
            self.send_response(200)
            self.send_header("Content-Type", "application/json")
            self.end_headers()
            self.wfile.write(json.dumps({"code": LoginServerHandler.last_ocr_code}).encode())
            return

        self.send_response(404)
        self.end_headers()

    def do_POST(self):
        global CAPTCHA_IDENTIFIER, SERVER_INSTANCE
        parsed = urlparse(self.path)

        if parsed.path == "/api/submit":
            length = int(self.headers.get("Content-Length", 0))
            body = self.rfile.read(length)
            try:
                data = json.loads(body.decode("utf-8"))
            except Exception:
                self.send_json({"success": False, "error": "无效的 JSON 数据"})
                return

            username = data.get("username", "").strip()
            password = data.get("password", "").strip()
            captcha = data.get("captcha", "").strip()

            if not username or not password or not captcha:
                self.send_json({"success": False, "error": "用户名、密码与验证码均不能为空"})
                return

            # 提交心动平台登录接口
            login_params = {
                "data[User][username]": username,
                "data[User][password]": password,
                "data[User][remember_me]": "true",
                "data[User][site]": "xd",
                "app": "sxd",
                "captcha": captcha,
                "captcha_identifier": CAPTCHA_IDENTIFIER,
                "callback": "jQuery1102_" + str(int(random.random() * 100000))
            }
            try:
                login_resp = SESSION.get("https://www.xd.com/users/loginService", params=login_params, timeout=12)
                text = login_resp.text
            except Exception as e:
                self.send_json({"success": False, "error": f"连接心动认证服务器失败: {e}"})
                return

            if '"user":-1' in text:
                # 解析错误信息
                err_msg = "登录失败: 验证码错误或用户名密码不匹配"
                m = re.search(r'"message"\s*:\s*"([^"]+)"', text)
                if m:
                    err_msg = f"心动平台提示: {m.group(1)}"
                self.send_json({"success": False, "error": err_msg})
                return

            if '"id":' in text or '"username":' in text:
                # 登录成功，拼装 Cookie
                cookie_parts = []
                for c in SESSION.cookies:
                    if "xd.com" in c.domain:
                        cookie_parts.append(f"{c.name}={c.value}")
                
                cookie_str = "; ".join(cookie_parts)
                if not cookie_str or "A1=" not in cookie_str:
                    self.send_json({"success": False, "error": "登录成功但未能捕获到核心 A1 通行证 Cookie"})
                    return

                # 测试凭据有效性 (尝试选服换取 FlashVars)
                server_num = TARGET_SERVER.lower().lstrip("s")
                server_slug = f"S{server_num}" if server_num else "S0"
                play_url = f"https://www.xd.com/games/play?app=sxd&server={server_slug}"
                
                flash_vars = {}
                try:
                    p_resp = SESSION.get(play_url, allow_redirects=True, timeout=10)
                    if "hash_code=" in p_resp.text:
                        flash_vars = dict(re.findall(r'["\']&?([a-zA-Z0-9_]+)=([^"\']*)["\']', p_resp.text))
                except Exception:
                    pass

                # 回写至配置文件
                updated = update_cookie_in_config(CONFIG_PATH, cookie_str, username=username)

                print("\n=======================================================")
                print("🎉 心动平台账号登录成功！")
                print(f"账号: {username}")
                print(f"Cookie: {cookie_str}")
                if flash_vars.get("player_name"):
                    print(f"角色名: {flash_vars.get('player_name')} | 区服: {TARGET_SERVER}")
                if updated:
                    print(f"已自动更新配置文件: {CONFIG_PATH}")
                print("=======================================================\n")

                self.send_json({
                    "success": True,
                    "cookie": cookie_str,
                    "updated_config": updated
                })

                # 异步延迟关闭 HTTP 服务，以便客户端接收完响应
                def shutdown_soon():
                    import time
                    time.sleep(1.5)
                    if SERVER_INSTANCE:
                        SERVER_INSTANCE.shutdown()

                import threading
                threading.Thread(target=shutdown_soon, daemon=True).start()
                return

            self.send_json({"success": False, "error": f"未能识别登录响应: {text[:150]}"})

    def send_json(self, data):
        self.send_response(200)
        self.send_header("Content-Type", "application/json")
        self.end_headers()
        self.wfile.write(json.dumps(data).encode("utf-8"))

def main():
    global CONFIG_PATH, TARGET_SERVER, SERVER_INSTANCE

    parser = argparse.ArgumentParser(description="神仙道助手 (sxd-pie-ng) 浏览器交互式辅助登录工具")
    parser.add_argument("--config", default="configs/config.yaml", help="配置文件路径 (默认 configs/config.yaml)")
    parser.add_argument("--server", default="s0", help="目标区服标识 (默认 s0)")
    parser.add_argument("--port", type=int, default=8989, help="本地临时 HTTP 监听端口 (默认 8989)")
    parser.add_argument("--no-browser", action="store_true", help="不自动拉起系统浏览器")
    args = parser.parse_args()

    CONFIG_PATH = args.config
    TARGET_SERVER = args.server
    port = args.port

    local_url = f"http://127.0.0.1:{port}"

    server = HTTPServer(("127.0.0.1", port), LoginServerHandler)
    SERVER_INSTANCE = server

    print(f"\n[sxd-pie-ng] 浏览器交互式辅助登录服务已就绪: {local_url}")
    print(f"[*] 配置文件: {CONFIG_PATH} | 目标区服: {TARGET_SERVER}")
    
    if not args.no_browser:
        print("[*] 正在打开系统默认浏览器...")
        try:
            webbrowser.open(local_url)
        except Exception as e:
            print(f"[!] 打开浏览器失败，请手动在浏览器访问: {local_url}")
    else:
        print(f"[*] 请在浏览器中打开: {local_url}")

    print("[*] 等待在网页中完成登录认证 (按 Ctrl+C 可取消)...")
    try:
        server.serve_forever()
    except KeyboardInterrupt:
        print("\n[!] 操作已由用户中断。")
    finally:
        server.server_close()

if __name__ == "__main__":
    main()
