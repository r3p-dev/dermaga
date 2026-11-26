import { readdirSync, readFileSync, statSync } from 'node:fs'
import { join, relative } from 'node:path'
import { __unstable__loadDesignSystem } from '@tailwindcss/node'
import { Scanner } from '@tailwindcss/oxide'

const uiDir = join(import.meta.dirname, '..')
const srcDir = join(uiDir, 'src')
const cssPath = join(srcDir, 'app.css')

function walk(dir) {
	const out = []
	for (const entry of readdirSync(dir)) {
		const full = join(dir, entry)
		if (statSync(full).isDirectory()) {
			out.push(...walk(full))
		} else if (full.endsWith('.svelte') || full.endsWith('.ts')) {
			out.push(full)
		}
	}
	return out
}

const css = readFileSync(cssPath, 'utf8')
if (typeof __unstable__loadDesignSystem !== 'function') {
	console.error(
		'check-tailwind: @tailwindcss/node.__unstable__loadDesignSystem is missing; update scripts/check-tailwind.js for this Tailwind version.'
	)
	process.exit(1)
}
if (typeof Scanner !== 'function') {
	console.error(
		'check-tailwind: @tailwindcss/oxide.Scanner is missing; update scripts/check-tailwind.js for this Tailwind version.'
	)
	process.exit(1)
}
const ds = await __unstable__loadDesignSystem(css, { base: srcDir })
if (
	typeof ds?.canonicalizeCandidates !== 'function' ||
	typeof ds?.candidatesToCss !== 'function'
) {
	console.error(
		'check-tailwind: Tailwind designSystem.canonicalizeCandidates or candidatesToCss is missing; update scripts/check-tailwind.js for this Tailwind version.'
	)
	process.exit(1)
}
const scanner = new Scanner({
	sources: [{ base: srcDir, pattern: '**/*', negated: false }]
})
const candidates = scanner.scan()
const canonical = ds.canonicalizeCandidates(candidates)

const nonCanonical = new Map()
for (let i = 0; i < candidates.length; i++) {
	if (canonical[i] !== candidates[i]) {
		nonCanonical.set(candidates[i], canonical[i])
	}
}

const svelteChecks = [
	{
		name: 'legacy Svelte 4 syntax or deprecated API',
		re: /\bexport\s+let\b|\bcreateEventDispatcher\b|\$\$props\b|\$\$restProps\b|<slot[\s>]|^\s*\$:\s|\bon:(?:click|submit|change|input|keydown|keyup)=/gm,
		hint: 'Use Svelte 5 runes ($props, $derived, $effect) and snippets/onclick attributes.'
	}
]

function extractCssProperties(cssText) {
	const props = new Set()
	if (!cssText) return props
	// Strip @property blocks; inspect only the innermost declaration block of the utility rule
	const withoutAtProp = cssText.replace(/@property\s+[^{]+\{[^}]*\}/g, '')
	const blockMatches = withoutAtProp.match(/\{([^{}]+)\}/g)
	if (!blockMatches) return props
	for (const block of blockMatches) {
		const inner = block.slice(1, -1)
		for (const decl of inner.split(';')) {
			const colon = decl.indexOf(':')
			if (colon === -1) continue
			const prop = decl.slice(0, colon).trim()
			if (prop && !prop.startsWith('--')) {
				props.add(prop)
			}
		}
	}
	return props
}

const allowedOverrides = new Set([
	'border-style', // e.g. "border border-dashed"
	'line-height' // e.g. "text-xs leading-relaxed"
])

let failures = 0
const files = walk(srcDir)

for (const file of files) {
	const content = readFileSync(file, 'utf8')
	const lines = content.split('\n')
	const rel = relative(process.cwd(), file)

	for (let idx = 0; idx < lines.length; idx++) {
		const line = lines[idx]

		// 1. Report any Tailwind v4 non-canonical class found by ds.canonicalizeCandidates
		if (nonCanonical.size > 0) {
			for (const [bad, good] of nonCanonical) {
				const tokenRe = new RegExp(
					`(?<=["'\\s])${bad.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')}(?=["'\\s])`
				)
				if (tokenRe.test(line)) {
					console.error(
						`${rel}:${idx + 1}: suggestCanonicalClasses: "${bad}" -> "${good}"`
					)
					failures++
				}
			}
		}

		// 2. Check cssConflict on static class="..." attributes without dynamic expressions
		const classAttrRe = /\bclass="([^"{}]+)"/g
		let attrMatch
		while ((attrMatch = classAttrRe.exec(line)) !== null) {
			const tokens = attrMatch[1].trim().split(/\s+/).filter(Boolean)
			if (tokens.length < 2) continue
			const compiled = ds.candidatesToCss(tokens)
			const seenByProp = new Map()
			for (let t = 0; t < tokens.length; t++) {
				const token = tokens[t]
				// Group by variant prefix (e.g. "md:", "dark:", or "")
				const lastColon = token.lastIndexOf(':')
				const variant = lastColon === -1 ? '' : token.slice(0, lastColon)
				for (const prop of extractCssProperties(compiled[t])) {
					if (allowedOverrides.has(prop)) continue
					const key = `${variant}::${prop}`
					const prev = seenByProp.get(key)
					if (prev && prev !== token) {
						console.error(
							`${rel}:${idx + 1}: cssConflict: "${prev}" and "${token}" both set "${prop}"`
						)
						failures++
					} else {
						seenByProp.set(key, token)
					}
				}
			}
		}

		// 3. Legacy Svelte 4 / deprecated APIs
		for (const check of svelteChecks) {
			check.re.lastIndex = 0
			const match = check.re.exec(line)
			if (match) {
				console.error(
					`${rel}:${idx + 1}: ${check.name}: "${match[0]}" — ${check.hint}`
				)
				failures++
			}
		}
	}
}

if (failures > 0) {
	process.exit(1)
}
console.log(
	`tailwind & svelte hygiene: 0 issues (${candidates.length} candidates checked)`
)
