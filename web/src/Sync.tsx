import { useEffect, useState, useCallback } from "react";
import {
  Plus,
  RefreshCw,
  Folder,
  ArrowLeft,
  ChevronRight,
  X,
  CloudUpload,
} from "lucide-react";
import { api, dateLabel, sizeLabel, type Source } from "./api";
type Task = {
  id: number;
  name: string;
  sourceId: number;
  target: string;
  intervalHours: number;
  enabled: boolean;
  lastRun: number;
};
type Run = {
  id: number;
  taskId: number;
  status: string;
  uploaded: number;
  skipped: number;
  failed: number;
  currentFile: string;
  message: string;
  startedAt: number;
  finishedAt: number;
};
type RecordItem = {
  id: number;
  path: string;
  status: string;
  message: string;
  size: number;
};
const labels: Record<string, string> = {
  running: "同步中",
  cooling: "等待重试 / 排队",
  completed: "已完成",
  partial: "部分失败",
  failed: "失败",
  canceled: "已取消",
  interrupted: "已中断",
  uploaded: "上传成功",
  uploading: "上传中 / 排队",
  unconfirmed: "结果待核对",
  skipped: "同名跳过",
};
export default function Sync({
  sources,
  notify,
}: {
  sources: Source[];
  notify: (s: string) => void;
}) {
  const [tasks, setTasks] = useState<Task[]>([]),
    [runs, setRuns] = useState<Run[]>([]),
    [show, setShow] = useState(false),
    [selected, setSelected] = useState<number | null>(null),
    [records, setRecords] = useState<RecordItem[]>([]),
    [more, setMore] = useState(false),
    [busy, setBusy] = useState(false),
    [error, setError] = useState("");
  const refresh = useCallback(async () => {
    try {
      const [t, r] = await Promise.all([
        api<Task[]>("/sync/tasks"),
        api<Run[]>("/sync/runs"),
      ]);
      setTasks(t);
      setRuns((v) => [
        ...r,
        ...v.filter((old) => r.length > 0 && old.id < r[r.length - 1].id),
      ]);
      setError("");
    } catch (e) {
      setError((e as Error).message);
    }
  }, []);
  useEffect(() => {
    void refresh();
    const timer = setInterval(() => void refresh(), 3000);
    return () => clearInterval(timer);
  }, [refresh]);
  const action = async (path: string, body?: unknown, method = "POST") => {
    setBusy(true);
    try {
      await api(path, method, body);
      await refresh();
    } catch (e) {
      notify((e as Error).message);
    } finally {
      setBusy(false);
    }
  };
  const loadRecords = async (id: number, after = 0) => {
    try {
      const data = await api<RecordItem[]>(
        `/sync/runs/${id}/records?after=${after}`,
      );
      setSelected(id);
      setRecords((v) => (after ? [...v, ...data] : data));
      setMore(data.length === 100);
    } catch (e) {
      notify((e as Error).message);
    }
  };
  return (
    <section className="sync-page">
      <header className="browse-header">
        <div>
          <h1>同步</h1>
          <p>本地 → 115 云盘</p>
        </div>
        <button className="primary" onClick={() => setShow(true)}>
          <Plus size={18} />
          新增同步任务
        </button>
      </header>
      <p className="sync-rule">
        串行上传，文件间隔 3 秒；失败最多尝试 3 次，限流冷却 10 分钟。同名项跳过，保留目录结构和本地文件。
      </p>
      {error && <p className="error-banner">{error}</p>}
      <div className="sync-tasks">
        {tasks.map((t) => (
          <article key={t.id} className="sync-task">
            <CloudUpload size={25} />
            <div>
              <h3>{t.name}</h3>
              <p>
                {sources.find((s) => s.id === t.sourceId)?.name || "本地目录"}{" "}
                <ChevronRight size={13} />
                115 · {t.target}
              </p>
              <small>
                {t.enabled ? `每 ${t.intervalHours} 小时同步` : "定时已关闭"} ·
                上次启动 {dateLabel(t.lastRun)}
              </small>
            </div>
            <div className="sync-task-actions">
              <button
                className="secondary"
                disabled={busy}
                onClick={() =>
                  void action(
                    `/sync/tasks/${t.id}`,
                    { enabled: !t.enabled, intervalHours: t.intervalHours },
                    "PATCH",
                  )
                }
              >
                {t.enabled ? "关闭定时" : "开启定时"}
              </button>
              <button
                className="primary"
                disabled={
                  busy ||
                  runs.some((r) => ["running", "cooling"].includes(r.status))
                }
                onClick={() => void action(`/sync/tasks/${t.id}/run`)}
              >
                <RefreshCw size={15} />
                立即同步
              </button>
            </div>
          </article>
        ))}
        {tasks.length === 0 && (
          <div className="sync-empty">
            <CloudUpload size={36} />
            <h2>创建第一个同步任务</h2>
            <p>选择本地媒体目录和云端目标，按设定的周期自动复制文件。</p>
          </div>
        )}
      </div>
      <div className="sync-section-heading">
        <h2>运行记录</h2>
        <button className="secondary" onClick={() => void refresh()}>
          刷新
        </button>
      </div>
      <div className="sync-runs">
        {runs.map((r) => (
          <article key={r.id}>
            <div>
              <strong>
                {tasks.find((t) => t.id === r.taskId)?.name || "同步任务"}
              </strong>
              <span className={"sync-status " + r.status}>
                {labels[r.status] || r.status}
              </span>
              <small>{dateLabel(r.startedAt)}</small>
            </div>
            <p>
              成功 {r.uploaded} · 跳过 {r.skipped} · 失败 / 待核对 {r.failed}
            </p>
            {r.currentFile && (
              <p className="sync-current">正在处理：{r.currentFile}</p>
            )}
            <p>{r.message}</p>
            <div>
              <button
                className="secondary"
                onClick={() => void loadRecords(r.id)}
              >
                查看明细
              </button>
              {["failed", "partial", "canceled", "interrupted"].includes(r.status) && (
                <button className="primary" disabled={busy || runs.some(v => ["running", "cooling"].includes(v.status))}
                  onClick={() => void action(`/sync/tasks/${r.taskId}/run`)}>
                  重新核对并继续
                </button>
              )}
              {["running", "cooling"].includes(r.status) && (
                <button
                  className="secondary"
                  disabled={busy}
                  onClick={() => void action(`/sync/runs/${r.id}/cancel`)}
                >
                  取消同步
                </button>
              )}
            </div>
          </article>
        ))}
        {!runs.length && <p className="muted">还没有运行记录</p>}
      </div>
      {runs.length >= 50 && (
        <button
          className="secondary"
          onClick={async () => {
            try {
              const data = await api<Run[]>(
                "/sync/runs?before=" + runs[runs.length - 1].id,
              );
              setRuns((v) => [...v, ...data]);
            } catch (e) {
              notify((e as Error).message);
            }
          }}
        >
          更早的运行记录
        </button>
      )}
      {selected !== null && (
        <div className="modal-backdrop">
          <section
            className="modal sync-modal"
            role="dialog"
            aria-modal="true"
            aria-label="同步明细"
          >
            <header>
              <h2>同步明细 · #{selected}</h2>
              <button
                className="icon-button"
                aria-label="关闭明细"
                onClick={() => setSelected(null)}
              >
                <X />
              </button>
            </header>
            <p className="sync-rule">成功为已确认结果；同名跳过不代表内容相同。失败或待核对可返回运行记录，点击“重新核对并继续”；新一轮会核对已有项并处理剩余文件。</p>
            <div className="sync-records">
              {records.map((r) => (
                <div key={r.id}>
                  <strong>{r.path}</strong>
                  <span className={"sync-status " + r.status}>
                    {labels[r.status]}
                  </span>
                  <small>
                    {sizeLabel(r.size)} · {r.message}
                  </small>
                </div>
              ))}
              {!records.length && <p>暂无文件记录；目录读取失败时请查看运行状态</p>}
            </div>
            <footer>
              <button
                className="secondary"
                onClick={() => void loadRecords(selected)}
              >
                刷新明细
              </button>
              {more && (
                <button
                  className="secondary"
                  onClick={() =>
                    void loadRecords(selected, records[records.length - 1].id)
                  }
                >
                  加载更多
                </button>
              )}
            </footer>
          </section>
        </div>
      )}
      {show && (
        <CreateTask
          sources={sources}
          onClose={() => setShow(false)}
          onCreated={() => {
            setShow(false);
            void refresh();
          }}
          notify={notify}
        />
      )}
    </section>
  );
}
function CreateTask({
  sources,
  onClose,
  onCreated,
  notify,
}: {
  sources: Source[];
  onClose: () => void;
  onCreated: () => void;
  notify: (s: string) => void;
}) {
  const [name, setName] = useState("本地视频备份"),
    [sourceId, setSource] = useState(""),
    [interval, setInterval] = useState(6),
    [enabled, setEnabled] = useState(true),
    [saving, setSaving] = useState(false);
  const [path, setPath] = useState<{ key: string; name: string }[]>([
      { key: "0", name: "115 根目录" },
    ]),
    [entries, setEntries] = useState<
      { key: string; name: string; dir: boolean }[]
    >([]),
    [next, setNext] = useState(""),
    [loading, setLoading] = useState(false),
    [error, setError] = useState("");
  const target = path[path.length - 1];
  useEffect(() => {
    const c = new AbortController();
    setLoading(true);
    setError("");
    setEntries([]);
    setNext("");
    api<{ entries: typeof entries; next: string }>(
      "/directories?kind=p115&key=" + target.key,
      "GET",
      undefined,
      c.signal,
    )
      .then((d) => {
        setEntries(d.entries.filter((e) => e.dir));
        setNext(d.next);
      })
      .catch((e) => {
        if (!c.signal.aborted) setError(e.message);
      })
      .finally(() => {
        if (!c.signal.aborted) setLoading(false);
      });
    return () => c.abort();
  }, [target.key]);
  return (
    <div className="modal-backdrop">
      <form
        className="modal sync-modal"
        role="dialog"
        aria-modal="true"
        aria-label="新增同步任务"
        onSubmit={async (e) => {
          e.preventDefault();
          setSaving(true);
          try {
            await api("/sync/tasks", "POST", {
              name,
              sourceId: Number(sourceId),
              target: target.key,
              intervalHours: interval,
              enabled,
            });
            onCreated();
          } catch (e) {
            notify((e as Error).message);
          } finally {
            setSaving(false);
          }
        }}
      >
        <header>
          <h2>新增同步任务</h2>
          <button
            type="button"
            className="icon-button"
            onClick={onClose}
            aria-label="关闭同步表单"
          >
            <X />
          </button>
        </header>
        <label>
          任务名称
          <input
            required
            maxLength={100}
            value={name}
            onChange={(e) => setName(e.target.value)}
          />
        </label>
        <label>
          本地目录
          <select
            aria-label="本地目录"
            required
            value={sourceId}
            onChange={(e) => setSource(e.target.value)}
          >
            <option value="">选择本地媒体源</option>
            {sources
              .filter((s) => s.kind === "local")
              .map((s) => (
                <option key={s.id} value={s.id}>
                  {s.name} · {s.root}
                </option>
              ))}
          </select>
        </label>
        <label>115 目标目录</label>
        <div className="sync-dir-path">
          <button
            type="button"
            className="icon-button"
            disabled={path.length === 1 || loading}
            onClick={() => setPath((v) => v.slice(0, -1))}
          >
            <ArrowLeft size={17} />
          </button>
          <span>{path.map((p) => p.name).join(" / ")}</span>
        </div>
        <div className="sync-dir-list">
          {loading ? (
            <p>正在读取目录…</p>
          ) : error ? (
            <p className="error-banner">{error}</p>
          ) : (
            entries.map((d) => (
              <button
                type="button"
                key={d.key}
                onClick={() => setPath((v) => [...v, d])}
              >
                <Folder size={17} />
                {d.name}
                <ChevronRight size={15} />
              </button>
            ))
          )}
          {next && (
            <button
              type="button"
              disabled={loading}
              onClick={async () => {
                setLoading(true);
                try {
                  const d = await api<{
                    entries: typeof entries;
                    next: string;
                  }>(`/directories?kind=p115&key=${target.key}&cursor=${next}`);
                  setEntries((v) => [...v, ...d.entries.filter((e) => e.dir)]);
                  setNext(d.next);
                } catch (e) {
                  setError((e as Error).message);
                } finally {
                  setLoading(false);
                }
              }}
            >
              更多目录
            </button>
          )}
        </div>
        <label>
          同步周期（小时）
          <input
            type="number"
            min={1}
            max={8760}
            required
            value={interval}
            onChange={(e) => setInterval(Number(e.target.value))}
          />
        </label>
        <label className="sync-check">
          <input
            type="checkbox"
            checked={enabled}
            onChange={(e) => setEnabled(e.target.checked)}
          />
          开启定时同步（首次在一个周期后执行）
        </label>
        <p className="sync-rule">
          同步目录内所有普通文件；同名跳过，不覆盖、不删除。创建后可点击“立即同步”。
        </p>
        <footer>
          <button type="button" className="secondary" onClick={onClose}>
            取消
          </button>
          <button
            className="primary"
            disabled={saving || loading || !!error || !sourceId}
          >
            {saving ? "正在保存…" : "创建任务"}
          </button>
        </footer>
      </form>
    </div>
  );
}
