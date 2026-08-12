# 字段映射

每个通过 Portico 登录的应用，都会收到关于这个人的一组事实，用的是 Portico
选定的名称。多数集成对此并无异议。有异议的那些，问题只有两种，而这里同时
解决它们：**名字不对**，或者**这个事实根本没发出去**。

服务提供方是按拿到的名称去映射的。它读 `dept`，Portico 发的是
`department`，这个字段就会到达、然后被丢弃。在这个功能之前，唯一的出路是改
代码——而且一改就对所有应用同时生效。

## 哪些字段可以映射

`GET /api/v1/fields` 返回字段目录，也就是所有可被命名的东西。它分两半：

| | |
|---|---|
| **内置** | 身份、SCIM 的 25 个 profile 属性、组织、租户 |
| **你自己的** | 本租户在**设置 → 用户属性**里自行定义的那些 |

两半的用法完全一样。映射存的是这个列表里的**键**，不是数据库列名——原因见
[下文](#为什么是一份列表而不是列名)。

**任何字段都不会以空值发出。** 某个账号没有值的字段是「不出现」，而不是
「出现但为空」。所以服务提供方如果始终收不到它映射的某个字段，该去看账号，
而不是去看映射配置。

有一处不对称需要知道，只发生在 webhook 载荷里。上面那条规则管的是**映射**发
出的内容。而 `user.*` 事件里默认的 `profile` 对象，一直是把没有值的成员写成
空字符串的，现在也还是——所以一个没有成本中心的账号，在默认载荷里是
`profile.costCenter: ""`，而如果你把 `cost_center` 映射成自己的名字，则什么都
不发。两者描述的是同一个账号的同一个事实，写法却不一样。改动默认载荷对所有现
有订阅方都是破坏性变更，所以没有改。

## 一条规则做什么

三件事，一条规则只做其中一件：

| | |
|---|---|
| **改名** | 本来就会发的事实，换个名字发 |
| **抑制** | 本来就会发的事实，不再发了 |
| **新增** | 本来不发的事实，用你指定的名字发出去 |

抑制是一个标记而不是一个空名称，因为「什么都不发」和「还没想好叫什么」是两
种不同的意图。

新增是更常用的那一半。那 25 个 profile 属性是存着的、会从 SCIM 进来，但默认
一个应用都到不了。

## 在哪里配置

按接收方配置，一共四种：

```
PUT /api/v1/applications/oauth-clients/{clientID}/field-mappings
PUT /api/v1/applications/saml-service-providers/{id}/field-mappings
PUT /api/v1/applications/cas-services/{id}/field-mappings
PUT /api/v1/webhooks/{id}/field-mappings
```

同一路径 `GET` 读回。保存是**整套替换**——表单发出的就是有人编辑过的那张
表，合并会把他们删掉的行留在原地。发一个空列表即恢复默认。

```bash
curl -X PUT https://<host>/api/v1/applications/oauth-clients/wiki/field-mappings \
  -H "Authorization: Bearer <管理员令牌>" \
  -H 'Content-Type: application/json' \
  -d '{"mappings":[
        {"sourceKey":"department","targetName":"dept"},
        {"sourceKey":"organization_path","targetName":"org_path"},
        {"sourceKey":"phone","suppressed":true}
      ]}'
```

### 空集合意味着「按默认发」

不是「什么都不发」，而是各协议文档里写明的那套默认值，一字不差。这是刻意
的，也是这个功能敢于上线的原因：升级不改变任何行为，直到有人做出决定；而表
里有一行，就意味着有人做过决定。这里没有预置规则需要你去和真正的决定区分。

## 各接收方的「名称」分别指什么

**OpenID Connect** —— 目标是 claim 名。

!!! warning "规则作用于 ID token 与 access token，不作用于 userinfo"

    规则在「知道是哪个客户端」的地方生效，而四个位置里只有两个知道：签发令牌
    时，以及组装 access token 的 claim 时。**userinfo 端点与内省端点不知道**
    ——这里的 access token 是一个没有落库记录的裸标识符，无从查出客户端。

    所以这两个端点无论应用配了什么，都按文档中的默认值作答。**抑制在那里不生
    效**：一个被配置为「不接收手机号」的应用，只要调 userinfo 仍然拿得到。如
    果你的抑制是出于披露考虑，请把它当作一个尚未堵上的口子，而不是一个细节
    ——另一方面，多数集成实际读的是 ID token。

    `sub`、`email_verified`、`phone_number_verified` 永远不可映射。第一个两头
    都封死——既不能被改名过去，也不能被改名走——因为应用的整个信任模型都建立在
    「它始终指同一个人」之上。后两个跟随它们所描述的那个 claim：只有当那个
    claim 以自己的名字发出时才发，否则不发。

**SAML** —— 目标是属性的 `Name`，这才是服务提供方实际据以映射的那个。
`friendlyName` 在旁边，仅供参考；设置它不会改变 SP 的匹配行为。

**CAS** —— 目标是票据校验响应里的**元素名**，`cas:` 前缀会自动补上。
`cas:user` **不是**属性，也不可映射：每个 CAS 客户端都拿它做本地记录的主键，
它是 `sub` 在这个协议里的对应物。

**Webhook** —— 目标是事件 `data` 对象**顶层**的一个键。这一种有另外三种没有
的额外影响，见下节。

## Webhook

上面三种协议是逐字段拼装 claim 集合或属性列表的。Webhook 的载荷不是拼出来
的——它就是账号或组织序列化的结果，在这个功能存在之前一直如此。所以这里的规
则是**叠加**在默认载荷之上，而不是取而代之。载荷原本带的东西，一样不少。

**改名会「提升」。**像 `profile.department` 这样的嵌套字段没法以新名字继续待
在嵌套里，因为目标只有一个名字。所以它会移到 `data` 顶层，并从 `profile` 中
移除：

```json
{
  "id": "…", "displayName": "…",
  "dept": "Analytical Engines",
  "profile": { "title": "Engineer" }
}
```

如果提升之后 `profile` 空了，整个对象会被删掉，而不是发一个 `{}`。

**群组事件不参与映射。**`group.created`、`group.updated`、`group.deleted`、
`group.members_changed` 携带的是群组，而字段目录里没有群组词汇。它们的载荷会
原样投递，即使该订阅已经为账号配置了规则。这是一个决定，不是遗漏。

**已入队的投递保持它被渲染时的形态。**载荷是在事件发生时渲染的，不是在发送
时。改动映射只影响此后的事件；已经在队列里等待的——包括正在重试的——会按当初
的形态发出。事件描述的是「发生过什么」，事后重新渲染会导致一条
`user.disabled` 描述一个早已被重新启用的账号。

## 哪些配置会被拒绝

在保存时拒绝，面对的是能改的那个人——而不是在登录时拒绝，面对的是改不了的那
个人。

| | |
|---|---|
| `RESERVED_CLAIM_NAME` | 该名称是 OpenID Connect 会据以行动的：`sub`、`iss`、`aud`、`exp`、`nonce` 等。**仅对 OIDC 应用生效**——SAML 里叫 `sub` 的属性很平常 |
| `DUPLICATE_MAPPING_SOURCE` | 一个字段两条规则。谁生效取决于先读到哪条 |
| `DUPLICATE_MAPPING_TARGET` | 两个字段用同一个名称。只有一个会到达，而且不是你选的那个 |
| `CLAIM_NAME_TAKEN` | OIDC 改名落到了本系统自己已在发送的 claim 上——比如把 `department` 映射成 `tenant_id`。规范没保留它，但它一样是被占用的 |
| `PAYLOAD_NAME_TAKEN` | webhook 改名落到了事件已用于别的字段的名称上——比如把 `department` 映射成 `id` |
| `MAPPING_TARGET_REQUIRED` | 既没有名称也没有抑制标记，这条规则什么都没说 |
| `UNKNOWN_FIELD` | 目录里没有这个键。通常是打错了 |

第一条是唯一有牙的那条。把 department 改名成 `nickname` 是别人的事；改名成
`sub` 则是在告诉一个应用「这个人是另一个人」，而且是在一个它完全有理由信任的
令牌里。

## 哪些应用收到了某个事实

四种接收方共用一张表，正因如此，这个问题一次查询就能回答，而不是四次。这是
披露审查**之后**才会有人问的问题：*谁在收 `department`？*

## 为什么是一份列表而不是列名

更省事的做法是让映射直接写数据库列名。而 `users` 表里还有
`password_hash`、`token_version`、`failed_login_attempts`。

一个能写列名的配置项，早晚会有人写到那几个上——而且是租户管理员，通过一个受
支持的字段，中间没有任何代码评审。所以字段目录是一份枚举，目录里没有的键一
律拒绝。

有些条目只能出站，且列表里逐条写明了原因。其中几条是安全边界而非疏漏：一个
能设置 `role` 的目录属性，等于把提权能力交给了一个 Portico 管不着的系统。这
条线画在哪里，见[读取目录](ldap.zh.md)。

## 另见

- [Webhook](webhooks.zh.md) —— 注册订阅、签名、重试
- [目录推送](scim.zh.md) —— profile 属性从哪里来
- [联合登录](federation.zh.md) —— 各协议默认发什么
