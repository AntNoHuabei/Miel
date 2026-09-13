---
name: office
description: 基于真实待办和操作记录生成周报、文档、表格或导出文件。
---

# Office

使用 `skill_run` 执行本 Skill。固定传入 `skill: "office"`，`command` 是下列子命令，所有 flag 和值必须拆成独立的 `args` 数组元素。不得拼接 shell 命令。

## Commands

- `weekly [--since-days 7]`
- `document --title <text> --content <markdown>`
- `table --title <text> --csv <csv>`
- `export-todos [--format md|csv]`

示例：

```json
{"skill":"office","command":"document","args":["--title","会议纪要","--content","# 会议纪要\n\n正文"]}
```

周报必须通过 `weekly` 从真实数据生成。文档和表格正文作为单个 args 元素传入，即使其中包含空格、换行或标点。以 stdout 的 JSON `data.path` 为实际保存路径；`exitCode` 非 0 时不得声称文件已生成。
