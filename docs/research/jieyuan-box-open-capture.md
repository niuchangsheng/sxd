# 结缘宝箱 232/34 开箱抓包分析（2026-11-13）

## 来源

- 抓包文件：`/tmp/jieyuan.pcapng`（官方客户端在活动期间连续开启两次 3938 高级结缘宝箱）
- 服务端：`81.70.56.121:8200`
- 提取命令：`tcpdump -nn -r /tmp/jieyuan.pcapng 'tcp port 8200' -w /tmp/game8200b.pcap`

## 客户端 → 服务端

每次开箱固定为三步：

```
# 232/30 库存列表（PrevAct=最近一次动作）
00e8001e 00e80022

# 528/1 上下文握手 [Uint16 0x0704][Uint32 PrevAct=232/30]
02100001 0704 00e8001e

# 232/34 批量开箱 [Int item_id][Int count][Uint32 PrevAct=528/1]
00e80022 00000f62 00000004 02100001
```

抓包中两次开箱（均为 item 3938、count 4）严格重复上述三步。
`00e80022` 是 232/30 列表请求里的 PrevAct，说明列表发生在两次开箱之间。

## 服务端 → 客户端

```
# 528/1 回包：仅 1 字节 result_code=0
02100001 00

# 232/34 回包：整包 zlib 压缩（帧体首字节 0x78 0x9c）
# 解压后 107 字节：
00e80022 05 000a <10 × 10B 奖励条目>
```

- 解压后首字节 `05` 为 result_code，实测两次成功开箱均为 5。例程已将该值定义为
  `protocol.JieYuanBoxOpenResultSuccess`；其它值一律按失败终止，不盲目重试。
- 奖励条目 10 字节编码，其中包含 `0x0f7b`(3963)、`0x0f77`(3959)、`0x0f73`(3955)
  等缘分奖励道具 ID 及数量。当前自动化只校验 result_code 与库存数量变化，不解析奖励明细。

## 库存列表 232/30 回包

```
00e8001e 0005
  00000f61 00000000
  00000f62 0000000f
  00000f64 00000002
  00000f65 00000000
  00000f66 0000011f
  0014 005a 0000
```

- `[Uint16 count][count × (Int32 item_id, Int32 count)]`，尾部 `0014 005a 0000` 为额外计数。
- 开箱后同一道具的数量条目会变化，可作为成功副作用核对（例程已保留该核对）。

## 实现位置

- `internal/protocol/feature_packets.go`：`BuildJieYuanBoxOpenContextPacket`（528/1）、
  `BuildJieYuanBoxOpenPacket`（232/34）、`ParseJieYuanOpenContextResult`、
  `ParseJieYuanBoxOpenResult`。
- `internal/routines/social/social.go`：列表 → 528/1 握手 → 232/34 开箱 → 库存核对。
- 单测：`internal/protocol/feature_packets_test.go`、`internal/routines/social/jieyuan_box_test.go`。