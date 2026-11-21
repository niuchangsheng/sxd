# 新版客户端法相页面抓包侦察（Module 518，2026-11-13）

## 来源

- 抓包：`/tmp/faxiang.pcapng`，游戏连接 `10.0.0.13:61974 → 81.70.56.121:8200`
- 有效窗口：23:03:39–23:04:07（约 28 秒，连接在抓包开始前已建立）
- 提取：`tcpdump -nn -r /tmp/faxiang.pcapng 'tcp port 8200' -w /tmp/faxiang8200.pcap`

## 页面打开动作序列

客户端（PrevAct 链）：

```
518/11 (空)                 PrevAct=46/0
518/17 (空)                 PrevAct=518/11
518/13 (空)                 PrevAct=518/17
518/15 [Int 14]             PrevAct=518/13
518/12 (空)                 PrevAct=518/15
518/16 [Int 0x00020001]     PrevAct=518/12
```

服务端：

```
518/11  [2, 996, 0, 时间戳]
518/17  result=0
518/13  [3, 6, 9, 14]              # 4 个分类/层级
518/15  [0, 时间戳]
518/12  [2, 61, 时间戳, 476, 97, 42]
518/16  90563 字节（zlib 整包压缩）
```

`518/16` 解压后是法相/伙伴数据，明文可见：
`同尘`、`魔玲珑`、`武圣女`、`圣夸父`、`魔张道陵`、`神不死凤凰`、`圣精卫`，
以及 `LingLongPiaoYi`（玲珑飘衣）等法相英文名，夹杂大量属性二进制字段。

## 关键结论

1. **新版客户端的法相页面走 Module 518**，不在 `docs/protocol/actions.json`
   （来源 Main.swf）的模块表中——518 正好是字典缺口。
2. 本次抓包**没有任何 721（Mod_ExtremeTrial_Base）报文**；现有 `faxiang_trial`
   例程基于旧版 721 试炼协议，可能已不对应新客户端页面。
3. 抓包在 518/16 大包下载完成后 1 秒即结束，**未包含「造物佛手 / 守护魔兽」按钮点击**。

## 下一步（需要补抓）

1. 先启动 tcpdump `tcp port 8200`，再打开法相页面并等待加载完成；
2. 依次点击「造物佛手」的打开/领取/挑战、再点「守护魔兽」对应按钮，每个动作间隔 3 秒；
3. 预期可看到 518/18 及之后的动作，或 721/x 的真实调用。

拿到按钮报文后再决定：
- 若按钮走 518/x，新增 `Mod_FaXiang`（暂名）协议与例程；
- 若按钮仍走 721，则修正面板入口（先按 518 打开页面再调 721）。
## 第二次抓包（/tmp/faxiang2.pcapng，2026-11-14 00:08:51–00:09:33）

用户只点开了「佛」侧，未点「魔」。游戏流量仍只有 8200 一条连接。

### 佛侧相关

```
C>S 518/0  空载荷 + PrevAct          （本次新增，首次抓包没有）
S>C 518/0  194 字节（旧时间戳 2021-12/2022-01 与若干数值，疑似功能/法相状态列表）

C>S 518/11 → 518/17 → 518/13 → 518/15[Int 14]
S>C 518/11 [3,1428,0,时间]；518/17 result=0；518/13 [3,6,14,5]；518/15 [0,时间]
```

两次页面打开都重复 11→17→13→15，未出现 518/12、518/16，也未出现任何 518/18+，
说明本次点「佛」**没有产生领取/挑战类动作**，518/0 只是状态查询。

### 同批抓到的“领取在线礼包”（与本 Issue 无关，已实现）

```
C>S 127/1 功能终结礼包列表
S>C 127/1 6 个礼包: gift_id=4(bei=4),17(2),16(2),7(2),13(10),1(2)
C>S 127/3 [Short gift_id] → 127/2 [Short gift_id] 逐个领取
C>S 2/29 [Int mail_id] ×10（055ddc20..055df33e）领取邮件/序列奖励
```

这两条链路分别由 `online_gift`（Module 127）与 `mail_collect`/序列奖励逻辑覆盖，
不属于法相协议。

### 结论

- 「佛」按钮目前只观测到 518/0 查询 + 518/11-15 页面加载；
- 若要实现“造物佛手”的领取/挑战，需要在点击后真正触发奖励或战斗的那一步补抓；
- 518 仍不在 Main.swf 协议字典中，响应结构待更多动作样本才能解析。

## 第三次抓包（/tmp/faxiang3.pcapng，2026-11-21 23:10–23:11）

本次抓包终于覆盖了「造物佛手」与「守护魔兽」的按钮点击。游戏连接仍是
`10.0.0.13:61029 → 81.70.56.121:8200`；抓包期间客户端还从 `uri.xdcdn.net`
下载了 `mrfx_extra_fo.swf` / `mrfx_extra_mo.swf` 两个法相子面板资源。

### 客户端动作序列（C2S，按时间顺序）

```
518/11  [空 + PrevAct]                  造物佛手面板 extra_fo_info
518/17  [空 + PrevAct]                  领取炼化奖励 gain_pray_award
518/13  [空 + PrevAct]                  祈愿，随机 3 个炼化奖励 extra_pray
518/15  [Int 14][PrevAct]               选择奖励 14 并开始炼化 extra_choose_pray_award
518/12  [空 + PrevAct]                  守护魔兽面板 extra_mo_info
...（页面内其它模块 5/5、46/0、312/38、760/17、46/3，与本玩法无关）...
518/16  [空 + PrevAct]                  守护魔兽挑战 extra_mo_challenge
```

### 服务端回包（已解压，已剥离 4 字节 ActionID）

```
518/11  00000003 00000644 0000 0000000e 6b000463
        level=3, exp=1604, award_list=[], cur_award_id=14, refine_finish_time=0x6b000463
518/17  00                                  result=SUCCESS，领取成功
518/13  00 0003 00000005 0000000f 0000000e  result=0, 3 个候选奖励 [5,15,14]
518/15  00 6b02b4ef                         result=0, 新炼化完成时间=0x6b02b4ef
518/12  00000001 00000000 00000000 0000011a 00000061 0000001f
        level=1, exp=0, next_can_fight_time=0, stunt=282, supernatural=97, pattern=31
518/16  00 0000001c 0001 03...              result=0, kill_num=28, 后接完整战报
```

挑战后再次请求 518/12：

```
00000001 0000001c 6b06fa7b 0000011a 000000b4 0000002a
level=1, exp=28（与 kill_num 一致）, next_can_fight_time=0x6b06fa7b,
stunt=282, supernatural=180, pattern=42
```

### 结论（Mod_MainRoleFaXiang_Base, Module 518）

`Mod.as` 中 `"518":Mod_MainRoleFaXiang_Base`，协议类完整定义可直接从
`~/rea-sxd/swf/Main.swf` 导出（`com.protocols.Mod_MainRoleFaXiang_Base`）：

| Act | 名称 | 请求 | 回包 |
| --- | --- | --- | --- |
| 0 | get_panel_info | `[]` | `[Int×6,[Int×5],Int,Int]` |
| 1 | refine_book | `[Int]` | `[UByte,Int]` |
| 2 | switch_fa_xiang | `[Int]` | `[UByte]` |
| 3 | fight | `[Int]` | `[UByte, 战报]` |
| 4 | fa_xiang_palace_info | `[Int]` | `[Int,Int,[Int,Int,Byte]]` |
| 5 | move | `[Int,Int]` | `[UByte,Int]` |
| 6 | fight_moster | `[Int,Int]` | `[UByte, 战报]` |
| 7 | use_buff_item | `[Int,Int]` | `[UByte]` |
| 8 | build_statue | `[Int]` | `[UByte]` |
| 9 | statue_challenge | `[Int]` | `[UByte, 战报]` |
| 10 | notify_change_fa_xiang_type | `[]` | `[Int]` |
| **11** | **extra_fo_info** | `[]` | `[Int level,Int exp,[Int award_id],Int cur_award,Int finish_time]` |
| **12** | **extra_mo_info** | `[]` | `[Int level,Int exp,Int next_fight,Int stunt,Int supernatural,Int pattern]` |
| **13** | **extra_pray** | `[]` | `[UByte result,[Int award_id]]` |
| 14 | extra_refresh_pray | `[]` | `[UByte result,[Int award_id]]`；消耗元宝，自动化禁用 |
| **15** | **extra_choose_pray_award** | `[Int award_id]` | `[UByte result,Int finish_time]` |
| **16** | **extra_mo_challenge** | `[]` | `[UByte result,Int kill_num, 战报]` |
| **17** | **gain_pray_award** | `[]` | `[UByte result]` |

结果码：

```
0 SUCCESS             2 FUNCTION_NO_OPEN    5 REFINING          22 HAS_CHALLENGE
24 CAN_NOT_PRAY       25 NOT_ENOUGH_INGOT   26 ERROR_PRAY_AWARD_ID
27 NOT_ENOUGH_DAO_YUAN 28 TIME_LIMIT         29 NOT_PRAY_AWARD   30 PACKAGE_FULL
```

### 玩法语义

1. **造物佛手（佛侧）**：
   - `518/11` 返回熟练度等级/经验、待选奖励、正在炼化的奖励与完成时间。
   - 有 `cur_award_id` 时点击「领取」= `518/17`；若炼化未完成，服务端返回
     `NOT_PRAY_AWARD(29)`，客户端不能凭本地时间戳猜测。
   - 无待选奖励时点击「祈愿」= `518/13`，服务端随机返回 3 个 `award_id`。
   - 选择其中一个 = `518/15 [Int award_id]`，开始炼化并返回新的完成时间。
   - 炼化奖励表 `MainRoleFaXiangType.extraPrayAwardList` 中每条奖励都带
     `cost_dao_yuan`（100–6000）与 `cost_time`，即「选择并炼化」会消耗**道缘**。
   - `518/14` 是花**元宝**重随 3 个候选奖励，自动化绝不调用。
2. **守护魔兽（魔侧）**：
   - `518/12` 返回等级/经验、下次可挑战时间、魔兽绝技/神通/阵法。
   - 点击挑战 = `518/16`，回包 `[UByte result][Int kill_num][战报]`；
     `kill_num` 与挑战后 `518/12` 的熟练度增量一致（本次 28）。
   - 冷却中返回 `HAS_CHALLENGE(22)`，不购买次数、不清除冷却。

### 与旧实现的关系

`internal/routines/minigame/faxiang_trial.go` 旧版基于 `Mod_ExtremeTrial (721)`
猜测「造物佛手/守护魔兽」，但 721 是极限试炼，抓包中从未出现。现已改为
Module 518：`internal/protocol/faxiang.go` + `internal/routines/minigame/faxiang.go`。

## 实机验证（configs/qq.yaml，2026-11-21 19:40–19:45，角色「同尘」）

### 518/0 主面板结构（194 字节）

```
00000001 00000002 00000001 00000001 00000000 00000000
0008
  8 × [Int task_id][Int progress][Int 1][Int 1][Int timestamp]
00000009 00000023
```

字段与 `MainRoleFaXiangInfo` 声明顺序一致：

- `function_open_flag=1`（>0 已开放）
- `main_role_fa_xiang_type=2`（当前为魔侧）
- `fight_fo_mission_flag=1`、`fight_mo_mission_flag=1`（佛/魔两侧均可进入）
- `fo_attr_add=0`、`mo_attr_add=0`
- 8 条法相任务；`seLiZiNum=9`、`hmzqNum=35`

### 造物佛手闭环

只读时：`level=2 exp=1084 cur_award=14 finish=1794753302`。

`SXD_PROBE_APPLY=1` 执行：

```
518/17 → result=0（领取 award 14）
518/13 → result=0, award_ids=[9,5,8]
518/15 [9] → result=0, finish=1795432319
```

领取后 `exp=1172`，增量 **+88**，与 `extraPrayAwardList` 中 id=14 的
`add_exp=88` 完全一致。选择 id=9（cost_time=190080s，熟练度 Lv2 减 10%）
后完成时间约 +171000s（约 2 天），证明 `cost_time` 与等级缩短率真实生效。

### 守护魔兽闭环

```
518/12 挑战前: level=2 exp=74 next=1795014243 stunt=426 supernatural=157 pattern=31
518/16 → result=0 kill_num=20, 战报 115637 字节
518/12 挑战后: level=2 exp=94 next=1795693248
```

`exp` 增量 **+20** 与 `kill_num=20` 一致；下次挑战时间被推到约 5 天后。

### Routine 实测

`probe_faxiang_routine_live.go` 直接执行正式例程：

```
518/0  主面板加载 (194B)
518/11 level=2 exp=1172 cur_award=9 finish=1795432319
518/17 result=29 NOT_PRAY_AWARD      → 返回冷却 2026-11-23 19:12:59
518/12 next_can_fight_time=1795693248 → 跳过 518/16 挑战请求
元宝 before=99990 after=99990 delta=0
```

炼化未完成和守护魔兽冷却均按服务端语义分流；道缘炼化上限与元宝零消耗均符合预期。
