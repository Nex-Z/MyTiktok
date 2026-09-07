import { spawn, execFileSync } from "node:child_process";
import { mkdirSync, mkdtempSync, copyFileSync } from "node:fs";
import path from "node:path";
const root = path.resolve("..");
const artifacts = path.join(root, "artifacts");
mkdirSync(artifacts, { recursive: true });
const dir = mkdtempSync(path.join(artifacts, "e2e-"));
const media = path.join(dir, "media");
mkdirSync(media);
for (const [name, color] of [
  ["Ocean walk", "0x185565"],
  ["Quiet afternoon", "0x755845"],
  ["Evening light", "0x47496b"],
]) {
  execFileSync(
    "ffmpeg",
    [
      "-nostdin",
      "-v",
      "error",
      "-f",
      "lavfi",
      "-i",
      `color=c=${color}:s=${name === "Quiet afternoon" ? "960x540" : "540x960"}:r=24`,
      "-f",
      "lavfi",
      "-i",
      "sine=frequency=220:sample_rate=44100",
      "-t",
      "8",
      "-c:v",
      "libx264",
      "-pix_fmt",
      "yuv420p",
      "-c:a",
      "aac",
      "-movflags",
      "+faststart",
      path.join(media, name + ".mp4"),
    ],
    { windowsHide: true },
  );
}
mkdirSync(path.join(media, "旅行", "第二天"), { recursive: true });
copyFileSync(
  path.join(media, "Ocean walk.mp4"),
  path.join(media, "旅行", "第二天", "Nested ocean.mp4"),
);
const bin = path.join(
  dir,
  process.platform === "win32" ? "mytiktok.exe" : "mytiktok",
);
execFileSync("go", ["build", "-o", bin, "./cmd/mytiktok"], {
  cwd: root,
  windowsHide: true,
});
const server = spawn(bin, [], {
  cwd: root,
  windowsHide: true,
  stdio: "inherit",
  env: {
    ...process.env,
    LISTEN_ADDR: "127.0.0.1:18080",
    MEDIA_ROOT: media,
    DATA_DIR: path.join(dir, "data"),
    WEB_DIR: path.join(root, "web/dist"),
  },
});
const stop = () => {
  server.kill();
  process.exit();
};
process.on("SIGTERM", stop);
process.on("SIGINT", stop);
server.on("exit", (code) => process.exit(code ?? 0));
