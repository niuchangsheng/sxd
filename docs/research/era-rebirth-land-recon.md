# 轮回净地渡业台/渡业仙池抓包分析 (2026-11-10)

## 输入
用户提供的 Wireshark 报文列表 `/tmp/x`（仅长度与序号，无载荷字节），时间段约 30 秒。

## 报文时序推断

| 时间 | 客户端长度 | 服务端回包 | 推断 |
| --- | --- | --- | --- |
| 3.25s | 12B | 127B | 852/1 get_panel_info |
| 3.35s | 12B | 54B | 852/9 get_auto_profit |
| 6.23s | 12B | 146B | 另一大面板 |
| 11.33s | 12B | 51B + 36B | 一笔领取（疑 852/13） |
| 11.46s | 12B | 143B | 另一笔领取（疑 852/14） |
| 11.51s / 11.52s | 12B ×2 | 66B ×2 | 两次领取/刷新 |
| 15.20s | 12B | 32B | **852/12 auto_pool_panel（24B 载荷，与线上日志 payload_len=24 一致）** |
| **20.21s** | **16B** | **17B** | **852/16 draw [Int][PrevAct]；回包 9B = [UByte,Int,Int]** |
| 20.31s | 12B | 32B | 抽取后刷新 852/12 |
| **27.56s** | **16B** | **17B** | **第二次抽取** |
| 27.62s | 12B | 32B | 再次刷新 852/12 |

关键判据：16 字节请求 = 8 字节帧 + 4 字节 Int + 4 字节 PrevAct，正好对应
`852/16 draw [Int]`；回包 17 字节 = 8 + 9，与 `[UByte result, Int, Int]` 吻合。
抽取后客户端立即重发 852/12 刷新渡业仙池面板。

## 结论
- 「渡业台」入口是 852/16 draw；「渡业仙池」领取是 852/12/13/14（已实现）。
- 两次抽取间隔约 7 秒，Int 参数可能为档位/类型，需 hex 确认。
- 抽取是否消耗渡业符/元宝尚未确认，确认前不得自动调用。

## 待办
- 需要包 16548/16549、19697、16555 的 Hex Dump 确认 ActionID、Int 参数与结果码；
- 若抽取消耗渡业符（非元宝），可将 852/16 接入 rebirth_land 例程；
- 若消耗元宝，永久不接入。
## 2026-11-11 PrevAct 链补充（渡业台 852/16 静默丢弃根因）

实测（qq s0）：

- 登录后立即 `852/12 → 852/16`：成功；
- 先发任意普通业务请求（如 0/42 活跃度）再 `852/12 → 852/16`：**852/16 被静默丢弃**；
- 在中间插入 `0/2 get_player_info`（PrevAct=最近回包动作）后，再 `852/12(prev=0/2) → 852/16`：**恢复成功**。

结论：
1. 852/16 对 PrevAct 链极其敏感，长会话中链锚点会被无 PrevAct 的请求污染；
2. 例程在抽取前先发 0/2 重建链，并依次尝试候选锚点，直到 0/2 真正被处理
   （`LastAckActionID == 0x00000002`）；
3. 心跳回包不计入业务锚点（已从 LastAckActionID 中排除）。

## 2026-11-21 渡业台领取挂机收益 + 两次免费快速挂机（852/9、852/8）

### 输入
用户抓包 `/tmp/duyetai.pcapng`（轮回净地，渡业台领取挂机收益并触发两次免费快速挂机）。
游戏连接为 `10.0.0.13:61029 → 81.70.56.121:8200`，客户端上行 16 帧、服务端下行 21 帧。
该连接为整包 zlib 压缩格式：`[4B len][zlib(4B ActionID + payload)]`，解压后按标准 8 字节头解析。

### 客户端请求时序（全部 12 字节 = 8 字节头 + 4 字节 PrevAct）

| # | ActionID | 请求载荷 | PrevAct |
| -: | --- | --- | --- |
| 1 | 851/0 | `[]` | 0x00EB000D |
| 2 | 851/1 | `[]` | 851/0 |
| 3 | 851/4 | `[]` | 851/1 |
| 4 | **852/1** | `[]` | 851/4 |
| 5 | **852/9** | `[]` | 852/1 |
| 6 | 852/1 | `[]` | 852/9 |
| 7 | 52/0 (助手面板) | `[]` | 852/1 |
| 8 | 52/0 | `[]` | 52/0 |
| 9 | **701/0 get_state** | `[Int key=8]` | 52/0 |
| 10 | **852/8 quick_auto** | `[]` | 701/0 |
| 11 | 852/1 | `[]` | 852/8 |
| 12 | 52/0 | `[]` | 852/1 |
| 13 | **701/0 get_state** | `[Int key=8]` | 52/0 |
| 14 | **852/8 quick_auto** | `[]` | 701/0 |
| 15 | 852/1 | `[]` | 852/8 |
| 16 | 52/0 | `[]` | 852/1 |

关键点：官方客户端在**每次 852/8 之前**都会先发一次 `701/0 get_state [Int 8]`，
把 PrevAct 链锚点推进到 701/0；两次快速挂机之间以及之后都重发 852/1 刷新面板。

### 服务端回包

- `852/9 get_auto_profit`（43 字节，未压缩）：
  ```
  00 | 00 01 54 41 | 00 03 |
  00 00 2a f1 00 00 00 00 00 02 5d 99
  00 00 2a f0 00 00 00 00 00 67 cb 54
  00 00 2a f2 00 00 00 00 00 19 f2 d5
  ```
  - `[UByte result=0][Int 87105][UInt16 count=3][Int item, Long num] × 3`
  - 奖励：`10993×155033`、`10992×6802260`、`10994×1700565`（资源类 ID，不在 item 静态表）。
- `852/8 quick_auto`（39 字节，两次回包完全相同）：
  ```
  00 | 00 03 |
  00 00 2a f0 00 00 00 00 00 04 59 c0
  00 00 2a f1 00 00 00 00 00 00 19 50
  00 00 2a f2 00 00 00 00 00 01 16 70
  ```
  - `[UByte result=0][UInt16 count=3][Int item, Long num] × 3`
  - 奖励：`10992×285120`、`10993×6480`、`10994×71280`。
  - 与 852/6 回包结构完全一致，可直接复用 `ParseEraRebirthFreeBackResult`。
- `701/0 get_state`（16 字节）：`[Int key=8][Int value=0]`，无结果码。
- `852/1 get_panel_info` 回包为整包 zlib（解压后 329 字节），无 UByte 结果码，
  首字段为 Int（本次为 4）；自动化只用于建立 PrevAct 上下文，不据此判定成功。

### 结论与实现

- `852/8`（`quick_auto`）是**免费快速挂机**：官方客户端每次产出一组挂机奖励，
  每日免费触发两次；付费档是 `852/5 quick_auto_back`（`[Int]`，历史实测 10 元宝），
  自动化永久禁用。
- `852/9`（`get_auto_profit`）回包带奖励列表，按「渡业台领取挂机收益」接入；
  无收益时返回 `NO_PROFIT(10)` / `NO_CAN_GET_GIFT(4)` 等，不应报错。
- 已落地：
  - `internal/protocol/era_rebirth_land.go`：新增 `ActionIDEraRebirthLandQuickAuto`、
    `BuildEraRebirthLandPanelPrevActPacket`、`BuildEraRebirthLandAutoProfitPrevActPacket`、
    `BuildEraRebirthLandQuickAutoPacket`、`BuildStateRecordGetStatePacket`、
    `ParseEraRebirthAutoProfit`、`ParseStateRecordGetStateResult`。
  - `internal/routines/daily/rebirth_land.go`：`runAltarProfitAndQuickAuto` /
    `runQuickAuto`，顺序对齐抓包（线上确认 701/0 锚点可省略）；`852/8` 只调用
    面板剩余免费次数，遇到 `NO_TIMES/NO_PROFIT/NO_FREE_STORAGE/NOT_ENOUGH_ITEM/ALREADY_GET`
    立即停止，`PACKAGE_FULL` 返回熔断，`NOT_ENOUGH_INGOT/NOT_ENOUGH_TODAY_INGOT`
    6 小时熔断；每次成功后刷新面板核对免费次数并比较元宝余额，异常即熔断。
- 单测：
  - `protocol/era_rebirth_land_test.go`：852/8 请求 4 字节 PrevAct、701/0 请求/回包、
    852/9 与 852/8 抓包回包、852/1 面板免费/付费次数逐字段解析。
  - `routines/daily/rebirth_land_test.go`：两次快速挂机成功、`NO_TIMES` 停止、
    `quick_free_used=2` 时零调用、剩余 1 次只补 1 次、852/9 `PACKAGE_FULL` 熔断。

### 2026-11-21 线上实测（configs/qq.yaml / s0-tongchen）

用 `test/probes/probe_rebirth_routine_live.go` 直接执行例程，实测结果：

1. **第 1 轮**（面板 `quick_free_used=0`）：`852/9` 领取成功 `profit=172800`；
   `852/8` 连续两次成功，各返回 3 条奖励；元宝无变化。
2. **第 2 轮**（新进程，重新登录）：`852/9` 成功；`852/8` 第 1 次成功，
   随后服务端推送 `元宝 before=100000 after=99990 delta=-10`。
   **结论：每日免费次数为 2，第 3 次 `852/8` 起仍返回 `result=0`，但会扣 10 元宝。**
   例程的元宝熔断在扣费后才发现，只能止损，不能防止扣费。
3. 只读探针 `probe_rebirth_panel_profit_live.go` 确认 `852/9` 本身免费
   （`ingot_delta=0`），扣费来自 `852/8`。

据此解析 852/1 面板（329 字节，两个账号结构一致）：

```
[Int level=4][UInt16 28][28 × (Int id, Int value)][Int × 12][String/String/...]
```

尾部 12 个 Int 的关键字段：

| 下标 | 含义 | 实测变化 |
| -: | --- | --- |
| 4 | 当前挂机累计秒数 | 87111 → 12 → 21 → 27（领取后清零再累计） |
| 5 | 挂机收益上限秒数 | 恒为 172800（48 小时） |
| 10 | 当日已用**免费**快速挂机次数 | 0 → 1 → 2 |
| 11 | 当日已用**付费**快速挂机次数 | 第 3 次后变为 1 |

因此最终实现改为：**先读 852/1 面板的 `QuickFreeUsed`，剩余次数为 0 时绝不调用 852/8**；
每次成功后刷新面板核对免费次数确实 +1，付费次数增加则立即熔断。面板解析失败时
放弃快速挂机（宁可少领，不可扣元宝）。

第 3 轮实测输出（当天免费次数已用尽）：

```
轮回净地面板 idle_seconds=277 idle_cap_seconds=172800
  quick_free_used=2 quick_free_remaining=0 quick_paid_used=1
轮回净地挂机收益领取成功 profit=272 rewards="10992 x 14760, 10994 x 3690, 10993 x 306"
轮回净地免费快速挂机次数已用尽，跳过以避免扣元宝
轮回净地例程结束 coin_delta=0 ingot_delta=0
```
