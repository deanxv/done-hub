---
title: "环境变量"
layout: doc
outline: deep
lastUpdated: true
---

# 环境变量

::: warning 注意
环境变量优先级高于配置文件。
:::

本页面可能未及时更新，具体值可参考 [配置文件](https://raw.githubusercontent.com/MartialBE/one-hub/refs/heads/main/config.example.yaml)。

## 配置文件转环境变量

将配置文件中的变量名全部大写，遇到子集用下划线连接，例如：

```yaml
# 配置文件
user_token_secret: "your-secret-key"
logs:
  filename: "one-hub.log" # 日志文件名
```

转换为环境变量：

```bash
USER_TOKEN_SECRET="your-secret-key"
LOGS_FILENAME="one-hub.log"
```

## 环境变量说明

1. `REDIS_CONN_STRING`：设置之后将使用 Redis 作为缓存使用。
   - 例子：`REDIS_CONN_STRING=redis://default:redispw@localhost:49153`
   - 如果数据库访问延迟很低，没有必要启用 Redis，启用后反而会出现数据滞后的问题。
2. `SESSION_SECRET`：设置之后将使用固定的会话密钥，这样系统重新启动后已登录用户的 cookie 将依旧有效。
   - 例子：`SESSION_SECRET=random_string`
3. `SQL_DSN`：设置之后将使用指定数据库而非 SQLite，请使用 MySQL 或 PostgreSQL。
   - 例子：
     - MySQL：`SQL_DSN=root:123456@tcp(localhost:3306)/oneapi`
     - PostgreSQL：`SQL_DSN=postgres://postgres:123456@localhost:5432/oneapi`（适配中，欢迎反馈）
   - 注意需要提前建立数据库 `oneapi`，无需手动建表，程序将自动建表。
   - 如果使用本地数据库：部署命令可添加 `--network="host"` 以使得容器内的程序可以访问到宿主机上的 MySQL。
   - 如果使用云数据库：如果云服务器需要验证身份，需要在连接参数中添加 `?tls=skip-verify`。
   - 请根据你的数据库配置修改下列参数（或者保持默认值）：
     - `SQL_MAX_IDLE_CONNS`：最大空闲连接数，默认为 `100`。
     - `SQL_MAX_OPEN_CONNS`：最大打开连接数，默认为 `1000`。
       - 如果报错 `Error 1040: Too many connections`，请适当减小该值。
     - `SQL_CONN_MAX_LIFETIME`：连接的最大生命周期，默认为 `60`，单位分钟。
4. `FRONTEND_BASE_URL`：设置之后将重定向页面请求到指定的地址，仅限从服务器设置。
   - 例子：`FRONTEND_BASE_URL=https://openai.justsong.cn`
5. `MEMORY_CACHE_ENABLED`：启用内存缓存，会导致用户额度的更新存在一定的延迟，可选值为 `true` 和 `false`，未设置则默认为 `false`。
   - 例子：`MEMORY_CACHE_ENABLED=true`
6. `SYNC_FREQUENCY`：在启用缓存的情况下与数据库同步配置的频率，单位为秒，默认为 `600` 秒。
   - 例子：`SYNC_FREQUENCY=60`
7. `NODE_TYPE`：设置之后将指定节点类型，可选值为 `master` 和 `slave`，未设置则默认为 `master`。
   - 例子：`NODE_TYPE=slave`
8. `CHANNEL_UPDATE_FREQUENCY`：设置之后将定期更新渠道余额，单位为分钟，未设置则不进行更新。
   - 例子：`CHANNEL_UPDATE_FREQUENCY=1440`
9. `CHANNEL_TEST_FREQUENCY`：设置之后将定期检查渠道，单位为分钟，未设置则不进行检查。
   - 例子：`CHANNEL_TEST_FREQUENCY=1440`
10. `POLLING_INTERVAL`：批量更新渠道余额以及测试可用性时的请求间隔，单位为秒，默认无间隔。
    - 例子：`POLLING_INTERVAL=5`
11. `BATCH_UPDATE_ENABLED`：启用数据库批量更新聚合，会导致用户额度的更新存在一定的延迟可选值为 `true` 和 `false`，未设置则默认为 `false`。
    - 例子：`BATCH_UPDATE_ENABLED=true`
    - 如果你遇到了数据库连接数过多的问题，可以尝试启用该选项。
12. `BATCH_UPDATE_INTERVAL=5`：批量更新聚合的时间间隔，单位为秒，默认为 `5`。
    - 例子：`BATCH_UPDATE_INTERVAL=5`
13. 请求频率限制：
    - `GLOBAL_API_RATE_LIMIT`：全局 API 速率限制（除中继请求外），三分钟内的最大请求数，默认为 `300`。
    - `GLOBAL_WEB_RATE_LIMIT`：全局 Web 速率限制，三分钟内的最大请求数，默认为 `300`。
    - 计数口径分两种情况：
      - **能识别出登录用户时**（携带有效 Web 会话），同时占用**用户**配额与**来源 IP**上限两个桶，任一满即 `429`。用户配额为上面配置的值，IP 上限为其 4 倍。这样同一 NAT / 公司出口后面的多个用户各有独立额度、不会互相挤占，而单机靠多注册账号也无法线性放大配额。
      - **识别不出登录用户时**（未登录访问，或按令牌鉴权的 `/dashboard` 等接口），只按来源 IP 计，限额就是上面配置的值、不做放宽——此时它是该路径上唯一的总量闸门。
      - 本机（loopback）请求不计入。
    - `GLOBAL_RATE_LIMIT_WHITELIST`：免限流的来源地址，支持 IP 与 CIDR，多个用逗号分隔，默认为空。供管理脚本、监控探针等高频可信调用方使用。
      - 例子：`GLOBAL_RATE_LIMIT_WHITELIST=10.0.0.5,192.168.0.0/16`
      - 生效范围仅限上面两项总量限流。登录、注册、改密、OAuth 回调等敏感端点有独立的限流，**不受白名单豁免**，以保留口令爆破防护。
      - 白名单匹配的是系统判定出的客户端 IP，与限流分桶用的是同一个地址。该地址的可信度由下面的 `TRUSTED_PROXIES` 决定：来自不可信来源的 `X-Forwarded-For` 会被直接忽略，因此公网直连时无法靠伪造请求头取得豁免。
    - 被限流时返回 `429`，响应头带 `Retry-After`（秒）与 `X-RateLimit-Limit` / `X-RateLimit-Remaining` / `X-RateLimit-Reset`，响应体为 JSON 错误信息（含还需等待的秒数与 request id）。
14. `TRUSTED_PROXIES`：可信反向代理的地址列表，支持 IP 与 CIDR，多个用逗号分隔。
    - 默认值：`127.0.0.0/8,::1/128,10.0.0.0/8,172.16.0.0/12,192.168.0.0/16,fc00::/7`（loopback 与私网网段），足以覆盖 Docker Compose、K8s、宿主机 Nginx 等常见部署。
    - 作用：只有来自这些地址的请求，其 `X-Forwarded-For` / `X-Real-IP` 才会被采信用于判定客户端 IP；否则一律以连接的实际对端地址为准。
    - **为什么不设成信任所有代理**：`X-Forwarded-For` 完全由客户端提供。若无条件采信，伪造 `127.0.0.1` 即可豁免全部限流，不断更换伪造地址即可无限换桶，登录、注册等端点的口令爆破防护会随之失效。
    - 反向代理部署在**非私网地址**（如跨机房网关）时，需显式列出其地址；如需完全不信任任何代理，设为 `none`。
    - 注意：不能靠设成空字符串来关闭（`TRUSTED_PROXIES=""` 会被 viper 视为未设置而回落到默认值），必须用 `none`。
    - 例子：`TRUSTED_PROXIES=172.20.0.0/16`
    - 例子：`TRUSTED_PROXIES=none`
15. 编码器缓存设置：
    - `TIKTOKEN_CACHE_DIR`：默认程序启动时会联网下载一些通用的词元的编码，如：`gpt-3.5-turbo`，在一些网络环境不稳定，或者离线情况，可能会导致启动有问题，可以配置此目录缓存数据，可迁移到离线环境。
    - `DATA_GYM_CACHE_DIR`：目前该配置作用与 `TIKTOKEN_CACHE_DIR` 一致，但是优先级没有它高。
16. `RELAY_TIMEOUT`：中继超时设置，单位为秒，默认不设置超时时间。
17. `SQLITE_BUSY_TIMEOUT`：SQLite 锁等待超时设置，单位为毫秒，默认 `3000`。
18. `TG_BOT_API_KEY`： 你的 Telegram bot 的 API 密钥。你可以在 [BotFather](https://t.me/BotFather) 获取这个密钥。
19. `TG_WEBHOOK_SECRET`：（可选）你的 webhook 密钥。你可以自定义这个密钥。如果设置了这个密钥，将使用`webhook`的方式接收消息，否则使用轮询（Polling）的方式。
20. `USER_TOKEN_SECRET` ： 设置用户令牌签名密钥，必填，大于 32 位以上， 设置后请勿修改，否则会导致用户令牌失效。
21. `HASHIDS_SALT` ：Sqids 字母表，用于混淆用户令牌信息， 可空，如为空则使用默认字母表`abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789`，如设置，则需要保证字母表中无重复字符。
22. `AUTO_PRICE_UPDATES`：自动更新价格，可选值为 `true` 和 `false`，未设置则默认为 `false`。开启后每次启动程序时，会检测数据库中的数据和程序中默认模型价格，如果数据库中的模型价格有缺失将会自动同步到数据库中。 开启带来的问题：你删不掉程序默认的模型价格，删除后，重启又回来了，这个选项适合跟官网一致价格的用户使用。
23. `AUTO_PRICE_UPDATES_MODE`：价格更新模式，可选值为 `add`:仅增加系统不存在的价格   `overwrite`：覆盖系统所有价格配置  `update`：仅仅更新现有数据   `system`:使用程序内置价格表配置初始化价格配置，默认为 `system`。建议生成环境使用`system`模式，手动去web的价格管理模块手动获取价格更新服务器数据并一一核对更新。
24. `AUTO_PRICE_UPDATES_INTERVAL` ：价格自动更新时间，单位分钟，仅`AUTO_PRICE_UPDATES_MODE`为`add`、`overwrite`时生效，系统将按照此时间周期性从价格更新服务器获取价格配置并更新系统价格。默认值：1440
25. `UPDATE_PRICE_SERVICE` ：设置之后将使用指定的价格服务更新价格。不设置则使用系统默认价格服务`https://raw.githubusercontent.com/MartialBE/one-api/prices/prices.json`
26. `USER_INVOICE_MONTH` ：是否开启用户月度账单功能，开启后系统每月1日凌晨生成用户上月数据汇总账单，数据量大的情况比较消耗资源，谨慎开启，默认`false`

