// Package codegen turns a recorded action log into a runnable Playwright script
// in Python (sync API) or JavaScript (ESM).
package codegen

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	"testdesk/internal/browser"
	"testdesk/internal/scripts"
)

// Action is one successful browser step as recorded by the agent.
type Action struct {
	Tool    string           // navigate | click | fill | press | select_option | wait_for | assert_visible | assert_text
	URL     string           // navigate
	Value   string           // fill, select_option
	Key     string           // press
	Text    string           // assert_text
	Note    string           // comment placed above the step
	Locator *browser.Locator // nil for navigate
}

type Script struct {
	Name    string
	Goal    string
	URL     string // start URL; used when the log has no navigate step
	Actions []Action
}

// Generate renders s in the given language.
func Generate(lang scripts.Language, s Script) (string, error) {
	actions := s.Actions
	if s.URL != "" && (len(actions) == 0 || actions[0].Tool != "navigate") {
		actions = append([]Action{{Tool: "navigate", URL: s.URL}}, actions...)
	}

	switch lang {
	case scripts.Python:
		return genPython(s, actions), nil
	case scripts.JavaScript:
		return genJavaScript(s, actions), nil
	default:
		return "", fmt.Errorf("unsupported language: %s", lang)
	}
}

// quote returns s as a string literal. JSON strings are valid in both Python and JavaScript.
func quote(s string) string {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(s)
	return strings.TrimSuffix(b.String(), "\n")
}

func oneLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

func genPython(s Script, actions []Action) string {
	var b strings.Builder
	writeHeader(&b, "#", s)
	b.WriteString("from playwright.sync_api import sync_playwright, expect\n\n\n")
	b.WriteString("def run(p):\n")
	b.WriteString("    browser = p.chromium.launch(headless=False)\n")
	b.WriteString("    page = browser.new_page()\n")
	b.WriteString("    try:\n")
	lines := []string{}
	for _, a := range actions {
		lines = append(lines, pyStep(a)...)
	}
	if len(lines) == 0 {
		lines = []string{"pass"}
	}
	for _, l := range lines {
		b.WriteString("        " + l + "\n")
	}
	b.WriteString("    finally:\n")
	b.WriteString("        browser.close()\n\n\n")
	b.WriteString("if __name__ == \"__main__\":\n")
	b.WriteString("    with sync_playwright() as p:\n")
	b.WriteString("        run(p)\n")
	return b.String()
}

func genJavaScript(s Script, actions []Action) string {
	var b strings.Builder
	writeHeader(&b, "//", s)
	b.WriteString("import { chromium } from 'playwright'\n")
	b.WriteString("import { expect } from '@playwright/test'\n\n")
	b.WriteString("async function run() {\n")
	b.WriteString("  const browser = await chromium.launch({ headless: false })\n")
	b.WriteString("  const page = await browser.newPage()\n")
	b.WriteString("  try {\n")
	for _, a := range actions {
		for _, l := range jsStep(a) {
			b.WriteString("    " + l + "\n")
		}
	}
	b.WriteString("  } finally {\n")
	b.WriteString("    await browser.close()\n")
	b.WriteString("  }\n")
	b.WriteString("}\n\n")
	b.WriteString("run().catch((err) => {\n")
	b.WriteString("  console.error(err)\n")
	b.WriteString("  process.exitCode = 1\n")
	b.WriteString("})\n")
	return b.String()
}

func writeHeader(b *strings.Builder, comment string, s Script) {
	fmt.Fprintf(b, "%s 由 TestDesk 录制生成\n", comment)
	if s.Name != "" {
		fmt.Fprintf(b, "%s 名称：%s\n", comment, oneLine(s.Name))
	}
	if s.Goal != "" {
		fmt.Fprintf(b, "%s 目标：%s\n", comment, oneLine(s.Goal))
	}
	b.WriteString("\n")
}

// pyLocator renders a locator as a Python expression.
func pyLocator(l *browser.Locator) string {
	if l == nil {
		return `page.locator("body")`
	}
	switch l.By {
	case "role":
		expr := fmt.Sprintf("page.get_by_role(%s", quote(l.Role))
		if l.Name != "" {
			expr += fmt.Sprintf(", name=%s, exact=True", quote(l.Name))
		}
		expr += ")"
		if l.Nth != nil {
			expr += fmt.Sprintf(".nth(%d)", *l.Nth)
		}
		return expr
	case "text":
		return fmt.Sprintf("page.get_by_text(%s).first", quote(l.Text))
	default:
		return fmt.Sprintf("page.locator(%s)", quote(l.Selector))
	}
}

// jsLocator renders a locator as a JavaScript expression.
func jsLocator(l *browser.Locator) string {
	if l == nil {
		return `page.locator("body")`
	}
	switch l.By {
	case "role":
		expr := fmt.Sprintf("page.getByRole(%s", quote(l.Role))
		if l.Name != "" {
			expr += fmt.Sprintf(", { name: %s, exact: true }", quote(l.Name))
		}
		expr += ")"
		if l.Nth != nil {
			expr += fmt.Sprintf(".nth(%d)", *l.Nth)
		}
		return expr
	case "text":
		return fmt.Sprintf("page.getByText(%s).first()", quote(l.Text))
	default:
		return fmt.Sprintf("page.locator(%s)", quote(l.Selector))
	}
}

func pyStep(a Action) []string {
	loc := pyLocator(a.Locator)
	var lines []string
	if a.Note != "" {
		lines = append(lines, "# "+oneLine(a.Note))
	}
	switch a.Tool {
	case "navigate":
		lines = append(lines, fmt.Sprintf("page.goto(%s)", quote(a.URL)))
	case "click":
		lines = append(lines, loc+".click()")
	case "fill":
		lines = append(lines, fmt.Sprintf("%s.fill(%s)", loc, quote(a.Value)))
	case "press":
		lines = append(lines, fmt.Sprintf("%s.press(%s)", loc, quote(a.Key)))
	case "select_option":
		lines = append(lines, fmt.Sprintf("%s.select_option(%s)", loc, quote(a.Value)))
	case "wait_for", "assert_visible":
		lines = append(lines, fmt.Sprintf("expect(%s).to_be_visible()", loc))
	case "assert_text":
		lines = append(lines, fmt.Sprintf("expect(%s).to_contain_text(%s)", loc, quote(a.Text)))
	}
	return lines
}

func jsStep(a Action) []string {
	loc := jsLocator(a.Locator)
	var lines []string
	if a.Note != "" {
		lines = append(lines, "// "+oneLine(a.Note))
	}
	switch a.Tool {
	case "navigate":
		lines = append(lines, fmt.Sprintf("await page.goto(%s)", quote(a.URL)))
	case "click":
		lines = append(lines, fmt.Sprintf("await %s.click()", loc))
	case "fill":
		lines = append(lines, fmt.Sprintf("await %s.fill(%s)", loc, quote(a.Value)))
	case "press":
		lines = append(lines, fmt.Sprintf("await %s.press(%s)", loc, quote(a.Key)))
	case "select_option":
		lines = append(lines, fmt.Sprintf("await %s.selectOption(%s)", loc, quote(a.Value)))
	case "wait_for", "assert_visible":
		lines = append(lines, fmt.Sprintf("await expect(%s).toBeVisible()", loc))
	case "assert_text":
		lines = append(lines, fmt.Sprintf("await expect(%s).toContainText(%s)", loc, quote(a.Text)))
	}
	return lines
}
