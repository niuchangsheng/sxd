# s0 每日活跃度 1200 达标审计与未实现功能清单

- 审计日期: 2026-11-06
- 账号/区服: s0 (角色 遁一, Lv.300)
- 目标: `s0.yaml` 执行后每日活跃度 ≥ 1200
- 实测结果: **1280 / 1200 达标**（含全量例程实跑追加的 80 分）
- 元宝约束：达标路径本身 `ingot_delta = 0`；但全量例程实跑暴露并修复了一个
  既有元宝泄漏缺陷（见 5.1），验证运行期间因此产生 600 元宝消耗

## 1. 活跃度测量方法

官方协议 `Mod_Player_Base.get_game_assistant_info`（Module 0, Action 42 → `0x0000002A`）的
`active_degree_list` 每行为 `[task_id, cur_times, max_times, per_active_add]`，
`task_id <= 83` 为每日任务。每日活跃度 = Σ(cur_times × per_active_add)。

协议描述符已固化为 `internal/protocol/daily_activity.go`：

```
"response":[Int,Long,Int,Long,Int x29,
            [Int,Int,Int,Int],
            [Int,Int,Int,Int]]
```

前 33 个标量中下标 1、3 为 Long，其后依次为 `back_times_list` 与 `active_degree_list`。

## 2. 当日达标路径

| 阶段 | 动作 | 协议 | 活跃度 |
| --- | --- | --- | ---: |
| 起始 | 既有日常（登录/灵脉/上供/兵法/多宝山等） | — | 1150 |
| 1 | 免费猎命：依次尝试猎命师，npc_id=1 成功 | Mod 22 Act 0/9 | +5 |
| 2 | 铜钱购买命宫碎片 1 次（铜钱 -3,000,000） | Mod 22 Act 31/34 | +5 |
| 3 | 悟法领域冥想 4 次（消耗冥想书卷 8641 ×4） | Mod 658 Act 0/1 | +40 |
| 合计 | | | **1200** |

关键实测结论：

- **猎命**：`get_fate_npc` 必须携带 PrevAct；`appoint_fate_npc` 仅接受 1 字节 `npc_id`。
  猎命师列表中只有 `status=1`（已激活/已解锁）的编号可指定，其余返回
  `INVALID_FATE_NPC_ID(12)`。响应字段为 `[UByte result, Short fate_id, Byte npc_id, Long temp_fate_id]`。
- **命宫碎片**：`get_ming_gong_info` 返回 `[命宫等级 10, 碎片 2628, 剩余兑换次数 500]`；
  `exchange_ming_gong_scrap(1)` 返回 `[0, 2629]`，只扣铜钱。
- **悟法领域**：免费感悟次数用尽（各秘馆 `left_free_one/ten = 0`）后，仍可用冥想道具抽取。
  面板资源为 8358 冥想竹简×29、8359 冥想玉简×0、8641 冥想书卷×12、8700 冥想玄简×10。
  实测 `field_type = 3` 消耗 **8641 冥想书卷**（12→8），每次 +10 活跃度且元宝不变；
  `field_type = 2` 对应数量为 0 的冥想玉简，自动化必须回避。
- **每日阶位赛**（Mod 349）：`join_pk` 返回 SUCCESS(3) 后服务端 2 秒内推送完整战报，
  首字节 `9 = LOSE`。加入本身免费、不消耗元宝，但胜负不可控。

## 3. 当前剩余活跃度来源与可行性

以下为 2026-11-06 20:58 全量快照中尚未完成的每日任务（`remain_pt` 为剩余可得分）。

### 3.1 已实现并可持续获取（本轮已固化为例程）

| task | 任务 | 每次 | 剩余次数 | 剩余分 | 实现 |
| ---: | --- | ---: | ---: | ---: | --- |
| 4 | 免费猎命 | 5 | 1 | 5 | 新增 `fate_hunt` 例程 |
| 56 | 购买命宫碎片 | 5 | 1 | 5 | 新增 `fate_hunt` 例程（仅铜钱） |
| 69 | 悟法领域冥想 | 10 | 19 | 190 | `wufa_field` 例程扩展：免费次数耗尽后用冥想书卷 |

### 3.2 未实现 / 当前不可达

| task | 任务 | 每次 | 剩余分 | 状态与结论 |
| ---: | --- | ---: | ---: | --- |
| 38 | 每日神龙高级上供 | 5 | 3000 | 仅元宝，按策略永久排除 |
| 5 | 召唤张道陵 | 10 | 300 | 元宝档，排除 |
| 53 | 聚宝盆 | 5 | 245 | 官方说明「花费元宝获得铜钱」，排除 |
| 1 | 购买体力 | 5 | 230 | 元宝，排除 |
| 40 | 异兽购买抓捕次数 | 5 | 200 | 元宝，排除 |
| 45 | 宠物房高级召唤 | 5 | 100 | 元宝，排除 |
| 15 | 购买仙露 | 5 | 90 | 元宝购买档，排除 |
| 79 | 每日服用修为丹 | 10 | 150 | 背包内一~四阶修为丹均为 0，无来源 |
| 80 | 诸法器冢寻器/招灵 | 10 | 100 | 功能 281 未开放（check_function_open=FALSE） |
| 72 | 阶位赛胜利 | 10 | 100 | 参赛成功但首战 LOSE；需提升胜率或增加挑战轮次 |
| 68 | 多宝山寻宝 | 10 | 100 | `dig_num=0`，免费挖掘次数已耗尽，例程无可执行动作 |
| 46 | 五福临门 | 5 | 50 | 现有例程按 Mod 68 判定「活动未开启」；正确玩法为 Mod 400 (Mod_FiveFu) |
| 42 | 五行天仪转十次 | 5 | 50 | 面板 free=0/coin=0，仅剩元宝档，排除 |
| 47 | 结缘宝箱 | 5 | 50 | 背包结缘宝箱数量 0，宝箱来源未接 |
| 74 | 琳琅阁购买商品 | 10 | 30 | Mod 2 Act 76 / Mod 100 Act 80 在机器人会话下全部超时（功能 42 已开放，疑似需城镇 NPC 场景） |
| 41 | 五行天仪转一次 | 5 | 30 | 同 42，剩余次数仅元宝档 |
| 36 | 取经打劫 | 5 | 25 | 圣域取经打劫已实现，但战斗失败无收益 |
| 70 | 轮回净地挂机奖励 | 20 | 20 | 无协议/例程（纪元归墟体系） |
| 71 | 纪元湮劫战斗 | 20 | 20 | 无协议/例程（纪元归墟体系） |
| 77 | 宗门灵兽喂养 | 20 | 20 | 无协议/例程（宗门仙境 Module 522 体系） |
| 82 | 领取好礼馈赠 | 20 | 20 | 琳琅阁同源入口，当前超时 |
| 73 | 进入仙幽猎境 | 20 | 20 | 功能 272 未开放；Module 762 请求超时 |
| 55 | 开启魂缘宝箱 | 5 | 15 | 今日 box_type=6 次数用尽、box_type=5 次数 0、box_type=7 仅元宝 |
| 51 | 抢夺仙界矿山 | 5 | 15 | `crystal_mine` 仅进入/领奖，抢占需战斗；进入返回 FAILED(13) |
| 83 | 劫镖 | 5 | 15 | `caravan_hijack` 未解析结算结果码，战斗败北时活跃度不变 |
| 50 | 奇门遁甲 | 5 | 5 | 例程请求超时；功能 152 已开放 |
| 58 | 家园奖励 | 5 | 5 | 无例程（MarryHome Module 210） |
| 49 | 仙盟上香 | 5 | 5 | 无例程（StUnion Module 178） |
| 14 | 使用仙露 | 5 | 5 | 发财树种植流程未实现（farm 模块的摇钱树播种） |
| 67 | 回忆之境 | 5 | 5 | 无例程（MemoryZone Module 526） |
| 64 | 召唤仙桃 | 5 | 5 | 首次成功，后续 `call_peach` 固定返回 FAILURE(3) |
| 63/62 | 孩子任务/孩子修行 | 5 | 10 | 无例程（Children Module 312） |
| 59/60 | 孩子学习/孩子探索 | 5 | 10 | 无例程（Children Module 312） |

> 元宝档合计约 4075 分已按用户硬性约束永久排除；其余未实现项目合计约 600+ 分，
> 其中真正「免费可达」的主要是 46、74、36、70、71、77、82、73、51、83 及零散 5 分任务。

## 3.3 非元宝功能续实现（2026-11-06 晚）

| 功能 | 结论 |
| --- | --- |
| 孩子日常（任务 59/63） | 已实现 `children_daily` 例程：学堂上学（UNDO 状态）+ 孩子任务奖励领取。线上实测孩子 1812、3 个任务均 `NOT_COMPLETE`、3 个学堂均 `DONE`，当日无可领取项；任务/学堂状态可正常解析 |
| 奇门遁甲修炼（任务 50） | 面板 6 个条目（587/371/2420/73/4059/784）以 `NORMAL_PRACTICE(3)` 调用均返回 `LEVEL_UNOPEN(8)`，id 映射未确认，暂不实现 |
| 五福临门（任务 46） | 当日已被外部操作完成 10/10；`is_five_blessings_open` 在 22:50 仍返回关闭，说明活动按时段开放，例程无需改动 |
| 回忆之境（任务 67） | 主面板可读（3 个区域），`backtrack` 需要具体回忆角色 id，暂未确认触发条件 |
| 仙盟上香（任务 49） | Module 178/179 请求全部超时，角色可能未加入仙盟 |
| **轮回净地（任务 70）** | **已实现 `rebirth_land`**：`one_click_quick_free_back (852/6)` 免费领取，线上实测 +20、`ingot_delta=0`；但任务 70 未计分（可能绑定 `quick_auto_back`），待对齐 |
| **兵法（任务 54）** | **已实现 `tactics_water`**：任务描述为「每次购买圣水 +5」。`buy_item (299/2, type=0 湛蓝圣水)` 用铜钱（约 200 万/滴），线上实测 25 次 +125、`ingot_delta=0`；黄金圣水 (type=1) 为元宝档，永不调用 |
| 纪元湮劫（任务 71） | Module 855 仅 `get_mission_list`；战斗在 853 (无尽洞天) 的 `fight` 流程，需要章节/队伍上下文，暂未实现 |
| 劫镖（任务 83） | 已修正协议语义（Act 2=rob、Act 12=open_panel）并解析结果码；`search` 重搜可能消耗 30 元宝，自动化只在已存在目标时劫镖，否则跳过 |

### 3.3.1 服务端会话链约束（实测）

1. 同一会话调用 **活跃度面板 `0x0000002A`** 后，后续 Module 312/315 子面板请求会被静默丢弃；
   `check_function_open (0x00000045)` 不会造成该问题。
   → `children_daily` 因此设为 `PriorityHigh` 之上（120），确保排在所有读取活跃度的例程之前。
2. 同一会话先调用 **孩子任务 Module 315**，后续 Module 312 学堂请求也会被丢弃；
   故例程顺序固定为「学堂 → 任务」。

## 4. 本轮代码变更

- `internal/protocol/daily_activity.go`：活跃度面板解析与任务剩余次数计算。
- `internal/protocol/fate_hunt.go`：猎命（Mod 22 Act 0/9）与命宫碎片（Act 31/34）编解码。
- `internal/routines/daily/fate_hunt.go`：新增 `fate_hunt` 例程（免费猎命 + 铜钱兑换命宫碎片）。
- `internal/routines/minigame/minigame.go`：`wufa_field` 例程改为读取活跃度任务 69 的剩余次数，
  免费次数耗尽后使用冥想书卷（8641 / field_type 3）补足，单轮上限 10 次。
- `internal/routines/registry.go`、`configs/s0.yaml`、`configs/s1.yaml`、`configs/config.example.yaml`：
  注册并默认启用 `fate_hunt`。
- `internal/routines/minigame/minigame.go`：修复禁地之书元宝泄漏（免费探索加计费门禁）。
- `internal/routines/pvp/holy_arena.go`：圣域竞技场改走圣域独立长连接（状态校验 + SA 会话），
  新增单测覆盖 SA 不可用时跳过。
- `fate_hunt` 与 `wufa_field` 提升为 `PriorityHigh`，确保活跃度补分动作在每轮调度中尽早执行，
  不被长任务队列挤到末尾。
- 新增 `internal/protocol/children.go`、`internal/routines/daily/children_daily.go`：
  孩子学堂上学与孩子任务奖励领取，优先级 120（必须早于活跃度读取）；注册并写入配置示例。
- 新增 `internal/protocol/era_rebirth_land.go`、`internal/routines/daily/rebirth_land.go`：
  轮回净地免费一键领取挂机奖励（+20/日），注册并写入配置。
- 修正劫镖协议语义与结果码解析（`feature_packets.go` / `pvp.go`），并加单测。
- 新增 `internal/protocol/tactics.go`、`internal/routines/daily/tactics_water.go`：
  铜钱购买湛蓝圣水完成兵法活跃度（+125/日），注册并写入配置。
- `wufa_field` 扩展冥想道具映射：field 1↔冥想竹简 8358、field 7↔冥想玄简 8700
  （此前只用了 field 3↔冥想书卷 8641），单轮上限提升到 20 次；线上实测 +170。
- 新增 `partner_link_box` 例程与缘魂宝箱协议（290/2、290/3）：
  道缘/铜钱购买宝箱（+15/日），元宝档永不购买。

## 6. 新的一天（2026-11-07）实测备注

- 活跃度在 00:00 重置；完整跑完一轮 `s0.yaml` 后为 610。
- 当晚补分：兵法 +125、悟法冥想道具 +170、缘魂宝箱 +15、召唤仙桃 +5 → **945**。
- 当日剩余主要缺口均受资源/场景限制：神龙上供 50（祭坛临时格被 9 颗失效龙珠占满，收取返回
  `INVALID_DRAGONBALL(2)`）、多宝山 100（`dig_num=0`）、五行天仪 50（免费/铜钱次数为 0）、
  五福临门 50（活动关闭）、阶位赛 100（战斗胜负）、琳琅阁 30/好礼 20/宗门 20/纪元 20（入口未打通）。
- 兵法任务描述经官方数据确认：「兵法中每次购买圣水获得活跃度」，不是浇灌。
- 单元测试：`daily_activity_test.go`、`fate_hunt_test.go`、`fate_hunt_test.go`(例程)、
  `forbidden_book_test.go`、`minigame_test.go`。

## 5. 已知缺陷

### 5.1 禁地之书元宝泄漏（已修复）

2026-11-06 21:34 的全量例程实跑（`configs/s0.yaml`）发现元宝从 72952 降到 72352。
定位结果：`NewForbiddenBookRoutine` 的"免费次数优先"循环对面板中 **所有**
`cur_number > 0` 的区域直接调用 explore，而线上面板同时下发两种入口：

| forbidden_type | explore_type | cost_ingot | cur_number | 含义 |
| ---: | ---: | ---: | ---: | --- |
| 20 | 22 (多次) | **300** | 4 | 元宝计费入口 |
| 19 | 22 (多次) | **200** | 0 | 元宝计费入口 |
| 18 | 22 (多次) | **100** | 0 | 元宝计费入口 |
| 20/19/18 | 21 (单次) | 0 | 0 | 免费入口（次日恢复） |

旧逻辑在"免费"名义下探索了 type=20 的多次入口 2 次，每次 300 元宝，合计 600。
修复：免费探索循环增加 `cost_ingot == 0 && cost_coin == 0` 门禁，并新增单测
`TestForbiddenBookRoutine_SkipsPaidExploreAreas`。修复后线上复跑该例程
`ingot_delta = 0`，日志显示 3 个元宝入口全部被跳过。

> 该缺陷属于既有代码，非本次新增；建议后续对所有"面板下发剩余次数"的例程统一
> 增加计费字段门禁。

### 5.2 其余缺陷（不直接影响每日活跃度）

- **圣域竞技场（已修复路由）**：`holy_arena` 原使用主服会话调用 `Mod_SaArena_Base`，
  线上主服 `get_info` 返回 5 个全空席位（假数据），抢占固定返回 `ACTIVITY_CLOSE(3)`。
  官方 `SaArenaThroneView` 将 `get_info`/`rob_the_seat` 路由到 `DataBase.SOCKETSAINTAREA`。
  现已改为先查圣域状态、再经 `client.OpenSaintAreaSession` 的独立连接调用；
  线上实测圣域连接能看到真实席位（玩家/战力），但当前不在活动开放时段，
  `rob_the_seat` 仍按预期返回 `ACTIVITY_CLOSE(3)`。活动开放后即可正常抢占。
- **劫镖**：`NewCaravanHijackRoutine` 对 `hijack`/`settle` 使用 `_, _ = session.Call(...)`，
  违反「回包结果码必须解析」铁律，需补齐结算结果判定。
- **五福临门**：例程检查的是 Mod 68（喜从天降）的五福面板，而每日任务 46 属于
  Mod 400（Mod_FiveFu_Base），两者不是同一玩法。