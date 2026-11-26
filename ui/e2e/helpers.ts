import type { Page } from '@playwright/test'

// Theme toggles and state changes start colour transitions; axe measuring mid-transition reports
// contrast between two half-blended colours. Infinite animations (pulse) are not transitions.
export async function settleTransitions(page: Page): Promise<void> {
	await page.evaluate(() =>
		Promise.all(
			document
				.getAnimations()
				.filter((a) => a instanceof CSSTransition)
				.map((a) =>
					a.finished.then(
						() => undefined,
						() => undefined // cancelled transition: nothing left to wait for
					)
				)
		)
	)
}
