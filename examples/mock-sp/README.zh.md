# mock-sp：把单点登录点一遍

一个能用浏览器点的接入方（依赖方 / 服务提供方 / CAS 客户端），用来**演示和验收** Portico
的单点登录。三个协议各一张卡：OpenID Connect、SAML 2.0、CAS 3.0。

它证明的东西和测试不一样。`internal/server/federation_test.go` 与 `saml_test.go` 把协议
证明给测试运行器看；这个程序把它证明给**在场的人**看——一次跳转、一个登录页、一个把拿
回来的东西摊开的页面。

**它不是库，也不是产品代码。** 除了一把 SAML 私钥，它什么都不存；登录完不留会话，点第
二次是从头再走一遍。值得从它这里抄走的是**哪几个库调用、按什么顺序出现**，不是它外面那
层程序。

---

## 前置条件

- Portico 在跑，并且 `PORTICO_PUBLIC_URL` 就是**浏览器实际用的那个地址**
- `portico` 命令行可用，且 `PORTICO_DB_DSN` 已设（注册应用要用它，和启动服务器一样）
- 端口 8413 空闲（`--addr` 可改）

下面的命令按默认部署写：Portico 在 `http://localhost:8410`，默认租户。换成你自己的地址
即可。

---

## 一、注册 OIDC 客户端

```bash
portico client register --id mock-sp --name "Mock SP" --public \
  --redirect-uri http://localhost:8413/oidc/callback
```

`--public` 表示它是公开客户端、拿不到密钥，靠 PKCE 认证——**Portico 实现的是 OAuth 2.1，
对每一个客户端都强制 PKCE**，机密客户端也不例外。

## 二、第一次启动

```bash
go run ./examples/mock-sp
```

启动时它会做三件事，然后把后两条注册命令**直接打印给你**：

1. 对 Portico 跑一次 OIDC 发现（discovery）
2. 拉取 Portico 的 SAML metadata
3. 在 `.mock-sp/` 里生成自己的 SAML 私钥、证书和 metadata 文档

此时打开 <http://localhost:8413>，**OpenID Connect 那张卡已经可以点了**。

## 三、注册 SAML 服务提供方与 CAS 服务

把上一步打印出来的两条命令跑掉（程序打印的是绝对路径，相对路径同样可用）：

```bash
portico sp register --metadata .mock-sp/saml-metadata.xml --name "Mock SP"
portico cas register --url http://localhost:8413/cas/ --name "Mock SP"
```

CAS 那条其实什么时候跑都行——它注册的是一段**URL 前缀**，事先就知道。SAML 那条必须等第
一次启动之后，因为它要的是程序刚生成的那份 metadata 文档。

## 四、三张卡逐个点

**不需要重启。** 注册这件事完全发生在 Portico 那一侧，mock-sp 不缓存它——回到
<http://localhost:8413>，三张卡现在都能点。

（只有一种情况需要重启：**第一次启动时它就没连上 Portico**。那种情况首页会直接写明原
因，不会让你猜。）

---

## 三张卡分别在演示什么

**OpenID Connect** —— 授权码 + PKCE。结束页把 **ID 令牌的声明**和 **userinfo 响应**并排
摆着，因为这是接入方最常搞混的一对：ID 令牌是**登录那一刻** Portico 断言的、带签名的事
实；userinfo 是它**此刻**对同一个人的说法。分不清这两者，写出来的应用要么永远看不到改过
的名字，要么每个请求都去调一次 userinfo。

**SAML 2.0** —— 断言由浏览器 POST 回来。页面会告诉你**这份断言是加密的**：因为这个服务
提供方在 metadata 里公布了加密密钥，Portico 就会加密它。签名、时间窗、受众、以及
`InResponseTo` 是否对应一个**本进程真的发出过**的请求，全部由 `crewjam/saml` 校验——
`ParseResponse` 一个调用做完，程序自己一项都不重复校验。**手写其中任何一项，都是 SAML 接
入最终会接受伪造断言的经典路径。**

**CAS 3.0** —— 浏览器只捎回一张不透明的票。页面上其余所有内容，都来自这台服务器**自己发
起的第二个请求**——这正是 CAS 的票据被人截获也不值钱的原因。看完票据**刷新一次**：它会被
拒绝，因为一张服务票只够验证一次，一分钟内有效。**那个失败是协议在正常工作。**

---

## 排错：三个「看起来像别的问题」的坑

**1. 重定向 URI 是逐字符匹配的。**
注册的那个、程序发出的那个、浏览器实际访问的那个，必须是同一个字符串。`localhost` 和
`127.0.0.1` 是同一台主机，**但不是同一个字符串**；端口同理。不一致时报的是
`invalid_request`，看起来像登录本身失败了。

**2. `PORTICO_PUBLIC_URL` 必须是浏览器用的地址。**
它就是 OIDC 的签发者标识，发现文档由它构造，而依赖方会核对拿回来的和自己问的是否一致。
指向一台公开 URL 写着别的地址的服务器，会在**发现阶段、也就是 mock-sp 启动时**就失败，
而不是等到你点下去。

**3. 请求的 scope 必须是客户端注册过的 scope。**
两边默认都是 `openid profile email`。要 `offline_access`（也就是要刷新令牌），注册时和
`--scope` 都得带上它。

SAML 与 CAS 各自还有一个：

**4. `sp register --metadata` 拒绝明文 `http` URL，所以只能给文件。**
那份文档写明了断言被送到哪里，**路径上的任何人都能把它改到别处**。程序本身也在
`/saml/metadata` 上提供这份文档，但注册时请用 `.mock-sp/` 里的文件。

**5. 换了 `--state-dir` 就要重新 `sp register`。**
Portico 会用**已注册 metadata 里公布的那把加密密钥**加密断言。换目录等于换密钥，而症状
是一个**解密失败**，不是任何能说清原因的东西。

---

## 停止它

```bash
pkill -f '/mock-sp'
```

不要去 kill `go run` 的进程号。**`go run` 编译到缓存再执行一个子进程**，你拿到的 PID 是
编译器的外壳——杀掉它，服务器还在监听。脚本里要起停它，请用
`go build -o <某处>/mock-sp ./examples/mock-sp` 再运行产物，`hack/walk-the-flow.sh` 就是
这么做的。

---

## 多租户

给签发者加上租户路径，并把三样东西都注册到那个租户：

```bash
portico client register --tenant acme --id mock-sp --name "Mock SP" --public \
  --redirect-uri http://localhost:8413/oidc/callback

go run ./examples/mock-sp --issuer http://localhost:8410/t/acme
# 再按第三步注册 sp / cas，记得都带 --tenant acme
```

登录页会自动带上租户，结束页上的签发者会是 `.../t/acme`。

---

## 其他

**三个协议各自独立初始化。** 起不来的那个会**在首页上说明原因**，另外两个照常可用——一个
配错的 SAML 证书，不应该有能力让 OpenID Connect 的演示做不成。

**它默认只绑 `127.0.0.1`。** 这些页面会渲染访问令牌和一份断言对某人的全部陈述，走的是明
文 HTTP。在会场的 Wi-Fi 上，默认值不该是整个房间都能访问的东西。

**`.mock-sp/` 已经在 `.gitignore` 里。** 里面有一把私钥——哪怕它只是个演示的私钥，仓库里
的私钥就是仓库里的私钥。

**它不是部署的一部分。** 跑它不会让 [docs/integrations.zh.md](../../docs/integrations.zh.md)
有任何变化，因为它不产生任何 Portico 本来就不会产生的连接。

**代理环境下可以直接用。** 系统装了 Clash 一类常驻代理时，`http_proxy` 会被子进程继承，
但 Go 对 `localhost` 与回环地址默认不走代理，所以 mock-sp 的出站请求（发现、metadata、
CAS 票据校验）不受影响。用 `curl` 手工验证本地地址时则要自己加 `--noproxy '*'`。

协议本身的完整说明在 [docs/federation.zh.md](../../docs/federation.zh.md)。
