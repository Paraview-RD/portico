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
  ACCOUNT_LOCKED: "登录失败次数过多。请稍后再试，或联系管理员解锁账号。",
  MISSING_TOKEN: "尚未登录。",
  INVALID_TOKEN: "登录状态已失效，请重新登录。",
  TOKEN_EXPIRED: "登录已过期，请重新登录。",
  TOKEN_REVOKED: "登录状态已被终止，请重新登录。",
  MALFORMED_AUTHORIZATION: "凭据格式无法解析。",
  UNAUTHENTICATED: "尚未登录。",
  ADMIN_REQUIRED: "该操作需要管理员权限。",
  CURRENT_PASSWORD_MISMATCH: "当前密码不正确。",
  WEAK_PASSWORD: "该密码不符合要求。",

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
  LAST_ADMIN: "这是本租户最后一名在用管理员，请先指定另一名管理员。",
  REGISTRATION_DISABLED: "本部署未开放自主注册。",

  // --- 机构 ---
  ORGANIZATION_NOT_FOUND: "机构不存在。",
  ORGANIZATION_DISABLED: "该机构已被停用。",
  ORGANIZATION_CODE_TAKEN: "该机构编码已被占用。",
  NAME_REQUIRED: "请填写名称。",
  CODE_REQUIRED: "请填写编码。",

  // --- 批量导入 ---
  MISSING_FILE: "请选择要上传的文件。",
  INVALID_UPLOAD: "上传内容无法读取。",
  INVALID_SPREADSHEET: "该文件不是可读的 .xlsx 工作簿。",
  EMPTY_SPREADSHEET: "该工作簿没有数据行。",
  TOO_MANY_ROWS: "该工作簿行数超出单次导入上限。",
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

  // --- 设置与筛选 ---
  INVALID_SETTINGS: "设置取值不合法。",
  INVALID_LOG_KIND: "日志类型不合法。",
  INVALID_TIMESTAMP: "日期时间格式不正确。",

  // --- 请求本身 ---
  MALFORMED_BODY: "请求内容无法解析。",
  EMPTY_BODY: "请求没有内容。",
  BODY_TOO_LARGE: "请求体过大。",
  INVALID_PATH_PARAMETER: "地址不合法。",
  ROUTE_NOT_FOUND: "接口不存在。",
  METHOD_NOT_ALLOWED: "该接口不支持此方法。",
  ALREADY_EXISTS: "该记录已存在。",
  INTERNAL_ERROR: "服务端出错了。",
};
