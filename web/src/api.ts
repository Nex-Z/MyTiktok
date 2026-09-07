export type Video = {
  mediaType?: "video" | "audio" | "image";
  sourceName: string;
  folder: string;
  fileKey: string;
  modifiedAt: number;
  id: number;
  sourceId: number;
  kind: string;
  title: string;
  size: number;
  duration: number;
  width: number;
  height: number;
  cover: string;
  indexedAt: number;
  available: boolean;
  hidden: boolean;
  favorite: boolean;
  liked: boolean;
  position: number;
};
export type Source = {
  id: number;
  name: string;
  kind: string;
  root: string;
  intervalHours: number;
  lastScan: number;
};
export type Job = {
  id: number;
  sourceId: number;
  status: string;
  scanned: number;
  errors: number;
  message: string;
  startedAt: number;
  finishedAt: number;
};
export type Feed = { items: Video[]; next: string; total: number };
export async function api<T>(
  path: string,
  method = "GET",
  body?: unknown,
  signal?: AbortSignal,
): Promise<T> {
  const r = await fetch("/api" + path, {
    method,
    headers: body === undefined ? {} : { "Content-Type": "application/json" },
    body: body === undefined ? undefined : JSON.stringify(body),
    signal,
  });
  if (!r.ok) {
    const b = await r.json().catch(() => null);
    throw new Error(b?.error?.message || `请求失败 (${r.status})`);
  }
  return r.json();
}
export const time = (seconds: number) =>
  `${Math.floor(seconds / 60)}:${String(Math.floor(seconds % 60)).padStart(2, "0")}`;
export const sizeLabel = (bytes: number) =>
  bytes < 1024
    ? `${bytes} B`
    : bytes < 1048576
      ? `${(bytes / 1024).toFixed(1)} KB`
      : bytes >= 1073741824
        ? `${(bytes / 1073741824).toFixed(2)} GB`
        : `${(bytes / 1048576).toFixed(1)} MB`;
export const dateLabel = (seconds: number) =>
  seconds > 0
    ? new Date(seconds * 1000).toLocaleString("zh-CN", { hour12: false })
    : "未知";
