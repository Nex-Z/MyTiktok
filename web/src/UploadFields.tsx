import { useRef, useState } from "react";
import { api } from "./api";

import { uploadBatch, type BatchProgress } from "./uploadBatch";

type Props = {
  directory: string;
  ready: boolean;
  setSaving: (busy: boolean) => void;
  enter: (name: string) => void;
};

export default function UploadFields({directory, ready, setSaving, enter}: Props) {
  const [folder, setFolder] = useState("");
  const [files, setFiles] = useState<File[]>([]);
  const [status, setStatus] = useState("");
  const [progress, setProgress] = useState<BatchProgress | null>(null);
  const [dragging, setDragging] = useState(false);
  const active = useRef(false);
  const input = useRef<HTMLInputElement>(null);
  async function send(batch: File[]) {
    if (!ready || active.current || !batch.length) return;
    active.current = true;
    setFiles(batch);
    setSaving(true);
    setStatus("");
    setProgress({percent: 0, index: 0, sent: false});
    try {
      const result = await uploadBatch(directory, batch, setProgress);
      const failures = result.items.filter(item => item.error).map(item => item.name + "：" + item.error);
      setStatus("已上传 " + result.saved + "/" + batch.length + " 个文件。" + result.message + failures.join("；"));
      setFiles([]);
      if (input.current) input.current.value = "";
    } catch (e) { setStatus((e as Error).message); }
    finally {
      active.current = false;
      setProgress(null);
      setSaving(false);
    }
  }
  return <>
    <form className="field" onSubmit={async event => {
      event.preventDefault();
      const name = folder.trim();
      setSaving(true);
      setStatus("正在创建目录…");
      try {
        await api("/local/directories?" + new URLSearchParams({key: directory, name}), "POST");
        enter(name);
        setFolder("");
        setStatus("");
      } catch (e) { setStatus((e as Error).message); }
      finally { setSaving(false); }
    }}>
      <label htmlFor="upload-folder">新建子目录</label>
      <input id="upload-folder" value={folder} onChange={e => setFolder(e.target.value)} />
      <button className="secondary" disabled={!ready || !folder.trim()}>创建并进入</button>
    </form>
    <div className="field">
      <label className={"upload-drop" + (dragging ? " dragging" : "")}
        onDragOver={e => {
          e.preventDefault();
          const available = ready && !e.currentTarget.closest("fieldset:disabled");
          e.dataTransfer.dropEffect = available ? "copy" : "none";
          setDragging(available);
        }}
        onDragLeave={e => { if (!e.currentTarget.contains(e.relatedTarget as Node | null)) setDragging(false); }}
        onDrop={e => {
          e.preventDefault();
          setDragging(false);
          if (!e.currentTarget.closest("fieldset:disabled")) void send(Array.from(e.dataTransfer.files));
        }}>
        拖入一个或多个音视频文件，即可上传到当前目录
        <input ref={input} type="file" multiple accept="audio/*,video/*" aria-label="选择音视频文件"
          onChange={e => setFiles(Array.from(e.target.files ?? []))} />
      </label>
      {progress ? <div className="upload-progress">
        <span>{progress.sent ? "文件已发送，正在等待上传结果…" :
          "正在上传 " + (progress.index + 1) + "/" + files.length + " · " + files[progress.index]?.name}</span>
        <progress aria-label="整批上传进度" max={100} value={progress.percent} />
        <strong>{progress.percent}%</strong>
      </div> : <button type="button" className="primary full" disabled={!ready || !files.length}
        onClick={() => void send(files)}>{files.length ? "上传 " + files.length + " 个文件" : "上传到当前目录"}</button>}
    </div>
    <p role="status">{status}</p>
  </>;
}
