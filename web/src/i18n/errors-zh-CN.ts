import type { errorsEnUS } from "./errors-en-US";

/**
 * 错误码的中文文案。
 *
 * 按英文那份做类型约束：漏一个码或拼错一个码都是编译错误，而不是在中文界面
 * 里渲染出一句英文。
 */
export const errorsZhCN: Record<keyof typeof errorsEnUS, string> = {
  // --- 认证与会话 ---
  INVALID_CREDENTIALS: "账号或密码不正确。",
  MISSING_CREDENTIALS: "请输入账号和密码。",
  ACCOUNT_DISABLED: "该账号已被停用。",
  ACCOUNT_UNVERIFIED:
    "该账号还没有确认邮箱地址。请查收邮件，或在下方重新发送。",
  INVALID_VERIFICATION_TOKEN: "该确认链接无效或已被使用，请重新获取。",
  VERIFICATION_UNAVAILABLE:
    "本部署要求新账号确认地址，但没有可用的发送渠道。需要管理员配置邮件中继，或关闭该要求。",
  NO_DELIVERY_CHANNEL:
    "要求确认地址需要有发送渠道。请先配置邮件中继，或保持关闭。",
  ACCOUNT_CLOSED: "该账号已由本人注销。如需恢复，请联系管理员。",
  ACCOUNT_LOCKED: "登录失败次数过多。请稍后再试，或联系管理员解锁账号。",
  MISSING_TOKEN: "尚未登录。",
  INVALID_TOKEN: "登录状态已失效，请重新登录。",
  TOKEN_EXPIRED: "登录已过期，请重新登录。",
  TOKEN_REVOKED: "登录状态已被终止，请重新登录。",
  SESSION_NOT_FOUND: "会话不存在。",
  MALFORMED_AUTHORIZATION: "凭据格式无法解析。",
  UNAUTHENTICATED: "尚未登录。",
  ADMIN_REQUIRED: "该操作需要管理员权限。",
  CURRENT_PASSWORD_MISMATCH: "当前密码不正确。",
  WEAK_PASSWORD: "该密码不符合要求。",
  PASSWORD_EXPIRED: "该密码已过期，登录前必须先更换。",
  PASSWORD_CHANGE_REQUIRED: "该账号还在用默认密码，登录前必须先更换。",
  PASSWORD_NOT_EXPIRED: "该密码尚未过期。请登录后在个人中心修改。",
  SNAPSHOT_IN_PROGRESS:
    "这个订阅的快照还在投递中。等它结束，或者停用该订阅以放弃它。",
  SNAPSHOT_EMPTY_SCOPE: "这个订阅没有选中任何快照能填充的事件。",
  SNAPSHOT_UNAVAILABLE: "本部署无法生成快照。",
  SUBSCRIPTION_DISABLED: "这个订阅已停用。要快照请先启用它。",

  EXTERNAL_IDP_NOT_FOUND: "没有这个身份提供方。",
  EXTERNAL_IDP_DISABLED: "这个身份提供方已关闭。",
  EXTERNAL_IDP_ISSUER_TAKEN: "本租户已经有一个使用该 issuer 的提供方了。",
  EXTERNAL_IDP_ISSUER_REQUIRED: "需要填写 issuer 地址。",
  EXTERNAL_IDP_KIND_UNKNOWN: "这一版还不支持这种身份提供方。",
  EXTERNAL_IDP_CLIENT_ID_REQUIRED: "需要填写 client id。",
  EXTERNAL_IDP_UNREACHABLE:
    "这个 issuer 读不出 OpenID Provider 配置。请检查地址，以及本服务器能否访问到它。",
  EXTERNAL_IDP_SECRET_UNREADABLE:
    "这个提供方的 client secret 解不开：它是用另一个加密密钥封装的，请重新填写。",
  EXTERNAL_STATE_UNKNOWN:
    "这次登录对不上本服务器发起过的任何一次。请重新开始。",
  EXTERNAL_EXCHANGE_FAILED: "身份提供方的应答无法被接受。",
  EXTERNAL_PROVIDER_REFUSED: "身份提供方拒绝了这次登录。",
  EXTERNAL_IDENTITY_UNKNOWN:
    "这个账号还没有在这里关联过。请先用密码登录，然后在个人中心里关联。",
  EXTERNAL_IDENTITY_TAKEN: "这个身份已经关联到某个账号了。",
  EXTERNAL_IDENTITY_NOT_FOUND: "没有这条关联记录。",

  PASSWORD_REUSED: "该密码最近使用过，请换一个没用过的。",

  // --- 密码找回 ---
  INVALID_RESET_TOKEN: "该重置链接无效或已被使用。",
  INVALID_CHANNEL: "该找回方式不可用。",
  MISSING_DESTINATION: "请填写接收的邮箱或手机号。",
  RECOVERY_UNAVAILABLE: "本部署未配置密码找回。",

  // --- 租户 ---
  TENANT_NOT_FOUND: "租户不存在。",
  TENANT_DISABLED: "该租户已被停用。",
  TENANT_CODE_TAKEN: "该租户编码已被占用。",

  // --- 用户 ---
  USER_NOT_FOUND: "账号不存在。",
  USERNAME_TAKEN: "该账号名已被占用。",
  USERNAME_REQUIRED: "请填写账号名。",
  INVALID_USERNAME: "该账号名不被允许。",
  DISPLAY_NAME_REQUIRED: "请填写显示名称。",
  EMAIL_TAKEN: "该邮箱已被占用。",
  PHONE_TAKEN: "该手机号已被占用。",
  INVALID_EMAIL: "邮箱格式不正确。",
  INVALID_PHONE: "手机号格式不正确。",
  INVALID_ROLE: "角色取值不合法。",
  INVALID_STATUS: "状态只能是正常或已停用。",
  CANNOT_DISABLE_SELF: "不能停用自己的账号。",
  EMPLOYEE_NUMBER_TAKEN: "该工号已被其它账号占用。",
  MANAGER_NOT_FOUND: "找不到该直属上级的账号。",
  MANAGER_IS_SELF: "账号不能把自己设为直属上级。",
  LAST_ADMIN: "这是本租户最后一名在用管理员，请先指定另一名管理员。",
  REGISTRATION_DISABLED: "本部署未开放自主注册。",

  // --- 组织 ---
  ORGANIZATION_NOT_FOUND: "组织不存在。",
  ORGANIZATION_DISABLED: "该组织已被停用。",
  ORGANIZATION_CODE_TAKEN: "该组织编码已被占用。",
  ORGANIZATION_MANAGER_NOT_FOUND: "找不到要设为负责人的账号。",
  ALREADY_PRIMARY_ORGANIZATION:
    "该账号本来就归属这个组织。附加挂靠是给它不归属的那些用的。",
  ALREADY_ORGANIZATION_ADMIN:
    "该账号已被记为这个组织的管理员。要改范围，请先移除再重新添加。",
  INVALID_ADMIN_SCOPE: "请选择生效范围：仅本组织，或本组织及其下级。",
  ORGANIZATION_CYCLE: "这会把组织移动到它自己或其下级之内。",
  ORGANIZATION_TOO_DEEP: "组织层级嵌套过深。",
  NAME_REQUIRED: "请填写名称。",
  CODE_REQUIRED: "请填写编码。",

  // --- 批量导入 ---
  MISSING_FILE: "请选择要上传的文件。",
  INVALID_UPLOAD: "上传内容无法读取。",
  INVALID_SPREADSHEET: "该文件不是可读的 .xlsx 工作簿。",
  EMPTY_SPREADSHEET: "该工作簿没有数据行。",
  TOO_MANY_USERS: "一次选中的账号太多了，请分批操作。",
  TOO_MANY_ROWS: "该工作簿行数超出单次导入上限。",

  UNSUPPORTED_IMAGE:
    "该文件不是 PNG 或 JPEG 图片。SVG 不能上传——它是可以携带脚本的文档，而且会由本服务器自己的地址提供。",
  LOGO_TOO_LARGE: "图片太大。磁贴图片最多 512 KiB，且边长不超过 1024 像素。",
  LOGO_NOT_FOUND: "该图片已不在此处存储。",
  IMPORT_FAILED: "导入未能完成。",

  // --- OAuth 客户端 ---
  CLIENT_NOT_FOUND: "客户端不存在。",
  CLIENT_ID_TAKEN: "该客户端 ID 在本租户已被注册。",
  CLIENT_ID_REQUIRED: "请填写客户端 ID。",
  INVALID_CLIENT_ID:
    "客户端 ID 只能包含字母、数字和 . _ - ，长度 3–128 个字符。",
  CLIENT_IS_PUBLIC: "这是公开客户端，仅靠 PKCE 认证，没有可轮换的密钥。",
  CLIENT_DISABLED: "该客户端已被停用。",
  INVALID_CLIENT: "客户端认证失败。",
  OAUTH_CLIENT_NOT_FOUND: "客户端不存在。",
  OAUTH_CLIENT_DISABLED: "该客户端已被停用。",
  INVALID_APPLICATION_TYPE: "应用形态只能是服务端网页、原生应用或浏览器应用。",
  REDIRECT_URI_REQUIRED: "至少需要一个回调地址。",
  INVALID_REDIRECT_URI: "回调地址不合规。",

  // --- 授权请求 ---
  AUTH_REQUEST_NOT_FOUND: "该登录请求已过期，请从应用侧重新发起。",
  AUTH_REQUEST_REQUIRED: "未携带登录请求。",
  AUTH_REQUEST_TAKEN: "该登录请求已被他人完成，请从应用侧重新发起。",
  AUTH_REQUEST_WRONG_TENANT: "该登录请求属于其它租户。",
  INVALID_CODE: "授权码无效或已被使用。",

  // --- SAML ---
  SERVICE_PROVIDER_NOT_FOUND: "服务提供方不存在。",
  SERVICE_PROVIDER_TAKEN: "该实体 ID 在本租户已被注册。",
  SERVICE_PROVIDER_DISABLED: "该服务提供方已被停用。",
  METADATA_REQUIRED: "请提供元数据文档。",
  METADATA_INVALID: "该内容无法按 SAML 元数据解析。",
  METADATA_AMBIGUOUS: "该元数据描述了多个实体，请逐个注册。",
  METADATA_NO_ENTITY_ID:
    "该元数据没有 entityID，无法把来自它的请求匹配到这条注册。",
  METADATA_NO_ACS:
    "该元数据没有声明 AssertionConsumerService，断言将无处投递。",
  METADATA_ACS_INVALID: "AssertionConsumerService 地址不可用。",
  METADATA_ACS_INSECURE:
    "AssertionConsumerService 地址在网络上使用了明文 http。",
  METADATA_ENTITY_ID_MISMATCH:
    "该元数据声明的实体 ID 不同，描述的是另一个服务提供方，请单独注册。",

  // --- CAS ---
  CAS_SERVICE_NOT_FOUND: "CAS 服务不存在。",
  CAS_SERVICE_TAKEN: "该 URL 前缀在本租户已被注册。",
  CAS_SERVICE_REQUIRED: "请填写服务 URL 前缀。",
  CAS_SERVICE_INVALID:
    "服务 URL 前缀必须是带主机名的 http 或 https 绝对地址，且不能带查询串或片段。",
  CAS_SERVICE_INSECURE:
    "服务 URL 前缀不能在网络上使用明文 http：投递到那里的票据在传输中可被读取。",
  CAS_SERVICE_WILDCARD:
    "不接受通配符。直接注册 URL 前缀即可，凡以它开头的地址都会匹配。",
  CAS_SERVICE_NOT_REGISTERED: "该服务未在本服务器注册。",

  // --- 用户组 ---
  GROUP_NOT_FOUND: "用户组不存在。",
  GROUP_NAME_TAKEN: "已有同名用户组。",
  GROUP_EXTERNAL_ID_TAKEN: "该 externalId 已绑定到另一个用户组。",
  MEMBER_NOT_FOUND: "其中有账号不在本租户内。",

  // --- 目录同步 ---
  SCIM_CREDENTIAL_NOT_FOUND: "目录同步凭据不存在。",
  SCIM_CREDENTIAL_NAME_TAKEN: "已有同名的目录同步凭据。",
  SCIM_UNAUTHORIZED: "该令牌不能用于目录同步。",
  EXTERNAL_ID_TAKEN: "该 externalId 已绑定到另一个账号。",

  // --- 目录同步 ---
  LDAP_SOURCE_NOT_FOUND: "目录不存在。",
  LDAP_SOURCE_NAME_TAKEN: "已有同名目录。",
  LDAP_SOURCE_DISABLED: "该目录已被停用。",
  INVALID_LDAP_ENCRYPTION: "加密方式只能是 none、STARTTLS 或 TLS。",
  INVALID_LDAP_PORT: "端口必须在 1 到 65535 之间。",
  INVALID_SYNC_INTERVAL: "自动同步间隔必须在 15 分钟到 7 天之间，或者关闭。",
  INVALID_LDAP_HOST:
    "主机名单独填写，不要带协议、路径或端口——它们是各自独立的字段。",
  LDAP_FIELD_REQUIRED:
    "主机、Base DN、用户过滤器，以及用户名、显示名、外部 ID 三个属性都必须填写。",
  // 订阅不能发送的自定义请求头名或值。服务端消息会指明是哪一条、为什么；这是兜底。
  INVALID_WEBHOOK_HEADER:
    "这个请求头不能发送。有一部分由 Portico 自己设置，且值里不能含换行。",
  NO_ENCRYPTION_KEY:
    "本部署未配置加密密钥，无法保存 Bind 密码。请让运维设置 PORTICO_ENCRYPTION_KEY，或改用匿名绑定。",

  // --- 事件订阅 ---
  WEBHOOK_NOT_FOUND: "订阅不存在。",
  WEBHOOK_NAME_TAKEN: "已有同名订阅。",
  INVALID_WEBHOOK_URL: "该接收地址不可用。",
  NO_EVENTS_SELECTED: "至少选择一个事件，或用 * 表示全部。",
  UNKNOWN_EVENT: "本版本不会发送该事件类型。",

  // --- 设置与筛选 ---
  INVALID_LAUNCH_URL: "访问地址必须是 http 或 https 的网址。",
  INVALID_LOGO_URI:
    "图标地址必须是 http 或 https 的网址，或本服务器上的路径（如 /icons/wiki.svg）。",
  INVALID_NAME: "名称不能为空。",
  INVALID_SETTINGS: "设置取值不合法。",
  INVALID_LOG_KIND: "日志类型不合法。",
  INVALID_TIMESTAMP: "日期时间格式不正确。",

  // --- 请求本身 ---
  MALFORMED_BODY: "请求内容无法解析。",
  EMPTY_BODY: "请求没有内容。",
  BODY_TOO_LARGE: "请求体过大。",
  WEBHOOK_DELIVERY_NOT_FOUND:
    "找不到这条投递记录。完成的记录 30 天后会被清理。",
  INVALID_CURSOR: "这个翻页标记已经失效，请回到第一页。",
  INVALID_DELIVERY_FILTER: "这不是该列表支持的筛选方式。",
  UNSUPPORTED_MEDIA_TYPE: "该接口不接受这种格式的请求。",
  TOO_MANY_ATTEMPTS: "该地址的尝试次数过多，请稍候再试。",
  // 自助试用，只在演示环境上。这几条每一条都是访客自己能处理的事，所以没有一条
  // 是干巴巴的拒绝——一个进不来的陌生人没有任何客服可问。
  NOT_FOUND: "没有找到。",
  TENANT_CONFIRM_MISMATCH: "请一字不差地输入租户编码以确认。",
  TENANT_CANNOT_DISABLE_DEFAULT:
    "默认租户不能在这里停用 —— 这个控制台就是它提供的。请用命令行。",
  TRIAL_SIGNUP_CLOSED: "本部署不提供自助试用。",
  TRIAL_MAIL_UNAVAILABLE:
    "试用需要邮件，而本部署没有配置邮件中继。请告知运维。",
  // 与 UNAVAILABLE 是两回事：那条是「没配」，这条是「配了但这一刻发不出去」——
  // 配额、凭据、网络。前者要找运维，后者过一会儿再试就行，所以措辞不同。
  TRIAL_MAIL_FAILED: "确认邮件这会儿发不出去，请过一分钟再试。",
  TRIAL_QUOTA_REACHED: "演示环境已满。请稍后再试，或者自己部署一套 Portico。",
  TRIAL_CODE_TAKEN: "该租户编码已被占用，请换一个。",
  TRIAL_EMAIL_USED: "该邮箱已经有一个试用租户了，直接用它登录即可。",
  TRIAL_TOO_MANY: "今天从这个地址申请的试用次数过多。",
  TRIAL_TOO_MANY_FOR_EMAIL:
    "今天已经往这个邮箱发过几封链接了。先看看收件箱和垃圾邮件，或者明天再来。",
  TRIAL_BUSY: "演示环境这一小时发放的试用已经到上限，请一小时后再试。",
  TRIAL_EMAIL_DOMAIN_BLOCKED:
    "这里不接受该邮箱服务商。请用一个能联系到你的地址。",
  TRIAL_LINK_INVALID: "这个链接无效。请重新申请试用。",
  TRIAL_LINK_EXPIRED: "这个链接已过期。请重新申请试用。",
  TRIAL_LINK_SPENT: "这个链接已经用过了。请用它发给你的凭据登录。",
  COMPANY_REQUIRED: "请填写组织名称。",
  INVALID_INDUSTRY: "请从提供的行业里选一个。",
  INVALID_PATH_PARAMETER: "地址不合法。",
  ROUTE_NOT_FOUND: "接口不存在。",
  METHOD_NOT_ALLOWED: "该接口不支持此方法。",
  ALREADY_EXISTS: "该记录已存在。",
  INTERNAL_ERROR: "服务端出错了。",
  // --- 自定义用户属性与字段目录 ---
  USER_ATTRIBUTE_NOT_FOUND: "该属性不存在。",
  USER_ATTRIBUTE_KEY_TAKEN:
    "该键名已被占用——键名在内置字段与你自己定义的字段之间共用一个命名空间。",
  INVALID_USER_ATTRIBUTE_KEY:
    "键名为 3 到 40 个字符，只能用小写字母、数字和下划线，且以字母开头。",
  INVALID_USER_ATTRIBUTE_KIND: "类型只能是文本、数字、是/否、日期或单选。",
  USER_ATTRIBUTE_LABEL_REQUIRED: "名称必填：它就是表单上显示的那一行。",
  USER_ATTRIBUTE_NEEDS_VALUES: "单选类型至少要有一个可选值。",
  TOO_MANY_USER_ATTRIBUTES:
    "自定义属性数量已达上限。每一个都可能被映射出站，而映射出去的属性就是每个令牌里的字节。",
  INVALID_USER_ATTRIBUTE_VALUE: "这个值与该属性的类型不符。",
  UNKNOWN_FIELD: "没有这个字段。只有字段目录里的字段可以被映射。",
  MAPPING_TARGET_REQUIRED: "填上对方期望的名称，或者把这个字段关掉。",
  DUPLICATE_MAPPING_SOURCE:
    "这个字段已经映射过了。一个字段只能有一条规则，两条的话谁生效取决于先读到哪条。",
  DUPLICATE_MAPPING_TARGET:
    "两个字段用了同一个名称发送。只有一个会到达，而且不是你能选的那个。",
  RESERVED_CLAIM_NAME:
    "这个名称由 OpenID Connect 保留，协议本身依赖它的含义。请换一个。",
  PAYLOAD_NAME_TAKEN: "事件载荷里这个名称已经用于别的字段了。请换一个。",
  RECIPIENT_NOT_FOUND: "这个应用或订阅不存在。",
  CLAIM_NAME_TAKEN: "这个应用已经在用这个 claim 名称接收另一个字段了。",
};
