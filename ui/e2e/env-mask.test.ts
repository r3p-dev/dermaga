import { test, expect, type Page, type Route } from '@playwright/test'
import AxeBuilder from '@axe-core/playwright'
import { settleTransitions } from './helpers'

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

const json = (route: Route, status: number, body: unknown) =>
	route.fulfill({
		status,
		contentType: 'application/json',
		body: JSON.stringify(body)
	})

const app = {
	id: 'app_env',
	name: 'shop',
	repo_full_name: 'r3p-dev/dermaga-test-app',
	branch: 'main',
	containerfile_path: 'Containerfile',
	build_context: '.',
	port: 8080,
	health_type: 'http',
	health_path: '/health',
	health_cmd: '',
	health_timeout_sec: 30,
	health_consecutive: 3,
	memory_limit: '512m',
	cpu_limit: '1.0',
	pids_limit: 128,
	read_only_root: false,
	add_capabilities: [],
	primary_domain: 'shop.example.com',
	env: [{ key: 'BUILD_VAR', runtime: false, build_time: true }],
	deployments: [],
	created_at: '2026-11-27T00:00:00Z',
	updated_at: '2026-11-27T00:00:00Z'
}

test.describe('M7a: short env values are not masked (SEC-9, APP-1, UI-8)', () => {
	test('env editor warns for values under 4 characters, desktop+narrow light+dark', async ({
		page
	}) => {
		await page.route('**/api/apps/app_env', (route) => json(route, 200, app))
		await page.route('**/api/apps/app_env/env/reveal', (route) =>
			json(route, 200, { key: 'BUILD_VAR', scope: 'build_time', value: 'b2' })
		)

		await page.setViewportSize({ width: 1280, height: 900 })
		await page.goto('/login')
		await page.fill('#admin-password', 'correct-password-123')
		await page.click('button[type="submit"]')
		await expect(page).toHaveURL(/\/$/)
		await page.goto('/apps/app_env')
		await page.getByRole('tab', { name: 'Environment' }).click()
		await page.getByRole('button', { name: 'Edit variables' }).click()

		const value = page.locator('#env-val-0')
		await expect(value).toHaveValue('b2')
		const hint = page.locator('#env-val-short-0')
		await expect(hint).toHaveText(
			'Shorter than 4 characters: this value is not masked in build and runtime logs.'
		)
		await expect(value).toHaveAttribute('aria-describedby', 'env-val-short-0')

		for (const theme of ['light', 'dark'] as const) {
			for (const width of [1280, 375]) {
				await page.setViewportSize({ width, height: 900 })
				await page.evaluate((t) => {
					document.documentElement.classList.toggle('dark', t === 'dark')
					localStorage.setItem('theme', t)
				}, theme)
				await assertAxeAA(page, `Env editor short value ${width} ${theme}`)
				await evidenceShot(page, `m7a-env-short-value-${width}-${theme}.png`)
			}
		}

		// Four characters (counted as characters, not bytes) are masked: no hint.
		await value.fill('b2c4')
		await expect(hint).toHaveCount(0)
		await expect(value).not.toHaveAttribute('aria-describedby', /.+/)
		await value.fill('tö')
		await expect(hint).toBeVisible()
		await value.fill('')
		await expect(hint).toHaveCount(0)
	})
})
