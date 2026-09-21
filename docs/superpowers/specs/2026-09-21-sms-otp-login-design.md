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

### 3.2 常量与限流实现方式

- 验证码：6 位数字，5 分钟过期，一次性消费——这部分**是 DB 表**（`sms_login_codes`），按租户存取，查询正常带 `tenant_id` 过滤，跟其余租户隔离的表毫无二致
- 单条验证码最多校验错 5 次，超过即失效（不必等到自然过期）——同样落在 `sms_login_codes` 表里的 `attempts` 字段，租户内查询

以下四个限流维度**不进数据库**，改在内存里实现，理由见下方"为什么不进数据库"：

- 冷却：**同一手机号**（跨租户聚合）60 秒内只能发一次
- 每手机号每日上限：**同一手机号**（同样跨租户聚合）24 小时内最多 5 条——理由跟 `RecoveryPerAccountPerDay` 一致：这是共享的发送额度和发送方信誉，攻击者不能通过换租户代码的方式对同一个受害者手机号叠加额度
- 每 IP 每日上限：20 条（跟 `RateLimitAuth` 一样用 `ClientIP`，天然跨租户）——单独的每分钟限流防不住"同一 IP 慢慢换号码发"
- 部署级每日总量硬顶：默认 1000 条，可通过环境变量调整——防的是"攻击者轮换大量不同手机号，每个号都在自己的 5 条/天以内，但合计打爆整个部署的短信预算/发送方信誉"

**为什么不进数据库**：`internal/store` 有一套"租户隔离守门测试"（`tenancy_guard_test.go`），核心规则是：任何触碰租户隔离表的查询都必须按 `tenant_id` 过滤，允许不过滤的查询白名单被硬编码为**恰好两条**，且文档写明白名单的资格是"决定租户是谁的查询"——我这四个维度恰恰是"明知道租户是谁、故意不按它过滤"，条件上就不满足这份白名单的入选资格，不能把新查询硬塞进去（哪怕用注释让文本检查凑巧通过，也是在绕开这份不变式本来要守的东西，不能这么做）。

改用内存计数器是更贴合现状的做法，不是退而求其次：`internal/httpx.RateLimiter` 现在就是这一类"按不可信输入（客户端 IP）限流、保护共享资源"场景的现成落脚点，而且它保护的是密码撞库这个风险更高的场景——"进程重启计数清零、多实例部署各算各的"这个局限,那个限流器已经先天带着,一直被接受。我这四个维度保护的是部署级共享的短信预算，天然就该跟它放在一起，而不是进租户数据模型。

实现上：60 秒冷却和 5 条/IP/20 条/1000 条总量都需要"日窗口"语义（一天内计数到点归零），跟现有 `RateLimiter` 的令牌桶（连续续杯、不是"一天一清零"）不是一回事——按 5/1440 每分钟配一个令牌桶不会得到真正的"每天 5 条"，09:00 发了 5 条,到 13:00 桶又续出新的名额,等于没限。所以要在 `internal/httpx` 里新写一个按"日窗口"计数、定期清扫的小类型（跟 `RateLimiter.sweep` 一个思路），60 秒冷却继续用现有令牌桶即可。

`/api/v1/auth/sms/code` 和 `/api/v1/auth/sms/login` 都挂在 `/api/v1/auth/` 前缀下，自动吃到现有 `RateLimitAuth` 中间件的按 IP 每分钟限流（这一层本来就是纯 IP key，天然跨租户，代码已核实：`internal/httpx/ratelimit.go` 的 `RateLimitAuth`）。

### 3.3 启用条件

租户级设置 `SMSLoginEnabled bool`（默认 `false`），且部署侧配置了真实短信发送器（非 `notify.NotConfiguredSMS`）时，登录页才展示这个入口。管理员在设置页手动开启。

## 4. `SMSSender` 接口改造 + 阿里云 transport

### 4.1 接口改造（影响既有调用点）

现有 `SMSSender.Send(ctx, phone, text string) error` 是一段拼好的自由文本——密码找回/注册验证目前就是把"点击这个链接，30 分钟内有效"整句渲染成字符串塞进去。阿里云短信要求走**审核过的模板**：固定文案 + 有限几个具名变量（比如"您的验证码是`${code}`，`${minute}`分钟内有效"），不接受任意自由文本，模板审核也是按这个变量结构过的。所以接口要改成"用途 + 具名变量"：

```go
// SMSKind is which template a purpose maps to. Each transport that needs a
// real template (Aliyun) is configured with one template code per kind;
// a transport with no such requirement is free to ignore it.
type SMSKind string

const (
	SMSKindLoginCode    SMSKind = "login_code"    // {"Code": "...", "Minutes": "..."}
	SMSKindRecovery     SMSKind = "recovery"      // {"Link": "...", "Minutes": "..."}
	SMSKindVerification SMSKind = "verification"  // {"Link": "...", "Minutes": "..."}
)

type SMSSender interface {
	Send(ctx context.Context, phone string, kind SMSKind, params map[string]string) error
}
```

这个改动同时改掉 `RecoveryService`、`VerificationService` 现有调用 `.Send(ctx, phone, text)` 的两处：不再自己拼好整句 text，而是把渲染用的原始变量（`Link`、`Minutes`）连同 `SMSKindRecovery`/`SMSKindVerification` 一起传给 `Send`。`NotConfiguredSMS.Send` 签名同步改，行为不变（永远返回 `ErrNotConfigured`）。

### 4.2 阿里云 transport

参照 `internal/notify` 里 `Mailer`/`ResendConfig` 的风格：原生 HTTP + 手写签名，不引入阿里云官方 SDK。

- `notify.AliyunSMSConfig{AccessKeyID, AccessKeySecret, SignName, TemplateCodes map[SMSKind]string, Endpoint}`——`TemplateCodes` 按用途配置各自的模板码（登录验证码、密码找回、注册验证三个模板需要在阿里云控制台分别报备，参数名要跟审核通过的模板占位符一致）；`Endpoint` 留空用默认域名，测试可覆盖。
- 签名用 Dysmsapi 的 RPC 风格（HMAC-SHA1，查询参数带 `Signature`）——手写 `crypto/hmac` + `crypto/sha1`，实现前对照阿里云当前文档核对签名步骤，不凭记忆写。
- `NewAliyunSMSSender(cfg)` 返回 `notify.SMSSender` 实现，跟 `NotConfiguredSMS` 并列；`AccessKeyID`/`AccessKeySecret`/`SignName` 缺一不可，在构造时就报错；`TemplateCodes` 缺某个 kind 时，`Send` 遇到那个 kind 才报错（一个部署可能只想开登录验证码，不想让密码找回也走短信）。
- `Send` 把 `params` 序列化成阿里云要求的 `TemplateParam`（JSON 字符串），连同对应 kind 的模板码一起发出。
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

- **短信正文本身不再经过 `internal/i18n`**：§4.1 改成模板+具名变量之后，短信的实际文案是在阿里云控制台报备、审核通过的固定文案，Go 这边只负责把 `Code`/`Link`/`Minutes` 之类的变量填进去，不再有"中英文两份短信文案"这件事——阿里云的模板审核是按中国大陆短信合规走的，这个项目目前也没有第二个地区的短信网关。`i18n.KeyRecoverySMS`/`KeyVerificationSMS` 这两个现有 key 和它们在 `mail.json` 里的值，随着 §4.1 的接口改造一起删除（连同调用它们 `Render` 的那两行）——它们渲染出的字符串不再有地方可用。
- 登录页新增文案（切换链接、手机号/验证码字段、倒计时按钮）中英文都要有——这部分是页面 UI 文案，跟上面短信正文的问题无关，仍然走 `internal/i18n`
- `docs/integrations.md` 新增阿里云短信条目：用途、认证方式、账号负责人、费用、相关环境变量
- `.env.example` + `docs/ops/environment-variables.md` 新增环境变量说明
- 新迁移脚本遵守本仓库现有的 goose 命名规范（`migrations/NNNNN_description.sql`，`-- +goose Up` / `-- +goose Down`）
- 改了登录页文案 → 必须跑 Playwright e2e 套件（有写死的文案断言）
- Bug 修复/新守门测试遵循"先写失败测试再修"的规则本身不适用（这是新功能不是修 bug），但每个新分支（防枚举、限流、状态检查跳过项）都要有对应测试，且要按"注入一个看起来合理的错误实现，确认测试真的变红"的方式验证过

## 8. 已确认的关键决策（复盘，避免遗漏）

1. 独立登录方式，非 MFA
2. 手机号未匹配账号 → 跟防枚举一致，统一回复"已发送"/`INVALID_CODE`，不单独提示"账号不存在"
3. 接入阿里云后，注册验证/密码找回的 SMS 渠道一并生效，接受这个副作用
4. 短信网关：阿里云 Dysmsapi
5. 限流维度：IP/分钟（现有中间件，DB 之外）、手机号/冷却 60s、手机号/天 ≤5、IP/天 ≤20、部署/天 ≤1000（默认，可配）——后四个维度**都在内存里实现**（新的日窗口计数器，放在 `internal/httpx`，跟 `RateLimiter` 并列），不写进 `sms_login_codes` 表也不新建别的表，理由是 `internal/store/tenancy_guard_test.go` 的白名单只收"决定租户是谁"的查询，"明知租户、故意跨租户聚合"的查询不满足这个条件，不应该把它塞进那份白名单或用注释绕过文本检查。`sms_login_codes` 表本身只存验证码，所有查询照常按 `tenant_id` 过滤，不受影响。
