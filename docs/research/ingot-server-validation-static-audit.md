# 元宝相关服务端校验薄弱点静态审计报告

- **日期**: 2026-11-06
- **性质**: 纯静态分析（白盒）。**不含任何利用代码，未在线上服务器执行任何刷取/验证操作。**
- **用途**: 负责任披露与服务端加固参考。
- **基准事实**: 官方 `Main.swf`（CDN `2026102301` 版本），FFDec 全量导出
  `com.protocols`（713 类 / 6788 动作 / 711 模块）及目标视图/控制器调用点。
- **产物**:
  - 解析器与动作清单：`~/rea-sxd/audit/parse_proto.py`、`~/rea-sxd/audit/actions.json`
  - 货币敏感候选清单：`~/rea-sxd/audit/filter_sensitive.py`、`~/rea-sxd/audit/candidates.txt`（615 条）
  - 协议导出目录：`~/rea-sxd/proto_full/`（com.protocols）、`~/rea-sxd/code_full/`、`~/rea-sxd/code_targeted/`

---

## 一、审计方法

1. 用 FFDec 从 `Main.swf` 导出完整 `com.protocols` 包，解析每个 RPC 动作的
   `module/action/request/response` schema（类型序列即客户端→服务端的字段清单）。
2. 按动作名（currency 关键词）与请求参数类型（IntUtil/ByteUtil/StringUtil 等）
   筛选"客户端上送数值/字符串"的货币敏感面。
3. 对可疑动作回溯视图层（`com/views/**`）调用点，确认客户端侧已有的校验逻辑，
   推导服务端**必须**独立实现的校验集合。
4. 检查协议承载层（`com/DataBase.as`、`com/Data.as`）的帧格式与防重放机制。

> 静态分析只能证明"客户端发送什么、客户端校验什么"；服务端实际校验状况
> **无法从客户端代码证明**，因此下列各项均为**候选薄弱点**，需按第六节验证计划
> 单次只读验证后才能定级。

---

## 二、协议层结构性观察

### 2.1 帧格式无防重放字段

`com/DataBase.as` `netCall()`（约 157–265 行）：

```
writeInt(length) + writeShort(mod) + writeShort(action) + Protocol.synthesize(args, request)
```

- 帧内**无 nonce、无序列号、无时间戳、无逐请求签名**；鉴权仅存在于连接/会话层。
- 后果：任何一次合法请求的原始字节均可原样重放。**所有领取/兑换/购买类动作的
  防重放与幂等完全依赖服务端状态机**（如"已领取"标志的原子置位）。
- 若服务端存在"先发奖励、后置标志"的非原子窗口，则并发重放可重复入账。

### 2.2 限频仅为客户端侧

`DataBase.call()` 的 `hasNotAllow()/addNotAllow()`（`param4=false` 时）只阻止
**本客户端**短间隔重复发包，对服务端限频无任何约束；脚本化客户端可绕过。

### 2.3 客户端错误码体系完备（服务端语义参考）

`Mod_*_Base` 类中大量 `NOT_ENOUGTH_INGOT`、`GOTTEN`、`COMPLETE_ALL`、
`FUNC_NOT_OPEN` 等常量表明服务端**存在**语义化失败码。这有利于审计：
验证时可依据回码判断服务端校验是否存在，而无需观察余额变化。

---

## 三、发现清单（按严重度候选排序）

### F1（关键候选）：Mod 559 `DebugGm.cheat` —— 通用 RPC 后门端点

`com/protocols/Mod_DebugGm_Base.as`：

```
cheat:           module=559, action=2, request=[String, String, String] → UByte
help_info:       module=559, action=1, request=[] → 命令自述列表
client_report_excetion: m559/a3
client_mod_memory:      m559/a4
```

调用链：`com/assist/gm/Debug.as` → `GmDebug.init()`（"GM 指令"悬浮按钮）→
`com/views/interaction/DebugGmView.onSendCheat(module, action, args)` →
`Mod_DebugGm.cheat`。

要点：

- `cheat` 是**通用桥接**：以字符串形式指定任意 `(module, action, args)` 并由
  服务端代为执行，即"服务端自带协议内代理执行器"。若服务端未做 GM 角色鉴权，
  等价于完全绕过客户端的一切 UI 约束（含付费确认弹窗、余额检查）。
- 生产客户端仅在 `Config.EnableProtocolHelper == true` 时启用该入口，而
  `Config.as` 中该常量为编译期 `false`——**禁用是纯客户端行为**。
  客户端禁用 ≠ 服务端端点关闭。`help_info` 的响应本身即服务端可执行命令的
  自述清单，信息泄露面也在此。
- **影响（若未鉴权）**：完全控制任意模块/动作调用，可绕过一切客户端校验，
  元宝可凭空发放。这是本报告唯一可能直接达成"大量元宝"级别的路径，
  因此其服务端鉴权状态是整份审计的核心问题。
- **修复建议**：生产环境网关直接封禁 module 559（或 action≥2 仅限 GM 会话）；
  `help_info` 同样不应向普通会话返回命令清单；为 GM 面板增加独立鉴权通道。

### F2（高危候选）：Mod 707 `CatGold.store` —— 客户端仅校验上界的存入金额

`store: module=707, action=2, request=[IntUtil(金额), ByteUtil(天数)]`。

调用点 `com/views/cat_gold/CatGoldView.as` `onStore()`：

```
if(storeIngot > _ctrl.player.ingot) { 提示不足; return; }   // 唯一的客户端校验：上界
```

- 客户端**没有** `storeIngot > 0`、倍数约束（如 100 的整数倍）、`day` 白名单
  （1/3/7…）校验——这些若同样缺失于服务端，负数/零/超大金额均可入包
  （`IntUtil` 为有符号 32 位，负数可编码）。
- 招财猫为"存入→到期返还"模型；若服务端以客户端金额记账而非服务端模板，
  负存入可能反向入账（存入时 `+=` 退款、返还时按绝对值发放）。
- **修复建议**：服务端校验 `0 < amount ≤ 余额`、`amount` 属于合法档位表、
  `day ∈ 白名单`、单日次数上限，且扣款/记账同事务。

### F3（结构性）：领取/兑换动作的幂等完全依赖服务端

结合 2.1（无防重放）与下列高频敏感动作，服务端必须对每个动作实现
**原子领取状态迁移**，否则重复包/并发包即可重复入账：

| 动作 | 模块/动作 | 请求 |
| :-- | :-- | :-- |
| `Email.get_attachment_award` | m345/a2 | `IntUtil(mail_id)` |
| `Email.one_key_get_award` | m345/a6 | `[]` |
| `ActivityOrder.get_award` / `get_all_award` | m788/a3,a4 | `IntUtil×1~3` |
| 各活动 `get_award/receive/gain_*` 族 | 全库 600+ | 多为档位 ID |

邮件附件领取尤其值得优先审计：奖励经邮件投递（排行结算、补偿），
`get_attachment_award(mail_id)` 以客户端提供的 ID 为键，若服务端
"读取附件内容 → 发奖 → 标记已领"非原子或以读取状态代替领取状态，
存在重复领取窗口。

### F4（类别）：客户端自选奖励的 `select/choose` 族 —— 服务端需池校验

全库共 **139** 个 `select/choose` 类动作（如 `select_draw_award`、
`select_award`、`choose_award`）。客户端上送档位/物品 ID，服务端必须
校验"所选奖励确实在玩家当前可达奖池中"。若服务端仅查模板表而
不校验池归属/次数，可越档领取（含元宝档）。

### F5（类别）：客户端上送数量的 exchange/store 族

客户端请求带数值参数的动作共 2864 个，其中多数值动作 876 个。
金额/数量直接由客户端提供的典型族（节选自 `candidates.txt`）：

| 动作 | 模块/动作 | 请求 |
| :-- | :-- | :-- |
| `CatGold.store` | m707/a2 | `[Int, Byte]` |
| `MakeGoodStart.write_num` | m404/a1 | `[Int]`（用户自由输入整数） |
| `AmagicEquip.exp_exchange` | m796/a9 | `[Int, Int]` |
| `Equipment.exchange_resource` / `batch_exchange_resource` | m77/a14,a29 | `[Int, Int(,Int)]` |
| `QiXiExchange.exchange_item` / `send_exchange_info` | m484/a5,a4 | `[String, Int, Int, Int]` |
| `StBridgeBless.exchange` / `send_exchange` | m372/a7,a5 | `[Int, Int(,Int)]` |
| `HumanWorld.exchange_invite` / `exchange_response` | m531/a42,a44 | `[Int×3~4]` |
| `Pawnshop.exchange` | m332/a5 | `[Int, Int]` |

其中"玩家↔玩家交换"类（QiXi、StBridgeBless、HumanWorld）还引入**对端状态
一致性**问题：双方各自确认时服务端需在同一事务内校验双方资产，否则存在
经典的双花/撤回竞态。

### F6（低危候选）：其它单项观察

- `Mod_PlayerNpc.present_ingot`（m158/a2，`[UByte]`）：参数为档位类型
  （0=铜钱/1=蛋），金额固定 10 元宝由服务端模板决定，客户端无金额上送。
  风险集中在档位枚举是否服务端白名单化。
- `Mod_ReleaseWelfare.get_extra_ingot`（m240/a3，`[ByteUtil]`）：有符号字节
  上送 `day_seq`；仅需服务端校验序列合法性。
- `Mod_Farm.ingot_for_farmland`（m13/a15，`[IntUtil]`）：上送田地等级，
  元宝扣减应由服务端按等级表计算。

---

## 四、影响评估汇总

| 编号 | 类型 | 静态置信度 | 需验证点 | 潜在影响 |
| :-- | :-- | :-- | :-- | :-- |
| F1 | 调试后门端点暴露 | 端点存在=确定；鉴权缺失=未知 | m559/a1 只读调用是否放行 | 完全绕过业务约束 |
| F2 | 金额下界/档位校验 | 客户端无下界=确定；服务端=未知 | 负数/越档存入的回码 | 反向入账 |
| F3 | 防重放/幂等 | 协议无字段=确定；服务端幂等=未知 | 同包重放的回码与余额 | 重复入账 |
| F4 | 奖池归属校验 | 未知 | 越档 select 的回码 | 越档领奖 |
| F5 | 数量/交换一致性 | 未知 | 边界值回码 | 超发/双花 |

---

## 五、修复建议（服务端输入校验矩阵）

对所有"客户端上送数值"的 2864 个动作，服务端应统一执行：

1. **类型与范围**：正数、非零、上限（含余额上限）、枚举白名单（档位/天数）。
2. **模板权威**：价格、汇率、奖励内容一律以服务端模板计算，绝不信任客户端
   上送的价格/金额字段（客户端仅作展示）。
3. **原子幂等**：领取状态迁移与资产变更同事务；以唯一键（邮件 ID+附件索引、
   活动档位）做数据库级约束（唯一索引/条件更新），杜绝"先发后标"。
4. **会话限频**：服务端侧对模块/动作做 QoS 与异常模式检测（当前仅客户端限频）。
5. **封禁调试面**：module 559 生产环境网关层拒绝；GM 指令走独立鉴权通道。
6. **错误码不泄露内部状态**：`help_info` 类自述接口不对普通会话开放。

---

## 六、验证计划（**未执行**，ADR-0013 探针规范）

如需将上述候选定级为确认漏洞，按最小化原则逐一单次验证（只读优先、
绝不做批量利用）：

1. **F1**：`m559/a1 help_info`（只读）——普通会话若返回命令清单即证明未鉴权。
   此步为整个验证计划的先决项；**不建议也绝不执行 `m559/a2 cheat`**。
2. **F2**：`m707/a2` 发送 `amount=0`（无害边界值）观察回码是否区分
   `NOT_ENOUGTH_INGOT` 与专用参数错误码。
3. **F3**：对已领取完成的档位重放原包一次，观察回码（`GOTTEN` 类）。
4. **F4**：`select_award` 上送一个不存在的档位 ID，观察回码。
5. 验证探针统一沉淀 `test/probes/`，声明 `//go:build probe`，结果回写本报告。

> 本报告到此为止：以上验证**均未执行**，所有结论停留在静态证据层面，
> 报告用途为向运营方披露与加固，不用于任何获利行为。

---

## 七、附录

- **协议动作全量字典（仓库内，6788 条）**：`docs/protocol/actions.json`，
  查询与 Go 骨架生成工具：`scripts/protocol/protodict.py`，
  用法见 `docs/protocol/README.md`。后续编写新玩法 Routine 或补
  ADR-0015 失败码分支时可直接查表，无需重新反编译 `Main.swf`。
- 审计会话原始产物：`~/rea-sxd/audit/actions.json`、
  `~/rea-sxd/audit/candidates.txt`（615 条货币敏感候选）、
  `~/rea-sxd/audit/parse_proto.py`、`~/rea-sxd/audit/filter_sensitive.py`
- 关键源码位置：
  - `com/Config.as`（EnableProtocolHelper=false）
  - `com/assist/gm/Debug.as`、`com/assist/gm/GmDebug.as`、
    `com/views/interaction/DebugGmView.as`
  - `com/DataBase.as` `call()/netCall()`（帧格式与客户端限频）
  - `com/views/cat_gold/CatGoldView.as` `onStore()`（仅上界校验）
  - `com/views/WinterCarnivalActivityView.as`（MakeGoodStart write_num /
    send_ingot_to_player 调用点）
