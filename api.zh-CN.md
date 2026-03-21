# API 参考文档

[English](api.md) | 中文

`isdict-api` 服务的完整参考文档。

## 基础 URL 与版本

- 本地开发时的默认基础 URL：`http://localhost:8080`
- 词典与业务端点的根路径为 `/api/v1`；`/health` 等运维端点位于该前缀之外
- 该服务当前未启用认证；生产环境运行时请应用自己的网关或代理

## 响应封装

每个 API 端点返回在 `isdict-commons/model/response.go` 中定义的统一响应结构：

```json
{
  "success": true,
  "data": {},
  "error": null,
  "meta": null
}
```

**字段：**
- `success` (布尔值)：成功请求为 `true`，错误请求为 `false`
- `data` (任意类型)：响应数据（对象、数组或 null）
- `error` (对象|null)：当 `success` 为 `false` 时的错误详情，否则为 `null`
  - `code` (字符串)：错误码（见[错误码](#错误码)）
  - `message` (字符串)：人类可读的错误消息
  - `details` (任意类型)：可选的额外错误上下文
- `meta` (对象|null)：可选的元数据，用于分页和统计

词典相关端点的响应封装属于公开契约的一部分。为保持向后兼容，响应载荷不会做扁平化处理：单词对象仍使用 `headword` 和 `senses` 字段，发音数据仍保持 `pronunciations[]` 数组结构。

**特殊情况：** `/health` 和 `/api/v1/health` 为简单起见返回不带封装的纯 JSON。

## 共享注解字段

返回词条、变体、搜索结果、自动补全或短语建议的词典响应，都会复用上游的 `WordAnnotations` 字段集。

当前这组共享字段中已包含整型透传字段 `school_level`：

| 字段 | 类型 | 含义 |
|------|------|------|
| `school_level` | int | 面向中国英语学习者的推荐学习阶段：`0=unknown`、`1=初中`、`2=高中`、`3=大学` |

API 会保留数值编码返回，而不是直接输出本地化名称，方便调用方自行决定展示文本或徽标样式。

## 单词

### GET /api/v1/words/{headword}

返回指定词条的完整词典条目（不区分大小写）。

| 查询参数 | 类型 | 默认值 | 说明 |
|---------|------|--------|------|
| `accent` | string | — | 按口音枚举过滤发音（见[枚举](#枚举)）|
| `include_variants` | bool | `true` | 包含形态变体 |
| `include_pronunciations` | bool | `true` | 包含发音列表 |
| `include_senses` | bool | `true` | 包含释义和例句 |

```bash
curl "http://localhost:8080/api/v1/words/run?include_pronunciations=true&include_senses=true"
```

### GET /api/v1/words/{headword}/pronunciations

仅返回发音。支持与主端点相同的 `accent` 过滤器。

### GET /api/v1/words/{headword}/senses

仅返回释义/例句。

| 查询参数 | 类型 | 默认值 | 说明 |
|---------|------|--------|------|
| `pos` | string | — | 按词性过滤 |
| `lang` | string | `both` | `both`（双语）、`en`（英文）或 `zh`（中文）|

### GET /api/v1/words/by-variant/{variant}

将变体（如 "running"）解析回其规范单词。

| 查询参数 | 类型 | 默认值 | 说明 |
|---------|------|--------|------|
| `kind` | string | — | 过滤为 `form`（屈折形式）或 `alias`（别名）|
| `include_pronunciations` | bool | `true` | 包含发音 |
| `include_senses` | bool | `true` | 包含释义 |

### POST /api/v1/words/batch

在一次调用中批量查询最多 `API_BATCH_MAX_SIZE` 个单词。服务端会忽略重复和空白条目。

```json
{
  "words": ["hello", "world"],
  "include_variants": true,
  "include_pronunciations": true,
  "include_senses": true
}
```

响应的 `meta` 部分包含 `requested`（请求的）、`found`（找到的）和 `not_found`（未找到的）列表。

## 搜索与发现

### GET /api/v1/search

带排名和过滤器的模糊搜索。要求 `q` 的归一化结果至少包含 3 个 Unicode 字符。

| 查询参数 | 类型 | 默认值 | 说明 |
|---------|------|--------|------|
| `q` | string | — | 搜索关键词（必需；在去首尾空白、转小写并移除空格、连字符和下划线后，归一化结果至少 3 个 Unicode 字符）|
| `pos` | string | — | 小写词性过滤器（见[词性](#词性)）|
| `cefr_level` | string | — | `A1`、`A2`、`B1`、`B2`、`C1` 或 `C2`（不区分大小写）|
| `oxford_level` | int | — | 0（任意）、1（牛津3000）、2（牛津5000）|
| `cet_level` | int | — | 0（任意）、4（四级）、6（六级）|
| `max_frequency_rank` | int | — | 保留排名 ≤ 此值的单词 |
| `min_collins_stars` | int | — | 0-5（柯林斯最低星级）|
| `limit` | int | 20 | 最大结果数（上限为 `API_SEARCH_MAX_LIMIT`）|
| `offset` | int | 0 | 分页偏移量 |

### GET /api/v1/suggest

搜索框的自动完成建议。使用与 `/search` 相同的过滤器语义。

| 查询参数 | 类型 | 默认值 | 说明 |
|---------|------|--------|------|
| `prefix` | string | — | 搜索前缀（必需；在去首尾空白、转小写并移除空格、连字符和下划线后，归一化结果至少 3 个 Unicode 字符）|
| `cefr_level` | string | — | `A1`、`A2`、`B1`、`B2`、`C1` 或 `C2`（不区分大小写）|
| `oxford_level` | int | — | 0（任意）、1（牛津3000）、2（牛津5000）|
| `cet_level` | int | — | 0（任意）、4（四级）、6（六级）|
| `max_frequency_rank` | int | — | 保留排名 ≤ 此值的单词 |
| `min_collins_stars` | int | — | 0-5（柯林斯最低星级）|
| `limit` | int | 10 | 最大结果数（上限为 `API_SUGGEST_MAX_LIMIT`）|

### GET /api/v1/phrases

查找包含指定关键词的短语。

**参数：**
- `q`（必需）：在短语中搜索的关键词（1-50 个字符）
- `limit`：默认 10，最大 50

**示例：**
```bash
curl "http://localhost:8080/api/v1/phrases?q=run&limit=20"
```

## 运维端点

### GET /health

返回 HTTP 200 和 JSON 健康状态。适用于负载均衡器、可用性探针和其他轻量运维监控。

**响应：**
```json
{
  "status": "ok",
  "service": "isdict-api"
}
```

**注意：** 为简单起见，此端点不使用标准响应封装。

### GET /api/v1/health

readiness 端点。该端点会在请求上下文中执行数据库 ping，以确认 API 当前可连接 PostgreSQL。

**成功响应（`200 OK`）：**
```json
{
  "status": "ok",
  "service": "isdict-api"
}
```

**失败响应（`503 Service Unavailable`）：**
```json
{
  "status": "not_ready",
  "service": "isdict-api"
}
```

**注意：** 此端点同样返回纯 JSON，不使用标准响应封装。

## 枚举

### 口音代码

`british`（英式）、`american`（美式）、`australian`（澳式）、`newzealand`（新西兰）、`canadian`（加拿大）、`irish`（爱尔兰）、`scottish`（苏格兰）、`indian`（印度）、`southafrican`（南非）、`other`（其他）、`unknown`（未知）

所有口音参数不区分大小写。

### 词性

`noun`（名词）、`verb`（动词）、`adjective`（形容词）、`adverb`（副词）、`pronoun`（代词）、`preposition`（介词）、`conjunction`（连词）、`article`（冠词）、`interjection`（感叹词）、`determiner`（限定词）、`numeral`（数词）、`modal`（情态动词）、`auxiliary`（助动词）、`particle`（小品词）、`phrasal_verb`（短语动词）、`idiom`（习语）、`abbreviation`（缩写）、`character`（字符）、`affix`（词缀）、`contraction`（缩略）、`punctuation`（标点）、`postposition`（后置词）、`unknown`（未知）

所有词性参数不区分大小写。

### 变体形式类型

`past`（过去式）、`past_participle`（过去分词）、`present_3rd`（第三人称单数）、`gerund`（动名词）、`infinitive`（不定式）、`plural`（复数）、`possessive`（所有格）、`comparative`（比较级）、`superlative`（最高级）

## 错误码

| 代码 | 状态 | 说明 |
|------|------|------|
| `WORD_NOT_FOUND` | 404 | 单词或变体不存在 |
| `MISSING_PARAMETER` | 400 | 缺少必需的路径或查询参数 |
| `INVALID_PARAMETER` | 400 | 提供的值验证失败 |
| `BATCH_LIMIT_EXCEEDED` | 400 | 批量有效载荷超过配置的最大值 |
| `INTERNAL_ERROR` | 500 | 未处理的服务端错误；对外消息固定为 `An internal error occurred` |

服务端内部错误细节会连同请求元数据一起记录到日志中，不会暴露在 API 响应体里。

## 数据迁移权威来源

数据迁移行为以 `isdict-commons/migration` 为权威实现。

本仓库 `db/` 目录现在只保留 `db/sample_data.sql`，它仅用于测试或本地一次性样本数据导入，不应视为生产环境或兼容性保证下的规范迁移来源。

## 使用说明

- `/search` 与 `/suggest` 端点强制要求归一化后的查询长度至少为 3 个 Unicode 字符；归一化会去首尾空白、转小写，并移除空格、连字符和下划线；`/phrases` 接受 1-50 个去首尾空白后的字符
- 所有字符串参数会自动去除空白字符
- 枚举参数（口音、词性）不区分大小写并规范化为小写
- 建议：为频繁访问的单词和建议实现客户端缓存
- 生产环境：在反向代理层对大型响应应用 gzip/brotli 压缩
- 监控连接池设置（`DB_MAX_IDLE_CONNS`、`DB_MAX_OPEN_CONNS`）相对于 PostgreSQL `max_connections` 的值
- 批量端点（`/words/batch`）会自动去重并移除空条目
