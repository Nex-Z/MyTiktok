import { useState } from "react";
import {
  ArrowLeft,
  Search,
  SlidersHorizontal,
  X,
  RefreshCw,
} from "lucide-react";
import type { Source } from "./api";
export type Filters = {
  q: string;
  mediaType: string;
  kind: string;
  sourceId: string;
  duration: string;
  orientation: string;
  sort: string;
};
export const initialFilters = (sort: string): Filters => ({
  q: "",
  mediaType: "",
  kind: "",
  sourceId: "",
  duration: "",
  orientation: "",
  sort,
});
export default function BrowseFilters({
  tab,
  title,
  f,
  sources,
  change,
  refresh,
  reset,
  onBack,
}: {
  tab: string;
  title: string;
  f: Filters;
  sources: Source[];
  change: (patch: Partial<Filters>) => void;
  refresh: () => void;
  reset: () => void;
  onBack?: () => void;
}) {
  const [expanded, setExpanded] = useState(false);
  const constrained = !!(
    f.mediaType ||
    f.q ||
    f.kind ||
    f.sourceId ||
    f.duration ||
    f.orientation
  );
  return (
    <>
      <header className={onBack ? "search-header" : "browse-header"}>
        {onBack && (
          <button
            className="icon-button"
            aria-label="返回播放"
            onClick={onBack}
          >
            <ArrowLeft size={21} />
          </button>
        )}
        {!onBack && (
          <div className="page-heading">
            <h1>{title}</h1>
            <span>
              {tab === "explorer"
                ? "浏览目录，快速找到下一段视频"
                : "喜欢的内容，随时接着看"}
            </span>
          </div>
        )}
        <form
          className="search-box"
          role="search"
          onSubmit={(e) => {
            e.preventDefault();
            refresh();
          }}
        >
          <Search size={19} />
          <input
            aria-label="搜索视频"
            placeholder="搜索视频名称"
            value={f.q}
            onChange={(e) => change({ q: e.target.value })}
          />
          {f.q && (
            <button
              type="button"
              aria-label="清空搜索"
              onClick={() => change({ q: "" })}
            >
              <X size={16} />
            </button>
          )}
          <button type="submit">搜索</button>
        </form>
        {onBack ? (
          <button
            className={`icon-button ${constrained ? "filtered" : ""}`}
            aria-label="筛选视频"
            aria-expanded={expanded}
            onClick={() => setExpanded((v) => !v)}
          >
            <SlidersHorizontal size={20} />
          </button>
        ) : (
          <button className="icon-button" aria-label="刷新" onClick={refresh}>
            <RefreshCw size={19} />
          </button>
        )}
      </header>
      {(!onBack || expanded) && (
        <section className="filter-bar" aria-label="搜索筛选">
          <div className="source-chips">
            {[
              ["", "全部"],
              ["local", "本地"],
              ["p115", "115 云盘"],
            ].map(([value, label]) => (
              <button
                key={value}
                className={f.kind === value ? "selected" : ""}
                onClick={() => change({ kind: value, sourceId: "" })}
              >
                {label}
              </button>
            ))}
          </div>
          <span className="filter-divider" />
          <select
            aria-label="媒体类型"
            value={f.mediaType}
            onChange={(e) => change({ mediaType: e.target.value })}
          >
            <option value="">全部类型</option>
            <option value="video">视频</option>
            <option value="audio">音频</option>
            <option value="image">图片</option>
          </select>
          <select
            aria-label="媒体源筛选"
            value={f.sourceId}
            onChange={(e) => change({ sourceId: e.target.value })}
          >
            <option value="">全部媒体源</option>
            {sources
              .filter((s) => !f.kind || s.kind === f.kind)
              .map((s) => (
                <option key={s.id} value={s.id}>
                  {s.name}
                </option>
              ))}
          </select>
          <select
            aria-label="时长筛选"
            value={f.duration}
            onChange={(e) => change({ duration: e.target.value })}
          >
            <option value="">全部时长</option>
            <option value="short">1 分钟以内</option>
            <option value="medium">1–5 分钟</option>
            <option value="long">5 分钟以上</option>
            <option value="unknown">时长未知</option>
          </select>
          <select
            aria-label="画幅筛选"
            value={f.orientation}
            onChange={(e) => change({ orientation: e.target.value })}
          >
            <option value="">全部画幅</option>
            <option value="portrait">竖屏</option>
            <option value="landscape">横屏</option>
            <option value="square">方形</option>
            <option value="unknown">画幅未知</option>
          </select>
          <select
            aria-label="排序方式"
            value={f.sort}
            onChange={(e) => change({ sort: e.target.value })}
          >
            {(tab === "feed" || tab === "explorer") && (
              <option value="random">随机排序</option>
            )}
            <option value="recent">
              {tab === "history" ? "最近观看" : "最近入库"}
            </option>
            <option value="title">名称 A–Z</option>
            <option value="duration">时长优先</option>
          </select>
          {constrained && (
            <button className="text-button reset-filter" onClick={reset}>
              重置
            </button>
          )}
        </section>
      )}
    </>
  );
}
