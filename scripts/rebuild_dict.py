#!/usr/bin/env python3
"""从官方客户端 SWF (Main.swf + templet.swf) 从零反编译提取并构建全新 data/gamedata.db。

用法:
    # 使用已下载的 2026112001 缓存从零构建 data/gamedata.db
    python3 scripts/rebuild_dict.py --ver 2026112001 --diff r395-01 --skip-fetch

    # 从官方 CDN 拉取最新版本并从零构建 data/gamedata.db
    python3 scripts/rebuild_dict.py --ver 2026112001 --diff r395-01

零外部依赖（仅使用 Python 标准库），内建 AVM2 DoABC 静态常量池与 cinit 字节码求值器，
从零生成 35 张官方静态数据表（10 张核心运行表 + 25 张官方 SWF 扩展表）与 _rebuild_meta。
"""

import argparse
import datetime
import hashlib
import os
import shutil
import sqlite3
import struct
import urllib.request
import zlib
from collections import defaultdict


CDN_BASE = "http://uri.xdcdn.net/sxd/"


def fetch_bytes(url: str) -> bytes:
    print(f"[fetch] GET {url}")
    req = urllib.request.Request(url, headers={"User-Agent": "Mozilla/5.0"})
    with urllib.request.urlopen(req, timeout=60) as resp:
        return resp.read()


def resolve_and_fetch_swfs(cur_ver: str, diff_tag: str, out_dir: str) -> dict:
    """按官方 Verison/WDURI 规则解析 CDN 树并下载 Index.swf、Main.swf、templet.swf。"""
    os.makedirs(out_dir, exist_ok=True)
    idx_raw = fetch_bytes(f"{CDN_BASE}index/{cur_ver}")
    try:
        idx = zlib.decompress(idx_raw)
    except Exception:
        idx = idx_raw

    diff = b""
    if diff_tag:
        try:
            diff_raw = fetch_bytes(f"{CDN_BASE}diff/{cur_ver}..{cur_ver}-{diff_tag}")
            try:
                diff = zlib.decompress(diff_raw)
            except Exception:
                diff = diff_raw
        except Exception as exc:
            print(f"[warn] diff fetch skipped: {exc}")

    tree = {}
    for buf in (idx, diff):
        for i in range(len(buf) // 25):
            rec = buf[i * 25 : (i + 1) * 25]
            tree[rec[:5].hex()] = rec[5:].hex()

    def dir_hash(path: str) -> str:
        return tree.get(hashlib.sha1(path.encode("utf-8")).hexdigest()[:10], "")

    game_hash = dir_hash("game")
    assets_hash = dir_hash("game/assets")
    if not game_hash or not assets_hash:
        raise RuntimeError(f"无法解析 CDN 目录哈希: game={game_hash!r}, game/assets={assets_hash!r}")

    downloads = [
        ("Index.swf", game_hash),
        ("Main.swf", game_hash),
        ("templet.swf", assets_hash),
    ]
    hashes = {"game": game_hash, "game/assets": assets_hash}
    for fn, h in downloads:
        data = fetch_bytes(f"{CDN_BASE}tree/{h}/{fn}")
        dest = os.path.join(out_dir, fn)
        with open(dest, "wb") as f:
            f.write(data)
        print(f"[saved] {dest} ({len(data):,} bytes)")
    return hashes


def extract_abc_from_swf(path: str) -> memoryview:
    with open(path, "rb") as f:
        raw = f.read()
    magic = raw[:3]
    if magic == b"CWS":
        body = zlib.decompress(raw[8:])
    elif magic == b"FWS":
        body = raw[8:]
    else:
        raise ValueError(f"不支持的 SWF 魔数 {magic!r}: {path}")
    nbits = body[0] >> 3
    pos = ((5 + 4 * nbits + 7) // 8) + 4
    while pos < len(body):
        hdr = struct.unpack_from("<H", body, pos)[0]
        pos += 2
        tag_type, tag_len = hdr >> 6, hdr & 0x3F
        if tag_len == 0x3F:
            tag_len = struct.unpack_from("<I", body, pos)[0]
            pos += 4
        if tag_type == 82:  # DoABC
            nul = body.index(b"\x00", pos + 4)
            return memoryview(body)[nul + 1 : pos + tag_len]
        if tag_type == 0:
            break
        pos += tag_len
    raise RuntimeError(f"未在 {path} 中找到 DoABC 标签")


class ABCParser:
    """轻量级 AVM2 ABC 常量池与类静态初始化器 (cinit) 求值器。"""

    def __init__(self, data: memoryview):
        self.d = data
        self.p = 0
        self.minor, self.major = self.u16(), self.u16()
        n = self.u30()
        self.ints = [0] + [self.s32() for _ in range(max(0, n - 1))]
        n = self.u30()
        self.uints = [0] + [self.u30() for _ in range(max(0, n - 1))]
        n = self.u30()
        self.doubles = [0.0] + [self.f64() for _ in range(max(0, n - 1))]
        n = self.u30()
        self.strings = [""] + [
            self.read_bytes(self.u30()).decode("utf-8", "replace")
            for _ in range(max(0, n - 1))
        ]
        n = self.u30()
        self.namespaces = [None]
        for _ in range(max(0, n - 1)):
            k = self.u8()
            nm = self.u30()
            self.namespaces.append((k, self.strings[nm]))
        for _ in range(max(0, self.u30() - 1)):
            for _ in range(self.u30()):
                self.u30()
        n = self.u30()
        self.multinames = [None]
        for _ in range(max(0, n - 1)):
            k = self.u8()
            if k in (0x07, 0x0D):
                ns, nm = self.u30(), self.u30()
                self.multinames.append((self.namespaces[ns][1] if ns else "", self.strings[nm]))
            elif k in (0x0F, 0x10):
                self.multinames.append(("", self.strings[self.u30()]))
            elif k in (0x11, 0x12):
                self.multinames.append(("", ""))
            elif k in (0x09, 0x0E):
                nm = self.u30()
                self.u30()
                self.multinames.append(("", self.strings[nm]))
            elif k in (0x1B, 0x1C):
                self.u30()
                self.multinames.append(("", ""))
            elif k == 0x1D:
                self.u30()
                for _ in range(self.u30()):
                    self.u30()
                self.multinames.append(("", ""))
            else:
                raise ValueError(f"未知 multiname 类型: {k:#x}")
        for _ in range(self.u30()):
            pc = self.u30()
            self.u30()
            for _ in range(pc):
                self.u30()
            self.u30()
            fl = self.u8()
            if fl & 0x08:
                for _ in range(self.u30()):
                    self.u30()
                    self.u8()
            if fl & 0x80:
                for _ in range(pc):
                    self.u30()
        for _ in range(self.u30()):
            self.u30()
            for _ in range(self.u30()):
                self.u30()
                self.u30()
        cc = self.u30()
        inst = []
        for _ in range(cc):
            nm = self.u30()
            self.u30()
            fl = self.u8()
            if fl & 0x08:
                self.u30()
            for _ in range(self.u30()):
                self.u30()
            self.u30()
            self.skip_traits()
            inst.append(self.multinames[nm])
        self.class_cinit = {}
        for i in range(cc):
            ci = self.u30()
            self.skip_traits()
            fqn = f"{inst[i][0]}.{inst[i][1]}" if inst[i][0] else inst[i][1]
            self.class_cinit[fqn] = ci
        for _ in range(self.u30()):
            self.u30()
            self.skip_traits()
        self.bodies = {}
        for _ in range(self.u30()):
            mid = self.u30()
            self.u30()
            self.u30()
            self.u30()
            self.u30()
            cl = self.u30()
            self.bodies[mid] = self.d[self.p : self.p + cl]
            self.p += cl
            for _ in range(self.u30()):
                self.u30()
                self.u30()
                self.u30()
                self.u30()
                self.u30()
            self.skip_traits()

    def u8(self) -> int:
        v = self.d[self.p]
        self.p += 1
        return v

    def u16(self) -> int:
        v = struct.unpack_from("<H", self.d, self.p)[0]
        self.p += 2
        return v

    def u30(self) -> int:
        r = s = 0
        for _ in range(5):
            b = self.d[self.p]
            self.p += 1
            r |= (b & 0x7F) << s
            if not (b & 0x80):
                return r
            s += 7
        return r

    def s32(self) -> int:
        v = self.u30()
        return v - 0x100000000 if (v & 0x80000000) else v

    def f64(self) -> float:
        v = struct.unpack_from("<d", self.d, self.p)[0]
        self.p += 8
        return v

    def read_bytes(self, n: int) -> bytes:
        v = bytes(self.d[self.p : self.p + n])
        self.p += n
        return v

    def skip_traits(self):
        for _ in range(self.u30()):
            self.u30()
            b = self.u8()
            k, a = b & 0x0F, b >> 4
            if k in (0, 6):
                self.u30()
                self.u30()
                if self.u30():
                    self.u8()
            elif k in (1, 2, 3, 4, 5):
                self.u30()
                self.u30()
            if a & 0x04:
                for _ in range(self.u30()):
                    self.u30()

    def eval_cinit(self, fqn: str) -> dict:
        code = self.bodies[self.class_cinit[fqn]]
        p, n = 0, len(code)

        def u8():
            nonlocal p
            v = code[p]
            p += 1
            return v

        def s8():
            v = u8()
            return v - 256 if v >= 128 else v

        def u30():
            nonlocal p
            r = s = 0
            for _ in range(5):
                b = code[p]
                p += 1
                r |= (b & 0x7F) << s
                if not (b & 0x80):
                    return r
                s += 7
            return r

        st, props = [], {}
        while p < n:
            op = u8()
            if op in (0xD0, 0xD1, 0xD2, 0xD3):
                st.append(None)
            elif op == 0x30:
                st.pop()
            elif op == 0x47:
                break
            elif op in (0x5E, 0x5D):
                st.append(("T", u30()))
            elif op == 0x24:
                st.append(s8())
            elif op == 0x25:
                v = u30()
                st.append(v - 0x100000000 if v & 0x80000000 else v)
            elif op == 0x2D:
                st.append(self.ints[u30()])
            elif op == 0x2E:
                st.append(self.uints[u30()])
            elif op == 0x2F:
                st.append(self.doubles[u30()])
            elif op == 0x2C:
                st.append(self.strings[u30()])
            elif op in (0x20, 0x21):
                st.append(None)
            elif op == 0x26:
                st.append(True)
            elif op == 0x27:
                st.append(False)
            elif op == 0x28:
                st.append(float("nan"))
            elif op == 0x70:
                st[-1] = str(st[-1])
            elif op in (0x73, 0x74):
                st[-1] = int(st[-1]) if st[-1] is not None else 0
            elif op == 0x75:
                st[-1] = float(st[-1]) if st[-1] is not None else 0.0
            elif op == 0x76:
                st[-1] = bool(st[-1])
            elif op == 0x80:
                u30()
            elif op in (0x82, 0x85):
                pass
            elif op == 0x56:
                c = u30()
                arr = st[-c:] if c else []
                if c:
                    del st[-c:]
                st.append(arr)
            elif op == 0x55:
                c = u30()
                obj = {}
                if c:
                    it = st[-2 * c :]
                    del st[-2 * c :]
                    for i in range(0, 2 * c, 2):
                        obj[it[i]] = it[i + 1]
                st.append(obj)
            elif op in (0x68, 0x61):
                nm = self.multinames[u30()][1]
                v = st.pop()
                o = st.pop()
                if isinstance(o, tuple) and o[0] == "T":
                    props[nm] = v
                elif isinstance(o, dict):
                    o[nm] = v
            elif op == 0x66:
                nm = self.multinames[u30()][1]
                st.pop()
                st.append(props.get(nm, f"<{nm}>"))
            elif op == 0x60:
                nm = self.multinames[u30()][1]
                st.append(props.get(nm, f"<{nm}>"))
            elif op == 0x4A:
                nm = self.multinames[u30()][1]
                c = u30()
                a = st[-c:] if c else []
                if c:
                    del st[-c:]
                st.pop()
                st.append({} if nm == "Dictionary" else (nm, a))
            elif op in (0x42, 0x46):
                if op == 0x46:
                    u30()
                c = u30()
                if c:
                    del st[-c:]
                st.pop()
                st.append(None)
            elif op == 0x4F:
                u30()
                c = u30()
                if c:
                    del st[-c:]
                st.pop()
            elif op == 0xA0:
                b, a = st.pop(), st.pop()
                st.append(f"{a}{b}" if isinstance(a, str) or isinstance(b, str) else a + b)
            elif op == 0x90:
                st[-1] = -st[-1]
            elif op in (0xD4, 0xD5, 0xD6, 0xD7, 0x29, 0x1D):
                st.pop()
            elif op == 0x62:
                u30()
                st.append(None)
            elif op == 0x63:
                u30()
                st.pop()
            elif op == 0x2A:
                st.append(st[-1])
            elif op == 0x48:
                st.pop()
                break
            else:
                break
        return props


def safe_int(v, default=0) -> int:
    if v is None:
        return default
    try:
        return int(v)
    except Exception:
        return default


def safe_str(v, default="") -> str:
    if v is None:
        return default
    return str(v)


def rebuild_sqlite(db_path: str, swf_dir: str, cur_ver: str):
    templet_path = os.path.join(swf_dir, "templet.swf")
    main_path = os.path.join(swf_dir, "Main.swf")
    print(f"[parse] loading {templet_path} ...")
    tabc = ABCParser(extract_abc_from_swf(templet_path))
    print(f"[parse] loading {main_path} ...")
    mabc = ABCParser(extract_abc_from_swf(main_path))

    # 1. item
    raw_items = tabc.eval_cinit("com.assist.server.source.ItemTypeData")["Items"]
    item_rows = []
    for k, arr in raw_items.items():
        if not isinstance(arr, list) or len(arr) < 25:
            continue
        item_rows.append((
            safe_int(k),
            safe_str(arr[0]),
            safe_int(arr[1]),
            safe_str(arr[3]),
            safe_int(arr[4]),
            safe_int(arr[5]),
            safe_int(arr[23]),
            safe_int(arr[24]),
            safe_str(arr[29]) if len(arr) > 29 else "",
        ))
    item_rows.sort(key=lambda r: r[0])

    # 2. roletype
    raw_roles = mabc.eval_cinit("com.assist.server.RoleType")["Roles"]
    role_rows = []
    for k, arr in raw_roles.items():
        if not isinstance(arr, list) or len(arr) < 19:
            continue
        role_rows.append((
            safe_int(k),
            safe_str(arr[1]),
            safe_int(arr[2]),
            safe_int(arr[3]),
            safe_int(arr[4]),
            safe_int(arr[9]),
            safe_int(arr[10]),
            safe_int(arr[12]),
            safe_int(arr[18]),
        ))
    role_rows.sort(key=lambda r: r[0])

    # 3. Missions
    mt = tabc.eval_cinit("com.assist.server.source.MissionTypeData")
    raw_missions = mt["Missions"]
    raw_sections = mt["Sections"]
    mm_teams = tabc.eval_cinit("com.assist.server.source.MissionMonsterTypeData")["missionMonsterTeamList"]
    teams_by_mission = defaultdict(list)
    for row in mm_teams:
        if isinstance(row, list) and len(row) >= 2:
            teams_by_mission[safe_int(row[1])].append(str(safe_int(row[0])))

    mission_rows = []
    for k, arr in raw_missions.items():
        if not isinstance(arr, list) or len(arr) < 10:
            continue
        mid = safe_int(k)
        sec_id = safe_int(arr[0])
        sec = raw_sections.get(str(sec_id))
        town_map = safe_int(sec[2]) if isinstance(sec, list) and len(sec) > 2 else 0
        mapkey = safe_int(arr[10]) if len(arr) > 10 else 0
        monsters = ",".join(teams_by_mission.get(mid, []))
        mission_rows.append((
            mid,
            sec_id,
            safe_int(arr[1]),
            safe_int(arr[2]),
            town_map,
            mapkey,
            safe_str(arr[7]),
            safe_int(arr[8]),
            safe_int(arr[9]),
            monsters,
        ))
    mission_rows.sort(key=lambda r: r[0])

    # 4. Quests
    raw_quests = tabc.eval_cinit("com.assist.server.source.QuestTypeData")["Quests"]
    quest_rows = []
    for k, arr in raw_quests.items():
        if not isinstance(arr, list) or len(arr) < 18:
            continue
        quest_rows.append((
            safe_int(k),
            safe_int(arr[0]),
            safe_int(arr[1]),
            safe_int(arr[2]),
            safe_str(arr[3]),
            safe_int(arr[7]),
            safe_int(arr[8]),
            safe_int(arr[9]),
            safe_int(arr[10]),
            safe_int(arr[11]),
            safe_int(arr[12]),
            safe_int(arr[16]),
            safe_int(arr[17]),
        ))
    quest_rows.sort(key=lambda r: r[0])

    # 5. NPCposition
    raw_towns = tabc.eval_cinit("com.assist.server.source.TownTypeData")["Towns"]
    raw_town_npcs = mabc.eval_cinit("com.assist.server.TownNPCType")["List"]
    raw_npcs = mabc.eval_cinit("com.assist.server.NPCType")["List"]
    npc_pos_map = {}
    for town_k, tarr in raw_towns.items():
        if not isinstance(tarr, list) or len(tarr) < 3 or not isinstance(tarr[2], dict):
            continue
        town_id = safe_int(town_k)
        for sign, info in tarr[2].items():
            if not isinstance(info, list) or len(info) < 3:
                continue
            town_npc_id = safe_int(info[0])
            npcx, npcy = safe_int(info[1]), safe_int(info[2])
            tn = raw_town_npcs.get(str(town_npc_id))
            npc_id = safe_int(tn[1]) if isinstance(tn, list) and len(tn) >= 2 else town_npc_id
            npc_info = raw_npcs.get(str(npc_id))
            npc_name = safe_str(npc_info[1]) if isinstance(npc_info, list) and len(npc_info) >= 2 else sign
            npc_pos_map[town_npc_id] = (town_npc_id, town_id, npc_id, npc_name, npcx, npcy)
    # 补全 TownNPCType.List 中可能未在 TownTypeData 坐标表列出的条目
    for tn_k, tn in raw_town_npcs.items():
        tn_id = safe_int(tn_k)
        if tn_id in npc_pos_map or not isinstance(tn, list) or len(tn) < 2:
            continue
        town_id, npc_id = safe_int(tn[0]), safe_int(tn[1])
        npc_info = raw_npcs.get(str(npc_id))
        npc_name = safe_str(npc_info[1]) if isinstance(npc_info, list) and len(npc_info) >= 2 else f"NPC_{npc_id}"
        npc_pos_map[tn_id] = (tn_id, town_id, npc_id, npc_name, 0, 0)
    npc_rows = sorted(npc_pos_map.values(), key=lambda r: r[0])

    # 6. wishpoolaward (天帝宝库自选大奖表 TreasureHouseTypeData.objHouseAward)
    th_data = mabc.eval_cinit("com.assist.server.source.TreasureHouseTypeData")
    th_obj = th_data.get("objHouseAward") or {}
    wish_rows = []
    for k, entry in th_obj.items():
        if not isinstance(entry, dict):
            continue
        wish_rows.append((
            safe_int(entry.get("id", k)),
            safe_int(entry.get("item_id")),
            safe_int(entry.get("item_num")),
            safe_int(entry.get("quality")),
            safe_int(entry.get("sort")),
            safe_int(entry.get("award_type")),
            safe_int(entry.get("func_id")),
        ))
    wish_rows.sort(key=lambda r: r[0])

    # 7. NPCQuest (NPC 结交每日问答 NpcFriendShipType.QuestList)
    # 答案编码对齐 protocol.NpcFriendshipResultAnswerA(13)/B(14)/C(15)，-1 永不选取
    raw_npc_quests = mabc.eval_cinit("com.assist.server.NpcFriendShipType").get("QuestList", [])
    npc_quest_rows = []
    for q in raw_npc_quests:
        if not isinstance(q, dict):
            continue
        aa, ab, ac = safe_int(q.get("award_a")), safe_int(q.get("award_b")), safe_int(q.get("award_c"))
        candidates = [(13, aa), (14, ab), (15, ac)]
        best_ans, best_award = 0, -1
        for ans_code, aw in candidates:
            if aw > best_award:
                best_award = aw
                best_ans = ans_code
        npc_quest_rows.append((
            safe_int(q.get("id")),
            safe_str(q.get("question")),
            safe_str(q.get("answer_a")),
            safe_str(q.get("answer_b")),
            safe_str(q.get("answer_c")),
            aa, ab, ac,
            best_ans, best_award,
        ))
    npc_quest_rows.sort(key=lambda r: r[0])

    # 8. beelzebub_boss (魔王试炼 Boss 表 BeelzeBubTrialsBossType.Bosses)
    raw_bb = mabc.eval_cinit("com.assist.server.BeelzeBubTrialsBossType").get("Bosses", {})
    beelzebub_rows = []
    for k, arr in raw_bb.items():
        if not isinstance(arr, list) or len(arr) < 16:
            continue
        beelzebub_rows.append((
            safe_int(k),
            safe_str(arr[0]),
            safe_str(arr[1]),
            safe_int(arr[4]),
            safe_int(arr[15]),
            safe_int(arr[10]),
        ))
    beelzebub_rows.sort(key=lambda r: r[0])

    # 9. faxiang_pray_award (主角法相造物佛手祈愿炼化奖励 MainRoleFaXiangType.extraPrayAwardList)
    raw_fx = mabc.eval_cinit("com.assist.server.MainRoleFaXiangType").get("extraPrayAwardList", [])
    faxiang_rows = []
    for e in raw_fx:
        if not isinstance(e, dict):
            continue
        faxiang_rows.append((
            safe_int(e.get("id")),
            safe_int(e.get("level")),
            safe_int(e.get("item_id")),
            safe_int(e.get("item_count")),
            safe_int(e.get("cost_dao_yuan")),
            safe_int(e.get("cost_time")),
            safe_int(e.get("add_exp")),
        ))
    faxiang_rows.sort(key=lambda r: r[0])

    # 10. functions (功能开放字典 FunctionTypeData.List)
    raw_funcs = tabc.eval_cinit("com.assist.server.source.FunctionTypeData").get("List", {})
    function_rows = []
    for k, arr in raw_funcs.items():
        if not isinstance(arr, list) or len(arr) < 3:
            continue
        function_rows.append((
            safe_int(k),
            safe_str(arr[0]),
            safe_int(arr[1]),
            safe_str(arr[2]),
            safe_str(arr[3]) if len(arr) > 3 else "",
        ))
    function_rows.sort(key=lambda r: r[0])

    # ===== 官方 SWF 扩展静态表 (25 张，覆盖所有存在于官方 SWF 的游戏字典) =====

    # 11. ActivityArray
    act_rows = [
        (safe_int(e.get("id")), safe_str(e.get("sign")), safe_str(e.get("name")))
        for e in mabc.eval_cinit("com.assist.server.source.ActivityTemplate").get("ActivityArray", [])
        if isinstance(e, dict)
    ]
    act_rows.sort(key=lambda r: r[0])

    # 12. ChallengeType
    challenge_rows = [
        (safe_int(a[0]), safe_str(a[2]), safe_str(a[3]) if len(a) > 3 else "")
        for a in mabc.eval_cinit("com.assist.server.StChallengeType").get("MissionMonsterTeamSpecialPattern", [])
        if isinstance(a, list) and len(a) >= 3
    ]
    challenge_rows.sort(key=lambda r: r[0])

    # 13-14. DragonBall & DragonBallResource
    db_cinit = mabc.eval_cinit("com.assist.server.DragonBallType")
    dragonball_rows = [
        (
            safe_int(a[0]), safe_int(a[1]), safe_int(a[2]), safe_str(a[3]),
            safe_str(a[4]), safe_int(a[5]), safe_int(a[6]), safe_int(a[7]),
        )
        for a in db_cinit.get("DragonBall", [])
        if isinstance(a, list) and len(a) >= 8
    ]
    dragonball_rows.sort(key=lambda r: r[0])
    dragonball_res_rows = [
        (safe_int(a[0]), safe_str(a[1]), safe_str(a[2]), safe_str(a[3]) if len(a) > 3 else "")
        for a in db_cinit.get("DragonBallResource", [])
        if isinstance(a, list) and len(a) >= 3
    ]
    dragonball_res_rows.sort(key=lambda r: r[0])

    # 15. FiveElementsEquip
    five_elem_rows = [
        (safe_int(a[0]), safe_str(a[2]), safe_int(a[3]), safe_int(a[4]), safe_int(a[5]), safe_int(a[6]))
        for a in mabc.eval_cinit("com.assist.server.FiveElementsType").get("ElementEquipList", [])
        if isinstance(a, list) and len(a) >= 7
    ]
    five_elem_rows.sort(key=lambda r: r[0])

    # 16. Furinkazan
    furin_rows = [
        (safe_int(e.get("id")), safe_int(e.get("type")), safe_int(e.get("begin_mission")), safe_int(e.get("end_mission")))
        for e in mabc.eval_cinit("com.assist.server.FurinkazanType").get("missionLineList", [])
        if isinstance(e, dict)
    ]
    furin_rows.sort(key=lambda r: r[0])

    # 17. IdentifyTreasureaward
    id_treasure_rows = [
        (safe_int(e.get("id")), safe_int(e.get("level")), safe_int(e.get("award_id")))
        for e in mabc.eval_cinit("com.assist.server.source.IdentifyTreasureTypeData").get("awardList", [])
        if isinstance(e, dict)
    ]
    id_treasure_rows.sort(key=lambda r: r[0])

    # 18. ImmortalFantasy
    immortal_rows = [
        (
            safe_int(a[0]), safe_int(a[1]), safe_int(a[2]), safe_int(a[3]),
            safe_int(a[4]), safe_int(a[5]), safe_int(a[6]), safe_int(a[7]),
            safe_int(a[8]), safe_int(a[9]), safe_int(a[10]),
        )
        for a in mabc.eval_cinit("com.assist.server.ImmortalFantasyType").get("immortalFantasyStage", [])
        if isinstance(a, list) and len(a) >= 11
    ]
    immortal_rows.sort(key=lambda r: (r[0], r[1]))

    # 19. Mapkey (城镇地图索引)
    mapkey_rows = [
        (safe_str(tarr[0]), safe_int(tk), safe_str(tarr[1]))
        for tk, tarr in raw_towns.items()
        if isinstance(tarr, list) and len(tarr) >= 2
    ]
    mapkey_rows.sort(key=lambda r: r[1])

    # 20. MascotBefall (按 id 去重，官方 MascotBefallArr 按等级展开了多条同名 buff)
    mascot_seen = {}
    for a in mabc.eval_cinit("com.assist.server.MascotBefallType").get("MascotBefallArr", []):
        if isinstance(a, list) and len(a) >= 3:
            mid = safe_int(a[0])
            if mid not in mascot_seen:
                mascot_seen[mid] = (mid, safe_str(a[2]))
    mascot_rows = sorted(mascot_seen.values(), key=lambda r: r[0])

    # 21. PartnerExpeditionmission
    partner_exp_rows = [
        (
            safe_int(e.get("id")), safe_str(e.get("mission_name")), safe_str(e.get("describe")),
            safe_int(e.get("during_time")), safe_int(e.get("award_num")),
            safe_int(e.get("item_id")), safe_int(e.get("item_num")),
            safe_int(e.get("extra_mission_type")), safe_int(e.get("extra_mission_type2")),
            safe_int(e.get("extra_award_num")), safe_int(e.get("gift_item_id")),
        )
        for e in mabc.eval_cinit("com.assist.server.PartnerExpeditionType").get("missionList", [])
        if isinstance(e, dict)
    ]
    partner_exp_rows.sort(key=lambda r: r[0])

    # 22. PlayerTitles
    title_rows = [
        (safe_int(a[0]), safe_str(a[1]), safe_str(a[3]) if len(a) > 3 else "")
        for a in mabc.eval_cinit("com.assist.server.PlayerTitleType").get("PlayerTitles", [])
        if isinstance(a, list) and len(a) >= 2
    ]
    title_rows.sort(key=lambda r: r[0])

    # 23. RoleScrapInfos
    role_cinit = mabc.eval_cinit("com.assist.server.RoleType")
    scrap_rows = [
        (safe_int(a[0]), safe_int(a[1]), safe_str(a[3]), safe_str(a[4]), safe_int(a[5]), safe_int(a[6]))
        for a in role_cinit.get("RoleScrapInfos", [])
        if isinstance(a, list) and len(a) >= 7
    ]
    scrap_rows.sort(key=lambda r: r[0])

    # 24. StEightImmortalsType
    eight_imm_rows = [
        (safe_int(a[0]), safe_int(a[1]), safe_str(a[6]), safe_int(a[7]), safe_int(a[8]), safe_int(a[9]))
        for a in mabc.eval_cinit("com.assist.server.StEightImmortalsType").get("EightImmortalsRules", [])
        if isinstance(a, list) and len(a) >= 10
    ]
    eight_imm_rows.sort(key=lambda r: r[0])

    # 25-26. StShanhaiShop & relicPoint
    shanhai_cinit = mabc.eval_cinit("com.assist.server.source.StShanHaiRelicTypeData")
    shanhai_shop_rows = [
        (safe_int(e.get("id")), safe_int(e.get("item_id")), safe_int(e.get("item_num")), safe_int(e.get("price")), safe_int(e.get("limit")))
        for e in shanhai_cinit.get("shopArray", [])
        if isinstance(e, dict)
    ]
    shanhai_shop_rows.sort(key=lambda r: r[0])
    relic_point_rows = [
        (safe_int(e.get("id")), safe_int(e.get("relic_id")), safe_int(e.get("type")), safe_str(e.get("name")), safe_int(e.get("to_point_id")))
        for e in shanhai_cinit.get("relicPointArray", [])
        if isinstance(e, dict)
    ]
    relic_point_rows.sort(key=lambda r: r[0])

    # 27. TowerAwardList
    item_name_by_id = {r[0]: r[1] for r in item_rows}
    tower_award_rows = [
        (
            safe_int(e.get("id")), safe_int(e.get("item_id")),
            item_name_by_id.get(safe_int(e.get("item_id")), ""),
            safe_int(e.get("count")), safe_int(e.get("cost")), safe_int(e.get("limit")),
        )
        for e in mabc.eval_cinit("com.assist.server.ServerTowerType").get("scoreAwardList", [])
        if isinstance(e, dict)
    ]
    tower_award_rows.sort(key=lambda r: r[0])

    # 28. WorldPKExchange
    world_pk_rows = [
        (
            safe_int(e.get("id")), safe_int(e.get("item_id")), safe_int(e.get("count")),
            safe_int(e.get("pk_coins")), safe_int(e.get("limit")), safe_int(e.get("gold_coins")),
        )
        for e in mabc.eval_cinit("com.assist.server.WorldPkRankingType").get("WorldExchangeArray", [])
        if isinstance(e, dict)
    ]
    world_pk_rows.sort(key=lambda r: r[0])

    # 29. cardSoulList
    card_soul_rows = [
        (safe_int(a[0]), safe_str(a[2]), safe_int(a[7]), safe_int(a[8]))
        for a in mabc.eval_cinit("com.assist.server.CardSoulType").get("cardSoulCardList", [])
        if isinstance(a, list) and len(a) >= 9
    ]
    card_soul_rows.sort(key=lambda r: r[0])

    # 30. daoyuanshop
    daoyuan_rows = [
        (
            safe_int(a[0]), safe_str(a[1]), safe_str(a[2]), safe_str(a[3]),
            safe_int(a[4]), safe_int(a[5]), safe_int(a[6]), safe_int(a[7]),
            safe_int(a[8]), safe_int(a[9]), safe_int(a[10]), safe_int(a[11]),
            safe_int(a[12]), safe_int(a[13]), safe_int(a[14]),
        )
        for a in mabc.eval_cinit("com.assist.server.StDaoYuanShopType").get("DaoYuanData", [])
        if isinstance(a, list) and len(a) >= 15
    ]
    daoyuan_rows.sort(key=lambda r: r[0])

    # 31. factureReel
    facture_rows = [
        (idx + 1, safe_int(a[0]), safe_int(a[1]), safe_int(a[2]), safe_str(a[3]), safe_int(a[4]))
        for idx, a in enumerate(mabc.eval_cinit("com.assist.server.FactureReelType").get("_factureReelList", []))
        if isinstance(a, list) and len(a) >= 5
    ]

    # 32. fatetype
    fate_rows = [
        (safe_int(k), safe_str(a[0]), safe_int(a[2]), safe_int(a[4]), safe_int(a[10]) if len(a) > 10 else 0)
        for k, a in tabc.eval_cinit("com.assist.server.source.FateTypeData").get("Fates", {}).items()
        if isinstance(a, list) and len(a) >= 5
    ]
    fate_rows.sort(key=lambda r: r[0])

    # 33. newShopGoods
    new_shop_rows = [
        (
            safe_int(e.get("id", k)), safe_int(e.get("item_id")), safe_int(e.get("item_count")),
            safe_int(e.get("money_type")), safe_int(e.get("price")),
            safe_int(e.get("limit_buy_count")), safe_int(e.get("function_limit")),
        )
        for k, e in mabc.eval_cinit("com.assist.server.StTravelShopType").get("newShopGoods", {}).items()
        if isinstance(e, dict)
    ]
    new_shop_rows.sort(key=lambda r: r[0])

    # 34. serchat
    serchat_rows = [
        (safe_int(a[0]), safe_str(a[2]))
        for a in mabc.eval_cinit("com.assist.server.ServerChatRoomType").get("ChatRooms", [])
        if isinstance(a, list) and len(a) >= 3
    ]
    serchat_rows.sort(key=lambda r: r[0])

    # 35. soulList
    soul_rows = [
        (safe_int(k), safe_str(a[2]))
        for k, a in tabc.eval_cinit("com.assist.server.source.SoulTypeData").get("Soul", {}).items()
        if isinstance(a, list) and len(a) >= 3
    ]
    soul_rows.sort(key=lambda r: r[0])

    # ===== 从零构建全新 SQLite 文件并原子替换 =====
    os.makedirs(os.path.dirname(os.path.abspath(db_path)), exist_ok=True)
    tmp_path = db_path + ".tmp"
    if os.path.exists(tmp_path):
        os.remove(tmp_path)

    con = sqlite3.connect(tmp_path)
    ddl = """
    CREATE TABLE item (
        id INTEGER PRIMARY KEY, sxdname TEXT, type_id INTEGER, description TEXT,
        quality INTEGER, require_level INTEGER, price INTEGER, ingot INTEGER, special_gift_tip TEXT
    );
    CREATE INDEX idx_item_name ON item(sxdname);

    CREATE TABLE roletype (
        id INTEGER PRIMARY KEY, sxdname TEXT, JobId INTEGER, Type INTEGER,
        Fame INTEGER, StuntId INTEGER, UnlockMod INTEGER, InviteType INTEGER, Gender INTEGER
    );

    CREATE TABLE Missions (
        MissionsId INTEGER PRIMARY KEY, SectionId INTEGER, MissionLock INTEGER, MissionPower INTEGER,
        map INTEGER, mapkey INTEGER, MissionName TEXT, MissionType INTEGER, isBossMission INTEGER, monsters TEXT
    );
    CREATE INDEX idx_missions_type_lock ON Missions(MissionType, MissionLock);

    CREATE TABLE Quests (
        QuestsId INTEGER PRIMARY KEY, Type INTEGER, Lock INTEGER, Level INTEGER, Title TEXT,
        BeginNPCId INTEGER, EndNPCId INTEGER, AwardExperience INTEGER, AwardCoins INTEGER,
        AwardItemId INTEGER, TalkQuest INTEGER, MissionId INTEGER, AwardItemCount INTEGER
    );
    CREATE INDEX idx_quests_type_level ON Quests(Type, Level);

    CREATE TABLE NPCposition (
        id INTEGER PRIMARY KEY, townid INTEGER, npcid INTEGER, npcname TEXT, npcx INTEGER, npcy INTEGER
    );
    CREATE INDEX idx_npcpos_npcid ON NPCposition(npcid);
    CREATE INDEX idx_npcpos_townid ON NPCposition(townid);

    CREATE TABLE wishpoolaward (
        id INTEGER PRIMARY KEY, sxdname INTEGER, itemnum INTEGER,
        quality INTEGER, sort INTEGER, award_type INTEGER, func_id INTEGER
    );

    CREATE TABLE NPCQuest (
        id INTEGER PRIMARY KEY, question TEXT,
        answer_a TEXT, answer_b TEXT, answer_c TEXT,
        award_a INTEGER, award_b INTEGER, award_c INTEGER,
        best_answer INTEGER, best_award INTEGER
    );

    CREATE TABLE beelzebub_boss (
        id INTEGER PRIMARY KEY, name TEXT, sign TEXT, level INTEGER, max_times INTEGER, secret INTEGER
    );

    CREATE TABLE faxiang_pray_award (
        id INTEGER PRIMARY KEY, level INTEGER, item_id INTEGER, item_count INTEGER,
        cost_dao_yuan INTEGER, cost_time INTEGER, add_exp INTEGER
    );

    CREATE TABLE functions (
        id INTEGER PRIMARY KEY, sign TEXT, open_level INTEGER, name TEXT, description TEXT
    );

    CREATE TABLE ActivityArray (id INTEGER PRIMARY KEY, sign TEXT, name TEXT);
    CREATE TABLE ChallengeType (id INTEGER PRIMARY KEY, sxdname TEXT, description TEXT);
    CREATE TABLE DragonBall (
        id INTEGER PRIMARY KEY, effect_id INTEGER, quality_id INTEGER, sign TEXT,
        sxdname TEXT, star INTEGER, unlock_map_id INTEGER, need_long_hun INTEGER
    );
    CREATE TABLE DragonBallResource (id INTEGER PRIMARY KEY, sign TEXT, sxdname TEXT, desc TEXT);
    CREATE TABLE FiveElementsEquip (
        id INTEGER PRIMARY KEY, sxdname TEXT, property_id INTEGER, quality INTEGER,
        exchange_exp INTEGER, five_dust INTEGER
    );
    CREATE TABLE Furinkazan (id INTEGER PRIMARY KEY, type INTEGER, begin_mission INTEGER, end_mission INTEGER);
    CREATE TABLE IdentifyTreasureaward (id INTEGER PRIMARY KEY, level INTEGER, award_id INTEGER);
    CREATE TABLE ImmortalFantasy (
        mission_seq_id INTEGER, stage_id INTEGER, stage_video_id INTEGER, stage_exp_id INTEGER,
        special_flag INTEGER, monster_sequence INTEGER, monster_scene_id INTEGER, boxes_id INTEGER,
        npc_id INTEGER, item_id INTEGER, supernatural_piece_id INTEGER
    );
    CREATE TABLE Mapkey (mapkey TEXT, mapid INTEGER PRIMARY KEY, mapname TEXT);
    CREATE TABLE MascotBefall (id INTEGER PRIMARY KEY, sxdname TEXT);
    CREATE TABLE PartnerExpeditionmission (
        id INTEGER PRIMARY KEY, mission_name TEXT, describe TEXT, during_time INTEGER,
        award_num INTEGER, item_id INTEGER, item_num INTEGER,
        extra_mission_type INTEGER, extra_mission_type2 INTEGER, extra_award_num INTEGER, gift_item_id INTEGER
    );
    CREATE TABLE PlayerTitles (id INTEGER PRIMARY KEY, sxdname TEXT, description TEXT);
    CREATE TABLE RoleScrapInfos (
        id INTEGER PRIMARY KEY, role_id INTEGER, sxdname TEXT, description TEXT, itemId INTEGER, sort INTEGER
    );
    CREATE TABLE StEightImmortalsType (
        id INTEGER PRIMARY KEY, rule_level INTEGER, eightname TEXT, attr1 INTEGER, attr2 INTEGER, attr3 INTEGER
    );
    CREATE TABLE StShanhaiShop (id INTEGER PRIMARY KEY, item_id INTEGER, item_num INTEGER, price INTEGER, limit1 INTEGER);
    CREATE TABLE TowerAwardList (id INTEGER PRIMARY KEY, item_id INTEGER, name TEXT, count INTEGER, cost INTEGER, exlimit INTEGER);
    CREATE TABLE WorldPKExchange (id INTEGER PRIMARY KEY, item_id INTEGER, count INTEGER, pk_coins INTEGER, "limit" INTEGER, gold_coins INTEGER);
    CREATE TABLE cardSoulList (id INTEGER PRIMARY KEY, sxdname TEXT, exchange_scrap INTEGER, resolve_scrap INTEGER);
    CREATE TABLE daoyuanshop (
        id INTEGER PRIMARY KEY, sign TEXT, sxdname TEXT, decription TEXT,
        daoyuan_price INTEGER, ingot_price INTEGER, item_count INTEGER, quality INTEGER,
        order_id INTEGER, is_max_limit INTEGER, item_id INTEGER, need_vip INTEGER,
        vip_item_id INTEGER, coin_price INTEGER, item_num INTEGER
    );
    CREATE TABLE factureReel (
        ID INTEGER PRIMARY KEY, reel_id INTEGER, item_id INTEGER, item_number INTEGER,
        item_description TEXT, mission_id INTEGER
    );
    CREATE INDEX idx_facture_reel_id ON factureReel(reel_id);
    CREATE TABLE fatetype (id INTEGER PRIMARY KEY, sxdname TEXT, Quality INTEGER, WarAttributeType INTEGER, NeedActived INTEGER);
    CREATE TABLE newShopGoods (
        id INTEGER PRIMARY KEY, item_id INTEGER, item_count INTEGER, money_type INTEGER,
        price INTEGER, limit_buy_count INTEGER, function_limit INTEGER
    );
    CREATE TABLE relicPoint (id INTEGER PRIMARY KEY, relic_id INTEGER, type INTEGER, name TEXT, to_point_id INTEGER);
    CREATE TABLE serchat (id INTEGER PRIMARY KEY, sxdname TEXT);
    CREATE TABLE soulList (id INTEGER PRIMARY KEY, sxdname TEXT);
    CREATE TABLE _rebuild_meta (key TEXT PRIMARY KEY, value TEXT);
    """

    table_inserts = [
        ("item", "INSERT INTO item VALUES (?,?,?,?,?,?,?,?,?)", item_rows),
        ("roletype", "INSERT INTO roletype VALUES (?,?,?,?,?,?,?,?,?)", role_rows),
        ("Missions", "INSERT INTO Missions VALUES (?,?,?,?,?,?,?,?,?,?)", mission_rows),
        ("Quests", "INSERT INTO Quests VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?)", quest_rows),
        ("NPCposition", "INSERT INTO NPCposition VALUES (?,?,?,?,?,?)", npc_rows),
        ("wishpoolaward", "INSERT INTO wishpoolaward VALUES (?,?,?,?,?,?,?)", wish_rows),
        ("NPCQuest", "INSERT INTO NPCQuest VALUES (?,?,?,?,?,?,?,?,?,?)", npc_quest_rows),
        ("beelzebub_boss", "INSERT INTO beelzebub_boss VALUES (?,?,?,?,?,?)", beelzebub_rows),
        ("faxiang_pray_award", "INSERT INTO faxiang_pray_award VALUES (?,?,?,?,?,?,?)", faxiang_rows),
        ("functions", "INSERT INTO functions VALUES (?,?,?,?,?)", function_rows),
        ("ActivityArray", "INSERT INTO ActivityArray VALUES (?,?,?)", act_rows),
        ("ChallengeType", "INSERT INTO ChallengeType VALUES (?,?,?)", challenge_rows),
        ("DragonBall", "INSERT INTO DragonBall VALUES (?,?,?,?,?,?,?,?)", dragonball_rows),
        ("DragonBallResource", "INSERT INTO DragonBallResource VALUES (?,?,?,?)", dragonball_res_rows),
        ("FiveElementsEquip", "INSERT INTO FiveElementsEquip VALUES (?,?,?,?,?,?)", five_elem_rows),
        ("Furinkazan", "INSERT INTO Furinkazan VALUES (?,?,?,?)", furin_rows),
        ("IdentifyTreasureaward", "INSERT INTO IdentifyTreasureaward VALUES (?,?,?)", id_treasure_rows),
        ("ImmortalFantasy", "INSERT INTO ImmortalFantasy VALUES (?,?,?,?,?,?,?,?,?,?,?)", immortal_rows),
        ("Mapkey", "INSERT INTO Mapkey VALUES (?,?,?)", mapkey_rows),
        ("MascotBefall", "INSERT INTO MascotBefall VALUES (?,?)", mascot_rows),
        ("PartnerExpeditionmission", "INSERT INTO PartnerExpeditionmission VALUES (?,?,?,?,?,?,?,?,?,?,?)", partner_exp_rows),
        ("PlayerTitles", "INSERT INTO PlayerTitles VALUES (?,?,?)", title_rows),
        ("RoleScrapInfos", "INSERT INTO RoleScrapInfos VALUES (?,?,?,?,?,?)", scrap_rows),
        ("StEightImmortalsType", "INSERT INTO StEightImmortalsType VALUES (?,?,?,?,?,?)", eight_imm_rows),
        ("StShanhaiShop", "INSERT INTO StShanhaiShop VALUES (?,?,?,?,?)", shanhai_shop_rows),
        ("TowerAwardList", "INSERT INTO TowerAwardList VALUES (?,?,?,?,?,?)", tower_award_rows),
        ("WorldPKExchange", "INSERT INTO WorldPKExchange VALUES (?,?,?,?,?,?)", world_pk_rows),
        ("cardSoulList", "INSERT INTO cardSoulList VALUES (?,?,?,?)", card_soul_rows),
        ("daoyuanshop", "INSERT INTO daoyuanshop VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)", daoyuan_rows),
        ("factureReel", "INSERT INTO factureReel VALUES (?,?,?,?,?,?)", facture_rows),
        ("fatetype", "INSERT INTO fatetype VALUES (?,?,?,?,?)", fate_rows),
        ("newShopGoods", "INSERT INTO newShopGoods VALUES (?,?,?,?,?,?,?)", new_shop_rows),
        ("relicPoint", "INSERT INTO relicPoint VALUES (?,?,?,?,?)", relic_point_rows),
        ("serchat", "INSERT INTO serchat VALUES (?,?)", serchat_rows),
        ("soulList", "INSERT INTO soulList VALUES (?,?)", soul_rows),
    ]

    with con:
        con.executescript(ddl)
        for tbl, sql, rows in table_inserts:
            con.executemany(sql, rows)

        now = datetime.datetime.now(datetime.timezone.utc).isoformat()
        meta = [
            ("cur_ver", cur_ver),
            ("rebuilt_at", now),
            ("table_count", str(len(table_inserts))),
        ] + [(f"{tbl}_rows", str(len(rows))) for tbl, _, rows in table_inserts]
        con.executemany("INSERT INTO _rebuild_meta (key, value) VALUES (?, ?)", meta)

    con.execute("VACUUM")
    con.close()
    os.replace(tmp_path, db_path)

    print(f"\n=== {db_path} 全量重建报告 (cur_ver={cur_ver}, 共 {len(table_inserts)} 张表) ===")
    total_rows = 0
    for tbl, _, rows in table_inserts:
        total_rows += len(rows)
        print(f"  {tbl:26s}: {len(rows):6d} 行")
    print(f"  {'TOTAL':26s}: {total_rows:6d} 行 ({os.path.getsize(db_path):,} bytes)")


def main():
    ap = argparse.ArgumentParser(description="从最新官方 SWF 从零反编译构建 data/gamedata.db")
    ap.add_argument("--ver", default="2026112001", help="官方 cur_ver (默认 2026112001)")
    ap.add_argument("--diff", default="r395-01", help="官方 diff_urls[cur_ver] (默认 r395-01)")
    ap.add_argument("--db", default="data/gamedata.db", help="目标 SQLite 路径 (默认 data/gamedata.db)")
    ap.add_argument("--swf-dir", default="", help="SWF 缓存目录 (默认 data/decompiled/swfs/<ver>)")
    ap.add_argument("--skip-fetch", action="store_true", help="跳过网络下载，直接使用本地缓存 SWF")
    ap.add_argument("--update-index-swf", action="store_true", help="同时覆盖根目录 Index.swf")
    args = ap.parse_args()

    swf_dir = args.swf_dir or os.path.join("data", "decompiled", "swfs", args.ver)
    if not args.skip_fetch:
        resolve_and_fetch_swfs(args.ver, args.diff, swf_dir)
    if args.update_index_swf:
        src_idx = os.path.join(swf_dir, "Index.swf")
        if os.path.exists(src_idx):
            shutil.copy2(src_idx, "Index.swf")
            print(f"[updated] Index.swf <- {src_idx}")
    rebuild_sqlite(args.db, swf_dir, args.ver)


if __name__ == "__main__":
    main()
