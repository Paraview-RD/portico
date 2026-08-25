# 排查手册

接入过程中常见的报错及处理方法。

## OpenID Connect / OAuth

### 缺少 PKCE 参数 {#pkce-required}

```
error=invalid_request
error_description=code_challenge is required; this server implements
OAuth 2.1, which requires PKCE of every client
```

**原因：** 授权请求没有携带 `code_challenge` 和 `code_challenge_method`。
Portico 实现的是 OAuth 2.1，要求所有客户端类型（包括机密客户端）都使用 PKCE。

**处理方法：** 在 OIDC 库或应用配置中启用 PKCE，设置 `code_challenge_method=S256`。
库会自动生成和管理 verifier 与 challenge。

- **Casdoor 接入：** 编辑对应的应用，开启 PKCE 选项。
- **手动构造请求：** 生成随机 `code_verifier`（43–128 个 URL-safe 字符），
  计算 `code_challenge = BASE64URL(SHA256(code_verifier))`，两者都加入授权请求。
  换 token 时再带上 `code_verifier`。

Portico 不接受 `code_challenge_method=plain`。

---

### redirect_uri 未注册

```
error=invalid_request
error_description=redirect_uri is not registered for this client
```

**原因：** 授权请求中的 `redirect_uri` 与该客户端注册的任何 URI 都不完全一致。
匹配是逐字节比较的：末尾斜杠、路径不同、`http` 与 `https` 的差异都算不匹配。

**处理方法：** 在 Portico 控制台打开该应用，核对已注册的 redirect URI，
让授权请求中的参数与其中一个完全一致。
即使只注册了一个 URI，请求时也必须传这个参数。

---

### 租户不匹配

```
{"code":"AUTH_REQUEST_WRONG_TENANT","message":"This sign-in request belongs
to a different tenant. Sign out and sign in to the tenant the application
asked for."}
```

**原因：** 浏览器当前登录的是租户 A，但授权请求针对的是租户 B。

**处理方法：** 先退出登录，再重新发起授权流程。
通常发生在浏览器已有某个租户的会话，而某个链接打开了另一个租户的登录页时。

---

### 授权请求已过期或已被使用

```
{"code":"AUTH_REQUEST_NOT_FOUND","message":"This sign-in request has
expired or was already used. Start again from the application."}
```

**原因：** 登录被中途放弃、页面停留过久，或授权码已被使用过一次。
授权码是一次性的。

**处理方法：** 从应用重新发起登录流程。

---

### 授权码无效或已过期

```
error=invalid_grant
error_description=invalid or expired authorization code
```

**原因：** 传给 `/oauth/token` 的 code 已被使用、已过期或不存在。
Code 是短生命周期的（几分钟），且一次性。

**处理方法：** 重新发起授权流程。如果在自动化流程中频繁出现，
检查换 token 的操作是否在收到重定向后立即执行，而不是延迟执行。

---

### 客户端不存在或已禁用

```
{"code":"OAUTH_CLIENT_NOT_FOUND","message":"The application this sign-in
was for is no longer registered."}

{"code":"OAUTH_CLIENT_DISABLED","message":"The application this sign-in
was for has been disabled."}
```

**原因：** 该 `client_id` 在此租户下不存在，或该应用已在控制台中被禁用。

**处理方法：** 在控制台的**应用**下核对 client ID。
Client ID 是租户级的：租户 A 的 `wiki` 和租户 B 的 `wiki` 是不同的注册记录。

---

### 授权端点 URL 路径错误

**现象：** 浏览器停在 Portico 首页或登录页，没有继续流程，也没有报错信息，
地址栏里仍包含授权参数。

**原因：** 路径写错了。Portico 的 OIDC 授权端点是 `/authorize`，
不是 `/auth` 或 `/oauth/authorize`。

**处理方法：** 使用正确的路径：

```
# 默认租户
https://<host>/authorize

# 指定租户
https://<host>/t/<租户代码>/authorize
```

通过 discovery 文档核对正确地址：

```
GET https://<host>/t/<租户代码>/.well-known/openid-configuration
```

响应中的 `authorization_endpoint` 字段即为正确 URL。

---

## SAML 2.0

### Assertion 签名验证失败

**原因：** SP 在用一个已被 Portico 轮换掉的旧证书验证 assertion，
或 SP 缓存的 metadata 已过期。

**处理方法：** 重新拉取 Portico 的 metadata 并导入 SP：

```
GET https://<host>/saml/metadata
GET https://<host>/t/<租户代码>/saml/metadata
```

证书轮换流程参见[单点登录 → 证书](federation.md#证书)。

---

### SP 没有收到响应

**原因：** Portico 中注册的 ACS URL 与 SP 在 `AuthnRequest` 中发送的不匹配，
或 SP 的 metadata 尚未导入 Portico。

**处理方法：** 在控制台检查该应用的 SAML 配置。
ACS URL 和 Entity ID 必须与 SP 的 metadata 完全一致。

---

## CAS

### Ticket 无效

**原因：** Service ticket 已过期（ticket 生命周期很短）、已被验证过，
或验证请求中的 `service` 参数与登录重定向时的 `service` 参数不完全一致。

**处理方法：** 登录重定向和验证调用中的 service URL 必须完全相同，
包括协议、端口和查询参数。若 ticket 已过期，重新发起登录流程。

---

### Service 未注册

**原因：** `/cas/login` 请求中的 `service` URL 未在该租户中注册。

**处理方法：** 在控制台的**应用 → CAS** 下注册该 service URL。

---

## 启动与配置

### 启动时退出：PORTICO_DB_DSN 未设置

数据库连接字符串没有默认值。启动前必须设置 `PORTICO_DB_DSN`。

### 启动时出现 PORTICO_JWT_SECRET 警告

```
warn: jwt_secret_generated=true
```

`PORTICO_JWT_SECRET` 未设置，系统随机生成了一个。进程重启后所有会话将失效。
请设置一个持久化的 secret：

```bash
export PORTICO_JWT_SECRET=$(openssl rand -hex 32)
```

### PORTICO_JWT_SECRET 过短

```
PORTICO_JWT_SECRET is N bytes; it must be at least 32.
Generate one with: openssl rand -hex 32
```

Secret 至少需要 32 字节，用提示的命令生成一个新的替换原值。

### PORTICO_ENCRYPTION_KEY 与 PORTICO_JWT_SECRET 值相同

两者必须不同。它们保护的内容不同，泄露的途径也不同；
一个值身兼两职意味着任意一个泄露就两个都丢失。生成第二个 key：

```bash
export PORTICO_ENCRYPTION_KEY=$(openssl rand -hex 32)
```

### 邮件未送达

1. 确认 `PORTICO_SMTP_HOST` 已设置（或 `PORTICO_MAIL_TRANSPORT=resend` 加 `PORTICO_RESEND_API_KEY`）。
2. 检查 `PORTICO_PUBLIC_URL` 是否是用户实际访问的地址——密码重置链接由此构建。
3. Resend 方式：`From` 地址必须是在 Resend 已验证域名下的地址。
4. 检查端口和加密方式：默认是 587 端口 + STARTTLS。
   465 端口用 `PORTICO_SMTP_ENCRYPTION=tls`，服务器不支持加密时用 `none`。

### 集群中其他实例拒绝 token

所有实例必须共享同一个 `PORTICO_JWT_SECRET`。
一个实例签发的 token 由所有实例验证；
各实例 secret 不同时，token 只在签发它的那个实例上有效。
