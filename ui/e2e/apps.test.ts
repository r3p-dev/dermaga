import { test, expect, type Page } from '@playwright/test'
import AxeBuilder from '@axe-core/playwright'
import { settleTransitions } from './helpers'

async function evidenceShot(page: Page, name: string): Promise<void> {
	const milestone = process.env.EVIDENCE_MILESTONE
	if (!milestone) return
	await page.screenshot({
		path: `../tmp/evidence/${milestone}/screenshots/${name}`
	})
}

async function assertAxeAA(page: Page, contextName: string) {
	await settleTransitions(page)
	const results = await new AxeBuilder({ page })
		.withTags(['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa'])
		.analyze()
	if (results.violations.length > 0) {
		const summary = results.violations
			.map(
				(v) =>
					`${v.id} (${v.impact}): ${v.description}\n` +
					v.nodes.map((n) => `  - ${n.html}: ${n.failureSummary}`).join('\n')
			)
			.join('\n\n')
		throw new Error(`WCAG AA violations on ${contextName}:\n${summary}`)
	}
}

test.describe('M6b: New App Wizard, App Tabs, Env, Logs (WIZ-1..6, APP-1/5/6/7/9, LOG-1..2, UI-1..8)', () => {
	test('wizard stepper, missing Containerfile guidance, app tabs desktop+narrow light+dark, env reveal, and typed delete', async ({
		page
	}) => {
		await page.setViewportSize({ width: 1280, height: 800 })
		await page.goto('/login')
		await page.fill('#admin-password', 'correct-password-123')
		await page.click('button[type="submit"]')
		await expect(page).toHaveURL(/\/$/)

		// Dashboard empty state
		await expect(page.locator('text=No applications yet')).toBeVisible()
		await assertAxeAA(page, 'Dashboard empty (light, desktop)')

		// Mock GitHub status & repo inspection for Wizard
		await page.route('**/api/settings/github', async (route) => {
			await route.fulfill({
				status: 200,
				contentType: 'application/json',
				body: JSON.stringify({
					connected: true,
					hook_domain: 'hooks.example.com',
					app: {
						id: 1,
						slug: 'dermaga-e2e',
						name: 'Dermaga E2E',
						html_url: 'https://github.com/apps/dermaga-e2e'
					},
					installations: [
						{
							id: 42,
							account_login: 'octo',
							account_type: 'User',
							repository_selection: 'selected',
							html_url: ''
						}
					],
					repositories: [
						{
							id: 10,
							installation_id: 42,
							full_name: 'octo/portfolio',
							default_branch: 'main',
							private: false,
							html_url: ''
						},
						{
							id: 11,
							installation_id: 42,
							full_name: 'octo/empty-repo',
							default_branch: 'main',
							private: false,
							html_url: ''
						}
					]
				})
			})
		})

		await page.route('**/api/github/repos/*/branches', async (route) => {
			await route.fulfill({
				status: 200,
				contentType: 'application/json',
				body: JSON.stringify({ branches: ['main', 'staging'] })
			})
		})

		await page.route(
			'**/api/github/repos/octo/empty-repo/inspect*',
			async (route) => {
				await route.fulfill({
					status: 200,
					contentType: 'application/json',
					body: JSON.stringify({
						branch: 'main',
						has_containerfile: false,
						containerfile: '',
						build_context: '',
						suggested_port: 0,
						candidates: [],
						has_submodules: true,
						has_lfs: false,
						tree_truncated: false
					})
				})
			}
		)

		await page.route(
			'**/api/github/repos/octo/portfolio/inspect*',
			async (route) => {
				await route.fulfill({
					status: 200,
					contentType: 'application/json',
					body: JSON.stringify({
						branch: 'main',
						has_containerfile: true,
						containerfile: 'Containerfile',
						build_context: '.',
						suggested_port: 8080,
						candidates: [
							{ path: 'Containerfile', build_context: '.', exposed_port: 8080 }
						],
						has_submodules: false,
						has_lfs: false,
						tree_truncated: false
					})
				})
			}
		)

		await page.route('**/api/domains/check', async (route) => {
			await route.fulfill({
				status: 200,
				contentType: 'application/json',
				body: JSON.stringify({
					domain: 'portfolio.example.com',
					matches: true,
					expected_ip: '203.0.113.10',
					resolved_ips: ['203.0.113.10'],
					message: ''
				})
			})
		})

		// 1. Go to Wizard, test missing Containerfile guidance on octo/empty-repo
		await page.click('a:has-text("New app")')
		await expect(page).toHaveURL(/\/apps\/new/)
		await page.selectOption('#wiz-repo', 'octo/empty-repo')
		await page.click('button:has-text("Next: Build & Port")')
		await expect(
			page.locator('text=No Containerfile or Dockerfile found')
		).toBeVisible()
		await expect(
			page.locator('text=Git submodules (.gitmodules)')
		).toBeVisible()
		await assertAxeAA(
			page,
			'Wizard Step 2 missing Containerfile (light, desktop)'
		)

		// 2. Back to Step 1, pick octo/portfolio, verify suggested port 8080
		await page.click('button:has-text("Back")')
		await page.selectOption('#wiz-repo', 'octo/portfolio')
		await page.fill('#wiz-name', 'portfolio')
		await page.click('button:has-text("Next: Build & Port")')
		await expect(page.locator('#wiz-port')).toHaveValue('8080')
		await assertAxeAA(page, 'Wizard Step 2 detected (light, desktop)')
		await evidenceShot(page, 'm6b-wizard-step2-light-desktop.png')

		// 3. Step 3: Health, Domain DNS check, Env with build-time warning
		await page.click('button:has-text("Next: Env, Health & Domain")')
		await page.fill('#app-domain-input', 'portfolio.example.com')
		await page.click('button:has-text("Check DNS")')
		await expect(page.locator('text=DNS matches this server')).toBeVisible()
		await page.click('button:has-text("+ Add variable")')
		await page.fill('#env-key-0', 'PUBLIC_SITE')
		await page.fill('#env-val-0', 'https://portfolio.example.com')
		await page.check('label:has-text("Build time") input[type="checkbox"]')
		await expect(
			page.locator('text=Build-time variables warning:')
		).toBeVisible()
		await assertAxeAA(page, 'Wizard Step 3 env & domain (light, desktop)')
		await evidenceShot(page, 'm6b-wizard-step3-light-desktop.png')

		// 4. Step 4: Deploy & live SSE build log
		let appCreated = false
		await page.route('**/api/apps', async (route) => {
			if (route.request().method() === 'POST') {
				appCreated = true
				await route.fulfill({
					status: 201,
					contentType: 'application/json',
					body: JSON.stringify({
						app: {
							id: 'app_e2e_1',
							name: 'portfolio',
							repo_full_name: 'octo/portfolio',
							branch: 'main',
							primary_domain: 'portfolio.example.com',
							port: 8080,
							health_type: 'http',
							created_at: '2026-11-26T00:00:00Z'
						},
						deployment: {
							id: 'dep_e2e_1',
							app_id: 'app_e2e_1',
							release_number: 1,
							status: 'queued',
							status_label: 'queued',
							trigger: 'manual',
							commit_hash: 'abc1234',
							commit_message: 'Initial',
							started_at: '2026-11-26T00:00:00Z'
						}
					})
				})
				return
			}
			await route.fulfill({
				status: 200,
				contentType: 'application/json',
				body: JSON.stringify({
					apps: appCreated
						? [
								{
									id: 'app_e2e_1',
									name: 'portfolio',
									repo_full_name: 'octo/portfolio',
									branch: 'main',
									primary_domain: 'portfolio.example.com',
									port: 8080,
									health_type: 'http',
									last_deployment: {
										id: 'dep_e2e_1',
										app_id: 'app_e2e_1',
										release_number: 1,
										status: 'active',
										status_label: 'live',
										trigger: 'manual',
										commit_hash: 'abc1234',
										commit_message: 'Initial',
										started_at: '2026-11-26T00:00:00Z'
									},
									created_at: '2026-11-26T00:00:00Z'
								}
							]
						: []
				})
			})
		})

		await page.route(
			'**/api/deployments/dep_e2e_1/logs/stream',
			async (route) => {
				await route.fulfill({
					status: 200,
					contentType: 'text/event-stream',
					body:
						'event: log\ndata: "==> Image built: localhost/dermaga/portfolio:r1\\n"\n\n' +
						'event: status\ndata: "live"\n\n' +
						'event: done\ndata: ""\n\n'
				})
			}
		)

		await page.click('button:has-text("Next: Review & Deploy")')
		await page.click('button:has-text("Deploy app")')
		await expect(
			page.locator('text=Image built: localhost/dermaga/portfolio:r1')
		).toBeVisible()
		await expect(page.locator('text=Deployment is live at')).toBeVisible()
		await evidenceShot(page, 'm6b-wizard-step4-live-light-desktop.png')

		// 5. App Detail page & all 6 tabs
		await page.route('**/api/apps/app_e2e_1', async (route) => {
			if (route.request().method() === 'DELETE') {
				await route.fulfill({
					status: 200,
					contentType: 'application/json',
					body: JSON.stringify({ success: true })
				})
				return
			}
			await route.fulfill({
				status: 200,
				contentType: 'application/json',
				body: JSON.stringify({
					id: 'app_e2e_1',
					name: 'portfolio',
					repo_full_name: 'octo/portfolio',
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
					primary_domain: 'portfolio.example.com',
					active_release_id: 'dep_e2e_1',
					env: [{ key: 'PUBLIC_SITE', runtime: true, build_time: true }],
					deployments: [
						{
							id: 'dep_e2e_1',
							app_id: 'app_e2e_1',
							release_number: 1,
							status: 'active',
							status_label: 'live',
							trigger: 'manual',
							commit_hash: 'abc1234',
							commit_message: 'Initial',
							build_logs: '==> Image built\n==> Deployment is live\n',
							started_at: '2026-11-26T00:00:00Z',
							finished_at: '2026-11-26T00:00:12Z'
						}
					],
					created_at: '2026-11-26T00:00:00Z',
					updated_at: '2026-11-26T00:00:12Z'
				})
			})
		})

		await page.route('**/api/apps/app_e2e_1/env/reveal', async (route) => {
			await route.fulfill({
				status: 200,
				contentType: 'application/json',
				body: JSON.stringify({
					key: 'PUBLIC_SITE',
					scope: 'runtime',
					value: 'https://portfolio.example.com'
				})
			})
		})

		await page.route('**/api/apps/app_e2e_1/logs/stream*', async (route) => {
			await route.fulfill({
				status: 200,
				contentType: 'text/event-stream',
				body: 'event: log\ndata: "2026-11-26T00:00:15Z listening on :8080"\n\nevent: done\ndata: ""\n\n'
			})
		})

		await page.click('a:has-text("Open App Page")')
		await expect(page).toHaveURL(/\/apps\/app_e2e_1/)
		await expect(
			page.locator('text=https://portfolio.example.com')
		).toBeVisible()
		await assertAxeAA(page, 'App Overview (light, desktop)')

		// Environment tab + reveal
		await page.click('button[role="tab"]:has-text("Environment")')
		await expect(page.locator('text=••••••••••••')).toBeVisible()
		await page.click('button:has-text("Reveal")')
		await expect(
			page.locator('text=https://portfolio.example.com')
		).toBeVisible()
		await assertAxeAA(page, 'App Environment revealed (light, desktop)')
		await evidenceShot(page, 'm6b-app-env-light-desktop.png')

		// Runtime Logs tab
		await page.click('button[role="tab"]:has-text("Logs")')
		await expect(page.locator('text=listening on :8080')).toBeVisible()

		// Narrow dark viewport check
		await page.setViewportSize({ width: 375, height: 812 })
		await page.evaluate(() => document.documentElement.classList.add('dark'))
		await page.click('button[role="tab"]:has-text("Settings")')
		await expect(
			page.getByRole('heading', { name: 'Resources & Security' })
		).toBeVisible()
		await assertAxeAA(page, 'App Settings (dark, narrow)')
		await evidenceShot(page, 'm6b-app-settings-dark-narrow.png')

		const overflow = await page.evaluate(
			() =>
				document.documentElement.scrollWidth >
				document.documentElement.clientWidth
		)
		expect(overflow).toBe(false)

		// Danger zone typed delete
		await page.click('button[role="tab"]:has-text("Danger zone")')
		await page.fill('#confirm-delete', 'portfolio')
		await page.click('button:has-text("Remove app")')
		await expect(page).toHaveURL(/\/$/)
	})
})
