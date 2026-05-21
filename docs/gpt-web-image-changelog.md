# GPT Web Image Change Log

## 2026-05-21

- 移除 GPT Web Image sidecar，恢复为 Go provider 内唯一一套 GPT Web 生图实现，避免注册机需求额外改变生图运行路径。
- 生产部署删除 `KLEIN_GPT_WEB_IMAGE_SIDECAR_URL` 与 `gpt-web-image` 服务，后端不再把生图请求转发到 Python sidecar。
- 生产部署补齐独立 `registrar` 服务，仅保留注册机相关 Python 服务。
- 恢复 ChatGPT Web 生图会话的根消息关联语义，`prepare` 与 `conversation` 请求重新使用 `client-created-root`，避免提示词挂到错误会话节点。
- 新增 provider 单测覆盖 Web 会话根消息 ID、请求状态与提示词位置。
- 移除前创建回滚 tag：`rollback-before-remove-gpt-web-image-a367acb`。
- 同步修复注册机与 Token 管理回归：注册机号池统计改读后端 DB 聚合统计，后台新增 admin JWT refresh，停止任务可中断等待，导入失败不再计为注册成功；Token 管理筛选布局收紧，检测通过后可把熔断/失效账号恢复正常。
- 部署验证：`klein-api /healthz` 正常，`klein-registrar /ping` 正常，`/admin/api/v1/auth/refresh` 不再 404，注册机聚合统计可正常读取，旧 `klein-gpt-web-image` 容器已移除。
