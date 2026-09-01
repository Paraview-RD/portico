# 配置参考

Portico 在启动时读取的所有环境变量及其默认值。

除 `PORTICO_DB_DSN` 外，每个配置项都有可用的默认值。
`PORTICO_DB_DSN` 没有默认值：数据库连接字符串不是可以猜的东西。
未设置时，服务器会报告该问题并退出。

## 核心

| 变量 | 默认值 | 说明 |
|---|---|---|
| `PORTICO_DB_DSN` | *(必填)* | PostgreSQL 连接字符串。URL 格式：`postgres://user:pass@host:5432/db?sslmode=disable`，也支持 keyword 格式。 |
| `PORTICO_ADDR` | `:8410` | HTTP 服务监听的 TCP 地址。 |
| `PORTICO_PUBLIC_URL` | `http://localhost:8410` | 用户访问本次部署的 URL，用于构建邮件中的链接。生产环境未设置时，密码重置链接将指向 localhost。 |
| `PORTICO_JWT_SECRET` | *(随机)* | 用于签发和验证访问令牌（access token），至少 32 字节。未设置时每次进程启动生成一个随机值——重启后会话失效，其他实例也会拒绝该实例签发的 token。生成命令：`openssl rand -hex 32`。 |
| `PORTICO_ENCRYPTION_KEY` | *(未设置)* | 32 字节十六进制 key，保护 LDAP bind 密码在数据库中的存储。未设置时拒绝保存目录连接器的凭证。必须与 `PORTICO_JWT_SECRET` 不同。生成命令：`openssl rand -hex 32`。 |
| `PORTICO_LOG_LEVEL` | `info` | 日志详细程度。可选值：`debug`、`info`、`warn`、`error`。 |

## SMTP 邮件

以下四个变量都需要设置，SMTP 方式才能正常发送邮件。
没有可用的邮件配置时，密码恢复和注册验证不可用。

| 变量 | 默认值 | 说明 |
|---|---|---|
| `PORTICO_MAIL_TRANSPORT` | `smtp` | 邮件传输方式。`smtp` 或 `resend`。 |
| `PORTICO_SMTP_HOST` | *(未设置)* | SMTP 中继主机名。未设置时邮件不可用。 |
| `PORTICO_SMTP_PORT` | `587` | SMTP 端口。通常 587（STARTTLS）或 465（TLS）。 |
| `PORTICO_SMTP_USERNAME` | *(未设置)* | SMTP 认证用户名。 |
| `PORTICO_SMTP_PASSWORD` | *(未设置)* | SMTP 认证密码。 |
| `PORTICO_SMTP_FROM` | *(未设置)* | 发件人地址，例如 `noreply@example.com`。 |
| `PORTICO_SMTP_ENCRYPTION` | `starttls` | 加密方式。`starttls`（587 端口）、`tls`（465 端口）或 `none`。 |

## Resend 邮件

当 `PORTICO_MAIL_TRANSPORT=resend` 时使用。

| 变量 | 默认值 | 说明 |
|---|---|---|
| `PORTICO_RESEND_API_KEY` | *(resend 必填)* | 来自 [resend.com](https://resend.com) 的 API key。 |
| `PORTICO_MAIL_FROM` | *(resend 必填)* | 在 Resend 已验证域名下的发件人地址。 |

## Token 与会话

| 变量 | 默认值 | 说明 |
|---|---|---|
| `PORTICO_TOKEN_TTL` | `2h` | Access token 有效期。接受 Go duration 格式（`15m`、`2h`）或秒数。可在**系统设置**中运行时覆盖；此值为启动时的默认值。 |

## 认证频率限制

频率限制默认开启。设为 `0` 可完全关闭。

| 变量 | 默认值 | 说明 |
|---|---|---|
| `PORTICO_AUTH_RATE_LIMIT` | `60` | 每个客户端 IP 每分钟在 `/api/v1/auth/` 下允许的写入次数。 |
| `PORTICO_AUTH_RATE_LIMIT_BURST` | `30` | 在该预算内允许的瞬间并发请求数。 |

## 初始化

| 变量 | 默认值 | 说明 |
|---|---|---|
| `PORTICO_INITIAL_ADMIN_USERNAME` | `admin` | 首位管理员的用户名，在空数据库上创建。管理员已存在时忽略。 |
| `PORTICO_INITIAL_ADMIN_PASSWORD` | *(随机，启动时打印一次)* | 首位管理员的密码。未设置时随机生成并在启动日志中打印一次。 |

## 功能开关

| 变量 | 默认值 | 说明 |
|---|---|---|
| `PORTICO_LANDING_PAGE` | `false` | 在根路径显示落地页而非登录表单。适合对外开放、访客可能尚无账号的部署。 |
| `PORTICO_TRUST_PROXY_HEADERS` | `false` | 信任 `X-Forwarded-For` 和 `X-Real-IP`。仅在受控的反向代理位于前端并改写这些 header 时启用——否则调用方可以伪造自己的审计日志 IP。 |
| `PORTICO_TENANT_CONSOLE` | `false` | 注册运营商专用页面：所有租户列表及禁用开关。默认关闭：若 `default` 租户属于某个客户，启用此项会让该客户的管理员看到所有其他客户的列表。 |
| `PORTICO_DEFAULT_LOCALE` | `en` | 当账号和租户均未指定语言时，发送消息使用的语言。必须是本构建已内置消息的语言；填入无法识别的值时服务器拒绝启动。 |

## 自助试用

以下配置仅在 `PORTICO_TRIAL_SIGNUP=true` 时生效。

| 变量 | 默认值 | 说明 |
|---|---|---|
| `PORTICO_TRIAL_SIGNUP` | `false` | 启用自助申请试用租户。需要 SMTP。非公开演示环境请勿开启。 |
| `PORTICO_TRIAL_MAX_TENANTS` | `50` | 同时存在的试用租户数量上限。达到上限后申请表单将提示已满而非排队。`0` 禁用上限。 |
| `PORTICO_TRIAL_RATE_PER_HOUR` | `10` | 整个部署每小时接受的试用申请数。保护所有租户共用的发送配额和发件人信誉。`0` 禁用。 |
| `PORTICO_TRIAL_RATE_LIMIT` | `5` | 每个客户端 IP 每分钟的试用申请次数。 |
| `PORTICO_TRIAL_RATE_LIMIT_BURST` | `3` | 试用申请频率限制的瞬间并发量。 |
| `PORTICO_TRIAL_BLOCKED_EMAIL_DOMAINS` | *(未设置)* | 试用申请时额外拒绝的邮件域名，逗号分隔，追加到内置的一次性邮件地址列表上。示例：`mailinator.com,guerrillamail.com`。 |

## 可观测性

| 变量 | 默认值 | 说明 |
|---|---|---|
| `PORTICO_METRICS_ADDR` | *(未设置)* | Prometheus `/metrics` 端点的 TCP 监听地址。示例：`127.0.0.1:9410`。未设置时不发布任何指标，也不开第二个监听端口。**请绑定到内网接口**——该端点按 Prometheus 惯例不做认证。不要通过与应用相同的端口暴露它。 |

## 示例：最小生产 `.env`

```bash
PORTICO_DB_DSN=postgres://portico:secret@db:5432/portico?sslmode=disable
PORTICO_PUBLIC_URL=https://id.example.com
PORTICO_JWT_SECRET=<openssl rand -hex 32 的输出>
PORTICO_ENCRYPTION_KEY=<openssl rand -hex 32 的输出>

# SMTP
PORTICO_SMTP_HOST=smtp.example.com
PORTICO_SMTP_USERNAME=noreply@example.com
PORTICO_SMTP_PASSWORD=<smtp 密码>
PORTICO_SMTP_FROM=noreply@example.com

# 反向代理
PORTICO_TRUST_PROXY_HEADERS=true
```

完整的部署流程参见[生产环境部署](deployment.md)。
