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
| 1 | 迁移 PostgreSQL | 🔄 进行中 |
| 2 | 多租户隔离 | ⬜ |
| 3 | 多凭证登录 + 自服务闭环 | ⬜ |
| 4 | OIDC + OAuth 2.1 | ⬜ |
| 5 | SAML 2.0 | ⬜ |
| 6 | CAS | ⬜ |

## 关键约束（易在压缩后丢失）

- **多租户隔离不能靠纪律**：必须做租户作用域 store 包装 + 自动化守卫测试（漏一条查询 = 跨租户泄露，review 看不出来）
- **分层守卫已存在**：`internal/server/layering_test.go`，改包结构时会失败，这是有意的
- **前端编译进二进制**：Vite 输出到 `internal/web/dist`，`tsc --noEmit` 检查 0 文件（根 tsconfig 是 references 存根），必须用 `npm run typecheck`
- **规范文档 8 份在 `docs/`**：改行为要同步改对应规范，文档描述的是代码实际行为
- **PG 迁移连带**：删掉 `internal/store/dbtime`（PG 有原生 timestamptz）、compose 加 DB 服务、README/integrations/database-conventions 要改（"一个二进制一个文件"的卖点不再成立）
- **提交纪律**：`git commit -s`（DCO 强制），禁用 `-am`，一切片一提交，**不推送**

## 验证命令

```bash
go build ./... && go vet ./... && golangci-lint run ./... && go test ./...
cd web && npm run typecheck && npm run build
```
