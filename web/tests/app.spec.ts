import { test, expect } from "@playwright/test";
test("local library: scan, play, favorite, history, hide and restore", async ({
  page,
  request,
}, info) => {
  const old = await (await request.get("/api/sources")).json();
  for (const s of old) await request.delete(`/api/sources/${s.id}`);
  const jsErrors: string[] = [];
  page.on("pageerror", (e) => jsErrors.push(e.message));
  await page.goto("/");
  await expect(page.getByText("把喜欢的视频，放在一起")).toBeVisible();
  await page.screenshot({
    path: `../artifacts/${info.project.name}-empty.png`,
    fullPage: true,
  });
  await page.getByRole("button", { name: "添加媒体源", exact: true }).click();
  await page.getByRole("button", { name: "添加媒体源", exact: true }).click();
  await page.getByPlaceholder("例如：旅行片段").fill("测试视频");
  await expect(
    page.getByRole("button", { name: "添加选中目录" }),
  ).toBeEnabled();
  await page.getByRole("button", { name: "添加选中目录" }).click();
  await expect(page.getByRole("dialog")).toHaveCount(0);
  await page.getByRole("button", { name: "扫描", exact: true }).click();
  await expect
    .poll(async () => {
      const jobs = await (await request.get("/api/jobs")).json();
      return jobs[0]?.status;
    })
    .toBe("completed");
  await page.screenshot({
    path: `../artifacts/${info.project.name}-admin.png`,
    fullPage: true,
  });
  await page.getByRole("button", { name: "首页", exact: true }).click();
  await page.getByRole("button", { name: "检索视频" }).click();
  await expect(page.locator(".video-card")).toHaveCount(4);
  await page.screenshot({
    path: `../artifacts/${info.project.name}-library-v2.png`,
    fullPage: true,
  });
  await page.locator(".video-open").first().click();
  const video = page.locator(".video-shell video");
  await expect(video).toBeVisible();
  await expect
    .poll(() => video.evaluate((v: HTMLVideoElement) => v.currentTime))
    .toBeGreaterThan(0);
  await page.getByRole("button", { name: "收藏", exact: true }).click();
  await expect(
    page.getByRole("button", { name: "取消收藏", exact: true }),
  ).toBeVisible();
  await page.locator('input[aria-label="播放进度"]').fill("3");
  await page.screenshot({
    path: `../artifacts/${info.project.name}-player.png`,
    fullPage: true,
  });
  const before = await video.getAttribute("src");
  if (info.project.name === "desktop") {
    await page.locator(".immersive-header > span").click();
    await expect(page.locator(".desktop-video-details")).toBeVisible();
    await page.keyboard.press("ArrowDown");
    await expect(video).not.toHaveAttribute("src", before!);
  } else {
    await page.locator(".video-shell").evaluate((target) => {
      const first = new Touch({
        identifier: 1,
        target,
        clientY: 500,
        clientX: 150,
      });
      target.dispatchEvent(
        new TouchEvent("touchstart", { bubbles: true, touches: [first] }),
      );
      const last = new Touch({
        identifier: 1,
        target,
        clientY: 200,
        clientX: 150,
      });
      target.dispatchEvent(
        new TouchEvent("touchend", { bubbles: true, changedTouches: [last] }),
      );
    });
    await expect(video).not.toHaveAttribute("src", before!);
  }
  await expect
    .poll(() =>
      page
        .locator("video")
        .evaluateAll(
          (vs) => vs.filter((v) => !(v as HTMLVideoElement).paused).length,
        ),
    )
    .toBeLessThan(2);
  await page.getByRole("button", { name: "退出刷视频" }).click();
  await page.getByRole("button", { name: "我的", exact: true }).click();
  await page.getByRole("button", { name: "我的收藏", exact: true }).click();
  await expect(page.locator(".video-row")).toHaveCount(1);
  await page.getByRole("button", { name: "最近观看", exact: true }).click();
  await expect(page.locator(".video-row").first()).toBeVisible();
  await page.locator(".video-row").first().click();
  await expect(video).toBeVisible();
  await page.getByRole("button", { name: "视频信息" }).click();
  await page.getByRole("button", { name: "隐藏视频" }).click();
  await page.getByRole("button", { name: "退出刷视频" }).click();
  await page.getByRole("button", { name: "设置", exact: true }).click();
  await page.getByRole("button", { name: "查看隐藏" }).click();
  await expect(
    page.getByRole("button", { name: "恢复", exact: true }),
  ).toBeVisible();
  await page.getByRole("button", { name: "恢复", exact: true }).click();
  await expect(page.getByText("没有隐藏的视频")).toBeVisible();
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= innerWidth,
    ),
  ).toBe(true);
  expect(jsErrors).toEqual([]);
  const sources = await (await request.get("/api/sources")).json();
  for (const s of sources) await request.delete(`/api/sources/${s.id}`);
});

test("115 player retries direct URL then falls back to NAS proxy", async ({
  page,
}) => {
  const attempts: string[] = [];
  await page.route("**/api/feed?*", (route) =>
    route.fulfill({
      json: {
        items: [
          {
            id: 99,
            title: "云端测试",
            kind: "p115",
            duration: 8,
            position: 0,
            cover: "",
            favorite: false,
          },
        ],
        next: "",
        total: 1,
      },
    }),
  );
  await page.route("**/api/videos/99/play?*", (route) => {
    attempts.push(route.request().url());
    return route.abort("failed");
  });
  await page.goto("/");

  await expect(
    page.getByText("暂时无法播放。请检查媒体源连接或视频编码。"),
  ).toBeVisible();
  expect(attempts.some((v) => v.includes("refresh=1"))).toBe(true);
  expect(attempts.some((v) => v.includes("proxy=1"))).toBe(true);
  expect(attempts.length).toBeLessThanOrEqual(4);
});
