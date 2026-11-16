#!/usr/bin/env python3
"""
scripts/check_contrast.py - Computes WCAG 2.1 Level AA contrast ratios for brand theme tokens.
Verifies all text/background and focus-ring pairs in light and dark themes.
"""

def relative_luminance(hex_str: str) -> float:
    hex_clean = hex_str.lstrip("#")
    r, g, b = [int(hex_clean[i:i+2], 16) / 255.0 for i in (0, 2, 4)]
    
    def adjust(c: float) -> float:
        return c / 12.92 if c <= 0.03928 else ((c + 0.055) / 1.055) ** 2.4

    r_adj, g_adj, b_adj = adjust(r), adjust(g), adjust(b)
    return 0.2126 * r_adj + 0.7152 * g_adj + 0.0722 * b_adj


def contrast_ratio(hex1: str, hex2: str) -> float:
    l1 = relative_luminance(hex1)
    l2 = relative_luminance(hex2)
    lighter = max(l1, l2)
    darker = min(l1, l2)
    return (lighter + 0.05) / (darker + 0.05)


def evaluate_pair(name: str, fg_hex: str, bg_hex: str, min_ratio: float, category: str):
    cr = contrast_ratio(fg_hex, bg_hex)
    passed = cr >= min_ratio
    status = "PASS (AA)" if passed else "FAIL (AA)"
    level = "AAA" if cr >= 7.0 else ("AA" if cr >= min_ratio else "FAIL")
    return {
        "name": name,
        "fg": fg_hex,
        "bg": bg_hex,
        "ratio": cr,
        "min": min_ratio,
        "passed": passed,
        "status": status,
        "level": level,
        "category": category,
    }


def main():
    print("=== Dermaga Brand Palette WCAG 2.1 Contrast Analysis (NFR-7) ===")
    print("Logo Source Colors:")
    print("  #1A1B36 : Logo Deep Navy")
    print("  #5B58A8 : Logo Slate Indigo")
    print("  #7C78EE : Logo Vibrant Periwinkle")
    print("  #C9C5FB : Logo Lavender Accent")
    print("")

    pairs = [
        # Light Theme
        ("Primary Button Text (Light)", "#FFFFFF", "#5B58A8", 4.5, "Text (Normal)"),
        ("Primary Button Hover Text (Light)", "#FFFFFF", "#4A478A", 4.5, "Text (Normal)"),
        ("Primary Button Active Text (Light)", "#FFFFFF", "#3C3972", 4.5, "Text (Normal)"),
        ("Focus Ring vs White Bg (Light)", "#7C78EE", "#FFFFFF", 3.0, "UI Component"),
        ("Focus Ring vs Stone-50 Bg (Light)", "#7C78EE", "#FAFAF9", 3.0, "UI Component"),
        ("Brand Text vs White Card (Light)", "#5B58A8", "#FFFFFF", 4.5, "Text (Normal)"),
        ("Origin Badge Text vs Badge Bg (Light)", "#4F4B9B", "#F4F3FE", 4.5, "Text (Normal)"),
        ("Origin Badge Text vs Card Bg (Light)", "#4F4B9B", "#FFFFFF", 4.5, "Text (Normal)"),

        # Dark Theme
        ("Primary Button Text (Dark, Navy on Periwinkle)", "#1A1B36", "#7C78EE", 4.5, "Text (Normal)"),
        ("Primary Button Hover Text (Dark)", "#1A1B36", "#8D8AF1", 4.5, "Text (Normal)"),
        ("Primary Button Active Text (Dark)", "#1A1B36", "#9E9BF4", 4.5, "Text (Normal)"),
        ("Focus Ring vs Stone-900 Bg (Dark)", "#7C78EE", "#1C1917", 3.0, "UI Component"),
        ("Focus Ring vs Stone-950 Bg (Dark)", "#7C78EE", "#0C0A09", 3.0, "UI Component"),
        ("Brand Text vs Stone-900 Card (Dark)", "#C9C5FB", "#1C1917", 4.5, "Text (Normal)"),
        ("Brand Text vs Stone-950 Bg (Dark)", "#C9C5FB", "#0C0A09", 4.5, "Text (Normal)"),
        ("Origin Badge Text vs Badge Bg (Dark)", "#C9C5FB", "#1A1B36", 4.5, "Text (Normal)"),
        ("Origin Badge Text vs Card Bg (Dark)", "#C9C5FB", "#1C1917", 4.5, "Text (Normal)"),
    ]

    all_passed = True
    print(f"{'Token / Usage Pair':<42} | {'Foreground':<10} | {'Background':<10} | {'Ratio':<7} | {'Required':<8} | {'Status'}")
    print("-" * 95)

    for item in pairs:
        res = evaluate_pair(*item)
        if not res["passed"]:
            all_passed = False
        print(f"{res['name']:<42} | {res['fg']:<10} | {res['bg']:<10} | {res['ratio']:>5.2f}:1 | >={res['min']:>4.1f}:1  | {res['status']} ({res['level']})")

    print("-" * 95)
    if all_passed:
        print("RESULT: ALL PAIRS PASS WCAG 2.1 LEVEL AA CONTRAST CRITERIA (NFR-7)")
    else:
        print("RESULT: ONE OR MORE PAIRS FAILED WCAG 2.1 LEVEL AA")
        exit(1)


if __name__ == "__main__":
    main()
