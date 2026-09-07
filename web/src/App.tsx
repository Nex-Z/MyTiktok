import { useCallback, useEffect, useRef, useState } from "react";
import {
  ArrowLeft,
  Check,
  UserRound,
  RefreshCw,
  Compass,
  Search,
  Film,
  HardDrive,
  LoaderCircle,
  Play,
  X,
  LayoutGrid,
  List,
  ChevronRight,
  Maximize2,
  Folder,
} from "lucide-react";
import { api, type Video, type Feed, type Source } from "./api";
import Admin from "./Admin";
import Sync from "./Sync";
import Player from "./Player";
import VideoCollection from "./VideoCollection";
import BrowseFilters, { initialFilters, type Filters } from "./BrowseFilters";

type BrowseTab = "likes" | "feed" | "favorites" | "history" | "explorer";
type Tab = BrowseTab | "admin" | "sync";
const titles = {
  likes: "喜欢",
  sync: "同步",
  feed: "随心看",
  favorites: "我的收藏",
  history: "最近观看",
  explorer: "文件速览",
  admin: "媒体源",
};
type FolderItem = { name: string; path: string; count: number };

export default function App() {
  const [homeBrowse, setHomeBrowse] = useState(false);
  const [tab, setTab] = useState<Tab>("feed");
  const [filters, setFilters] = useState<Record<BrowseTab, Filters>>({
    likes: initialFilters("recent"),
    feed: initialFilters("random"),
    favorites: initialFilters("recent"),
    history: initialFilters("recent"),
    explorer: initialFilters("title"),
  });
  const [sources, setSources] = useState<Source[]>([]),
    [items, setItems] = useState<Video[]>([]),
    [next, setNext] = useState(""),
    [total, setTotal] = useState(0),
    [busy, setBusy] = useState(false),
    [error, setError] = useState(""),
    [toast, setToast] = useState("");
  const [selected, setSelected] = useState<number | null>(null),
    [immersive, setImmersive] = useState(false),
    [muted, setMuted] = useState(true),
    [revision, setRevision] = useState(0),
    [layout, setLayout] = useState<"grid" | "list">("grid"),
    [folder, setFolder] = useState(""),
    [folders, setFolders] = useState<FolderItem[]>([]),
    [recursive, setRecursive] = useState(false),
    [folderError, setFolderError] = useState("");
  const generation = useRef(0),
    pending = useRef(false),
    wheelGesture = useRef(0),
    lastNavigation = useRef(0),
    loadTrigger = useRef<HTMLDivElement>(null);
  const immersiveRef = useRef<HTMLDivElement>(null);
  const browseTab: BrowseTab = tab === "admin" || tab === "sync" ? "feed" : tab;
  const f = filters[browseTab];
  const notify = useCallback((message: string) => setToast(message), []);
  const change = (patch: Partial<Filters>) => {
    setFilters((v) => ({ ...v, [browseTab]: { ...v[browseTab], ...patch } }));
    if ("sourceId" in patch || "kind" in patch) setFolder("");
  };
  const switchTab = (value: Tab) => {
    setImmersive(false);
    setSelected(null);
    setTab(value);
  };
  const closePlayer = useCallback(() => setImmersive(false), []);
  useEffect(() => {
    if (!toast) return;
    const t = setTimeout(() => setToast(""), 3200);
    return () => clearTimeout(t);
  }, [toast]);
  useEffect(() => {
    const c = new AbortController();
    api<Source[]>("/sources", "GET", undefined, c.signal)
      .then(setSources)
      .catch((e) => {
        if (!c.signal.aborted) notify(e.message);
      });
    return () => c.abort();
  }, [tab, revision, notify]);
  useEffect(() => {
    if (!immersive) return;
    const old = document.body.style.overflow;
    const previousFocus = document.activeElement as HTMLElement | null;
    document.body.style.overflow = "hidden";
    immersiveRef.current?.focus({ preventScroll: true });
    const key = (e: KeyboardEvent) => {
      if (document.querySelector("dialog[open]")) return;
      if (e.key === "Tab") {
        const nodes = Array.from(
          immersiveRef.current?.querySelectorAll<HTMLElement>(
            'button:not([disabled]),input:not([disabled]),[tabindex="0"]',
          ) || [],
        ).filter(
          (el) =>
            el.getClientRects().length > 0 &&
            getComputedStyle(el).visibility !== "hidden",
        );
        if (nodes?.length) {
          const first = nodes[0],
            last = nodes[nodes.length - 1];
          if (
            e.shiftKey &&
            (document.activeElement === first ||
              document.activeElement === immersiveRef.current)
          ) {
            e.preventDefault();
            last.focus();
          } else if (!e.shiftKey && document.activeElement === last) {
            e.preventDefault();
            first.focus();
          }
        }
      }
      if (e.key === "Escape" && !document.querySelector("dialog[open]"))
        closePlayer();
    };
    window.addEventListener("keydown", key);
    return () => {
      document.body.style.overflow = old;
      previousFocus?.focus({ preventScroll: true });
      window.removeEventListener("keydown", key);
    };
  }, [immersive, closePlayer]);
  useEffect(() => {
    setSelected(null);
    setImmersive(false);
    setError("");
    setItems([]);
    setNext("");
    setTotal(0);
    pending.current = false;
    const gen = ++generation.current;
    if (tab === "admin" || tab === "sync") return;
    setBusy(true);
    const c = new AbortController();
    const timer = setTimeout(() => {
      const params = new URLSearchParams({
        ...f,
        mode: tab === "explorer" ? "feed" : tab,
        limit: "24",
      });
      if (tab === "explorer" && f.sourceId) {
        params.set("folder", folder);
        params.set("recursive", recursive ? "1" : "0");
      }
      void api<Feed>("/feed?" + params, "GET", undefined, c.signal)
        .then((data) => {
          if (gen === generation.current) {
            setItems(data.items);
            if (tab === "feed" && data.items.length) setSelected(0);
            setTotal(data.total);
            setNext(data.next);
          }
        })
        .catch((e) => {
          if (!c.signal.aborted) setError(e.message);
        })
        .finally(() => {
          if (gen === generation.current) setBusy(false);
        });
    }, 220);
    return () => {
      clearTimeout(timer);
      c.abort();
    };
  }, [tab, f, folder, recursive, revision]);
  useEffect(() => {
    setFolders([]);
    setFolderError("");
    if (tab !== "explorer" || !f.sourceId) return;
    const c = new AbortController();
    api<FolderItem[]>(
      `/library/folders?sourceId=${f.sourceId}&parent=${encodeURIComponent(folder)}`,
      "GET",
      undefined,
      c.signal,
    )
      .then(setFolders)
      .catch((e) => {
        if (!c.signal.aborted) setFolderError(e.message);
      });
    return () => c.abort();
  }, [tab, f.sourceId, folder, revision]);
  const load = useCallback(async () => {
    if (!next || pending.current) return;
    pending.current = true;
    const gen = generation.current;
    try {
      const data = await api<Feed>(
        "/feed?cursor=" + encodeURIComponent(next) + "&limit=24",
      );
      if (gen === generation.current) {
        setItems((v) => [...v, ...data.items]);
        setNext(data.next);
      }
    } catch (e) {
      notify((e as Error).message);
    } finally {
      if (gen === generation.current) pending.current = false;
    }
  }, [next, notify]);
  useEffect(() => {
    if (
      selected !== null &&
      (immersive || (tab === "feed" && !homeBrowse)) &&
      selected >= items.length - 4 &&
      next
    )
      void load();
  }, [selected, immersive, items.length, next, load, tab, homeBrowse]);
  useEffect(() => {
    const el = loadTrigger.current;
    if (!el || !next || immersive) return;
    const observer = new IntersectionObserver(
      (entries) => {
        if (entries[0].isIntersecting) void load();
      },
      { rootMargin: "160px" },
    );
    observer.observe(el);
    return () => observer.disconnect();
  }, [next, load, immersive, busy]);
  const favorite = async (v: Video) => {
    try {
      await api(`/videos/${v.id}/favorite`, "PUT", { value: !v.favorite });
      setItems((rows) =>
        rows.map((x) => (x.id === v.id ? { ...x, favorite: !v.favorite } : x)),
      );
      notify(v.favorite ? "已取消收藏" : "已加入收藏");
    } catch (e) {
      notify((e as Error).message);
    }
  };
  const like = async (v: Video) => {
    try {
      await api("/videos/" + v.id + "/like", "PUT", { value: !v.liked });
      setItems((rows) =>
        rows.map((x) => (x.id === v.id ? { ...x, liked: !v.liked } : x)),
      );
    } catch (e) {
      notify((e as Error).message);
    }
  };
  const hide = async (v: Video) => {
    try {
      await api(`/videos/${v.id}/hidden`, "PUT", { value: true });
      setItems((rows) => rows.filter((x) => x.id !== v.id));
      setTotal((n) => Math.max(0, n - 1));
      setSelected((i) =>
        items.length < 2 ? null : Math.min(i ?? 0, items.length - 2),
      );
      if (items.length < 2) setImmersive(false);
      notify("已隐藏，可在媒体源中恢复");
    } catch (e) {
      notify((e as Error).message);
    }
  };
  const open = (
    index: number,
    full = tab !== "explorer" || window.innerWidth < 900,
  ) => {
    setSelected(index);
    setImmersive(full);
  };
  const current = selected === null ? undefined : items[selected];
  const move = (delta: number) => {
    if (Date.now() - lastNavigation.current < 500) return;
    lastNavigation.current = Date.now();
    const i = (selected ?? 0) + delta;
    if (i >= 0 && i < items.length) setSelected(i);
    else if (next) void load();
    else if (delta > 0) notify("已到最后一条，刷新可重新开始");
  };
  const player = (variant: "immersive" | "preview") =>
    current && (
      <Player
        key={current.id + variant}
        video={current}
        nextVideo={
          immersive || (tab === "feed" && !homeBrowse)
            ? items[(selected ?? 0) + 1]
            : undefined
        }
        muted={muted}
        setMuted={setMuted}
        resume={tab === "history"}
        wheelGesture={wheelGesture}
        onMove={move}
        onLike={() => void like(current)}
        onFavorite={() => void favorite(current)}
        onHide={() => void hide(current)}
        notify={notify}
        variant={variant}
      />
    );
  const constrained = !!(
    f.mediaType ||
    f.q ||
    f.kind ||
    f.sourceId ||
    f.duration ||
    f.orientation
  );
  const reset = () => {
    setFilters((v) => ({
      ...v,
      [browseTab]: initialFilters(
        browseTab === "feed"
          ? "random"
          : browseTab === "explorer"
            ? "title"
            : "recent",
      ),
    }));
    setFolder("");
  };
  return (
    <div
      className={`app douyin-app ${tab === "feed" && !homeBrowse ? "home-playing" : ""}`}
    >
      <aside className="sidebar" inert={immersive}>
        <a
          className="brand"
          href="#"
          onClick={(e) => {
            e.preventDefault();
            switchTab("feed");
          }}
        >
          <span className="brand-icon">
            <Film size={27} />
          </span>
          <span>
            MyTiktok<small>我的私人视频库</small>
          </span>
        </a>
        <nav>
          {(
            [
              { id: "feed", icon: Compass },
              { id: "likes", icon: UserRound },
              { id: "sync", icon: RefreshCw },
              { id: "admin", icon: HardDrive },
            ] as const
          ).map((n) => (
            <button
              className={`nav-item ${tab === n.id || (n.id === "likes" && ["favorites", "history", "explorer"].includes(tab)) ? "active" : ""}`}
              key={n.id}
              onClick={() => switchTab(n.id)}
            >
              <n.icon size={21} />
              <span>
                {n.id === "likes"
                  ? "我的"
                  : n.id === "feed"
                    ? "首页"
                    : n.id === "admin"
                      ? "设置"
                      : titles[n.id]}
              </span>
            </button>
          ))}
        </nav>
        <div className="sidebar-bottom">
          <span className="online-dot" />
          仅属于你的媒体库<small>LOCAL / 115 CLOUD</small>
        </div>
      </aside>
      <main inert={immersive}>
        {tab === "sync" ? (
          <Sync notify={notify} sources={sources} />
        ) : tab === "admin" ? (
          <Admin notify={notify} />
        ) : (
          <>
            {tab !== "feed" && (
              <>
                <div className="profile-heading">
                  <span className="profile-avatar">
                    <UserRound size={32} />
                  </span>
                  <div>
                    <h1>我的视频</h1>
                    <p>喜欢的内容，留在这里</p>
                  </div>
                </div>
                <div className="profile-tabs">
                  {(["likes", "favorites", "history", "explorer"] as const).map(
                    (t) => (
                      <button
                        key={t}
                        className={tab === t ? "active" : ""}
                        onClick={() => switchTab(t)}
                      >
                        {titles[t]}
                      </button>
                    ),
                  )}
                </div>
              </>
            )}
            {tab === "feed" && !homeBrowse && (
              <div className="home-toolbar">
                <button
                  className="icon-button home-search"
                  aria-label={homeBrowse ? "返回播放" : "检索视频"}
                  title={homeBrowse ? "返回播放" : "检索视频"}
                  onClick={() => setHomeBrowse((v) => !v)}
                >
                  {homeBrowse ? <ArrowLeft size={21} /> : <Search size={21} />}
                </button>
              </div>
            )}
            <div className="browse-controls">
              <BrowseFilters
                onBack={tab === "feed" ? () => setHomeBrowse(false) : undefined}
                tab={tab}
                title={titles[tab]}
                f={f}
                sources={sources}
                change={change}
                refresh={() => setRevision((v) => v + 1)}
                reset={reset}
              />
              <div className="results-heading">
                <span>
                  {busy ? "正在检索…" : `${total} 个视频`}
                  {f.q && <> · “{f.q}”</>}
                </span>
                <div className="result-actions">
                  <div className="view-switch">
                    <button
                      aria-label="封面视图"
                      aria-pressed={layout === "grid"}
                      onClick={() => setLayout("grid")}
                    >
                      <LayoutGrid size={17} />
                    </button>
                    <button
                      aria-label="列表视图"
                      aria-pressed={layout === "list"}
                      onClick={() => setLayout("list")}
                    >
                      <List size={18} />
                    </button>
                  </div>
                  {tab !== "feed" && (
                    <button
                      className="primary start-feed"
                      disabled={!items.length || busy}
                      onClick={() => open(0, true)}
                    >
                      <Play size={16} fill="currentColor" />
                      开始刷视频
                    </button>
                  )}
                </div>
              </div>
            </div>
            {tab === "feed" && !homeBrowse && current && !immersive ? (
              <div className="home-player">{player("immersive")}</div>
            ) : null}
            <div
              hidden={tab === "feed" && !homeBrowse && !!current}
              className={`browse-workspace ${tab === "explorer" ? "explorer" : ""} ${current && !immersive && tab === "explorer" ? "with-preview" : ""}`}
            >
              {tab === "explorer" && (
                <aside className="folder-sidebar">
                  <h2>
                    <HardDrive size={15} />
                    媒体位置
                  </h2>
                  <button
                    className={!f.sourceId ? "active" : ""}
                    onClick={() => change({ sourceId: "" })}
                  >
                    <LayoutGrid size={15} />
                    全部视频
                  </button>
                  {sources
                    .filter((s) => !f.kind || s.kind === f.kind)
                    .map((s) => (
                      <button
                        key={s.id}
                        className={f.sourceId === String(s.id) ? "active" : ""}
                        onClick={() => change({ sourceId: String(s.id) })}
                      >
                        <Folder size={16} />
                        <span>{s.name}</span>
                        <ChevronRight size={13} />
                      </button>
                    ))}
                  <p>仅显示已入库目录</p>
                </aside>
              )}
              <section className="browse-content">
                {tab === "explorer" && f.sourceId && (
                  <>
                    <div className="breadcrumbs">
                      <button
                        aria-label="上级目录"
                        disabled={!folder}
                        onClick={() =>
                          setFolder(folder.split("/").slice(0, -1).join("/"))
                        }
                      >
                        <ArrowLeft size={16} />
                      </button>
                      <button onClick={() => setFolder("")}>
                        {sources.find((s) => String(s.id) === f.sourceId)?.name}
                      </button>
                      {folder
                        .split("/")
                        .filter(Boolean)
                        .map((part, i) => (
                          <span key={i}>
                            <ChevronRight size={12} />
                            <button
                              onClick={() =>
                                setFolder(
                                  folder
                                    .split("/")
                                    .slice(0, i + 1)
                                    .join("/"),
                                )
                              }
                            >
                              {part}
                            </button>
                          </span>
                        ))}
                      <label>
                        <input
                          type="checkbox"
                          checked={recursive}
                          onChange={(e) => setRecursive(e.target.checked)}
                        />
                        包含子目录
                      </label>
                    </div>
                    {folderError && (
                      <p className="error-banner">{folderError}</p>
                    )}
                    {folders.length > 0 && (
                      <div className="folder-grid">
                        {folders.map((d) => (
                          <button
                            key={d.path}
                            onClick={() => setFolder(d.path)}
                          >
                            <Folder size={24} />
                            <span>
                              {d.name}
                              <small>{d.count} 个视频</small>
                            </span>
                            <ChevronRight size={15} />
                          </button>
                        ))}
                      </div>
                    )}
                  </>
                )}
                {busy ? (
                  <div className="empty browse-empty">
                    <LoaderCircle className="spin" />
                    <p>正在检索媒体库</p>
                  </div>
                ) : error ? (
                  <div className="empty browse-empty">
                    <h2>暂时无法加载</h2>
                    <p>{error}</p>
                    <button
                      className="secondary"
                      onClick={() => setRevision((v) => v + 1)}
                    >
                      重新加载
                    </button>
                  </div>
                ) : items.length === 0 ? (
                  <div className="empty browse-empty">
                    <Film size={42} />
                    <h2>
                      {constrained
                        ? "没有匹配的视频"
                        : tab === "favorites"
                          ? "还没有收藏的视频"
                          : tab === "history"
                            ? "还没有观看记录"
                            : "把喜欢的视频，放在一起"}
                    </h2>
                    <p>
                      {constrained
                        ? "试试其他关键词，或调整筛选条件。"
                        : "添加本地目录或连接 115，开始你的私人放映。"}
                    </p>
                    <button
                      className="primary"
                      onClick={() =>
                        constrained ? reset() : switchTab("admin")
                      }
                    >
                      {constrained ? "清除筛选" : "添加媒体源"}
                    </button>
                  </div>
                ) : (
                  <>
                    <VideoCollection
                      items={items}
                      layout={layout}
                      selected={selected}
                      explorer={tab === "explorer"}
                      onOpen={open}
                      onFavorite={favorite}
                    />
                    <div className="load-trigger" ref={loadTrigger}>
                      {next ? (
                        <button
                          className="secondary"
                          onClick={() => void load()}
                        >
                          加载更多
                        </button>
                      ) : (
                        <span>已显示全部结果</span>
                      )}
                    </div>
                  </>
                )}
              </section>
              {tab === "explorer" && current && !immersive && (
                <aside className="quick-preview">
                  <header>
                    <span>快速预览</span>
                    <button
                      className="icon-button"
                      aria-label="进入竖屏播放"
                      onClick={() => setImmersive(true)}
                    >
                      <Maximize2 size={17} />
                    </button>
                    <button
                      className="icon-button"
                      aria-label="关闭预览"
                      onClick={() => setSelected(null)}
                    >
                      <X size={18} />
                    </button>
                  </header>
                  {player("preview")}
                </aside>
              )}
            </div>
          </>
        )}
      </main>
      {immersive && current && (
        <div
          className="immersive-view"
          ref={immersiveRef}
          tabIndex={-1}
          role="dialog"
          aria-modal="true"
          aria-label="竖屏刷视频"
        >
          <header className="immersive-header">
            <button
              className="back-button"
              onClick={closePlayer}
              aria-label="退出刷视频"
            >
              <ArrowLeft size={22} />
              <span>返回</span>
            </button>
            <span>
              {titles[tab]}
              <small>
                {(selected ?? 0) + 1} / {total}
              </small>
            </span>
            <button
              className="icon-button"
              onClick={closePlayer}
              aria-label="关闭播放器"
            >
              <X size={21} />
            </button>
          </header>
          {player("immersive")}
        </div>
      )}
      {toast && (
        <div className="toast" role="status">
          <Check size={16} />
          {toast}
        </div>
      )}
    </div>
  );
}
