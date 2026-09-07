import { dateLabel, sizeLabel, time, type Video } from "./api";

export default function VideoDetails({
  video: v,
  duration = v.duration,
}: {
  video: Video;
  duration?: number;
}) {
  return (
    <div className="video-details">
      <h3>{v.title}</h3>
      <dl>
        <div>
          <dt>媒体源</dt>
          <dd>
            {v.sourceName || (v.kind === "local" ? "本地视频" : "115 云盘")}
          </dd>
        </div>
        <div>
          <dt>时长</dt>
          <dd>{duration > 0 ? time(duration) : "未知"}</dd>
        </div>
        <div>
          <dt>分辨率</dt>
          <dd>
            {v.width > 0 && v.height > 0 ? `${v.width} × ${v.height}` : "未知"}
          </dd>
        </div>
        <div>
          <dt>大小</dt>
          <dd>{sizeLabel(v.size || 0)}</dd>
        </div>
        <div>
          <dt>格式</dt>
          <dd>{v.title.split(".").pop()?.toUpperCase() || "未知"}</dd>
        </div>
        <div>
          <dt>入库时间</dt>
          <dd>{dateLabel(v.indexedAt)}</dd>
        </div>
        <div>
          <dt>修改时间</dt>
          <dd>{dateLabel(v.modifiedAt)}</dd>
        </div>
        <div className="detail-path">
          <dt>所在目录</dt>
          <dd>{v.folder || "媒体源根目录"}</dd>
        </div>
        {v.kind === "p115" && (
          <div>
            <dt>115 文件 ID</dt>
            <dd>{v.fileKey || "未知"}</dd>
          </div>
        )}
      </dl>
    </div>
  );
}
