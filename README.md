# Recipe Body

把机台原始配方文件转换成工程师能直接阅读的 Recipe Body 页面。首期只做读取链路：机台原始字节 → 解析 → 浏览，不写回机台、不生成机台文件。

## 目录

- `apps/mockserver`：Node 机台模拟服务，直接读 `machine-data/`，推导机台列表并提供配方目录与原始文件；测试用的合成样本在 `apps/mockserver/testdata/`。
- `apps/server`：Go API，按 config → machine → service → httpapi 分层，输出 JSON 与原始字节下载。
- `apps/web`：React + Vite 工作台，表格浏览测试项、右侧详情、来源信息与原始文件下载。
- `packages/recipe`：PGS/DCEX 解析包，保留原始行号、字段和顺序。
- `packages/contracts`：前后端共享的 TypeScript 数据类型。
- `packages/ui`：shadcn/ui 风格的共享基础组件。
- `packages/logging`：Go 结构化日志，负责等级、格式、输出目标与按大小归档。
- `machine-data/`：云盘同步下来的机台原始数据，只存本地，除 README 外全部被忽略；机台模拟服务默认读这里。
- `scripts/sync-machine-data.mjs`：从操作系统已连接的 SMB 公盘增量同步机台数据。
- `deploy/`：生产部署单元，Docker Compose 一键起 web + api + 机台服务。

## 本地运行

需要 Node.js 22+ 与 Go 1.24+。

```bash
npm install
npm run dev
```

启动后：Web `http://127.0.0.1:5173`，Go API `http://127.0.0.1:8080`，机台模拟 `http://127.0.0.1:3101`。Vite 将 `/api` 代理到 Go API。机台服务读 `machine-data/`，首次运行前先 `make sync`，或用 `MOCK_DATA_DIR` 指向任意数据目录；目录为空时机台列表就是空的。

常用命令都收在 Makefile 里，`make help` 可列出全部；`make check` 是提交前的格式、静态检查与测试总关口。`make smoke` 依赖本地已启动的服务。

单独启动：

```bash
npm run dev:mock      # 机台模拟服务
npm run dev:server    # Go API（首次需 go install github.com/air-verse/air@latest）
npm run dev:web       # 前端
```

## 生产部署

部署机上只需要 Docker 与 Compose v2。拉代码后进 `deploy/`：

```bash
cd deploy
cp .env.example .env                    # 对外端口、时区
cp config.example.toml config.toml      # 服务端运行参数
docker compose up -d --build
```

默认对外只有 web 的 8088 端口，nginx 把 `/api/` 反代到容器内的 Go API；API 与机台服务不映射宿主，服务端参数来自挂载进容器的 `deploy/config.toml`，机台服务的数据来自挂载的 `machine-data/`（部署机上先 `make sync`，或把 `[mock] url` 指向真实网关）。服务端日志同时进 `docker compose logs` 与卷 `api-logs` 下的文件。更新时 `git pull && docker compose up -d --build` 即可，细节见 `deploy/README.md`。根目录也可用 `make deploy`、`make deploy-ps`、`make deploy-logs`、`make deploy-down`。

## 机台数据同步

机台数据不入库。生产公盘为 SMB 共享 `\\10.128.0.21\d`。同步脚本读取操作系统已连接的共享目录，认证由操作系统处理，不保存账号密码，也不再使用 `FTP_*` 配置。

Windows 先在资源管理器打开 `\\10.128.0.21\d`，如提示登录，请向公盘管理员获取账号、密码及可能的域信息。脚本默认读取该 UNC 路径；如果配方位于子目录，设置 `SMB_SOURCE_DIR` 指向该子目录。运行同步任务的用户必须具备相应共享和文件读取权限；资源管理器当前用户的登录状态不一定适用于计划任务或服务账号。Windows 可在 PowerShell 中设置 `$env:SMB_SOURCE_DIR='\\10.128.0.21\d\实际机台目录'`，再运行 `node scripts/sync-machine-data.mjs --dry-run`；无需安装 make。

Linux/macOS 先通过操作系统挂载共享盘，再将仓库根目录 `.env` 中的 `SMB_SOURCE_DIR` 设置为挂载后的机台目录。Linux 例如 `/mnt/recipe-share`，macOS 例如 `/Volumes/d`。源目录的直接子目录应为机台编号；`d` 是共享名，实际机台文件所在子目录仍需确认。部署机同步还需要 Node.js 22+；Docker Compose 本身不会连接 SMB，也不会定时同步。

```bash
make sync         # 复制到 machine-data/，未变化的文件按大小与修改时间跳过
make sync-dry     # 只列出将要复制的文件
make sync-prune   # 额外清理公盘上已不存在的本地文件
```

同步保留源文件修改时间、拒绝越出数据目录的路径、忽略符号链接和系统垃圾文件，并通过临时文件加改名写入配方。无法访问共享目录时直接失败，空源目录拒绝执行清理，避免挂载失效时清空本地缓存。共享目录与本地目标不能相同或互相包含。同步期间请避免其他进程同时修改源目录或目标目录；不要并发运行多个同步任务。

Linux 可安装系统的 CIFS 挂载工具，使用 `//10.128.0.21/d` 作为挂载源，以只读方式挂载到 `/mnt/recipe-share`。账号密码未知时请先确认是否允许访客访问；需要认证则向管理员申请只读账号，使用系统凭据文件或凭据管理器保存，凭据文件仅允许所属用户读取，不要将密码写进仓库、命令行或日志。macOS 可在 Finder 的“连接服务器”输入 `smb://10.128.0.21/d`，使用系统登录和钥匙串完成连接。定时任务运行前应检查 SMB 挂载仍存在且可读。

同步日志复用 Node 日志入口，遵循 `LOG_LEVEL`、`LOG_FORMAT`，输出到 stdout，记录 `sync_started`、`sync_file_planned`、`sync_file_copied`、`sync_completed`、`sync_failed` 等事件及文件数、字节数、耗时，不记录配方内容或认证凭据。单文件复制记录为 debug，完成摘要为 info，失败为 error；该脚本不自行管理日志文件或轮转，生产由调度器或日志采集服务保存输出。

## 配置

服务端配置放在 TOML 里，`apps/server/config.example.toml` 是完整示例：

```bash
cp apps/server/config.example.toml apps/server/config.toml
```

默认读取 `apps/server/config.toml`（已忽略，不入库）；也可用 `RECIPE_CONFIG` 指向其他文件，此时文件必须存在且能解析，避免拼错路径后静默回落到默认值。优先级为环境变量 > TOML 文件 > 内置默认值，因此可以在 `.env` 里覆盖单项而不必改文件。

| 配置项 | 环境变量 | 说明 |
| --- | --- | --- |
| `server.host` / `server.port` | `RECIPE_HOST` / `RECIPE_PORT` | 监听地址，默认 `127.0.0.1:8080` |
| `server.timeout` | `RECIPE_TIMEOUT` | 机台服务超时，默认 `5s`，范围 `(0, 1m]` |
| `mock.url` | `RECIPE_MOCK_URL` | 机台数据服务基地址 |
| `database.url` | `RECIPE_DATABASE_URL` | 留空表示不连接数据库 |
| `log.*` | `LOG_*` | 见下节日志 |

## 日志

日志默认写在 TOML 的 `[log]` 段，也可全部用环境变量覆盖，本地与生产使用同一份代码：

| 变量 | 取值 | 默认 | 说明 |
| --- | --- | --- | --- |
| `LOG_LEVEL` | `debug` / `info` / `warn` / `error` | `info` | 低于该等级的记录直接丢弃 |
| `LOG_FORMAT` | `text` / `json` | `text` | `json` 便于 `jq` 和日志系统采集 |
| `LOG_DEST` | `stdout` / `file` / `both` | `both` | 容器内通常用 `stdout` |
| `LOG_DIR` | 目录 | `logs` | 生产可指向 `/var/log/recipe` |
| `LOG_ROTATE_MB` | 1–1024 | `10` | 单个服务日志超过后归档 |
| `LOG_KEEP` | 1–100 | `5` | 归档目录保留份数 |

上表变量与 `apps/server/config.toml` 的 `[log]` 段一一对应（`rotate_mb`、`keep`），二选一填写即可，同时存在时环境变量生效。

`make dev` 会用 `LOG_LEVEL=debug`、`LOG_DEST=file` 覆盖文件的 `[log]` 段：服务端日志只写 `logs/api/api.log`，控制台只保留 mock、Vite 与 Air 的启动与崩溃信息，不再被逐条请求刷屏。日志查看用 `make logs`，需要恢复终端输出时用 `LOG_DEST=both make dev`。超过 10 MiB 会自动归档到 `logs/api/archive/`，`logs/` 已在 `.gitignore` 中，不会入库。

查询用 `make logs` 跟随服务端日志（默认 `logs/api/api.log`，先回看最后 80 行再持续追加，日志按大小轮转时会自动续到新文件），`svc=` 可切换服务目录（目前只有 `api`，新增服务时复用同一约定）；`make logs-list` 查看全部文件，`make logs-grep q=recipe_parse_failed` 在日志目录里检索；`make logs-clean` 清空本地日志。定位时先看 `level=ERROR` 与 `level=WARN`，再用 `path`、`machine`、`recipe`、`duration_ms` 关联同一次请求。

## 接口

- `GET /healthz`
- `GET /api/machines`
- `GET /api/machines/{machine}/recipes`
- `GET /api/machines/{machine}/recipes/{file}`：解析结果，含 `metadata`、`tests`、`fields`、`sha256`、`warnings`
- `GET /api/machines/{machine}/recipes/{file}/source`：原始字节下载

服务只暴露 GET，由 Casbin 策略显式允许；未配置数据库也能运行。

## 验证

```bash
npm test              # 机台模拟 + Go + 前端
npm run test:mock
npm run test:go
npm run test:web
npm run typecheck
```

`packages/recipe` 覆盖 96.6%，`packages/logging` 覆盖 91.0%，`apps/web` 的 `App.tsx` 与 `api.ts` 覆盖 100%（语句、分支、函数）；服务端按包统计为 `apps/server` 81.2%（进程装配与 `Serve`/`run`）、`internal/config` 95.3%、`internal/httpapi` 93.5%、`internal/machine` 100%、`internal/service` 100%、`internal/apperr` 100%。

## 服务端分层

`apps/server` 只做装配，其余按依赖方向从外到内排开，新增能力时按“放在哪一层”判断即可。

`apps/server/main.go` 负责读取配置、初始化日志与数据库连接池、按顺序拼装各层、启动监听并在信号到达时退出；进程级测试（`Serve` 的优雅关闭、`run` 的失败分支）也留在这里。

`apps/server/internal/config` 读取 TOML 与环境变量并做校验，任何层都不直接读环境变量。

`apps/server/internal/machine` 是对机台服务的适配器，负责拼路径、超时、目录解码与字段校验、10 MiB 上限、404 与 5xx 的语义转换，并把每次读取的字节数和耗时写进 debug 日志。要换真实网关或加缓存，只改这一层。

`apps/server/internal/service` 写用例顺序：取字节、调用 `packages/recipe` 解析、把失败转成 `apperr.Parse`。它依赖 `MachineReader` 接口而不是具体客户端，测试可以替换成桩。

`apps/server/internal/apperr` 定义 `NotFound`、`Upstream`、`Parse`、`Invalid` 四类业务错误，各自带面向用户的说明与底层原因；状态码只在 HTTP 层决定，换成 gRPC 或 CLI 时不用复制错误语义。

`apps/server/internal/httpapi` 是唯一的 HTTP 边界：Gin 路由、Casbin 只读策略、下载头、请求日志中间件，以及 `Kind` 到状态码的映射。处理器只做参数转交，不写业务判断。

## 已知边界

`.dcex` 的单位、Range、Bias 为厂商编码，页面保留原始值并提示未确认；`.pgs` 的启用与判定标志同样按原值展示。控制类函数（`*_CTRL`、`VIRTUALFUNC`、`RELAY_INIT`）与测量项分开统计，默认不混入测试规格列表。
