# isdict-api

[English](README.md) | 中文

[![Go Version](https://img.shields.io/badge/go-1.24-blue.svg)](https://golang.org)
[![Go Report Card](https://goreportcard.com/badge/github.com/simp-lee/isdict-api)](https://goreportcard.com/report/github.com/simp-lee/isdict-api)
[![License](https://img.shields.io/badge/license-MIT-green.svg)](LICENSE)
[![Test](https://github.com/simp-lee/isdict-api/workflows/Test/badge.svg)](https://github.com/simp-lee/isdict-api/actions)

基于 PostgreSQL 的英汉词典 API，使用共享的 [isdict-commons](https://github.com/simp-lee/isdict-commons) 模型。

## 核心特性

易思词典（IsDict）整合六大权威词典数据源（Wiktionary、Oxford、CEFR-J、CET、ECDICT、WordFreq），提供：

- **完整的词典数据**：71.5万+词条、发音、释义、例句、词形变化
- **多口音支持**：英美澳加新等10种口音的IPA音标，16万+发音数据
- **双语支持**：97.6万英文义项 + 26万中文翻译，58.4万真实例句中英对照
- **强大的搜索和查询功能**：前缀自动补全、短语智能匹配、88.8万词形变体反查
- **多维等级标注**：CEFR A1-C2、CET 四六级、Oxford 3000/5000、柯林斯星级、词频 TOP 50K
- **生产级中间件**：限流、CORS、缓存、请求 ID 追踪和超时控制，详见 [中间件功能](#中间件功能)
- **开箱即用**：健康检查、优雅关闭、连接池和请求限制
- **内置网页控制台**：轻量级 Alpine.js/Tailwind 单页应用，支持快速查询

完整 API 文档请查看 [api.zh-CN.md](api.zh-CN.md)。

## 快速开始

### 前置要求

- Go 1.24+
- PostgreSQL 14+
- 词典数据（示例 SQL 或完整转储）

### 本地运行

```bash
git clone https://github.com/simp-lee/isdict-api.git
cd isdict-api
go mod download
cp configs/api.example.env configs/api.env
# 编辑 configs/api.env 填写你的数据库凭证
go run cmd/api/main.go
```

- REST API: http://localhost:8080/api/v1
- 网页界面: http://localhost:8080
- 健康检查: http://localhost:8080/health

### Docker

```bash
docker build -t isdict-api .
docker run -d -p 8080:8080 --name isdict-api \
  -e DB_HOST=your-db-host \
  -e DB_USER=your-user \
  -e DB_PASSWORD=your-password \
  isdict-api
```

或使用 Docker Compose 创建包含 PostgreSQL 的完整本地环境：

```bash
docker-compose up -d
```

## 配置

运行时设置从环境变量和 `.env` 文件（如果存在）加载。搜索顺序为：环境变量 → `configs/api.env` → `.env` → `api/.env` → `../.env`，找到第一个文件后即停止。

| 变量 | 默认值 | 说明 |
|------|--------|------|
| `DB_HOST` | localhost | PostgreSQL 主机 |
| `DB_PORT` | 5432 | PostgreSQL 端口 |
| `DB_USER` | postgres | 数据库用户 |
| `DB_PASSWORD` | postgres | 数据库密码 |
| `DB_NAME` | isdict | 数据库名称 |
| `DB_SSLMODE` | prefer | PostgreSQL SSL 模式 |
| `PORT` | 8080 | HTTP 监听端口 |
| `GIN_MODE` | debug | `debug`、`release` 或 `test` |
| `DB_MAX_IDLE_CONNS` | 10 | 连接池空闲连接上限 |
| `DB_MAX_OPEN_CONNS` | 100 | 连接池最大打开连接数 |
| `API_BATCH_MAX_SIZE` | 100 | 批量请求最大单词数 |
| `API_SEARCH_MAX_LIMIT` | 100 | 搜索结果上限 |
| `API_SUGGEST_MAX_LIMIT` | 50 | 建议结果上限 |

### 中间件配置

| 变量 | 默认值 | 说明 |
|------|--------|------|
| `ENABLE_RATE_LIMIT` | true | 启用限流 |
| `RATE_LIMIT_RPS` | 100 | 每秒请求数限制 |
| `RATE_LIMIT_BURST` | 200 | 令牌桶突发容量 |
| `RATE_LIMIT_PER_HOUR` | 10000 | 每小时请求数限制 |
| `RATE_LIMIT_PER_DAY` | 0 | 每天请求数限制（0=禁用）|
| `ENABLE_CORS` | true | 启用 CORS 头 |
| `CORS_ALLOW_ORIGINS` | * | 允许的源（逗号分隔）|
| `ENABLE_TIMEOUT` | true | 启用请求超时 |
| `TIMEOUT_SECONDS` | 30 | 请求超时时长（秒）|
| `ENABLE_REQUEST_ID` | true | 启用请求 ID 生成 |
| `ENABLE_CACHE` | true | 启用响应缓存 |
| `CACHE_MAX_SIZE` | 1000 | 缓存最大条目数 |
| `CACHE_EXPIRATION_MINS` | 5 | 缓存条目过期时间（分钟）|

**说明：**
- `/health` 和静态文件不受限流限制
- 响应缓存仅应用于 `/api/*` 路径的 GET 请求
- 所有响应通过 `X-Request-Id` 头包含请求 ID
- 限流响应头：`X-Ratelimit-Limit`、`X-Ratelimit-Limit-Hour`、`X-Ratelimit-Remaining`

## 数据库

数据库架构和索引通过 `isdict-commons/migration` 中的数据库迁移管理。使用 `cmd/migrate-db` 工具进行所有数据库操作。

### 生产环境工作流

```bash
# 全新迁移（删除并重建所有表）
go run cmd/migrate-db/main.go --drop

# 增量迁移（创建缺失的表/索引）
go run cmd/migrate-db/main.go

# 验证迁移状态
go run cmd/migrate-db/main.go --verify
```

### 使用示例数据快速测试

对于快速本地测试，可以使用 `db/` 目录中的 SQL 辅助文件：

```bash
createdb isdict
psql -d isdict -f db/schema.sql
psql -d isdict -f db/indexes.sql
psql -d isdict -f db/sample_data.sql
```

**注意：** `db/` 中的 SQL 文件仅供参考和测试使用。它们可能落后于 `isdict-commons/migration` 中的权威迁移。

## 中间件功能

API 包含由 [ginx](https://github.com/simp-lee/ginx) 提供支持的生产级中间件：

- **限流**：令牌桶算法，可配置 RPS、突发和每小时/每天限制
- **CORS**：可配置源和方法的跨域资源共享
- **响应缓存**：自动 GET 请求缓存，可配置大小和 TTL
- **请求 ID**：用于调试和日志记录的唯一请求追踪
- **超时保护**：可配置的请求超时以防止资源耗尽
- **恐慌恢复**：从 panic 中自动恢复，返回结构化错误响应
- **结构化日志**：带时序和状态码的请求/响应日志记录

运行验证测试：
```bash
# 首先启动 API 服务器
go run cmd/api/main.go

# 在另一个终端运行中间件测试
go run tests/middleware/verify/main.go

# 或运行压力测试
go run tests/middleware/stress/main.go
```

## 网页控制台

- 从 `web/index.html` 提供的单页应用
- 带 CEFR/Oxford/CET 徽章和词频排名的即时建议
- 在专用标签页中显示变体、双语释义、发音和标记示例
- 开箱即用于 `http://localhost:8080`
- 如果指向远程 API 端点，请在 `index.html` 中配置 `apiBaseURL`

## 项目结构

```
cmd/
  api/          # API 入口点
  migrate-db/   # 数据库迁移工具
configs/        # 环境变量模板
db/             # SQL 参考（架构、索引、示例数据）
internal/       # 处理器、服务、仓库、配置
  api/          # API 层（handler、service、repository、middleware）
  config/       # 配置加载器
tests/          # 测试套件
  middleware/   # 中间件测试（basic、verify、stress）
web/            # 静态网页控制台（Alpine.js + Tailwind）
```

## 许可证

MIT 许可证。详见 `LICENSE` 文件。

为英语学习者和教育工作者打造。
