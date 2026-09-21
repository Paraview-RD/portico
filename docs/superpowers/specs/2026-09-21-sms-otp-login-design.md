# SMS 验证码登录 — 设计

2026-09-21 · 状态：已过用户确认，待写实施计划

## 1. 目标与范围

新增一种**独立的**登录方式：手机号 + 短信验证码，与用户名/邮箱/手机号 + 密码的现有登录并列，不是登录后的第二因素（MFA）。

同一提交范围内还包含：
- 接入真实短信网关（阿里云 Dysmsapi），替换目前唯一存在的 `notify.NotConfiguredSMS` 占位实现。
- 作为该网关接入的**已确认的副作用**：注册地址验证（`VerificationService`）和密码找回（`RecoveryService`）的 SMS 渠道会随之从"从未生效"变为真正可用（这两处代码早已写好、只是因为没有真实网关一直是死代码）。这不是本次要单独设计的功能，只是接入网关后自然获得的能力，在此明确记录，避免被当作意外行为。

不在范围内：
- 手机号验证码登录时的自动开户——手机号必须已经绑定在某个已存在账号上，找不到匹配账号时按 §3 的防枚举策略处理，不自动创建账号。
- 除阿里云以外的短信网关。

## 2. 认证语义

### 2.1 防枚举

跟现有的密码登录（`UserService.Login` 统一返回 `ErrInvalidCredentials`）、密码找回（`RecoveryService.Request` 无论命中与否都返回同一个"已发送"、真正查找在响应之后异步完成）保持同一套姿态：

- **发验证码接口**：无论手机号是否存在于当前租户，都返回同一个"已发送"的响应；真正的查找、限流计数、发送在响应之后异步完成，接口耗时不能泄露是否命中。
- **验证码校验接口**：验证码错误、已过期、超过尝试次数、手机号不存在——统一返回同一个 `INVALID_CODE`。只有验证码本身核对通过之后，才依次检查账号锁定/禁用/关闭并分别提示——这时调用方已经证明"手机在自己手里"，跟密码登录"猜对密码之后才敢说锁定"是同一个分寸：披露的前提是对方已经掌握了等价于凭证的东西。

### 2.2 登录尾段：复用 `IssueSessionForExternalIdentity`

验证码核对通过、查到账号之后，走跟 `UserService.IssueSessionForExternalIdentity`（Google 等外部身份登录使用的尾段）相同的检查序列：

- **挡**：`LockedUntil`（账号锁定）、`Status != Active`（禁用/关闭，区分关闭的提示语跟密码登录一致）
- **不挡**：`PasswordPolicy().Expired(...)`（密码过期）、`MustChangePassword`（默认密码未替换）

理由：这两项是对"密码"这个凭证本身提出的要求，短信登录全程没有用到密码这个凭证，挡它没有意义。

**跟 Google 登录路径的一处不同，需要单独说明**：`IssueSessionForExternalIdentity` 的文档注释里，跳过 `MustChangePassword` 的理由是"绑定第三方身份之前必须先过一次密码登录，所以能走到这条路径的账号已经证明自己不是在用默认密码"。短信登录没有这道前置动作——账号的手机号可能是管理员创建时就填好的，从未有人用它登录过一次密码。所以这里跳过 `MustChangePassword` 是**独立做出的决定**，不是复用 Google 那条理由：判断依据仍然是"密码状态跟短信这个凭证无关"，而不是"这个账号已经被验证过安全"。

`FailedLoginAttempts`/`LockedUntil` 的锁定计数器不因短信验证码失败而变动——那个计数器统计的是密码猜测，短信验证码的失败次数由验证码表自己的 `attempts` 字段承担，两者不共享、不互相触发锁定。

代码层面：把 `IssueSessionForExternalIdentity` 里"查到账号之后"的公共尾段（状态检查 + 建会话 + 发 token + 写审计）抽成一个内部共享函数，新的 `LoginWithSMSCode` 和它共同调用，避免第三份检查链复制粘贴。

## 3. 数据模型与限流

### 3.1 新表 `sms_login_codes`

| 列 | 说明 |
|---|---|
| `id` | 主键 |
| `tenant_id` | 该验证码归属的租户——登录校验时必须匹配，不可跨租户通用 |
| `phone` | 目标手机号（存储格式沿用现有 `validateContactDetails` 的规则：仅数字，可选前导 `+`） |
| `code_hash` | sha256(验证码)，不存明文，跟密码哈希同一个思路 |
| `attempts` | 已校验失败次数 |
| `consumed_at` | 一次性消费标记，NULL 表示未使用 |
| `expires_at` | 过期时间 |
| `created_at` | 用于冷却时间和每日额度计算 |
| `ip` | 请求来源，写入审计 |

### 3.2 常量

- 验证码：6 位数字，5 分钟过期，一次性消费
- 单条验证码最多校验错 5 次，超过即失效（不必等到自然过期）
- 冷却：**同一手机号**（跨租户聚合，不按 `tenant_id` 过滤）60 秒内只能发一次
- 每手机号每日上限：**同一手机号**（同样跨租户聚合）24 小时内最多 5 条——理由跟 `RecoveryPerAccountPerDay` 一致：这是共享的发送额度和发送方信誉，攻击者不能通过换租户代码的方式对同一个受害者手机号叠加额度
- 每 IP 每日上限：20 条（沿用现有 `ClientIP`，天然跨租户）——单独的每分钟限流防不住"同一 IP 慢慢换号码发"
- 部署级每日总量硬顶：默认 1000 条，可通过环境变量调整——防的是"攻击者轮换大量不同手机号，每个号都在自己的 5 条/天以内，但合计打爆整个部署的短信预算/发送方信誉"，用对 `sms_login_codes` 按时间窗口 `COUNT(*)` 实现，不需要额外的计数存储

`/api/v1/auth/sms/code` 和 `/api/v1/auth/sms/login` 都挂在 `/api/v1/auth/` 前缀下，自动吃到现有 `RateLimitAuth` 中间件的按 IP 每分钟限流（这一层本来就是纯 IP key，天然跨租户，代码已核实：`internal/httpx/ratelimit.go` 的 `RateLimitAuth`）。

### 3.3 启用条件

租户级设置 `SMSLoginEnabled bool`（默认 `false`），且部署侧配置了真实短信发送器（非 `notify.NotConfiguredSMS`）时，登录页才展示这个入口。管理员在设置页手动开启。

## 4. 阿里云短信 transport

参照 `internal/notify` 里 `Mailer`/`ResendConfig` 的风格：原生 HTTP + 手写签名，不引入阿里云官方 SDK。

- `notify.AliyunSMSConfig{AccessKeyID, AccessKeySecret, SignName, TemplateCode, Endpoint}`，`Endpoint` 留空用默认域名，测试可覆盖。
- 签名用 Dysmsapi 的 RPC 风格（HMAC-SHA1，查询参数带 `Signature`）——手写 `crypto/hmac` + `crypto/sha1`，实现前对照阿里云当前文档核对签名步骤，不凭记忆写。
- `NewAliyunSMSSender(cfg)` 返回 `notify.SMSSender` 实现，跟 `NotConfiguredSMS` 并列；四个字段缺一不可，在构造时就报错，不等到发送才发现。
- 验证码走审核过的短信模板，验证码作为模板变量传入，不拼自由文本。
- 单测用 fake sender（跟 Mailer 测试同一手法），不需要真实密钥。

## 5. API / OpenAPI

- `POST /api/v1/auth/sms/code` — `{tenant, phone}` → 始终 `200`（防枚举，§2.1）
- `POST /api/v1/auth/sms/login` — `{tenant, phone, code}` → 成功返回 `Session`（跟密码登录同一个响应结构），失败 `INVALID_CODE`（除验证码核对通过之后的锁定/禁用/注销）
- 补充一个公开的"该租户能否使用短信登录"查询（挂在现有 `registrationStatus`/`externalOptions` 同类的公开信息接口里，或单独一个字段），供登录页决定是否显示入口
- 写入 `openapi.yaml`，提交前跑 `redocly lint`

## 6. 前端

`web/src/pages/LoginPage.tsx` 密码表单旁增加"验证码登录"切换。切换后：

- 手机号输入框
- "发送验证码"按钮，点击后进入 60 秒倒计时禁用（新增的 UI 模式，当前代码里没有现成的倒计时组件可抄，需要新写）
- 验证码输入框 + 登录按钮
- 只有该租户 `SMSLoginEnabled` 且部署侧配置了短信网关时才展示切换入口

## 7. i18n / 文档 / 测试义务（同一提交内）

- 新增短信模板消息 key（参照现有 `i18n.KeyRecoverySMS` 模式），中英文都要有
- 登录页新增文案（切换链接、手机号/验证码字段、倒计时按钮）中英文都要有
- `docs/integrations.md` 新增阿里云短信条目：用途、认证方式、账号负责人、费用、相关环境变量
- `.env.example` + `docs/ops/environment-variables.md` 新增环境变量说明
- 新迁移脚本遵守 Flyway 命名规范
- 改了登录页文案 → 必须跑 Playwright e2e 套件（有写死的文案断言）
- Bug 修复/新守门测试遵循"先写失败测试再修"的规则本身不适用（这是新功能不是修 bug），但每个新分支（防枚举、限流、状态检查跳过项）都要有对应测试，且要按"注入一个看起来合理的错误实现，确认测试真的变红"的方式验证过

## 8. 已确认的关键决策（复盘，避免遗漏）

1. 独立登录方式，非 MFA
2. 手机号未匹配账号 → 跟防枚举一致，统一回复"已发送"/`INVALID_CODE`，不单独提示"账号不存在"
3. 接入阿里云后，注册验证/密码找回的 SMS 渠道一并生效，接受这个副作用
4. 短信网关：阿里云 Dysmsapi
5. 限流维度：IP/分钟（现有中间件）、手机号/冷却 60s、手机号/天 ≤5、IP/天 ≤20、部署/天 ≤1000（默认，可配），手机号维度的计数**不按租户过滤**
