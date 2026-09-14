# Miel

Miel 是一个面向 Windows 的本地优先 AI 办公 Agent。它通过对话、工具和 Skill 协助管理待办、处理截图、生成文档与表格，并将会话和业务数据保存在本机。

项目基于 Wails 3、Go、React、TypeScript、Vite、Ant Design 和 SQLite 构建。

## 主要能力

- **对话 Agent**：支持流式响应、多轮会话、工具调用、会话历史和 KV Cache 复用。
- **工作区工具**：可列出目录、读写文件、执行命令和访问网页；相对路径始终以当前选择的工作区为基准。
- **权限控制**：区分工作区内外操作，支持临时审批和不同授权模式，工作区外访问仍按规则确认。
- **模型接入**：内置 DeepSeek、OpenAI、OpenRouter、火山方舟 Agent Plan、通义千问、Moonshot、Herdsman 和 Ollama 模板，也支持任意 OpenAI 兼容服务。
- **模型能力配置**：支持模型级多模态能力和思考级别配置。
- **待办与里程碑**：支持手动、对话、截图和粘贴板来源，并保留原文或原图以便追溯。
- **快捷助手**：使用 `Alt+S` 打开浮动对话，使用 `Alt+T` 从粘贴板生成待办。
- **截图处理**：默认使用 `Ctrl+Alt+S` 截图，可转待办、进行视觉问答或仅保存到本地。
- **办公产物**：可生成周报、Markdown 文档、CSV 表格，并导出待办。
- **长期记忆**：基于 SQLite Memory 提供跨会话召回、自动提取、自定义提示词和本地管理。
- **Skill 扩展**：从本地目录、ZIP 或 SkillHub 导入 Skill，可单独启用和停用。
- **桌面集成**：支持系统托盘、全局快捷键、原生窗口主题和单实例运行。

## 快速开始

### 环境要求

- Windows 10 或 Windows 11
- WebView2 Runtime
- Go 1.25 或更高版本
- Node.js 和 npm
- MinGW-w64 GCC，且 `go env CC` 能找到 `gcc.exe`
- Wails 3 CLI

### 获取源码

```powershell
git clone https://github.com/AntNoHuabei/Miel.git
cd Miel
```

### 开发与构建

```powershell
wails3 task dev       # 启动前后端开发模式
wails3 task build     # 构建 Windows 应用到 bin/miel.exe
wails3 task package   # 生成 NSIS 安装包，可切换为 MSIX
```

长期记忆依赖 `github.com/mattn/go-sqlite3`，因此 Go 测试和 Windows 构建必须启用 CGO。

```powershell
$env:CGO_ENABLED = '1'
go test ./...

cd frontend
npm install
npm run test:run
npm run build
```

Wails 后端接口发生变化后，应在仓库根目录重新生成前端绑定：

```powershell
wails3 generate bindings -ts -d frontend/bindings
```

## 数据与兼容性

全新安装默认使用 `%LOCALAPPDATA%\Miel`。如果升级前的 `%LOCALAPPDATA%\BlankMind` 已存在，且 `%LOCALAPPDATA%\Miel` 尚未创建，Miel 会继续使用旧目录，不移动或复制用户数据。

为保证升级兼容，部分内部文件名和持久化键仍保留 `blankmind` 前缀。

| 路径 | 内容 |
| --- | --- |
| `blankmind.db` | 服务商、待办、事件、会话、消息、截图和设置 |
| `agui.db` | Agent 会话与 AG-UI 消息轨迹 |
| `memory.db` | 跨会话长期记忆 |
| `logs/blankmind.log` | 应用日志 |
| `screenshots/` | 截图原图 |
| `sources/clipboard/` | 待办关联的粘贴板图片来源 |
| `attachments/chat/` | 对话附件、草稿和缩略图 |
| `outputs/reports/` | 周报 |
| `outputs/documents/` | Markdown 文档 |
| `outputs/tables/` | CSV 表格和待办导出 |
| `outputs/memories/` | 记忆导出文件 |
| `skills/` | 已安装的用户 Skill |

所有持久化路径由 `internal/app/directory_manager.go` 统一管理。新增数据库、日志、附件或产物类型时，应先扩展 `DirectoryManager`，避免在业务服务中直接拼接应用数据目录。

## Skill 目录

每个 Skill 使用独立目录，并以 `SKILL.md` 作为入口：

```text
skills/
└── example-skill/
    └── SKILL.md
```

Miel 会从数据目录加载已启用的 Skill。内置 Skill 的命令执行范围受后端白名单限制。

## 项目结构

```text
.
├── internal/app/        Go 业务服务、Agent、工具、权限和持久化
├── internal/capture/    Windows 截图实现
├── internal/credential/ 系统凭据存储
├── frontend/src/        React 界面与前端状态
├── frontend/bindings/   Wails 生成的 TypeScript 绑定
├── build/windows/       Windows 清单、安装包和资源配置
├── main.go              Wails 应用装配与窗口创建
└── Taskfile.yml         开发、构建和打包任务
```

## 平台说明

- 当前仅维护 Windows 构建。
- 视觉功能需要为对应模型启用多模态能力。
- 火山方舟 Agent Plan 必须使用套餐专属地址 `https://ark.cn-beijing.volces.com/api/plan/v3`。
