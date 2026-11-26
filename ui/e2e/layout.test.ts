import { test, expect, type Page } from '@playwright/test'
import AxeBuilder from '@axe-core/playwright'
import { settleTransitions } from './helpers'

// Evidence screenshots only when EVIDENCE_MILESTONE is set (AGENTS.md "Evidence").
async function evidenceShot(page: Page, name: string): Promise<void> {
	const milestone = process.env.EVIDENCE_MILESTONE
	if (!milestone) return
	await page.screenshot({
		path: `../tmp/evidence/${milestone}/screenshots/${name}`,
		fullPage: true
	})
}

async function assertAxeAA(page: Page, context: string) {
	await settleTransitions(page)
	const results = await new AxeBuilder({ page })
		.withTags(['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa'])
		.analyze()
	expect(results.violations, `${context}: axe WCAG AA violations`).toEqual([])
}

// Requirement IDs belong in docs and tests, never in text the panel shows.
const internalCode =
	/\b(NFR|UI|DEP|GHA|SEC|AUD|WIZ|APP|LOG|SETUP|ADM|TLS|RUN|DASH|DISK|DOC|HOST|INST|LAP|LOCK|REL|SRV|UPD|MIG)-\d+\b/

async function assertNoInternalCodes(page: Page, context: string) {
	const text = await page.locator('body').innerText()
	expect(
		text.match(internalCode)?.[0],
		`${context}: visible requirement code`
	).toBe(undefined)
	expect(text, `${context}: footer tagline`).not.toContain(
		'stateless rootless deployment platform'
	)
}

async function setTheme(page: Page, theme: 'light' | 'dark') {
	const isDark = await page
		.locator('html')
		.evaluate((el) => el.classList.contains('dark'))
	if (isDark !== (theme === 'dark')) {
		await page
			.locator('button[aria-label*="Toggle theme"]:visible')
			.first()
			.click()
	}
	await expect(page.locator('html')).toHaveClass(
		theme === 'dark' ? /dark/ : /^(?!.*dark)/
	)
}

test.describe('v0.6.2 layout (UI-2, UI-8)', () => {
	test('UI-8: main content fills the width; UI-2: server overview shows only API values', async ({
		page
	}) => {
		// 18 page × theme × width combinations, each with an axe run.
		test.setTimeout(180_000)
		await page.setViewportSize({ width: 1280, height: 800 })
		await page.goto('/login')
		await page.fill('#admin-password', 'correct-password-123')
		await page.click('button[type="submit"]')
		await expect(page).toHaveURL(/\/$/)

		// Negative control: the matcher must catch a code if one were shown.
		expect('Footprint (NFR-2)'.match(internalCode)?.[0]).toBe('NFR-2')

		const status = await (await page.request.get('/api/server/status')).json()

		for (const theme of ['light', 'dark'] as const) {
			for (const width of [1280, 1920, 375]) {
				await page.setViewportSize({ width, height: 900 })
				await page.goto('/')
				await setTheme(page, theme)
				await expect(page.locator('h1')).toHaveText('Server Overview')

				const overview = page.getByRole('region', { name: 'Server overview' })
				for (const label of ['Status', 'Memory', 'Apps', 'Version']) {
					await expect(overview.getByText(label, { exact: true })).toBeVisible()
				}
				await expect(overview).toContainText(status.version)
				await expect(overview).toContainText('Database: healthy')
				await expect(
					overview.locator('[title^="Database schema version"]')
				).toHaveAttribute(
					'title',
					`Database schema version ${status.schema_version}`
				)

				// Full width: no max-width on <main>, and it reaches the right edge of the viewport.
				const main = page.locator('main')
				await expect(main).toHaveCSS('max-width', 'none')
				const box = await main.boundingBox()
				const viewport = await page.evaluate(
					() => document.documentElement.clientWidth
				)
				expect(Math.round((box?.x ?? 0) + (box?.width ?? 0))).toBe(viewport)
				await expect(page.locator('footer')).toHaveCount(0)

				await assertNoInternalCodes(page, `Dashboard ${width} ${theme}`)
				await assertAxeAA(page, `Dashboard ${width} ${theme}`)
				await evidenceShot(page, `dashboard-${width}-${theme}.png`)
			}
		}

		// A long dev version (git describe of an untagged, dirty build) stays on one line, full value in title.
		const longVersion = 'v0.6.2-14-g3e4d902-dirty'
		await page.route('**/api/server/status', async (route) => {
			const res = await route.fetch()
			await route.fulfill({
				response: res,
				json: { ...(await res.json()), version: longVersion }
			})
		})
		await page.setViewportSize({ width: 1280, height: 900 })
		await page.goto('/')
		const versionEl = page
			.getByRole('region', { name: 'Server overview' })
			.locator(`[title="${longVersion}"]`)
		await expect(versionEl).toHaveText(longVersion)
		const oneLine = await versionEl.evaluate(
			(el) =>
				el.getBoundingClientRect().height <=
				parseFloat(getComputedStyle(el).lineHeight) + 1
		)
		expect(oneLine, 'long version on one line at 1280').toBe(true)
		await page.unroute('**/api/server/status')

		// Every other page reachable without an app: no codes, AA in both themes.
		const pages = [
			'/apps',
			'/activity',
			'/settings',
			'/settings?tab=security',
			'/settings?tab=hook-domain',
			'/settings?tab=github'
		]
		for (const theme of ['light', 'dark'] as const) {
			await page.setViewportSize({ width: 1280, height: 900 })
			for (const path of pages) {
				await page.goto(path)
				await setTheme(page, theme)
				await expect(page.locator('h1')).toBeVisible()
				await expect(page.getByText(/^Loading/)).toHaveCount(0)
				await assertNoInternalCodes(page, `${path} ${theme}`)
				await assertAxeAA(page, `${path} ${theme}`)
			}
			await page.setViewportSize({ width: 1920, height: 900 })
			await page.goto('/activity')
			await expect(page.locator('h1')).toBeVisible()
			await expect(page.getByText(/^Loading/)).toHaveCount(0)
			await evidenceShot(page, `activity-1920-${theme}.png`)
		}
	})
})
