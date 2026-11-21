type Result = {saved: number; message: string; items: {name: string; error?: string}[]};
export type BatchProgress = {percent: number; index: number; sent: boolean};

export function uploadBatch(directory: string, files: File[], progress: (p: BatchProgress) => void) {
  // Keep file blobs intact. The byte offsets map XHR progress to the correct filename.
  const boundary = "mytiktok-" + Array.from(crypto.getRandomValues(new Uint32Array(4))).join("-");
  const parts: BlobPart[] = [];
  const ends: number[] = [];
  let offset = 0;
  for (const file of files) {
    const name = file.name.replace(/["\\]/g, "\\$&").replace(/\r/g, "%0D").replace(/\n/g, "%0A");
    const header = new Blob([`--${boundary}\r\nContent-Disposition: form-data; name="files"; filename="${name}"\r\nContent-Type: application/octet-stream\r\n\r\n`]);
    parts.push(header, file, "\r\n");
    offset += header.size + file.size + 2;
    ends.push(offset);
  }
  parts.push(`--${boundary}--\r\n`);
  const body = new Blob(parts);
  return new Promise<Result>((resolve, reject) => {
    const xhr = new XMLHttpRequest();
    xhr.open("POST", "/api/local/batch?" + new URLSearchParams({key: directory}));
    xhr.setRequestHeader("Content-Type", "multipart/form-data; boundary=" + boundary);
    xhr.responseType = "json";
    let index = 0;
    xhr.upload.onprogress = ({loaded}) => {
      while (index < ends.length - 1 && loaded >= ends[index]) index++;
      progress({percent: Math.floor(100 * loaded / body.size), index, sent: loaded >= body.size});
    };
    xhr.upload.onload = () => progress({percent: 100, index: files.length - 1, sent: true});
    xhr.onload = () => {
      if (xhr.status >= 200 && xhr.status < 300 && Array.isArray(xhr.response?.items)) resolve(xhr.response);
      else reject(new Error(xhr.response?.error?.message || "上传失败 (" + xhr.status + ")"));
    };
    xhr.onerror = () => reject(new Error("连接中断，请检查已上传的文件后重试。"));
    xhr.send(body);
  });
}
