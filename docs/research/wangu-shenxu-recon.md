# /tmp/wangushenxu.pcapng 抓包侦察：万古神墟（Module 840）

日期：2026-11-21 23:48（抓包时间窗约 36 秒）
来源：`/tmp/wangushenxu.pcapng`，客户端 `10.0.0.13`，角色「司空测试号」(s0, player_id=259239)

## 一、模块定位结论

| 模块 | 反编译类 | 游戏内含义 | 关键动作 |
| :--- | :--- | :--- | :--- |
| **840** | `Mod_EternalGamesPresent_Base` | **万古神墟**（`TownType.WanGuShenXu`） | 0=main_info、1=worship、2=get_award、3~5=排行榜 |
| 419 | `Mod_SaUnionWorship_Base` | 圣盟膜拜（**不是**万古神墟） | 1=open_panel、2=worship、3=get_record、4/5=投票 |
| 95 | `Mod_StTown_Base` | 跨服城镇（万古神墟城镇 id=102） | 0=enter_town、4/5=移动、6=get_players |

> 纠正：仓库此前把 419 命名为「万古神墟/圣盟膜拜」，并把 646 疑为万古神墟。抓包与
> `EternalGamesPresentMapView.gotoTown()` 中 `TownType.WanGuShenXu` + 840 协议共同确认：
> **万古神墟 = Module 840**；419 仅为圣盟膜拜。

## 二、连接拓扑

抓包中与游戏相关的连接只有以下几条（其余为 61.170.82.99:80 的 SWF/PNG 资源下载）：

| 连接 | 用途 | 本次抓到的内容 |
| :--- | :--- | :--- |
| `10.0.0.13:56262 -> 81.70.56.121:8200` | **主服会话** | 心跳 0/22、红点 558/3、**840/0、840/1**、0/3 元宝推送 |
| `10.0.0.13:62057 -> 81.70.56.121:8219` | 跨服会话 | 95/0 enter_town(id=102 万古神墟)、95/4/5/6 |
| `81.70.56.121:8205 / 8251` | 跨服辅助连接 | 仅 0/75 心跳 |

840 的面板与膜拜请求全部走在**主服会话 8200** 上；跨服会话只负责进入万古神墟城镇
（id=102）。因此自动化直接在主 `RoleSession` 上调用 840 即可，不需要先切城。

## 三、物理帧格式

主服会话中 840/0 的回包使用**整包 zlib 压缩**：

```
[4B Length][zlib( 4B ActionID + Payload )]
```

例：`00 00 00 e5 | 78 9c ...`，Length=229，解压后首 4 字节为 `03 48 00 00`（840/0）。
840/1 的客户端请求与回包均为未压缩裸包（840/2 未在本次抓包中出现）。

## 四、协议字段（与 AS3 Base 类一致，并已用抓包逐字段验证）

### 4.1 840/0 main_info

```
request: []
response: [UByte result,
           UInt16 count,
           count × [Int type, Int season, Int step, Int player_id,
                    String server_name, String nickname,
                    Int role_id, Int suit_id, Long war_power,
                    UByte is_worship, Int worship_num,
                    UInt16 season_count, season_count × [Int season, Int step],
                    Long weed_award_coin, Long total_award_coin],
           Long can_get_coin]
```

- `player_id=0` 表示该神像位暂无对象（抓包中 type=2/4 为空位），不可膜拜。
- `is_worship=1` 表示今日已膜拜该神像。
- `can_get_coin` 是**被膜拜者**可领取的累计铜钱奖励；普通角色通常为 0。

### 4.2 840/1 worship

```
request:  [Int type]          // 抓包中 type=3/5/1，分别对应 3 个有对象的神像
response: [UByte result, Int worship_ingot]
```

结果码（`Mod_EternalGamesPresent_Base`）：

| 码 | 名称 | 含义 |
| :--- | :--- | :--- |
| 0 | SUCCESS | 成功 |
| 1 | FAILED | 失败 |
| 2 | FUNC_NOT_OPEN | 功能/活动未开放 |
| 3 | HAS_WORSHIPED | 今日已膜拜 |
| 4 | NOT_AWARD | 暂无可领取奖励 |

### 4.3 840/2 get_award

```
request:  []
response: [UByte result, Long get_coin]
```

本次抓包中 `can_get_coin` 始终为 0，官方客户端因此未发送 840/2；该接口字段取自
`Mod_EternalGamesPresent_Base.get_award` 与客户端 `onGet()/getCallBack()` 流程。

## 五、抓包时序与关键证据

```
686.72  客户端 840/0  (PrevAct=558/3)
686.79  服务端 840/0  整包压缩面板，5 个神像，is_worship 全 0，can_get_coin=0

692.94  客户端 840/1  [Int 3][PrevAct=840/0]
692.98  服务端 840/1  result=0, worship_ingot=5
693.06  服务端 0/3    update_player_data: (PLAYER_INGOT=19, 83555)
693.14  服务端 840/0  type=3 变为 is_worship=1，worship_num=38258

700.76  客户端 840/1  [Int 5][PrevAct=840/0]
700.87  服务端 840/1  result=0, worship_ingot=7
700.95  服务端 0/3    PLAYER_INGOT=83562      (+7)
700.95  服务端 840/0  type=5 变为 is_worship=1

705.59  客户端 840/1  [Int 1][PrevAct=840/0]
705.68  服务端 840/1  result=0, worship_ingot=8
705.77  服务端 0/3    PLAYER_INGOT=83570      (+8)
705.77  服务端 840/0  type=1 变为 is_worship=1
```

`0/3` 回包结构为 `[UInt16 count][count × (UByte type, Long value)]`，
其中 `type=19` 即 `Mod_Player_Base.PLAYER_INGOT`。元宝余额 83550 → 83555 → 83562 → 83570
的增量与 840/1 回包 `worship_ingot` 完全一致。

**语义修正（2026-11-21 实机验证）**：`worship_ingot` 是给**被膜拜榜一**的元宝奖励，
不是给膜拜者的。本次抓包客户端自身就是榜一（司空测试号，正是 5 个神像的 player_id），
所以同一会话能看到自己的元宝增加。用 `configs/qq.yaml` 的普通角色「同尘」实测：
膜拜 type=3/5 均返回成功（`worship_ingot=9/7`），但重新登录后元宝仍为 100000，
**普通膜拜者膜拜他人不会获得元宝**。膜拜对膜拜者本身免费，不消耗任何元宝；
被膜拜榜一获得元宝，并通过 840/2 领取累计铜钱。

## 六、实机验证（configs/qq.yaml · s0 · 同尘）

```
18:53:22  840/0  result=0, 5 个神像, 3 个可膜拜, can_get_coin=0
18:53:22  840/1  type=3  result=0  worship_ingot=9
18:53:23  840/1  type=5  result=0  worship_ingot=7
18:53:33  840/1  type=1  10s 无回包（连续快速膜拜被服务端静默丢弃）
18:53:53  重新登录: ingots=100000（与膜拜前一致）
          面板: type=3/5 is_worship=true, type=1 仍为 false
19:00:15  840/1  type=1  result=0  target_ingot=5（单独重试成功）
          重新登录: ingots 仍为 100000；3 个神像 is_worship 全部 true
```

结论：

- 840/0、840/1 的请求格式、PrevAct 链与回包解析在实机完全对齐；
- 膜拜成功状态会持久化（is_worship 变 true、worship_num +1）；
- 普通膜拜者不增加元宝；奖励归被膜拜榜一；
- 连续快速膜拜会被静默丢弃，例程已改为**每次成功后刷新 840/0 并注入抖动间隔**，
  与官方客户端行为一致。

## 七、PrevAct 细节

官方客户端在 840/0/1/2 的载荷末尾都携带 4 字节 PrevAct（主服网关原子链）：

- 840/0：载荷仅 4 字节 `[PrevAct]`；
- 840/1：载荷 8 字节 `[Int type][PrevAct]`；
- 840/2：载荷 4 字节 `[PrevAct]`（按 main_info 风格实现）。

Go 侧统一使用 `Packet.HasPrevAct = true` + 4 字节占位，由 `RoleSession.Send` 在发送时覆盖为
最后收到的 ActionID。

## 八、实现落点

| 层 | 文件 | 内容 |
| :--- | :--- | :--- |
| 协议 | `internal/protocol/wangu_shenxu.go` | 840/0/1/2 构造与解析、结果码文本 |
| 协议测试 | `internal/protocol/wangu_shenxu_test.go` | 抓包黄金面板 685B、膜拜/领奖成功与失败码 |
| 例程 | `internal/routines/social/wangu_shenxu.go` | 面板 → 逐个膜拜未膜拜神像（每次成功后刷新面板并抖动间隔）→ `can_get_coin>0` 时领奖 |
| 例程测试 | `internal/routines/social/wangu_shenxu_test.go` | 成功、已膜拜跳过、领奖、未开放熔断、全部失败退避 |
| 探针 | `test/probes/probe_wangu_shenxu_live.go` | 默认只读面板；`WORSHIP_EXECUTE=1` 才执行 840/1/2 |

自动化行为约束：

- 只对 `player_id > 0 && is_worship == false` 的神像发送 840/1；
- 每次膜拜成功后重新拉取 840/0 并注入抖动，避免连续快速请求被服务端静默丢弃；
- `can_get_coin == 0` 时不发送 840/2，避免 `NOT_AWARD(4)`；
- 膜拜对膜拜者免费，不消耗元宝；`worship_ingot` 属于被膜拜榜一，普通角色不会因此增加元宝；
- 面板无回包 / 未开放 / 全部膜拜失败按静默丢包退避（1 小时重试，连续 3 次挂起至次日）。