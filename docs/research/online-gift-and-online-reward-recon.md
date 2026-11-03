# 在线礼包与在线奖励协议逆向报告 (Online Gift & Online Reward Recon)

- **日期**: 2026-11-03
- **关联 Issue**: [#3](https://github.com/niuchangsheng/sxd/issues/3) / [#4](https://github.com/niuchangsheng/sxd/issues/4)
- **基准目标**: 官方微端加载的 `Main.swf`（`cur_ver=2026102301`，CDN 目录哈希 `4f160f61e0968bd8568bc9cc38bbea6224ed274e`）

---

## 一、官方客户端获取路径

微端换票返回的 `Ticket.Extra` 包含 CDN 索引参数：

```text
client_url = http://uri.xdcdn.net/sxd/
cur_ver    = 2026102301
index_url  = http://uri.xdcdn.net/sxd/index/
diff_url   = http://uri.xdcdn.net/sxd/diff/
diff_urls[2026102301] = r393-10
```

按 `com.assists.Verison` / `WDURI` 的解析规则：

1. 拉取 `index/<cur_ver>` 与 `diff/<cur_ver>..<cur_ver>-<diff_urls[cur_ver]>`；
2. 每条记录 25 字节：前 5 字节为目录路径 SHA1 前十位，后 20 字节为 CDN 目录哈希；
3. `game` 目录哈希为 `4f160f61e0968bd8568bc9cc38bbea6224ed274e`；
4. 游戏主 SWF：`http://uri.xdcdn.net/sxd/tree/<hash>/Main.swf`。

`test/probes/probe_ticket_extra.go` 可复现上述 `Ticket.Extra` 字段。

---

## 二、`Mod_Player_Base` 在线礼包 (滚动在线时长福利)

### 2.1 协议

| 动作 | Module | Action | request | response |
| --- | ---: | ---: | --- | --- |
| `player_get_online_gift` | 0 | 30 | `[]` | `[UByte, [Int, Int, Int], Int]` |
| `get_player_current_online_gift` | 0 | 31 | `[]` | `[Int, [Int, Int, Int]]` |

- 领取回包：`result`、`online_gift_list`（`type/item_id/value`）、`next_time_stamp`（剩余秒数，0=无后续）。
- 查询回包：`time_stamp`（0=无待领取预览）、下一份礼包列表。
- 奖励类型 `type`：0 铜钱 / 1 元宝 / 2 体力 / 3 阅历 / 4 经验 / 5 物品。

### 2.2 结果码（此前实现错误，已修正）

`com.protocols.Mod_Player_Base` 的真实常量：

| 常量 | 值 |
| --- | ---: |
| `ACTION_SUCCESS` | 97 |
| `ALREADY_GET` | 98 |
| `ACTION_FAILED` | 100 |
| `PACK_FULL` | 101 |

> 旧实现使用的 `84/87/88` 实际是属性 ID（`PLAYER_DAY_QUEST` / `NOT_ENOUGH_INGOT` / `FULL_BUY_TIMES`），不是领取结果码。

### 2.3 客户端状态机 (`ActivitiesView.onlineGiftActivity`)

状态来自 `update_player_data`：

| 属性 ID | 常量 | 语义 |
| ---: | --- | --- |
| 81 | `ONLINE_GIFT` | 距下一次可领取的剩余秒数，客户端换算为 `now + value` |
| 82 | `GET_ONLINE_GIFT` | `-1` 无礼包 / `0` 倒计时中 / `1` 可领取 |

- `82 == -1`：`removeActivity(TownType.OnlineGift)`，不查询、不领取；
- `82 == 0`：展示倒计时，倒计时结束后才可领取；
- `82 == 1`：展示「立即领取」；
- `100 ACTION_FAILED`：提示「时间未到」；`101 PACK_FULL`：提示背包不足。

### 2.4 实机验证 (s1-niuchangsheng, 2026-11-03)

登录 `update_player_data` 原始包：

```text
0001510000000000000000  -> prop 81 = 0
000152ffffffffffffffff  -> prop 82 = -1
```

因此该角色当前无可领取的在线礼包。修复前 routine 无视该状态盲领，服务端稳定返回：

```text
64 0000 00000000  -> result=100 (ACTION_FAILED), 空列表, next_time_stamp=0
```

修复后 routine 直接跳过，不再发出无效请求。

复现探针：`test/probes/probe_online_gift_state.go`。

---

## 三、`Mod_OnlineReward` 在线奖励 (活动型，Module 712)

### 3.1 协议

| 动作 | Module | Action | request | response |
| --- | ---: | ---: | --- | --- |
| `main_panel` | 712 | 0 | `[]` | `[Int, Int, Byte, [Int, [Int, Long, Byte], Byte, Int], Int]` |
| `one_key_get` | 712 | 1 | `[]` | `[UByte, [Int]]` |

`main_panel` 字段：`start_time`、`end_time`、`day`、`award_list`、`online_time`。

每条 `award_list`：`award_id`、`award_item_list`（`item_id`、`item_num`、`is_big_award`）、`is_get`、`need_online_time`。

结果码：`0 SUCCESS` / `1 FAILED` / `2 ITEM_PACK_FULL`。

### 3.2 实机样本

请求 `0x02C80000`（s1，2026-11-03）：

```text
6ad8e180 6adcd5ff 03 0007 ...
```

解析结果：

- 活动窗口：2026-10-22 00:00 ~ 2026-10-24 23:59:59，`day=3`；
- 7 档奖励，`need_online_time` = 300/600/900/1200/1500/2100/2700 秒；
- 全部 `is_get=1`，`online_time=1072`。

该活动在样本日期已结束；协议接入见 Issue #4。

复现命令：

```bash
go run -tags=probe test/probes/probe_module_call_live.go 02C80000 02C80000
```

---

## 四、相关代码

- `internal/protocol/constants.go`：结果码 `97/98/100/101`、属性 ID `81/82`
- `internal/client/session.go`：`PlayerState.OnlineGiftTime` / `IsGetOnlineGift`
- `internal/routines/daily/daily.go`：`online_gift` 状态机
- `internal/client/player_update_test.go`：属性 81/82 同步单测
- `test/probes/probe_online_gift_state.go`、`probe_online_gift_prevact.go`、`probe_ticket_extra.go`