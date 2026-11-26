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

// Same effect as the theme toggle (class on <html> + localStorage); works while a modal dialog
// blocks clicks on the page behind it.
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
	eligible: boolean,
	extra: Record<string, unknown> = {}
) {
	return {
		id: `dep_${n}`,
		app_id: 'app_rb',
		release_number: n,
		status,
		status_label: label,
		trigger: n === 4 ? 'rollback' : 'push',
		commit_hash: `${n}${n}${n}${n}abc`,
		commit_message: `change ${n}`,
		build_logs: '==> Deployment is live\n',
		started_at: new Date(Date.now() - n * 3600_000).toISOString(),
		finished_at: new Date(Date.now() - n * 3600_000 + 90_000).toISOString(),
		rollback_eligible: eligible,
		...extra
	}
}

const baseApp = {
	id: 'app_rb',
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
	env: [{ key: 'API_KEY', runtime: true, build_time: false }],
	created_at: '2026-11-26T00:00:00Z',
	updated_at: '2026-11-26T00:00:00Z'
}

test.describe('M7a: Deployments tab, redeploy and rollback (DEP-1, DEP-4, DEP-9, UI-3, UI-7, UI-8)', () => {
	test('rollback and redeploy dialogs, refusals, live status, desktop+narrow light+dark', async ({
		page
	}) => {
		test.setTimeout(120_000)
		let rolledBack = false
		let memoryOK = true
		let rollbackBodies: unknown[] = []
		let rollbackConflict = true

		await page.route('**/api/apps/app_rb', (route) =>
			json(route, 200, {
				...baseApp,
				active_release_id: rolledBack ? 'dep_5' : 'dep_3',
				deployments: rolledBack
					? [
							dep(5, 'switching', 'Switching traffic', false, {
								trigger: 'rollback',
								finished_at: undefined
							}),
							dep(3, 'active', 'live', false),
							dep(2, 'inactive', 'Previous release', true),
							dep(1, 'failed', 'failed', false, {
								error_message: 'HTTP health check /health timed out after 30s'
							})
						]
					: [
							dep(3, 'active', 'live', false),
							dep(2, 'inactive', 'Previous release', true),
							dep(1, 'failed', 'failed', false, {
								error_message: 'HTTP health check /health timed out after 30s'
							})
						]
			})
		)
		await page.route('**/api/apps/app_rb/memory-check', (route) =>
			json(route, 200, {
				available_bytes: memoryOK ? 1536 * 1048576 : 300 * 1048576,
				needed_bytes: 512 * 1048576,
				ok: memoryOK,
				memory_limit: '512m'
			})
		)
		await page.route('**/api/apps/app_rb/rollback-preview*', (route) =>
			json(route, 200, {
				deployment: dep(2, 'inactive', 'Previous release', true),
				env_changes: [
					{ name: 'API_KEY', change: 'changed' },
					{ name: 'NEW_FLAG', change: 'added' },
					{ name: 'OLD_FLAG', change: 'removed' }
				],
				memory: {
					available_bytes: 1536 * 1048576,
					needed_bytes: 512 * 1048576,
					ok: true,
					memory_limit: '512m'
				}
			})
		)
		await page.route('**/api/apps/app_rb/rollback', async (route) => {
			rollbackBodies.push(route.request().postDataJSON())
			if (rollbackConflict) {
				await json(route, 409, { error: 'Cancel the running build first' })
				return
			}
			rolledBack = true
			await json(
				route,
				202,
				dep(5, 'starting', 'starting', false, { trigger: 'rollback' })
			)
		})
		await page.route('**/api/apps/app_rb/deploy', async (route) => {
			const body = route.request().postDataJSON() as { commit?: string }
			if (body.commit && body.commit !== 'abc1234') {
				await json(route, 400, {
					error: 'validation failed',
					fields: {
						commit: `commit ${body.commit} was not found in r3p-dev/dermaga-test-app`
					}
				})
				return
			}
			await json(route, 202, dep(6, 'queued', 'queued', false))
		})
		await page.route('**/api/deployments/*/logs/stream', (route) =>
			route.fulfill({
				status: 200,
				contentType: 'text/event-stream',
				body: route.request().url().includes('/dep_5/')
					? 'event: log\ndata: "==> Rolling back to r2: image localhost/dermaga/shop:r2, its environment and settings (no rebuild)\\n"\n\n' +
						'event: status\ndata: "Switching traffic"\n\n'
					: 'event: status\ndata: "queued"\n\n'
			})
		)

		await page.setViewportSize({ width: 1280, height: 900 })
		await page.goto('/login')
		await page.fill('#admin-password', 'correct-password-123')
		await page.click('button[type="submit"]')
		await expect(page).toHaveURL(/\/$/)

		await page.goto('/apps/app_rb')
		await page.getByRole('tab', { name: 'Deployments' }).click()
		const rows = page.locator('div.rounded-lg.border', { hasText: 'change ' })
		await expect(rows).toHaveCount(3)
		await expect(page.getByRole('button', { name: 'Roll back' })).toHaveCount(1)
		await expect(page.getByText('Previous release')).toBeVisible()
		await expect(
			page.getByText('HTTP health check /health timed out after 30s')
		).toBeVisible()

		for (const theme of ['light', 'dark'] as const) {
			for (const width of [1280, 375]) {
				await page.setViewportSize({ width, height: 900 })
				await setTheme(page, theme)
				await assertAxeAA(page, `Deployments tab ${width} ${theme}`)
				await evidenceShot(page, `m7a-deployments-${width}-${theme}.png`)
			}
		}

		// Rollback dialog (mockup A): preview, warning for a changed secret, 409 refusal.
		await page.setViewportSize({ width: 1280, height: 900 })
		await setTheme(page, 'light')
		await page.getByRole('button', { name: 'Roll back' }).click()
		const rbDialog = page.getByRole('dialog', { name: 'Roll back shop to r2?' })
		await expect(rbDialog).toBeVisible()
		await expect(rbDialog.getByText('API_KEY', { exact: true })).toBeVisible()
		await expect(rbDialog.getByText('value changed since r2')).toBeVisible()
		await expect(rbDialog.getByText('removed since r2')).toBeVisible()
		await expect(rbDialog.getByRole('note')).toContainText(
			'API_KEY changed after r2'
		)
		for (const theme of ['light', 'dark'] as const) {
			for (const width of [1280, 375]) {
				await page.setViewportSize({ width, height: 900 })
				await setTheme(page, theme)
				await assertAxeAA(page, `Rollback dialog ${width} ${theme}`)
				await evidenceShot(page, `m7a-rollback-dialog-${width}-${theme}.png`)
			}
		}
		await page.setViewportSize({ width: 1280, height: 900 })
		await rbDialog.getByRole('button', { name: 'Roll back to r2' }).click()
		await expect(rbDialog.getByRole('alert')).toHaveText(
			'Cancel the running build first'
		)
		await page.keyboard.press('Escape')
		await expect(rbDialog).toBeHidden()

		// Confirm: the rollback starts, its log and live status show without a reload (UI-7).
		rollbackConflict = false
		await page.getByRole('button', { name: 'Roll back' }).click()
		await rbDialog.getByRole('button', { name: 'Roll back to r2' }).click()
		await expect(rbDialog).toBeHidden()
		expect(rollbackBodies.at(-1)).toEqual({ deployment_id: 'dep_2' })
		await expect(page.getByText('Rolling back to r2')).toBeVisible()
		await expect(page.getByText('Switching traffic').first()).toBeVisible()
		await expect(
			page.getByText(/Build log .* r5 \(Switching traffic\)/)
		).toBeVisible()

		// Redeploy menu → a specific commit: server field error next to the field (UI-3).
		await setTheme(page, 'light')
		await page.getByText('Redeploy').click()
		await page.getByRole('button', { name: 'A specific commit...' }).click()
		const rdDialog = page.getByRole('dialog', { name: 'Deploy shop' })
		await expect(rdDialog.getByText('Free memory: 1536 MiB')).toBeVisible()
		await rdDialog.getByLabel('Commit SHA').fill('deadbee')
		await rdDialog.getByRole('button', { name: 'Deploy' }).click()
		await expect(
			rdDialog.getByText('commit deadbee was not found')
		).toBeVisible()
		await expect(rdDialog.getByLabel('Commit SHA')).toHaveAttribute(
			'aria-invalid',
			'true'
		)
		await assertAxeAA(page, 'Redeploy dialog with field error')
		await evidenceShot(page, 'm7a-redeploy-commit-error-1280-light.png')
		await rdDialog.getByLabel('Commit SHA').fill('abc1234')
		await rdDialog.getByRole('button', { name: 'Deploy' }).click()
		await expect(rdDialog).toBeHidden()

		// Memory refusal shown before starting: Deploy is disabled.
		memoryOK = false
		await page
			.getByRole('button', { name: 'Deploy', exact: true })
			.first()
			.click()
		await expect(rdDialog.getByRole('alert')).toContainText(
			'Not enough free memory: 300 MiB available, the new release needs 512 MiB'
		)
		await expect(
			rdDialog.getByRole('button', { name: 'Deploy' })
		).toBeDisabled()
		for (const theme of ['light', 'dark'] as const) {
			await setTheme(page, theme)
			await assertAxeAA(page, `Redeploy memory refusal ${theme}`)
			await evidenceShot(page, `m7a-redeploy-memory-1280-${theme}.png`)
		}
		await rdDialog.getByRole('button', { name: 'Cancel' }).click()
		await expect(rdDialog).toBeHidden()
	})
})
