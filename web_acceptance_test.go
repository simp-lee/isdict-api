package isdictapi

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

// AC-7: Web UI 中 CEFR 相关 JS/显示逻辑适配字符串类型
func TestWebUICEFRStringContract_UsesStringHelpersAndBindings(t *testing.T) {
	t.Helper()

	html, err := os.ReadFile("web/index.html")
	if err != nil {
		t.Fatalf("os.ReadFile(\"web/index.html\") error = %v", err)
	}

	source := string(html)

	requiredSnippets := []string{
		"normalizeCEFRLevel(level) {",
		"if (typeof level !== 'string') {",
		"const validLevels = ['A1', 'A2', 'B1', 'B2', 'C1', 'C2'];",
		"return validLevels.includes(normalized) ? normalized : '';",
		"hasCEFRLevel(level) {",
		"return this.normalizeCEFRLevel(level) !== '';",
		"getCEFRLevelText(level) {",
		"return this.normalizeCEFRLevel(level);",
		"getCEFRBadgeClass(level) {",
		"A1: 'bg-green-100 text-green-800'",
		"A2: 'bg-green-200 text-green-900'",
		"B1: 'bg-yellow-100 text-yellow-800'",
		"B2: 'bg-orange-100 text-orange-800'",
		"C1: 'bg-red-100 text-red-800'",
		"C2: 'bg-purple-100 text-purple-800'",
		"return classes[this.normalizeCEFRLevel(level)] || 'bg-slate-100 text-slate-600';",
		`x-show="hasCEFRLevel(item.cefr_level)" :class="getCEFRBadgeClass(item.cefr_level)" class="rounded-full px-2 py-0.5 font-medium" x-text="getCEFRLevelText(item.cefr_level)"`,
		`x-show="hasCEFRLevel(currentWord?.cefr_level)" :class="getCEFRBadgeClass(currentWord?.cefr_level)" class="level-badge bg-white border border-slate-200 text-slate-700" x-text="getCEFRLevelText(currentWord?.cefr_level)"`,
		`x-show="hasCEFRLevel(sense.cefr_level)" :class="getCEFRBadgeClass(sense.cefr_level)" class="rounded bg-white px-1.5 py-0.5 text-[10px] font-semibold" x-text="getCEFRLevelText(sense.cefr_level)"`,
	}

	for _, snippet := range requiredSnippets {
		if !strings.Contains(source, snippet) {
			t.Fatalf("web/index.html missing CEFR string-contract snippet %q", snippet)
		}
	}

	forbiddenSnippets := []string{
		"cefr_level > 0",
		"cefr_level >= 0",
		"['', 'A1', 'A2', 'B1', 'B2', 'C1', 'C2']",
	}

	for _, snippet := range forbiddenSnippets {
		if strings.Contains(source, snippet) {
			t.Fatalf("web/index.html still contains legacy CEFR int-contract snippet %q", snippet)
		}
	}

	forbiddenWholeKeyPatterns := map[string]string{
		`(?m)^\s*1\s*:\s*'bg-green-100 text-green-800'\s*,?\s*$`:   "1: 'bg-green-100 text-green-800'",
		`(?m)^\s*2\s*:\s*'bg-green-200 text-green-900'\s*,?\s*$`:   "2: 'bg-green-200 text-green-900'",
		`(?m)^\s*3\s*:\s*'bg-yellow-100 text-yellow-800'\s*,?\s*$`: "3: 'bg-yellow-100 text-yellow-800'",
		`(?m)^\s*4\s*:\s*'bg-orange-100 text-orange-800'\s*,?\s*$`: "4: 'bg-orange-100 text-orange-800'",
		`(?m)^\s*5\s*:\s*'bg-red-100 text-red-800'\s*,?\s*$`:       "5: 'bg-red-100 text-red-800'",
		`(?m)^\s*6\s*:\s*'bg-purple-100 text-purple-800'\s*,?\s*$`: "6: 'bg-purple-100 text-purple-800'",
	}

	for pattern, label := range forbiddenWholeKeyPatterns {
		if regexp.MustCompile(pattern).MatchString(source) {
			t.Fatalf("web/index.html still contains legacy CEFR int-contract snippet %q", label)
		}
	}
}

// AC-R027: highlightWord 必须先转义 HTML 再包裹高亮匹配词
func TestHighlightWordEscapesHTMLBeforeWrapping(t *testing.T) {
	// REG-027
	// Ledger Key: highlightWord | XSS | x-html | highlightWord 必须先转义 HTML 再包裹高亮匹配词

	html, err := os.ReadFile("web/index.html")
	if err != nil {
		t.Fatalf("os.ReadFile(\"web/index.html\") error = %v", err)
	}
	source := string(html)

	// 1. The highlightWord function must exist
	hlIdx := strings.Index(source, "highlightWord(text, word)")
	if hlIdx == -1 {
		t.Fatal("web/index.html does not contain highlightWord(text, word) function")
	}

	// Extract the function body (from the function signature to a reasonable length)
	fnBody := source[hlIdx:]
	// Find the end by matching balanced braces (approximate: take first ~60 lines)
	lines := strings.SplitN(fnBody, "\n", 60)
	if len(lines) > 55 {
		lines = lines[:55]
	}
	fnBody = strings.Join(lines, "\n")

	// 2. escapeHTML must be called on 'text' BEFORE any regex replacement
	escapeCallRe := regexp.MustCompile(`(?m)this\.escapeHTML\(text\)`)
	if !escapeCallRe.MatchString(fnBody) {
		t.Fatal("highlightWord does not call this.escapeHTML(text) — raw text would be injected into x-html (XSS)")
	}

	// 3. The escaped result (safeText) must be the variable used for .replace(), not raw 'text'
	replaceOnSafeRe := regexp.MustCompile(`safeText\.replace\(`)
	if !replaceOnSafeRe.MatchString(fnBody) {
		t.Fatal("highlightWord does not apply .replace() on the escaped safeText — highlight operates on raw text (XSS)")
	}

	// 4. The highlight replacement must produce only controlled <span> tags, not arbitrary HTML
	highlightTagRe := regexp.MustCompile(`<span class="highlight">`)
	if !highlightTagRe.MatchString(fnBody) {
		t.Fatal("highlightWord does not use <span class=\"highlight\"> for wrapping — unexpected highlight tag")
	}

	// 5. Ensure escapeHTML is called BEFORE the regex replace — escapeHTML call index must precede .replace( index
	escapePos := strings.Index(fnBody, "this.escapeHTML(text)")
	replacePos := strings.Index(fnBody, "safeText.replace(")
	if escapePos == -1 || replacePos == -1 || escapePos >= replacePos {
		t.Fatal("highlightWord: escapeHTML must be called BEFORE safeText.replace() to prevent XSS")
	}

	// 6. Verify that x-html bindings that use highlightWord exist (the attack surface)
	xhtmlHighlightRe := regexp.MustCompile(`x-html="highlightWord\(`)
	if !xhtmlHighlightRe.MatchString(source) {
		t.Fatal("web/index.html does not use x-html with highlightWord — expected attack surface binding")
	}

	// 7. Verify the escapeHTML function itself handles the critical characters
	escapeHTMLIdx := strings.Index(source, "escapeHTML(value)")
	if escapeHTMLIdx == -1 {
		t.Fatal("web/index.html does not contain escapeHTML(value) helper function")
	}
	escFnBody := source[escapeHTMLIdx:]
	escFnLines := strings.SplitN(escFnBody, "\n", 15)
	if len(escFnLines) > 12 {
		escFnLines = escFnLines[:12]
	}
	escFnBody = strings.Join(escFnLines, "\n")

	criticalEscapes := []string{
		`/&/g, '&amp;'`,
		`/</g, '&lt;'`,
		`/>/g, '&gt;'`,
		`/"/g, '&quot;'`,
	}
	for _, esc := range criticalEscapes {
		if !strings.Contains(escFnBody, esc) {
			t.Fatalf("escapeHTML is missing critical escape: %s", esc)
		}
	}
}
