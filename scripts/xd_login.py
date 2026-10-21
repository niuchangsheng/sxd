#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""
心动游戏平台 (sxd.xd.com) 全自治 Web 认证与 FlashVars 凭据提取辅助脚本。
集成 ddddocr 图形验证码自动识别与双轨 Ticket 导出。
"""

import sys
import json
import random
import re
import argparse
import urllib.parse

def do_login(username, password, server_id, cookie_str=None, max_retries=10):
    import requests

    server_num = server_id.lower().lstrip('s')
    server_slug = f"S{server_num}" if server_num else "S0"

    headers = {
        "User-Agent": "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36",
        "Referer": "https://www.xd.com/users/login"
    }

    session = requests.Session()
    session.headers.update(headers)

    # 1. 若外部传入已有 Cookie，先尝试快速换票
    if cookie_str and "A1=" in cookie_str and "YOUR_" not in cookie_str:
        for part in cookie_str.split(";"):
            part = part.strip()
            if "=" in part:
                k, v = part.split("=", 1)
                session.cookies.set(k.strip(), v.strip(), domain=".xd.com")
        
        play_url = f"https://www.xd.com/games/play?app=sxd&server={server_slug}"
        try:
            r = session.get(play_url, allow_redirects=True, timeout=12)
            if "hash_code=" in r.text:
                flash_vars = dict(re.findall(r'["\']&?([a-zA-Z0-9_]+)=([^"\']*)["\']', r.text))
                cookies_out = "; ".join([f"{c.name}={c.value}" for c in session.cookies if "xd.com" in c.domain])
                return {"success": True, "cookies": cookies_out, "flashVars": flash_vars}
        except Exception:
            pass

    # 2. Cookie 不存在或已失效，通过 ddddocr 全自动执行图形验证码识别与账号密码登录
    try:
        import ddddocr
        ocr = ddddocr.DdddOcr(show_ad=False)
    except Exception as e:
        return {"success": False, "error": f"ddddocr not available: {e}"}

    for attempt in range(max_retries):
        session = requests.Session()
        session.headers.update(headers)

        # 访问登录页初始化会话
        try:
            session.get("https://www.xd.com/users/login", timeout=10)
        except Exception as e:
            continue

        # 获取验证码图片
        rand_id = str(random.random())
        captcha_url = f"https://www.xd.com/security/captcha/{rand_id}"
        try:
            resp_cap = session.get(captcha_url, timeout=10)
            code = ocr.classification(resp_cap.content).strip()
        except Exception:
            continue

        # 提交登录请求
        params = {
            "data[User][username]": username,
            "data[User][password]": password,
            "data[User][remember_me]": "true",
            "data[User][site]": "xd",
            "app": "sxd",
            "captcha": code,
            "captcha_identifier": rand_id,
            "callback": "jQuery1102_12345"
        }
        try:
            resp = session.get("https://www.xd.com/users/loginService", params=params, timeout=10)
            text = resp.text
            sys.stderr.write(f"[{attempt+1}] code={code} resp={text[:120]}\n")
        except Exception as e:
            sys.stderr.write(f"[{attempt+1}] err={e}\n")
            continue

        if '"user":-1' not in text and ('"id":' in text or '"username":' in text):
            # 登录成功，换取区服凭据
            play_url = f"https://www.xd.com/games/play?app=sxd&server={server_slug}"
            try:
                play_resp = session.get(play_url, allow_redirects=True, timeout=15)
                html = play_resp.text
                if "hash_code=" in html:
                    flash_vars = dict(re.findall(r'["\']&?([a-zA-Z0-9_]+)=([^"\']*)["\']', html))
                    cookies_out = "; ".join([f"{c.name}={c.value}" for c in session.cookies if "xd.com" in c.domain])
                    return {"success": True, "cookies": cookies_out, "flashVars": flash_vars}
                else:
                    return {"success": False, "error": f"login succeeded but failed to extract flashVars (len {len(html)})"}
            except Exception as e:
                return {"success": False, "error": f"play request failed: {e}"}

    return {"success": False, "error": f"login failed after {max_retries} attempts (captcha recognition retried)"}

def main():
    import os
    parser = argparse.ArgumentParser(description="Xindong platform auto-login script")
    parser.add_argument("--username", required=True, help="Username")
    parser.add_argument("--password", default="", help="Password")
    parser.add_argument("--password-env", default="PLATFORM_PASSWORD", help="Env var name for password")
    parser.add_argument("--server", default="s0", help="Server ID (e.g. s0)")
    parser.add_argument("--cookie", default="", help="Existing cookie string (optional)")
    args = parser.parse_args()

    password = args.password
    if not password and args.password_env:
        password = os.environ.get(args.password_env, "")
    if not password:
        password = os.environ.get("PLATFORM_PASSWORD", "")

    result = do_login(args.username, password, args.server, args.cookie)
    print(json.dumps(result, ensure_ascii=False))
    if not result.get("success"):
        sys.exit(1)

if __name__ == "__main__":
    main()
