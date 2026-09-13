---
name: todo
description: 待办、任务、里程碑、进度、统计和操作记录管理。
---

# Todo

使用 `skill_run` 执行本 Skill。固定传入 `skill: "todo"`，`command` 是下列子命令，所有 flag 和值必须拆成独立的 `args` 数组元素。不得拼接 shell 命令。

## Commands

- `add --title <text> [--description <text>] [--deadline <unix-seconds>] [--milestone]`
- `list [--status pending|doing|done]`
- `get --id <id>`
- `status --id <id> --status pending|doing|done`
- `delete --id <id>`
- `stats`
- `events [--since-days 7]`

示例：

```json
{"skill":"todo","command":"add","args":["--title","写季度方案","--deadline","1789401600","--milestone"]}
```

创建带截止时间的待办时，用户未给具体日期就先追问。修改或删除前必须先用 `get` 或 `list` 核对 ID。以 stdout 的 JSON `data` 为真实结果；`exitCode` 非 0 时根据 stderr 修正参数，不得声称操作成功。
