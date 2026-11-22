#!/usr/bin/env python3
"""从官方 2026 客户端 SWF (Main.swf + templet.swf) 全自动反编译提取并重建 data/Pieb.db 核心静态表。

用法:
    # 使用已下载的 2026112001 缓存重建 data/Pieb.db
    python3 scripts/rebuild_dict.py --ver 2026112001 --diff r395-01 --skip-fetch

    # 从官方 CDN 拉取最新版本并重建 data/Pieb.db
    python3 scripts/rebuild_dict.py --ver 2026112001 --diff r395-01

零外部依赖（仅使用 Python 标准库），内建 AVM2 DoABC 静态常量池与 cinit 字节码求值器，
重建 internal/dictionary/repository.go 实际使用的全部 6 张核心表：
  1. item           <- templet.swf :: com.assist.server.source.ItemTypeData.Items
  2. roletype       <- Main.swf    :: com.assist.server.RoleType.Roles
  3. Missions       <- templet.swf :: MissionTypeData.{Missions,Sections} + MissionMonsterTypeData.missionMonsterTeamList
  4. Quests         <- templet.swf :: com.assist.server.source.QuestTypeData.Quests
  5. NPCposition    <- templet.swf :: TownTypeData.Towns + Main.swf :: TownNPCType.List + NPCType.List
  6. wishpoolaward  <- Main.swf    :: com.assist.server.source.TreasureHouseTypeData.awardList
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

    # 6. wishpoolaward (天帝宝库自选大奖表 TreasureHouseTypeData.awardList)
    th_awards = mabc.eval_cinit("com.assist.server.source.TreasureHouseTypeData")["awardList"]
    wish_rows = []
    for entry in th_awards:
        if not isinstance(entry, dict):
            continue
        wish_rows.append((
            safe_int(entry.get("id")),
            safe_int(entry.get("item_id")),
            safe_int(entry.get("item_num")),
        ))
    wish_rows.sort(key=lambda r: r[0])

    # 写入 SQLite（单事务原子替换这 6 张核心表，保留其他历史表）
    con = sqlite3.connect(db_path)
    old_counts = {}
    for tbl in ("item", "roletype", "Missions", "Quests", "NPCposition", "wishpoolaward"):
        try:
            old_counts[tbl] = con.execute(f'SELECT count(*) FROM "{tbl}"').fetchone()[0]
        except Exception:
            old_counts[tbl] = 0

    with con:
        con.execute("DELETE FROM item")
        con.executemany(
            "INSERT INTO item (id, sxdname, type_id, description, quality, require_level, price, ingot, special_gift_tip) "
            "VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)",
            item_rows,
        )

        con.execute("DELETE FROM roletype")
        con.executemany(
            "INSERT INTO roletype (id, sxdname, JobId, Type, Fame, StuntId, UnlockMod, InviteType, Gender) "
            "VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)",
            role_rows,
        )

        con.execute("DELETE FROM Missions")
        con.executemany(
            "INSERT INTO Missions (MissionsId, SectionId, MissionLock, MissionPower, map, mapkey, MissionName, MissionType, isBossMission, monsters) "
            "VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)",
            mission_rows,
        )

        con.execute("DELETE FROM Quests")
        con.executemany(
            "INSERT INTO Quests (QuestsId, Type, Lock, Level, Title, BeginNPCId, EndNPCId, AwardExperience, AwardCoins, AwardItemId, TalkQuest, MissionId, AwardItemCount) "
            "VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)",
            quest_rows,
        )

        con.execute("DELETE FROM NPCposition")
        con.executemany(
            "INSERT INTO NPCposition (id, townid, npcid, npcname, npcx, npcy) "
            "VALUES (?, ?, ?, ?, ?, ?)",
            npc_rows,
        )

        con.execute("DELETE FROM wishpoolaward")
        con.executemany(
            "INSERT INTO wishpoolaward (id, sxdname, itemnum) VALUES (?, ?, ?)",
            wish_rows,
        )

        con.execute(
            "CREATE TABLE IF NOT EXISTS _rebuild_meta (key TEXT PRIMARY KEY, value TEXT)"
        )
        now = datetime.datetime.now(datetime.timezone.utc).isoformat()
        meta = [
            ("cur_ver", cur_ver),
            ("rebuilt_at", now),
            ("item_rows", str(len(item_rows))),
            ("roletype_rows", str(len(role_rows))),
            ("missions_rows", str(len(mission_rows))),
            ("quests_rows", str(len(quest_rows))),
            ("npcposition_rows", str(len(npc_rows))),
            ("wishpoolaward_rows", str(len(wish_rows))),
        ]
        con.executemany(
            "INSERT OR REPLACE INTO _rebuild_meta (key, value) VALUES (?, ?)",
            meta,
        )

    con.execute("VACUUM")
    con.close()

    print("\n=== Pieb.db 核心表重建报告 ===")
    new_counts = {
        "item": len(item_rows),
        "roletype": len(role_rows),
        "Missions": len(mission_rows),
        "Quests": len(quest_rows),
        "NPCposition": len(npc_rows),
        "wishpoolaward": len(wish_rows),
    }
    for tbl, new_n in new_counts.items():
        old_n = old_counts.get(tbl, 0)
        delta = new_n - old_n
        sign = "+" if delta >= 0 else ""
        print(f"  {tbl:15s}: {old_n:6d} -> {new_n:6d} ({sign}{delta})")


def main():
    ap = argparse.ArgumentParser(description="从最新官方 SWF 反编译重建 data/Pieb.db")
    ap.add_argument("--ver", default="2026112001", help="官方 cur_ver (默认 2026112001)")
    ap.add_argument("--diff", default="r395-01", help="官方 diff_urls[cur_ver] (默认 r395-01)")
    ap.add_argument("--db", default="data/Pieb.db", help="目标 SQLite 路径")
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
