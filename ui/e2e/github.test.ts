import { test, expect, type Page } from "@playwright/test";
import AxeBuilder from "@axe-core/playwright";

async function evidenceShot(page: Page, name: string): Promise<void> {
  const milestone = process.env.EVIDENCE_MILESTONE;
  if (!milestone) return;
  await page.screenshot({
    path: `../tmp/evidence/${milestone}/screenshots/${name}`,
  });
}

async function assertAxeAA(page: Page, contextName: string) {
  const results = await new AxeBuilder({ page })
    .withTags(["wcag2a", "wcag2aa", "wcag21a", "wcag21aa"])
    .analyze();
  if (results.violations.length > 0) {
    const summary = results.violations
      .map(
        (v) =>
          `${v.id} (${v.impact}): ${v.description}\n` +
          v.nodes.map((n) => `  - ${n.html}: ${n.failureSummary}`).join("\n"),
      )
      .join("\n\n");
    throw new Error(`WCAG AA violations on ${contextName}:\n${summary}`);
  }
}

test.describe("Settings -> GitHub (M5a: GHA-1, GHA-3, GHA-6, GHA-8)", () => {
  test("hook domain blocker, guided connect card, connected repos desktop+narrow light+dark, and typed disconnect", async ({
    page,
  }) => {
    await page.setViewportSize({ width: 1280, height: 800 });
    await page.goto("/login");
    await page.fill("#admin-password", "correct-password-123");
    await page.click('button[type="submit"]');
    await expect(page).toHaveURL(/\/$/);

    // 1. Settings -> GitHub without hook domain: blocked with guidance
    await page.goto("/settings?tab=github");
    await expect(page.locator("text=Hook domain required first")).toBeVisible();
    await assertAxeAA(page, "Settings GitHub blocked (light, desktop)");

    // Switch to Hook Domain tab via the blocker button
    await page.click('button:has-text("Configure Hook Domain")');
    await expect(page.locator("#hook-domain-input")).toBeVisible();

    // 2. Mock hook domain set and test guided Connect GitHub card (light desktop)
    await page.route("**/api/settings/github", async (route) => {
      if (route.request().method() === "GET") {
        await route.fulfill({
          status: 200,
          contentType: "application/json",
          body: JSON.stringify({
            connected: false,
            hook_domain: "hooks.example.com",
            installations: [],
            repositories: [],
          }),
        });
        return;
      }
      await route.continue();
    });

    await page.click('button[role="tab"]:has-text("GitHub")');
    await expect(
      page.locator("text=What happens next on GitHub:"),
    ).toBeVisible();
    await expect(
      page.locator('button:has-text("Connect GitHub")'),
    ).toBeVisible();
    await assertAxeAA(page, "Settings GitHub guided card (light, desktop)");
    await evidenceShot(page, "m5a-github-connect-light-desktop.png");

    // 3. Connected state with installations, repositories, and last delivery
    await page.unroute("**/api/settings/github");
    await page.route("**/api/settings/github", async (route) => {
      if (route.request().method() === "GET") {
        await route.fulfill({
          status: 200,
          contentType: "application/json",
          body: JSON.stringify({
            connected: true,
            hook_domain: "hooks.example.com",
            app: {
              id: 12345,
              slug: "dermaga-vm",
              name: "Dermaga VM",
              html_url: "https://github.com/apps/dermaga-vm",
              install_url:
                "https://github.com/apps/dermaga-vm/installations/new",
            },
            installations: [
              {
                id: 99,
                account_login: "r3p-dev",
                account_type: "User",
                repository_selection: "selected",
                html_url: "https://github.com/settings/installations/99",
              },
            ],
            repositories: [
              {
                id: 1,
                installation_id: 99,
                full_name: "r3p-dev/dermaga-test-app",
                owner: "r3p-dev",
                name: "dermaga-test-app",
                private: true,
                default_branch: "main",
                html_url: "https://github.com/r3p-dev/dermaga-test-app",
              },
            ],
            last_delivery: null,
          }),
        });
        return;
      }
      if (route.request().method() === "DELETE") {
        await route.fulfill({
          status: 200,
          contentType: "application/json",
          body: JSON.stringify({
            success: true,
            delete_app_url: "https://github.com/apps/dermaga-vm/advanced",
          }),
        });
        return;
      }
      await route.continue();
    });

    await page.reload();
    await expect(page.locator("text=Dermaga VM")).toBeVisible();
    await expect(
      page.locator("text=r3p-dev/dermaga-test-app").first(),
    ).toBeVisible();
    await expect(
      page.locator("text=No webhook deliveries received yet"),
    ).toBeVisible();
    await assertAxeAA(page, "Settings GitHub connected (light, desktop)");
    await evidenceShot(page, "m5a-github-connected-light-desktop.png");

    // 4. Dark mode + narrow viewport (375x812) stacked repository rows
    await page.click('button[aria-label="Toggle theme"]');
    await page.setViewportSize({ width: 375, height: 812 });
    const hasOverflow = await page.evaluate(
      () => document.documentElement.scrollWidth > window.innerWidth,
    );
    expect(hasOverflow).toBe(false);
    await assertAxeAA(page, "Settings GitHub connected (dark, narrow)");
    await evidenceShot(page, "m5a-github-connected-dark-narrow.png");

    // 5. Typed-confirmation Disconnect
    const discBtn = page.locator('button:has-text("Disconnect GitHub")');
    await expect(discBtn).toBeDisabled();
    await page.fill("#confirm-disconnect", "disconnect");
    await expect(discBtn).toBeEnabled();
  });
});
