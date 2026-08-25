# 集成食谱

常见应用的逐步配置指南。每篇指南假设 Portico 已在运行，且你拥有目标租户的管理员权限。

**任何集成前**：先拉取 discovery 文档，确认 issuer URL，并将端点地址复制到应用配置中。

```
GET https://<host>/.well-known/openid-configuration          ← 默认租户
GET https://<host>/t/<租户代码>/.well-known/openid-configuration
```

---

## 通用 OIDC 应用

这是通用配置流程。如果你的应用文档写的是"OpenID Connect"或"OAuth 2.0 / OIDC"，按以下步骤操作。

### 第一步：在 Portico 注册应用

在 Portico 控制台：**应用 → 新建应用**。

| 字段 | 填写内容 |
|---|---|
| Client ID | 简短的标识符，例如 `grafana` 或 `myapp` |
| 客户端类型 | 服务端应用、能保管 secret 的选**机密**；浏览器或移动端应用选**公开** |
| 回调地址 | 应用 OIDC 文档中给出的回调 URL，例如 `https://grafana.example.com/login/generic_oauth` |

保存后立即复制 **client secret**——只显示一次。

也可以从命令行注册：

```bash
portico client register \
  --id myapp \
  --name "My App" \
  --redirect-uri https://myapp.example.com/auth/callback
```

### 第二步：配置应用

将以下四个值填入你的应用：

| 值 | 在哪里找 |
|---|---|
| Issuer URL | 默认租户为 `https://<host>`，其他租户为 `https://<host>/t/<代码>` |
| Client ID | 第一步中设置的 ID |
| Client secret | 注册后显示的 secret（仅机密客户端需要） |
| Scopes | `openid profile email`，按需加 `phone` 或 `offline_access` |

大多数 OIDC 库会从 issuer URL 自动发现 token、userinfo、JWKS 等端点。
如果需要手动填写：

| 端点 | URL |
|---|---|
| 授权 | `<issuer>/authorize` |
| Token | `<issuer>/oauth/token` |
| Userinfo | `<issuer>/userinfo` |
| JWKS | `<issuer>/keys` |
| 登出 | `<issuer>/end_session` |

### 第三步：启用 PKCE

Portico 实现的是 OAuth 2.1，要求所有客户端都使用 PKCE。
在应用的 OIDC 配置中找到 PKCE 选项，设为 `S256`。
如果库没有专门的 PKCE 开关，很可能默认已自动处理——发起一次登录，
确认授权 URL 中包含 `code_challenge` 即可验证。

若请求被拒绝，参见[排查手册 → 缺少 PKCE 参数](troubleshooting.md#pkce-required)。

### 第四步：测试

从应用发起一次登录。在 Portico 完成认证后，应跳回应用并完成登录。
出错时，回调地址会收到 `error` 和 `error_description` 参数，说明问题所在，
参见[排查手册](troubleshooting.md)。

---

## Grafana

Grafana 通过其通用 OAuth provider 支持 OIDC。

### 在 Portico 注册

```bash
portico client register \
  --id grafana \
  --name "Grafana" \
  --redirect-uri https://grafana.example.com/login/generic_oauth
```

复制 client secret。

### 配置 Grafana

在 `grafana.ini` 或对应的环境变量中：

```ini
[auth.generic_oauth]
enabled = true
name = Portico
allow_sign_up = true
client_id = grafana
client_secret = <上面的 secret>
scopes = openid profile email
auth_url = https://<host>/authorize
token_url = https://<host>/oauth/token
api_url = https://<host>/userinfo
use_pkce = true

# 将 Portico 的 claim 映射到 Grafana 字段
login_attribute_path = preferred_username
name_attribute_path = name
email_attribute_path = email

# 可选：按角色限制
# role_attribute_path = contains(roles[*], 'Admin') && 'Admin' || 'Viewer'
```

非默认租户时，把三个 URL 中的 `https://<host>` 替换为 `https://<host>/t/<租户代码>`。

**环境变量方式**（Docker / Kubernetes）：

```bash
GF_AUTH_GENERIC_OAUTH_ENABLED=true
GF_AUTH_GENERIC_OAUTH_NAME=Portico
GF_AUTH_GENERIC_OAUTH_CLIENT_ID=grafana
GF_AUTH_GENERIC_OAUTH_CLIENT_SECRET=<secret>
GF_AUTH_GENERIC_OAUTH_SCOPES=openid profile email
GF_AUTH_GENERIC_OAUTH_AUTH_URL=https://<host>/authorize
GF_AUTH_GENERIC_OAUTH_TOKEN_URL=https://<host>/oauth/token
GF_AUTH_GENERIC_OAUTH_API_URL=https://<host>/userinfo
GF_AUTH_GENERIC_OAUTH_USE_PKCE=true
GF_AUTH_GENERIC_OAUTH_LOGIN_ATTRIBUTE_PATH=preferred_username
GF_AUTH_GENERIC_OAUTH_NAME_ATTRIBUTE_PATH=name
GF_AUTH_GENERIC_OAUTH_EMAIL_ATTRIBUTE_PATH=email
```

重启 Grafana。登录页会出现 **Sign in with Portico** 按钮。

### 注意事项

- Grafana 在首次登录时创建本地用户。如果 `allow_sign_up = false`，
  用户必须已在 Grafana 中存在。
- 要将 Portico 的 `role` claim 映射到 Grafana 角色，
  参考 Grafana 的 `role_attribute_path` 文档，claim 名称在 Portico 中是 `role`。

---

## Nextcloud

Nextcloud 通过 **OpenID Connect user backend** 应用支持 OIDC。

### 安装应用

在 Nextcloud：**应用 → 搜索"OpenID Connect user backend"** → 启用。

### 在 Portico 注册

```bash
portico client register \
  --id nextcloud \
  --name "Nextcloud" \
  --redirect-uri https://nextcloud.example.com/apps/user_oidc/code
```

复制 client secret。

### 在 Nextcloud 配置

**设置 → OpenID Connect** → 添加提供商：

| 字段 | 值 |
|---|---|
| 标识符 | `Portico`（显示在登录页） |
| Client ID | `nextcloud` |
| Client secret | 上面的 secret |
| Discovery URL | `https://<host>/.well-known/openid-configuration` |
| Scope | `openid profile email` |

如有 **Use PKCE** 选项，勾选。

非默认租户时，使用 `https://<host>/t/<租户代码>/.well-known/openid-configuration`。

### 注意事项

- Nextcloud 从 discovery URL 自动发现所有端点。
- 用户的 `sub` claim（稳定的用户 ID）作为账号标识。
  Nextcloud 原有账号不会自动关联——建议从一开始就通过 Portico 创建账号，
  或用 SCIM 推送账号。

---

## 通用 SAML 2.0 应用

SAML 集成需要在 Portico 和 SP 之间交换 metadata。
Metadata 携带双方验证签名所需的端点和证书。

### 第一步：获取 Portico 的 metadata

```
GET https://<host>/saml/metadata                     ← 默认租户
GET https://<host>/t/<租户代码>/saml/metadata         ← 其他租户
```

将此 URL（或下载 XML）导入 SP。SP 从中读取签名证书和 SSO 端点。

### 第二步：获取 SP 的 metadata

SP 会提供 metadata URL 或 XML 文件。在 Portico 控制台：
**应用 → 新建应用 → SAML**。

| 字段 | 值 |
|---|---|
| Entity ID | SP metadata 中的 `entityID` 属性 |
| ACS URL | SP 的断言消费服务 URL |
| SP metadata URL | 粘贴 SP 的 metadata URL（Portico 自动拉取） |

若 SP 不发布 URL，直接粘贴 metadata XML。

### 第三步：配置字段映射（如需）

Portico 默认释放与 OIDC 相同的属性：用户 ID、姓名、邮箱、角色。
如果 SP 期望不同的属性名或额外字段，在[字段映射](field-mappings.md)中配置。

### 第四步：测试

从 SP 发起登录。在 Portico 完成认证后，SP 的 ACS URL 会收到签名断言。
如果 SP 报签名错误，重新从 Portico 拉取 metadata——证书可能已轮换。

---

## Gitea

Gitea 支持包括 OIDC 在内的多种认证来源。

### 在 Portico 注册

```bash
portico client register \
  --id gitea \
  --name "Gitea" \
  --redirect-uri https://gitea.example.com/user/oauth2/portico/callback
```

复制 client secret。

### 在 Gitea 配置

**站点管理 → 认证来源 → 添加认证来源**：

| 字段 | 值 |
|---|---|
| 认证类型 | OAuth2 |
| 名称 | `portico`（出现在回调 URL 中） |
| OAuth2 提供商 | OpenID Connect |
| Client ID | `gitea` |
| Client 密钥 | 上面的 secret |
| OpenID Connect 自动发现 URL | `https://<host>/.well-known/openid-configuration` |

保存。Gitea 显示的回调 URL（`/user/oauth2/portico/callback`）必须与 Portico 中注册的一致。

登录页会出现 **Sign in with portico** 链接。

---

## 未列出的应用

如果你的应用支持 OIDC 但未在列表中，[通用 OIDC 应用](#通用-oidc-应用)涵盖了通用流程。
支持"generic OAuth2"或"OIDC"的应用大多遵循相同的四步：
注册客户端、指向 issuer、启用 PKCE、测试。

SAML 应用参见[通用 SAML 2.0 应用](#通用-saml-20-应用)。

遇到错误时，[排查手册](troubleshooting.md)列出了常见错误信息及原因。
