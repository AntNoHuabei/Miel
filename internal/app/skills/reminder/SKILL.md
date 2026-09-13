---
name: reminder
description: 查询逾期或即将到期事项，以及读取和修改提醒设置。
---

# Reminder

使用 `skill_run` 执行本 Skill。固定传入 `skill: "reminder"`，`command` 是下列子命令，所有 flag 和值必须拆成独立的 `args` 数组元素。不得拼接 shell 命令。

## Commands

- `upcoming [--lead-hours <hours>]`
- `settings get`
- `settings set [--enabled true|false] [--lead-hours <hours>]`

调用嵌套命令时，例如读取设置：

```json
{"skill":"reminder","command":"settings","args":["get"]}
```

`lead-hours` 必须是正整数。以 stdout 的 JSON `data` 为真实结果；`exitCode` 非 0 时根据 stderr 修正参数，不得编造提醒或设置状态。
