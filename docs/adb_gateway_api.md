## ADB Gateway HTTP API 文档

### 鉴权与前缀

- 所有接口需要在 Header 中携带：
  - `Authorization: Bearer <token>`
  - 当后端未配置 `-api-token` / `ADB_GATEWAY_TOKEN` 时，可不带。
- 所有接口统一前缀：`http://<api-listen>/api`
- 默认 API 监听地址：`0.0.0.0:8080`（可通过 `-api-listen` 参数或 `API_LISTEN` 环境变量配置）

### CORS 支持

网关默认启用 CORS，允许所有来源（`Access-Control-Allow-Origin: *`），支持以下方法：
- `GET`
- `POST`
- `OPTIONS`

### HTTP 状态码

- **200 OK**：请求成功
- **400 Bad Request**：请求参数错误或业务逻辑错误
- **401 Unauthorized**：未授权（token 错误或缺失）
- **404 Not Found**：资源不存在
- **500 Internal Server Error**：服务器内部错误

### 错误响应格式

所有错误响应统一使用以下格式：

```json
{
  "error": "错误描述信息"
}
```

网关当前的核心资源是 **Mapping**（一条 `listen -> upstream` 的 ADB 转发映射）。

---

### 数据结构

#### Mapping（响应体）

```json
{
  "id": "c8f8a2ac-0f18-4f1a-9c39-1e0b1a3c2f9b",
  "project_id": "proj-001",
  "from_id": "from-001",
  "to_id": "to-001",
  "name": "sandbox-phone-01",
  "note": "用于 sandbox 回放",
  "listen": "127.0.0.1:5556",
  "upstream": "192.168.1.10:5555",
  "status": "running",
  "last_error": "",
  "created_at": "2025-12-18T12:40:00Z"
}
```

- `id`：**网关内部生成的全局唯一 ID（UUID）**，适合作为外部系统（如 sandbox / 数据库）引用此映射的主键。
- `project_id`：项目 ID，创建时可传入，如果为空网关会自动生成。
- `from_id`：来源 ID，例如调用方 / sandbox 侧 ID，创建时可传入，如果为空网关会自动生成。
- `to_id`：目标 ID，例如真实设备在外部系统中的 ID，创建时可传入，如果为空网关会自动生成。
- `name`：人类可读的名称，前端创建 / 编辑时必填，建议在网关内唯一。
- `note`：可选备注。
- `listen`：本地监听地址（`host:port`）。如果创建时未指定，网关会自动分配一个可用端口，格式为 `0.0.0.0:端口`。
- `upstream`：实际设备 ADB 地址（`host:port`）。**可以为空**，表示暂未绑定设备（状态会显示为 `pending`）。
- `status`：`running` / `pending` / `stopped` / `error`。
  - `running`：正常运行，已绑定 upstream。
  - `pending`：等待绑定 upstream（upstream 为空）。
  - `stopped`：已停止。
  - `error`：错误状态。
- `last_error`：最近一次错误信息（可空）。
- `created_at`：创建时间，RFC3339。

#### MappingSpec（请求体）

```jsonc
// 创建 / 更新共用结构：
{
  "id": "c8f8a2ac-0f18-4f1a-9c39-1e0b1a3c2f9b", // 仅更新时必填，创建时忽略
  "project_id": "proj-001",                     // 可选，创建时不传则网关生成
  "from_id": "from-001",                        // 可选，创建时不传则网关生成
  "to_id": "to-001",                            // 可选，创建时不传则网关生成
  "name": "sandbox-phone-01",                   // 必填
  "note": "可选备注",                             // 可选
  "listen": "0.0.0.0:5556",                     // 可选，创建时不传则自动分配可用端口（格式：0.0.0.0:端口）
  "upstream": "192.168.1.10:5555",              // 可选，可以为空创建"待绑定"映射
  "force_disconnect": false                     // 可选，仅更新时有效；为 true 时会强制断开所有现有连接
}
```

---

### 映射管理接口

#### 1. 查询所有映射

- **GET** `/api/mappings`
- **描述**：返回当前所有映射列表。
- **请求参数**：无
- **请求体**：无
- **响应状态码**：200
- **响应体**：`Mapping[]`（数组）

**响应示例**：

```json
[
  {
    "id": "c8f8a2ac-0f18-4f1a-9c39-1e0b1a3c2f9b",
    "project_id": "proj-001",
    "from_id": "from-001",
    "to_id": "to-001",
    "name": "sandbox-phone-01",
    "note": "用于 sandbox 回放",
    "listen": "127.0.0.1:5556",
    "upstream": "192.168.1.10:5555",
    "status": "running",
    "last_error": "",
    "created_at": "2025-12-18T12:40:00Z"
  }
]
```

**curl 示例**：

```bash
curl -X GET http://127.0.0.1:8080/api/mappings \
  -H "Authorization: Bearer your_secret_token"
```

---

#### 2. 查询单个映射

- **GET** `/api/mappings/:id`
- **描述**：按 ID 查询映射详情。
- **路径参数**：
  - `id`：映射的网关内部 ID（`Mapping.id`，UUID 格式）。
- **响应状态码**：
  - **200**：成功，返回 `Mapping` 对象
  - **404**：`{"error":"not found"}`（找不到该 ID）

**响应示例**：

```json
{
  "id": "c8f8a2ac-0f18-4f1a-9c39-1e0b1a3c2f9b",
  "project_id": "proj-001",
  "from_id": "from-001",
  "to_id": "to-001",
  "name": "sandbox-phone-01",
  "note": "用于 sandbox 回放",
  "listen": "127.0.0.1:5556",
  "upstream": "192.168.1.10:5555",
  "status": "running",
  "last_error": "",
  "created_at": "2025-12-18T12:40:00Z"
}
```

**curl 示例**：

```bash
curl -X GET http://127.0.0.1:8080/api/mappings/c8f8a2ac-0f18-4f1a-9c39-1e0b1a3c2f9b \
  -H "Authorization: Bearer your_secret_token"
```

---

#### 3. 创建映射

- **POST** `/api/mappings/create`
- **描述**：创建一条新的 `listen -> upstream` 映射。
- **请求体**：`MappingSpec`（创建场景，`id` 字段会被忽略）

```json
{
  "name": "sandbox-phone-01",
  "note": "用于 sandbox 回放",
  "listen": "0.0.0.0:5556",
  "upstream": "192.168.1.10:5555"
}
```

或者不传 `listen`，由网关自动分配：

```json
{
  "name": "sandbox-phone-01",
  "note": "用于 sandbox 回放",
  "upstream": "192.168.1.10:5555"
}
```

或者创建一个"待绑定"的映射（upstream 为空），后续再通过 Update 接口设置：

```json
{
  "name": "sandbox-phone-02",
  "note": "等待设备分配"
}
```

- 说明：
  - `id` 字段在创建时 **会被忽略**，由网关自动生成 UUID。
  - `listen` 字段在创建时 **可选**，如果不传，网关会自动分配一个未被占用的端口，格式为 `0.0.0.0:端口`。
  - `upstream` 字段在创建时 **可选**，如果不传或为空，映射会处于 `pending` 状态，等待后续通过 Update 接口绑定设备。
  - 必填项：`name`。

- **响应状态码**：
  - **200**：成功，返回创建成功后的 `Mapping`（带生成好的 `id`、`listen`（如果未传）、`project_id`、`from_id`、`to_id` 等字段）。
  - **400**：请求错误，常见错误信息：
    - `"invalid json"`：请求体 JSON 格式错误
    - `"name must not be empty"`：名称不能为空
    - `"another mapping is already listening on 0.0.0.0:5556"`：监听地址已被占用
    - `"upstream address 0.0.0.0:5556 conflicts with existing listen address of mapping xxx"`：upstream 地址不能是已有的 listen 地址
    - `"failed to find available port: ..."`：自动分配端口失败（通常是因为配置的端口范围已用完）
    - `"failed to listen on ..."`：监听失败（端口被占用或权限不足）

- 示例（curl）：

```bash
# 指定 listen 地址
curl -X POST http://127.0.0.1:8080/api/mappings/create \
  -H "Authorization: Bearer your_secret_token" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "sandbox-phone-01",
    "note": "用于 sandbox 回放",
    "listen": "0.0.0.0:5556",
    "upstream": "192.168.1.10:5555"
  }'

# 不传 listen，由网关自动分配端口
curl -X POST http://127.0.0.1:8080/api/mappings/create \
  -H "Authorization: Bearer your_secret_token" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "sandbox-phone-02",
    "upstream": "192.168.1.11:5555"
  }'

# 创建待绑定映射（upstream 为空）
curl -X POST http://127.0.0.1:8080/api/mappings/create \
  -H "Authorization: Bearer your_secret_token" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "sandbox-phone-03",
    "note": "等待设备分配"
  }'
```

---

#### 4. 更新映射

- **POST** `/api/mappings/update`
- **描述**：修改已有映射的 `name` / `note` / `listen` / `upstream`，可选择是否强制断开现有连接。
- **请求体**：`MappingSpec`（更新场景，`id` 字段必填）

**示例1：普通更新（不断开现有连接）**

```json
{
  "id": "c8f8a2ac-0f18-4f1a-9c39-1e0b1a3c2f9b",
  "name": "sandbox-phone-01-renamed",
  "note": "备注更新",
  "listen": "127.0.0.1:5557",
  "upstream": "192.168.1.10:5555"
}
```

**示例2：切换设备并强制断开所有连接（热切换）**

```json
{
  "id": "c8f8a2ac-0f18-4f1a-9c39-1e0b1a3c2f9b",
  "name": "sandbox-phone-01",
  "note": "切换到设备B",
  "listen": "127.0.0.1:5557",
  "upstream": "192.168.1.20:5555",
  "force_disconnect": true
}
```

**示例3：为待绑定的映射设置 upstream**

```json
{
  "id": "c8f8a2ac-0f18-4f1a-9c39-1e0b1a3c2f9b",
  "name": "sandbox-phone-01",
  "upstream": "192.168.1.10:5555"
}
```

**示例4：清空 upstream（取消绑定设备）**

```json
{
  "id": "c8f8a2ac-0f18-4f1a-9c39-1e0b1a3c2f9b",
  "name": "sandbox-phone-01",
  "upstream": ""
}
```

- 说明：
  - `id`：**必填**，指定要更新的映射。
  - `name`：**必填**。
  - `listen` / `upstream`：可选；如不传则保持原值。
    - `upstream` 字段的特殊处理：
      - **不传** `upstream` 字段：保持原值不变
      - **传空字符串** `"upstream": ""`：清空 upstream，映射变为 `pending` 状态
      - **传具体值** `"upstream": "192.168.1.10:5555"`：设置新的 upstream
  - `force_disconnect`：**可选**，默认为 `false`。
    - `false`（默认）：温和更新，已有连接继续使用旧配置，新连接使用新配置。
    - `true`：强制更新，关闭并重启 listener，**断开所有现有连接**。适合设备热切换场景。
  - 如 `listen` 变更，会自动关闭旧 listener 并在新地址上重新监听（等同于 `force_disconnect=true`）。

- **响应状态码**：
  - **200**：成功，返回更新后的 `Mapping`。
  - **400**：请求错误，常见错误信息：
    - `"invalid json"`：请求体 JSON 格式错误
    - `"id is required for updating mapping"`：更新时必须提供 id
    - `"name must not be empty"`：名称不能为空
    - `"mapping not found: ..."`：找不到指定的映射
    - `"another mapping is already listening on ..."`：新的监听地址已被其他映射占用
    - `"upstream address 0.0.0.0:5556 conflicts with existing listen address of mapping xxx"`：upstream 地址不能是已有的 listen 地址
    - `"failed to listen on ..."`：监听失败（端口被占用或权限不足）

**curl 示例**：

```bash
# 普通更新
curl -X POST http://127.0.0.1:8080/api/mappings/update \
  -H "Authorization: Bearer your_secret_token" \
  -H "Content-Type: application/json" \
  -d '{
    "id": "c8f8a2ac-0f18-4f1a-9c39-1e0b1a3c2f9b",
    "name": "sandbox-phone-01-renamed",
    "note": "备注更新"
  }'

# 强制断开连接并切换设备
curl -X POST http://127.0.0.1:8080/api/mappings/update \
  -H "Authorization: Bearer your_secret_token" \
  -H "Content-Type: application/json" \
  -d '{
    "id": "c8f8a2ac-0f18-4f1a-9c39-1e0b1a3c2f9b",
    "name": "sandbox-phone-01",
    "upstream": "192.168.1.20:5555",
    "force_disconnect": true
  }'
```

---

#### 5. 删除映射

- **POST** `/api/mappings/remove`
- **描述**：删除指定 ID 的映射并关闭其监听，同时断开所有相关连接。
- **请求体**：

```json
{
  "id": "c8f8a2ac-0f18-4f1a-9c39-1e0b1a3c2f9b"
}
```

- **响应状态码**：
  - **200**：成功，返回 `{"status":"ok"}`
  - **400**：请求错误，常见错误信息：
    - `"invalid json or missing id"`：请求体格式错误或缺少 id 字段
    - `"mapping not found: ..."`：找不到指定的映射

**curl 示例**：

```bash
curl -X POST http://127.0.0.1:8080/api/mappings/remove \
  -H "Authorization: Bearer your_secret_token" \
  -H "Content-Type: application/json" \
  -d '{
    "id": "c8f8a2ac-0f18-4f1a-9c39-1e0b1a3c2f9b"
  }'
```

---

#### 6. 查询 ADB 命令日志

- **GET** `/api/logs/adb-commands`
- **描述**：从按天切分的网关日志文件中，查询某个映射在时间区间内的已解析 ADB 命令。日志文件按天切分，格式为 `adb-wifi-YYYY-MM-DD.log`。
- **Query 参数**（均为必填）：
  - `mapping_id`：映射 ID（即 `Mapping.id`，UUID 格式）。
  - `start`：起始时间，RFC3339 格式，例如：`2025-12-18T00:00:00Z`。
  - `end`：结束时间，RFC3339 格式，必须晚于 `start`。
- **响应状态码**：
  - **200**：成功，返回日志条目数组
  - **400**：请求参数错误
  - **500**：服务器内部错误（如日志文件读取失败）

**响应体**：`AdbCommandLogEntry[]`（数组），每项为：

```json
[
  {
    "time": "2025-12-18T12:44:53Z",
    "mapping_id": "c8f8a2ac-0f18-4f1a-9c39-1e0b1a3c2f9b",
    "project_id": "proj-001",
    "from_id": "from-001",
    "to_id": "to-001",
    "dir": "S->D",
    "conn_id": "c8f8a2ac-0f18-4f1a-9c39-1e0b1a3c2f9b#56486",
    "desc": "input tap 100 200"
  }
]
```

**字段说明**：
  - `time`：日志时间，RFC3339 格式（根据日志行前缀解析）。
  - `mapping_id`：映射 ID（必填）。
  - `project_id`：项目 ID（可选，可能为空）。
  - `from_id`：来源 ID（可选，可能为空）。
  - `to_id`：目标 ID（可选，可能为空）。
  - `dir`：数据方向，`S->D`（Source to Destination，客户端到设备）或 `D->S`（设备到客户端）。
  - `conn_id`：连接 ID，格式为 `mapping_id#端口号`。
  - `desc`：提取出来的"纯 ADB 命令"描述，例如 `input tap 100 200`、`ime set com.android.adbkeyboard/.AdbIME` 等。

**错误响应示例**：
  - `{"error":"missing mapping_id"}`：缺少 mapping_id 参数
  - `{"error":"missing start or end"}`：缺少 start 或 end 参数
  - `{"error":"invalid time format, expect RFC3339"}`：时间格式错误
  - `{"error":"end must be after start"}`：结束时间必须晚于起始时间
  - `{"error":"failed to open log file ..."}`：日志文件读取失败

**curl 示例**：

```bash
curl -X GET "http://127.0.0.1:8080/api/logs/adb-commands?mapping_id=c8f8a2ac-0f18-4f1a-9c39-1e0b1a3c2f9b&start=2025-12-18T00:00:00Z&end=2025-12-18T23:59:59Z" \
  -H "Authorization: Bearer your_secret_token"
```

---

### ADB 命令上报格式（Upload）

当在配置文件中开启 `upload.enabled = true` 时，网关在解析到 ADB SHELL / SYNC / ABB_EXEC 等可读命令后，会向配置的 `upload.url` 发送 HTTP POST，上报 JSON。

请求头示例（如果在配置中设置了 `upload.token`）：

```http
Authorization: Bearer <upload.token>
Content-Type: application/json
```

请求体示例：

```json
{
  "time": "2025-12-18T12:44:53Z",
  "from": "127.0.0.1:5556",
  "to": "192.168.1.10:5555",
  "adb_command": "ime set com.android.adbkeyboard/.AdbIME",
  "connection_id": "127.0.0.1:5556->192.168.1.10:5555#56486",
  "mapping_id": "c8f8a2ac-0f18-4f1a-9c39-1e0b1a3c2f9b",
  "project_id": "proj-001",
  "from_id": "from-001",
  "to_id": "to-001",
  "gateway_id": "gw-local"
}
```

- `time`：网关解析到该命令的时间，UTC，RFC3339 格式。
- `from`：本地监听地址（mapping 的 `listen` 字段）。
- `to`：实际设备地址（mapping 的 `upstream` 字段）。
- `adb_command`：已解析的 ADB 命令描述。
- `connection_id`：连接在网关内部的标识。
- `mapping_id`：该命令所属的映射 ID。
- `project_id`：该命令所属映射对应的项目 ID。
- `from_id`：来源 ID。
- `to_id`：目标 ID。
- `gateway_id`：来自配置文件中的 `gateway_id`。

接收端只需提供一个支持 `POST` JSON 的 HTTP 服务，根据上述字段进行存储或二次处理即可。

---

## 配置说明

### 端口自动分配

当创建映射时未指定 `listen` 地址，网关会自动从配置的端口范围内分配可用端口。默认端口范围为 `5555-65535`，可通过配置文件中的 `listen.min_port` 和 `listen.max_port` 进行调整。

### 数据库持久化

网关支持将映射信息持久化到 SQLite 数据库（默认路径：`mappings.db`）。启动时会自动从数据库加载已保存的映射并恢复监听。可通过配置文件中的 `database.path` 指定数据库路径。

### 日志轮转

网关日志按天切分，格式为 `adb-wifi-YYYY-MM-DD.log`。可通过配置文件中的 `log.max_days` 设置保留天数，超过保留期的日志文件会自动清理。

---

## 完整 API 端点列表

| 方法 | 路径 | 描述 |
|------|------|------|
| GET | `/api/mappings` | 查询所有映射 |
| GET | `/api/mappings/:id` | 查询单个映射 |
| POST | `/api/mappings/create` | 创建映射 |
| POST | `/api/mappings/update` | 更新映射 |
| POST | `/api/mappings/remove` | 删除映射 |
| GET | `/api/logs/adb-commands` | 查询 ADB 命令日志 |

---

## 注意事项

1. **端口冲突**：`upstream` 地址不能与任何现有映射的 `listen` 地址相同。
2. **连接管理**：更新映射时，如果不设置 `force_disconnect=true`，已有连接会继续使用旧的 `upstream` 地址，只有新连接会使用新地址。
3. **待绑定映射**：可以创建 `upstream` 为空的映射（状态为 `pending`），后续通过更新接口绑定设备。
4. **自动端口分配**：如果配置的端口范围已用完，创建映射时会返回错误。
5. **日志查询**：日志查询会扫描时间范围内的所有日志文件，如果某天的日志文件不存在，会自动跳过。


