# 英雄副本多城镇扫荡侦察（Module 35，2026-11-13/14）

## 抓包证据

`/tmp/saodang.pcapng`（20 秒窗口）显示官方客户端不切换场景连续扫荡多个城镇：

```
35/0 get_hero_mission_list  [Short town_id] + PrevAct(心跳 0x17)
35/2 start_practice         [Short town_id][Byte auto_num=1][Byte hero_type=0] + PrevAct
35/7 quickly                + PrevAct
```

城镇顺序：`54(0x36) → 43(0x2b) → 40(0x28) → 39(0x27)`，20 秒内两轮；
服务端对每城返回 35/0 列表（73 字节）、35/2 `result=3(SUCCESS)`、35/4 结算推送。

## 线上关键差异（2026-11-14 实测）

1. **上下文敏感**：登录后立即发 35/0（城镇 54）能正常回包；
   但在查询过活动面板 `0/42` 之后再发 35/0，服务端会静默丢弃直到超时。
   补发心跳也无法恢复，必须在任何其它例程查询之前执行。
2. `auto_num=1` 在官方客户端出现，但该字段语义是“战斗后自动购买翻牌奖励”，
   可能产生消耗；自动化固定使用 `auto_num=0`。
3. 城镇 5（MissionLock=1101600）与 55（71031）在当前账号未解锁/无英雄副本，
   请求会被静默丢弃并污染 PrevAct 链；例程从城镇 54 开始并跳过 5/55。
4. 一次失败的 35/0 会把会话 `lastActionID` 推进到未送达的动作，导致后续
   请求全部失配；失败后需回退到 `LastAckActionID()` 再继续。

## 实现

- `internal/routines/dungeon/dungeon.go`：
  - `RunHeroMissionSweepOnLogin`：登录后、调度器启动前执行多城镇扫荡
    （54 → 43 → 40 → 39 …，跳过 5/55，失败回退 PrevAct）；
  - 常规 `sweep_elite` 中英雄副本与 Module 25 全局精英扫荡不再互斥
    （两个 yaml 都配置了 `global_elite: true`）。
- `cmd/server/fleet.go`：在淬炼池/渡业台登录钩子之后、`dispatcher.StartAll` 之前调用。
- 探针：`probe_hero_mission_direct_live.go`（验证 35/0/2/7 单城可达）、
  `probe_hero_sweep_login_live.go`（验证登录钩子多城镇扫荡）。
## 2026-11-14 qq s0 实测收敛结果

- **35/0 列表形态**：裸 `[Uint16 town]` 可回包，但后续 35/2 会被丢弃；
  登录钩子改回官方 `[Uint16 town][Uint32 PrevAct]` 后，配合 35/7 之后
  约 1.2s 结算窗口，连续 20 城全部成功：`登录后英雄副本多城镇扫荡完成 swept=20`
  （体力 0 时同样成功，说明英雄副本不消耗体力）。
- **2/29 系统奖励**：带 PrevAct 的 8 字节载荷对 `0x055DFC41-44` 全部静默丢弃；
  改为裸 `[Int award_id]` 后 `claimed=4 total=4`。
- **2/53 使用背包物品**：会话层不再自动追加 PrevAct（并行会话 `a1f7f4a`）。
- **25/0 副本状态**：仅接受裸 `[Int32 missionID]`（`4b2cd81`）。
- **41/1 膜拜上香**：4 字节与 8 字节载荷、跳过 41/4 均被服务端静默丢弃；
  41/0、41/4 正常。判定为场景/资格上下文限制，等待官方客户端「上香」抓包。

## 其他观测（非缺陷）

- 远征任务 `result=3(FAILED)`：服务端业务拒绝，例程记录后继续下一任务。
- 每日灵石 `FAIL`：协议只有一个失败码，无法区分“失败/今日已领”。
- PC 微端礼包 `result=111`：资格状态未知，按 2 小时冷却重试。
