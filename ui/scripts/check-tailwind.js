import { readdirSync, readFileSync, statSync } from "node:fs";
import { join, relative } from "node:path";

const srcDir = join(import.meta.dirname, "..", "src");

function walk(dir) {
  const out = [];
  for (const entry of readdirSync(dir)) {
    const full = join(dir, entry);
    if (statSync(full).isDirectory()) {
      out.push(...walk(full));
    } else if (full.endsWith(".svelte") || full.endsWith(".ts")) {
      out.push(full);
    }
  }
  return out;
}

const checks = [
  {
    name: "deprecated Tailwind v3 utility",
    re: /\b(?:flex-shrink-(?:0|\d+)|flex-grow-(?:0|\d+)|bg-gradient-to-[trbl]+|overflow-ellipsis|decoration-(?:slice|clone))\b/g,
    hint: "Use Tailwind v4 canonical classes (shrink-*, grow-*, bg-linear-to-*, text-ellipsis, box-decoration-*).",
  },
  {
    name: "non-canonical width/height pair (use size-*)",
    re: /\bw-([0-9.]+)\s+h-\1\b|\bh-([0-9.]+)\s+w-\2\b/g,
    hint: "Replace matching w-X h-X with size-X.",
  },
  {
    name: "arbitrary spacing/size with canonical Tailwind v4 equivalent",
    re: /\b(?:w|h|min-w|min-h|max-w|max-h|size|p|px|py|pt|pb|pl|pr|m|mx|my|mt|mb|ml|mr|gap|space-x|space-y)-\[(?:1px|[0-9]+(?:\.[0-9]+)?rem)\]/g,
    hint: "Use canonical Tailwind v4 spacing/size utilities (e.g. h-px, max-h-112) instead of arbitrary values.",
  },
  {
    name: "legacy Svelte 4 syntax or deprecated API",
    re: /\bexport\s+let\b|\bcreateEventDispatcher\b|\$\$props\b|\$\$restProps\b|<slot[\s>]|^\s*\$:\s|\bon:(?:click|submit|change|input|keydown|keyup)=/gm,
    hint: "Use Svelte 5 runes ($props, $derived, $effect) and snippets/onclick attributes.",
  },
];

let failures = 0;
for (const file of walk(srcDir)) {
  const content = readFileSync(file, "utf8");
  const lines = content.split("\n");
  for (let idx = 0; idx < lines.length; idx++) {
    const line = lines[idx];
    for (const check of checks) {
      check.re.lastIndex = 0;
      const match = check.re.exec(line);
      if (match) {
        console.error(
          `${relative(process.cwd(), file)}:${idx + 1}: ${check.name}: "${match[0]}" — ${check.hint}`,
        );
        failures++;
      }
    }
  }
}

if (failures > 0) {
  process.exit(1);
}
console.log("tailwind & svelte hygiene: 0 issues");
