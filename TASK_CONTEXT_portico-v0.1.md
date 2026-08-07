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
| 4 | OIDC + OAuth 2.1 | ✅ `430d41d` `eae63ca` `bfb8e0f` `fdac3da` `197c299` `619d90e` `7aa7829` `407d081` `eefe263` `52f29c8` |
| 5 | SAML 2.0 | ⬜ |
| 6 | CAS | ⬜ |

## 阶段 4 已完成（收尾记录）

- **端到端跑通**：`internal/server/federation_test.go` 用真实 RP（`zitadel/oidc` 的 `pkg/client/rp`、`pkg/client/rs`）打真实端口，15 个用例覆盖 discovery/PKCE/重放/错 verifier/无 PKCE/refresh 轮换+链撤销/停用账号/双 issuer/跨租户/内省/撤销/RP 发起登出/清扫。
- **写测试与浏览器实测各查出真 bug**（都已修，都有测试守着）：
  - ID token 缺 `tenant_code`/`role`（私有 claim 只进 access token）
  - discovery 文档宣告 implicit / JWT-bearer / device 端点——库的默认值，与实现相反；已改为发布修正后的文档
  - 内省对停用账号仍返回 `active: true`（handler 在 storage 返回后自己置 true，只能靠返回 error 表达）
  - 撤销端点收到的是 **token id 不是 token**，按 hash 查恒 miss → 静默不撤销
  - 前端：错租户后退出登录，错误信息卡死（once-guard 按挂载而非按账号）
  - 前端：记忆的租户会盖掉授权请求所属租户
- **已知缺口（写进 docs/federation.md「Known limitations」）**：过期 refresh token 不清理（授权请求会清）；access token 无法撤销（15 分钟 + 内省）；无 consent 页；无 `private_key_jwt`。
- **未覆盖**：前端无测试框架，上面两个前端 bug 没有回归测试。

## 验收实例（阶段 4 交付时的状态）

- 二进制 `/tmp/portico`，环境变量在 `/tmp/portico-env.sh`（`source` 后即可跑 CLI）
- 库 `portico_dev` **已重建**（`00001_init.sql` 加了 `issuer` 列，旧库不兼容；先前的 2 租户 6 用户数据已丢，属预发布预期）
- 租户：`default`（admin / `Portico-Admin-2026`）、`acme`（admin / `Acme-Admin-2026`）
- 客户端：`demo`（public，default 租户）、`grafana`（confidential，default）、`acme-demo`（public，acme）
- 演示 RP 回调：`python3 /tmp/rp-callback.py` 监听 `127.0.0.1:9999`，打印收到的 code

## 阶段 4 范围与决策（已定，勿再议）

- **库选 `github.com/zitadel/oidc/v3`**：v3.49.2 今天还在发；ory/fosite 更有名但停在 2024-12 的 v0.49.0、仍是 0.x、四条已披露公告。解析攻击者可控的授权请求的那个依赖，"今早刚发版"胜过"名气大"。
- **每租户一个 issuer**：`{PUBLIC_URL}/t/{code}`，各自的 discovery / JWKS / 密钥。共用 issuer + 租户 claim 只有在每个接入方都额外写代码校验时才安全（没有标准 RP 库会校验自定义 claim），那正是阶段 2 拒绝的"靠纪律"。路径里带租户还能让租户在查 client / auth code **之前**就已知，`unscopedQueries` 才能保持恰好 1 条。默认租户额外在根路径也提供一份，单租户部署不必知道租户存在。
- **OIDC 必须用非对称签名**（RS256/ES256 + JWKS），HS256 做不了——RP 靠 JWKS 离线验签。新增签名密钥表 + 轮换（双活 + kid）。Portico 自己的会话继续用 HS256 `TokenService`，两种 token 是有意的：一种本服务自己验，一种别人离线验。
- **README「Sessions that actually revoke」会因此变成假话**：下游资源服务器拿 access token 时根本不碰 Portico。同一提交里改掉措辞（短寿命 + introspection，或把这句限定为 Portico 自己的会话）。
- **范围内**：授权码 + PKCE、refresh（带轮换）、discovery、JWKS、userinfo、revocation、RP-initiated logout、客户端注册走 CLI。**明确不做**：动态客户端注册、device flow、client_credentials、front-channel logout、DPoP、PAR。
- **⚠️ 开发库别再用 `portico_dev`**：验收实例正跑在上面，阶段 4 还要再改 `00001_init.sql`。另起库名。

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
