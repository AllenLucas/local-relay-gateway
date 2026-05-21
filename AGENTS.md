# AGENTS.md

本文件面向后续接手本仓库的 AI agent，也面向未来间隔很久之后再次维护本项目的你。

目标不是服务人工阅读排版，而是让 AI 在尽量少的上下文和 token 消耗下，快速恢复项目状态、理解实现边界、避免重复建设和冲突修改。

## 1. 项目定位

这是一个本地优先的 Go 项目，用于把多个上游大模型中转站统一收敛成一个稳定的本地网关。

当前支持两类本地兼容入口：

- OpenAI 兼容入口
  - `/openai/v1/responses`
  - `/openai/v1/chat/completions`
- Anthropic 兼容入口
  - `/anthropic/v1/messages`

项目目标：

- 给 Codex CLI、Claude Code 和其他本地脚本提供稳定单一入口
- 屏蔽具体上游中转站差异
- 支持模型别名映射
- 支持多站点优先级、失败切换、冷却和健康检查
- 提供本地网页管理界面
- 低资源占用，适合长期本地常驻

## 2. AI 优先规则

本仓库的维护原则优先考虑 AI 的理解效率，而不是人的阅读美观。

强制规则：

- 优先保持实现边界清晰、职责单一、命名稳定
- 优先减少隐式行为、魔法分支、无说明的兼容逻辑
- 优先增加对 AI 有帮助的结构化说明，而不是增加面向人的修辞性注释
- 如果能用更少的上下文理解代码，就优先那种实现
- 如果能减少后续模型重复扫描大文件，就优先拆分职责
- 不要为了“优雅”做会增加理解成本的抽象
- 不要引入无必要的框架、生成代码、复杂元编程或过度封装

注释规则：

- 注释只保留对 AI 或未来维护者有信息增益的内容
- 不写解释语法层面的废话注释
- 兼容性补丁、协议绕过、上游特殊行为必须说明原因

## 3. 长期维护规则

这个项目可能长期不改动，过很久才会重新维护。

因此后续 agent 在开始任何功能修改、重构或修复前，必须先完成以下检查：

1. 先阅读：
   - `README.md`
   - `docs/usage.md`
   - 本文件 `AGENTS.md`
2. 再确认：
   - 当前入口
   - 当前协议兼容行为
   - 当前 Admin 页面能力
   - 当前测试覆盖的行为
3. 再判断新需求是否已经被现有实现覆盖，或者是否和现有能力冲突

如果后续新增功能与已有实现重复、交叉、冲突或会导致语义重叠，必须优先提醒用户，不能直接叠加实现。

必须优先提醒用户的情况包括：

- 已有功能只是用户忘记了
- 新需求和当前实现只有命名不同，本质重复
- 新改动会造成双通路、双配置源、双状态模型
- 新增兼容逻辑会覆盖或破坏已有兼容逻辑
- 新增页面、字段、接口和现有数据模型职责冲突

默认原则：

- 先复用，再扩展
- 先提醒冲突，再改代码
- 不制造第二套并行实现

## 4. 仓库地图

关键目录和职责：

- `cmd/local-relay-gateway/main.go`
  - 进程入口
  - 负责加载启动配置、初始化存储、启动后台任务和 HTTP 服务

- `internal/config/`
  - 环境变量运行时配置
  - Windows 文件引导运行时配置
  - `runtime.json` 读写

- `internal/gateway/`
  - 本地协议入口
  - 本地鉴权
  - 请求路由到候选上游
  - 失败切换
  - 请求日志和切换事件记录

- `internal/protocol/openai/`
  - OpenAI 兼容请求标准化与上游请求构造

- `internal/protocol/anthropic/`
  - Anthropic 兼容请求标准化与上游请求构造
  - 这里承载协议兼容补丁，后续改动要格外谨慎

- `internal/routing/`
  - 站点候选选择
  - 按协议、映射、优先级、状态过滤

- `internal/jobs/`
  - 后台健康检查
  - 日志清理

- `internal/storage/sqlite/`
  - SQLite schema 与持久化实现

- `internal/admin/`
  - 本地管理页面
  - 运行时配置、站点、映射、状态、日志

- `docs/usage.md`
  - 操作手册

- `README.md`
  - 对外说明

## 5. 当前关键行为

后续改动前应默认保留这些行为，除非用户明确要求改变：

- OpenAI 本地入口使用 `Authorization: Bearer <local key>`
- Anthropic 本地入口兼容：
  - `x-api-key`
  - `Authorization: Bearer <local key>`
- 站点可以是：
  - `OpenAI-only`
  - `Anthropic-only`
  - 双协议
- 每种协议的 `base_url` 和 `api_key` 必须成对出现
- 路由只会选择当前协议真正可用的站点
- Anthropic 转发前会去掉部分上游不兼容字段，例如 `context_management`
- Admin 页面支持：
  - 首次 `/admin/setup` 保存本地 `Local API Key`
  - `/admin/runtime` 查看本地入口、显示/复制/修改本地 key
  - 新增站点
  - 编辑站点
  - 映射管理
  - 状态查看
  - 日志查看
- Windows 无运行时环境变量启动时：
  - 配置文件是 `%LocalAppData%\LocalRelayGateway\runtime.json`
  - 默认数据库是 `%LocalAppData%\LocalRelayGateway\local-relay-gateway.db`
  - 首次自动打开 `/admin/setup`
  - 后续自动打开 `/admin/stations`
- 环境变量启动时：
  - `LRG_LOCAL_API_KEY`
  - `LRG_LISTEN_ADDR`
  - `LRG_DB_PATH`
  - 仍然是开发/脚本优先入口
- `/admin/runtime` 只管理本地网关身份和本地客户端入口，不管理上游站点
- `/admin/stations` 仍然是唯一的上游站点配置来源
- `/admin/runtime` 修改本地 key 后需要重启生效，不做热更新

## 6. 修改优先级

若用户需求与以下原则冲突，优先级从高到低如下：

1. 不泄露本地敏感信息
2. 不引入重复实现
3. 不破坏既有协议兼容
4. 不破坏已有测试覆盖的核心行为
5. 优先让 AI 更容易恢复上下文
6. 最后才考虑人工排版与形式美观

## 7. Git 与仓库规则

本项目未来准备公开到 GitHub。

因此默认规则：

- 不提交本地缓存
- 不提交本地数据库
- 不提交临时测试产物
- 不提交过程性规划/草稿文档
- 不提交任何本地账户、token、路径、历史记录、会话文件

应被忽略或移除的典型内容：

- `.gocache/`
- `.gomodcache/`
- `local-relay-gateway.db`
- `docs/superpowers/`
- 二进制产物
- 系统临时文件

如果发现新的本地状态文件、日志、缓存或会话痕迹，应优先补 `.gitignore`。

## 8. 变更前检查

开始修改前，agent 应优先回答这些问题：

- 这项需求是否已经实现过？
- 是否只是已有功能的别名或包装？
- 是否会和现有字段、页面、接口、数据流重叠？
- 是否会影响 OpenAI 和 Anthropic 双协议兼容？
- 是否需要同步文档？
- 是否需要补测试防止未来回归？

如果其中任一项答案不明确，应先提醒用户，再实施修改。

## 9. 常用命令

Windows:

```powershell
# 正式双击入口：编译后直接运行 local-relay-gateway.exe
# 首次无环境变量时会使用 %LocalAppData%\LocalRelayGateway\runtime.json

$env:LRG_LOCAL_API_KEY="replace-this-key"
$env:LRG_LISTEN_ADDR="127.0.0.1:8787"
$env:LRG_DB_PATH=".\local-relay-gateway.db"
& "C:\Program Files\Go\bin\go.exe" run .\cmd\local-relay-gateway
& "C:\Program Files\Go\bin\go.exe" test ./...
```

Linux / macOS:

```bash
export LRG_LOCAL_API_KEY="replace-this-key"
export LRG_LISTEN_ADDR="127.0.0.1:8787"
export LRG_DB_PATH="./local-relay-gateway.db"
go run ./cmd/local-relay-gateway
go test ./...
```

## 10. 文档同步规则

以下内容发生变化时，必须同步更新 `README.md` 和 `docs/usage.md`：

- 启动方式
- 环境变量
- 管理页面能力
- 协议兼容行为
- 鉴权方式
- 站点配置规则
- 客户端接入方式

## 11. 最终目标

这个仓库应长期保持：

- 对 AI 低上下文成本
- 对未来维护低恢复成本
- 对 GitHub 公开发布安全
- 对本地长期使用稳定
