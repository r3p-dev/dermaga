import { test, expect, type Page } from "@playwright/test";
import AxeBuilder from "@axe-core/playwright";

// Evidence screenshots are written only when EVIDENCE_MILESTONE is set (AGENTS.md "Evidence"),
// to tmp/evidence/<milestone>/screenshots/; a plain test run writes none.
async function evidenceShot(page: Page, name: string): Promise<void> {
  const milestone = process.env.EVIDENCE_MILESTONE;
  if (!milestone) return;
  await page.screenshot({
    path: `../tmp/evidence/${milestone}/screenshots/${name}`,
  });
}

async function getComputedRgb(page: Page, selector: string): Promise<string> {
  return await page.evaluate((sel) => {
    const el = sel === "body" ? document.body : document.querySelector(sel);
    if (!el) return "";
    const bg = window.getComputedStyle(el).backgroundColor;
    const canvas = document.createElement("canvas");
    canvas.width = 1;
    canvas.height = 1;
    const ctx = canvas.getContext("2d");
    if (!ctx) return bg;
    ctx.fillStyle = bg;
    ctx.fillRect(0, 0, 1, 1);
    const [r, g, b] = ctx.getImageData(0, 0, 1, 1).data;
    return `rgb(${r}, ${g}, ${b})`;
  }, selector);
}

async function assertThemeBackgrounds(
  page: Page,
  expectedTheme: "light" | "dark",
) {
  const expectedBodyBg =
    expectedTheme === "dark" ? "rgb(12, 10, 9)" : "rgb(250, 250, 249)";
  const expectedSurfaceBg =
    expectedTheme === "dark" ? "rgb(28, 25, 23)" : "rgb(255, 255, 255)";

  const bodyBg = await getComputedRgb(page, "body");
  expect(bodyBg).toBe(expectedBodyBg);

  const navSelector = (await page.locator("aside").first().isVisible())
    ? "aside"
    : "header";
  const navBg = await getComputedRgb(page, navSelector);
  expect(navBg).toBe(expectedSurfaceBg);

  const card = page.locator(".bg-white, .dark\\:bg-stone-900").first();
  if (await card.isVisible()) {
    const cardBg = await page.evaluate(
      (el) => {
        const bg = window.getComputedStyle(el).backgroundColor;
        const canvas = document.createElement("canvas");
        canvas.width = 1;
        canvas.height = 1;
        const ctx = canvas.getContext("2d");
        if (!ctx) return bg;
        ctx.fillStyle = bg;
        ctx.fillRect(0, 0, 1, 1);
        const [r, g, b] = ctx.getImageData(0, 0, 1, 1).data;
        return `rgb(${r}, ${g}, ${b})`;
      },
      await card.elementHandle(),
    );
    expect(cardBg).toBe(expectedSurfaceBg);
  }
}

async function assertAxeAA(page: Page, contextName: string) {
  const results = await new AxeBuilder({ page })
    .withTags(["wcag2a", "wcag2aa", "wcag21a", "wcag21aa"])
    .analyze();

  if (results.violations.length > 0) {
    const violationSummary = results.violations
      .map(
        (v) =>
          `${v.id} (${v.impact}): ${v.description}\n` +
          v.nodes.map((n) => `  - ${n.html}: ${n.failureSummary}`).join("\n"),
      )
      .join("\n\n");
    throw new Error(
      `WCAG AA accessibility/contrast violations on ${contextName}:\n${violationSummary}`,
    );
  }
}

test.describe("Dermaga Theme & Panel Journeys (M1-dark)", () => {
  test("system preference prefers-color-scheme: dark produces fully dark page without saved choice", async ({
    browser,
  }) => {
    const context = await browser.newContext({
      colorScheme: "dark",
      viewport: { width: 1280, height: 800 },
    });
    const page = await context.newPage();
    await page.goto("/login");

    await expect(page.locator("html")).toHaveClass(/dark/);
    await assertThemeBackgrounds(page, "dark");
    await assertAxeAA(
      page,
      "Login page on first visit with dark OS preference",
    );
    await context.close();
  });

  test("unauthenticated access redirects to /login", async ({ page }) => {
    await page.goto("/");
    await expect(page).toHaveURL(/.*\/login/);
    await expect(page.locator("h1")).toHaveText("Admin Login");
  });

  test("full panel journeys, theme switching, computed backgrounds, and axe AA contrast", async ({
    page,
  }) => {
    // 1. Unauthenticated Login page (Light Theme by default)
    await page.setViewportSize({ width: 1280, height: 800 });
    await page.goto("/login");
    await expect(page.locator("h1")).toHaveText("Admin Login");
    await expect(page.locator('header img[alt="dermaga"]')).toBeVisible();
    await expect(page.locator('img[alt="dermaga logo"]')).toBeVisible();

    // Verify Light computed backgrounds and Axe AA
    await assertThemeBackgrounds(page, "light");
    await assertAxeAA(page, "Login Desktop Light");
    await evidenceShot(page, "login-desktop-light.png");

    await page.setViewportSize({ width: 375, height: 667 });
    await assertAxeAA(page, "Login Narrow Light");
    await evidenceShot(page, "login-narrow-light.png");

    // Toggle to Dark Theme
    await page.setViewportSize({ width: 1280, height: 800 });
    await page.locator('button[aria-label*="Toggle theme"]:visible').click();
    await expect(page.locator("html")).toHaveClass(/dark/);

    // Verify Dark computed backgrounds and Axe AA
    await assertThemeBackgrounds(page, "dark");
    await assertAxeAA(page, "Login Desktop Dark");
    await evidenceShot(page, "login-desktop-dark.png");

    await page.setViewportSize({ width: 375, height: 667 });
    await assertAxeAA(page, "Login Narrow Dark");
    await evidenceShot(page, "login-narrow-dark.png");

    // Verify saved theme persistence on reload
    await page.reload();
    await expect(page.locator("html")).toHaveClass(/dark/);
    await assertThemeBackgrounds(page, "dark");

    // 2. Submit invalid password
    await page.setViewportSize({ width: 1280, height: 800 });
    await page.fill("#admin-password", "wrong-password-test");
    await page.click('button[type="submit"]');

    const alert = page.locator('div[role="alert"]');
    await expect(alert).toBeVisible();
    await expect(alert).toContainText("Invalid password");
    await assertAxeAA(page, "Login Alert Dark");

    // 3. Submit valid password
    await page.fill("#admin-password", "correct-password-123");
    await page.click('button[type="submit"]');

    // Should navigate to dashboard (retaining Dark Theme from localStorage)
    await expect(page).toHaveURL("http://127.0.0.1:7070/");
    await expect(page.locator("h1")).toHaveText("Server Overview");
    await expect(page.locator("text=Daemon Active")).toBeVisible();
    await expect(page.locator('aside img[alt="dermaga"]')).toBeVisible();

    // Verify Dashboard Dark Theme
    await assertThemeBackgrounds(page, "dark");
    await assertAxeAA(page, "Dashboard Desktop Dark");
    await evidenceShot(page, "dashboard-desktop-dark.png");

    await page.setViewportSize({ width: 375, height: 667 });
    await assertAxeAA(page, "Dashboard Narrow Dark");
    await evidenceShot(page, "dashboard-narrow-dark.png");

    // Switch Dashboard to Light Theme
    await page.setViewportSize({ width: 1280, height: 800 });
    await page.locator('button[aria-label*="Toggle theme"]:visible').click();
    await expect(page.locator("html")).not.toHaveClass(/dark/);
    await assertThemeBackgrounds(page, "light");
    await assertAxeAA(page, "Dashboard Desktop Light");
    await evidenceShot(page, "dashboard-desktop-light.png");

    await page.setViewportSize({ width: 375, height: 667 });
    await assertAxeAA(page, "Dashboard Narrow Light");
    await evidenceShot(page, "dashboard-narrow-light.png");

    // 4. Navigate to Audit Log
    await page.setViewportSize({ width: 1280, height: 800 });
    await page.locator('a:has-text("Audit Log"):visible').click();
    await expect(page).toHaveURL("http://127.0.0.1:7070/audit-logs");
    await expect(page.locator("h1")).toHaveText("Audit Log");
    await expect(page.locator("table")).toBeVisible();

    // Verify Light Audit Log
    await assertThemeBackgrounds(page, "light");
    await assertAxeAA(page, "Audit Log Desktop Light");
    await evidenceShot(page, "audit-desktop-light.png");

    await page.setViewportSize({ width: 375, height: 667 });
    await assertAxeAA(page, "Audit Log Narrow Light");
    await evidenceShot(page, "audit-narrow-light.png");

    // Check filters
    const filter = page.locator("#origin-filter");
    await expect(filter).toBeVisible();
    await filter.selectOption("panel");

    const appFilter = page.locator("#app-filter");
    await expect(appFilter).toBeVisible();

    const timeFilter = page.locator("#time-filter");
    await expect(timeFilter).toBeVisible();
    await timeFilter.selectOption("1h");

    // Switch Audit Log to Dark Theme
    await page.setViewportSize({ width: 1280, height: 800 });
    await page.locator('button[aria-label*="Toggle theme"]:visible').click();
    await expect(page.locator("html")).toHaveClass(/dark/);
    await assertThemeBackgrounds(page, "dark");
    await assertAxeAA(page, "Audit Log Desktop Dark");
    await evidenceShot(page, "audit-desktop-dark.png");

    await page.setViewportSize({ width: 375, height: 667 });
    await assertAxeAA(page, "Audit Log Narrow Dark");
    await evidenceShot(page, "audit-narrow-dark.png");

    // 5. Logout
    await page.setViewportSize({ width: 1280, height: 800 });
    await page.locator('button:has-text("Log out"):visible').click();
    await expect(page).toHaveURL(/.*\/login/);
  });
});
