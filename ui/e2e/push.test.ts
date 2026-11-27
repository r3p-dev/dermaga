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

async function setTheme(page: Page, theme: 'light' | 'dark') {
	await page.evaluate((t) => {
		document.documentElement.classList.toggle('dark', t === 'dark')
		localStorage.setItem('theme', t)
	}, theme)
}

const json = (route: Route, status: number, body: unknown) =>
	route.fulfill({
		status,
		contentType: 'application/json',
		body: JSON.stringify(body)
	})

function dep(
	n: number,
	status: string,
	label: string,
	trigger: string,
	eligible = false
) {
	return {
		id: `dep_${n}`,
		app_id: 'app_push',
		release_number: n,
		status,
		status_label: label,
		trigger,
		commit_hash: `${n}${n}${n}${n}abcdef`,
		commit_message: `change ${n}`,
		build_logs: '',
		started_at: new Date(Date.now() - (10 - n) * 60_000).toISOString(),
		finished_at:
			status === 'active' || status === 'inactive'
				? new Date(Date.now() - (10 - n) * 60_000 + 30_000).toISOString()
				: undefined,
		rollback_eligible: eligible
	}
}

const baseApp = {
	id: 'app_push',
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
	env: [],
	created_at: '2026-11-27T00:00:00Z',
	updated_at: '2026-11-27T00:00:00Z'
}

test.describe('M7b: push-to-deploy (APP-8, UI-7, UI-8)', () => {
	test('live status, deploy on push settings, rollback notice, desktop+narrow light+dark', async ({
		page
	}) => {
		test.setTimeout(120_000)
		let pushed = false
		let autoDeploy = true
		let watchPaths = ''
		const saved: Record<string, unknown>[] = []

		await page.route('**/api/apps/app_push', async (route) => {
			if (route.request().method() === 'PATCH') {
				const body = route.request().postDataJSON() as Record<string, unknown>
				saved.push(body)
				if (String(body.watch_paths).includes('/etc/')) {
					await json(route, 400, {
						error:
							'watch path "/etc/" must be relative to the repository root (no leading /)'
					})
					return
				}
				autoDeploy = body.auto_deploy as boolean
				watchPaths = body.watch_paths as string
			}
			const deployments = [
				dep(2, 'active', 'live', 'manual'),
				dep(1, 'inactive', 'Previous release', 'manual', true)
			]
			if (pushed) deployments.unshift(dep(3, 'building', 'building', 'push'))
			await json(route, 200, {
				...baseApp,
				auto_deploy: autoDeploy,
				watch_paths: watchPaths,
				active_release_id: 'dep_2',
				deployments
			})
		})
		await page.route('**/api/apps/app_push/rollback-preview*', (route) =>
			json(route, 200, {
				deployment: dep(1, 'inactive', 'Previous release', 'manual', true),
				env_changes: [],
				memory: {
					available_bytes: 1536 * 1048576,
					needed_bytes: 512 * 1048576,
					ok: true,
					memory_limit: '512m'
				}
			})
		)

		await page.setViewportSize({ width: 1280, height: 900 })
		await page.goto('/login')
		await page.fill('#admin-password', 'correct-password-123')
		await page.click('button[type="submit"]')
		await expect(page).toHaveURL(/\/$/)

		// Live status (UI-7): a push-started deployment shows up without a reload.
		await page.goto('/apps/app_push')
		await page.getByRole('tab', { name: 'Deployments' }).click()
		const rows = page.locator('div.rounded-lg.border', { hasText: 'change ' })
		await expect(rows).toHaveCount(2)
		pushed = true
		await expect(rows).toHaveCount(3, { timeout: 5_000 })
		await expect(rows.first()).toContainText('push')
		await expect(rows.first()).toContainText('3333abc')

		// Rollback dialog: the push notice while deploy on push is on.
		await page.getByRole('button', { name: 'Roll back' }).click()
		const rb = page.getByRole('dialog', { name: 'Roll back shop to r1?' })
		await expect(
			rb.getByText('The next push to main will deploy over this rollback.')
		).toBeVisible()
		for (const theme of ['light', 'dark'] as const) {
			for (const width of [1280, 375]) {
				await page.setViewportSize({ width, height: 900 })
				await setTheme(page, theme)
				await assertAxeAA(
					page,
					`Rollback dialog with push notice ${width} ${theme}`
				)
				await evidenceShot(
					page,
					`m7b-rollback-push-notice-${width}-${theme}.png`
				)
			}
		}
		await rb.getByRole('button', { name: 'Cancel' }).click()
		await page.setViewportSize({ width: 1280, height: 900 })
		await setTheme(page, 'light')

		// Settings: Deploy on push inside the existing form (owner choice A).
		await page.getByRole('tab', { name: 'Settings' }).click()
		const toggle = page.getByLabel(
			'Deploy automatically when main receives a push'
		)
		const watch = page.getByLabel('Watch paths')
		await expect(toggle).toBeChecked()
		await expect(page.locator('#set-watch-help')).toContainText(
			'One pattern per line. A trailing / means the whole folder'
		)
		await watch.fill('web/\n/etc/')
		await page.getByRole('button', { name: 'Save settings' }).click()
		await expect(
			page.getByText('must be relative to the repository root')
		).toBeVisible()
		await watch.fill('web/\nContainerfile')
		// A poll in between must not wipe the edit.
		await page.waitForTimeout(3_500)
		await expect(watch).toHaveValue('web/\nContainerfile')
		for (const theme of ['light', 'dark'] as const) {
			for (const width of [1280, 375]) {
				await page.setViewportSize({ width, height: 900 })
				await setTheme(page, theme)
				await page.locator('#set-watch').scrollIntoViewIfNeeded()
				await assertAxeAA(page, `Settings deploy on push ${width} ${theme}`)
				await evidenceShot(page, `m7b-settings-push-${width}-${theme}.png`)
			}
		}
		await page.setViewportSize({ width: 1280, height: 900 })
		await setTheme(page, 'light')
		await page.getByRole('button', { name: 'Save settings' }).click()
		await expect(page.getByText('Settings saved.')).toBeVisible()
		expect(saved.at(-1)).toMatchObject({
			auto_deploy: true,
			watch_paths: 'web/\nContainerfile'
		})

		await toggle.uncheck()
		await expect(watch).toBeDisabled()
		await page.getByRole('button', { name: 'Save settings' }).click()
		await expect(page.getByText('Settings saved.')).toBeVisible()
		expect(saved.at(-1)).toMatchObject({ auto_deploy: false })

		// Negative control: with deploy on push off, the rollback dialog has no push notice.
		await page.getByRole('tab', { name: 'Deployments' }).click()
		await page.getByRole('button', { name: 'Roll back' }).click()
		await expect(
			page.getByRole('dialog', { name: 'Roll back shop to r1?' })
		).toBeVisible()
		await expect(page.getByText('will deploy over this rollback')).toHaveCount(
			0
		)
	})

	test('Settings → GitHub shows the last ignored push', async ({ page }) => {
		await page.route('**/api/settings/github', (route) =>
			json(route, 200, {
				connected: true,
				hook_domain: 'hooks.example.com',
				app: {
					id: 1,
					slug: 'dermaga-vm',
					name: 'dermaga-vm',
					html_url: 'https://github.com/apps/dermaga-vm',
					install_url: 'https://github.com/apps/dermaga-vm/installations/new'
				},
				installations: [],
				repositories: [],
				last_delivery: {
					delivery_id: 'd-1',
					event: 'push',
					action: '',
					repository_full_name: 'r3p-dev/dermaga-test-app',
					status: 'accepted',
					received_at: '2026-11-27T10:00:00Z'
				},
				last_ignored_push: {
					description:
						'Push to dev (abc1234) ignored for shop: the app deploys from main.',
					created_at: '2026-11-27T10:00:00Z'
				}
			})
		)
		await page.setViewportSize({ width: 1280, height: 900 })
		await page.goto('/login')
		await page.fill('#admin-password', 'correct-password-123')
		await page.click('button[type="submit"]')
		await expect(page).toHaveURL(/\/$/)
		await page.goto('/settings?tab=github')
		await expect(page.getByText('Last ignored push')).toBeVisible()
		await expect(
			page.getByText(
				'Push to dev (abc1234) ignored for shop: the app deploys from main.'
			)
		).toBeVisible()
		for (const theme of ['light', 'dark'] as const) {
			for (const width of [1280, 375]) {
				await page.setViewportSize({ width, height: 900 })
				await setTheme(page, theme)
				await assertAxeAA(
					page,
					`Settings GitHub last ignored push ${width} ${theme}`
				)
				await evidenceShot(
					page,
					`m7b-github-ignored-push-${width}-${theme}.png`
				)
			}
		}
	})
})
