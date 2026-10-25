import { test, expect } from "@playwright/test";

test("navigation, light DOM and responsive workspace", async ({ page }) => {
  const errors: string[] = [];
  page.on("pageerror", (error) => errors.push(error.message));
  await page.goto("/");
  await expect(
    page.getByRole("heading", { name: "Dashboard", exact: true }),
  ).toBeVisible();
  for (const [label, path] of [
    ["Scheduled tasks", "/scheduled"],
    ["Condition tasks", "/conditions"],
    ["Executions", "/executions"],
    ["Risk records", "/records"],
    ["Cluster monitor", "/monitor"],
  ]) {
    await page.getByRole("link", { name: label, exact: true }).click();
    await expect(page).toHaveURL(new RegExp(`${path}$`));
    await expect(
      page.getByRole("heading", { name: label, exact: true }),
    ).toBeVisible();
  }
  expect(
    await page.locator("app-shell").evaluate((element) => element.shadowRoot),
  ).toBeNull();
  await page.setViewportSize({ width: 390, height: 844 });
  await page.getByRole("link", { name: "Dashboard", exact: true }).click();
  await expect(
    page.getByRole("heading", { name: "Dashboard", exact: true }),
  ).toBeVisible();
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= window.innerWidth,
    ),
  ).toBeTruthy();
  expect(errors).toEqual([]);
});

test("unsafe record stays text and produces a durable condition execution", async ({
  page,
}) => {
  await page.goto("/records");
  const title = `Browser audit ${Date.now()}`;
  await page.getByLabel("Title", { exact: true }).fill(title);
  await page
    .getByLabel("Content", { exact: true })
    .fill(
      '<img src=x onerror="window.__xss=1"><script>window.__xss=1</script>',
    );
  const response = page.waitForResponse(
    (response) =>
      response.url().endsWith("/api/v1/risk-records") &&
      response.request().method() === "POST",
  );
  await page
    .getByRole("button", { name: "Insert record", exact: true })
    .click();
  const result = await response;
  expect(result.ok()).toBeTruthy();
  const payload = await result.json();
  expect(payload.data.event_delivery).toBe("durable");
  await page.reload();
  await expect(page.getByText(title, { exact: true })).toBeVisible();
  expect(
    await page.evaluate(() => Reflect.get(window, "__xss")),
  ).toBeUndefined();
  await expect
    .poll(
      async () => {
        const response = await page.request.get(
          "/api/v1/executions?task_type=condition&page_size=200",
        );
        const executions = (await response.json()).data.items;
        return executions.some((execution: { trigger_info: string }) =>
          execution.trigger_info.includes(title),
        );
      },
      { timeout: 15000 },
    )
    .toBeTruthy();
});

test("task editor persists changes and a repeated manual key creates one run", async ({
  page,
}) => {
  await page.goto("/scheduled");
  await page.getByRole("button", { name: "New task", exact: true }).click();
  const dialog = page.getByRole("dialog", { name: "Task editor" });
  const name = `Browser schedule ${Date.now()}`;
  await dialog.getByLabel("Task name", { exact: true }).fill(name);
  await dialog
    .getByLabel("Prompt", { exact: true })
    .fill(
      "Count database tables using mysql_tool, then write the required report sections.",
    );
  await dialog.getByLabel("Enabled", { exact: true }).uncheck();
  const created = page.waitForResponse(
    (response) =>
      response.url().endsWith("/api/v1/scheduled-tasks") &&
      response.request().method() === "POST",
  );
  await dialog.getByRole("button", { name: "Create", exact: true }).click();
  const response = await created;
  expect(response.ok()).toBeTruthy();
  const task = (await response.json()).data;
  try {
    await expect(dialog).not.toBeVisible();
    const row = page.getByRole("row").filter({ hasText: name });
    await expect(row).toBeVisible();
    await row.getByRole("button", { name: "Edit", exact: true }).click();
    await dialog
      .getByLabel("Description", { exact: true })
      .fill("Updated in the browser");
    await dialog.getByRole("button", { name: "Save", exact: true }).click();
    await expect(row).toContainText("Updated in the browser");
    await row.getByRole("button", { name: "Edit", exact: true }).click();
    await dialog.getByLabel("Description", { exact: true }).fill("");
    await dialog.getByRole("button", { name: "Save", exact: true }).click();
    await expect(row).not.toContainText("Updated in the browser");
    const responses = await Promise.all(
      Array.from({ length: 16 }, () =>
        page.request.post(`/api/v1/scheduled-tasks/${task.id}/trigger`, {
          headers: { "Idempotency-Key": name },
        }),
      ),
    );
    const ids = await Promise.all(
      responses.map(async (response) => {
        expect(response.ok()).toBeTruthy();
        return (await response.json()).data.execution_id;
      }),
    );
    expect(new Set(ids).size).toBe(1);
    const executions = await page.request.get(
      `/api/v1/executions?task_type=manual&task_id=${task.id}`,
    );
    expect((await executions.json()).data.total).toBe(1);
  } finally {
    await page.request.delete(`/api/v1/scheduled-tasks/${task.id}`);
  }
});
