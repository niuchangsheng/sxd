# 官方微端 AS3 协议字典与 Module 0 逆向解构报告 (Official AS3 Protocol & Module 0 Reconnaissance)

- **日期**: 2026-10-11
- **分析人员**: @Ateng
- **关联 Issue**: [#19](https://github.com/atengk/sxd-pie-ng/issues/19)
- **基准目标**: `C:\Users\kongyu\Desktop\sxd_wd_V3.11_1693793577.exe` 及 `data/captures/wd_capture_20261011_112551.pcapng`

---

## 一、 官方微端架构与资源分发机制

官方微端（`sxd_wd_*.exe`）是一个基于 Flash/ActiveX 的极简桌面包装壳程序（8.12 MB）。
1. **外壳机制**：启动时加载内部 Flash 运行时并访问运营平台（如 `member.fengwanyx.3fangyuan.com`）执行 Web 认证换票；
2. **CDN 动态分发**：主玩法逻辑、场景与协议类库并非全量打包在 exe 内，而是通过 Web 加载后由 CDN 域名（`uri.xdcdn.net/sxd/tree/*`）流式按需分发 SWF 资源（包含 `choose_mission.swf`、`mission_practice.swf`、`war.swf`、`get_award.swf` 等）；
3. **隔离解包归档**：所有从真实抓包会话流中提取的原始 SWF 资源均统一归档于 `data/decompiled/swfs/` 隔离目录，并通过 `.gitignore` 排除，保证 Git 仓库纯净零污染。

---

## 二、 Module 0 (`Mod_Player_Base`) 官方核心协议契约

在官方微端与服务端真实的 TCP 8381 游戏长连接通信中，Module 0 负责玩家会话全生命周期：

### 1. 登录握手与凭证校验 (`Action 0`: `Mod_Player_Base.login`)

- **客户端请求 (Client -> Server)**：
  - 封包结构：大端序 `[4B Length=104] + [2B Module=0] + [2B Action=0] + Payload`
  - 载荷字段映射：
    1. `Username` (string): `[2B Len] + UTF-8 string`（登录平台通行证）
    2. `Hash` (string): `[2B Len] + UTF-8 string`（动态 32 位 MD5 签名）
    3. `Time` (string): `[2B Len] + UTF-8 string`（时间戳）
    4. `Source` (string): `[2B Len] + UTF-8 string`（运营渠道如 `fengwanyx`）
    5. `EmptyPlaceholder1` (string): `[2B Len=0]`
    6. `EmptyPlaceholder2` (string): `[2B Len=0]`
    7. `EmptyPlaceholder3` (string): `[2B Len=0]`
    8. `RegDateTimestamp` (int32): 注册时间戳（如 `1655964000`）
    9. `ReservedByte` (int8): 保留字节 `0x00`
    10. `PlatformName` (string): `[2B Len] + UTF-8 string`（平台名称如 `"疯玩"`）
    11. `ClientType` (string): `[2B Len] + UTF-8 string`（客户端类型如 `"pc_wd"`）
    12. `Flag` (int8): 标志位 `0x01`
    13. `TailUsername` (string): `[2B Len] + UTF-8 string`（确认用户名）

- **服务端响应 (Server -> Client)**：
  - 封包结构：大端序 `[4B Length=26] + [2B Module=0] + [2B Action=0] + Payload`
  - 核心状态码：
    - `Payload[4] == 0x04` 代表 **`ResultCode = 4 (SUCCESS)`**，认证成功！
    - 其它非 4 状态码代表认证拒绝或凭证过期。

---

### 2. 长连接心跳保活与同步 (`Action 23`: `Mod_Player_Base.heartbeat`)

- 官方微端在长连接建立后，以高频周期性向服务器发送心跳探活封包；
- **客户端请求 (Client -> Server)**：
  - 封包结构：`[4B Length=8] + [2B Module=0] + [2B Action=23] + [4B Sequence/Timestamp]`
  - 初始参数通常为 `0x00000030`（48），后续携带累加或同步序列号。
- **服务端响应 (Server -> Client)**：
  - 返回确认应答 `0x06000a28`。

---

### 3. 同步服务器时间 (`Action 22`: `Mod_Player_Base.get_server_time`)

- **客户端请求**: `[4B Length=8] + [2B Module=0] + [2B Action=22] + [4B Param]`
- **服务端响应**: 返回当前服务器 Unix 时间戳。

---

### 4. 场景初始化进场 (`Action 72 & 99`: `Mod_Player_Base.enter_town`)

- **Action 72 (步 1)**：客户端发送 `[4B Length=8] + [2B Module=0] + [2B Action=72] + [4B 0]`，服务端返回角色所在城镇场景与分线数据。
- **Action 99 (步 2)**：客户端确认场景进入，参数携带前序动作号 `0x00000048`。

---

## 三、 Go 代码库镜像与对齐成果

在 `internal/protocol/` 中已完成对齐并保持 100% 单元测试通过：
1. **常量字典**:
   - `ActionIDPlayerLogin` (`0x00000000`)
   - `ActionIDPlayerGetInfo` (`0x00000002`)
   - `ActionIDPlayerUpdateData` (`0x00000003`)
   - `ActionIDPlayerHeartbeat` (`0x00000017` / Action 23)
   - `ActionIDPlayerServerTime` (`0x00000016` / Action 22)
   - `ActionIDPlayerInitStep1` (`0x00000048` / Action 72)
   - `ActionIDPlayerInitStep2` (`0x00000063` / Action 99)
2. **编解码与构建函数**:
   - `BuildPlayerLoginPacket(req)`
   - `ParsePlayerLoginAuthResponse(payload)` (断言 ResultCode=4)
   - `BuildPlayerHeartbeatPacket(seq)`
   - `BuildPlayerServerTimePacket(param)`
   - `BuildPlayerInitStep1Packet(param)` / `BuildPlayerInitStep2Packet(param)`
3. **单测验证**:
   - `TestPlayerLogin_BuildAndParse` (PASS)
   - `TestPlayerLogin_LivePcapWholeCompressedPacket` (PASS)
   - `TestPlayerLogin_HeartbeatAndServerTime` (PASS)
