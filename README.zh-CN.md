# isdict-api

[English](README.md) | 中文

[![Go Version](https://img.shields.io/badge/go-1.25-blue.svg)](https://golang.org)
[![Go Report Card](https://goreportcard.com/badge/github.com/simp-lee/isdict-api)](https://goreportcard.com/report/github.com/simp-lee/isdict-api)
[![License](https://img.shields.io/badge/license-MIT-green.svg)](LICENSE)
[![Test](https://github.com/simp-lee/isdict-api/workflows/Test/badge.svg)](https://github.com/simp-lee/isdict-api/actions)

isdict-api 是一个基于 Gin 和 PostgreSQL 的英汉词典服务，提供 `/api/v1` 下的 REST API，同时暴露存活与就绪检查，并内置一个适合本地使用的网页界面。

完整接口字段和示例请查看 [api.zh-CN.md](api.zh-CN.md)。

## 核心特性

IsDict 整合了六类权威词典数据源（Wiktionary、Oxford、CEFR-J、CET、ECDICT、WordFreq），提供：

- **完整词典数据**：715,000+ 词条，覆盖发音、释义、例句和词形变化
- **多口音支持**：10 种口音的 IPA 音标（英音、美音、澳音、加音、新西兰等），160,000+ 发音记录
- **双语支持**：976,000 条英文义项 + 260,000 条中文释义，584,000 条真实双语例句
- **强大的搜索与查询**：前缀自动补全、短语智能匹配、888,000 条词形变体反查
- **多维等级标签**：CEFR A1-C2、CET-4/6、Oxford 3000/5000、Collins 星级、词频 TOP 50K
- **生产可用的中间件**：限流、CORS、缓存、请求 ID 跟踪和超时控制；详见 [中间件说明](#中间件说明)
- **开箱即用**：健康检查、优雅停机、连接池和请求限制
- **内置 Web 控制台**：基于 Alpine.js/Tailwind 的轻量单页界面，适合快速查词

## 包含内容

- 单词、变体、发音、释义、搜索、建议和短语查询接口
- 依赖 PostgreSQL，并在启动与校验阶段检查 `pg_trgm`
- 内置限流、CORS、超时、请求 ID、缓存、恢复和日志中间件
- 本地由服务直接提供 `/` 页面以及 `/static/js` 下的打包 JavaScript
- 提供 `cmd/migrate-db` 迁移工具

## 运行要求

- Go 1.25+
- PostgreSQL 14+
- Node.js 22+ 仅在运行前端回归测试或 `make test` 时需要

## 快速开始

```bash
git clone https://github.com/simp-lee/isdict-api.git
cd isdict-api
go mod download
cp configs/api.example.env configs/api.env
# 编辑 configs/api.env
make db-setup
make run
```

默认情况下，`make run` 和 `make db-setup` 会通过 `ISDICT_API_ENV_FILE` 加载 `configs/api.env`。

本地默认地址：

- API 基础路径: http://localhost:8080/api/v1
- 网页界面: http://localhost:8080/
- 存活检查: http://localhost:8080/health
- 就绪检查: http://localhost:8080/api/v1/health

## 配置

程序总是优先读取进程环境变量。

- 如果设置了 `ISDICT_API_ENV_FILE`，只加载该文件
- 否则依次尝试 `.env`、`api/.env`、`../.env`
- `DB_*` 同时支持 `PG*` 别名，例如 `PGHOST`、`PGPORT`、`PGUSER`、`PGPASSWORD`、`PGDATABASE`、`PGSSLMODE`

常用配置如下：

| 变量 | 默认值 | 说明 |
|------|--------|------|
| `DB_HOST` | `localhost` | PostgreSQL 主机 |
| `DB_PORT` | `5432` | PostgreSQL 端口 |
| `DB_USER` | `postgres` | PostgreSQL 用户 |
| `DB_PASSWORD` | `postgres` | PostgreSQL 密码 |
| `DB_NAME` | `isdict` | 数据库名 |
| `DB_SSLMODE` | `prefer` | 托管 PostgreSQL 通常应设为 `require` 或更严格 |
| `PORT` | `8080` | HTTP 监听端口 |
| `GIN_MODE` | `debug` | `debug`、`release`、`test` |
| `DB_MAX_IDLE_CONNS` | `10` | 连接池空闲连接上限 |
| `DB_MAX_OPEN_CONNS` | `100` | 连接池最大打开连接数 |
| `API_BATCH_MAX_SIZE` | `100` | `POST /api/v1/words/batch` 最大词数 |
| `API_SEARCH_MAX_LIMIT` | `100` | `/search` 的最大 `limit` |
| `API_SUGGEST_MAX_LIMIT` | `50` | `/suggest` 的最大 `limit` |
| `ENABLE_RATE_LIMIT` | `true` | 是否启用限流 |
| `ENABLE_CORS` | `true` | 是否启用 CORS |
| `ENABLE_TIMEOUT` | `true` | 是否启用超时中间件 |
| `TIMEOUT_SECONDS` | `30` | 请求超时时间 |
| `ENABLE_REQUEST_ID` | `true` | 是否注入 `X-Request-Id` |
| `ENABLE_CACHE` | `true` | 是否为 `/api/*` 的 GET 请求启用缓存 |
| `ISDICT_API_ENV_FILE` | 空 | 显式配置文件路径 |

就绪检查不仅验证数据库连通性，还会检查必需扩展是否可用。只有数据库可连接且 `pg_trgm` 存在时，`/api/v1/health` 才会返回 `200`。

## 数据库工作流

主流程是使用 `cmd/migrate-db` 迁移工具。

```bash
# 执行迁移
ISDICT_API_ENV_FILE=configs/api.env go run ./cmd/migrate-db

# 校验迁移管理对象
ISDICT_API_ENV_FILE=configs/api.env go run ./cmd/migrate-db --verify

# 删除并重建表，需要显式确认目标
ISDICT_API_ENV_FILE=configs/api.env \
go run ./cmd/migrate-db --drop --force --confirm-drop postgres@db.example.com:5432/isdict
```

辅助命令：

- `make db-setup`：执行迁移工具
- `make db-verify`：通过迁移工具校验受迁移管理的数据库对象
- `make db-reset`：带确认的重置流程；要求 `CONFIRM_DROP` 与目标库完全匹配
- `make db-fixtures`：针对空目标库，先通过权威迁移工具重建受迁移管理的表，再导入 `db/sample_data.sql`；要求 `CONFIRM_FIXTURES`

这四个数据库辅助命令现在使用统一的连接参数解析顺序：先读已导出的 `DB_*`，再读已导出的 `PG*`，最后回退到 `ISDICT_API_ENV_FILE` 指向的配置文件（Makefile 默认值为 `configs/api.env`）。

## HTTP 接口概览

| 方法 | 路径 | 说明 |
|------|------|------|
| `GET` | `/health` | 存活检查 |
| `GET` | `/api/v1/health` | 就绪检查 |
| `GET` | `/api/v1/words/:headword` | 完整词条 |
| `GET` | `/api/v1/words/:headword/pronunciations` | 仅发音 |
| `GET` | `/api/v1/words/:headword/senses` | 仅释义 |
| `GET` | `/api/v1/words/by-variant/:variant` | 通过变体反查词条 |
| `POST` | `/api/v1/words/batch` | 批量查词 |
| `GET` | `/api/v1/search` | 带过滤条件的搜索 |
| `GET` | `/api/v1/suggest` | 自动补全 |
| `GET` | `/api/v1/phrases` | 短语检索 |

当前实现中需要注意的请求规则：

- `cefr_level` 仅接受 `A1`、`A2`、`B1`、`B2`、`C1`、`C2`
- `/search` 必须提供 `q`，默认 `limit=20`
- `/suggest` 必须提供 `prefix`，默认 `limit=10`
- `/phrases` 必须提供 `q`，且 `limit` 最大为 `50`
- `/health` 和 `/api/v1/health` 返回纯 JSON，不使用标准响应封装

当前实现中需要注意的响应字段：

- `/api/v1/words`、`/api/v1/words/by-variant`、`/api/v1/words/batch`、`/api/v1/search`、`/api/v1/suggest`、`/api/v1/phrases` 返回的词类载荷都会包含上游共享注解字段
- 这组共享注解字段现在包含 `school_level`，数值含义为 `0=unknown`、`1=初中`、`2=高中`、`3=大学`
- API 会按整数值透传 `school_level`，由调用方自行决定展示文本或徽标样式

## 中间件说明

- `/health` 和 `/api/v1/health` 会跳过限流和响应缓存
- 普通 API 路由走超时中间件；就绪检查使用独立的数据库探测超时
- 本地静态资源由 API 进程直接提供在 `/`，并仅暴露 `/static/js` 下的打包 JavaScript

## 测试

```bash
# 默认完整测试流程
make test

# 仅运行 Go 测试
go test -v -race ./...

# 仅运行前端回归测试
node --test web/dictionary_app.test.mjs

# 中间件包测试
go test ./internal/api/middleware -count=1

# 中间件探针客户端
go run ./tests/middleware/basic
go run ./tests/middleware/verify
go run ./tests/middleware/stress
```

这些中间件探针客户端默认访问 `http://localhost:8080`，如有需要可通过环境变量覆盖。

## 项目结构

```text
cmd/            应用入口
configs/        环境变量模板
db/             示例夹具数据
internal/       API handler、middleware、应用日志、config
tests/          中间件探针程序与测试
web/            内置静态页面
```

## 许可证

MIT。详见 [LICENSE](LICENSE)。
