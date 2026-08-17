# Portico

*[English](README.md) · 简体中文*

一个可自托管的身份平台：标准单点登录、多租户隔离，以及一整套自助流程 —— 单个
Go 二进制，Web 界面编译在内，后端是 PostgreSQL。

> **v0.1.0 已打标签，当前分支是进行中的 0.2。** 下面写的是仓库里已经存在的东
> 西，不是计划里的 —— 标签与此处的差异在
> [CHANGELOG.md](CHANGELOG.md) 顶部的 Unreleased 一节，那一节的末尾列出了两者
> 都刻意没有的部分。目前还没有发布的二进制包；用 [运行](#运行) 里的任一种方式
> 从源码构建。
>
> 根目录的几份文件（CHANGELOG、SECURITY、CONTRIBUTING、CODE_OF_CONDUCT、
> `.env.example`）目前只有英文版。手册的章节两种语言都有，本页链接直接指向中文
> 那一份。

## 用 Docker 跑起来

PostgreSQL 和服务一起拉起来，不用装别的：

```bash
export POSTGRES_PASSWORD=$(openssl rand -hex 16)
export PORTICO_JWT_SECRET=$(openssl rand -hex 32)
docker compose -f deploy/docker-compose.yml up -d
```

然后打开 <http://localhost:8410>，用 `admin` / `Portico@1` 登录 —— 那是文档写
明的默认口令，**第一次登录会被拒绝，要求先改掉它**。手册就在同一个二进制的
`/docs` 下，所以它描述的是你刚启动的这个版本，而不是最新的那个。

[运行](#运行) 一节里有从源码构建的方式、每个环境变量是干什么的，以及一份备份必
须包含什么。下面全部是「它做什么、以及为什么这么做」。

还没装、想先读的话，同一份手册也发布在
[paraview-rd.github.io/portico](https://paraview-rd.github.io/portico/zh/)。

## 它做什么

**标准单点登录** —— OAuth 2.1、OpenID Connect 1.0、SAML 2.0 和 CAS，已有的业
务系统不必为它实现一套专有协议。

**多租户** —— 每张表都带租户，隔离由查询层强制，而不是交给评审人的自觉。每个
租户有自己的管理员；租户从命令行开通，所以系统里不存在任何能横跨所有租户的角
色。

**账号、组织与组** —— 新建、编辑、批量启用/停用，从表格导入也能导回表格。一个
账号承载的是目录里真正会有的那些属性 —— 职务、部门、员工编号、姓名的各个部
分、语言、地址 —— 命名照 SCIM 2.0 的 schema，所以你目录里的东西落进对应的字
段，而不是被丢掉。目录里有、而这里没有对应字段的，租户自己定义 ——
**系统 → 用户属性**，可以是文本、数字、是/否、日期，或者一个自己写出来的候选
列表 —— 定义之后它就出现在每个账号的表单上，并进入字段目录，寻址方式和内置字
段完全一样。描述一个人和决定他的权限是两个不同的接口，这正是让人能自己维护资
料、而这件事不会顺带变成改角色的途径。账号只停用、从不删除，审计链因此保持完
整。组织是一个人所在的位置：只有一个，排成一棵树。组是他所属的集合：可以有任
意多个，扁平的，通常由推送它的那个目录维护。它们是两个概念，因为形状不兼容，
而且组成员身份不授予任何权限。一个人还可以被**附加**到任意多个别的组织上 ——
既做平台又在某个项目里的工程师 —— 这只是说明性的：不授予权限、不同步到任何地
方，也不动那一个权威归属。每个组织都可以指定负责人，同样不授予权限：这个版本
只有两个固定角色，而一个悄悄变成第三个角色的字段，是最糟糕的获得角色的方式。
[docs/organizations.zh.md](docs/organizations.zh.md) 把两种形状并排放在一起，
用来判断某个事实该放进哪一种。

**从目录读取账号** —— 连接 Active Directory 或 OpenLDAP 把用户拉进来，以目录
自己的稳定标识符对账，所以改名就只是改名。不再出现的账号被停用；重新出现的会
回来。一次拿到空结果的同步会拒绝据此行动，因为一个写错的 base DN 和一个所有人
都已离开的目录，长得一模一样。
[docs/ldap.zh.md](docs/ldap.zh.md) 有两种目录各自的属性映射表，以及一份「同步
不会做什么」的清单。

**由目录供给** —— 用户和组走 SCIM 2.0，支持的是 Okta 和 Entra 实际发出的那些
PATCH 形状，而不只是规范里排在最前面的那几种。账号以 `externalId` 对账，所以目
录里的改名在这里也是改名，不会变成第二个账号。
[docs/scim.zh.md](docs/scim.zh.md) 明确写出了哪些东西**不**在供给范围内 —— 这
恰好是做集成的人最先需要知道的部分。

**Webhook** —— 账号、组织或组发生变化时发一个带签名的 POST，有重试，也有一份
可以查的投递历史，用在订阅方声称什么都没收到的时候。目标地址被限制为公网
HTTPS，并且在建立连接时再检查一次，这样租户管理员没法把 Portico 当成通往它所在
网络的代理。
[docs/webhooks.zh.md](docs/webhooks.zh.md) 有事件清单、接收方要验的那个签名，以
及一个持续失败的订阅会被重试多久。

**字段用对方读得懂的名字发出去** —— 服务提供方是按它拿到的名字匹配的，所以一个
找 `dept` 的系统会把发过去的 `department` 扔掉，而这个字段在两边看起来都像是没
有。字段目录里的任何字段，都可以在出站时改名或新增，按应用配置，Webhook 订阅
再各自配一份。一条规则做的是改名还是新增，不是你能选的：OpenID Connect 本来就
会发的那十个 claim 是改名，其余的一切 —— 二十五个 SCIM 档案属性、组织、租户自
己定义的那些 —— 都是点名之后新增。一条规则只影响那一个应用或那一个订阅方，别的
都不受影响。[docs/field-mappings.zh.md](docs/field-mappings.zh.md) 有完整目录、
那十个是哪十个，以及映射做不到的事。

**指标** —— Prometheus，跑在一个单独的监听地址上，而且只有你配了才存在；标签里
不带租户，也不带请求路径。

**三种登录方式** —— 用户名、手机号或邮箱，产出的是同一份凭据。

**自助** —— 注册（可选，默认关闭，并且可以要求邮箱验证通过后账号才生效）、改密
码、通过邮箱找回密码、维护自己的资料，全程不需要管理员介入。密码规则按租户配：
一个任何策略都不能再降低的最小长度，再加上可选的组合要求、重复使用检查和有效
期 —— 后三项默认关闭，并且是按它们实际的身份（合规特性）来记录的，而不是按它们
不是的那个身份（安全特性）。找回密码需要一个 SMTP 中继，把
`PORTICO_SMTP_HOST` 指向你已经在跑的那个即可。短信找回定义成了一个供应商接口，
但不附带任何一家的实现。

**一个人人都有的首页** —— 登录后落在你能打开的应用、你账号的概览，以及最近几次
登录记录上，而不是落在大多数人根本用不了的管理界面上。首页上的应用是这个租户
的，不是这个读者的，界面本身就把这句话说清楚：这个版本只有两个固定角色，也没有
「谁可以用哪个」这个概念，所以每个人看到的列表是一样的。一个应用一旦有了启动地
址就会出现在那里 —— 协议本身存的那些地址都不算，因为重定向 URI 和断言消费服务
是浏览器在流程中途被送去的地方 —— 并带上注册时给的图标，没给就用一个印着名称首
字母的方块。

**离开** —— 任何人都可以注销自己的账号，用密码确认。这是唯一允许自我停用的地
方；别处一律拒绝，这样管理员就不会一不小心把自己锁在外面。注销是停用而不是删
除，所以管理员还能把它恢复回来，审计链指向的也仍然是一个存在的账号。

**能真正吊销的会话** —— 每次登录都列在你自己的资料页上，带着来源地址和浏览器，
其中任何一条都可以单独结束。退出登录结束当前这一条；「退出所有设备」、改密码、
停用账号这三件事各自都会立刻结束全部会话，连同联邦应用持有的每一个刷新令牌。任
何身份提供方都收不回的，是别人已经拿在手里的凭据 —— 一个离线验证的访问令牌，或
者一个应用在接受断言之后为自己建立的会话。
[docs/federation.zh.md](docs/federation.zh.md) 有一张按协议列出吊销究竟能触及
什么的表，部署之前值得先读。

**审计日志** —— 登录、操作、授权、注册、组织变更，以及每一次应用注册，可按类型
和时间范围过滤。[docs/settings.zh.md](docs/settings.zh.md) 说明它记录了什么，并
且诚实地讲了它不会告诉你什么。

**会自我解释的界面** —— 每个管理界面开头都有几句话，说明它是干什么的、在这里改
动会影响到哪里，各自都链进手册看剩下的部分。运维人员已经读过四百遍的租户，可以
在自己的设置里把它们关掉；默认开着，因为一个没人能看懂的界面，代价大于一段有人
已经不需要的解释。

**双语界面** —— English 和简体中文，运行时可切换。

这个版本刻意**没有**：自定义角色与权限（只有两个固定角色）、MFA，以及登录端点之外
的请求限流。组织可以**登记**谁将来管理它、以及生效范围，那是为路线图上的分级授权做的
数据准备——今天不授予任何权限，并且有测试守着这一点。路线图在
[docs/requirements/v0.1-requirements.md](docs/requirements/v0.1-requirements.md)（英文）。

> **在把它暴露到网络之前：** Portico 提供的是明文 HTTP，这是刻意的。
> `/api/v1/auth/*` 按客户端地址做了限流，但那是一条下限而不是一道防线 —— 它按
> 地址、按进程计数，所以对手里有大量地址或面对多实例部署时都起不了作用。账号在
> 多次登录失败后也会锁定，那又是第三种控制：它阻止的是某一个账号的密码被猜出来。
> 它必须跑在一个终结 TLS、并对 `/api/v1/auth/*` 限流的反向代理后面 —— 原因见
> [SECURITY.md](SECURITY.md)（英文），可直接用的 nginx 与 Caddy 配置见
> [docs/access-guide.zh.md](docs/access-guide.zh.md)。

## 不装就先看看

[**开一个 Codespace**](https://codespaces.new/Paraview-RD/portico) —— GitHub
会构建控制台、手册和服务端，往数据库里灌进人、组织、应用和历史记录，然后在浏览
器标签页里打开控制台。不用装任何东西，也不用配任何东西。

第一次创建通常要 10-20 分钟：GitHub 先给容器装好 Go、Node、Python 这些工具链，
之后才轮到上面说的那些构建 —— 编译控制台、手册和服务端，再灌入数据库。这段时
间打开的那个标签页大多是空的，这是正常现象，不是卡住了。

用 `admin`（超级管理员）或 `liyan`（普通用户）登录；所有种子账号共用密码
`Portico@1`。`zhangwei` 是第二个管理员，也是大部分种子历史记录的操作者，想看一
个「有过去」的账号就登它。同样这批名字在第二个租户 `acme` 里也有，但几乎没有任
何东西是从第一个租户带过去的 —— 这是理解这里的多租户是什么意思的最短路径。邮件
发往第二个转发端口上的一个 Mailpit 收件箱，而不是任何真实地址，所以重置密码的链
接是拿来读的，不是拿来等的。

点之前有两件事值得知道：

- **它是你的，不是一个共享 demo。** Codespace 跑在你自己的 GitHub 账号里，转发
  的端口只对你可见 —— 不存在一个能发给别人的地址。想看的人从同一个按钮开自己
  的，花的是他自己的免费额度而不是你的。免费个人账号每月有 120 核·时和 15
  GB·月，付费方案更多；一台 2 核机器每醒着一小时消耗其中两个核·时。
- **看完就停掉。** 空闲 30 分钟它会自行挂起，但一个被忘掉的 Codespace 仍然占着
  存储。用 `gh codespace delete`，或者去
  [github.com/codespaces](https://github.com/codespaces) 的列表里删。

如果想让账单落在组织而不是打开它的人身上：组织所有者为这个仓库启用 Codespaces
并给它设定支出，之后按钮还是那个按钮，只是计量表指向了别处。

## 运行

### 二进制

需要有一个 PostgreSQL 实例可以指过去。前端必须先构建 —— 它被编译进二进制，所以
只跑 Go 的构建会得到一个能用的 API 但没有界面（服务端会把这件事说出来，而不是给
你一个空白页）。

```bash
cd web && npm ci && npm run build && cd ..
go build -o portico ./cmd/server

PORTICO_DB_DSN=postgres://portico:portico@localhost:5443/portico?sslmode=disable \
PORTICO_JWT_SECRET=$(openssl rand -hex 32) ./portico
```

需要 Go 1.26+ 和 Node 22+ —— 这是 `go.mod` 声明的版本，也是 CI 和发布流程实际跑
的版本，所以下限只有一个答案而不是三个。两个都是下限而不是钉死的版本：下面那个
从源码构建的 Docker 镜像用的 Node 就比这里要求的更新。

### Docker

把 PostgreSQL 和服务端一起拉起来，别的什么都不用装。

```bash
export POSTGRES_PASSWORD=$(openssl rand -hex 16)
export PORTICO_JWT_SECRET=$(openssl rand -hex 32)
docker compose -f deploy/docker-compose.yml up -d
```

配置全部是环境变量 —— 完整清单见 [.env.example](.env.example)（英文）。除了
`PORTICO_DB_DSN` 之外每一项都有一个能用的默认值，而它没有：不设，服务端会把这件
事说出来然后退出。`PORTICO_JWT_SECRET` 也请显式设置 —— 不设不会阻止启动，但每次
启动都会生成一个随机密钥，于是每次重启所有会话都会失效。

这两个都请存进你放密钥的地方，因为它们都不在数据库里，而恢复的时候需要它们：
`PORTICO_JWT_SECRET` 签的是会话，`PORTICO_ENCRYPTION_KEY` 用来打开目录的 bind
密码 —— 数据库导出里只有那份密文。所以单独一份 `pg_dump` 并不是这个系统的备
份。
[docs/backup-and-restore.zh.md](docs/backup-and-restore.zh.md) 说明该复制哪些东
西，以及真的用到这份副本时，少了哪一样各自会付出什么代价。

### 租户

首次启动会创建一个租户，编码 `default`。没有指名租户的登录都落在它上面，所以单
租户部署可以整节跳过。

更多租户从命令行开通 —— 不存在一个跨租户的角色能让 API 去授权：

```bash
portico tenant create --code acme --name "Acme Corp"
portico tenant list
portico tenant disable --code acme     # 拒绝登录，不删除任何东西
```

每个租户都有自己的管理员。不带 `--admin-password` 时它取文档里写明的那个默认
值，并且在这个值被替换之前无法登录；传了就能正常登录。它的用户登录时要带上租户
编码，填在**租户**那一栏，或者由链接带过来：`/login?tenant=acme`。

### 应用

三种协议，同一套账号。用哪一种取决于这个应用本来会说哪一种；三者回答的是同样的
事实，用的是同样的名字。

在控制台的**应用**里注册 —— 每种协议一个页签，还有一个集成面板，把 issuer、发现
文档、SAML 元数据与证书、CAS 服务地址交回给你，粘到对面去。或者用命令行 —— 底下
是同一个服务，因此是同样的规则和同一条审计链：

```bash
# OpenID Connect / OAuth 2.1 —— 让库指向 issuer，然后注册
portico client register --id grafana --name Grafana \
  --redirect-uri https://grafana.example.com/login/generic_oauth

# SAML 2.0 —— 双向交换元数据文档
portico sp register --metadata ./sp-metadata.xml --name Confluence

# CAS 2.0/3.0 —— 注册票据可以被送到的 URL 前缀
portico cas register --url https://wiki.example.com/ --name Wiki
```

三者都还接受 `--launch-url` 和 `--logo-uri`，这才是让一个应用带着可辨认的方块出
现在首页上的东西。两个都是可选的，也都不影响登录 —— 没有启动地址的应用照样能
用，只是不会作为「可以打开的东西」被列出来。

一切都挂在 `PORTICO_PUBLIC_URL` 上（默认租户），别的租户挂在
`PORTICO_PUBLIC_URL/t/<编码>` 下：OIDC 的 issuer 在根路径，SAML 元数据在
`/saml/metadata`，CAS 在 `/cas`。
[docs/federation.zh.md](docs/federation.zh.md) 讲细节，包括哪些东西是刻意没有实
现的，以及为什么。

## 开发

```bash
# 后端，热重载用你自己习惯的那套
go run ./cmd/server

# 前端，把 /api 代理到上面那个后端
cd web && npm install && npm run dev   # http://localhost:5410
```

```bash
go test ./...                 # 后端
cd web && npm run build       # 前端类型检查 + 构建
```

改了 SQL 查询就要重新生成查询层：

```bash
sqlc generate    # brew install sqlc
```

生成的代码是提交进仓库的，所以只有动到 `internal/store/queries/` 的贡献者才需要
装 `sqlc`。

## 目录结构

```
cmd/server/        入口
cmd/seed/          往开发数据库里灌进九十天的使用痕迹
internal/
  config/          环境变量，读取并只校验一次
  server/          路由，以及构建所报告的版本
  handler/         HTTP 处理器
  service/         业务规则
  store/           数据库访问；sqlcgen/ 是生成的
  model/           领域类型
  auth/            密码、JWT、令牌验证
  httpx/           响应封装、它承载的错误类型，以及请求日志和安全响应头
  secrets/         AES-GCM，给那一个必须能读回来、而不只是能校验的凭据用
  i18n/            消息目录，英文和中文
  mailfmt/         一封邮件描述一次，渲染成纯文本和 HTML 两份
  casp/            CAS 协议，直接实现
  oidcp/           把 Portico 适配到 OpenID Provider 接口
  oidcrp/          反过来的方向：经由别人的 OpenID Provider 登录
  socialrp/        没有 discovery 文档的那两家，一家一个文件
  samlp/           把 Portico 适配到 SAML 身份提供方的角色
  scim/            目录用来供给的那些 SCIM 2.0 接口
  directory/       另一个方向：从 LDAP 里读账号
  webhook/         出站投递、签名与重试
  notify/          邮件，以及不带供应商实现的短信接口
  metrics/         Prometheus，配了才启用它自己的监听
  provision/       租户与客户端开通，给 CLI 用
  seed/            一个看起来被用过的开发数据库，给 cmd/seed 用
  demo/            往单个租户里灌一个行业的示例数据，给自助试用用
  testdb/          测试用的一次性 PostgreSQL
  web/             嵌入构建好的前端
  docs/            嵌入这份文档，由二进制自己提供
migrations/        表结构，嵌入并在启动时应用
web/               React + Vite 前端
docs/              手册（两种语言），以及各项约定和需求
deploy/            Dockerfile 与 compose
```

## 设计取舍

有两个选择解释了其余大部分：

**PostgreSQL，通过纯 Go 的 pgx 驱动访问。** 早期版本用的是 SQLite，在范围还是
「单租户内网工具」的时候，一个文件型数据库是正确的取舍。多租户和面向公网的 SSO
改变了这一点：租户隔离需要真正的约束，而单写入者的数据库对一个多个系统都依赖的
身份提供方来说是错的形状。这个驱动不需要 cgo，所以二进制仍然可以交叉编译、仍然
能装进 `scratch` 容器 —— 代价是一次部署现在是两个进程而不是一个。

**无状态令牌 + 一个吊销计数器。** 每个账号带一个 `token_version`，退出登录、改
密码和停用都会把它加一。中间件每个请求重新读一次账号，拒绝版本过旧的令牌。代价
是一次带索引的读取，换来的是不需要维护一致性的黑名单就能立即吊销。

这些内容都发布成了一份手册：
[paraview-rd.github.io/portico](https://paraview-rd.github.io/portico/) ——
同样的页面，渲染过、可搜索、两种语言都有。运行中的 Portico 在 `/docs` 下提供自
己的那一份，编译在那个二进制里；当一个版本就在你面前时，那一份才是描述它的。

这个项目对自己要求的各项约定 —— 都在 [docs/](docs/) 下，除联邦一章外只有英文：
[代码](docs/code-conventions.md) ·
[配置](docs/configuration-conventions.md) ·
[API](docs/api-conventions.md) ·
[联邦](docs/federation.zh.md) ·
[数据库](docs/database-conventions.md) ·
[错误](docs/error-conventions.md) ·
[日志](docs/logging-conventions.md) ·
[i18n](docs/i18n-conventions.md) ·
[界面](docs/design-principles.md)。

## 参与贡献

欢迎提 issue 和 pull request。每个提交都需要 DCO 签署（`git commit -s`），CI 会
检查。[CONTRIBUTING.md](CONTRIBUTING.md)（英文）有一份从克隆到跑起服务的、经过
验证的完整流程。

参与本项目须遵守我们的
[行为准则](CODE_OF_CONDUCT.md)（英文）。

**发现安全问题？** 请不要开 issue —— 通过
[Security → Report a vulnerability](https://github.com/Paraview-RD/portico/security/advisories/new)
私下报告。另见 [SECURITY.md](SECURITY.md)（英文）。

## 许可

[Apache License 2.0](LICENSE)。署名信息见 [NOTICE](NOTICE)。
