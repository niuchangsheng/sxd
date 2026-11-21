# 结缘宝箱 232/30 · 528/1 · 232/34 抓包与 PrevAct 链分析

## 来源

| 抓包 | 时间 | 内容 |
| :--- | :--- | :--- |
| `/tmp/jieyuan.pcapng` | 2026-11-13 | 官方客户端连续开启 3938 高级宝箱 |
| `/tmp/jieyuan2.pcapng` | 2026-11-21 | 官方客户端一键开启 3937/3938/3940/3941/3942 全部箱型 |

- 服务端：`81.70.56.121:8200`
- 提取：`tcpdump -nn -r <pcap> 'tcp port 8200' -w /tmp/game8200.pcap`
- 客户端反编译基准：`rea-sxd/decompiled/scripts/com/DataBase.as`、
  `Mod_LinkFate_Base`（ffdec 从 `rea-sxd/swf/Main.swf` 导出）、
  `rea-sxd/decompiled/scripts/com/Protocol.as`

## 协议字典（反编译确认）

`com.protocols.Mod_LinkFate_Base`：

| Action | ActionID | 方法 | request | response |
| :--- | :--- | :--- | :--- | :--- |
| 232/30 | `0x00E8001E` | `get_link_fate_box` | `[]` + PrevAct | `[[Int,Int], Short, Short, Short]` |
| 232/31 | `0x00E8001F` | `open_link_fate_box` | `[Int,Int]` + PrevAct | `[UByte, Int, [Short,Short]]` |
| 232/34 | `0x00E80022` | `one_key_open_box` | `[Int,Int]` + PrevAct | `[UByte, [Int,[Short,Short]]]` |

`com.protocols.Mod_PlayerSetting_Base`：528/1 `set_setting` request=`[Byte,Byte]`，
response=`[UByte]`。

## PrevAct 尾链规则（关键）

官方客户端 `DataBase.netCall`（`DataBase.as:255-270`）对**每一个**请求都在包尾
追加 `tmpBuff`，即「**上一条发送的请求**的 `[Module,Action]` 4 字节」，随后把
`tmpBuff` 更新为当前请求。它与「最后收到的包」无关：

- 服务端会对 528/1、232/34 校验这 4 字节；链路错位时**静默丢弃**（无回包）。
- 抓包 23:45:51 处客户端在 2.847 发出 528/1、2.878 发出 232/34，而 528/1 的
  回包 2.881 才到达——232/34 的 PrevAct 仍为 `0x02100001`，证明锚点是「最后
  发送」而非「最后收到」。
- 抓包中第一条 528/1 的 PrevAct 为 `0x008A0000`（抓包开始前发送的上一条请求），
  后续均为 `0x00E8001E`（列表）；232/34 均为 `0x02100001`（528/1）。

因此 232/34 前的请求链必须是：

```
232/30 (或任意请求)  →  528/1 [07 04 + 上一条请求]  →  232/34 [item, 4 + 528/1]
```

## 开箱参数与批次语义

- `one_key_open_box` 参数为 `[Int item_id, Int autoMergeQuality]`，不是数量；
  抓包中恒为 `[item, 4]`（合成档位 4，对应 528/1 的 `07 04`）。
- **一次请求按最多 10 个同型宝箱连开**：列表数量 25→15→5→0 严格每次 -10
  （库存不足 10 时按剩余数量结算），开出的五行石/经验石按档位自动合成，
  并可能回补高阶宝箱（例如 10 个普通箱 → +4 高级箱）。
- 传说箱 3942 也可正常开启（抓包末尾即开启 3942 成功）；自动化规则仍按产品
  约定跳过 3942。

## 232/34 回包

整包 zlib 压缩（帧体首字节 `0x78 0x9C`），解压后：

```
00e80022 05 000a
  <entry × 10>
```

- `05` = `Mod_LinkFate_Base.SUCCESS`（成功码）；其它值按失败分流。
- `000a` = 奖励条目数（Uint16）。
- 每条 entry = `[Int, Uint16 n, n × (Short, Short)]`，抓包中每项 10 字节：
  `00000f62 | 0001 | 0f7b 0000`。条目中的 Int 为开箱来源/档位信息，
  inner 列表为 `(道具 ID, 附加字段)`。

## 232/30 列表回包

```
00e8001e 0005
  00000f61 00000000
  00000f62 0000000f
  00000f64 00000002
  00000f65 00000000
  00000f66 0000011f
0014 005a 0000
```

- `[Uint16 count][count × (Int32 item_id, Int32 count)]`，尾部 3 个 Uint16
  （抓包中恒为 `0014 005a 0000`）。

## 线上故障与修复（2026-11-21）

旧实现把 528/1 的 PrevAct 固定写成 `0x00E8001E`（232/30）。但例程在列表后、
开箱前会插入背包查询 2/41（`0x00020029`），导致：

1. 6 字节 528/1 的尾链 `0x00E8001E` ≠ 实际上一条请求 `0x00020029` → 被丢弃；
2. 例程退回裸 2 字节 528/1，服务端返回 `result=0`，但**不建立有效上下文**；
3. 随后 232/34 的 PrevAct 虽然等于 528/1，仍被服务端丢弃。

表现即日志中的「开箱无回包且库存未变化」，所有箱型被误标 6 小时跳过。
2026-11-21 在 s0-tongchen 实测：把 528/1 尾链改为真实上一条请求后，单次
232/34 立即成功（3937：35→25），多批次流程亦全量开完（27 个）。

修复：

- `internal/protocol/feature_packets.go`：
  - `BuildJieYuanBoxOpenContextPacket()` / `BuildJieYuanBoxOpenPacket(item, quality)`
    写入 4 字节占位并置 `Packet.PrevActFromLastSent`；
  - 删除裸 2 字节兜底 builder（会造成「握手成功但开不了箱」的假象）。
- `internal/client/session.go`：`Send` 在 `writeMu` 内用最后发送动作填充
  `PrevActFromLastSent` 包尾，与心跳发送互斥。
- `internal/routines/social/social.go`：`callJieYuanContext` 统一握手；无回包时
  用最新锚点重试一次，不再使用裸 2 字节兜底。
- 单测：`internal/protocol/feature_packets_test.go`、
  `internal/routines/social/jieyuan_box_test.go`（断言 2/41 之后 528/1 的
  PrevAct 必须是 `0x00020029`，232/34 的 PrevAct 必须是 528/1）。

## 验证探针

- `test/probes/probe_jieyuan_chain_live.go`：对照 A（官方链）/B（中间插 2/41，
  PrevAct 跟随）/C（旧实现固定 232/30）。A、B 成功，C 复现静默丢弃。
- `test/probes/probe_jieyuan_box_live.go`：跑真实 Routine 并复查库存。

## 实现位置

- `internal/protocol/feature_packets.go`：列表/握手/开箱 builder 与结果解析。
- `internal/routines/social/social.go`：列表 → 背包空格检查 → 528/1 → 232/34
  → 库存核对 → 按箱型迭代。