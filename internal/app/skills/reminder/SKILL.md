---
name: reminder
description: 到期提醒管理。当用户询问即将到期/逾期事项,或要调整提醒开关、提前量时使用。
---

# reminder — 到期提醒管理(子命令式)

提醒引擎每分钟自动扫描未完成待办的截止时间,双通道触达(系统通知 + 应用内提醒中心)。用户话术形如:`reminder list`、`reminder upcoming`、`reminder enable/disable`、`reminder lead 提前2小时`。映射到 subcommand(工具):

| 子命令 | 触发话术示例 | 工具 |
| --- | --- | --- |
| `reminder list` | "reminder upcoming""最近有什么到期/逾期" | reminder_upcoming(逾期 + 提前量内即将到期) |
| `reminder set` | "把提醒关掉/打开""提前 1 天提醒" | reminder_settings(enabled、leadHours) |

规则:leadHours 为整数小时(默认 24);enabled 取值 on/off。查询结果基于真实待办数据,不要编造。
