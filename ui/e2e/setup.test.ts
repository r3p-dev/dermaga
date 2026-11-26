import { test, expect, type Page } from "@playwright/test";
import AxeBuilder from "@axe-core/playwright";
import { spawn, execFileSync, type ChildProcess } from "node:child_process";
import { createHmac } from "node:crypto";
import { mkdtempSync, mkdirSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";

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

function base32Decode(input: string): Buffer {
  const alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZ234567";
  const clean = input.toUpperCase().replace(/=+$/, "");
  let bits = 0;
  let value = 0;
  const out: number[] = [];
  for (const ch of clean) {
    const idx = alphabet.indexOf(ch);
    if (idx === -1) throw new Error(`Invalid base32 char: ${ch}`);
    value = (value << 5) | idx;
    bits += 5;
    if (bits >= 8) {
      out.push((value >>> (bits - 8)) & 0xff);
      bits -= 8;
    }
  }
  return Buffer.from(out);
}

function computeTOTP(secret: string, stepOffset = 0): string {
  const key = base32Decode(secret);
  const step = Math.floor(Date.now() / 1000 / 30) + stepOffset;
  const buf = Buffer.alloc(8);
  buf.writeBigUInt64BE(BigInt(step));
  const digest = createHmac("sha1", key).update(buf).digest();
  const offset = digest[digest.length - 1] & 0x0f;
  const code =
    ((digest[offset] & 0x7f) << 24) |
    (digest[offset + 1] << 16) |
    (digest[offset + 2] << 8) |
    digest[offset + 3];
  return String(code % 1_000_000).padStart(6, "0");
}

interface FreshDaemon {
  baseURL: string;
  env: Record<string, string>;
  stop: () => void;
}

async function startFreshDaemon(port: number): Promise<FreshDaemon> {
  const root = mkdtempSync(join(tmpdir(), "dermaga-setup-e2e-"));
  const dataDir = join(root, "data");
  const cfgDir = join(root, "config");
  const runDir = join(root, "run");
  mkdirSync(dataDir, { recursive: true });
  mkdirSync(cfgDir, { recursive: true });
  mkdirSync(runDir, { recursive: true });

  const env: Record<string, string> = {
    ...process.env,
    XDG_DATA_HOME: dataDir,
    XDG_CONFIG_HOME: cfgDir,
    XDG_RUNTIME_DIR: runDir,
  };

  execFileSync(
    "../bin/dermaga",
    ["install-settings", "--ssh-address", "192.0.2.10", "--ssh-port", "4222"],
    { env },
  );

  const baseURL = `http://127.0.0.1:${port}`;
  const proc: ChildProcess = spawn(
    "../bin/dermaga",
    ["daemon", "--addr", `127.0.0.1:${port}`],
    { env, stdio: "ignore" },
  );

  // Wait until /api/auth/session responds
  for (let i = 0; i < 40; i++) {
    try {
      const res = await fetch(`${baseURL}/api/auth/session`);
      if (res.ok) break;
    } catch {
      // retry
    }
    await new Promise((r) => setTimeout(r, 100));
  }

  return {
    baseURL,
    env,
    stop: () => {
      proc.kill("SIGTERM");
      rmSync(root, { recursive: true, force: true });
    },
  };
}

function issueSetupToken(env: Record<string, string>): string {
  const out = execFileSync("../bin/dermaga", ["panel"], {
    env,
    encoding: "utf8",
  });
  const m = out.match(/setup\?token=([A-Za-z0-9_-]+)/);
  if (!m) {
    throw new Error(`No setup token found in dermaga panel output:\n${out}`);
  }
  return m[1];
}

test.describe("First-Run Setup Journey (M4a: SETUP-1, SETUP-2, SETUP-3, SEC-8)", () => {
  test("full setup with TOTP enrolment, theme/viewport axe AA checks, and TOTP login", async ({
    page,
  }) => {
    const daemon = await startFreshDaemon(17071);
    try {
      const token = issueSetupToken(daemon.env);

      // 1. Visiting / redirects to /setup (SETUP-1)
      await page.setViewportSize({ width: 1280, height: 800 });
      await page.goto(`${daemon.baseURL}/`);
      await expect(page).toHaveURL(/\/setup$/);
      await expect(page.locator("h1")).toHaveText("First-Run Setup");
      await expect(page.locator("text=dermaga panel")).toBeVisible();
      await assertAxeAA(page, "Setup token prompt (light, desktop)");
      await evidenceShot(page, "m4a-setup-token-light-desktop.png");

      // 2. Open link with ?token=... -> strips token from URL and shows Step 1: Password
      await page.goto(`${daemon.baseURL}/setup?token=${token}`);
      await expect(page.locator("#setup-password")).toBeVisible();
      expect(page.url()).not.toContain("token=");
      await assertAxeAA(page, "Setup password step (light, desktop)");

      await page.fill("#setup-password", "Correct-Horse-Battery-99");
      await expect(page.locator("text=Strength: Strong")).toBeVisible();
      await page.fill("#setup-password-confirm", "Correct-Horse-Battery-99");
      await evidenceShot(page, "m4a-setup-password-light-desktop.png");
      await page.click('button[type="submit"]');

      // 3. Step 2: TOTP Enrolment (QR SVG + secret + otpauth URI)
      await expect(page.locator("#setup-totp-code")).toBeVisible();
      await expect(
        page.locator('svg[aria-label="TOTP QR code"]'),
      ).toBeVisible();
      await assertAxeAA(page, "Setup TOTP step (light, desktop)");
      await evidenceShot(page, "m4a-setup-totp-light-desktop.png");

      // Narrow + dark mode check on TOTP step
      await page.setViewportSize({ width: 375, height: 812 });
      await page.locator('button[aria-label*="Toggle theme"]:visible').click();
      await expect(page.locator("html")).toHaveClass(/dark/);
      await assertAxeAA(page, "Setup TOTP step (dark, narrow)");
      await evidenceShot(page, "m4a-setup-totp-dark-narrow.png");

      // Read displayed secret, compute valid 6-digit TOTP code, and verify
      const secret = (
        await page.locator("code.select-all").first().innerText()
      ).trim();
      const code = computeTOTP(secret, 0);
      await page.fill("#setup-totp-code", code);
      await page.click('button:has-text("Verify & Complete Setup")');

      // 3b. Step 3: Hook Domain (skip to dashboard)
      await page.setViewportSize({ width: 1280, height: 800 });
      await expect(page.locator("#hook-domain-input")).toBeVisible();
      await assertAxeAA(page, "Setup hook domain step (light, desktop)");
      await evidenceShot(page, "m4b-setup-hook-domain-light-desktop.png");
      await page.click('button:has-text("Skip for now")');

      // 4. Lands on authenticated Dashboard with hook-domain pending notice (no TOTP warning)
      await expect(page).toHaveURL(`${daemon.baseURL}/`);
      await expect(page.locator("h1")).toHaveText("Server Overview");
      await expect(
        page.locator(
          "text=Setup incomplete: hook domain is not configured yet",
        ),
      ).toBeVisible();
      await expect(
        page.locator("text=Two-factor authentication is disabled"),
      ).toHaveCount(0);

      // 5. Log out and sign back in with password + TOTP
      await page.locator('button:has-text("Log out"):visible').click();
      await expect(page).toHaveURL(/\/login$/);
      await page.fill("#admin-password", "Correct-Horse-Battery-99");
      await page.click('button[type="submit"]');
      await expect(page.locator("#totp-code")).toBeVisible();
      await evidenceShot(page, "m4a-login-totp-dark-desktop.png");

      const nextCode = computeTOTP(secret, 1);
      await page.fill("#totp-code", nextCode);
      await page.click('button[type="submit"]');
      await expect(page).toHaveURL(`${daemon.baseURL}/`);
      await expect(page.locator("h1")).toHaveText("Server Overview");
    } finally {
      daemon.stop();
    }
  });

  test("skipping TOTP shows persistent warning banner in panel", async ({
    page,
  }) => {
    const daemon = await startFreshDaemon(17072);
    try {
      const token = issueSetupToken(daemon.env);

      await page.setViewportSize({ width: 1280, height: 800 });
      await page.goto(`${daemon.baseURL}/setup`);
      await page.fill("#setup-token", token);
      await page.click('button[type="submit"]');

      await expect(page.locator("#setup-password")).toBeVisible();
      await page.fill("#setup-password", "correct-horse-battery-99");
      await page.fill("#setup-password-confirm", "correct-horse-battery-99");
      await page.click('button[type="submit"]');

      await expect(page.locator("#setup-totp-code")).toBeVisible();
      await page.click('button:has-text("Skip for now")');

      await expect(page.locator("#hook-domain-input")).toBeVisible();
      await page.click('button:has-text("Skip for now")');

      await expect(page).toHaveURL(`${daemon.baseURL}/`);
      await expect(page.locator("h1")).toHaveText("Server Overview");
      await expect(
        page.locator("text=Two-factor authentication is disabled"),
      ).toBeVisible();
      await assertAxeAA(page, "Dashboard with TOTP disabled warning (light)");
      await evidenceShot(page, "m4a-dashboard-totp-warning-light.png");
    } finally {
      daemon.stop();
    }
  });
});
