# 单点登录协议

五个概念理解一次，四个协议就能各自说得清楚，而不是一堆陌生名词。

本页解释协议是什么、一次单点登录里发生了什么。
[单点登录](federation.md)解释如何配置它们。

## 角色：谁是谁

每个单点流程都有两端。各协议的命名不同，这是大多数困惑的来源。

| 概念 | OIDC 名称 | SAML 名称 | CAS 名称 |
|---|---|---|---|
| 持有账号、签发凭证的服务器 | OpenID Provider（OP） | Identity Provider（IdP） | CAS Server |
| 需要验证用户身份的应用 | Relying Party（RP） | Service Provider（SP） | CAS Client / Service |

Portico 永远在左边。接入它的应用永远在右边。

**Issuer** — 标识一个 OpenID Provider（或其中某个租户）的 URL。OIDC
的 discovery 文档位于 `<issuer>/.well-known/openid-configuration`。
客户端配置好 issuer URL 后，其余所有端点地址都能自动发现。

## 凭证形式因协议而异

各协议在单点完成时传递身份的方式不同。

| 协议 | 应用收到什么 | 在哪里验证 |
|---|---|---|
| OIDC | Access token（JWT）+ ID token（JWT）| 对照 `/keys` 处的 JWKS 验证签名 |
| SAML 2.0 | Assertion（已签名的 XML）| 对照 IdP 证书验证签名 |
| CAS | Service ticket（不透明字符串）| 回调 `/serviceValidate` 做后端验证 |

Token 是持有者凭证——谁拿到谁可以用，直到过期。Assertion 和 ticket
是一次性凭证，为特定服务颁发，用一次即失效。

## 协议数量说明

能力表写四个协议，单点登录页标题写三个，两者都没错。

**OAuth 2.1** 是授权协议。它规定应用如何代表用户行事——获取 token、
出示 token、被信任。它不涉及用户是谁。

**OpenID Connect 1.0** 是在 OAuth 2.1 之上的身份层。它新增了
ID token（回答"谁登录了"）和 userinfo 端点（回答"这个人有哪些属性"）。
每一次 OIDC 登录都是一次 OAuth 2.1 交换；反之不然。

分开计数：四个（OAuth 2.1、OIDC、SAML、CAS）。
按开发者配置的视角：三个，因为选 OIDC 就同时得到了 OAuth 2.1。

---

## OpenID Connect

近五年内新写的应用的默认选择。库负责处理协议，应用只看到一个已登录的用户。

**需要配置什么：** issuer URL、client ID，公开客户端则无需其他——PKCE 处理其余。

**最重要的一件事：** Portico 实现的是 OAuth 2.1，要求所有客户端（包括机密客户端）都使用 PKCE。
没有 `code_challenge` 的请求会被拒绝。
参见[排查手册](troubleshooting.md#pkce-required)。

### 授权码流程（含 PKCE）

```
Browser           App（RP）              Portico（OP）
   │                 │                       │
   │─ 点击登录 ──────→│                       │
   │                 │ 生成 verifier          │
   │                 │ challenge=S256(v)      │
   │←── 302 ─────────────────────────────────│
   │    /authorize?client_id=…               │
   │    &code_challenge=…                    │
   │    &code_challenge_method=S256          │
   │                 │                       │
   │─ GET /authorize ──────────────────────→│
   │←── 登录页 ────────────────────────────│
   │─ 提交凭证 ────────────────────────────→│
   │←── 302 /callback?code=… ──────────────│
   │                 │                       │
   │─ ?code=… ──────→│                       │
   │                 │─ POST /oauth/token ──→│
   │                 │  grant_type=authorization_code
   │                 │  code=…               │
   │                 │  code_verifier=…      │
   │                 │←── access_token ──────│
   │                 │    id_token           │
   │                 │                       │
   │←── 已登录 ───────│                       │
```

**已有会话？** 浏览器持有有效的会话 cookie 时，Portico 跳过登录页，
直接颁发授权码。用户无感知。

**没有授权确认页。** 注册应用的管理员就是授权决策本身。
用户不会被要求逐条同意 scope。

### Token 携带的内容

ID token 是 JWT，可在 [jwt.io](https://jwt.io)（离线）或
`portico client token`（本地）解码查看：

| Claim | 含义 |
|---|---|
| `sub` | 用户 ID，在本租户内稳定且唯一 |
| `name` | 显示名 |
| `email` | 主邮箱 |
| `preferred_username` | 用户名 |
| `phone_number` | 手机号（已设置且 scope 含 `phone` 时） |

userinfo 端点（`GET /userinfo`）以 JSON 形式返回同样的内容。

### 延伸阅读

[单点登录 → 实现范围](federation.md#实现范围) — 支持的 scope、端点、授权类型完整列表。

[单点登录 → Claim](federation.md#claim) — 每个 claim 及其来源。

[单点登录 → 本地测试](federation.md#本地测试) — 对本地实例运行一个模拟 SP。

---

## SAML 2.0

OIDC 普及之前，企业产品已内置的集成方式。使用签名 XML 而非 JWT。
库不是可选项——签名要求使得手写 SAML 是不安全的。

**需要配置什么：** 交换 metadata。把 Portico 的 metadata URL 交给 SP；
把 SP 的 metadata XML 交给 Portico。metadata 携带双方所需的端点和证书。

**最重要的一件事：** SAML 没有可靠的单点注销。Portico 刻意不实现它。
参见[单点登录 → 刻意不实现](federation.md#刻意不实现)。

### SP 发起的流程

```
Browser          SP（应用）           Portico（IdP）
   │                │                     │
   │─ 访问页面 ──────→│                     │
   │                │ 构造 AuthnRequest    │
   │←── 302 ────────────────────────────→│
   │    /saml/sso?SAMLRequest=…          │
   │                │                     │
   │─ GET /saml/sso ──────────────────────→│
   │←── 登录页 ───────────────────────────│
   │─ 提交凭证 ───────────────────────────→│
   │←── POST /acs（SAMLResponse）──────────│
   │                │                     │
   │─ POST assertion ──→│                 │
   │                │ 验证 XML 签名        │
   │                │ 提取属性            │
   │←── 建立会话 ────│                     │
```

**Portico 的 metadata URL：**
```
https://<host>/saml/metadata              ← 默认租户
https://<host>/t/<code>/saml/metadata     ← 其他租户
```

### 延伸阅读

[单点登录 → SAML 2.0](federation.md#saml-20) — metadata 交换、属性释放、证书轮换。

---

## CAS

在 SAML 和 OIDC 广泛普及之前，大学门户和 Java 应用已采用的协议。
比 SAML 简单，设计上是有状态的。

**需要配置什么：** CAS server URL（Portico 的 `/cas` 路径）和 service URL（应用的回调地址）。
Service URL 必须在 Portico 中注册。

**最重要的一件事：** CAS ticket 是一次性的，且过期很快。
重定向回来后必须立即发起 serviceValidate 调用。缓存 ticket 或两次验证均会失败。

### 登录流程

```
Browser       App（CAS 客户端）     Portico（CAS 服务端）
   │                 │                      │
   │─ 访问页面 ───────→│                      │
   │                 │                      │
   │←── 302 /cas/login?service=<app URL> ──│
   │                 │                      │
   │─ GET /cas/login ──────────────────────→│
   │←── 登录页 ─────────────────────────────│
   │─ 提交凭证 ─────────────────────────────→│
   │←── 302 <app URL>?ticket=ST-… ─────────│
   │                 │                      │
   │─ ?ticket=ST-… ─→│                      │
   │                 │─ GET /cas/serviceValidate
   │                 │   ?service=<app URL> │
   │                 │   &ticket=ST-…       │
   │                 │←── <cas:authenticationSuccess>
   │                 │    <cas:user>alice</cas:user>
   │←── 建立会话 ─────│                      │
```

**客户端填写的 CAS server URL：**
```
https://<host>/cas              ← 默认租户
https://<host>/t/<code>/cas     ← 其他租户
```

**验证端点：** `/cas/serviceValidate`（CAS 2.0）或
`/cas/p3/serviceValidate`（CAS 3.0，返回更多属性）。

### 延伸阅读

[单点登录 → CAS](federation.md#cas) — service 注册、属性映射、2.0 与 3.0 的响应差异。

---

## 选哪个

| 场景 | 选择 |
|---|---|
| 新建应用 | OIDC — 所有现代框架都有现成的库 |
| 已有 SAML 支持的企业系统 | SAML 2.0 — 配置 metadata 即完成 |
| 已在用 CAS 的大学门户或 Java 应用 | CAS — 迁移成本最低 |
| 需要代表用户调用 API | OIDC — access token 正是 OAuth 2.1 的用途 |
| 文档写的是"OpenID Connect" | OIDC |
| 文档只写"SSO"未说明协议 | 追问——通常指 SAML |

三个协议接触的是同一批账号，返回的是同样的信息，选择取决于应用已经支持什么，与 Portico 无关。

---

## 字段名称对照

登录完成后，同一个人的信息会以不同的字段名传到应用，具体取决于使用了哪个协议。
[单点登录中的名字映射表](federation.md#字段名称对照)列出了三套名字的对应关系。
