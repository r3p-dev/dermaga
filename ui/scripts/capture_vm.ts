import { firefox } from "playwright";

async function main() {
  const browser = await firefox.launch();
  const page = await browser.newPage();
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

  console.log("Capturing VM dashboard screenshots...");
  await page.screenshot({
    path: "../docs/evidence/M1/brand/panel-vm-brand.png",
  });
  await page.screenshot({
    path: "../docs/evidence/M1/panel-vm-v0.1.3.png",
  });

  // Dark mode
  await page.click('button[aria-label="Toggle theme"]');
  await page.waitForTimeout(300);
  await page.screenshot({
    path: "../docs/evidence/M1/brand/panel-vm-brand-dark.png",
  });

  console.log("Captured successfully!");
  await browser.close();
}

main().catch((err) => {
  console.error("Error capturing VM screenshots:", err);
  process.exit(1);
});
