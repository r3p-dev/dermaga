// UI smoke test of a freshly installed dermaga-vm through an SSH tunnel (run from ui/: bun ../test/vm/smoke_ui.mjs M7).
// First-run setup with the token from `dermaga panel` (never printed), then the Dashboard at 1280/1920/375 in
// light and dark: full-width main, server overview values equal the API, no requirement codes, no footer.
import { firefox } from 'playwright'
import { execFileSync, spawn } from 'node:child_process'
import { existsSync, mkdirSync, readFileSync, writeFileSync } from 'node:fs'
import { randomBytes } from 'node:crypto'

const milestone = process.argv[2] ?? 'M7'
const BASE = 'http://127.0.0.1:7070'
const shots = `../tmp/evidence/${milestone}/screenshots`
const passFile = `../tmp/evidence/${milestone}/vm-panel-pass.txt`
mkdirSync(shots, { recursive: true })

const vm = (cmd) =>
	execFileSync('ssh', ['dermaga-vm', cmd], { encoding: 'utf8', timeout: 60000 }).trim()

console.log(`date: ${new Date().toISOString()} vm: ${vm('hostname')} binary: ${vm('~/.local/bin/dermaga version')}`)

const code =
	/\b(NFR|UI|DEP|GHA|SEC|AUD|WIZ|APP|LOG|SETUP|ADM|TLS|RUN|DASH|DISK|DOC|HOST|INST|LAP|LOCK|REL|SRV|UPD|MIG)-\d+\b/
if (!code.test('Footprint (NFR-2)')) throw new Error('negative control: code matcher misses NFR-2')
console.log('PASS [control]: code matcher detects "Footprint (NFR-2)"')

const tunnel = spawn('ssh', ['-N', '-o', 'ExitOnForwardFailure=yes', '-L', '7070:127.0.0.1:7070', 'dermaga-vm'], {
	stdio: 'ignore'
})
const browser = await firefox.launch()
let failed = false
const check = (ok, msg) => {
	console.log(`${ok ? 'PASS' : 'FAIL'} ${msg}`)
	if (!ok) failed = true
}

try {
	for (let i = 0; i < 30; i++) {
		try {
			await fetch(BASE)
			break
		} catch {
			await new Promise((r) => setTimeout(r, 500))
		}
	}
	const page = await browser.newPage({ viewport: { width: 1280, height: 900 } })

	// First-run setup (skipped when the VM was already set up by an earlier run).
	await page.goto(`${BASE}/`)
	await page.waitForURL(/\/(setup|login)?$/)
	if (page.url().includes('/setup')) {
		if (!existsSync(passFile)) writeFileSync(passFile, randomBytes(18).toString('base64url') + '\n', { mode: 0o600 })
		const token = vm('~/.local/bin/dermaga panel').match(/setup\?token=([A-Za-z0-9_-]+)/)?.[1]
		if (!token) throw new Error('dermaga panel printed no setup link')
		const password = readFileSync(passFile, 'utf8').trim()
		await page.goto(`${BASE}/setup?token=${token}`)
		await page.fill('#setup-password', password)
		await page.fill('#setup-password-confirm', password)
		await page.click('button[type="submit"]')
		await page.click('button:has-text("Skip for now")') // TOTP
		await page.click('button:has-text("Skip for now")') // hook domain
		await page.waitForURL(`${BASE}/`)
		check(true, '[setup]: first-run link → password → panel (TOTP and hook domain skipped)')
	} else {
		await page.fill('#admin-password', readFileSync(passFile, 'utf8').trim())
		await page.click('button[type="submit"]')
		await page.waitForURL(`${BASE}/`)
		check(true, '[login]: logged in with the stored panel password')
	}

	const status = await (await page.request.get(`${BASE}/api/server/status`)).json()
	for (const theme of ['light', 'dark']) {
		for (const width of [1280, 1920, 375]) {
			await page.setViewportSize({ width, height: 900 })
			await page.goto(`${BASE}/`)
			const dark = await page.locator('html').evaluate((el) => el.classList.contains('dark'))
			if (dark !== (theme === 'dark')) await page.locator('button[aria-label*="Toggle theme"]:visible').first().click()
			const overview = page.getByRole('region', { name: 'Server overview' })
			await overview.getByText('Database: healthy').waitFor()
			const text = await overview.innerText()
			const title = await overview.locator('[title^="Database schema version"]').getAttribute('title')
			const main = await page.locator('main').evaluate((el) => ({
				maxWidth: getComputedStyle(el).maxWidth,
				right: Math.round(el.getBoundingClientRect().right),
				viewport: document.documentElement.clientWidth
			}))
			const body = await page.locator('body').innerText()
			const ctx = `${width} ${theme}`
			check(text.includes(status.version) && status.version === 'v0.6.2', `[overview ${ctx}]: version ${status.version} from the API`)
			check(title === `Database schema version ${status.schema_version}`, `[overview ${ctx}]: tooltip "${title}"`)
			check(main.maxWidth === 'none' && main.right === main.viewport, `[layout ${ctx}]: main max-width ${main.maxWidth}, right ${main.right} of ${main.viewport}`)
			check(!code.test(body) && (await page.locator('footer').count()) === 0, `[text ${ctx}]: no requirement codes, no footer`)
			await page.screenshot({ path: `${shots}/vm-v062-dashboard-${width}-${theme}.png`, fullPage: true })
		}
	}
	for (const path of ['/apps', '/activity', '/settings']) {
		await page.setViewportSize({ width: 1920, height: 900 })
		await page.goto(`${BASE}${path}`)
		await page.locator('h1').waitFor()
		check(!code.test(await page.locator('body').innerText()), `[text ${path}]: no requirement codes`)
	}
} finally {
	await browser.close()
	tunnel.kill()
}
if (failed) process.exit(1)
console.log('all checks passed')
