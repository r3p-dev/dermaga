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

const app = {
	id: 'app_dm',
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
	deployments: [],
	auto_deploy: true,
	watch_paths: '',
	active_release_id: 'dep_1',
	created_at: '2026-11-26T00:00:00Z',
	updated_at: '2026-11-26T00:00:00Z'
}

const dnsWaiting = {
	domain: 'new.example.com',
	server_ip: '203.0.113.10',
	a_records: [],
	aaaa_records: [],
	a_matches: false,
	aaaa_mismatch: false,
	warning:
		'No A record found yet. Create an A record for new.example.com pointing to 203.0.113.10.'
}

function domains() {
	const list = [
		{
			domain: 'shop.example.com',
			role: 'primary',
			dns_verified: true,
			certificate: {
				status: 'active',
				expires_at: '2027-02-25T04:31:50Z',
				expiring_soon: false
			}
		},
		{
			domain: 'www.shop.example.com',
			role: 'redirect',
			dns_verified: true,
			certificate: { status: 'requesting', expiring_soon: false }
		},
		{
			domain: 'new.example.com',
			role: 'alias',
			dns_verified: false,
			dns: dnsWaiting,
			certificate: { status: 'waiting_dns', expiring_soon: false }
		},
		{
			domain: 'old.example.net',
			role: 'redirect',
			dns_verified: true,
			certificate: {
				status: 'failed',
				expiring_soon: false,
				reason:
					'The certificate authority could not reach this server on port 80 in time.',
				fix: 'Check that the A record points to this server and that no firewall or proxy blocks port 80, then press Check again.',
				detail:
					'acme: error: 400 :: urn:ietf:params:acme:error:connection :: context deadline exceeded',
				next_retry_at: '2026-11-27T05:31:00Z'
			}
		}
	]
	return {
		domains: list,
		path_prefix: '',
		strip_prefix: false,
		server_ip: '203.0.113.10',
		live: true
	}
}

test.describe('M8a: Domains tab, certificate status, hook domain and dashboard warnings (APP-2, APP-10, TLS-2–TLS-5, UI-3, UI-8)', () => {
	test('domain rows, add with www redirect, make primary, Traefik refusal, check again, prefix error', async ({
		page
	}) => {
		test.setTimeout(120_000)
		const puts: unknown[] = []
		let checks = 0
		let state: ReturnType<typeof domains> = domains()
		let refuseNext: 'field' | 'traefik' | null = 'field'

		await page.route('**/api/apps/app_dm', (route) => json(route, 200, app))
		await page.route('**/api/domains/check', (route) =>
			json(route, 200, {
				domain: 'api.example.com',
				matches: false,
				expected_ip: '203.0.113.10',
				message: 'No A record found yet.'
			})
		)
		await page.route('**/api/apps/app_dm/domains/check', async (route) => {
			checks++
			await json(route, 200, state)
		})
		await page.route('**/api/apps/app_dm/domains', async (route) => {
			if (route.request().method() === 'GET') {
				await json(route, 200, state)
				return
			}
			const body = route.request().postDataJSON() as {
				domains: { domain: string; role: string }[]
				path_prefix: string
			}
			puts.push(body)
			if (body.path_prefix === '/blog/') {
				await json(route, 400, {
					error: 'validation failed',
					fields: {
						path_prefix:
							'Use a path like /blog: it starts with /, has no spaces and does not end with /.'
					}
				})
				return
			}
			if (refuseNext === 'field') {
				refuseNext = null
				await json(route, 400, {
					error: 'validation failed',
					fields: { 'domains.4': 'This domain already belongs to another app.' }
				})
				return
			}
			if (refuseNext === 'traefik') {
				refuseNext = null
				await json(route, 502, {
					error:
						'Traefik did not load the new route, so the previous route and settings stay in place: middleware "x@file" does not exist'
				})
				return
			}
			// Keep known domains with their new role; new ones wait for DNS.
			state = {
				...state,
				domains: body.domains.map((d) => {
					const known = state.domains.find((x) => x.domain === d.domain)
					return known
						? { ...known, role: d.role }
						: {
								domain: d.domain,
								role: d.role,
								dns_verified: false,
								dns: { ...dnsWaiting, domain: d.domain },
								certificate: { status: 'waiting_dns', expiring_soon: false }
							}
				})
			}
			await json(route, 200, state)
		})

		await page.setViewportSize({ width: 1280, height: 900 })
		await page.goto('/login')
		await page.fill('#admin-password', 'correct-password-123')
		await page.click('button[type="submit"]')
		await expect(page).toHaveURL(/\/$/)

		await page.goto('/apps/app_dm')
		await page.getByRole('tab', { name: 'Domains' }).click()
		await expect(
			page.getByText('● HTTPS active · until 25 Feb 2027')
		).toBeVisible()
		await expect(page.getByText('◌ Requesting certificate')).toBeVisible()
		await expect(page.getByText('○ Certificate waits for DNS')).toBeVisible()
		await expect(
			page.getByText('Create this record at your DNS provider:')
		).toContainText('new.example.com → 203.0.113.10')
		await expect(
			page.getByText('could not reach this server on port 80 in time')
		).toBeVisible()
		await expect(
			page.getByText('This domain is not served until the certificate works.')
		).toContainText(/Next\s+automatic\s+try:\s+\d\d:\d\d\./)

		for (const theme of ['light', 'dark'] as const) {
			for (const width of [1280, 375]) {
				await page.setViewportSize({ width, height: 900 })
				await setTheme(page, theme)
				await assertAxeAA(page, `Domains tab ${width} ${theme}`)
				await evidenceShot(page, `m8a-domains-${width}-${theme}.png`)
			}
		}
		await page.setViewportSize({ width: 1280, height: 900 })
		await setTheme(page, 'light')

		// Check again (the whole app at once).
		await page.getByRole('button', { name: 'Check again' }).first().click()
		await expect(page.getByText('Checked again.')).toBeVisible()
		expect(checks).toBe(1)

		// Add a domain with its www redirect; the first save gets a field error next to the field.
		await page.getByRole('button', { name: '+ Add domain' }).click()
		const dialog = page.getByRole('dialog', { name: 'Add a domain' })
		await dialog
			.getByRole('textbox', { name: 'Domain' })
			.fill('api.example.com')
		await dialog.getByRole('button', { name: 'Check DNS' }).click()
		await expect(
			dialog.getByText('DNS does not point to this server yet.')
		).toBeVisible()
		await expect(
			dialog.getByText('the certificate is requested once the record is right')
		).toBeVisible()
		await dialog.getByLabel('Also add www.api.example.com').check()
		for (const theme of ['light', 'dark'] as const) {
			for (const width of [1280, 375]) {
				await page.setViewportSize({ width, height: 900 })
				await setTheme(page, theme)
				await assertAxeAA(page, `Add domain dialog ${width} ${theme}`)
				await evidenceShot(page, `m8a-add-domain-${width}-${theme}.png`)
			}
		}
		await page.setViewportSize({ width: 1280, height: 900 })
		await setTheme(page, 'light')
		await dialog.getByRole('button', { name: 'Save' }).click()
		await expect(
			dialog.getByText('This domain already belongs to another app.')
		).toBeVisible()
		await dialog.getByRole('button', { name: 'Save' }).click()
		await expect(dialog).toBeHidden()
		expect(puts.at(-1)).toEqual({
			domains: [
				{ domain: 'shop.example.com', role: 'primary' },
				{ domain: 'www.shop.example.com', role: 'redirect' },
				{ domain: 'new.example.com', role: 'alias' },
				{ domain: 'old.example.net', role: 'redirect' },
				{ domain: 'api.example.com', role: 'alias' },
				{ domain: 'www.api.example.com', role: 'redirect' }
			],
			path_prefix: '',
			strip_prefix: false
		})
		await expect(page.getByText('api.example.com was added.')).toBeVisible()

		// Make primary from the row menu.
		await page.getByLabel('Actions for new.example.com').click()
		await page.getByRole('button', { name: 'Make primary' }).click()
		await expect(
			page.getByText('new.example.com is now the primary domain.')
		).toBeVisible()
		const roles = (
			puts.at(-1) as { domains: { domain: string; role: string }[] }
		).domains
		expect(roles.find((d) => d.domain === 'new.example.com')?.role).toBe(
			'primary'
		)
		expect(roles.find((d) => d.domain === 'shop.example.com')?.role).toBe(
			'alias'
		)

		// Traefik refuses: the reason is shown and nothing changes.
		refuseNext = 'traefik'
		await page.getByLabel('Actions for shop.example.com').click()
		await page.getByRole('button', { name: 'Remove' }).click()
		await expect(
			page.getByRole('alert').filter({ hasText: 'Traefik did not load' })
		).toContainText(
			'previous route and settings stay in place: middleware "x@file" does not exist'
		)
		await expect(
			page.getByText('shop.example.com', { exact: true })
		).toBeVisible()
		await assertAxeAA(page, 'Domains tab with a Traefik refusal')
		await evidenceShot(page, 'm8a-domains-refused-1280-light.png')

		// Path prefix error next to its field.
		await page.getByRole('textbox', { name: 'Prefix' }).fill('/blog/')
		await page.getByRole('button', { name: 'Save', exact: true }).click()
		await expect(
			page.getByText('it starts with /, has no spaces')
		).toBeVisible()
		await expect(page.getByRole('textbox', { name: 'Prefix' })).toHaveAttribute(
			'aria-invalid',
			'true'
		)
	})

	test('dashboard certificate warning and hook domain certificate (TLS-4, TLS-5)', async ({
		page
	}) => {
		await page.route('**/api/server/status', (route) =>
			json(route, 200, {
				version: 'v0.8.0-test',
				commit: 'abc1234',
				started_at: new Date().toISOString(),
				uptime_sec: 60,
				database_ok: true,
				schema_version: 6,
				daemon_rss: 15 * 1048576,
				certificate_warnings: [
					'The certificate for shop.example.com expires in 6 days (3 Dec 2026) and has not been renewed.'
				]
			})
		)
		await page.route('**/api/settings/hook-domain', (route) =>
			json(route, 200, {
				hook_domain: 'hooks.example.com',
				server_ip: '203.0.113.10',
				setup_state: 'ready',
				dns_check: {
					domain: 'hooks.example.com',
					server_ip: '203.0.113.10',
					a_records: ['203.0.113.10'],
					aaaa_records: [],
					a_matches: true,
					aaaa_mismatch: false
				},
				certificate: {
					status: 'active',
					expires_at: '2027-02-25T04:31:50Z',
					expiring_soon: false
				}
			})
		)
		await page.setViewportSize({ width: 1280, height: 900 })
		await page.goto('/login')
		await page.fill('#admin-password', 'correct-password-123')
		await page.click('button[type="submit"]')
		await expect(page).toHaveURL(/\/$/)
		await expect(
			page.getByRole('alert').filter({ hasText: 'The certificate for' })
		).toContainText('The certificate for shop.example.com expires in 6 days')
		for (const theme of ['light', 'dark'] as const) {
			for (const width of [1280, 375]) {
				await page.setViewportSize({ width, height: 900 })
				await setTheme(page, theme)
				await assertAxeAA(
					page,
					`Dashboard certificate warning ${width} ${theme}`
				)
				await evidenceShot(
					page,
					`m8a-dashboard-cert-warning-${width}-${theme}.png`
				)
			}
		}
		await page.setViewportSize({ width: 1280, height: 900 })
		await setTheme(page, 'light')
		await page.goto('/settings?tab=hook-domain')
		await expect(
			page.getByText('HTTPS certificate active until 25 Feb 2027.')
		).toBeVisible()
		await assertAxeAA(page, 'Hook domain certificate')
		await evidenceShot(page, 'm8a-hook-domain-cert-1280-light.png')
	})
})
