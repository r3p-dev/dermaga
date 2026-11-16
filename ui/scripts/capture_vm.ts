import { firefox } from "playwright";
import AxeBuilder from "@axe-core/playwright";

async function main() {
  const browser = await firefox.launch();
  const context = await browser.newContext();
  const page = await context.newPage();
  await page.setViewportSize({ width: 1280, height: 800 });

  console.log("Navigating to http://127.0.0.1:7070/login...");
  await page.goto("http://127.0.0.1:7070/login");
  await page.waitForSelector("#admin-password");

  console.log("Submitting login form...");
  await page.fill("#admin-password", "correct-password-123");
  await page.click('button[type="submit"]');

  console.log("Waiting for dashboard...");
  await page.waitForURL("http://127.0.0.1:7070/");
  await page.waitForSelector("text=Daemon Active");

  console.log("Checking VM dashboard light mode contrast and a11y...");
  const a11yLight = await new AxeBuilder({ page })
    .withTags(["wcag2a", "wcag2aa", "wcag21a", "wcag21aa"])
    .analyze();
  console.log(
    `VM Light Dashboard Axe violations: ${a11yLight.violations.length}`,
  );
  if (a11yLight.violations.length > 0) {
    throw new Error(
      `Axe violations on VM light dashboard: ${JSON.stringify(a11yLight.violations)}`,
    );
  }

  console.log("Capturing VM dashboard light screenshot...");
  await page.screenshot({
    path: "../docs/evidence/M1/brand/panel-vm-brand.png",
  });
  await page.screenshot({
    path: "../docs/evidence/M1/panel-vm-v0.1.4.png",
  });

  // Dark mode
  console.log("Switching VM dashboard to dark mode...");
  await page.click('button[aria-label="Toggle theme"]');
  await page.waitForTimeout(300);

  // Assert dark computed background on VM dashboard using canvas to resolve OKLCH to sRGB
  const bodyBg = await page.evaluate(() => {
    const bg = window.getComputedStyle(document.body).backgroundColor;
    const canvas = document.createElement("canvas");
    canvas.width = 1;
    canvas.height = 1;
    const ctx = canvas.getContext("2d");
    if (!ctx) return bg;
    ctx.fillStyle = bg;
    ctx.fillRect(0, 0, 1, 1);
    const [r, g, b] = ctx.getImageData(0, 0, 1, 1).data;
    return `rgb(${r}, ${g}, ${b})`;
  });
  if (bodyBg !== "rgb(12, 10, 9)") {
    throw new Error(
      `Expected dark body background rgb(12, 10, 9), got ${bodyBg}`,
    );
  }

  // Axe accessibility scan on VM dashboard in dark mode
  const a11y = await new AxeBuilder({ page })
    .withTags(["wcag2a", "wcag2aa", "wcag21a", "wcag21aa"])
    .analyze();
  console.log(`VM Dark Dashboard Axe violations: ${a11y.violations.length}`);
  if (a11y.violations.length > 0) {
    throw new Error(
      `Axe violations on VM dark dashboard: ${JSON.stringify(a11y.violations)}`,
    );
  }

  console.log("Capturing VM dashboard dark screenshot...");
  await page.screenshot({
    path: "../docs/evidence/M1/brand/panel-vm-brand-dark.png",
  });

  console.log("Captured VM screenshots successfully!");
  await browser.close();
}

main().catch((err) => {
  console.error("Error capturing VM screenshots:", err);
  process.exit(1);
});
