---
name: office
description: 办公产出:周报、文档、表格与数据导出。当用户要生成周报、纪要、方案、表格、导出待办时使用。
---

# office — 办公产出(子命令式)

用户话术形如:`office weekly`、`office doc <标题>`、`office table <标题>`、`office export todos`。映射到 subcommand(工具):

| 子命令 | 触发话术示例 | 工具 |
| --- | --- | --- |
| `office weekly` | "帮我生成这周周报" | generate_weekly_report(聚合本周完成/新增/里程碑/逾期/操作流水,存 md) |
| `office doc` | "写一份会议纪要/方案(内容…)" | create_document(title + markdown body,存 md) |
| `office table` | "做一个排期/预算表(给出 CSV)" | create_table(title + CSV,存 csv) |
| `office export` | "把待办导出" | export_todos(format=md/csv) |
| `office events` | "最近 7 天我做了什么" | list_events(sinceDays) |

规则:周报必须基于 list_events / todo_stats / generate_weekly_report 的真实记录;产物自动保存到应用数据目录 outputs/ 下,回复中给出保存路径与预览。
