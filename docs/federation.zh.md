# 单点登录：OpenID Connect、SAML 2.0 与 CAS

另一个应用怎么通过 Portico 让人登录，以及登录之后它能指望什么、不能指望什么。

三种协议，一套账号。用哪一种由**这个应用本来就会讲哪一种**决定，而不是由这里的任何
东西决定：现代应用用 OpenID Connect，企业产品通常有一个多年前写好的 SAML 集成，大学
或 Java 应用往往用 CAS。**三者回答的是同一批事实**——租户、角色、组织，以及这个人本身的信息
——所以一个从其中一种迁到另一种的应用，能知道的东西不会变少。**名字是不同的**，因为
每种协议都有自己的一套：[同一个人，三套名字](#同一个人三套名字)就是用来对照的那张表。

下面依次是 OpenID Connect、[SAML](#saml-20)、[CAS](#cas)。

## 简短版本

Portico 是一个 OpenID Provider。注册你的应用——在控制台的**应用管理**里，或者从命令
行——把你的 OIDC 库指向 issuer，就完事了：**没有任何 Portico 专有的代码要写。**

```bash
portico client register --id grafana --name "Grafana" \
  --redirect-uri https://grafana.example.com/login/generic_oauth
```

```
issuer:  https://id.example.com
```

其它一切——端点、密钥集、有哪些 grant 和 scope——都在发现文档里：
`https://id.example.com/.well-known/openid-configuration`，那也正是你的库在启动时会
读的东西。

## Issuer

每个租户都是它自己的 issuer，有自己的发现文档、自己的密钥集、自己的账号：

```
https://id.example.com/t/<tenant-code>
```

默认租户额外在根路径上也提供一份，这样**单租户部署拥有人们预期的那个 issuer，也永远
不必向集成方解释多租户是怎么回事**：

```
https://id.example.com
```

**租户在 URL 里而不是在一个 claim 里**，因为正是这一点让一个为某租户签发的令牌对另一
个租户不可用。依赖方会检查 `iss` 并拉取该 issuer 指明的密钥集；这两件事每个 OIDC 库
本来就在做。**而一个共享 issuer 加一个 `tenant` claim，只有在每个集成方都额外写代码
去检查那个 claim 时才安全——而他们都不会写，因为没有任何标准库要求他们写。**

默认租户的两个挂载点是**同一批账号、同一批密钥之上的两个不同 issuer**。一个令牌写明
它是从哪一个获得的，并且只对那一个校验通过。

## 实现了什么

| | |
|---|---|
| Grant | 授权码，**强制 PKCE** |
| PKCE 方法 | 仅 `S256` |
| 签名 | RS256，公钥在 issuer 的 `/keys` |
| 访问令牌（access token） | JWT。默认 15 分钟，可在**系统设置**里改，取值 1–60 |
| ID 令牌（ID token） | 跟随访问令牌 |
| Refresh token | 默认 30 天，可改，取值 1–90；**每次使用都轮换** |
| 最长会话时长 | 默认关闭。开启后，一条刷新链从最初那次登录起算满这么多天即终止，无论中间刷新得多勤 |
| 客户端认证 | `client_secret_basic`、`client_secret_post`，或无（公共客户端） |
| Scope | `openid`、`profile`、`email`、`phone`、`offline_access` |
| 端点 | discovery、authorize、token、userinfo、introspect、revoke、end_session、keys |

**发现文档写的就是这些。** Portico 所基于的那个协议库会用它自己的默认值而不是配置去
填其中三个字段——否则它会宣告 implicit 流程、JWT-bearer grant，以及一个根本没挂载的
设备授权端点——所以**文档在发布之前被修正过**。一个客户端只会在没人盯着的时候、从那
个文件里配置自己一次；**文件里任何不实之处，都会在之后、在别的地方、以一个没人能追溯
回它的错误的形式暴露出来。**

**刻意不实现**：implicit 与 hybrid 流程、device 与 client-credentials grant、动态客户
端注册、front-channel logout、DPoP、PAR，以及 `private_key_jwt` 客户端认证。

implicit 与 hybrid 流程把令牌放进 URL，这正是 OAuth 2.1 移除它们的原因。**机密客户端
同样强制 PKCE**，理由与 2.1 要求它的理由相同：**一个在浏览器和应用之间被截获的授权码，
没有 PKCE 就可以被兑换，而客户端是否持有 secret 改变不了这一点。**

### 没有授权确认页

每一个客户端都由管理员审核并注册——在控制台里或命令行里，**永远不是由应用自己**——
所以**不存在需要人去同意的第三方**；这些应用就是这个部署自己的应用。一个为某应用登录
Portico 的人看到的是普通的登录页，然后被送回应用；没有任何东西要求他批准一个 scope。
如果 Portico 将来接受未经审核的客户端，这一点就会改变，**而那将是一次刻意的改变，不
是一次疏忽**。

## 注册一个应用

两条等价的路径，都只对租户管理员开放，也都被审计：控制台的**应用管理**页面，或者下面
的命令。**它们走的是同一个服务**，所以校验规则和审计轨迹两边完全一致。控制台还会额外
显示需要配到对端的那些地址，取自正在运行的部署。

动态客户端注册（[RFC 7591](https://www.rfc-editor.org/rfc/rfc7591)）**刻意缺席**：
那是由一个匿名调用方完成的注册，**没有管理员在环路里**。

命令行仍然是这些情况下的答案：一次还没有人登录过的首次部署、脚本化，以及控制台够不到
的时候。

```bash
# 一个服务端应用。secret 只打印一次，存下来的是哈希。
portico client register --id grafana --name Grafana \
  --redirect-uri https://grafana.example.com/login/generic_oauth

# 一个浏览器或移动端应用，它保不住秘密。它不会拿到 secret，
# 仅靠 PKCE 完成认证。
portico client register --id console --name Console --public \
  --redirect-uri https://console.example.com/callback

portico client list
portico client disable --id grafana    # 拒绝新的登录；不删除任何东西
```

加 `--tenant acme` 可以注册到默认租户之外的租户里。

默认 scope 是 `openid profile email`。**需要刷新令牌（refresh token）的客户端必须同时用
`--scope offline_access` 注册**——**一个客户端没有注册过的 scope 会被丢弃而不是被拒绝**，
所以症状是一个不含 `refresh_token` 的令牌响应，而且哪里都没有错误：

```bash
portico client register --id grafana --name Grafana \
  --scope openid --scope profile --scope email --scope offline_access \
  --redirect-uri https://grafana.example.com/login/generic_oauth
```

回调地址**精确匹配**，并在注册时校验：通配符、fragment，以及非回环的 `http://` 一律
拒绝。回环的 `http://` 是允许的，因为一个原生应用的回调**无处可去**。

### 磁贴上的那张图

选填，不填则磁贴显示名称首字——这个兜底清晰可读、不需要网络、也不会坏。图片买到的是
**辨识度**：在门户上找一个应用，认图标远快于读完六个名字。

三种给法，最终都落到同一个字段：

| | |
|---|---|
| 上传 | **PNG 或 JPEG**，最大 512 KiB、边长不超过 1024 像素。存在数据库里，从 `/t/<租户>/logos/<id>` 提供 |
| 本服务器上的路径 | 你自己放上去的任何文件，例如 `/icons/wiki.svg` |
| 完整的 `https` 地址 | 由浏览器去它指向的地方取 |

**SVG 不能上传**，而拒绝的理由不是这个格式冷门。SVG 是**文档**，可以携带脚本；而上传的
文件会由本服务器自己的地址提供——也就是管理控制台所在的那个地址。通过 `<img>` 渲染时
浏览器不会执行那段脚本，这也正是 SVG **路径**仍然被接受的原因：运维放到 `/icons` 下的
文件是他自己挑的。但从网页表单传上来的文件同样可以被**直接在标签页里打开**，那时它是一
个带着本源 cookie 的页面。`<img>` 那条路的安全性是某一个组件渲染方式的性质，而一份仅
因为"某处恰好那样渲染"才安全的存储数据，对下一个改那个组件的人就是陷阱。

第三种给法有个用之前该知道的后果：本服务器发送的 `Content-Security-Policy` 只允许
`self` 和 `data:` 两种来源的图片，所以**指向另一台主机的完整地址在浏览器里不会渲染**，
即使它在注册时被接受。前两种是同源的，不受影响。

最终没有被任何应用引用的上传，一天后会被清掉。上传必须发生在表单保存之前，所以取消表单
会留下文件，换图标也会留下被换掉的那一张。

## Claim

除标准 claim 之外，每个令牌都携带下游系统"给这个人定位"所需的东西：

| Claim | 含义 |
|---|---|
| `tenant_id`、`tenant_code` | 账号属于哪个租户 |
| `role` | `SUPER_ADMIN` 或 `USER` |
| `organization_id`、`organization_name` | 账号有组织时才出现 |

这些在 ID 令牌、访问令牌和 userinfo 响应里**都有**。依赖方从 ID 令牌读身份，资源
服务器从访问令牌读；**一个只出现在其中之一里的 claim，是一半集成方看不见的 claim**。

`email_verified` 和 `phone_number_verified` **永远是 `false`**。本版本从不要求任何人
证明一个地址，**而一个声称相反情况的提供方，是在对一个可能据此行动的依赖方撒谎**。

## 经由别人的提供方登录

上面全部是 Portico 作为签发方。这一节是**反过来的方向**：别人运行的一个 OpenID
Provider，被信任来说明"这个人是谁"。

在**身份提供方**页面里配置——它和应用管理、事件订阅在同一组菜单里，因为这四者都是
"把另一个系统接上来"，只不过这一个的方向是往里。你要提供 issuer、在对方那里注册的
client id 与 secret，以及界面上显示的回调地址——**逐字符复制**，对方是按字符匹配
的。保存时会去联系那个 issuer，所以一份 discover 不出来的配置会在表单上被拒绝，而
不是三天后在某个人的登录页上。

那个回调地址是**控制台的地址**——`/external/callback`，非默认租户则是
`/t/<租户编码>/external/callback`——而不是真正完成登录的那个 API 端点。从提供方回
来是一次整页跳转，所以谁来应答，谁就是人看到的东西；而那个端点应答的是 JSON。于是
由控制台接住这次落地，从自己的地址里读出 `state` 与 `code`，再拿它们去调那个端
点——顺带也让签发出来的会话不必出现在 URL 里。

**必须是公网 HTTPS 地址。** 规则和 webhook 目的地是同一套，理由也一样：这个地址由
租户管理员填写，而由本服务器去抓取。

### 外部身份不会创建账号

一个身份还没在这里关联过的人，第一次来会被拒绝，并被告知"先用密码登录，再去个人
中心关联"。**这一版永远不会因为一次外部登录而创建账号** —— 自助注册和它下面那个
"必须先确认邮箱"才是决定谁能拥有账号的两个开关，而一个会悄悄建号的登录按钮，把这
两个都绕过去了。

所以一个人的顺序是：照旧登录 → 在**个人中心**里关联 → 从此那个按钮可用。

### 唯一能把账号交给陌生人的那个开关

**信任该提供方已验证的邮箱**，会让第一次来的人按地址匹配到一个已有账号，而不是被
拒绝。**它默认关闭**，而且除非那个提供方是你自己运行的、或者你清楚它怎么验证邮箱，
否则就该一直关着。

一个不验证邮箱的身份提供方，会让任何人注册 `ceo@你的公司.example`，然后拿着一个
这么写的令牌过来。**如果只凭地址就能进一个已有账号，那就是整个账号。**

开着的时候，**地址只被查这一次**：身份在那一次就被关联上，从下一次起靠对方的
subject 找账号。**对方那边之后改了邮箱、或者把邮箱重新分配给别人，都改不了已有的
关联指向。**

### 回来的路上查什么

`state` 被读取它的那条语句同时删除，所以重放的回调什么也找不到。ID 令牌里的 `nonce`
要和登录出发时存下的那个比对。签名、`iss`、`aud`、有效期由对方公布的密钥校验。而
"没匹配上"的所有原因合并为同一个错误——能区分它们的调用方，可以用这个差别去探测哪些
state 存在过。

**这一趟是登录还是关联，在出发时就定好并记在服务端**，绝不从回来的东西里读。一个能
自称是哪一种的回调，就是一条构造的链接能撒谎的地方。

### 移除一个提供方

**停用**只是把按钮从登录页撤下，所有关联都留着——那是对方故障时用的。**删除**会解绑
所有经它进来的人，审计条目里记着有多少个。

两种都不会把人锁在外面：这一版每个账号都还有密码，所以关联是便利，不是唯一入口。


## 关于撤销，说实话

三件事会结束一个会话：退出登录、改密码、管理员停用账号。三者都会**立即撤销每一个
依赖方持有的刷新令牌**。它们的区别在于，各自结束了多少个 Portico 自己的会话：

| | Portico 自己的会话 | 依赖方的刷新令牌 |
|---|---|---|
| 退出登录 | 只有正在退出的那一个 | 全部 |
| **退出所有设备** | 全部 | 全部 |
| 改密码 | 全部 | 全部 |
| 管理员停用账号 | 全部 | 全部 |

**退出登录只结束正在退出的那个会话，手机上的那个不动**——因为那才是一个人关掉浏览器
时预期的事；「全部结束」是一个单独的动作，有它自己的按钮。而依赖方那一列每行都相反：
**在一个单点登录系统里，「退出」被理解为「退出我登进去的那些东西」，做得比这少，才是
真正会让人意外的那种失败。**

**它们都无法收回一个已经签发出去的访问令牌。** 那不是 Portico 的缺口；**那就是"签发
自校验令牌"这件事本身的含义**。资源服务器校验签名和过期时间，从不回调——而这正是选择
联邦而不是代理每一个请求的全部理由。有两件事给它划了边界：

- 访问令牌的有效期，默认十五分钟、最长不超过一小时——上限的存在是因为这个到期时间是
  约束一份已被收回的权限的唯一手段，见 SECURITY.md，以及
- issuer 的 `/oauth/introspect` 端点对一个已停用账号**立刻**回 `active: false`，供那
  些需要比过期更早拿到答案的资源服务器使用。

**"退出 Portico 时一并撤销依赖方的刷新令牌"是一个选择，不是一项义务。** 不动它
们也说得过去——那正是应用自己的 `end_session` 端点的用处。Portico 不那样做，是因为在
一个单点登录系统里，**点下"退出"的那个人理解的就是"退出我登录进去的那些东西"，而做得
比这更少，才是真正要命的意外**。

### Refresh token 轮换

每一次刷新都会铸一个新令牌、并把旧的用掉。**出示一个已被用掉的令牌意味着有副本泄露了**
——合法持有者手上会是那个替换件——所以处理方式是**撤销整条链**而不是让这一次调用失败，
因为**哪一环泄露的是无法知道的**。

一次刷新还会重新检查账号是否仍然启用，所以那三十天是一个**上界，不是一个承诺**。

## 签名密钥

每个租户有自己的 RSA 密钥，**在首次使用时生成而不是在启动时**：大多数租户从不做联邦，
每个都生成一把是白付的代价。

```bash
portico client rotate-key --tenant acme
```

轮换会签发一把新密钥并让旧的退役。**退役的密钥会在已发布的密钥集里再留 24 小时**，
这样用它签过的令牌在全部过期之前仍能校验通过，之后才被删除。

## 每种协议下，撤销到底能到达什么

| | 退出登录 / 改密码 / 停用 |
|---|---|
| Portico 自己的会话 | 立即结束——退出登录结束正在退出的那一个，另外两件结束全部；见上表 |
| OIDC 刷新令牌 | 被撤销 |
| OIDC 访问令牌 | **无法收回**；等它到期（默认十五分钟，最长一小时），或者用 introspect |
| SAML | **没有东西可撤销**——不存在服务端会话，因为没有单点登出。服务提供方自己的会话完全不受影响，按它自己的规则结束。 |
| CAS | **没有东西可撤销**——一张票只活一分钟且一次性，而且没有 ticket-granting ticket。某个服务自己的会话与 SAML 一样，是它自己的事。 |
| 外部身份提供方 | **没有东西可撤销，而且什么也到不了对方。** 退出 Portico 不会把任何人从 Google 或 Entra 退出去；下一次点那个按钮会立刻重新登进来，因为对方仍然持有它自己的会话。这不是这里的缺口——**这就是这个方向上的联邦是什么意思**，和上面两行是同一件事，只是换了一侧看。 |

**最后两行值得在部署前读两遍。** 在 Portico 里结束一个会话，**并不会结束应用在接受了
一份断言或一张票之后为自己创建的那个会话**；没有一个可用的单点登出规约，任何身份提供
方都做不到这件事，而本版本没有。

## 已知限制

- **过期的刷新令牌会在过期后再多活三十天。** 每小时的清理只有在一条轮换链里的
  每一个令牌都既已过期、又已过期满三十天时才移除它，而且只按整条链移除。**在过期当天
  就删掉那一行会破坏重用检测**：一个**既已过期、又已被用掉**的令牌被出示时，仍然会触
  发整条链的撤销，而这正是一个被盗的刷新令牌被抓住的方式。**那一行是证据，所以
  它比凭据活得久。**
- **访问令牌无法被撤销。** 见上；`/revoke` 端点会接受它们并按 RFC 7009 的要求成功应答，
  但真正被撤销的只有刷新令牌。
- **没有授权确认页**，如上所述。
- **没有 `private_key_jwt`**，所以一个只能用这种方式认证的客户端无法被注册。
- **SAML 与 CAS 都没有单点登出**，因此没有办法结束应用为自己创建的会话。见上表。
- **没有 SAML 身份提供方发起的登录**，CAS 也没有代理票。

## SAML 2.0

Portico 是一个 SAML 身份提供方。把 Portico 的元数据交给服务提供方，再把服务提供方的
元数据注册到 Portico——**整个交换就是这些**，两份文档带着双方各自需要的一切。

这个交换的两侧都可以在控制台的 **应用管理 → SAML 2.0** 里完成，它接受上传或粘贴的元
数据文档，并提供 Portico 自己的元数据与证书供复制。**Portico 从不去你给的 URL 拉取元
数据**：那会让服务器向调用方指定的地址发起请求，**而那是一次针对它所能到达的其它一切
的服务端请求伪造（SSRF）**。

```bash
# Portico 的元数据，配到服务提供方那边：
#   {PORTICO_PUBLIC_URL}/saml/metadata           默认租户
#   {PORTICO_PUBLIC_URL}/t/<code>/saml/metadata  其它租户

# 服务提供方的元数据，配到 Portico 这边：
portico sp register --metadata ./sp-metadata.xml --name "Confluence"
portico sp list
portico sp disable --entity-id https://confluence.example.com/saml
```

`--metadata` 接受一个文件或一个 `https://` 地址。**纯 `http` 会被拒绝**：这份文档写明
了断言要投递到哪里，所以路径上的任何人都能把它们改道。

### 实现了什么

| | |
|---|---|
| 规约 | Web browser SSO，服务提供方发起 |
| 绑定 | 入站 HTTP-Redirect 与 HTTP-POST，出站 HTTP-POST |
| 签名 | 对 Response 做 RSA-SHA256 |
| 加密 | 只要服务提供方在元数据里发布了加密密钥，断言就会被加密 |
| NameID | Persistent，其值是账号 id |
| 断言有效期 | 5 分钟 |
| 登录时限 | 15 分钟 |

**名称标识符是账号 id，不是用户名。** 用户名可以被管理员改掉，**而一个用它作为本地
记录键的服务提供方，会在改名的那一天悄悄为同一个人再建一个账号**。用户名放在 `uid`
属性里供显示。

有既定约定的属性用 OASIS X.500 的名字，没有约定的用 Portico 自己的。每个属性带
**两个**名字，而这个区别在配置服务提供方时是要紧的：友好名是给读断言的人看的标签，
**Name 才是映射真正匹配的那个字符串**。

| SAML 属性 | 服务提供方实际映射的 Attribute Name |
|---|---|
| `uid` | `urn:oid:0.9.2342.19200300.100.1.1` |
| `displayName` | `urn:oid:2.16.840.1.113730.3.1.241` |
| `cn` | `urn:oid:2.5.4.3` |
| `mail` | `urn:oid:0.9.2342.19200300.100.1.3` |
| `telephoneNumber` | `urn:oid:2.5.4.20` |
| `tenantId` | `tenant_id` |
| `tenantCode` | `tenant_code` |
| `role` | `role` |
| `organizationId` | `organization_id` |
| `organizationName` | `organization_name` |
| `urn:oasis:names:tc:SAML:attribute:subject-id` | `urn:oasis:names:tc:SAML:attribute:subject-id` |

最后一个没有友好名，因为定义它的那份 profile 就没给。它携带账号 id，与名称标识符
是同一个值，供那些遵循 subject identifier profile、而不是去读 NameID 的服务提供方
使用。

**这份清单就是全部，而且每个名字只出现一次。** `cn` 与 `displayName` 携带同一个值，这不
是疏忽：**它们是服务提供方真正会拿来映射「人名」的那两个名字，而它们彼此并不一致。**

签名的构造与校验**完全**交给 [crewjam/saml](https://github.com/crewjam/saml) 和
[goxmldsig](https://github.com/russellhaering/goxmldsig)，后者被钉在比 crewjam 解析出
来的更新的版本上，因为**整件事都压在这段代码上**。Portico 自己不构造也不校验任何 XML
签名；**手搓一个，是交付一个会接受伪造断言的 SAML 实现最可靠的办法。**

### 一次登录是怎么接回去的

认证请求是以一次普通的浏览器跳转到达的，上面没有任何凭证，所以 Portico 先把请求存下
来，把浏览器送到自己的登录页，等登录完成后再把协议接着走完。这里涉及三个地址，它们的
敏感程度并不相同：

1. `/t/<code>/saml/sso` —— 服务提供方把浏览器送到这里。
2. `/login?saml_request=<id>` —— Portico 自己的登录页，并告知它要完成哪一个请求。
   **请求 id 就在这个 URL 里**，也就是说它在浏览器历史里，也在沿途任何一台代理的日志里。
3. `/t/<code>/saml/sso/callback` —— 铸造断言并把它转投给服务提供方。

第三个地址是断言真正被创建的地方，而它没法要求凭证：一次顶层导航没有地方放。所以它必
须用别的方式认出这个浏览器，而请求 id 不能充当这个角色 —— id 是好几处日志里都有的东西。

因此，完成一个请求时会同时签发一个一次性 secret。它在 `POST
/api/v1/saml/authenticate`（也就是登录完成后控制台发起的那次**已认证**调用）里生成，
只出现在那一次的响应里，并被拼进控制台随后要跳转的回调地址。回调必须带上它，比对是常量
时间的；库里存的是 SHA-256，与授权码同等对待。

于是从日志里捞到的 id，就只是一个 id。一次失败的尝试也**不会**消耗掉这个请求 —— 若在
不匹配时就删掉它，那么任何拿到泄露 id 的人都能毁掉一次正在进行的登录，等于用一个容易
的攻击换掉一个困难的攻击。

这件事在 OpenID Connect 那条流程里没有对应物，其中的差别值得说清楚：那个回调把授权码交
给依赖方**注册过的**地址，所以攻击者拿到它的 id 什么也得不到。而 SAML 断言是交给**发起
请求的那个浏览器**再由它转投出去的 —— 这正是"调用者是谁"在这里要紧、在那里不要紧的原因。

### 刻意不实现

- **身份提供方发起的登录。** 没有一个请求可以用来与断言做关联，**这会让一份被盗后重放
  进登录流程的断言与真的那份无法区分**。
- **单点登出。** 该规约要求身份提供方在浏览器里触达此人登录过的每一个服务提供方，并
  处理其中任何一个不可达的情况。**一个半吊子的实现比没有更糟，因为它会报告自己结束了
  一些其实没结束的会话。** 元数据里如实说明这一点，而不是宣告一个会 404 的端点。
- **签名的认证请求**、artifact resolution，以及属性查询。

### 证书

每个租户有自己的证书，首次使用时生成，有效期十年。

```bash
portico sp certificate                  # 打印出来，给服务提供方
portico sp rotate-certificate           # 生成一张新的
```

轮换会让旧证书退役，**并且什么都不删除**。这是 SAML 与 OpenID Connect 在**性质上**而
非细节上唯一不同的地方：依赖方会自己去重新拉取密钥集，所以一把 OIDC 密钥可以退役、并
在一天后被丢弃；**而服务提供方是把证书敲进它自己的配置里的，并且没有任何办法得知有了
新的**。**每一个服务提供方都必须被人手工重新配置，之后它才会接受下一份断言**，而在每
一个都完成之前，运维人员需要能查到的正是上一张证书。这就是为什么两者存在不同的表里，
也是为什么 `sp rotate-certificate` 是一条独立于 `client rotate-key` 的命令。

## CAS

Portico 支持 CAS 2.0 和 3.0。在控制台 **应用管理 → CAS** 里注册这个服务，或者用
`portico cas register`，然后把客户端的 CAS 服务器地址指向 `/login` 之前的那一段：

```
{PORTICO_PUBLIC_URL}/cas             默认租户
{PORTICO_PUBLIC_URL}/t/<code>/cas    其它租户
```

```bash
portico cas register --url https://wiki.example.com/ --name Wiki
portico cas list
portico cas disable --url https://wiki.example.com/
```

`--url` 是一个**前缀**，不是一个模式。一个 `service` 参数只有在以注册值开头**且这个
前缀正好停在一个路径边界上**时才匹配，所以 `https://app.example.com/` 永远不会覆盖
`https://app.example.com.somewhere-else.test`。**没有通配符**，注册时会把末尾的分隔符
规范化补上，而查询串、fragment 以及跨网络的纯 `http` 都会被拒绝——**一个 service URL
就是 CAS 的回调地址，所以它享受同样的待遇。**

| | |
|---|---|
| 端点 | `/cas/login`、`/cas/logout`、`/cas/serviceValidate`、`/cas/p3/serviceValidate` |
| 票 | `ST-` 前缀，一次性，一分钟 |
| 属性 | 仅 CAS 3.0，用 CAS 自己的名字——见[同一个人，三套名字](#同一个人三套名字) |

**一张票被绑定在它被签发给的那个服务上**：否则，把它拿到另一个服务的校验端点去出示，
会让一个合法收到票的服务得以在别处冒充那个人。**校验永远返回 `200`，即使是失败**，因
为规范就是这么说的，而且好几个客户端一看到别的状态码就不再往下读，转而报一个传输错误
而不是真正的原因。

**没有 ticket-granting ticket。** CAS 把它放在一个长效 cookie 里，好让浏览器不必重新
登录就能取到后续的票；**Portico 已经有一个会话在做这件事**，而第二个长效凭据会成为退
出登录、改密码、停用时**第三个需要撤销的东西**。搭在既有会话上，意味着那三件事已经把
它覆盖了。

`/cas/logout` 重定向到 Portico 自己的登录页，**因为会话真正在的地方是那里**——一次普通
的页面跳转够不到 Web 应用持有的令牌，所以应用是在到达时才登出的。`service` 参数**刻意
不被跟随**：规范把它定为可选并对它提出了警告，**而一个会重定向到调用方指定的任何地方
的端点，就是一个披着协议外衣的开放重定向。**

不实现：代理票，以及 CAS 1.0 的 `/validate`——它那句光秃秃的 `yes\n<user>\n` 既不带属性，
也没有办法说明一张票为什么失败。

## 同一个人，三套名字

三种协议携带的是同一批事实，各自用自己的一套名字。**要映射，就照这张表。**

它们不是同一套名字，而且做不到：OpenID Connect 的名字是它规范定的，而服务提供方和
CAS 客户端都按拿到的名字去映射——**统一成一套自家风格，意味着每一次对接都要为每个
目录本来就在发布的事实手写一遍映射。**

| 事实 | OpenID Connect 声明 | SAML 属性 | CAS 3.0 属性 |
|---|---|---|---|
| 账号 id | `sub` | 名称标识符，以及 `urn:oasis:names:tc:SAML:attribute:subject-id` | 不发 |
| 用户名 | `preferred_username` | `uid` | cas:user 元素，它不是一个属性 |
| 显示名 | `name` | `displayName`、`cn` | `displayName` |
| 邮箱 | `email` | `mail` | `email` |
| 电话 | `phone_number` | `telephoneNumber` | `phone` |
| 这两项是否被证实过 | `email_verified`、`phone_number_verified` | 不发 | 不发 |
| 租户 | `tenant_id`、`tenant_code` | `tenantId`、`tenantCode` | `tenant_id`、`tenant_code` |
| 角色 | `role` | `role` | `role` |
| 组织 | `organization_id`、`organization_name` | `organizationId`、`organizationName` | `organization_id`、`organization_name` |
| 最后变更时间 | `updated_at` | 不发 | 不发 |

SAML 这一列是**友好名**。服务提供方真正映射的 Attribute Name 在[上面那张表](#实现了什么_1)。

**一个名字只在账号确实有这项事实时才出现**：没有邮箱就没有 `mail`，没有组织就没有
`organization_id`。**不会发空值**——所以一个映射了却始终收不到的字段，先去看账号，再
去看映射。

OpenID Connect 的声明还需要对应的 scope 被申请过——`phone_number` 只在申请了 `phone`
时才来。**这比本页上的任何一条，都更常是「声明不见了」的真正原因。**

**这张表是被校验的，不是被维护的。** `TestEachProtocolSendsTheNamesTheManualLists`
会用一个拥有全部事实的账号跑通三种协议，把回来的名字与这里写的比对——**双向比对，
且两种语言的本页都比**。它之所以存在，是因为这一节此前声称 CAS 用的名字与另外两种
协议相同，而它从来就不是。

## 本地试一下

注册一个应用，再问服务器它对外声明了什么：

```bash
portico client register --id mock-sp --name "Mock SP" --public \
  --redirect-uri http://localhost:8413/oidc/callback

curl -s http://localhost:8410/.well-known/openid-configuration | jq
```

`examples/mock-sp` 是另一半：一个**带浏览器界面的依赖方**，好让一次登录能被看见，而不
只是被断言。

```bash
go run ./examples/mock-sp
```

然后打开 <http://localhost:8413> 并选择 OpenID Connect。它会离开到 Portico 的登录页，
再回到一个把 ID 令牌的声明与 userinfo 响应**并排摆出来**的页面。

有三处细节决定它能不能一次跑通，而**每一处出错时看起来都像是别的问题**：

- **重定向 URI 必须逐字符相同。** 上面注册的那个、`mock-sp` 发出的那个、浏览器实际访
  问的那个，是同一个字符串，否则登录会以 `invalid_request` 收场。`localhost` 与
  `127.0.0.1` 是同一台主机，**但不是同一个字符串**。
- **`PORTICO_PUBLIC_URL` 必须是浏览器用的那个地址。** 它就是签发者标识，发现文档由它
  构造，而依赖方会核对拿回来的和自己问的是否一致。指向一台公开 URL 写着别的地址的服务
  器，会在**发现阶段、也就是启动时**就失败。
- **请求的 scope 必须是该客户端注册过的 scope。** 两边的默认值都是
  `openid profile email`。要 `offline_access`——也就是要刷新令牌——就得在注册时也带上它。

### 另外两个协议

`mock-sp` 三个协议都说。SAML 与 CAS 各需要注册一次。CAS 那条什么时候跑都行——它是一段
URL 前缀，事先就知道；但 **SAML 那条要用到程序生成的一份文档**，所以先启动一次：

```bash
go run ./examples/mock-sp        # 写出 .mock-sp/，并打印下面这两条命令

portico sp register --metadata .mock-sp/saml-metadata.xml --name "Mock SP"
portico cas register --url http://localhost:8413/cas/ --name "Mock SP"
```

**从这一刻起 SAML 与 CAS 页面就能用了，不需要重启**——注册是 Portico 那一侧的状态，
`mock-sp` 一点都不缓存它。

逐步操作指引见 [`examples/mock-sp/README.zh.md`](https://github.com/Paraview-RD/portico/blob/main/examples/mock-sp/README.zh.md)。

这里 `sp register` 收的是**文件**，而不是程序自己提供的那个 metadata URL，因为
`--metadata` 拒绝明文 `http`：那份文档写明了断言被送到哪里，**路径上的任何人都能把它改
到别处**。它背后的密钥留在 `.mock-sp/` 里、跨运行复用——Portico 会用已注册 metadata 里
公布的那把加密密钥去加密断言，所以一个每次启动都换新密钥的程序，每次都得重新注册一遍，
而忘了重新注册时的症状是**一个解密失败**，不是任何能说清原因的东西。

CAS 那条注册的是**前缀**，它必须覆盖程序发出的 service URL：
`http://localhost:8413/cas/` 覆盖 `http://localhost:8413/cas/callback`。留意最后那个页面
上的票据，然后刷新一次——它会被拒绝，**因为一张服务票只够验证一次**。

换一个租户，就给签发者加上它的路径，并把东西都注册到那里：

```bash
portico client register --tenant acme --id mock-sp --name "Mock SP" --public \
  --redirect-uri http://localhost:8413/oidc/callback

go run ./examples/mock-sp --issuer http://localhost:8410/t/acme
```

三个协议各自独立初始化，所以**起不来的那个会在首页上说明原因，另外两个照常可用**。它是
开发期工具，**不是部署的一部分**——跑它不会让 [integrations.md](integrations.md) 有任何
变化，因为它不产生任何 Portico 本来就不会产生的连接。

由一个真实的依赖方库驱动的完整流程在
[internal/server/federation_test.go](https://github.com/Paraview-RD/portico/blob/main/internal/server/federation_test.go)
——**它是这个仓库里最有用的实例，因为它是那个必须一直通过的实例。**
