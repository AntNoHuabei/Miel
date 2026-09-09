---
name: todo
description: 待办与里程碑管理。当用户表达任务、安排、待办、里程碑,或询问进度/统计时使用。
---

# todo — 待办与里程碑管理(子命令式)

用户的话术通常形如:`todo add <事项>`、`todo list`、`todo done <id>`、`todo del <id>`、`todo stats`。把它们映射到下列 subcommand(工具)执行:

| 子命令 | 触发话术示例 | 工具 |
| --- | --- | --- |
| `todo add` | "todo add 写季度方案,3月15日截止,里程碑" | create_todo(含 deadline 与 milestone) |
| `todo list` | "todo list""我有哪些待办" | list_todos(含状态/截止/里程碑) |
| `todo set <id> <状态>` | "把 3 号待办标为进行中/完成" | set_todo_status(pending/doing/done) |
| `todo del <id>` | "删掉 3 号待办" | delete_todo |
| `todo stats` | "待办统计" | todo_stats(逾期/24h到期/里程碑数) |

规则:创建带截止时间的待办时,若用户没给具体日期要追问;里程碑(重要节点)记得置 milestone=true。操作类请求务必先 list 确认 id 再 set/delete。
