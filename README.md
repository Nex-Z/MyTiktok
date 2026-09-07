# MyTiktok

适合 NAS 自用的媒体库：本地目录与 115 云盘统一浏览，支持视频、音频、静态图片、搜索筛选、喜欢、收藏、观看记录及本地到 115 的定时同步。

Go + SQLite + React，单容器部署，局域网／VPN 内免登录。FFmpeg 用于时长、尺寸和封面提取，不做实时转码。所有能访问页面的人均可管理媒体库。

## NAS 使用发布产物部署

GitHub Release 提供 Linux amd64 / arm64 的 Docker 镜像包，NAS 无需安装 Go 或 Node，也无需编译源码。

```sh
mkdir -p MyTiktok && cd MyTiktok
curl -fL https://github.com/Nex-Z/MyTiktok/releases/latest/download/update.sh -o update.sh
sh update.sh
```

默认地址为 `http://NAS地址:18081`。首次启动为空媒体库，在设置里连接 115，或添加本地 `/media` 目录。

编辑部署目录中的 `.env` 指定 NAS 上真实目录：

```dotenv
MEDIA_PATH=/your/nas/media
PORT=18081
BIND_ADDRESS=0.0.0.0
APP_UID=1000
APP_GID=1000
IMAGE=mytiktok:latest
```

`APP_UID`、`APP_GID` 必须与 NAS 上有权读取该目录的用户匹配，可运行 `id` 查看。目录只读挂载，容器不会修改或删除本地媒体。修改配置后执行：

```sh
docker compose -f compose.release.yaml up -d
```

以后更新只需：

```sh
sh update.sh
```

更新脚本下载对应架构镜像，检查 SHA-256，加载镜像，停止服务备份数据，再重建容器。下载失败不会停止当前服务。备份位于 `backups/`，数据库、115 凭证和封面位于 `data/`；请保护备份的访问权限。更新脚本默认跟随最新 Release；不要在本目录混用源码版 Compose 管理同一个服务。

## 媒体浏览

- 首页直接播放；“我的”内包含喜欢、收藏、最近观看、文件速览。搜索支持名称、类型、来源、时长和画幅。
- 音频使用 HTML audio；图片静态显示，手动切换，不自动幻灯播放。音频首次尝试有声播放，浏览器拦截时点击播放。
- 视频和图片支持双指放大／缩小（1–4 倍），放大后单指拖动，点击“恢复缩放”复位；切换媒体时复位。缩放手势不会触发上下切换或长按菜单。
- 横竖视频完整显示；手机横屏控件自动收起，点击唤出。长按或信息按钮查看文件信息；PC 显示在下方。
- 本地视频格式：MP4、M4V、MOV、WebM、MKV、AVI；音频：MP3、M4A、AAC、WAV、FLAC、OGG、OPUS；图片：JPEG、PNG、WebP、BMP、AVIF。能否播放取决于浏览器编码支持，不能保证每种容器内的所有编码。
- 115 凭证只存服务端。直连失败后刷新一次，再切换 NAS 代理；代理支持 Range。
- 视频封面优先使用网盘缩略图，否则有限读取开头与 MP4 索引，最长边 480px。补全失败可在设置里点击“补全时长与封面”。云端视频元数据每文件最多 16MiB、原文件 75%、60 秒；图片和音频元数据仅下载不超过 8MiB 的文件，超出时允许未知时长或占位封面，不影响尝试播放原文件。

## 同步

选择本地媒体源和 115 目标目录，保留目录结构，递归复制普通文件。同目录同名项直接跳过，不覆盖、不删除。可设置周期、手动执行、取消及查看逐文件结果。避免其他客户端同时向同一目标目录写同名文件。

## 从源码运行

依赖 Go 1.26、Node 24、FFmpeg/ffprobe。

```sh
cd web && npm ci && npm run build && cd ..
MEDIA_ROOT=/your/media go run ./cmd/mytiktok
```

Windows 可使用 `scripts/dev.ps1`。源码容器构建使用 `compose.yaml`；网络受限时可用构建参数 `GOPROXY` 指定依赖代理。HTTP 接口见 [docs/API.md](docs/API.md)。

## 测试与发布

```sh
go test ./...
go vet ./...
cd web && npm ci && npm run build && npx playwright install chromium && npm test
```

CI 运行 Go 与浏览器测试；推送 `v*` 标签后，Release 工作流构建两种架构的镜像，全部构建成功才公开 Release。发布库只包含源码、配置模板和合成测试夹具，运行数据、凭证、媒体、环境配置、机器专属文档和截图不纳入版本控制。

本项目不提供公网登录、多用户权限、爬虫、分享或转码。第三方说明见 [THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md)。
