# 生产部署

这个目录是一套可复制的部署单元：网页、Go API、机台数据服务打包成镜像，`docker compose up` 一键起栈。宿主机只需要 Docker 与 Compose v2，不需要装 Node 或 Go。

## 首次启动

```
cd deploy
cp .env.example .env                    # compose 层面的变量：对外端口、时区
cp config.example.toml config.toml      # 服务端运行参数：监听、机台地址、日志
docker compose up -d --build
```

浏览器打开 `http://<主机>:8088`（端口取 `.env` 的 `WEB_PORT`）。`docker compose ps` 三个服务都应是 healthy。

机台数据不入库，mock 挂载的是宿主机仓库根目录的 `machine-data/`：部署机上先执行一次 `make sync`，或把 `config.toml` 的 `[mock] url` 指向真实机台网关并停掉 mock。没有数据时页面能打开，但机台列表是空的。

## 日常更新

服务端拉代码后仍在 `deploy/` 里执行：

```
git pull
docker compose up -d --build
```

Compose 会重建有变化的镜像并滚动替换容器；数据卷 `api-logs` 保留。

## 组成

| 服务 | 镜像来源 | 暴露 | 说明 |
| --- | --- | --- | --- |
| web | `apps/web/Dockerfile` | 宿主 `${WEB_PORT}` | nginx 提供静态页，并把 `/api/` 反代到 api |
| api | `apps/server/Dockerfile` | 仅容器网络 8080 | 只读配方接口，健康检查 `/healthz` |
| mock | `apps/mockserver/Dockerfile` | 仅容器网络 3101 | 读宿主机 `machine-data/` 的模拟机台；接真实网关后可停用 |

只有 web 映射宿主端口，API 与机台服务留在 compose 网络内，从外部扫不到。

## 配置

服务端的运行参数放在 `deploy/config.toml`，compose 以只读方式挂到容器的 `/app/config.toml`，容器启动时用 `RECIPE_CONFIG` 指定这个位置。挂载内容变化不会触发容器重建，所以改完配置要重启进程：`docker compose restart api`，或回到仓库根目录用 `make deploy-restart`。镜像里不打包任何配置文件，仓库里只有 `deploy/config.example.toml` 这份模板，`deploy/config.toml` 属于部署机，不入库。

```toml
[server]
host = "0.0.0.0"     # 容器里必须是 0.0.0.0，否则 nginx 反代不进来
port = 8080          # 固定 8080，健康检查与 nginx 都指向它
timeout = "10s"      # 单次机台请求超时

[mock]
url = "http://mock:3101"   # 接真实网关后改成真实地址，并停掉 mock 服务

[database]
url = ""                   # 读取链路暂不需要数据库

[log]
level = "info"             # 排查时可临时改 debug
format = "json"            # json 便于采集
dest = "both"              # 同时进 docker logs 与卷内文件
dir = "/app/logs"
rotate_mb = 10
keep = 5
```

`.env` 只保留 compose 自己的变量：`WEB_PORT`（对外端口，默认 8088）与 `TZ`。环境变量的优先级仍高于 TOML，需要临时覆盖某一项时在 `.env` 里写 `LOG_LEVEL=debug` 即可，不必改文件。

## 日志

服务端日志默认 `LOG_DEST=both`：既进 `docker compose logs`，也写进卷 `api-logs` 下的 `/app/logs/api/api.log`，超过 `LOG_ROTATE_MB` 自动归档到 `archive/`，只留最近 `LOG_KEEP` 份。

```
docker compose logs -f api                          # 跟随容器标准输出
docker compose exec api tail -n 100 -F /app/logs/api/api.log   # 跟随文件日志
docker compose exec api sh -c 'ls -lt /app/logs/api/archive'   # 看归档
```

排查生产问题时把 `config.toml` 的 `[log] level` 改成 `debug` 并 `docker compose restart api`，复现后改回 `info` 再重启；或在 `.env` 里写 `LOG_LEVEL=debug` 后 `docker compose up -d`，环境变量会压过文件。

## 验证与排障

```
docker compose config                 # 校验变量替换后的最终配置
docker compose ps                     # healthy 才说明健康检查通过
curl -s http://127.0.0.1:8088/api/machines
curl -s http://127.0.0.1:8088/healthz
```

- 8088 打不开：先看 `docker compose ps`，web 不 healthy 时 `docker compose logs web` 通常是 nginx 反代不上游。
- api 起不来且日志提示 `cannot read /app/config.toml`：缺少 `deploy/config.toml`，按上面的步骤从模板复制。
- 页面能开但列表报“机台服务暂时不可用”：api 到 mock 的网络或 `config.toml` 里的 `[mock] url` 写错，用 `docker compose exec api wget -qO- http://mock:3101/api/machines` 验证。
- 机台列表为空：`machine-data/` 没有数据，部署机上 `make sync` 后 `docker compose restart mock`；也可以直接 `docker compose exec mock ls /data` 确认挂载。
- 日志目录权限错误：容器以 uid 10001 运行，卷是新建的命名卷即为正常；若改成宿主目录绑定，需保证该目录可写。

## 停栈与清理

```
docker compose down            # 停容器，保留日志卷
docker compose down -v         # 连日志卷一起删，日志会丢
```
