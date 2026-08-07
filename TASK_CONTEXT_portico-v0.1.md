# TASK_CONTEXT — Portico V0.1

> 长任务断点锚点。会话压缩/中断后从这里恢复。全部完成后删除。

## 项目

- 位置：`~/workspace/github/portico`（**原 keylite，已改名**）
- 独立 git 仓库，**零远端、从未推送**
- 需求真相源：`docs/requirements/v0.1-requirements.md`（新范围）
  + `docs/requirements/v0.1-baseline-mvp.md`（旧基线，记录了"禁用而非删除"等既有决策的由来）

## 已确认决策（用户 2026-08-07）

| # | 决策 |
|---|---|
| 1 | 演进为**完整 IAM**；改名 **Portico**（xiam 被否：撞 authentik 的 XIAM 定位术语 / 品类缩写不可拥有 / 中文读作虾米） |
| 2 | 版本仍用 **v0.1**（从未发布）；本地 v0.1.0 标签已删，CHANGELOG 回退 Unreleased |
| 3 | **每租户各自管理员**；**无跨租户超级管理员**；租户开通走 **CLI**（带外运维） |
| 4 | 找回密码：邮件走**标准 SMTP**（不绑厂商），短信定义 **provider 接口 + 占位实现** |
| 5 | 存储改 **PostgreSQL**（替换 SQLite） |
| 6 | **尽量用成熟库**；SAML 签名验证**禁止手写** |

## 阶段进度

| # | 阶段 | 状态 |
|---|---|---|
| 0 | 改名 Portico + 定位重写 + 版本回退 | ✅ `9245b6c` |
| 1 | 迁移 PostgreSQL | ✅ `22b2b6c` |
| 2 | 多租户隔离 | ✅ `d16a2a8`（前置修复 `5f879c4`） |
| 3 | 多凭证登录 + 自服务闭环 | ✅ `5e29d2f` + `08f0086` |
| 4 | OIDC + OAuth 2.1 | 🔄 下一步 |
| 5 | SAML 2.0 | ⬜ |
| 6 | CAS | ⬜ |

## 关键约束（易在压缩后丢失）

- **多租户隔离已落地，三重保障勿拆**：`internal/store/scoped.go`（租户绑定一次）+ `internal/store/tenancy_guard_test.go`（SQL 漏 `tenant_id` 直接构建失败）+ `internal/server/tenancy_test.go`（双租户行为验证）。唯一免检查询是 `GetUserForAuthentication`，白名单断言恰好 1 条
- **已认证请求的租户只来自 principal**：`X-Portico-Tenant` 仅公开端点可读（`handler/resolvePublicTenant`），认证后忽略
- **改了 `00001_init.sql` 本地库不会自动更新**：goose 已记 00001 为已应用，须 drop/create 库；测试不受影响（每个测试独立库），所以 `go test` 全绿也不代表本地库是新的
- **租户开通走 CLI**：`portico tenant create|list|enable|disable`（`internal/provision`），无 API
- **登录 vs 找回密码用不同查询，勿合并**：登录 `GetUserByIdentifier`（三列并集 + 用户名优先），找回密码 `GetUserByEmail`/`GetUserByPhone`（单列）。合并 = 账号接管（一个账号的用户名等于另一个账号的邮箱时，重置令牌会发错人），有测试守着
- **users 三个租户内唯一约束**：username/email/phone，后两个是 partial index（空 = 未绑定）。约束名在迁移里显式声明，service 按 `pgErr.ConstraintName` 判断哪个字段冲突
- **本地联调邮件**：Mailpit container `portico-mailpit`（SMTP 1026 / UI 8426），`PORTICO_SMTP_ENCRYPTION=none`
- **测试替换发信**：`server.New(cfg, server.WithMailer(...))`
- **分层守卫已存在**：`internal/server/layering_test.go`，改包结构时会失败，这是有意的
- **前端编译进二进制**：Vite 输出到 `internal/web/dist`，`tsc --noEmit` 检查 0 文件（根 tsconfig 是 references 存根），必须用 `npm run typecheck`
- **规范文档 8 份在 `docs/`**：改行为要同步改对应规范，文档描述的是代码实际行为
- **PG 已就位**：`internal/store/dbtime` 已删；动态查询走 `internal/service/common.go` 的 `filters` 构建器（PG 占位符按序编号，手数极易错位）；测试用 `internal/testdb`（testcontainers，或用 `PORTICO_TEST_DB_DSN` 指向现成实例，快很多）
- **提交纪律**：`git commit -s`（DCO 强制），禁用 `-am`，一切片一提交，**不推送**

## 验证命令

```bash
go build ./... && go vet ./... && golangci-lint run ./... && go test ./...
cd web && npm run typecheck && npm run build
```
