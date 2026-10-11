"""官方微端与原版辅助 (pie.exe) 双端抓包差分对比分析工具 (Capture Diff Analyzer)。

自动解析 data/captures/ 目录下生成的微端与辅助 pcapng 抓包文件，
针对神仙道大端序二进制协议提取并对比两者在业务模块 (Module)、指令编号 (Action)、
报文序列时序以及载荷规模上的异同，为协议重构对齐提供一手差分依据。

@author Ateng
@since 2026-10-11
"""

import argparse
from collections import Counter
import glob
import os
import struct
import sys

if sys.platform == "win32":
    try:
        sys.stdout.reconfigure(encoding="utf-8", errors="replace")
        sys.stderr.reconfigure(encoding="utf-8", errors="replace")
    except Exception:
        pass

try:
    from colorama import Fore, Style, init as colorama_init
    colorama_init(autoreset=True)
    HAS_COLORAMA = True
except ImportError:
    HAS_COLORAMA = False

try:
    from scapy.all import IP, TCP, rdpcap
    HAS_SCAPY = True
except ImportError:
    HAS_SCAPY = False

CAPTURES_DIR = os.path.abspath(
    os.path.join(os.path.dirname(__file__), "..", "..", "data", "captures")
)

# 模块与动作基础映射 (对齐 internal/protocol/constants.go)
MODULE_NAMES = {
    0: "角色主模块 (Mod_Player)",
    1: "城镇场景模块 (Mod_Town)",
    4: "关卡任务模块 (Mod_Mission)",
    5: "物品背包模块 (Mod_Item)",
    13: "药园农场模块 (Mod_Farm)",
    21: "仙履奇缘模块 (Mod_Fate)",
    25: "副本扫荡挂机 (Mod_MissionPractice)",
    28: "竞技场模块 (Mod_Arena)",
    35: "英雄副本模块 (Mod_HeroMission)",
    94: "跨服/网页登录 (Mod_StLogin)",
    165: "角色初始化握手步3 (Mod_Player_Init3)",
}

ACTION_NAMES = {
    (0, 0): "登录握手 (player_login)",
    (0, 2): "获取角色主属性 (player_get_player_info)",
    (0, 3): "服务器属性推送 (player_update_player_data)",
    (0, 72): "角色初始化步1 (player_init_step1)",
    (0, 99): "角色初始化步2 (player_init_step2)",
    (1, 0): "进入城镇 (town_enter)",
    (1, 1): "离开城镇 (town_leave)",
    (13, 0): "药园基础信息 (farm_get_player_role_grow_info)",
    (13, 1): "药园伙伴列表 (farm_get_all_roles)",
    (13, 21): "药园作物状态 (farm_get_herbal_plant_state)",
    (13, 22): "种子背包商城 (farm_get_herbal_seeds)",
    (13, 23): "购买药园种子 (farm_buy_herbal_seed)",
    (13, 24): "药园种植作物 (farm_plant_herbal)",
    (13, 25): "收获药园作物 (farm_harvest_herbal)",
    (21, 0): "仙履奇缘状态 (fate_get_player_fate_info)",
    (21, 1): "获取奇缘题目 (fate_get_question)",
    (21, 2): "提交奇缘回答 (fate_answer_question)",
    (25, 0): "开始挂机扫荡 (practice_start)",
    (25, 1): "挂机扫荡结算 (practice_notify)",
    (25, 2): "取消挂机扫荡 (practice_cancel)",
    (28, 0): "打开竞技场 (arena_open)",
    (28, 1): "发起竞技挑战 (arena_challenge)",
    (94, 0): "跨服/网页登录握手 (stlogin_handshake)",
    (165, 0): "角色初始化步3 (init_step3)",
}


def c_print(color_prefix: str, text: str):
    """支持无损降级与带颜色控制台输出。"""
    if HAS_COLORAMA:
        print(f"{color_prefix}{text}{Style.RESET_ALL}")
    else:
        print(text)


def find_latest_capture(pattern: str) -> str:
    """在 data/captures/ 目录下检索最新匹配的 pcapng 文件。"""
    search_path = os.path.join(CAPTURES_DIR, pattern)
    files = glob.glob(search_path)
    if not files:
        return ""
    files.sort(key=os.path.getmtime, reverse=True)
    return files[0]


def parse_game_frames_from_pcap(pcap_path: str, game_ports: set) -> list:
    """读取 pcapng 并拆解神仙道 TCP 二进制封包序列。"""
    if not os.path.exists(pcap_path):
        return []

    try:
        packets = rdpcap(pcap_path)
    except Exception as e:
        c_print(Fore.RED if HAS_COLORAMA else "", f"[-] 解析 {pcap_path} 失败: {e}")
        return []

    stream_buffers = {}
    frames = []

    for pkt in packets:
        if not pkt.haslayer(TCP) or not pkt.haslayer(IP):
            continue

        ip_layer = pkt[IP]
        tcp_layer = pkt[TCP]
        src_port = tcp_layer.sport
        dst_port = tcp_layer.dport

        if src_port not in game_ports and dst_port not in game_ports:
            continue

        raw_payload = bytes(pkt[TCP].payload)
        if not raw_payload:
            continue

        stream_key = (f"{ip_layer.src}:{src_port}", f"{ip_layer.dst}:{dst_port}")
        buf = stream_buffers.get(stream_key, b"") + raw_payload

        is_c2s = (dst_port in game_ports)
        direction = "C2S" if is_c2s else "S2C"

        while len(buf) >= 8:
            pkt_len, module_id, action_id = struct.unpack("!IHH", buf[:8])
            if pkt_len < 4 or pkt_len > 10 * 1024 * 1024:
                buf = buf[1:]
                continue

            total_frame_len = pkt_len + 4
            if len(buf) < total_frame_len:
                break

            payload_data = buf[8:total_frame_len]
            buf = buf[total_frame_len:]

            frames.append({
                "time": float(pkt.time),
                "direction": direction,
                "module": module_id,
                "action": action_id,
                "length": pkt_len,
                "payload_len": len(payload_data),
            })

        stream_buffers[stream_key] = buf

    return frames


def print_diff_summary(file_a: str, frames_a: list, file_b: str, frames_b: list):
    """输出双端抓包结构化差分比对报表。"""
    c_print(Fore.CYAN if HAS_COLORAMA else "", "\n========================================================")
    c_print(Fore.CYAN if HAS_COLORAMA else "", "         神仙道 双端协议抓包差分比对报告 (Protocol Diff) ")
    c_print(Fore.CYAN if HAS_COLORAMA else "", "========================================================")

    base_a = os.path.basename(file_a)
    base_b = os.path.basename(file_b)

    print(f"基准端 A [官方微端]: {base_a} (捕获业务封包: {len(frames_a)} 个)")
    print(f"对照端 B [原版辅助]: {base_b} (捕获业务封包: {len(frames_b)} 个)")

    # 1. 业务模块/动作覆盖度分布对比
    counter_a = Counter((f["direction"], f["module"], f["action"]) for f in frames_a)
    counter_b = Counter((f["direction"], f["module"], f["action"]) for f in frames_b)
    all_keys = sorted(list(set(counter_a.keys()) | set(counter_b.keys())))

    c_print(Fore.YELLOW if HAS_COLORAMA else "", "\n--- [1. 业务协议指令 (Module/Action) 频次分布差分] ---")
    header = f"{'方向':<5} | {'模块':<18} | {'动作指令':<28} | {'微端 (A)':<10} | {'辅助 (B)':<10} | {'状态'}"
    print(header)
    print("-" * len(header))

    for direction, mod_id, act_id in all_keys:
        cnt_a = counter_a.get((direction, mod_id, act_id), 0)
        cnt_b = counter_b.get((direction, mod_id, act_id), 0)
        mod_name = MODULE_NAMES.get(mod_id, f"Mod {mod_id}")
        act_name = ACTION_NAMES.get((mod_id, act_id), f"Act {act_id}")

        if cnt_a > 0 and cnt_b == 0:
            status = Fore.RED + "[微端独有]" if HAS_COLORAMA else "[微端独有]"
        elif cnt_a == 0 and cnt_b > 0:
            status = Fore.MAGENTA + "[辅助独有]" if HAS_COLORAMA else "[辅助独有]"
        elif cnt_a == cnt_b:
            status = Fore.GREEN + "[完全对齐]" if HAS_COLORAMA else "[完全对齐]"
        else:
            status = Fore.YELLOW + f"[频次差异: {cnt_a} vs {cnt_b}]" if HAS_COLORAMA else f"[频次差异: {cnt_a} vs {cnt_b}]"

        print(f"{direction:<5} | {mod_name:<18} | {act_name:<28} | {cnt_a:<10} | {cnt_b:<10} | {status}")

    # 2. 初始登录握手时序切片比对 (前 10 个封包)
    c_print(Fore.YELLOW if HAS_COLORAMA else "", "\n--- [2. 初始登录时序特征切片对比 (前 10 帧)] ---")
    max_steps = max(min(len(frames_a), 10), min(len(frames_b), 10))
    print(f"{'序号':<4} | {'微端 (A) 时序':<35} | {'辅助 (B) 时序':<35}")
    print("-" * 78)

    for i in range(max_steps):
        desc_a = "-"
        if i < len(frames_a):
            fa = frames_a[i]
            desc_a = f"{fa['direction']} M{fa['module']}:A{fa['action']} ({fa['length']}B)"

        desc_b = "-"
        if i < len(frames_b):
            fb = frames_b[i]
            desc_b = f"{fb['direction']} M{fb['module']}:A{fb['action']} ({fb['length']}B)"

        match_color = Fore.GREEN if desc_a.split("(")[0] == desc_b.split("(")[0] else Fore.RED
        row_str = f"#{i+1:<3} | {desc_a:<35} | {desc_b:<35}"
        c_print(match_color if HAS_COLORAMA else "", row_str)

    c_print(Fore.CYAN if HAS_COLORAMA else "", "\n========================================================\n")


def main():
    parser = argparse.ArgumentParser(description="神仙道官方微端与原版辅助抓包差分分析器")
    parser.add_argument("--wd", "-w", default=None, help="官方微端 pcapng 路径 (默认自动寻找最新)")
    parser.add_argument("--pie", "-p", default=None, help="原版辅助 pcapng 路径 (默认自动寻找最新)")
    parser.add_argument("--ports", default="8381,19753", help="过滤的游戏端口 (逗号分隔)")
    args = parser.parse_args()

    game_ports = {int(p.strip()) for p in args.ports.split(",") if p.strip()}

    file_a = args.wd or find_latest_capture("wd_capture_*.pcapng")
    file_b = args.pie or find_latest_capture("pie_capture_*.pcapng")

    if not file_a:
        c_print(Fore.YELLOW if HAS_COLORAMA else "", f"[*] 未在 {CAPTURES_DIR} 找到微端抓包文件 (wd_capture_*.pcapng)。")
        c_print(Fore.YELLOW if HAS_COLORAMA else "", "    请先运行 scripts\\capture\\run_capture.bat 捕获微端流量。")
        sys.exit(1)

    if not file_b:
        c_print(Fore.YELLOW if HAS_COLORAMA else "", f"[*] 未在 {CAPTURES_DIR} 找到原版辅助抓包文件 (pie_capture_*.pcapng)。")
        c_print(Fore.YELLOW if HAS_COLORAMA else "", "    请先运行 scripts\\capture\\run_capture_pie.bat 捕获辅助流量。")
        sys.exit(1)

    c_print(Fore.GREEN if HAS_COLORAMA else "", f"[+] 读取微端抓包: {file_a}")
    frames_a = parse_game_frames_from_pcap(file_a, game_ports)

    c_print(Fore.GREEN if HAS_COLORAMA else "", f"[+] 读取辅助抓包: {file_b}")
    frames_b = parse_game_frames_from_pcap(file_b, game_ports)

    print_diff_summary(file_a, frames_a, file_b, frames_b)


if __name__ == "__main__":
    main()
