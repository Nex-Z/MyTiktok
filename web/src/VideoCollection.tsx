import { useEffect, useState } from "react";
import { Bookmark, Film, Play, Music2, ImageIcon } from "lucide-react";
import { time, sizeLabel, type Video } from "./api";
export default function VideoCollection({
  items,
  layout,
  selected,
  explorer,
  onOpen,
  onFavorite,
}: {
  items: Video[];
  layout: "grid" | "list";
  selected: number | null;
  explorer: boolean;
  onOpen: (index: number, full?: boolean) => void;
  onFavorite: (v: Video) => Promise<void>;
}) {
  return (
    <div className={layout === "grid" ? "video-grid" : "file-list"}>
      {layout === "list" && (
        <div className="file-list-head">
          <span>名称</span>
          <span>时长</span>
          <span>大小</span>
          <span>媒体源</span>
        </div>
      )}
      {items.map((v, i) =>
        layout === "grid" ? (
          <article
            className={`video-card ${explorer && selected === i ? "selected" : ""}`}
            key={v.id}
          >
            <button
              className="video-row video-open"
              onClick={() => onOpen(i)}
              onDoubleClick={() => onOpen(i, true)}
              aria-label={`播放 ${v.title}`}
            >
              <div className="cover-frame">
                <Cover video={v} />
                <span className="cover-duration">
                  {v.mediaType === "image"
                    ? "图片"
                    : v.duration > 0
                      ? time(v.duration)
                      : v.mediaType === "audio"
                        ? "音频"
                        : "未知时长"}
                </span>
                <span className="cover-play">
                  <Play fill="currentColor" size={25} />
                </span>
                <span className="cover-source">
                  {v.kind === "local" ? "本地" : "115"}
                </span>
              </div>
              <h3 title={v.title}>{v.title}</h3>
            </button>
            <div className="card-meta">
              <span>
                {v.sourceName || (v.kind === "local" ? "本地视频" : "115 云盘")}
              </span>
              <button
                aria-label={
                  v.favorite ? `取消收藏 ${v.title}` : `收藏 ${v.title}`
                }
                className={v.favorite ? "is-favorite" : ""}
                onClick={() => void onFavorite(v)}
              >
                <Bookmark
                  size={15}
                  fill={v.favorite ? "currentColor" : "none"}
                />
              </button>
            </div>
          </article>
        ) : (
          <button
            className={`video-row file-row ${selected === i && explorer ? "selected" : ""}`}
            key={v.id}
            onClick={() => onOpen(i)}
            onDoubleClick={() => onOpen(i, true)}
          >
            <span className="file-name">
              <span className="file-cover">
                <Cover video={v} />
              </span>
              <span>
                <strong>{v.title}</strong>
                <small>{v.folder || "根目录"}</small>
              </span>
              {v.favorite && <Bookmark size={13} fill="currentColor" />}
            </span>
            <span>{v.duration > 0 ? time(v.duration) : "未知"}</span>
            <span>{sizeLabel(v.size)}</span>
            <span>{v.sourceName || v.kind}</span>
          </button>
        ),
      )}
    </div>
  );
}
function Cover({ video: v }: { video: Video }) {
  const [failed, setFailed] = useState(false);
  useEffect(() => setFailed(false), [v.cover]);
  return v.cover && !failed ? (
    <img src={v.cover} alt="" loading="lazy" onError={() => setFailed(true)} />
  ) : (
    <span className="cover-placeholder">
      {v.mediaType === "audio" ? (
        <Music2 size={32} />
      ) : v.mediaType === "image" ? (
        <ImageIcon size={32} />
      ) : (
        <Film size={32} />
      )}
      <small>{v.mediaType === "audio" ? "音频" : "暂无封面"}</small>
    </span>
  );
}
