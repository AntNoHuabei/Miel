---
name: vocabulary
description: 收藏、查询和整理生词，并从真实生词本随机抽词进行组句复习。
---

# Vocabulary

使用 `skill_run` 执行本 Skill。固定传入 `skill: "vocabulary"`，`command` 是下列子命令，所有 flag 和值必须拆成独立的 `args` 数组元素。不得拼接 shell 命令。

## Commands

- `add --term <text> [--meaning <text>] [--example <sentence>]`
- `add-many --items <json-array>`
- `list [--query <text>]`
- `get --id <id>`
- `update --id <id> [--term <text>] [--meaning <text>] [--example <sentence>]`
- `delete --id <id>`
- `random [--count 1..50]`
- `review [--count 1..8]`
- `sentence --ids <id,id,...>`
- `grade --ids <id,id,...> --result mastered|again`
- `stats`

需要无偏随机抽取词条时使用 `random`；组句复习使用 `review`，它会优先选择尚未复习的词。抽取后让用户自行组句，用户要求参考句时才调用 `sentence`。用户确认掌握情况后再调用 `grade`，不得自行判定。修改或删除前必须先用 `get` 或 `list` 核对 ID。

批量添加时，把 JSON 数组作为 `--items` 后的单个 args 元素传入；每项格式为 `{"term":"word","meaning":"释义","example":"例句"}`，一次最多 100 条。整批会先校验再以事务写入，任一条无效则全部不写入。

`add`、`add-many` 和 `update` 保存词语时会自动去除英文字母与空白之外的字符；清洗后没有英文字母会返回错误。

快捷划词由应用全局快捷键 `Alt+W` 完成。以 stdout 的 JSON `data` 为真实结果；`exitCode` 非 0 时根据 stderr 修正参数，不得声称操作成功。
