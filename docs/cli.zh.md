# CLI 参考

`portico` 是一个单一可执行文件。不带参数运行时，它启动服务端；所有子命令作为短
生命周期进程运行，连接数据库后退出。

所有子命令读取 `PORTICO_DB_DSN`——与服务端使用的连接字符串相同。不需要其他配置。

---

## portico（服务端）

```
portico
```

启动服务端。配置完全通过环境变量进行，详见[配置参考](configuration.md)。

```
portico --version   打印版本后退出
portico --help      打印用法后退出
```

---

## portico tenant

租户从命令行创建，因为没有任何账号能在自己的租户之外操作，API 无法授权这一操作。

```
portico tenant create --code <code> [--name <name>]
                      [--admin-username <name>] [--admin-password <password>]
portico tenant list
portico tenant enable  --code <code>
portico tenant disable --code <code>
```

### tenant create

创建一个租户及其第一个管理员账号。

| 参数 | 默认值 | 说明 |
|---|---|---|
| `--code` | 必填 | 登录时使用的短代码，也用于租户专属 URL |
| `--name` | 同 code | 显示名称 |
| `--admin-username` | `admin` | 第一个管理员的用户名 |
| `--admin-password` | 自动生成 | 密码。省略时使用文档默认密码，首次登录必须立即修改 |

非默认租户的登录地址为 `{PORTICO_PUBLIC_URL}/t/<code>`，默认租户直接是
`{PORTICO_PUBLIC_URL}`。

### tenant list

列出所有租户，含代码、名称、状态和创建日期。

### tenant enable / disable

停用租户会拒绝所有登录，但不删除任何数据。`enable` 可以撤销此操作。

| 参数 | 必填 | 说明 |
|---|---|---|
| `--code` | 是 | 租户代码 |

---

## portico client

注册和管理通过 Portico 登录的 OIDC/OAuth 2.1 应用。控制台也能完成这些操作；CLI
适用于初次部署、脚本化场景以及控制台无法访问时。两条路径使用相同的服务层，产生
相同的审计记录。

```
portico client register --id <client-id> --redirect-uri <uri> [--redirect-uri <uri>]
                        [--tenant <code>] [--name <name>] [--public]
                        [--type WEB|NATIVE|USER_AGENT]
                        [--post-logout-redirect-uri <uri>] [--scope <scope>]
                        [--launch-url <url>] [--logo-uri <url|path>]
portico client list        [--tenant <code>]
portico client enable      --id <client-id> [--tenant <code>]
portico client disable     --id <client-id> [--tenant <code>]
portico client rotate-key  [--tenant <code>]
```

所有子命令的 `--tenant` 默认为默认租户。

### client register

| 参数 | 默认值 | 说明 |
|---|---|---|
| `--id` | 必填 | 应用发送的 `client_id` |
| `--redirect-uri` | 必填，可重复 | 授权码的投递地址。精确匹配，无通配符 |
| `--tenant` | 默认租户 | 租户代码 |
| `--name` | 同 id | 显示名称 |
| `--public` | false | 浏览器或移动端客户端。无法保密，改用 PKCE 验证 |
| `--type` | `WEB` | `WEB` \| `NATIVE` \| `USER_AGENT` |
| `--post-logout-redirect-uri` | 无，可重复 | 注销后跳转地址 |
| `--scope` | `openid profile email`，可重复 | 允许的 scope |
| `--launch-url` | 无 | 用户打开该应用的地址，用于主屏显示 |
| `--logo-uri` | 无 | 应用图标：`https://` URL，或服务端相对路径如 `/icons/wiki.svg` |

机密客户端（不带 `--public`）会获得一个 secret，只向 stderr 打印一次。存储的是
哈希值，secret 无法还原——轮换时需重新注册。

所有客户端都必须使用 PKCE，包括机密客户端。这是 OAuth 2.1 的要求。不携带
`code_challenge` 的请求会被拒绝。

### client list

列出已注册客户端，含 id、名称、类型（公开/机密）、状态和重定向 URI。

### client enable / disable

| 参数 | 必填 | 说明 |
|---|---|---|
| `--id` | 是 | 客户端 id |
| `--tenant` | 否 | 租户代码 |

### client rotate-key

替换租户的签名密钥。旧密钥保留在已发布的密钥集（`/api/v1/jwks`）中，直到它签发的
令牌全部过期，因此轮换不会让任何有效会话失效。

| 参数 | 必填 | 说明 |
|---|---|---|
| `--tenant` | 否 | 租户代码 |

---

## portico sp

注册和管理 SAML 2.0 服务提供方。

```
portico sp register           --metadata <file|url> [--tenant <code>] [--name <name>]
                              [--launch-url <url>] [--logo-uri <url|path>]
portico sp list               [--tenant <code>]
portico sp enable             --entity-id <id> [--tenant <code>]
portico sp disable            --entity-id <id> [--tenant <code>]
portico sp certificate        [--tenant <code>]
portico sp rotate-certificate [--tenant <code>]
```

所有子命令的 `--tenant` 默认为默认租户。

Portico 自身的 metadata 地址：默认租户为 `{PORTICO_PUBLIC_URL}/saml/metadata`，
其他租户为 `/t/<code>/saml/metadata`。将该 URL 交给服务提供方，这是交换的另一半。

### sp register

注册接受的是服务提供方的 metadata 文档——而不是逐一填写字段。该文档由服务提供方
自身发布，包含协议所需的全部信息：entity id、断言消费者服务端点、接受的 NameID
格式。

| 参数 | 默认值 | 说明 |
|---|---|---|
| `--metadata` | 必填 | 本地文件路径，或 `https://` URL。拒绝纯 HTTP |
| `--tenant` | 默认租户 | 租户代码 |
| `--name` | entity id | 显示名称 |
| `--launch-url` | 无 | 用户打开该应用的地址，用于主屏显示 |
| `--logo-uri` | 无 | 应用图标：`https://` URL，或服务端相对路径 |

### sp list

列出已注册服务提供方，含 entity id、名称、状态和断言消费者服务 URL。

### sp enable / disable

| 参数 | 必填 | 说明 |
|---|---|---|
| `--entity-id` | 是 | 服务提供方的 entity id |
| `--tenant` | 否 | 租户代码 |

### sp certificate

以 PEM 格式打印当前 SAML 签名证书。

### sp rotate-certificate

生成新的签名证书，将当前证书标记为已退役（不删除）。每个服务提供方都必须用新证
书重新配置，才能接受下一次断言；在此之前，旧证书是它们需要查找的那个。

---

## portico cas

注册和管理 CAS 服务。

```
portico cas register --url <prefix> [--tenant <code>] [--name <name>]
                     [--launch-url <url>] [--logo-uri <url|path>]
portico cas list     [--tenant <code>]
portico cas enable   --url <prefix> [--tenant <code>]
portico cas disable  --url <prefix> [--tenant <code>]
```

所有子命令的 `--tenant` 默认为默认租户。

CAS 端点地址：默认租户为 `{PORTICO_PUBLIC_URL}/cas/...`，其他租户为
`/t/<code>/cas/...`。将客户端的 CAS server URL 指向 `/login` 之前的部分。

### cas register

`--url` 是 URL 前缀，不是模式。`service=` 参数以该值开头时匹配。注册始终按路
径边界进行：`https://app.example.com/` 不会匹配 `https://app.example.com.elsewhere.test`。

| 参数 | 默认值 | 说明 |
|---|---|---|
| `--url` | 必填 | 服务 URL 前缀 |
| `--tenant` | 默认租户 | 租户代码 |
| `--name` | 同前缀 | 显示名称 |
| `--launch-url` | 无 | 用户打开该应用的地址，用于主屏显示 |
| `--logo-uri` | 无 | 应用图标：`https://` URL，或服务端相对路径 |

### cas list

列出已注册服务，含 URL 前缀、名称和状态。

### cas enable / disable

| 参数 | 必填 | 说明 |
|---|---|---|
| `--url` | 是 | 已注册的 URL 前缀 |
| `--tenant` | 否 | 租户代码 |

---

## portico trial

管理自助试用表单创建的租户。这些命令同样需要 `PORTICO_DB_DSN`。试用租户的管理
也在命令行完成，原因与 `portico tenant` 相同：没有账号能在自己的租户之外操作。

```
portico trial list
portico trial delete --code <code> --yes
portico trial prune
```

### trial list

列出已确认试用产生的所有租户：代码、名称、状态、行业、申请地址、确认时间。手工
用 `portico tenant create` 创建的租户不会出现。

### trial delete

删除一个试用租户及其全部数据：账号、组织、应用、审计记录。**不可撤销。**

| 参数 | 必填 | 说明 |
|---|---|---|
| `--code` | 是 | 租户代码 |
| `--yes` | 是 | 确认删除。必填，防止脚本误删 |

拒绝操作非试用创建的租户：默认租户和通过 `portico tenant create` 创建的租户均
不在可操作范围内。

### trial prune

删除链接过期但从未被确认的试用申请，释放它们占用的租户代码。运行中的服务端每小
时自动执行一次；该命令用于没有服务端运行时，或需要立即释放代码时。

---

## portico ready

```
portico ready [--url <base-url>]
```

询问运行中的实例是否可以提供服务。就绪则以状态码 0 退出，否则以 1 退出。

| 参数 | 默认值 | 说明 |
|---|---|---|
| `--url` | `http://127.0.0.1:<port>` | 要检查的实例基础 URL。端口取自 `PORTICO_ADDR` |

发布镜像使用 `FROM scratch`——没有 shell，没有 `curl`。该命令是容器健康检查唯一
可以运行的可执行文件，因为它是镜像中包含的唯一内容。

它通过 HTTP 向实例发起请求，而不是直接连接数据库：关键在于正在提供服务的进程能
否访问其依赖，而不是一个新建连接能否做到。
