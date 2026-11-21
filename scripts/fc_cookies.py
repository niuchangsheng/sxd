#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""
从 FlashCenter（CEF 内核）的 Cookies 数据库导出 cookie。

原理：
  * FlashCenter 用 CEF，cookie 存在 SQLite 里，值用 Chromium 的 v10 方案加密；
  * v10 的 AES-256-GCM 主密钥放在 profile 的 LocalPrefs.json
    (os_crypt.encrypted_key = "DPAPI" + Windows DPAPI 加密的 32 字节密钥)；
  * 在 WSL 里调用 Windows 的 powershell.exe 做一次 DPAPI 解密即可。

用法（在 WSL 里运行）：
  python3 fc_cookies.py                            # 默认导出 *.sxd.xd.com，JSON
  python3 fc_cookies.py --xindong                  # 直接输出 sxd-pie-ng configs 所需的 A1; A2
  python3 fc_cookies.py --list-hosts               # 看看各 profile 里有哪些站点
  python3 fc_cookies.py --host s0.sxd.xd.com --format header
  python3 fc_cookies.py --domain xd.com --format netscape > cookies.txt
  python3 fc_cookies.py --profile flashcenter      # 取 FlashCenter 自身界面用的 profile

依赖：Python 的 cryptography 包；Windows interop 可用（powershell.exe 在 PATH 里）。
"""

import argparse
import base64
import hashlib
import json
import os
import shutil
import sqlite3
import subprocess
import sys
import tempfile
from pathlib import Path

PROFILE_DIRS = {
    # 游戏/浏览器窗口使用的 profile（sxd.xd.com 的登录 cookie 在这里）
    "fcbrowser": r"AppData\Local\Flash_Center\FCBrowser\cache",
    # FlashCenter 自己的主页/登录窗口
    "flashcenter": r"AppData\Local\Flash_Center\cache\flashcenter",
}

WIN_USER = None


def run(cmd):
    p = subprocess.run(cmd, check=False, capture_output=True)
    if p.returncode != 0:
        err = p.stderr.decode("utf-8", "replace")
        if "�" in err:
            err = p.stderr.decode("gbk", "replace")
        raise RuntimeError(f"命令失败({p.returncode}): {err.strip()}")
    return p.stdout.decode("utf-8", "replace").strip()


def get_windows_username():
    global WIN_USER
    if WIN_USER is None:
        try:
            WIN_USER = run(["cmd.exe", "/c", "echo %USERNAME%"])
        except Exception:
            WIN_USER = os.environ.get("USER", "")
    return WIN_USER


def to_wsl_path(win_path):
    """C:\\Users\\x\\... -> /mnt/c/Users/x/..."""
    p = win_path.replace("\\", "/")
    if len(p) >= 2 and p[1] == ":":
        p = "/mnt/" + p[0].lower() + p[2:]
    return p


def get_master_key(local_prefs_win):
    """用 Windows DPAPI 解开 CEF 的 os_crypt 主密钥，返回 32 字节。"""
    ps = (
        "Add-Type -AssemblyName System.Security; "
        f"$j = Get-Content -Raw -LiteralPath '{local_prefs_win}' | ConvertFrom-Json; "
        "$b = [Convert]::FromBase64String($j.os_crypt.encrypted_key); "
        "$b = $b[5..($b.Length-1)]; "
        "$k = [System.Security.Cryptography.ProtectedData]::Unprotect("
        "$b, $null, [System.Security.Cryptography.DataProtectionScope]::CurrentUser); "
        "[Convert]::ToBase64String($k)"
    )
    out = run(["powershell.exe", "-NoProfile", "-NonInteractive", "-Command", ps])
    key = base64.b64decode(out)
    if len(key) != 32:
        raise RuntimeError(f"DPAPI 返回的密钥长度异常: {len(key)}")
    return key


def decrypt_value(aes, host, encrypted):
    if not encrypted:
        return ""
    if encrypted[:3] in (b"v10", b"v11"):
        nonce, ct, tag = encrypted[3:15], encrypted[15:-16], encrypted[-16:]
        pt = aes.decrypt(nonce, ct + tag, None)
        # 新版 Chromium 会在明文前加 32 字节 host_key 的 SHA-256
        if pt[:32] == hashlib.sha256(host.encode()).digest():
            pt = pt[32:]
        return pt.decode("utf-8", "replace")
    return encrypted.decode("utf-8", "replace")


def read_cookies(profile):
    from cryptography.hazmat.primitives.ciphers.aead import AESGCM

    base_win = os.path.join(
        f"C:\\Users\\{get_windows_username()}", PROFILE_DIRS[profile]
    )
    base = Path(to_wsl_path(base_win))
    db = base / "Cookies"
    prefs = base / "LocalPrefs.json"
    if not db.exists():
        sys.exit(f"找不到 Cookies 数据库: {db}")
    if not prefs.exists():
        sys.exit(f"找不到 {prefs}")

    key = get_master_key(os.path.join(base_win, "LocalPrefs.json"))
    aes = AESGCM(key)

    tmp = Path(tempfile.mkdtemp(prefix="fccookies-"))
    copy = tmp / "Cookies"
    shutil.copy2(db, copy)
    journal = base / "Cookies-journal"
    if journal.exists():
        shutil.copy2(journal, tmp / "Cookies-journal")

    con = sqlite3.connect(f"file:{copy}?mode=ro", uri=True)
    rows = con.execute(
        "SELECT host_key, name, value, encrypted_value, path, expires_utc, "
        "is_secure, is_httponly FROM cookies ORDER BY host_key, name"
    ).fetchall()
    con.close()

    cookies = []
    for host, name, value, enc, path, expires_utc, secure, httponly in rows:
        try:
            value = decrypt_value(aes, host, enc) if enc else value
        except Exception as e:
            print(f"[warn] 解密失败 {host} {name}: {e}", file=sys.stderr)
            continue
        exp = 0
        if expires_utc:
            exp = max(0, int(expires_utc / 1_000_000 - 11644473600))
        cookies.append(
            {
                "host": host,
                "name": name,
                "value": value,
                "path": path,
                "expires": exp,
                "secure": bool(secure),
                "httponly": bool(httponly),
            }
        )
    return cookies


def main():
    ap = argparse.ArgumentParser(description="导出 FlashCenter (CEF) 的 cookie")
    ap.add_argument("--profile", choices=PROFILE_DIRS, default="fcbrowser")
    ap.add_argument("--domain", default="sxd.xd.com",
                    help="按域名后缀过滤（默认 sxd.xd.com）")
    ap.add_argument("--host", help="精确指定 host，例如 s0.sxd.xd.com")
    ap.add_argument("--names", help="按 cookie 名称过滤，逗号分隔，例如 A1,A2")
    ap.add_argument("--xindong", action="store_true",
                    help="快捷模式：导出 .xd.com 的 A1、A2（供 sxd-pie-ng configs/*.yaml 使用）")
    ap.add_argument("--format", choices=["json", "header", "netscape"], default="json")
    ap.add_argument("--list-hosts", action="store_true", help="只列出 cookie 所属 host")
    args = ap.parse_args()

    cookies = read_cookies(args.profile)

    if args.list_hosts:
        counts = {}
        for c in cookies:
            counts[c["host"]] = counts.get(c["host"], 0) + 1
        for host in sorted(counts):
            print(f"{counts[host]:4d}  {host}")
        return

    if args.xindong:
        xd = {c["name"]: c["value"] for c in cookies if c["host"] == ".xd.com"}
        missing = [k for k in ("A1", "A2") if not xd.get(k)]
        if missing:
            sys.exit(
                f".xd.com 缺少必要 cookie: {', '.join(missing)}（请先在 FlashCenter 重新登录心动账号）"
            )
        print(f"A1={xd['A1']}; A2={xd['A2']}")
        return

    if args.host:
        selected = [c for c in cookies if c["host"] == args.host]
        if not selected:
            sys.exit(f"host {args.host!r} 没有 cookie")
    else:
        selected = [
            c for c in cookies
            if c["host"].lstrip(".").endswith(args.domain)
        ]
        if not selected:
            sys.exit(f"没有匹配 {args.domain!r} 的 cookie")

    if args.names:
        wanted = {n.strip() for n in args.names.split(",") if n.strip()}
        selected = [c for c in selected if c["name"] in wanted]
        if not selected:
            sys.exit(f"没有匹配名称 {sorted(wanted)} 的 cookie")

    if args.format == "json":
        print(json.dumps(selected, ensure_ascii=False, indent=2))
    elif args.format == "header":
        hosts = {c["host"] for c in selected}
        if len(hosts) > 1:
            sys.exit(
                "匹配到多个 host，header 格式请用 --host 指定其一: "
                + ", ".join(sorted(hosts))
            )
        print("; ".join(f"{c['name']}={c['value']}" for c in selected))
    else:  # netscape
        print("# Netscape HTTP Cookie File")
        for c in selected:
            domain = c["host"]
            include_sub = "TRUE" if domain.startswith(".") else "FALSE"
            secure = "TRUE" if c["secure"] else "FALSE"
            print(
                "\t".join(
                    [domain, include_sub, c["path"], secure, str(c["expires"]),
                     c["name"], c["value"]]
                )
            )


if __name__ == "__main__":
    main()