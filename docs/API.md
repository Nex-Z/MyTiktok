# HTTP API v1

同源前缀 `/api`，无需登录。JSON 请求必须使用 `Content-Type: application/json`。写接口只修改数据库或应用自有缓存，不提供源文件写入操作。

失败结构：`{"error":{"code":"auth_required","message":"115 登录已失效，请重新连接"}}`。主要状态码：400 参数错误、401 115 需重连、403 跨站请求、404 不可用、409 扫描冲突、410 Feed 会话过期、429 限流、502 上游错误、504 超时。错误消息不包含 Cookie、云端原始响应或临时播放地址。

## 来源与目录

| 方法和路径 | 请求 | 响应 |
|---|---|---|
| `GET /health` | 无 | `{status:"ok",version:"0.1.0"}` |
| `GET /sources` | 无 | `Source[]` |
| `POST /sources` | `{name,kind,root,intervalHours?}` | 新建 `Source`，自动启动首次扫描 |
| `PATCH /sources/{id}` | `{name,intervalHours}` | `{ok:true}` |
| `DELETE /sources/{id}` | 无 | `{ok:true}`；正在扫描时返回 409 |
| `GET /directories` | `kind=local\|p115&key=…&cursor=…` | `{entries:[{key,name,dir,size,modified}],next,root}` |

`Source`：`{id,name,kind,root,intervalHours,lastScan}`。`kind` 为 `local` 或 `p115`。本地 `root` 必须是 `MEDIA_ROOT` 内现存目录；115 为数字目录 ID。周期默认 6 小时，0 关闭，最大 8760 小时。`lastScan` 是最近任务启动时间，Unix 秒。

本地目录浏览的 `key` 是相对 `MEDIA_ROOT` 的路径，首次留空；115 首次为 `0`。响应 `root` 是可直接用于创建来源的根路径或目录 ID。目录页可能没有子目录但仍有 `next`（这一页只有文件），客户端需继续翻页。每页最多遍历 256 个条目。

同一种来源的同一根目录不能重复添加；不同根目录若重叠，视频会按不同来源独立入库。源内唯一键为 `(source_id,file_key)`，本地 file key 为相对路径，因此重命名或移动后视为新文件；原索引完整扫描后标记不可用。115 使用稳定 fileId。

## 115 连接

| 方法和路径 | 请求 | 响应 |
|---|---|---|
| `GET /p115/status` | 无 | `{connected,state}`；state 为 connected / disconnected / auth_required / error |
| `PUT /p115/cookie` | `{cookie:"UID=…; CID=…; SEID=…"}` | `{connected:true}`，连接验证成功后才保存 |
| `POST /p115/qr` | 无 | `{id,image}`，image 为 PNG data URL |
| `GET /p115/qr/{id}` | 无 | `{state}` |

扫码状态：waiting、scanned、success、expired、canceled；上游失败返回结构化错误。客户端每 2 秒查询一次，success / expired / canceled 后停止。会话服务端保存 3 分钟，最多同时保留 16 个未过期会话。成功时凭证由服务端存储，响应不包含 Cookie。

## 扫描任务

| 方法和路径 | 请求 | 响应 |
|---|---|---|
| `POST /sources/{id}/scan` | 无 | `{id:jobId}`；同源已有任务则 409 |
| `GET /jobs` | 无 | 最近 100 个 `ScanJob`，新到旧 |
| `POST /jobs/{id}/cancel` | 无 | `{ok:true}`，异步取消，轮询任务确认结束 |

`ScanJob`：`{id,sourceId,status,scanned,errors,message,startedAt,finishedAt}`。状态为 running / cooling / completed / failed / canceled / interrupted。`scanned` 为已发现的视频数，`errors` 为元数据提取失败数；提取失败不使整个扫描失败。任务时间为 Unix 秒。取消和网络错误均不执行缺失文件清理。列目录被限流后任务冷却 10 分钟再重试，直到恢复或用户取消。

## Feed 和媒体

`GET /feed?mode=feed&kind=local&sort=random&limit=20`

- `mode`：feed（默认）、favorites、history、hidden。
- `kind`：空为全部，或 local / p115。
- `q`：文件名包含搜索，忽略 ASCII 大小写；`%`、`_` 按普通字符处理，最多 300 字节。
- `sourceId`：限定一个媒体源。
- `duration`：空 / short（0–60 秒，不含未知）/ medium（>60–300 秒）/ long（>300 秒）/ unknown。
- `orientation`：空 / portrait / landscape / square / unknown。
- `folder`：存在时限定媒体源内相对目录，空字符串表示根目录；`recursive=1` 包括子目录，默认仅当前目录。和 `sourceId` 一起使用。
- `sort`：`recent` 为入库倒序（历史模式为最近观看），`title` 按名称升序，`duration` 按时长倒序。普通 Feed 默认或 `random` 为随机顺序。
- `limit`：1–50，默认 20。
- 响应：`{items:Video[],next:string,total:number}`。
- 后续请求使用 `GET /feed?cursor=<next>&limit=20`。cursor 已固定首次筛选和排序，不需要重传。
- 序列保存 2 小时，服务端最多保留 32 个序列，满时淘汰最旧序列。过期返回 410，重新请求无 cursor 的 Feed 即可。
- `total` 是创建序列时的快照数量。后来被删除、隐藏或失效的项会跳过，所以可能与最终返回数不同；新入库项不会进入旧序列。

`Video` 示例：

```json
{
  "id": 1, "sourceId": 1, "kind": "local", "title": "clip.mp4",
  "size": 123456, "duration": 8.2, "width": 1080, "height": 1920,
  "cover": "/api/videos/1/cover", "indexedAt": 1788790000,
  "available": true, "hidden": false, "favorite": true, "position": 3.5
}
```

新增字段：`sourceName`（媒体源名称）、`folder`（媒体源内相对目录）、`fileKey`（本地相对路径或 115 fileId）、`modifiedAt`（Unix 秒，统一两个来源的时间单位）。

`GET /library/folders?sourceId=1&parent=旅行` 返回当前目录下已入库的子目录：`[{name:"第二天",path:"旅行/第二天",count:12}]`。计数包含后代目录的视频，排除隐藏和不可用视频。查询只读数据库，不访问 115；空目录不会出现在索引导航中。

未知时长和尺寸为 0，缺失封面为空字符串。`position` 单位为秒；`indexedAt` 为 Unix 秒。

| 方法和路径 | 请求 | 响应 |
|---|---|---|
| `GET /videos/{id}` | 无 | `Video` |
| `GET /videos/{id}/cover` | 无 | 图片；不存在则 404 |
| `PUT /videos/{id}/favorite` | `{value:true\|false}` | `{ok:true}` |
| `PUT /videos/{id}/hidden` | `{value:true\|false}` | `{ok:true}` |
| `PUT /videos/{id}/history` | `{position:3.5}` | `{ok:true}`，合并同一视频历史，更新时间 |

收藏与历史通过 `/feed?mode=favorites` 和 `/feed?mode=history` 读取。隐藏视频通过 `mode=hidden` 读取，恢复发送 `value:false`。进度必须为非负有限数字，已知时长时自动限制到视频时长内。

## 播放

`GET` / `HEAD /videos/{id}/play`：

- 本地：直接返回文件流，支持 Range / If-Range、206、416、Content-Length。
- 115：以浏览器 User-Agent 获取临时地址并 302 跳转；`refresh=1` 绕过临时地址缓存；`proxy=1` 经 NAS 转发流。
- 代理只接收媒体 ID，保留上游 Content-Range / Accept-Ranges / Content-Length / Content-Type / ETag / Last-Modified，不返回上游 Set-Cookie。
- 代理收到 401/403 后刷新一次地址；再次失败返回错误，不无限重试。
- 前端顺序：普通直连 → refresh=1 → proxy=1。浏览器明确报告解码错误时直接提示跳过。
- 临时链接只在服务端内存短时缓存，不写入视频数据库，不在错误响应中出现。302 Location 按播放用途包含临时地址。

应用通过配置目录边界和符号链接解析限制本地访问；云端代理拒绝私网和 loopback 上游地址，且不转发 115 账号 Cookie。应用没有用户权限系统，隐藏表示从 Feed 排除，不构成访问权限隔离。

## 喜欢与同步

- `PUT /api/videos/{id}/like`：`{"value":true}`；Video 新增 `liked`，Feed 支持 `mode=likes`，可组合既有搜索和筛选参数。
- `GET /api/sync/tasks`：同步配置列表。
- `POST /api/sync/tasks`：`{"name":"备份","sourceId":1,"target":"115目录ID","intervalHours":6,"enabled":true}`。来源必须为本地媒体源。目标目录通过 `/api/directories?kind=p115&key=0` 分页选择。首次定时执行在一个周期后。
- `PATCH /api/sync/tasks/{id}`：`{"enabled":false,"intervalHours":6}`。
- `POST /api/sync/tasks/{id}/run`：手动启动，返回运行 ID。全局已有同步时返回 409 `sync_running`。
- `POST /api/sync/runs/{id}/cancel`：取消执行，不回滚已上传文件。
- `GET /api/sync/runs?before=0`：最新 50 条；下一页传最后一条 id。
- `GET /api/sync/runs/{id}/records?after=0`：100 条文件结果；下一页传最后一条 id。

运行状态：`running/cooling/completed/partial/failed/canceled/interrupted`。文件状态：`uploaded/skipped/failed`。运行包含 `uploaded/skipped/failed/currentFile/message/startedAt/finishedAt`。明细包含 `path/status/message/size/createdAt`。上传规则固定为单向复制、同目录同名跳过，不覆盖或删除。

### 补全元数据
`POST /api/sources/{id}/metadata` 创建只处理缺失时长、尺寸或封面的任务，返回 `{"id":任务ID}`。使用 `/api/jobs` 查看进度、`POST /api/jobs/{id}/cancel` 取消。同一来源已有扫描或补全任务时返回 409。已有成功字段和可用状态保留；失败项目可再次补全。

## 媒体类型
Video 响应增加 `mediaType: video|audio|image`；`GET /api/feed?mediaType=audio` 可过滤类型。为保持接口兼容，视频、音频、图片共用 `/videos/{id}`、收藏和历史接口。图片显示成功后记录浏览时间，图片没有播放进度。媒体类型按扩展名识别，实际解码由浏览器决定。
