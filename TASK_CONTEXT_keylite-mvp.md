# TASK_CONTEXT — Keylite MVP 无人值守实施

> 本文件是长任务的**断点续传锚点**。会话被压缩/中断后，从这里恢复上下文。
> 任务全部完成后删除本文件。

## 项目定位

- 位置：`~/workspace/github/keylite`（独立 git 仓库，**未推送任何远端**）
- 名称：Keylite（已定，暂不改名）
- 形态：Go 单体后端 + React 前端，`go:embed` 打成单一二进制
- License：Apache-2.0 + DCO（`git commit -s`）
- 需求真相源：`docs/requirements/mvp-requirements.md`
- 自有规范：`docs/api-conventions.md`、`docs/design-principles.md`

## 已确认决策（用户 2026-08-06 确认，全部按推荐方案）

| # | 决策项 | 结论 |
|---|---|---|
| 1 | Go 工具链 | ✅ 已装 go 1.26.5 + golangci-lint 2.12.2（brew） |
| 2 | 数据库 | SQLite 默认；repository 走接口，Postgres 留作可选驱动 |
| 3 | 后端技术栈 | `net/http` + chi（路由）/ sqlc（类型安全 SQL）/ goose（migration） |
| 4 | 前端 UI 库 | Tailwind + shadcn/ui |
| 5 | UI/文档语言 | **英文优先**（界面文案 + 代码注释），中文作 i18n 语言包 |
| 6 | 端口 | API `8410` / Vite dev `5410`（已验证空闲，绑定后回写 ports.md） |
| 7 | GitHub org/user | 未定，`go.mod` 保持占位 `github.com/paraview/keylite` |
| 8 | 范围与完成判定 | 需求 §3.1–3.10 全部 12 切片；build/vet/test/前端 build 全绿 + 主流程跑通 |
| 9 | 推送授权 | **只本地提交，不建远端、不推送**（需单独授权） |

## 实施清单（对应需求文档 §3.1–3.10）

| # | 模块 | 需求章节 | 状态 |
|---|---|---|---|
| 0 | 环境与骨架（Go 工具链 / 目录 / 配置 / 日志 / 健康检查） | — | ⬜ |
| 1 | 数据库 schema + migration | §3.1/3.4 | ⬜ |
| 2 | 用户账号管理（增改查/启禁用/改密/个人中心） | §3.1 | ⬜ |
| 3 | Excel 批量导入 | §3.1 | ⬜ |
| 4 | 自主注册 + 注册开关 | §3.1/3.10 | ⬜ |
| 5 | 登录 / 会话 / JWT 签发校验 | §3.2/3.6 | ⬜ |
| 6 | 两档角色 + 全局鉴权拦截 | §3.3/3.5 | ⬜ |
| 7 | 一级机构管理 + 用户机构归属 | §3.4 | ⬜ |
| 8 | 开放鉴权接口（用户信息 / 身份校验） | §3.7 | ⬜ |
| 9 | 下游同步接口（幂等拉取） | §3.8 | ⬜ |
| 10 | 日志审计（登录/操作/鉴权/注册/机构变更） | §3.9 | ⬜ |
| 11 | 系统配置（会话超时/Token 有效期/注册开关/初始管理员） | §3.10 | ⬜ |
| 12 | 前端页面（登录/注册/用户/机构/日志/配置/个人中心） | §3.5 菜单自适应 | ⬜ |
| 13 | go:embed 集成 + Dockerfile + compose | — | ⬜ |
| 14 | 文档收尾（README/access-guide/integrations/CHANGELOG） | — | ⬜ |

## 工作纪律（无人值守期间）

- 每完成一个切片：`go build` + `go vet` + `go test` + 前端 `npm run build` 全绿才提交
- 提交用 `git commit -s`（DCO 必需），**禁用 `-am`**（会漏 untracked 新文件）
- 一个切片一个提交，commit message 说清 why
- **不推送、不建 GitHub 远端**——除非用户另行明确授权
- 实际写的接口路径必须与 `docs/api-conventions.md` 一致（发现不一致时改代码，不改文档去将就）
- 端口一旦实际绑定，立即回写 `~/.claude/ports.md`
- 引入任何第三方服务/依赖 → 同批次更新 `docs/integrations.md`

## 进度日志

（每完成一个切片追加一行：日期 + 切片 + commit hash）

- 2026-08-06 骨架 + License + 需求文档归位 — `9cecaa3`
- 2026-08-06 前端 Vite 脚手架 + 两份规范文档 — `74fe213`
