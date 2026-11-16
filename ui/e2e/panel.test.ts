import { test, expect } from "@playwright/test";

test.describe("Dermaga Panel Journeys (M1)", () => {
  test("unauthenticated access redirects to /login", async ({ page }) => {
    await page.goto("/");
    await expect(page).toHaveURL(/.*\/login/);
    await expect(page.locator("h1")).toHaveText("Admin Login");
  });

  test("login journey with failure, success, and evidence capture", async ({
    page,
  }) => {
    // 1. Unauthenticated Login page
    await page.setViewportSize({ width: 1280, height: 800 });
    await page.goto("/login");
    await expect(page.locator("h1")).toHaveText("Admin Login");
    await page.screenshot({ path: "../docs/evidence/M1/login-desktop.png" });

    await page.setViewportSize({ width: 375, height: 667 });
    await page.screenshot({ path: "../docs/evidence/M1/login-narrow.png" });

    // 2. Submit invalid password
    await page.setViewportSize({ width: 1280, height: 800 });
    await page.fill("#admin-password", "wrong-password-test");
    await page.click('button[type="submit"]');

    // Error banner must appear
    const alert = page.locator('div[role="alert"]');
    await expect(alert).toBeVisible();
    await expect(alert).toContainText("Invalid password");

    // 3. Submit valid password
    await page.fill("#admin-password", "correct-password-123");
    await page.click('button[type="submit"]');

    // Should navigate to dashboard
    await expect(page).toHaveURL("http://127.0.0.1:7070/");
    await expect(page.locator("h1")).toHaveText("Server Overview");
    await expect(page.locator("text=Daemon Active")).toBeVisible();

    // Capture dashboard evidence
    await page.screenshot({
      path: "../docs/evidence/M1/dashboard-desktop.png",
    });
    await page.setViewportSize({ width: 375, height: 667 });
    await page.screenshot({ path: "../docs/evidence/M1/dashboard-narrow.png" });

    // 4. Navigate to Audit Log
    await page.setViewportSize({ width: 1280, height: 800 });
    await page.click('a:has-text("Audit Log")');
    await expect(page).toHaveURL("http://127.0.0.1:7070/audit-logs");
    await expect(page.locator("h1")).toHaveText("Audit Log");
    await expect(page.locator("table")).toBeVisible();

    // Check filters: origin, app, and time (AUD-2)
    const filter = page.locator("#origin-filter");
    await expect(filter).toBeVisible();
    await filter.selectOption("panel");

    const appFilter = page.locator("#app-filter");
    await expect(appFilter).toBeVisible();

    const timeFilter = page.locator("#time-filter");
    await expect(timeFilter).toBeVisible();
    await timeFilter.selectOption("1h");

    // Capture audit log evidence
    await page.screenshot({ path: "../docs/evidence/M1/audit-desktop.png" });
    await page.setViewportSize({ width: 375, height: 667 });
    await page.screenshot({ path: "../docs/evidence/M1/audit-narrow.png" });

    // 5. Logout
    await page.setViewportSize({ width: 1280, height: 800 });
    await page.click('button:has-text("Log out")');
    await expect(page).toHaveURL(/.*\/login/);
  });
});
