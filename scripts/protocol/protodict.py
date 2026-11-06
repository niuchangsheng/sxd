#!/usr/bin/env python3
"""神仙道全量协议字典工具 (protocol dictionary tool).

从 FFDec 导出的 com.protocols 反编译目录构建 module/action/schema 字典，
支持按模块或动作名查询，并按仓库 internal/protocol 约定生成 Go 协议骨架。

用法:
  # 1) 从反编译目录重建字典 (只需 Main.swf 版本变化时重跑)
  python3 scripts/protocol/protodict.py build ~/rea-sxd/proto_full/scripts/com/protocols docs/protocol/actions.json

  # 2) 查询: 按模块 / 动作名模糊 / 类名
  python3 scripts/protocol/protodict.py lookup docs/protocol/actions.json --module 707
  python3 scripts/protocol/protodict.py lookup docs/protocol/actions.json --action store
  python3 scripts/protocol/protodict.py lookup docs/protocol/actions.json --class Mod_CatGold_Base

  # 3) 生成 Go 协议骨架 (含类内 result 常量)
  python3 scripts/protocol/protodict.py codegen docs/protocol/actions.json --module 707 --action 2

FFDec 导出命令 (版本见 com/Config.as GameVersion):
  java -jar ffdec.jar -cli -selectclass 'com.protocols.*' \\
       -export script OUT_DIR Main.swf
"""
from __future__ import annotations

import argparse
import json
import re
import sys
from pathlib import Path

ACTIONS_RE = re.compile(
    r'public\s+static\s+const\s+(\w+):Object\s*=\s*\{(.*?)\};', re.S)
CONST_RE = re.compile(
    r'public\s+static\s+const\s+(\w+):(?:int|uint)\s*=\s*(-?\d+);')
FIELD_RE = re.compile(r'"(\w+)"\s*:\s*')
TYPE_RE = re.compile(r'Utils\.(\w+Util)')

# AS3 线类型 -> Go 类型
GO_TYPE = {
    'ByteUtil': 'int8',
    'UByteUtil': 'uint8',
    'ShortUtil': 'int16',
    'UShortUtil': 'uint16',
    'IntUtil': 'int32',
    'UIntUtil': 'uint32',
    'LongUtil': 'int64',
    'ULongUtil': 'uint64',
    'StringUtil': 'string',
    'BoolUtil': 'bool',
}


def read_value(text: str, pos: int) -> str:
    """Read a literal value at pos, honouring bracket nesting."""
    if pos >= len(text):
        return ''
    if text[pos] == '[':
        depth = 0
        for i in range(pos, len(text)):
            if text[i] == '[':
                depth += 1
            elif text[i] == ']':
                depth -= 1
                if depth == 0:
                    return text[pos:i + 1]
        return text[pos:]
    end = pos
    while end < len(text) and text[end] not in ',}':
        end += 1
    return text[pos:end].strip()


def parse_fields(body: str) -> dict:
    out = {}
    for m in FIELD_RE.finditer(body):
        key = m.group(1)
        if key not in out:
            out[key] = read_value(body, m.end())
    return out


def split_top(expr: str) -> list[str]:
    """Split 'a,b,[c,d],e' on top-level commas."""
    parts, depth, cur = [], 0, []
    for ch in expr:
        if ch == '[':
            depth += 1
            cur.append(ch)
        elif ch == ']':
            depth -= 1
            cur.append(ch)
        elif ch == ',' and depth == 0:
            parts.append(''.join(cur).strip())
            cur = []
        else:
            cur.append(ch)
    if cur:
        parts.append(''.join(cur).strip())
    return [p for p in parts if p]


def parse_schema(expr: str) -> list[dict]:
    """Parse '[Utils.IntUtil,Utils.ByteUtil,[Utils.IntUtil]]' into field AST.

    Each field is {'t': <wire type>} or {'t': 'array', 'fields': [...]}
    where nested arrays denote a repeated composite item.
    """
    expr = expr.strip()
    if not expr.startswith('['):
        return []
    inner = expr[1:-1].strip()
    if not inner:
        return []
    out = []
    for tok in split_top(inner):
        m = TYPE_RE.fullmatch(tok)
        if m:
            out.append({'t': m.group(1)})
        elif tok.startswith('['):
            out.append({'t': 'array', 'fields': parse_schema(tok)})
        else:
            out.append({'t': tok})
    return out


def flat_types(ast: list[dict]) -> list[str]:
    """Flatten AST to ordered leaf wire types (backwards-compatible view)."""
    out = []
    for f in ast:
        if f['t'] == 'array':
            out.extend(flat_types(f.get('fields', [])))
        else:
            out.append(f['t'])
    return out


def ast_to_str(ast: list[dict]) -> str:
    """Compact schema encoding: '[IntUtil,[ByteUtil,ByteUtil]]'."""
    parts = []
    for f in ast:
        if f['t'] == 'array':
            parts.append(ast_to_str(f.get('fields', [])))
        else:
            parts.append(f['t'])
    return '[' + ','.join(parts) + ']'


def build(directory: Path, out_path: Path) -> None:
    classes: dict[str, dict] = {}
    n_actions = 0
    files = sorted(directory.rglob('Mod_*_Base.as'))
    for f in files:
        text = f.read_text(encoding='utf-8', errors='replace')
        consts = {name: int(val) for name, val in CONST_RE.findall(text)}
        acts = []
        module = None
        for m in ACTIONS_RE.finditer(text):
            name, body = m.group(1), m.group(2)
            fields = parse_fields(body)
            if 'module' not in fields or 'action' not in fields:
                continue
            module = int(fields['module'])
            req_ast = parse_schema(fields.get('request', ''))
            resp_ast = parse_schema(fields.get('response', ''))
            acts.append({
                'action': name,
                'act': int(fields['action']),
                'req': ast_to_str(req_ast),
                'resp': ast_to_str(resp_ast),
            })
        if not acts:
            continue
        acts.sort(key=lambda a: a['act'])
        classes[f.stem] = {'module': module, 'consts': consts, 'actions': acts}
        n_actions += len(acts)
    modules = {c['module'] for c in classes.values()}
    payload = {
        'meta': {
            'source': 'Main.swf (com.protocols, FFDec script export)',
            'note': '由 scripts/protocol/protodict.py build 生成；'
                    '版本以客户端 com/Config.as GameVersion 为准',
        },
        'classes': classes,
    }
    out_path.parent.mkdir(parents=True, exist_ok=True)
    out_path.write_text(json.dumps(payload, ensure_ascii=False, indent=1),
                        encoding='utf-8')
    print(f'parsed {len(files)} classes -> {len(classes)} protocol classes, '
          f'{n_actions} actions across {len(modules)} modules -> {out_path}')


def fmt_ast(ast: list[dict]) -> str:
    def one(f: dict) -> str:
        if f['t'] == 'array':
            return '[' + ', '.join(one(x) for x in f.get('fields', [])) + ']'
        return GO_TYPE.get(f['t'], f['t'])
    return '[' + ', '.join(one(f) for f in ast) + ']'


def load(path: Path) -> list[dict]:
    """Load dictionary and flatten per-class entries into action rows."""
    data = json.loads(path.read_text(encoding='utf-8'))
    rows = []
    for cls, info in data['classes'].items():
        for a in info['actions']:
            rows.append({
                **a,
                'class': cls,
                'module': info['module'],
                'consts': info.get('consts', {}),
                'request_ast': parse_schema(a['req']),
                'response_ast': parse_schema(a['resp']),
            })
    return rows


def cmd_lookup(args) -> None:
    acts = load(args.dictionary)
    rows = acts
    if args.module is not None:
        rows = [a for a in rows if a['module'] == args.module]
    if args.action is not None:
        pat = re.compile(args.action, re.I)
        rows = [a for a in rows if pat.search(a['action'])]
    if args.klass is not None:
        pat = re.compile(args.klass, re.I)
        rows = [a for a in rows if pat.search(a['class'])]
    if args.ingot:
        rows = [a for a in rows
                if 'ingot' in a['class'].lower()
                or 'ingot' in a['action'].lower()
                or 'ingot' in json.dumps(a['consts'], ensure_ascii=False).lower()]

    if not rows:
        print('no matches')
        return
    print(f'{len(rows)} action(s):')
    for a in rows:
        print(f"  {a['class']:34s} {a['action']:42s} "
              f"m{a['module']:>4}/a{a['act']:<3} "
              f"(0x{(a['module'] << 16) | a['act']:08X})")
        print(f"    request : {fmt_ast(a['request_ast'])}")
        print(f"    response: {fmt_ast(a['response_ast'])}")
        if args.consts and a['consts']:
            vals = ', '.join(f'{k}={v}' for k, v in sorted(a['consts'].items()))
            print(f"    consts  : {vals}")


def pascal(name: str) -> str:
    """CatGold_Store -> CatGoldStore, preserving inner capitalisation."""
    return ''.join(p[:1].upper() + p[1:]
                   for p in re.split(r'[_\W]+', name) if p)


def go_type_of(f: dict, prefix: str, depth: int, emitted: list) -> str:
    if f['t'] != 'array':
        return GO_TYPE.get(f['t'], f['t'])
    item_fields = f.get('fields', [])
    # emit a named struct for composite items; plain types become slices
    if all(x['t'] != 'array' for x in item_fields):
        name = f'{prefix}Item{depth}'
        lines = [f'// {name} 数组元素']
        lines.append(f'type {name} struct {{')
        for i, x in enumerate(item_fields, 1):
            lines.append(f'\tArg{i} {GO_TYPE.get(x["t"], x["t"])}')
        lines.append('}')
        emitted.append('\n'.join(lines))
        return f'[]{name}'
    inner = ', '.join(go_type_of(x, prefix, depth + 1, emitted) for x in item_fields)
    return f'[]{inner}'


def emit_struct(name: str, ast: list[dict], emitted: list) -> str:
    lines = [f'type {name} struct {{']
    for i, f in enumerate(ast, 1):
        lines.append(f'\tArg{i} {go_type_of(f, name, i, emitted)}')
    lines.append('}')
    return '\n'.join(lines)


def cmd_codegen(args) -> None:
    acts = load(args.dictionary)
    match = [a for a in acts
             if a['module'] == args.module and a['act'] == args.action]
    if not match:
        sys.exit(f'no action at module {args.module} action {args.action}')
    a = match[0]
    cls = a['class'][len('Mod_'):-len('_Base')] if a['class'].startswith('Mod_') else a['class']
    base = pascal(cls + '_' + a['action'])
    mod_const = f'Module{pascal(cls)}'
    action_const = f'ActionID{pascal(cls + "_" + a["action"])}'

    emitted: list[str] = []
    req_struct = emit_struct(base + 'Request', a['request_ast'], emitted)
    resp_struct = emit_struct(base + 'Response', a['response_ast'], emitted)

    lines = [
        '// 本文件由 scripts/protocol/protodict.py codegen 生成骨架，',
        '// 字段语义需对照视图层调用点补齐 (Arg1/Arg2 → 真实字段名)。',
        'package protocol',
        '',
        'import "errors"',
        '',
        '// 模块与动作 (自动生成)',
        'const (',
        f'\t{mod_const} uint16 = {a["module"]}',
        f'\t{action_const} uint32 = 0x{(a["module"] << 16) | a["act"]:08X}',
        ')',
        '',
    ]
    if a['consts']:
        lines.append(f'// {a["class"]} 结果码与枚举常量 (自动生成)')
        lines.append('const (')
        for k, v in a['consts'].items():
            lines.append(f'\t{pascal(cls)}{pascal(k.lower())} int32 = {v}')
        lines.append(')')
        lines.append('')
    lines.append(f'// {base}Request {a["class"]}.{a["action"]} 请求体')
    lines.append(req_struct)
    lines.append('')
    lines.append(f'// {base}Response {a["class"]}.{a["action"]} 响应体')
    lines.append(resp_struct)
    if emitted:
        lines.append('')
        lines.append('// 复合数组元素类型')
        lines.extend(emitted)
    lines.append('')
    lines.append(f'// Err{base}Result 非成功结果码的语义错误 (ADR-0015)')
    lines.append(f'var Err{base}Result = errors.New("{a["class"]}.{a["action"]} failed")')
    lines.append('')
    print('\n'.join(lines))


def main() -> None:
    ap = argparse.ArgumentParser(description=__doc__,
                                 formatter_class=argparse.RawDescriptionHelpFormatter)
    sub = ap.add_subparsers(dest='cmd', required=True)

    b = sub.add_parser('build', help='从反编译目录重建字典')
    b.add_argument('directory', type=Path)
    b.add_argument('out', type=Path)
    b.set_defaults(func=lambda a: build(a.directory, a.out))

    q = sub.add_parser('lookup', help='查询动作签名')
    q.add_argument('dictionary', type=Path)
    q.add_argument('--module', type=int)
    q.add_argument('--action', help='动作名正则 (不区分大小写)')
    q.add_argument('--class', dest='klass', help='类名正则')
    q.add_argument('--ingot', action='store_true', help='仅元宝相关动作')
    q.add_argument('--consts', action='store_true', help='显示类内常量')
    q.set_defaults(func=cmd_lookup)

    g = sub.add_parser('codegen', help='生成 Go 协议骨架')
    g.add_argument('dictionary', type=Path)
    g.add_argument('--module', type=int, required=True)
    g.add_argument('--action', type=int, required=True)
    g.set_defaults(func=cmd_codegen)

    args = ap.parse_args()
    args.func(args)


if __name__ == '__main__':
    main()