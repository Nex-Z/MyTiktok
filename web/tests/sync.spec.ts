import { test, expect } from "@playwright/test";

test("sync recovery shows uncertain results and starts reconciliation", async ({ page }) => {
  let resumed = false;
  await page.route("**/api/sync/tasks", route => route.fulfill({ json: [{ id: 77, name: "批量备份", sourceId: 1, target: "0", intervalHours: 6, enabled: false, lastRun: 1 }] }));
  await page.route("**/api/sync/runs", route => route.fulfill({ json: [{ id: 88, taskId: 77, status: resumed ? "running" : "failed", uploaded: 200, skipped: 2, failed: 1, currentFile: "", message: "重试后暂停", startedAt: 1, finishedAt: 2 }] }));
  await page.route("**/api/sync/runs/88/records?*", route => route.fulfill({ json: [
    { id: 1, path: "目录/已完成.mp4", status: "uploaded", size: 4, message: "上传成功" },
    { id: 2, path: "目录/待核对.mp4", status: "unconfirmed", size: 4, message: "响应丢失" },
  ] }));
  await page.route("**/api/sync/tasks/77/run", route => { resumed = true; return route.fulfill({ json: { id: 89 } }); });
  await page.goto("/");
  await page.getByRole("button", { name: "同步", exact: true }).click();
  await expect(page.getByText("成功 200 · 跳过 2 · 失败 / 待核对 1")).toBeVisible();
  await page.getByRole("button", { name: "查看明细" }).click();
  await expect(page.getByText("目录/待核对.mp4", { exact: true })).toBeVisible();
  await expect(page.getByText("结果待核对", { exact: true })).toBeVisible();
  await page.getByRole("button", { name: "关闭明细" }).click();
  await page.getByRole("button", { name: "重新核对并继续" }).click();
  await expect.poll(() => resumed).toBe(true);
  await expect(page.getByRole("button", { name: "取消同步" })).toBeVisible();
});
