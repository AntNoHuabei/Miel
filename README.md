# 🧠 BlankMind — 本地办公 Agent

一个本地优先的 AI 办公助手桌面应用:**免登录、数据全在本机、托盘后台常驻**,通过对话 + 工具 + Skill 完成待办管理、截图处理、周报生成与文档/表格产出。

技术栈:Wails3(Go)+ React 18 + TypeScript + Vite + Ant Design + SQLite(modernc.org/sqlite,纯 Go)。

## ✨ 功能

- **对话 Agent 中枢**:基于 trpc-agent-go,function calling 驱动;流式输出、多轮会话、历史可回溯。
- **模型服务商灵活接入**:内置 DeepSeek / OpenAI / 通义千问 / Moonshot / Ollama 模板,支持任意 OpenAI 兼容的自定义服务商;默认未配置时用全屏向导引导(首次启动必经)。
- **待办与里程碑**:手动/对话/截图三种来源;勾选完成、截止时间、里程碑标记、逾期与 24h 内到期统计。
- **里程碑倒计时**:未完成里程碑以卡片呈现剩余天数/进度,逾期红色预警。
- **双通道提醒**:系统通知 + 应用内提醒中心/角标(每分钟自动扫描,托盘态也能收到)。
- **全局截图**:默认 `Ctrl+Alt+S`(设置可改)任意时刻截屏 → 弹出处理菜单:
  - **转待办**:视觉模型提取任务 → 勾选确认 → 入库(需多模态模型)
  - **问答**:针对截图内容提问/总结/翻译
  - **仅保存**:落盘到截图库并记备注
- **办公生成**(对话内一句话触发):
  - 周报 `generate_weekly_report`:聚合一周完成/新增/里程碑/逾期/操作流水,自动落盘
  - 文档 `create_document`(markdown)、表格 `create_table`(CSV)、待办导出 `export_todos`(md/csv 带 BOM)
- **Skill 扩展**:数据目录 `skills/<name>/SKILL.md` 即插即用(trpc-agent-go 加载),无需改代码。
- **皮肤系统**:内置明亮 / 暗夜 / 护眼绿 / 极客紫,设置页一键热切换并持久化。
- **后台常驻**:关窗进系统托盘(显示/隐藏/退出),数据目录可从设置页一键打开。

## 🚀 开发与运行

```bash
wails3 dev        # 开发模式(前后端热重载)
wails3 build      # 生产构建(需系统 WebView2 与平台编译链)
```

前端单独调试:

```bash
cd frontend
npm install
npm run build     # tsc + vite 生产打包
```

> 本机若无 C 编译链/WebView2,可用 `go build .` 验证后端编译,前端用 `npm run build` 验证。

## 🗂 数据目录(Windows:`%LOCALAPPDATA%\BlankMind`)

| 路径 | 内容 |
| --- | --- |
| `blankmind.db` | SQLite:providers / todos / events / conversations / messages / screenshots / settings |
| `screenshots/` | 截图原图 |
| `outputs/reports/` | 生成的周报(markdown) |
| `outputs/documents/` | 生成的文档(markdown) |
| `outputs/tables/` | 生成的表格(CSV)与待办导出 |
| `skills/` | 用户 Skill 目录(`<name>/SKILL.md`) |

## 🧩 添加自定义 Skill

在数据目录 `skills/` 下新建目录与 `SKILL.md`,对话中即可被 Agent 按需加载调用(格式遵循 trpc-agent-go skill 约定)。

## 🏗 项目结构(Go 侧)

| 文件 | 职责 |
| --- | --- |
| `main.go` | 装配:服务注册 / 托盘 / 全局热键 / 事件广播 |
| `storage.go` | SQLite 打开与建表迁移 |
| `settings_service.go` | Provider CRUD / 联通测试 / 应用设置 |
| `todo_service.go` | 待办 CRUD / 状态 / 统计 / 事件日志 |
| `agent_service.go` | 对话 Agent(llmagent)+ 会话持久化 |
| `agent_tools.go` | 待办类 function-calling 工具 |
| `office_tools.go` | 周报/文档/表格/导出办公工具 |
| `screenshot_service.go` | 截屏 / 视觉提取转待办 / 截图问答 |
| `capture_windows.go` | Windows GDI 截屏(纯 syscall) |
| `reminder_service.go` | 每分钟 deadline 扫描提醒引擎 |
| `system_service.go` | 系统托盘与全局快捷键注册 |
| `outputs.go` | 产物目录与“打开数据目录” |
| `model_client.go` | OpenAI 兼容模型客户端构建 / Ping |

## 📌 说明

- 全局热键与截屏当前以 Windows 为主实现;Linux/macOS 截屏为占位(需按平台补实现)。
- 视觉能力(截图转待办/问答)依赖配置**多模态**模型(设置中勾选"多模态")。
