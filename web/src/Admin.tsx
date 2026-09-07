import { useCallback, useEffect, useState } from "react";
import {
  Cloud,
  HardDrive,
  Plus,
  Folder,
  ArrowLeft,
  RefreshCw,
  ScanLine,
  Trash2,
  Square,
  Check,
  Eye,
  LoaderCircle,
  X,
} from "lucide-react";
import { api, type Source, type Job, type Feed, type Video } from "./api";
type Directory = { key: string; name: string; dir: boolean };
type DirectoryPage = { entries: Directory[]; next: string; root: string };
const labels: Record<string, string> = {
  running: "扫描中",
  cooling: "冷却中",
  completed: "已完成",
  failed: "失败",
  canceled: "已取消",
  interrupted: "已中断",
};
export default function Admin({ notify }: { notify: (s: string) => void }) {
  const [sources, setSources] = useState<Source[]>([]),
    [jobs, setJobs] = useState<Job[]>([]),
    [error, setError] = useState(""),
    [adding, setAdding] = useState(false),
    [connected, setConnected] = useState(false),
    [checking, setChecking] = useState(false),
    [cookie, setCookie] = useState(""),
    [qr, setQR] = useState<{ id: string; image: string } | null>(null),
    [qrState, setQRState] = useState(""),
    [hidden, setHidden] = useState<Video[]>([]),
    [hiddenNext, setHiddenNext] = useState(""),
    [showHidden, setShowHidden] = useState(false),
    [connectBusy, setConnectBusy] = useState(false);
  const refresh = useCallback(async () => {
    try {
      const [s, j] = await Promise.all([
        api<Source[]>("/sources"),
        api<Job[]>("/jobs"),
      ]);
      setSources(s);
      setJobs(j);
      setError("");
    } catch (e) {
      setError((e as Error).message);
    }
  }, []);
  const check = useCallback(async () => {
    setChecking(true);
    try {
      const s = await api<{ connected: boolean }>("/p115/status");
      setConnected(s.connected);
    } catch (e) {
      notify((e as Error).message);
    } finally {
      setChecking(false);
    }
  }, [notify]);
  useEffect(() => {
    void refresh();
    void check();
    const t = setInterval(() => void refresh(), 4000);
    return () => clearInterval(t);
  }, [refresh, check]);
  useEffect(() => {
    if (!qr) return;
    let stopped = false;
    let timer: ReturnType<typeof setTimeout>;
    const poll = async () => {
      try {
        const s = await api<{ state: string }>(`/p115/qr/${qr.id}`);
        if (stopped) return;
        setQRState(s.state);
        if (s.state === "success") {
          setConnected(true);
          setQR(null);
          notify("115 已连接");
          return;
        }
        if (s.state === "expired" || s.state === "canceled") return;
        timer = setTimeout(() => void poll(), 2000);
      } catch (e) {
        if (!stopped) {
          setQRState("error");
          notify((e as Error).message);
        }
      }
    };
    timer = setTimeout(() => void poll(), 2000);
    return () => {
      stopped = true;
      clearTimeout(timer);
    };
  }, [qr, notify]);
  const action = async (f: () => Promise<unknown>, message?: string) => {
    try {
      await f();
      await refresh();
      if (message) notify(message);
    } catch (e) {
      notify((e as Error).message);
    }
  };
  const loadHidden = async (cursor = "") => {
    try {
      const f = await api<Feed>(
        `/feed?mode=hidden${cursor ? "&cursor=" + encodeURIComponent(cursor) : ""}`,
      );
      setHidden((v) => (cursor ? [...v, ...f.items] : f.items));
      setHiddenNext(f.next);
      setShowHidden(true);
    } catch (e) {
      notify((e as Error).message);
    }
  };
  return (
    <div className="admin">
      <header className="topbar">
        <div>
          <div className="eyebrow">CONNECTED TO YOUR COLLECTION</div>
          <h1>媒体源</h1>
        </div>
        <button className="primary" onClick={() => setAdding(true)}>
          <Plus size={18} />
          添加媒体源
        </button>
      </header>
      <div className="admin-content">
        {error && (
          <p role="alert" className="error-banner">
            {error}
          </p>
        )}
        <section className="admin-section">
          <div className="section-title">
            <div>
              <h2>
                你的目录{" "}
                <span>{sources.length.toString().padStart(2, "0")}</span>
              </h2>
              <p>扫描选中的目录，视频会自动加入媒体库。</p>
            </div>
            <span className="subtle">文件只读 · 索引同步</span>
          </div>
          {sources.length === 0 ? (
            <div className="empty-source">
              <Folder size={28} />
              <span>还没有媒体源</span>
              <button className="text-button" onClick={() => setAdding(true)}>
                添加第一个目录 →
              </button>
            </div>
          ) : (
            sources.map((s) => (
              <SourceRow
                key={s.id}
                source={s}
                job={jobs.find((j) => j.sourceId === s.id)}
                action={action}
              />
            ))
          )}
        </section>
        <section className="admin-section">
          <div className="section-title">
            <div>
              <h2>
                <Cloud size={22} />
                115 云盘
              </h2>
              <p>使用 115 App 扫码连接，或填写已有 Cookie。</p>
            </div>
            <button
              className="status-button"
              onClick={() => void check()}
              disabled={checking}
            >
              {checking ? (
                <LoaderCircle size={14} className="spin" />
              ) : (
                <span className={connected ? "online-dot" : "offline-dot"} />
              )}{" "}
              {checking ? "检查连接" : connected ? "已连接" : "未连接 / 待重连"}
              <RefreshCw size={13} />
            </button>
          </div>
          <div className="connect-row">
            <button
              className="secondary"
              disabled={connectBusy}
              onClick={async () => {
                setConnectBusy(true);
                try {
                  setQR(await api("/p115/qr", "POST"));
                  setQRState("waiting");
                } catch (e) {
                  notify((e as Error).message);
                } finally {
                  setConnectBusy(false);
                }
              }}
            >
              <ScanLine size={18} />
              {connectBusy ? "正在获取二维码" : "扫码连接"}
            </button>
            <details>
              <summary>使用 Cookie 连接</summary>
              <form
                className="cookie-form"
                onSubmit={async (e) => {
                  e.preventDefault();
                  setConnectBusy(true);
                  try {
                    await api("/p115/cookie", "PUT", { cookie });
                    setCookie("");
                    setConnected(true);
                    notify("115 已连接");
                  } catch (e) {
                    notify((e as Error).message);
                  } finally {
                    setConnectBusy(false);
                  }
                }}
              >
                <input
                  type="password"
                  autoComplete="off"
                  aria-label="115 Cookie"
                  placeholder="UID=…; CID=…; SEID=…"
                  value={cookie}
                  onChange={(e) => setCookie(e.target.value)}
                  required
                />
                <button className="secondary" disabled={connectBusy}>
                  保存连接
                </button>
              </form>
            </details>
          </div>
          {qr && (
            <div className="qr-panel">
              <img src={qr.image} alt="115 登录二维码" />
              <div>
                <h3>
                  {
                    {
                      waiting: "请使用 115 App 扫码",
                      scanned: "已扫码，请在 App 确认",
                      expired: "二维码已过期，请重新获取",
                      canceled: "登录已取消",
                      error: "连接失败，请重新获取",
                    }[qrState]
                  }
                </h3>
                <p>登录凭证仅保存在当前服务端。</p>
                <button className="text-button" onClick={() => setQR(null)}>
                  关闭二维码
                </button>
              </div>
            </div>
          )}
        </section>
        <section className="admin-section">
          <div className="section-title">
            <div>
              <h2>扫描任务</h2>
              <p>定时与手动扫描的执行记录。</p>
            </div>
            <button
              className="icon-button"
              aria-label="刷新任务"
              onClick={() => void refresh()}
            >
              <RefreshCw size={17} />
            </button>
          </div>
          {jobs.length === 0 ? (
            <p className="muted-line">暂无扫描任务</p>
          ) : (
            <div className="jobs">
              {jobs.slice(0, 12).map((j) => (
                <div className="job-row" key={j.id}>
                  <span className={"job-state " + j.status}>
                    {labels[j.status] || j.status}
                  </span>
                  <div>
                    <strong>
                      {sources.find((s) => s.id === j.sourceId)?.name ||
                        "媒体源"}
                    </strong>
                    <small>
                      {j.message || "正在遍历目录"}
                      {j.errors > 0 ? ` · ${j.errors} 项元数据提取失败` : ""}
                    </small>
                  </div>
                  <span>{j.scanned} 条</span>
                  <time>{new Date(j.startedAt * 1000).toLocaleString()}</time>
                  {["running", "cooling"].includes(j.status) && (
                    <button
                      className="icon-button"
                      aria-label="取消扫描"
                      onClick={() =>
                        void action(() => api(`/jobs/${j.id}/cancel`, "POST"))
                      }
                    >
                      <Square size={15} />
                    </button>
                  )}
                </div>
              ))}
            </div>
          )}
        </section>
        <section className="admin-section">
          <div className="section-title">
            <div>
              <h2>已隐藏的视频</h2>
              <p>恢复后重新出现在媒体库，收藏与观看记录会保留。</p>
            </div>
            <button
              className="secondary"
              onClick={() =>
                showHidden ? setShowHidden(false) : void loadHidden()
              }
            >
              {showHidden ? "收起" : "查看隐藏"}
            </button>
          </div>
          {showHidden && (
            <>
              {hidden.length === 0 && (
                <p className="muted-line">没有隐藏的视频</p>
              )}
              {hidden.map((v) => (
                <div className="hidden-row" key={v.id}>
                  <span>{v.title}</span>
                  <button
                    className="text-button"
                    onClick={() =>
                      void action(async () => {
                        await api(`/videos/${v.id}/hidden`, "PUT", {
                          value: false,
                        });
                        setHidden((rows) => rows.filter((x) => x.id !== v.id));
                      }, "视频已恢复")
                    }
                  >
                    <Eye size={16} />
                    恢复
                  </button>
                </div>
              ))}
              {hiddenNext && (
                <button
                  className="secondary"
                  onClick={() => void loadHidden(hiddenNext)}
                >
                  加载更多
                </button>
              )}
            </>
          )}
        </section>
        <footer className="admin-footer">
          MyTiktok / 私有部署 · 局域网免登录 · 本地文件只读
        </footer>
      </div>
      {adding && (
        <AddSource
          connected={connected}
          onClose={() => setAdding(false)}
          onSaved={() => {
            setAdding(false);
            void refresh();
            notify("媒体源已添加，正在首次扫描");
          }}
          notify={notify}
        />
      )}
    </div>
  );
}

function SourceRow({
  source: s,
  job,
  action,
}: {
  source: Source;
  job?: Job;
  action: (f: () => Promise<unknown>, msg?: string) => Promise<void>;
}) {
  const [edit, setEdit] = useState(false),
    [name, setName] = useState(s.name),
    [interval, setInterval] = useState(s.intervalHours);
  const busy = job && ["running", "cooling"].includes(job.status);
  return (
    <div className="source-row">
      <div className="source-icon">
        {s.kind === "local" ? <HardDrive /> : <Cloud />}
      </div>
      <div className="source-info">
        <h3>
          {s.name}
          <span>{s.kind === "local" ? "LOCAL" : "115"}</span>
        </h3>
        <p>{s.root}</p>
        {edit ? (
          <form
            className="edit-source"
            onSubmit={(e) => {
              e.preventDefault();
              void action(async () => {
                await api(`/sources/${s.id}`, "PATCH", {
                  name,
                  intervalHours: interval,
                });
                setEdit(false);
              }, "配置已保存");
            }}
          >
            <input
              aria-label="媒体源名称"
              value={name}
              onChange={(e) => setName(e.target.value)}
              required
            />
            <label>
              每{" "}
              <input
                aria-label="扫描间隔小时"
                type="number"
                min="0"
                max="8760"
                value={interval}
                onChange={(e) => setInterval(Number(e.target.value))}
              />{" "}
              小时扫描（0 关闭）
            </label>
            <button className="text-button">保存</button>
            <button
              type="button"
              className="text-button"
              onClick={() => setEdit(false)}
            >
              取消
            </button>
          </form>
        ) : (
          <button
            className="schedule text-button"
            onClick={() => setEdit(true)}
          >
            {s.intervalHours
              ? `每 ${s.intervalHours} 小时扫描`
              : "定时扫描已关闭"}{" "}
            · 编辑
          </button>
        )}
      </div>
      <div className="source-actions">
 <button className="secondary" disabled={!!busy} onClick={() => void action(() => api(`/sources/${s.id}/metadata`, "POST"), "元数据补全已启动")}>补全时长与封面</button>
        <button
          className="secondary"
          disabled={!!busy}
          onClick={() =>
            void action(
              () => api(`/sources/${s.id}/scan`, "POST"),
              "扫描已启动",
            )
          }
        >
          {busy ? (
            <LoaderCircle size={16} className="spin" />
          ) : (
            <RefreshCw size={16} />
          )}{" "}
          {busy ? "扫描中" : "扫描"}
        </button>
        <button
          className="icon-button danger"
          aria-label={`删除 ${s.name}`}
          onClick={() => {
            if (
              confirm(
                `移除「${s.name}」及其索引、喜欢、收藏、观看记录、同步任务和同步记录？真实文件不会删除。`,
              )
            )
              void action(
                () => api(`/sources/${s.id}`, "DELETE"),
                "媒体源已移除",
              );
          }}
        >
          <Trash2 size={17} />
        </button>
      </div>
    </div>
  );
}

function AddSource({
  connected,
  onClose,
  onSaved,
  notify,
}: {
  connected: boolean;
  onClose: () => void;
  onSaved: () => void;
  notify: (s: string) => void;
}) {
  const [kind, setKind] = useState("local"),
    [name, setName] = useState(""),
    [root, setRoot] = useState(""),
    [interval, setInterval] = useState(6),
    [stack, setStack] = useState<{ key: string; name: string }[]>([
      { key: "", name: "媒体挂载目录" },
    ]),
    [entries, setEntries] = useState<Directory[]>([]),
    [next, setNext] = useState(""),
    [loading, setLoading] = useState(false),
    [saving, setSaving] = useState(false),
    [error, setError] = useState("");
  const key = stack[stack.length - 1].key;
  useEffect(() => {
    let stopped = false;
    setLoading(true);
    setError("");
    setRoot("");
    api<DirectoryPage>(
      `/directories?kind=${kind}&key=${encodeURIComponent(key)}`,
    )
      .then((p) => {
        if (!stopped) {
          setEntries(p.entries);
          setNext(p.next);
          setRoot(p.root);
        }
      })
      .catch((e) => {
        if (!stopped) {
          setError(e.message);
          setEntries([]);
          setNext("");
        }
      })
      .finally(() => {
        if (!stopped) setLoading(false);
      });
    return () => {
      stopped = true;
    };
  }, [kind, key]);
  const changeKind = (k: string) => {
    setKind(k);
    setStack([
      {
        key: k === "local" ? "" : "0",
        name: k === "local" ? "媒体挂载目录" : "115 根目录",
      },
    ]);
    setRoot("");
  };
  return (
    <div
      className="modal-backdrop"
      onClick={(e) => {
        if (e.target === e.currentTarget) onClose();
      }}
    >
      <section
        className="modal"
        role="dialog"
        aria-modal="true"
        aria-label="添加媒体源"
      >
        <div className="section-title">
          <h2>添加媒体源</h2>
          <button className="icon-button" onClick={onClose} aria-label="关闭">
            <X />
          </button>
        </div>
        <div className="segmented">
          <button
            className={kind === "local" ? "selected" : ""}
            onClick={() => changeKind("local")}
          >
            <HardDrive size={17} />
            本地目录
          </button>
          <button
            className={kind === "p115" ? "selected" : ""}
            onClick={() => changeKind("p115")}
          >
            <Cloud size={17} />
            115 云盘
          </button>
        </div>
        {kind === "p115" && !connected ? (
          <p className="error-banner">请先关闭窗口并连接 115 账号。</p>
        ) : (
          <>
            <label className="field">
              名称
              <input
                placeholder="例如：旅行片段"
                value={name}
                onChange={(e) => setName(e.target.value)}
                maxLength={120}
              />
            </label>
            <div className="directory-browser">
              <header>
                <button
                  aria-label="上级目录"
                  className="icon-button"
                  disabled={stack.length === 1}
                  onClick={() => setStack((v) => v.slice(0, -1))}
                >
                  <ArrowLeft size={17} />
                </button>
                <span>{stack.map((s) => s.name).join(" / ")}</span>
              </header>
              {loading ? (
                <p className="muted-line">正在加载目录…</p>
              ) : error ? (
                <p className="error-banner">{error}</p>
              ) : (
                <div className="directory-items">
                  {entries.length === 0 && (
                    <p className="muted-line">当前目录没有子文件夹</p>
                  )}
                  {entries.map((d) => (
                    <button
                      key={d.key}
                      onClick={() =>
                        setStack((v) => [...v, { key: d.key, name: d.name }])
                      }
                    >
                      <Folder size={18} />
                      {d.name}
                      <span>→</span>
                    </button>
                  ))}
                  {next && (
                    <button
                      onClick={async () => {
                        try {
                          const p = await api<DirectoryPage>(
                            `/directories?kind=${kind}&key=${encodeURIComponent(key)}&cursor=${next}`,
                          );
                          setEntries((v) => [...v, ...p.entries]);
                          setNext(p.next);
                        } catch (e) {
                          notify((e as Error).message);
                        }
                      }}
                    >
                      加载更多目录
                    </button>
                  )}
                </div>
              )}
            </div>
            <p className="selected-root">
              <Check size={14} />
              选中当前目录：{root || "—"}
            </p>
            <label className="field">
              扫描间隔（小时，0 表示关闭）
              <input
                type="number"
                min="0"
                max="8760"
                value={interval}
                onChange={(e) => setInterval(Number(e.target.value))}
              />
            </label>
            <button
              className="primary full"
              disabled={!root || !name.trim() || saving}
              onClick={async () => {
                setSaving(true);
                try {
                  await api("/sources", "POST", {
                    name,
                    kind,
                    root,
                    intervalHours: interval,
                  });
                  onSaved();
                } catch (e) {
                  notify((e as Error).message);
                } finally {
                  setSaving(false);
                }
              }}
            >
              {saving ? "正在添加" : "添加选中目录"}
            </button>
          </>
        )}
      </section>
    </div>
  );
}
