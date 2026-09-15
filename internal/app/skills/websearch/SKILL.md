---
name: websearch
description: 使用多个中文搜索引擎检索公开网页，并按需提取已确认公网网页的正文。
---

# Web Search

使用 `skill_run` 执行本 Skill。固定传入 `skill: "websearch"`，所有 flag 和值必须作为独立的 `args` 数组元素传入。不得拼接 shell 命令、位置参数或未声明 flag。

## Commands

- `search --query <text> [--limit 1..10]`
- `fetch --url <http(s)-url>`

示例：

```json
{"skill":"websearch","command":"search","args":["--query","Miel GitHub 项目","--limit","5"]}
```

首次联网请求需要用户批准。`search` 返回标题、URL、摘要和来源引擎；需要网页正文时，先根据搜索结果选择 URL，再调用 `fetch`。`fetch` 不会跟随重定向，也不会访问本机、私网或本地域名。`exitCode` 非 0 时不要声称已获取搜索结果或网页内容。
