# 开发进度 / Changelog

> 仅记录对外可见的能力变更与里程碑；项目内部规范文档见 `docs/`。

---

## v2.0.5 — 2026-05-21

### GPT Web 生图

- 移除 `gpt-web-image` sidecar，恢复为 Go provider 内唯一一套 GPT Web 生图实现，避免注册机需求额外改变生图运行路径。
- 生产编排删除 `KLEIN_GPT_WEB_IMAGE_SIDECAR_URL` 与 `gpt-web-image` 服务，后端不再转发生图请求到 Python sidecar。
- 生产编排补齐独立 `registrar` 服务，仅保留注册机相关 Python 服务。
- 恢复 ChatGPT Web 生图 `prepare` / `conversation` 请求的新会话根消息语义，重新使用 `client-created-root` 和稳定的 `client_prepare_state`，避免提示词挂到错误会话节点。
- 新增 GPT Web provider 单测，覆盖 Web 会话根消息 ID、请求状态和提示词位置，防止再次偏离 `main` 的可用协议。
- 已在移除前创建回滚 tag：`rollback-before-remove-gpt-web-image-a367acb`。

### 注册机 / Token 管理

- 修复注册机号池统计一直为 0 的问题：`/accounts/stats` 改为返回 DB 聚合统计，注册机读取 GPT provider 的 `available` 与 `quota_remaining`。
- 新增后台 `POST /admin/api/v1/auth/refresh`，注册机可在导入账号或读取号池统计前自动刷新 admin JWT，避免 refresh 404 后导入失败。
- 注册机停止逻辑改为协作取消：停止后不再继续提交新任务，等待邮箱验证码、FlareSolverr、重试等待会尽快退出；被停止的任务不计入失败。
- 账号导入 Klein 号池失败时不再把注册任务计为成功，修复全局平均耗时异常放大的问题。
- 修复号池模式检查间隔未生效的问题：线程池已满时不再每秒触发号池检查，等待提交新任务或下一轮检查时才按配置间隔读取号池。
- Token 管理页收紧“全部类型 / 全部状态”筛选布局，避免两个下拉各占一整行。
- 单账号检测或批量检测通过后，会把“熔断 / 失效”状态自动恢复为正常并清理错误信息；用户手动禁用的账号不会被自动启用。
- 已构建并部署后端、注册机与管理后台生产镜像，并复用既有 `klein-registrar-data` 数据卷。
- 验证 `klein-api /healthz` 正常，`klein-registrar /ping` 正常，`/admin/api/v1/auth/refresh` 返回 401 而非 404，注册机聚合统计可正常读取，旧 `klein-gpt-web-image` 容器已移除。

### 部署约束

- 后续生产更新统一使用 `klein-*` 服务命名：`klein-api`、`klein-openai`、`klein-admin`、`klein-worker`、`klein-user-web`、`klein-admin-web`、`klein-registrar`。
- 用户端反代必须显式指向 `klein-api` 与 `klein-openai`，不得回退到旧短名 `api` / `openai`，避免 Docker 网络中残留旧容器时请求落到旧后端。
- 旧短名容器 `api`、`openai`、`admin`、`worker` 已停止并删除，后续部署不得再并行拉起同职责的第二套后端服务。

---

## v2.0.4 — 2026-05-20

### Token（账号）管理

- 修复“检测失效账号”误禁用 403 账号的问题：现在只有 401 或明确凭证失效会自动禁用，403 仅记录为检测失败。
- 批量失效检测结果新增失败账号统计，前端提示会区分“已禁用”和“检测失败但未禁用”。
- 新增 `POST /admin/api/v1/accounts/batch-status` 批量修改状态接口，支持批量改为正常、禁用、熔断；恢复正常时会清理熔断冷却和最近错误。
- Token 管理页新增“批量状态”操作，以及“状态”筛选（正常 / 禁用 / 熔断），便于筛选 401 后被禁用的账号并全选删除。
- OAuth 刷新在账号检测流程中复用同一次解析出来的代理，避免全局随机代理模式下刷新与检测走不同出口。
- 修复账号测试、OAuth 刷新、Grok 额度检测相关提示中的中文乱码。
- 已构建并部署后端与管理后台生产镜像。

---

## v2.0.3 — 2026-05-20

### GPT Web 生图部署验证

- 修复 `gpt-web-image` sidecar 对 `ref_assets=null` 的兼容问题，避免无参考图生图请求被 FastAPI/Pydantic 拒绝为 422。
- 生产编排新增并部署 `gpt-web-image` 内部服务，后端通过 `KLEIN_GPT_WEB_IMAGE_SIDECAR_URL=http://gpt-web-image:8080` 调用该服务执行 ChatGPT Web 生图。
- 已构建并部署后端与 sidecar 生产镜像。
- 验证 `/v1/health` 正常，`gpt-image-2` 生图任务成功并返回可下载图片。

---

## v2.0.2 — 2026-05-19

### GPT 生图

- 修复 `gpt-image-2` ChatGPT Web 生图路径未传递账号 `session_token_enc` 的问题，带 Session Token 的账号现在会以登录态 Cookie 进行 Web bootstrap。
- Web 生图客户端改为每次任务独立 Cookie Jar，首页 bootstrap 使用页面导航头，并且无代理时也使用 uTLS 浏览器指纹直连，减少首页 bootstrap 被拦截的概率。
- GPT Web 生图调度会优先选择带 Session Token 的 OAuth 账号；没有可用 Session Token 账号时仍回落到普通 OAuth access token 账号。
- GPT Web 生图优先选择已绑定代理的 Session Token 账号；bootstrap 403 / 连接拒绝会按临时路径错误换账号重试，避免单个出口或账号风控直接失败。
- GPT Web 生图同一任务内重试会排除已失败账号，避免固定命中同一个代理绑定账号导致重试无效。
- GPT Web `chat-requirements / prepare / conversation` 阶段的 Cloudflare 403 也按临时路径错误换账号重试。
- GPT Web 上游返回 `token_invalidated` / 401 时会禁用当前账号并换下一个账号重试，避免单个失效 AT 终止整批任务。
- GPT Web 首页预热失败不再直接终止任务，后续改由 `/backend-api/sentinel/chat-requirements` 与对话接口判断真实可用性。
- GPT Web 生图会通过 FlareSolverr 预取 ChatGPT Cloudflare cookies 并注入任务 Cookie Jar，可通过 `KLEIN_GPT_WEB_FLARESOLVERR_URL=off` 关闭。
- 新增 `gpt-web-image` 内部 sidecar，通过 `curl_cffi` 浏览器指纹执行 ChatGPT Web 生图；Go 后端在 `KLEIN_GPT_WEB_IMAGE_SIDECAR_URL` 配置存在时优先转发到 sidecar，继续由现有账号池负责调度、重试、计费与结果缓存。
- 非 Web 的 `gpt-image-2` 路径没有 Codex 专用 OAuth 账号时，会回落到普通 OAuth 账号走 `/v1/responses`，便于在 Web CF 拦截时继续验证 API 生图能力。
- Web bootstrap 403 诊断新增 `has_session_token` 标记，方便区分匿名态/登录态访问被 Cloudflare 拦截。

---

## v2.0.1 — 2026-05-04

### 视频生成

- 修复 默认分辨率仍按 `480p` 下发的问题：常量 `defaultVideoResolution` 改为 `1080p`，`defaultVideoSize` 改为 `1920x1080`
- 新增 `quality` 入参，`standard / draft → 720p`、`hd → 1080p`，并按 `aspect_ratio`（`16:9 / 9:16 / 1:1`）正确推导宽高
- 兜底宽高 由硬编码 `1280x720` 改为 `videoConfig` 推导出来的默认宽高，保留后续接入 4K 等更高分辨率的扩展位

### 代理管理

- 新增 `POST /admin/api/v1/proxies/import` 批量导入：按行解析 `scheme://user:pass@host:port#name`，密码 AES-256-GCM 落盘
- 新增 `POST /admin/api/v1/proxies/batch-delete` 批量软删除
- 新增 `POST /admin/api/v1/proxies/batch-test` 批量测试，信号量并发 4，返回 `tested / ok / failed`
- 前端 `ProxiesPage` 重写：批量导入 / 批量删除 / 批量测试 / 多选交互

### Token（账号）管理

- 列表新增 `plan_type` 过滤项（`basic / super / heavy`），通过 `oauth_meta` JSON 字段查询
- 导入后自动并发探测（信号量并发 4）GROK Cookie 账号，识别 plan 类型后回填，导入结果新增 `detected / pending / failed` 字段
- 新增 `POST /admin/api/v1/accounts/batch-assign-proxy` 批量代理分配：
  - `mode = single`：所有选中账号绑定到同一个 `proxy_id`
  - `mode = cycle`：按 `idx % len(proxy_ids)` 轮询绑定到 `proxy_ids` 列表
- 前端 `TokenAccountsPage` 重写：账号类型列、按类型过滤、批量代理分配弹窗、导入结果回显

### 系统配置

- 新增 `proxy.selection_mode` 全局代理选择模式：`fixed`（固定代理） / `random`（随机代理）
- `random` 模式下，每次任务通过 `crypto/rand` 从启用代理列表中随机挑一个
- 账号级 `proxy_id` 仍始终优先于全局策略
- 前端 `ConfigPage` 新增「全局代理模式」下拉与说明文案

### 其他

- 清理界面与文档中暴露给最终用户的源码入口与品牌点
- 修复 `resolveProxyURL` 在 `account_test_service` 与 `generation_service` 中的重复实现，统一走 `proxySvc`

---

## v2.0.0 — 2026-04-27

- 统一文字、图片、视频三条生成链路
- 统一账号池、代理、刷新、熔断、轮换、用量检测
- 统一 OpenAI 兼容 API（`/v1/chat/completions`、`/v1/images/*`、`/v1/video/*`、`/v1/models`）
- 统一管理后台：用户、账单、CDK、优惠码、模型价格、请求日志、上游日志
- 统一部署：Docker Compose 一键拉起，可平滑迁移到 K8s

---

## v1.0.x

历史稳定基线，仅作为对照保留，新需求不再回灌。

---

## 当前已具备模块

| 模块 | 状态 | 备注 |
|------|------|------|
| 后端 API / Admin / OpenAI / Worker | ✅ | 4 个 cmd 二进制 + healthz / readyz |
| GPT / GROK 账号池 | ✅ | 批量导入 · 自动探测 · 熔断 · 轮换 |
| 代理池 | ✅ | 批量导入 · 批量测试 · 固定 / 随机回落 |
| 系统配置中心 | ✅ | OAuth 刷新窗口 · 代理策略 · 数据保留 |
| 用户前台 | ✅ | 文 / 图 / 视频 · 历史 · 密钥 · 账户 |
| 管理后台 | ✅ | Token / 代理 / 用户 / 计费 / CDK / 日志 |
| OpenAI 兼容层 | ✅ | chat / images / video / models |
| 计费体系 | ✅ | 积分 · 充值 · CDK · 模型价格 |
| 部署体系 | ✅ | Docker Compose 单机 · 反代 · SSL |

## 后续路线

- 前端图片 / 视频任务面板暴露 `quality` 选项与 4K 选项预留
- 上游日志按账号 / 代理维度的聚合视图
- 限流策略表单化（当前部分仍在配置文件）
- K8s Helm Chart 预研
