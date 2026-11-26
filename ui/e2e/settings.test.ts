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

test.describe('Settings & Narrow Audit Log (M4b: LAP-3, ADM-4, ADM-5, SEC-10, CONVENTIONS §5.2)', () => {
	test('Settings tabs (Access, Security, Hook Domain) and narrow Audit Log stacked rows', async ({
		page
	}) => {
		await page.setViewportSize({ width: 1280, height: 800 })
		await page.goto('/login')
		await page.fill('#admin-password', 'correct-password-123')
		await page.click('button[type="submit"]')
		await expect(page).toHaveURL(/\/$/)

		// Navigate to Settings -> Access
		await page.locator('a:has-text("Settings"):visible').click()
		await expect(page).toHaveURL(/\/settings/)
		await expect(page.locator('h1')).toHaveText('Settings')
		await expect(page.locator('text=Active Sessions')).toBeVisible()
		await expect(page.locator('text=Current')).toBeVisible()
		await assertAxeAA(page, 'Settings Access tab (light, desktop)')
		await evidenceShot(page, 'm4b-settings-access-light-desktop.png')

		// Settings -> Security
		await page.click('button[role="tab"]:has-text("Security")')
		await expect(page.locator('text=Change Admin Password')).toBeVisible()
		await assertAxeAA(page, 'Settings Security tab (light, desktop)')
		await evidenceShot(page, 'm4b-settings-security-light-desktop.png')

		// Settings -> Hook Domain (dark mode)
		await page.locator('button[aria-label*="Toggle theme"]:visible').click()
		await page.click('button[role="tab"]:has-text("Hook Domain")')
		await expect(page.locator('#hook-domain-input')).toBeVisible()
		await assertAxeAA(page, 'Settings Hook Domain tab (dark, desktop)')
		await evidenceShot(page, 'm4b-settings-hook-domain-dark-desktop.png')

		// Narrow Activity stacked rows (375x812, no horizontal scroll)
		await page.setViewportSize({ width: 375, height: 812 })
		await page.goto('/activity')
		await expect(
			page.locator('div[aria-label="Activity list"] article').first()
		).toBeVisible()
		const hasOverflow = await page.evaluate(
			() => document.documentElement.scrollWidth > window.innerWidth
		)
		expect(hasOverflow).toBe(false)
		await assertAxeAA(page, 'Audit Log stacked rows (dark, narrow)')
		await evidenceShot(page, 'm4b-audit-logs-narrow-dark.png')
	})
})
