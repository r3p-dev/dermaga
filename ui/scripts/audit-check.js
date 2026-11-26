#!/usr/bin/env bun
// audit-check.js verifies bun audit results against known accepted advisories (docs/DEPENDENCIES.md §6).
// Fails on any unexpected or new advisory.

import { spawnSync } from 'child_process'

const proc = spawnSync('bun', ['audit', '--json'], { encoding: 'utf8' })
const raw = (proc.stdout || '').trim()

if (!raw) {
	process.exit(0)
}

let data
try {
	data = JSON.parse(raw)
} catch (e) {
	console.error('Failed to parse bun audit JSON:', e, raw)
	process.exit(1)
}

// Documented in docs/DEPENDENCIES.md §6: deepmerge prototype poisoning via @sveltejs/vite-plugin-svelte
const allowedUrls = new Set([
	'https://github.com/advisories/GHSA-gcqx-w48m-w2h4'
])

const unexpected = []
for (const [pkg, advisories] of Object.entries(data)) {
	for (const adv of advisories) {
		if (!allowedUrls.has(adv.url)) {
			unexpected.push({ pkg, ...adv })
		}
	}
}

if (unexpected.length > 0) {
	console.error('FAIL: bun audit found unexpected advisories:')
	console.error(JSON.stringify(unexpected, null, 2))
	process.exit(1)
}

console.log(
	'bun audit passed (only documented advisory GHSA-gcqx-w48m-w2h4 present)'
)
process.exit(0)
