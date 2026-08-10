# 事件订阅（Webhook）

发生变化时——账号被创建、更新、启用或停用——Portico 会向你注册的地址 POST 一个带
签名的 JSON。

## 注册一个

控制台里 **事件订阅 → 新建订阅**，或者：

```bash
curl -X POST https://<host>/api/v1/webhooks \
  -H "Authorization: Bearer <admin token>" \
  -H 'Content-Type: application/json' \
  -d '{"name":"Billing","url":"https://hooks.example.com/portico","events":["*"]}'
```

响应里包含签名密钥。**它只显示一次。** 与本系统中其它凭据不同，它是**明文存储**的，
因为它用来**签名**而不是用来**认证**——没有什么摘要可供比对——所以它和签名密钥处在
同一档，[备份与恢复](backup-and-restore.md) 里说明了这对一份数据库导出意味着什么。

## 允许哪些目的地

必须是 https、可公网解析、且不是你内网里的地址。被拒绝的：

| | |
|---|---|
| `http://` | 请求体和它的签名在传输中可读，而**一个谁都能读、谁都能重放的签名什么也证明不了** |
| 回环地址（`127.0.0.1`、`::1`、`localhost`） | 这个进程已经认证过的那个数据库 |
| 私有网段（`10/8`、`172.16/12`、`192.168/16`） | 你的内部网络 |
| 链路本地（`169.254/16`） | 云元数据——那个**谁问就把凭据发给谁**的端点 |
| 运营商级 NAT（`100.64/10`） | 容器与运营商基础设施 |
| 带凭据的 URL | 它们会被存下来、也会被写进日志 |

**这不是一个加固选项**；它是阻止一个租户管理员把 Portico 当作通往 Portico 所在网络
的代理的那道东西。

**这个检查跑两次，而第二次才是它真正管用的原因。** 只在注册时检查，会被"注册时解析
到公网、之后解析到 `127.0.0.1`"的域名击穿——DNS rebinding，它只需要攻击者控制一条
DNS 记录。所以**真正要连过去的那个地址是在拨号器里、在建立连接的那一刻被校验的**。
出于同样的理由，**不跟随重定向**。

## 校验一次投递

每个请求都带：

| 请求头 | |
|---|---|
| `X-Portico-Event` | 事件类型 |
| `X-Portico-Delivery` | 投递 id，同时也是请求体里的 `id` |
| `X-Portico-Timestamp` | Unix 秒 |
| `X-Portico-Signature` | `sha256=` 加十六进制的 HMAC |

签名是 HMAC-SHA256，以你的密钥为 key，对**时间戳、一个字面量 `.`、以及原始请求体**
计算：

```
signature = "sha256=" + hex(hmac_sha256(secret, timestamp + "." + body))
```

**时间戳在签名之内，而不只是放在它旁边一起发过来。** 一个只覆盖请求体的签名，对任何
见过它的人——包括你自己的日志——都是**永久可重放**的，而一次被重放的 `user.disabled`
在下游消费系统里就是对某一个人账号的拒绝服务。

```python
import hashlib, hmac, time

def verify(secret: str, headers, body: bytes) -> bool:
    timestamp = headers["X-Portico-Timestamp"]
    expected = "sha256=" + hmac.new(
        secret.encode(), f"{timestamp}.".encode() + body, hashlib.sha256
    ).hexdigest()
    # 常数时间比较：一次快速的字符串比较会一个字节一个字节地
    # 泄露伪造签名猜对了多少。
    if not hmac.compare_digest(expected, headers["X-Portico-Signature"]):
        return False
    # 再拒掉太旧的，否则签名只能证明这个请求体“在历史上某一刻”
    # 是我们发的。
    return abs(time.time() - int(timestamp)) < 300
```

用**原始**请求体，在任何 JSON 解析之前。重新序列化会改变空白和键的顺序，**而签名是
对字节算的**。

## 请求体

```json
{
  "id": "3f1c…",
  "type": "user.disabled",
  "tenant": "6b2e…",
  "occurredAt": "2026-08-08T09:15:00Z",
  "data": { "id": "…", "username": "jsmith", "status": "DISABLED", "…": "…" }
}
```

`data` 就是 API 返回的那个账号或机构。**请求体在事件发生时渲染并按发出的样子存下来**
——事件描述的是**已经发生的事**，而在投递时才渲染，会发出一个描述"某个此后已被重新
启用的账号"的 disabled 事件。

## 事件类型

`user.created`、`user.updated`、`user.enabled`、`user.disabled`、
`organization.created`、`organization.updated`、`group.created`、
`group.updated`、`group.deleted`、`group.members_changed`。

`group.members_changed` 携带的是**当前完整的组**而不是差异——想知道谁在组里的订阅方
会去读这个组，而**每个成员一个事件会把一次批量替换变成一阵没人要的洪水**。

订阅 `*` 表示全部，包括将来版本里新增的类型；也可以只点名你的端点能处理的那些。
`GET /api/v1/webhooks/events` 返回当前的清单。

## 投递、重试，以及"已投递"是什么意思

事件发生时投递被入队，由一个 worker **每十五秒**发送一次。**引发事件的那个操作从不
等待你的端点**——创建一个用户不会因为某个订阅方慢而变慢，也不会因为某个订阅方挂了而
失败。

**至少一次，永远不是恰好一次。** 一个在回程中丢失的响应，与一个从未到达的响应无法
区分，所以**一次你已经处理过的投递可能再次到达**。用 `id` 认出它。

任何 2xx 都算成功。5xx、429 或网络失败会重试——大约半小时内五次尝试，然后这次投递
被标记为失败并留在历史里。**其它任何状态码（包括重定向）都不重试**：400 意味着你听
懂了并且拒绝，再发四次只会得到四次拒绝。

**尽快应答，然后再去干活。** 请求二十秒后超时。

暂停一个订阅会停止为它入队事件。恢复**不会**补发暂停期间发生的事——**那正是"暂停"
的含义**。

## 当订阅方说他们什么都没收到

**事件订阅 → 投递记录**显示尝试过什么、回来的是什么、试了几次。这就是"我们从来没发"
和"你的端点五次都回了 500"之间的区别，**而且不需要请对方去翻他们自己的日志**。

投递记录保留三十天。

## 在容器里运行

发布镜像里包含一份 CA 证书包，而在 webhook 出现之前它没有——**投递是这个服务唯一的
出站 TLS**。没有什么需要配置的；写在这里，是因为一个用旧 Dockerfile 构建出来的镜像
会让每一次投递都因证书错误而失败，**而在开发者的机器上一切正常**。
