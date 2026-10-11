"""针对《神仙道》官方微端 (sxd_wd_*.exe) 的全链路网络流量捕获与实时协议分析器。

具备双阶段通信嗅探能力：
1. 阶段 1：Web 登录换票与 HTTP/HTTPS 选区认证流量审计；
2. 阶段 2：TCP 8381 私有二进制长连接封包拆解、大端序解码与透明 zlib 解压。

@author Ateng
@since 2026-10-11
"""

import argparse
import ctypes
from ctypes import wintypes
from datetime import datetime
import os
import re
import socket
import struct
import subprocess
import sys
import threading
import time
import zlib

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
    from scapy.all import IP, TCP, PcapNgWriter, PcapWriter, Raw, conf, get_if_list, sniff
    HAS_SCAPY = True
except ImportError:
    HAS_SCAPY = False

# 基础默认常量与目录定义
DEFAULT_TARGET_EXE = r"C:\Users\kongyu\Desktop\sxd_wd_V3.11_1693793577.exe"
DEFAULT_CAPTURES_DIR = os.path.abspath(
    os.path.join(os.path.dirname(__file__), "..", "..", "data", "captures")
)
DEFAULT_GAME_PORTS = {8381, 19753}
DEFAULT_WEB_PORTS = {80, 8080, 443}


def get_default_pcap_path(prefix: str = "wd_capture") -> str:
    """生成带时间戳的标准抓包归档路径并确保目录存在。"""
    os.makedirs(DEFAULT_CAPTURES_DIR, exist_ok=True)
    ts = datetime.now().strftime("%Y%m%d_%H%M%S")
    return os.path.join(DEFAULT_CAPTURES_DIR, f"{prefix}_{ts}.pcapng")


# Win32 API 常量与定义
TH32CS_SNAPPROCESS = 0x00000002
AF_INET = 2
TCP_TABLE_OWNER_PID_ALL = 5
PROCESS_QUERY_LIMITED_INFORMATION = 0x1000

kernel32 = ctypes.windll.kernel32
shell32 = ctypes.windll.shell32
iphlpapi = ctypes.windll.iphlpapi


class PROCESSENTRY32W(ctypes.Structure):
    _fields_ = [
        ("dwSize", wintypes.DWORD),
        ("cntUsage", wintypes.DWORD),
        ("th32ProcessID", wintypes.DWORD),
        ("th32DefaultHeapID", ctypes.c_size_t),
        ("th32ModuleID", wintypes.DWORD),
        ("cntThreads", wintypes.DWORD),
        ("th32ParentProcessID", wintypes.DWORD),
        ("pcPriClassBase", wintypes.LONG),
        ("dwFlags", wintypes.DWORD),
        ("szExeFile", wintypes.WCHAR * 260),
    ]


# 模块名称映射字典 (对齐 docs/protocol-spec.md 与 internal/protocol/constants.go)
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

# 常见 Action 名称映射
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


def is_admin() -> bool:
    """检查当前进程是否具备 Windows 管理员特权。"""
    try:
        return ctypes.windll.shell32.IsUserAnAdmin() != 0
    except Exception:
        return False


def request_elevation():
    """触发 Windows UAC 请求管理员提权。"""
    c_print(Fore.YELLOW if HAS_COLORAMA else "", "[*] Scapy 网络包捕获需要管理员权限，正在请求 UAC 提权...")
    script_abs = os.path.abspath(sys.argv[0])
    args_list = [f'"{script_abs}"'] + [f'"{arg}"' for arg in sys.argv[1:]]
    params = " ".join(args_list)
    script_dir = os.path.dirname(script_abs)
    ret = shell32.ShellExecuteW(None, "runas", sys.executable, params, script_dir, 1)
    if ret <= 32:
        c_print(Fore.RED if HAS_COLORAMA else "", f"[-] UAC 提权失败或被用户取消，返回值: {ret}")
        sys.exit(1)
    sys.exit(0)


def find_matching_processes(keywords: list) -> list:
    """枚举系统快照定位符合关键字命名的目标进程。"""
    h_snap = kernel32.CreateToolhelp32Snapshot(TH32CS_SNAPPROCESS, 0)
    if h_snap == -1:
        return []

    pe = PROCESSENTRY32W()
    pe.dwSize = ctypes.sizeof(PROCESSENTRY32W)
    results = []

    if kernel32.Process32FirstW(h_snap, ctypes.byref(pe)):
        while True:
            exe_lower = pe.szExeFile.lower()
            if any(k.lower() in exe_lower for k in keywords):
                full_path = ""
                h_proc = kernel32.OpenProcess(
                    PROCESS_QUERY_LIMITED_INFORMATION, False, pe.th32ProcessID
                )
                if h_proc:
                    buf = (wintypes.WCHAR * 1024)()
                    size = wintypes.DWORD(1024)
                    if kernel32.QueryFullProcessImageNameW(h_proc, 0, buf, ctypes.byref(size)):
                        full_path = buf.value
                    kernel32.CloseHandle(h_proc)
                results.append((pe.th32ProcessID, pe.szExeFile, full_path))
            if not kernel32.Process32NextW(h_snap, ctypes.byref(pe)):
                break
    kernel32.CloseHandle(h_snap)
    return results


def get_tcp_connections(target_pids: set) -> list:
    """获取指定 PID 关联的 TCP 连接信息。"""
    table_size = wintypes.DWORD(0)
    iphlpapi.GetExtendedTcpTable(
        None, ctypes.byref(table_size), True, AF_INET, TCP_TABLE_OWNER_PID_ALL, 0
    )
    if table_size.value == 0:
        return []

    buf = (ctypes.c_byte * table_size.value)()
    ret = iphlpapi.GetExtendedTcpTable(
        ctypes.byref(buf),
        ctypes.byref(table_size),
        True,
        AF_INET,
        TCP_TABLE_OWNER_PID_ALL,
        0,
    )
    if ret != 0:
        return []

    num_entries = struct.unpack_from("<I", buf, 0)[0]
    conns = []
    offset = 4
    for _ in range(num_entries):
        state, loc_ip, loc_port, rem_ip, rem_port, owner_pid = struct.unpack_from(
            "<IIIIII", buf, offset
        )
        offset += 24
        if owner_pid in target_pids:
            local_ip_str = socket.inet_ntoa(struct.pack("<I", loc_ip))
            remote_ip_str = socket.inet_ntoa(struct.pack("<I", rem_ip))
            local_port_real = socket.ntohs(loc_port & 0xFFFF)
            remote_port_real = socket.ntohs(rem_port & 0xFFFF)
            conns.append({
                "pid": owner_pid,
                "local": f"{local_ip_str}:{local_port_real}",
                "remote": f"{remote_ip_str}:{remote_port_real}",
                "local_port": local_port_real,
                "remote_port": remote_port_real,
            })
    return conns


def format_hex_dump(data: bytes, max_bytes: int = 128) -> str:
    """生成带 ASCII 字符对照的标准 Hex Dump 预览。"""
    lines = []
    display_data = data[:max_bytes]
    for i in range(0, len(display_data), 16):
        chunk = display_data[i:i + 16]
        hex_part = " ".join(f"{b:02X}" for b in chunk)
        ascii_part = "".join(chr(b) if 32 <= b <= 126 else "." for b in chunk)
        lines.append(f"    {i:04X}  {hex_part:<48}  |{ascii_part}|")
    if len(data) > max_bytes:
        lines.append(f"    ... 剩余 {len(data) - max_bytes} 字节省略 ...")
    return "\n".join(lines)


class TrafficCaptureSession:
    """全链路流量捕获与协议解码会话。"""

    def __init__(self, output_pcap: str, game_ports: set, web_ports: set, capture_mode: str):
        self.output_pcap = output_pcap
        self.game_ports = game_ports
        self.web_ports = web_ports
        self.capture_mode = capture_mode  # all / game / web
        self.writer = None
        self.packet_count = 0
        self.game_frame_count = 0
        self.web_req_count = 0
        self.stream_buffers = {}  # (src, dst) -> bytes
        self.lock = threading.Lock()

        # 初始化标准 pcapng/pcap 写入器
        try:
            self.writer = PcapNgWriter(self.output_pcap)
            c_print(Fore.GREEN if HAS_COLORAMA else "", f"[+] PCAPNG 存储就绪: {self.output_pcap}")
        except Exception as e:
            c_print(Fore.YELLOW if HAS_COLORAMA else "", f"[!] PcapNgWriter 初始化失败 ({e})，降级为标准 PcapWriter")
            self.writer = PcapWriter(self.output_pcap, append=True, sync=True)

    def handle_packet(self, pkt):
        """处理 Scapy 捕获的单条网络封包。"""
        if not pkt.haslayer(TCP) or not pkt.haslayer(IP):
            return

        with self.lock:
            self.packet_count += 1
            if self.writer:
                try:
                    self.writer.write(pkt)
                    if hasattr(self.writer, "f") and self.writer.f:
                        self.writer.f.flush()
                except Exception:
                    pass

        ip_layer = pkt[IP]
        tcp_layer = pkt[TCP]
        src_ip = ip_layer.src
        dst_ip = ip_layer.dst
        src_port = tcp_layer.sport
        dst_port = tcp_layer.dport
        raw_payload = bytes(pkt[TCP].payload)

        if not raw_payload:
            return

        is_game_traffic = (src_port in self.game_ports or dst_port in self.game_ports)
        is_web_traffic = (src_port in self.web_ports or dst_port in self.web_ports)

        # 1. 游戏私有 TCP 二进制协议解码
        if is_game_traffic and self.capture_mode in ("all", "game"):
            self.decode_game_stream(src_ip, src_port, dst_ip, dst_port, raw_payload)

        # 2. Web HTTP/HTTPS 登录与选区通信解码
        elif is_web_traffic and self.capture_mode in ("all", "web"):
            self.decode_web_traffic(src_ip, src_port, dst_ip, dst_port, raw_payload)

    def decode_game_stream(self, src_ip: str, src_port: int, dst_ip: str, dst_port: int, payload: bytes):
        """解析神仙道定长包头 [4B Length][2B Module][2B Action][Payload] 二进制协议。"""
        stream_key = (f"{src_ip}:{src_port}", f"{dst_ip}:{dst_port}")
        buf = self.stream_buffers.get(stream_key, b"") + payload

        is_client_to_server = (dst_port in self.game_ports)
        direction_tag = "上行 [客户端 -> 服务端]" if is_client_to_server else "下行 [服务端 -> 客户端]"
        dir_color = Fore.CYAN if is_client_to_server else Fore.GREEN

        # 循环解析流中可能黏附的多个完整封包
        while len(buf) >= 8:
            pkt_len, module_id, action_id = struct.unpack("!IHH", buf[:8])

            # 长度合法性过滤防误判
            if pkt_len < 4 or pkt_len > 10 * 1024 * 1024:
                # 字节流失步，推进 1 字节重新探测
                buf = buf[1:]
                continue

            total_frame_len = pkt_len + 4  # 包头 4 字节自身不包含在 pkt_len 中
            if len(buf) < total_frame_len:
                # 半包等待下一分段抵达
                break

            # 成功提取完整封包
            body = buf[8:total_frame_len]
            buf = buf[total_frame_len:]
            self.game_frame_count += 1

            # 模块与动作文本转义
            module_desc = MODULE_NAMES.get(module_id, f"未知模块 (Module {module_id})")
            action_desc = ACTION_NAMES.get((module_id, action_id), f"Action {action_id}")
            now_str = datetime.now().strftime("%H:%M:%S.%f")[:-3]

            # 检查透明 zlib 解压缩
            decompressed_hint = ""
            payload_view = body
            if len(body) >= 2 and body[0] == 0x78 and body[1] in (0x9C, 0x01, 0xDA):
                try:
                    decompressed = zlib.decompress(body)
                    decompressed_hint = f" [zlib 解压: {len(body)}B -> {len(decompressed)}B]"
                    payload_view = decompressed
                except Exception:
                    pass

            c_print(
                dir_color if HAS_COLORAMA else "",
                f"[{now_str}] {direction_tag} {src_ip}:{src_port} -> {dst_ip}:{dst_port} | "
                f"{module_desc} -> {action_desc} (Len: {pkt_len}B){decompressed_hint}"
            )
            print(format_hex_dump(payload_view, max_bytes=64))

        self.stream_buffers[stream_key] = buf

    def decode_web_traffic(self, src_ip: str, src_port: int, dst_ip: str, dst_port: int, payload: bytes):
        """解析 Web 登录认证 HTTP 请求行与响应状态。"""
        now_str = datetime.now().strftime("%H:%M:%S.%f")[:-3]
        text_preview = payload[:512].decode("latin1", errors="ignore")

        # HTTP 请求
        if any(text_preview.startswith(m) for m in ("GET ", "POST ", "HEAD ", "PUT ")):
            self.web_req_count += 1
            first_line = text_preview.split("\r\n")[0]
            host_match = re.search(r"Host:\s*([^\r\n]+)", text_preview, re.IGNORECASE)
            host = host_match.group(1) if host_match else "unknown"
            c_print(
                Fore.YELLOW if HAS_COLORAMA else "",
                f"[{now_str}] [WEB 请求] {src_ip}:{src_port} -> {dst_ip}:{dst_port} | {first_line} (Host: {host})"
            )

        # HTTP 响应
        elif text_preview.startswith("HTTP/1."):
            first_line = text_preview.split("\r\n")[0]
            loc_match = re.search(r"Location:\s*([^\r\n]+)", text_preview, re.IGNORECASE)
            loc = f" -> 重定向: {loc_match.group(1)}" if loc_match else ""
            c_print(
                Fore.MAGENTA if HAS_COLORAMA else "",
                f"[{now_str}] [WEB 响应] {src_ip}:{src_port} -> {dst_ip}:{dst_port} | {first_line}{loc}"
            )

    def close(self):
        """安全关闭 PCAP 写入器并打印统计。"""
        if self.writer:
            try:
                self.writer.close()
            except Exception:
                pass
        c_print(Fore.GREEN if HAS_COLORAMA else "", "\n================ 抓包统计 ================")
        print(f"  捕获总 TCP 数据包: {self.packet_count}")
        print(f"  解析游戏二进制封包: {self.game_frame_count}")
        print(f"  嗅探 Web 交互条目: {self.web_req_count}")
        print(f"  抓包文件已归档至: {self.output_pcap}")
        c_print(Fore.GREEN if HAS_COLORAMA else "", "==========================================")


def main():
    parser = argparse.ArgumentParser(description="神仙道官方微端全链路抓包与协议解析器")
    parser.add_argument("--exe", "-e", default=DEFAULT_TARGET_EXE, help="官方微端执行档路径")
    parser.add_argument("--output", "-o", default=None, help="生成的 pcapng 保存路径 (默认自动按时间戳保存至 data/captures/)")
    parser.add_argument("--mode", "-m", choices=["all", "game", "web"], default="all", help="抓包模式: all/game/web")
    parser.add_argument("--iface", "-i", default=None, help="监听的网络网卡名称/IP (默认自动适配)")
    parser.add_argument("--extra-port", "-p", type=int, action="append", default=[], help="追加监听的自定义端口")
    args = parser.parse_args()

    if not args.output:
        args.output = get_default_pcap_path("wd_capture")

    c_print(Fore.CYAN if HAS_COLORAMA else "", "==================================================")
    c_print(Fore.CYAN if HAS_COLORAMA else "", "   神仙道官方微端全链路网络抓包器 (Traffic Sniffer)  ")
    c_print(Fore.CYAN if HAS_COLORAMA else "", "==================================================")

    # 1. 管理员特权校验与自动请求提权
    if not is_admin():
        c_print(Fore.YELLOW if HAS_COLORAMA else "", "[!] 检测到当前非管理员权限。正在调用 Windows UAC 提权...")
        request_elevation()
        return

    # 2. 检查 Scapy 依赖安装状态
    if not HAS_SCAPY:
        c_print(Fore.RED if HAS_COLORAMA else "", "[-] 未检测到 scapy 依赖库。")
        print("    请在当前目录执行以下命令安装:")
        print("    pip install -r requirements.txt")
        sys.exit(1)

    # 3. 目标微端进程探测
    target_exe_basename = os.path.basename(args.exe)
    match_procs = find_matching_processes(["sxd_wd", "sxd", target_exe_basename])
    target_pids = {p[0] for p in match_procs}

    if match_procs:
        c_print(Fore.GREEN if HAS_COLORAMA else "", f"[+] 发现运行中的微端进程 ({len(match_procs)} 个):")
        for pid, name, path in match_procs:
            print(f"    - PID: {pid} | 进程: {name} | 路径: {path or 'N/A'}")
    else:
        c_print(Fore.YELLOW if HAS_COLORAMA else "", f"[*] 未检测到正在运行的微端进程，目标路径: {args.exe}")
        if os.path.exists(args.exe):
            c_print(Fore.BLUE if HAS_COLORAMA else "", "    - 建议启动微端完成登录与进入游戏。")
        else:
            c_print(Fore.YELLOW if HAS_COLORAMA else "", f"    - [提示] 目标文件当前不存在: {args.exe}")

    # 4. 计算需要监听的目标端口集
    game_ports = set(DEFAULT_GAME_PORTS)
    web_ports = set(DEFAULT_WEB_PORTS)
    for p in args.extra_port:
        game_ports.add(p)

    if target_pids:
        conns = get_tcp_connections(target_pids)
        for c in conns:
            if c["remote_port"] > 0:
                game_ports.add(c["remote_port"])
            if c["local_port"] > 0:
                game_ports.add(c["local_port"])
        print(f"[*] 结合微端连接，动态扩展监控端口至: {sorted(list(game_ports))}")

    # 构建 BPF 过滤表达式
    port_clauses = [f"tcp port {p}" for p in sorted(list(game_ports | web_ports))]
    bpf_filter = " or ".join(port_clauses)
    print(f"[*] 设定 BPF 过滤器: {bpf_filter[:120]} ... (共 {len(port_clauses)} 个端口规则)")

    # 5. 启动抓包会话
    session = TrafficCaptureSession(
        output_pcap=args.output,
        game_ports=game_ports,
        web_ports=web_ports,
        capture_mode=args.mode
    )

    c_print(Fore.GREEN if HAS_COLORAMA else "", "\n[*] 嗅探引擎已启动，正在监听网络流量中... 按 Ctrl+C 停止抓包\n")

    stop_event = threading.Event()

    # 注册 Windows 底层控制台事件钩子 (强力捕获 Ctrl+C 与关闭窗口事件)
    @ctypes.WINFUNCTYPE(ctypes.c_bool, wintypes.DWORD)
    def win32_ctrl_handler(ctrl_type):
        stop_event.set()
        return True

    kernel32.SetConsoleCtrlHandler(win32_ctrl_handler, True)

    try:
        while not stop_event.is_set():
            sniff(
                filter=bpf_filter,
                prn=session.handle_packet,
                store=False,
                iface=args.iface,
                timeout=1,
                stop_filter=lambda p: stop_event.is_set(),
            )
    except KeyboardInterrupt:
        c_print(Fore.YELLOW if HAS_COLORAMA else "", "\n[*] 接收到用户中断信号，正在退出抓包...")
    except Exception as e:
        c_print(Fore.RED if HAS_COLORAMA else "", f"\n[-] 嗅探过程中捕获异常: {e}")
    finally:
        session.close()


if __name__ == "__main__":
    main()
