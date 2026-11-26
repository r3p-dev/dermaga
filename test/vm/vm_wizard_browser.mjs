import { chromium, firefox } from 'playwright';
import { readFileSync } from 'node:fs';
import { execSync } from 'node:child_process';

const BASE = 'http://127.0.0.1:7070';
const password = readFileSync('../tmp/evidence/M6/vm-panel-pass.txt', 'utf8').trim();

function vm(cmd) {
  return execSync(`ssh dermaga-vm ${JSON.stringify(cmd)}`, { encoding: 'utf8', timeout: 60000 }).trim();
}

const browser = await firefox.launch();
const context = await browser.newContext({ viewport: { width: 1280, height: 800 } });
const page = await context.newPage();

try {
  // 1. Login via tunnel
  await page.goto(`${BASE}/login`);
  await page.fill('#admin-password', password);
  await page.click('button[type="submit"]');
  await page.waitForURL(`${BASE}/`);
  console.log('PASS [browser 1]: logged in via SSH tunnel');

  // 2. Open New App Wizard
  await page.click('a:has-text("New app")');
  await page.waitForURL(`${BASE}/apps/new`);
  await page.selectOption('#wiz-repo', 'r3p-dev/dermaga-test-app');
  await page.fill('#wiz-name', 'vmwizard');
  await page.click('button:has-text("Next: Build & Port")');

  // 3. Step 2: Containerfile + port 8080 detected (wait for GitHub API inspect)
  await page.waitForFunction(() => document.querySelector('#wiz-port')?.value === '8080', { timeout: 15000 });
  console.log('PASS [browser 2]: Step 2 detected Containerfile and port 8080');

  // 4. Step 3: Domain DNS check + runtime & build-time env
  await page.click('button:has-text("Next: Env, Health & Domain")');
  await page.fill('#app-domain-input', 'vmwizard.dermaga.test');
  await page.click('button:has-text("Check DNS")');
  await page.waitForSelector('button:has-text("Check DNS"):not([disabled])', { timeout: 15000 });

  await page.click('button:has-text("+ Add variable")');
  await page.fill('#env-key-0', 'VM_RT_SECRET');
  await page.fill('#env-val-0', 'vm_rt_secret_321');
  await page.click('button:has-text("+ Add variable")');
  await page.fill('#env-key-1', 'BUILD_VAR');
  await page.fill('#env-val-1', 'vm_bv_654');
  // Uncheck Runtime and check Build time on row 1
  const rows = page.locator('div.p-3.rounded-lg.border');
  await rows.nth(1).locator('label:has-text("Runtime") input[type="checkbox"]').uncheck();
  await rows.nth(1).locator('label:has-text("Build time") input[type="checkbox"]').check();
  await page.waitForSelector('text=Build-time variables warning:');
  console.log('PASS [browser 3]: Step 3 configured DNS check, runtime env, and build-time env');

  // 5. Step 4: Deploy & watch live build log
  await page.click('button:has-text("Next: Review & Deploy")');
  await page.click('button:has-text("Deploy app")');
  await page.waitForSelector('text=Image built: localhost/dermaga/vmwizard:r1', { timeout: 120000 });
  await page.waitForSelector('text=Deployment is live at', { timeout: 90000 });
  await page.screenshot({ path: '../tmp/evidence/M6/screenshots/m6b-vm-wizard-live.png' });
  console.log('PASS [browser 4]: Step 4 live build log streamed and deployment reached live');

  // 6. Verify Pebble HTTPS on vmwizard.dermaga.test
  let httpsOK = false;
  for (let i = 0; i < 15; i++) {
    try {
      const out = vm('curl -fsS --cacert ~/.local/share/dermaga/traefik/pebble-minica.pem --resolve vmwizard.dermaga.test:443:127.0.0.1 https://vmwizard.dermaga.test/health');
      if (out.includes('healthy')) {
        httpsOK = true;
        break;
      }
    } catch {}
    await new Promise((r) => setTimeout(r, 1000));
  }
  if (!httpsOK) throw new Error('HTTPS Pebble cert check failed for vmwizard.dermaga.test');
  console.log('PASS [browser 5]: vmwizard.dermaga.test answered healthy over Pebble TLS');

  // 7. Open App Page -> Environment reveal audited
  await page.click('a:has-text("Open App Page")');
  await page.waitForSelector('button[role="tab"]:has-text("Environment")');
  await page.click('button[role="tab"]:has-text("Environment")');
  await page.waitForSelector('text=••••••••••••');
  await page.locator('div.p-3:has-text("VM_RT_SECRET") button:has-text("Reveal")').click();
  await page.waitForSelector('text=vm_rt_secret_321');
  console.log('PASS [browser 6]: Environment reveal showed value');

  // 8. Logs tab (runtime logs SSE)
  await page.click('button[role="tab"]:has-text("Logs")');
  await page.waitForSelector('text=GET /health HTTP/1.1', { timeout: 15000 });
  const logText = await page.locator('pre').first().textContent();
  if (logText.includes('vm_rt_secret_321') || logText.includes('vm_bv_654')) {
    throw new Error('Secret leaked in runtime logs UI!');
  }
  console.log('PASS [browser 7]: Runtime logs streamed in UI with 0 secret leaks');

  // 9. Danger zone typed delete
  await page.click('button[role="tab"]:has-text("Danger zone")');
  await page.fill('#confirm-delete', 'vmwizard');
  await page.click('button:has-text("Remove app")');
  await page.waitForURL(`${BASE}/`);
  console.log('PASS [browser 8]: App deleted with typed confirmation');
} finally {
  await browser.close();
}
