# Klein Registrar Service

独立的 Python 注册机服务，从 chatgpt2api 提取核心注册逻辑。

## 功能

1. 完整的 ChatGPT 账号注册流程
2. 支持多种邮箱提供商（TempMail.lol, DuckMail, MoEmail, DDGMail 等）
3. 注册成功后自动导入 Klein 号池
4. 对接 Klein 前端注册管理页面

## API 接口

与 Klein Go 项目完全一致的接口：

- `GET /admin/api/v1/register` - 获取配置
- `POST /admin/api/v1/register` - 更新配置
- `POST /admin/api/v1/register/start` - 启动注册
- `POST /admin/api/v1/register/stop` - 停止注册
- `POST /admin/api/v1/register/reset` - 重置统计
- `GET /admin/api/v1/register/events` - SSE 事件流

## 配置说明

注册机配置包含以下字段：

```json
{
  "mail": {
    "request_timeout": 30,
    "wait_timeout": 30,
    "wait_interval": 2,
    "providers": [
      {
        "type": "tempmail_lol",
        "enable": true,
        "api_key": "",
        "domain": []
      }
    ]
  },
  "proxy": "http://proxy.example:7890",
  "flaresolverr_url": "http://klein-flaresolverr:8191",
  "klein_api_base": "http://klein-admin:17188",
  "klein_jwt": "<admin-jwt-token>",
  "total": 10,
  "threads": 3,
  "mode": "total",
  "target_quota": 100,
  "target_available": 10,
  "check_interval": 5
}
```

### 关键配置

- `klein_api_base`: Klein admin 服务的地址
- `klein_jwt`: 管理员 JWT token，用于调用账号导入 API
- `flaresolverr_url`: FlareSolverr 服务地址（绕过 Cloudflare）
- `proxy`: HTTP 代理地址

## 部署

### Docker 构建

```bash
cd registrar
docker build -t klein-registrar .
```

### Docker Compose

```yaml
services:
  klein-registrar:
    build: ./registrar
    container_name: klein-registrar
    ports:
      - "8080:8080"
    environment:
      - KLEIN_API_BASE=http://klein-admin:17188
    volumes:
      - ./registrar/data:/app/data
    depends_on:
      - klein-admin
```

## 前端对接

前端注册管理页面可以直接调用此服务的接口，接口格式与 Go 版本完全一致。

建议的部署方案：

1. Go admin 服务作为前端入口
2. Go admin 将注册机相关请求转发到 Python registrar 服务
3. 或者前端直接调用 Python registrar 服务（需要配置 CORS）
