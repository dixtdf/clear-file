# clear-file（文件清理器）

一个跑在 NAS / 服务器 / 下载机 / Docker 主机上的 Web 文件清理与磁盘分析工具。

- 后端：Go（标准库，零第三方依赖），单文件二进制，内嵌前端
- 前端：Vue 3 + Vite + vue-router
- 运行：Docker 或本机；宿主机目录按需挂载

```
浏览器 (Vue 3)
      │  REST + SSE
      ▼
Go 服务 ── 可访问根目录（默认容器内 `/`，可配置）
```

## 快速开始

### Docker Compose（推荐）

用 GitHub 上发布的镜像，不本地构建：

```bash
docker compose up -d            # 拉 ghcr.io/dixtdf/clear-file:latest 并启动
# 打开 http://<host>:6888
docker compose down             # 停止
```

固定版本或换成自己的仓库：

```bash
# bash / zsh
FILE_CLEANER_IMAGE=ghcr.io/dixtdf/clear-file:0.1.0 docker compose up -d
```

```powershell
# Windows PowerShell
$env:FILE_CLEANER_IMAGE="ghcr.io/dixtdf/clear-file:0.1.0"; docker compose up -d
```

GHCR 的包默认是私有的，第一次拉之前先 `docker login ghcr.io -u <github-user> -p <PAT>`（或在 GitHub 的 Packages 设置里改成 public）。

`docker-compose.yml` 默认不挂载宿主机目录。要管理宿主机文件，在服务下按需添加一个或多个挂载，例如：

```yaml
volumes:
  - /srv/media:/media
  - /home/user/downloads:/downloads
```

浏览器中可选择 `/media`、`/downloads` 或其他容器内可访问的目录。未配置挂载时只能看到容器自身的文件系统。也可设置 `FILE_CLEANER_ROOT=/media`，将浏览、扫描和删除限制在该目录下。

### 本地开发

```bash
# 1) 后端（默认 :6888，根目录为当前系统的文件系统根）
go run ./cmd/server -addr :6888 -verbose

# Windows 上想试跑后端可以指向任意测试目录
go run ./cmd/server -root D:\test -addr :6888

# 2) 前端（Vite 开发服务器 :5173，/api 自动代理到 :6888）
cd web
npm install
npm run dev
```

生产构建：

```bash
cd web && npm run build          # 产物写入 web/dist，会被 go:embed 打进二进制
go build -o file-cleaner ./cmd/server
```

## 配置

| 环境变量 | 默认值 | 说明 |
|---|---|---|
| `FILE_CLEANER_ADDR` | `:6888` | HTTP 监听地址 |
| `FILE_CLEANER_ROOT` | `/`（Linux/Docker） | 允许访问的根目录，可指定任意已存在目录 |
| `FILE_CLEANER_TRASH` | `false` | `true` 时删除改为移动到根目录下的 `.file-cleaner-trash/` |
| `FILE_CLEANER_MAX_RESULTS` | `500000` | 单次扫描在内存中保留的最大结果条数 |

命令行参数 `-addr` / `-root` / `-verbose` 可覆盖环境变量。

## 版本与发布

版本号只有一个来源：根目录的 `VERSION` 文件（当前 `0.1.0`）。

本地改版本号（会同步 `VERSION`、`web/package.json`、`web/package-lock.json`）：

```powershell
.\set-version.ps1 0.1.1              # 直接指定
.\set-version.ps1 -Bump patch        # 自增 patch / minor / major
.\set-version.ps1 0.1.1 -Commit -Tag # 顺手提交并打本地 tag
.\set-version.ps1 0.1.1 -DryRun      # 只看改动不落盘
```

发版走 GitHub Actions（手动触发，`.github/workflows/release.yml`）：

1. 到仓库 Actions → Release → Run workflow。
2. `version` 填 `0.1.0`（不带 `v`），`branch` 默认 `main`，可勾选是否同时更新 `latest` 镜像标签。
3. workflow 会先校验版本号→ 用 `main` 建并推送 `vX.Y.Z` tag → 构建 `linux/amd64` + `linux/arm64` 镜像推到 `ghcr.io/dixtdf/clear-file`（标签 `X.Y.Z`、`X.Y`、可选 `latest`）→ 交叉编译 6 个平台二进制，打包后连同 `checksums.txt` 上传到对应 GitHub Release。

镜像里默认端口是 `6888`，二进制里的版本号由 `-ldflags` 注入（`/api/v1/system/info` 能看到）：

```bash
docker run -d --name file-cleaner -p 6888:6888 -v /srv/media:/media ghcr.io/dixtdf/clear-file:0.1.0
```

tag 由 release workflow 创建，本地不要重复推同名 tag；发版用的分支默认 `main`，若仓库默认分支是 `master`，触发时把 `branch` 输入改成 `master`。

## 功能（第一版 MVP）

- 文件管理：目录浏览、面包屑、自然排序、名称/扩展名/大小/时间筛选、多选、全选筛选结果、反选、批量删除
- 文件清理：空文件、空目录（含递归向上清理）、小文件（预设或自定义阈值）扫描 + 批量删除
- 重复文件：仅大小 / 快速校验（≤3 MiB 文件全量读取，其他文件读取头+中+尾各 1 MiB）/ 完整校验（整文件 Hash）三种模式，分组展示，可"每组保留第一个"或"优先保留指定目录"
- 空间分析：目录大小扫描、目录树、WinDirStat 风格 Treemap（可点击下钻）、最大文件 Top 200
- 扫描任务：全部后台任务化（等待/扫描中/完成/取消/失败），SSE 实时进度，随时取消

## 关键设计

1. 浏览不递归：打开目录只 `lstat` 一层，绝不统计子目录大小。
2. 扫描与删除分离：扫描只产出结果，删除必须由用户勾选并在二次确认后执行。
3. 流式遍历：目录遍历用 `filepath.WalkDir` 逐条回调，不把整棵树读进内存。重复文件扫描先用**两遍流式扫描**（第一遍只统计每种大小出现次数，第二遍只保留大小重复的文件），百万级文件也不会 OOM。
4. 不跟随符号链接：`/a/b -> /a` 之类的环不会造成无限扫描，链接按普通项展示。
5. 路径范围：所有 path 参数经 `filepath.Clean` + `filepath.Abs` + `filepath.Rel` 校验，并用 `EvalSymlinks` 二次确认最终路径仍在配置的根目录内。默认根目录为 `/`，若要限制范围请设置 `FILE_CLEANER_ROOT`。
6. 删除安全：服务端二次校验——根目录不可删、扫描根目录不可删、`..` 不可作为目标。
7. 后台任务队列：Go 内 `context.WithCancel` 任务，HTTP 请求从不被扫描阻塞。
8. 大批量删除：前端把删除请求按 500 条一批切分，确认弹窗最多渲染 20 条路径；选择超过 2000 项时不再向服务器要预览（改用本地估算），避免一次几万条路径把请求体和 DOM 撑爆。服务端请求体上限 64 MiB，预览接口回显的路径也截断到 200 条。"全选结果"上限 20 万条（服务端单次扫描结果本身受 `FILE_CLEANER_MAX_RESULTS` 限制，默认 50 万）。
9. 删除后结果同步：删除请求会带上该次扫描的 `taskId`，服务端把已删路径从扫描结果里剔除并重算统计（清理列表、重复文件组、空间树/TOP 文件都同步），所以删除后刷新看到的就是磁盘上的真实状态，不需要重扫。
10. 重复文件批量操作不分页局限："每组保留第一个 / 保留该目录"会自己逐页遍历全部重复组（每页 200 组）后生效，不是只作用于当前显示页；单个组内文件超过 200 个时列表自动折叠（"展开全部"可展开），避免一个组几千个文件把页面渲染卡死。选中数量/释放空间用增量计数器维护，不会因为勾选几万项而每次重算。
11. 扫描进度口径：重复扫描的两轮目录遍历只累计一次文件数、目录数和文件总大小；候选文件与最终重复组分别计数。文件总大小来自目录元数据，内容读取量来自校验时读入的字节，平均读取速度为内容读取量除以任务总耗时，并非物理磁盘吞吐。

## API 一览

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/api/v1/system/info` | 版本号 + 根目录 + 磁盘用量（statfs） |
| GET | `/api/v1/files` | 目录列表（`path/page/pageSize/sort/order/name/ext/minSize/maxSize/mtimeFrom/mtimeTo`） |
| POST | `/api/v1/files/delete/preview` | 删除前统计（数量、总大小、被跳过项；回显路径截断到 200 条） |
| POST | `/api/v1/files/delete` | 执行删除：成功/失败清单与释放空间（前端按 500 条一批调用，可带 `taskId` 同步剔除扫描结果） |
| POST | `/api/v1/cleanup/scan` | 启动空文件/空目录/小文件扫描，返回 `taskId` |
| POST | `/api/v1/duplicate/scan` | 启动重复文件扫描，返回 `taskId` |
| GET | `/api/v1/duplicate/{id}/groups` | 分页获取重复组 |
| POST | `/api/v1/disk/scan` | 启动目录大小扫描 |
| GET | `/api/v1/disk/{id}/tree` | 目录树 + Top 文件 |
| GET | `/api/v1/disk/{id}/treemap` | Treemap 单元（按目录下钻） |
| GET | `/api/v1/tasks` | 任务列表 |
| GET | `/api/v1/tasks/{id}` | 任务详情 |
| GET | `/api/v1/tasks/{id}/results` | 扫描结果分页（清理类） |
| GET | `/api/v1/tasks/{id}/events` | SSE：`snapshot` / `progress` / `failed` / `done` |
| POST | `/api/v1/tasks/{id}/cancel` | 取消扫描 |
| DELETE | `/api/v1/tasks/{id}` | 移除已完成任务记录 |

## 项目结构

```
clear-file/
├── cmd/server/main.go             # 入口：HTTP 服务 + 优雅退出
├── internal/
│   ├── api/                       # REST + SSE 路由与处理
│   ├── cleanup/                   # 空文件 / 空目录 / 小文件扫描
│   ├── config/                    # 环境变量配置
│   ├── disk/                      # 目录大小扫描、Treemap 数据、statfs
│   ├── duplicate/                 # 三阶段重复文件扫描 + 探针/完整 Hash
│   ├── filesystem/                # 目录浏览、自然排序、路径安全、删除
│   └── task/                      # 后台任务管理器（进度 / 取消 / 事件订阅）
├── web/                           # Vue 3 前端（构建产物被 go:embed 内嵌）
│   └── src/{views,components,composables,utils,api}
├── .github/workflows/             # release.yml（手动发版：tag + ghcr 镜像 + Release 二进制）
├── set-version.ps1                # 一键改版本号（VERSION + web/package*.json）
├── VERSION                        # 版本号唯一来源
├── Dockerfile                     # 三段式：node 构建前端 → go 编译 → alpine 运行（CI 发版构建镜像用）
└── docker-compose.yml
```

## 说明与取舍

- Hash 算法：当前用标准库 `sha256`（零依赖，约 1-2 GB/s）。需求推荐的 BLAKE3 只需替换一个函数——`internal/duplicate/hasher.go` 里的 `newHasher()` 改成 `blake3.New()`（`go get github.com/zeebo/blake3`）即可，其余代码不变。
- 自然排序：数字段按数值比较（`1 < 01 < 001 < 2 < 10`），非数字段按 Unicode 码点比较，因此中文不会被打散，但不做拼音排序。
- 扫描结果保存在内存并分页返回，上限由 `FILE_CLEANER_MAX_RESULTS` 控制；超出会标记 `truncated`。
- Treemap 只对扫描时保留的层级（默认深度 8、节点上限 40 万）绘图，更深的文件聚合进父目录大小。

## 后续可扩展（不在第一版）

用户系统、权限系统、文件预览、在线播放、上传/下载/复制/移动、回收站模式（已预留 `FILE_CLEANER_TRASH`）、增量索引、定时扫描。
