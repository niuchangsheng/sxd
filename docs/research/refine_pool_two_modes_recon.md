# 粗铁淬炼池双目标（玄铁 / 灵材）协议侦察

> 关联 Issue: [#1](https://github.com/niuchangsheng/sxd/issues/1) `spirit_pool: 确认淬炼池第二种「一键淬炼」的产出`
> 协议基准: 原版 `Main.swf` 反编译类 `com.protocols.Mod_RefinePool_Base`、
> `com.views.RefinePoolView`、`com.assist.view.info.refine_pool.RefinePoolInfo`。

## 1. 结论

淬炼池面板提供两个一键操作入口，协议请求的第一个字节即产出目标：

| target | 客户端常量 | 客户端文案 | 驱动函数 | 产出 |
| :---: | :--- | :--- | :--- | :--- |
| 5 | `REFINE_1` | 淬炼 | `refine` | 玄铁 / 炼器精华，暴击额外掉落灵材 |
| 6 | `REFINE_2` | 提炼 | `refine` | 灵材 |

请求完整语义（3 个 UByte + prevAct）：

```text
UByte target   // 5 REFINE_1(玄铁) / 6 REFINE_2(灵材)
UByte cost     // 7 COINS(铜钱) / 8 INGOT(元宝)
UByte mode     // 9 NORMAL(单次) / 10 ONE_KEY(一键)
Uint32 prevAct
```

**请求不携带槽位数量**。一键模式下由服务端对该角色全部已完成槽位生效。
旧实现把 `target/cost/mode` 误读为 `quality/type/count`，并以已完成槽位数作为
第 3 字节；在恰好 10 个已完成槽位时因数值碰巧等于 `ONE_KEY=10` 才被服务端受理。

## 1.1 功能开放探测

未解锁淬炼池的角色，服务端会**静默丢弃** `open_panel` 请求，客户端只能等到超时。
原版提供通用探测接口 `Mod_Player_Base.check_function_open`：

- Action：`0x00000045 (Mod 0, Act 69)`
- 请求：`Int32 function_id, Uint32 prevAct`
- 回包：`UByte`，119 = TRUE（开放）、120 = FALSE（未开放）
- 淬炼池 `function_id = 163`（`FunctionTypeData` 中 `RefinePool`）

实机验证（2026-11-06）：s1 角色返回 `0x78`（120，未开放），s0 角色返回 `0x77`
（119，开放）。自动化应先探测再发业务请求，未开启直接跳过。

## 2. 回包结果码

| result | 常量 |
| :---: | :--- |
| 11 | `SUCCESS` |
| 12 | `LACK_OF_COINS` |
| 13 | `LACK_OF_INGOT` |
| 14 | `FAILED` |

旧实现把 `SUCCESS` 当作 9、把 11 当作 `LACK_OF_INGOT`，因此曾出现
"成功回包首字节为 0x0B 却疑似失败" 的误判；现按原版常量修正。

## 3. 奖励字段

每个 award 包含两个 `[Int32, Int32]` 列表：

- `award_info`：道具 ID + 数量（主产物为炼器精华 4759，高品级槽位可产出玄铁 4760）。
- `lingcai_info`：灵材编号 + 数量。客户端数据层字段名即 `lingcai_info`，
  编号对应 `com.assist.server.LingCaiType`，**不是** `Pieb.db` item 表主键。

`haha.pcapng` 样本解包结果：10 槽合计炼器精华 110（品质 1/2/3/6 分别 5/10/15/20），
槽位 3 暴击在 `lingcai_info` 掉落灵材 14 ×1，`xuan_tie` 总量保持 803 不变。

## 4. 自动化策略

- 新增 `scheduler.spirit_pool.refine_target` 配置项：
  - `xuantie`（默认）：`05 07 0a`，一键淬炼玄铁。
  - `lingcai`：`06 07 0a`，一键提炼灵材。
- 消耗类型固定为铜钱（`cost=7`），不提供元宝开关，符合 [ADR-0004](../adr/) 一类的
  "自动化不消耗元宝" 约束。
- 失败语义：铜钱不足 / 元宝不足返回 `scheduler.ErrConditionNotMet`；
  失败与未知结果码终止本轮并记录 `result/target`；成功码无产出按失败处理。

## 5. 复现命令

只读探测：

```bash
go run -tags=probe test/probes/probe_spirit_pool_live.go
```

显式执行（默认玄铁 + 铜钱 + 一键）：

```bash
SPIRIT_POOL_EXECUTE=1 go run -tags=probe test/probes/probe_spirit_pool_live.go
```

提炼灵材：

```bash
SPIRIT_POOL_EXECUTE=1 SPIRIT_POOL_TARGET=6 \
  go run -tags=probe test/probes/probe_spirit_pool_live.go
```