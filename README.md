# Go Template（本地启动指南）

本项目已精简为可直接本地编译和运行的版本，默认使用 SQLite 存储，不依赖前端构建。下面仅保留本地启动相关说明。

## 前置要求
- Go 1.24+（已在 1.25 测试）
- macOS/Linux/Windows 任意环境

## 快速开始
1) 编译（可选）
```
go build -v -o bin/go-template .
```

2) 使用 SQLite 直接运行（推荐，最少依赖）
```
GIN_MODE=release \
PORT=3000 \
TZ=Asia/Shanghai \
go run .
```

或运行已编译的二进制：
```
PORT=3000 ./bin/go-template
```

3) 使用 MySQL + Redis 运行（生产场景常见）

- 准备 MySQL 连接串（示例）：
  - MySQL: `gotemplate:123456@tcp(127.0.0.1:3306)/go_template?charset=utf8mb4&parseTime=true&loc=Local`
  - PostgreSQL（可选）: `postgres://user:password@127.0.0.1:5432/go_template`

```
GIN_MODE=release \
PORT=3000 \
TZ=Asia/Shanghai \
SQL_DSN='gotemplate:123456@tcp(127.0.0.1:3306)/go_template?charset=utf8mb4&parseTime=true&loc=Local' \
REDIS_CONN_STRING='redis://127.0.0.1:6379/0' \
SESSION_SECRET='please_change_me' \
go run .
```

可选的数据库连接池配置（如需）：
```
SQL_MAX_IDLE_CONNS=100 \
SQL_MAX_OPEN_CONNS=1000 \
SQL_MAX_LIFETIME=60 \
go run .
```

4) 验证服务
浏览器或命令行访问：
```
http://127.0.0.1:3000/api/status
```

看到包含 `success: true` 的 JSON 即表示启动成功。

## 默认行为与说明
- 数据库：首次启动会在当前目录创建 `go-template.db`（SQLite），并自动创建一个初始管理员：
  - 用户名：`root`
  - 密码：`123456`
- 日志：默认写入 `./logs` 目录。
- 首页：未嵌入前端构建，访问根路径 `/` 返回一个简单的运行页。

## 环境变量（完整清单）
说明：所有变量均可通过环境变量直接注入（基于 Viper 自动映射），无需配置文件。

```
| 变量名                      | 默认值                          | 说明 |
|---------------------------|---------------------------------|-----|
| PORT                      | 3000                            | 服务监听端口 |
| GIN_MODE                  | release                         | Gin 运行模式：release/debug |
| TZ                        | Asia/Shanghai                   | 时区（影响日志、统计分组等） |
| LOG_LEVEL                 | info                            | 日志级别：debug/info/warn/error... |
| LOG_DIR                   | ./logs                          | 日志目录 |
| LOGS_FILENAME             | go-template.log                    | 日志文件名 |
| LOGS_MAX_SIZE             | 100                             | 单文件最大 MB |
| LOGS_MAX_AGE              | 7                               | 保留天数 |
| LOGS_MAX_BACKUP           | 10                              | 最大备份数 |
| LOGS_COMPRESS             | false                           | 是否压缩历史日志 |
| 
| SQL_DSN                   | （空）                          | 设置即用 MySQL/PostgreSQL；不设置则使用 SQLite |
| SQLITE_PATH               | go-template.db                     | SQLite 文件路径 |
| SQLITE_BUSY_TIMEOUT       | 3000                            | SQLite busy timeout（ms） |
| SQL_MAX_IDLE_CONNS        | 100                             | 连接池：最大空闲连接数 |
| SQL_MAX_OPEN_CONNS        | 1000                            | 连接池：最大打开连接数 |
| SQL_MAX_LIFETIME          | 60                              | 连接生命周期（秒） |
|
| REDIS_CONN_STRING         | （空）                          | 形如 redis://127.0.0.1:6379/0，设置即启用 Redis |
| REDIS_DB                  | 0                               | Redis DB 索引（若连接串未带 db，可用本变量指定） |
| SYNC_FREQUENCY            | 600                             | 同步频率（秒），为 0 时认为不启用 Redis 功能 |
|
| HTTPS                     | false                           | 处于 HTTPS 环境（影响 Cookie Secure） |
| TRUSTED_HEADER            | （空）                          | 可信代理源 IP 头，如 CF-Connecting-IP |
| FRONTEND_BASE_URL         | （空）                          | 从节点 Web 重定向到前端的地址 |
| NODE_TYPE                 | master                          | 节点类型：master/slave（slave 关闭 Cron） |
|
| LANGUAGE                  | zh_CN                           | 语言标识，用于部分文案默认值 |
| FAVICON                   | （空）                          | 网站 favicon 地址（留空则不设置） |
| USER_INVOICE_MONTH        | false                           | 是否开启用户月账单（保留字段） |
|
| SESSION_SECRET            | 随机生成                        | 会话加密密钥（建议显式设置） |
| POLLING_INTERVAL          | （空）                          | 轮询间隔（秒，保留字段） |
|
| MCP_ENABLE                | false                           | MCP 功能开关（保留字段） |
| UPTIME_KUMA_ENABLE        | false                           | Uptime Kuma 开关（保留字段） |
| UPTIME_KUMA_DOMAIN        | （空）                          | Uptime Kuma 域名（保留字段） |
| UPTIME_KUMA_STATUS_PAGE_NAME | （空）                      | Uptime Kuma 状态页（保留字段） |
|
| GLOBAL_API_RATE_LIMIT     | 300                             | 全局 API 速率阈值（次/窗口） |
| GLOBAL_WEB_RATE_LIMIT     | 300                             | 全局 WEB 速率阈值（次/窗口） |
| METRICS_USER              | （空）                          | Prometheus 指标的 BasicAuth 用户，未设置则 /api/metrics 返回 404 |
| METRICS_PASSWORD          | （空）                          | Prometheus 指标的 BasicAuth 密码 |
```

## 停止服务
- 前台运行：直接 `Ctrl + C`
- 后台运行（自行放入后台时）：找到进程并终止，或在启动脚本中保存 PID 以便停止。

## 使用 Docker Compose（可选）
项目已提供 `docker-compose.yml`，内含 MySQL 与 Redis，一条命令启动：
```
docker compose up -d
```
启动后访问 `http://127.0.0.1:3000/api/status` 验证，管理员初始账号为 `root/123456`。

## 版本信息注入（可选）
默认日志中的版本为 `v0.0.0`。为了在日志里打印真实版本/构建时间/提交号，可在构建或运行时通过 `-ldflags` 注入：

```
VER=$(git describe --tags --always || echo dev)
TIME=$(date -u +%Y-%m-%dT%H:%M:%SZ)
COMMIT=$(git rev-parse --short HEAD)

go run -ldflags "-X 'go-template/common/config.Version=$VER' \
               -X 'go-template/common/config.BuildTime=$TIME' \
               -X 'go-template/common/config.Commit=$COMMIT'" .
```

- 二进制构建同理：
```
go build -ldflags "-s -w -X 'go-template/common/config.Version=$VER' \
                        -X 'go-template/common/config.BuildTime=$TIME' \
                        -X 'go-template/common/config.Commit=$COMMIT'" -o bin/go-template .
```

Docker 与 CI 工作流已默认注入 `Version`，直接使用即可。
