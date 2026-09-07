import { test, expect, type APIRequestContext } from "@playwright/test";

async function seed(request: APIRequestContext) {
  const sources = await (await request.get("/api/sources")).json();
  for (const s of sources) await request.delete(`/api/sources/${s.id}`);
  const dirs = await (await request.get("/api/directories?kind=local")).json();
  const created = await request.post("/api/sources", {
    data: {
      name: "浏览测试",
      kind: "local",
      root: dirs.root,
      intervalHours: 0,
    },
  });
  expect(created.ok()).toBe(true);
  const source = await created.json();
  await expect
    .poll(async () => {
      const j = await (await request.get("/api/jobs")).json();
      return j[0]?.status;
    })
    .toBe("completed");
  const feed = await (await request.get("/api/feed?sort=title")).json();
  const ocean = feed.items.find(
    (v: { title: string }) => v.title === "Ocean walk.mp4",
  );
  await request.put(`/api/videos/${ocean.id}/favorite`, {
    data: { value: true },
  });
  await request.put(`/api/videos/${ocean.id}/history`, {
    data: { position: 3 },
  });
  return source;
}
test("search, filters, indexed folders and quick preview", async ({
  page,
  request,
}, info) => {
  const source = await seed(request);
  await page.goto("/");
  await page.getByRole("button", { name: "检索视频" }).click();
  for (const tab of ["首页", "我的收藏", "最近观看"]) {
    if (tab === "我的收藏")
      await page.getByRole("button", { name: "我的", exact: true }).click();
    await page.getByRole("button", { name: tab, exact: true }).click();
    await page.getByRole("textbox", { name: "搜索视频" }).fill("Ocean walk");
    await expect(page.locator(".video-card")).toHaveCount(1);
    if (tab === "首页")
      await page.getByRole("button", { name: "筛选视频" }).click();
    await page.getByLabel("画幅筛选").selectOption("landscape");
    await expect(page.getByText("没有匹配的视频")).toBeVisible();
    await page.getByLabel("画幅筛选").selectOption("portrait");
    await expect(page.locator(".video-card")).toHaveCount(1);
    await page.getByLabel("时长筛选").selectOption("long");
    await expect(page.getByText("没有匹配的视频")).toBeVisible();
    await page.getByLabel("时长筛选").selectOption("short");
    await expect(page.locator(".video-card")).toHaveCount(1);
  }
  await page.getByRole("button", { name: "文件速览", exact: true }).click();
  await page.getByLabel("媒体源筛选").selectOption(String(source.id));
  await expect(page.locator(".folder-grid")).toContainText("旅行");
  await expect(page.locator(".video-card")).toHaveCount(3);
  await page
    .locator(".folder-grid")
    .getByRole("button", { name: /旅行/ })
    .click();
  await page
    .locator(".folder-grid")
    .getByRole("button", { name: /第二天/ })
    .click();
  await expect(page.locator(".video-card")).toHaveCount(1);
  await expect(page.locator(".breadcrumbs")).toContainText("第二天");
  await page.getByRole("button", { name: "列表视图" }).click();
  await expect(page.locator(".file-row")).toHaveCount(1);
  await page.locator(".file-row").click();
  if (info.project.name === "desktop") {
    await expect(page.locator(".quick-preview video")).toBeVisible();
    await expect(
      page.locator(".quick-preview .desktop-video-details .video-details"),
    ).toContainText("旅行/第二天");
    await page.screenshot({
      path: "../artifacts/desktop-explorer-v2.png",
      fullPage: true,
    });
    await page.getByRole("button", { name: "进入竖屏播放" }).click();
  } else {
    await expect(
      page.getByRole("dialog", { name: "竖屏刷视频" }),
    ).toBeVisible();
  }
  await expect(
    page.locator(".immersive-view .video-shell video"),
  ).toBeVisible();
  await page.getByRole("button", { name: "退出刷视频" }).click();
  await expect(page.locator(".file-row")).toHaveCount(1);
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= innerWidth,
    ),
  ).toBe(true);
  await page.getByRole("button", { name: "设置", exact: true }).click();
  await request.delete(`/api/sources/${source.id}`);
});
test("long press opens information, movement cancels it, exit stops playback", async ({
  page,
  request,
}, info) => {
  const source = await seed(request);
  await page.goto("/");
  if (await page.getByRole("button", { name: "检索视频" }).isVisible())
    await page.getByRole("button", { name: "检索视频" }).click();
  await page.locator(".video-open").first().click();
  const shell = page.locator(".immersive-view .video-shell");
  const video = shell.locator("video");
  await expect
    .poll(() => video.evaluate((v: HTMLVideoElement) => v.currentTime))
    .toBeGreaterThan(0);
  const first = await video.getAttribute("src");
  // Synthetic touch objects also exercise handlers on the desktop browser project.
  await shell.evaluate((target) => {
    const e = new Event("touchstart", { bubbles: true });
    Object.defineProperty(e, "touches", {
      value: [{ clientX: 150, clientY: 400 }],
    });
    target.dispatchEvent(e);
  });
  const sheet = page.getByRole("dialog", { name: "视频基础信息" });
  await expect(sheet).toBeVisible();
  await expect(video).toHaveAttribute("src", first!);
  await expect
    .poll(() => video.evaluate((v: HTMLVideoElement) => v.paused))
    .toBe(true);
  await expect(sheet).toContainText("分辨率");
  const bounds = await page.locator(".immersive-view").boundingBox();
  expect(bounds?.y).toBe(0);
  expect(bounds?.height).toBe(page.viewportSize()?.height);
  await page.screenshot({
    path: `../artifacts/${info.project.name}-info-v2.png`,
    fullPage: info.project.name === "desktop",
  });
  await shell.evaluate((target) => {
    const e = new Event("touchend", { bubbles: true });
    Object.defineProperty(e, "changedTouches", {
      value: [{ clientX: 150, clientY: 400 }],
    });
    target.dispatchEvent(e);
  });
  await page.getByRole("button", { name: "关闭视频信息" }).click();
  await expect(sheet).not.toBeVisible();
  await expect
    .poll(() => video.evaluate((v: HTMLVideoElement) => v.paused))
    .toBe(false);
  await shell.evaluate((target) => {
    for (const [type, y] of [
      ["touchstart", 500],
      ["touchmove", 200],
    ] as const) {
      const e = new Event(type, { bubbles: true });
      Object.defineProperty(e, "touches", {
        value: [{ clientX: 150, clientY: y }],
      });
      target.dispatchEvent(e);
    }
  });
  await page.waitForTimeout(650);
  await expect(sheet).not.toBeVisible();
  await shell.evaluate((target) => {
    const e = new Event("touchend", { bubbles: true });
    Object.defineProperty(e, "changedTouches", {
      value: [{ clientX: 150, clientY: 200 }],
    });
    target.dispatchEvent(e);
  });
  await expect(video).not.toHaveAttribute("src", first!);
  await page.getByRole("button", { name: "退出刷视频" }).click();
  await expect(page.locator("video")).toHaveCount(0);
  await request.delete(`/api/sources/${source.id}`);
});

test("Douyin home: independent likes, mute, center pause and fullscreen", async ({
  page,
  request,
}, info) => {
  await seed(request);
  await page.goto("/");
  const video = page.locator(".home-player .video-shell video");
  await expect(video).toBeVisible();
  await expect
    .poll(() => video.evaluate((v: HTMLVideoElement) => v.currentTime))
    .toBeGreaterThan(0);
  await expect(page.locator(".source-badge")).toHaveCount(0);
  await expect(page.locator(".transport button")).toHaveCount(0);
  await page.getByRole("button", { name: "喜欢", exact: true }).click();
  await expect(
    page.getByRole("button", { name: "取消喜欢", exact: true }),
  ).toBeVisible();
  await page.getByRole("button", { name: "开启声音", exact: true }).click();
  await expect(page.locator(".mute-action")).toHaveCount(0);
  await video.click({ position: { x: 100, y: 180 } });
  await expect(page.getByRole("button", { name: "继续播放" })).toBeVisible();
  await expect
    .poll(() => video.evaluate((v: HTMLVideoElement) => v.paused))
    .toBe(true);
  await page.screenshot({
    path: `../artifacts/${info.project.name}-home-v3.png`,
  });
  await page.getByRole("button", { name: "继续播放" }).click();
  await expect
    .poll(() => video.evaluate((v: HTMLVideoElement) => v.paused))
    .toBe(false);
  await page.getByRole("button", { name: "切换全屏" }).click();
  await expect
    .poll(() => page.evaluate(() => !!document.fullscreenElement))
    .toBe(true);
  await page.evaluate(() => document.exitFullscreen());
  await page.getByRole("button", { name: "我的", exact: true }).click();
  await expect(page.locator(".home-player video")).toHaveCount(0);
  await expect(page.locator(".video-card")).toHaveCount(1);
  await expect(page.locator(".sidebar nav")).not.toContainText("最近观看");
});

test("sync task creation, schedule toggle and paginated cloud selection", async ({
  page,
  request,
}, info) => {
  const source = await seed(request);
  await page.route("**/api/directories?kind=p115*", (route) => {
    const url = new URL(route.request().url());
    return route.fulfill({
      json: {
        entries: url.searchParams.get("cursor")
          ? [{ key: "22", name: "备份目标", dir: true }]
          : [{ key: "11", name: "其他目录", dir: true }],
        next: url.searchParams.get("cursor") ? "" : "256",
      },
    });
  });
  await page.goto("/");
  await page.getByRole("button", { name: "同步", exact: true }).click();
  await page.getByRole("button", { name: "新增同步任务" }).click();
  await page
    .getByLabel("本地目录", { exact: true })
    .selectOption(String(source.id));
  await page.getByRole("button", { name: "更多目录" }).click();
  await page.getByRole("button", { name: "备份目标" }).click();
  await page.getByLabel("开启定时同步", { exact: false }).uncheck();
  await page.getByRole("button", { name: "创建任务", exact: true }).click();
  await expect(page.getByRole("dialog")).toHaveCount(0);
  await expect(page.locator(".sync-task")).toContainText("定时已关闭");
  const tasks = await (await request.get("/api/sync/tasks")).json();
  expect(tasks[0].target).toBe("22");
  expect(tasks[0].enabled).toBe(false);
  await page.getByRole("button", { name: "开启定时", exact: true }).click();
  await expect(page.locator(".sync-task")).toContainText("每 6 小时同步");
  await page.getByRole("button", { name: "关闭定时", exact: true }).click();
  await page.screenshot({
    path: `../artifacts/${info.project.name}-sync-v3.png`,
    fullPage: true,
  });
});

test("mobile controls shrink in portrait and disappear in landscape", async ({
  page,
  request,
}, info) => {
  test.skip(info.project.name !== "mobile", "Touch layout");
  await seed(request);
  await page.goto("/");
  const player = page.locator(".home-player .player-layout");
  const video = player.locator(".video-shell video");
  await expect(video).toBeVisible();
  await expect(player).toHaveClass(/controls-hidden/, { timeout: 6000 });
  await expect(player.locator(".video-bottom h2")).toBeHidden();
  const bottom = await player.locator(".video-bottom").boundingBox();
  expect(bottom!.height).toBeLessThanOrEqual(40);
  await page.screenshot({ path: "../artifacts/mobile-compact-idle.png" });
  await page.setViewportSize({ width: 915, height: 412 });
  await expect(player).toHaveClass(/touch-wide/);
  expect((await player.locator(".stage").boundingBox())!.width).toBe(915);
  await expect(player).toHaveClass(/controls-hidden/, { timeout: 6000 });
  await expect(player.locator(".player-actions")).toBeHidden();
  await expect(player.locator(".video-bottom")).toBeHidden();
  await expect
    .poll(() => video.evaluate((v: HTMLVideoElement) => v.paused))
    .toBe(false);
  await page.screenshot({ path: "../artifacts/mobile-wide-idle.png" });
  await video.tap({ position: { x: 400, y: 160 } });
  await expect(player).toHaveClass(/controls-visible/);
  await expect
    .poll(() => video.evaluate((v: HTMLVideoElement) => v.paused))
    .toBe(false);
  await expect(player.locator(".player-actions")).toBeVisible();
  await video.tap({ position: { x: 400, y: 160 } });
  await expect(page.getByRole("button", { name: "继续播放" })).toBeVisible();
  await page.waitForTimeout(2600);
  await expect(player).toHaveClass(/controls-visible/);
  await page.getByRole("button", { name: "继续播放" }).click();
  const seek = player.getByLabel("播放进度");
  await seek.dispatchEvent("pointerdown", { pointerId: 1 });
  await page.waitForTimeout(2600);
  await expect(player).toHaveClass(/controls-visible/);
  await seek.fill("4");
  await seek.dispatchEvent("pointerup", { pointerId: 1 });
  await expect(player).toHaveClass(/controls-hidden/, { timeout: 6000 });
  await video.tap({ position: { x: 400, y: 160 } });
  await page.waitForTimeout(220);
  await page.screenshot({ path: "../artifacts/mobile-wide-controls.png" });
});

test("audio, static image and pinch zoom reset", async ({
  page,
  request,
}, info) => {
  const source = await seed(request);
  const dirs = await (await request.get("/api/directories?kind=local")).json();
  const { execFileSync } = await import("node:child_process");
  const fs = await import("node:fs");
  const path = await import("node:path");
  const audio = path.join(dirs.root, "e2e-tone.wav"),
    picture = path.join(dirs.root, "e2e-still.jpg");
  try {
    execFileSync(
      "ffmpeg",
      [
        "-v",
        "error",
        "-f",
        "lavfi",
        "-i",
        "sine=frequency=440",
        "-t",
        "4",
        audio,
      ],
      { windowsHide: true },
    );
    execFileSync(
      "ffmpeg",
      [
        "-v",
        "error",
        "-f",
        "lavfi",
        "-i",
        "color=c=orange:s=600x400",
        "-frames:v",
        "1",
        picture,
      ],
      { windowsHide: true },
    );
    await request.post(`/api/sources/${source.id}/scan`);
    await expect
      .poll(async () => {
        const jobs = await (await request.get("/api/jobs")).json();
        return jobs[0]?.status;
      })
      .toBe("completed");
    await page.goto("/");
    await page.getByRole("button", { name: "检索视频", exact: true }).click();
    await page.getByRole("button", { name: "筛选视频" }).click();
    await page.getByLabel("媒体类型", { exact: true }).selectOption("audio");
    await expect(page.locator(".video-card")).toHaveCount(1);
    await page.locator(".video-open").click();
    const el = page.locator("audio.audio-element");
    await expect(el).toHaveCount(1);
    if (
      await page
        .getByRole("button", { name: "点击播放", exact: true })
        .isVisible()
    )
      await page.getByRole("button", { name: "点击播放", exact: true }).click();
    await expect
      .poll(() => el.evaluate((v: HTMLAudioElement) => v.currentTime))
      .toBeGreaterThan(0);
    await page.getByRole("button", { name: "退出刷视频" }).click();
    await expect(page.locator("audio")).toHaveCount(0);
    await page.getByLabel("媒体类型", { exact: true }).selectOption("image");
    await expect(page.locator(".video-card")).toHaveCount(1);
    await page.locator(".video-open").click();
    await expect(page.locator(".media-picture")).toBeVisible();
    await expect(page.getByLabel("播放进度")).toHaveCount(0);
    const shell = page.locator(".immersive-view .video-shell");
    await shell.evaluate((target) => {
      for (const [type, points] of [
        [
          "touchstart",
          [
            [120, 160],
            [220, 160],
          ],
        ],
        [
          "touchmove",
          [
            [70, 160],
            [270, 160],
          ],
        ],
        ["touchend", []],
      ] as const) {
        const e = new Event(type, { bubbles: true });
        Object.defineProperty(e, "touches", {
          value: points.map(([clientX, clientY]) => ({ clientX, clientY })),
        });
        Object.defineProperty(e, "changedTouches", { value: [] });
        target.dispatchEvent(e);
      }
    });
    await expect(page.getByRole("button", { name: "恢复缩放" })).toBeVisible();
    await expect(page.locator(".media-picture")).toHaveAttribute(
      "style",
      /scale\(2\)/,
    );
    await page.getByRole("button", { name: "恢复缩放" }).click();
    await expect(page.locator(".media-picture")).toHaveAttribute(
      "style",
      /scale\(1\)/,
    );
    await page.screenshot({
      path: `../artifacts/${info.project.name}-mixed-media.png`,
    });
    await page.getByRole("button", { name: "退出刷视频" }).click();
  } finally {
    for (const file of [audio, picture])
      if (fs.existsSync(file)) fs.unlinkSync(file);
    await request.delete(`/api/sources/${source.id}`);
  }
});
