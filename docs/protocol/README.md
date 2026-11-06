# 全量协议字典 (Protocol Dictionary)

本目录保存从官方客户端 `Main.swf` 反编译得到的**全量 RPC 协议字典**，
用于在编写新玩法 Routine、补协议编解码或补齐 ADR-0015 结果码分支时
**直接查表**，无需每次重新反编译 `Main.swf`。

- `actions.json`：711 个模块 / 6788 个动作的
  `module / action / 请求 schema / 响应 schema`，按协议类归并，
  并附带每个 `Mod_*_Base` 类的全部结果码常量。

> 字典版本以客户端 `com/Config.as` 的 `GameVersion` 为准
> (当前对应 `2026102301`)。游戏大版本更新后需重新生成。

## 一、查询

```bash
# 按模块列出全部动作（含动作 ID 0xMMAA 与类内结果码）
python3 scripts/protocol/protodict.py lookup docs/protocol/actions.json --module 707 --consts

# 按动作名模糊匹配
python3 scripts/protocol/protodict.py lookup docs/protocol/actions.json --action '^send_ingot'

# 按协议类名
python3 scripts/protocol/protodict.py lookup docs/protocol/actions.json --class Mod_CatGold_Base

# 仅元宝相关动作
python3 scripts/protocol/protodict.py lookup docs/protocol/actions.json --ingot
```

## 二、生成 Go 协议骨架

```bash
python3 scripts/protocol/protodict.py codegen docs/protocol/actions.json --module 707 --action 2
```

输出包含：

- `ModuleXxx` 模块常量与 `ActionIDXxxYyy uint32 = 0xMMAA0000` 动作常量
- 类内全部结果码常量（符合 ADR-0015 的失败码分流要求）
- 请求/响应 struct 骨架（`Arg1/Arg2…` 为占位字段名，
  需对照视图层调用点补齐语义，如 `CatGoldView.onStore` 中
  `Arg1=存入元宝数, Arg2=天数`）
- 复合数组元素类型的命名 struct

生成的是**骨架**：字段命名、编解码方法（`MarshalBinary` /
`Parse…`）与失败码语义分支仍需按 `internal/protocol` 现有约定手写补齐。

## 三、类型映射

| AS3 线类型 | Go 类型 | 字节数 |
| :-- | :-- | :-- |
| `ByteUtil` | `int8` | 1 |
| `UByteUtil` | `uint8` | 1 |
| `ShortUtil` | `int16` | 2 |
| `UShortUtil` | `uint16` | 2 |
| `IntUtil` | `int32` | 4 |
| `UIntUtil` | `uint32` | 4 |
| `LongUtil` | `int64` | 8 |
| `StringUtil` | `string` | 2B 长度前缀 + UTF-8 |

schema 字符串编码规则：`[...]` 为字段序列；嵌套 `[...]` 表示
"数组元素为结构体，其字段为首层序列"。例：

```
[IntUtil,ByteUtil]                     -> 两个基础字段
[IntUtil,[ByteUtil,ByteUtil,IntUtil]]  -> 1 个 int + 1 个结构体数组
```

## 四、重新生成字典

```bash
# 1) 从 Main.swf 导出完整 com.protocols 包（FFDec）
java -jar ~/rea-sxd/tools/ffdec/ffdec.jar -cli \
     -selectclass 'com.protocols.*' \
     -export script ~/rea-sxd/proto_full ~/rea-sxd/swf/Main.swf

# 2) 重建字典
python3 scripts/protocol/protodict.py build \
    ~/rea-sxd/proto_full/scripts/com/protocols docs/protocol/actions.json
```

注意：`Main_uncompressed.swf` 为不完整流，FFDec 会报
`EndOfStreamException`，请直接使用原始 `Main.swf`。