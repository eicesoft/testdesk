package codegen

import (
	"strings"
	"testing"

	"testdesk/internal/browser"
	"testdesk/internal/scripts"
)

func sampleScript() Script {
	zero := 0
	return Script{
		Name: "login",
		Goal: "用户可以登录",
		URL:  "https://example.com/login",
		Actions: []Action{
			{Tool: "fill", Value: "alice", Note: "输入用户名", Locator: &browser.Locator{By: "role", Role: "textbox", Name: "用户名"}},
			{Tool: "click", Note: "提交", Locator: &browser.Locator{By: "role", Role: "button", Name: "登录", Nth: &zero}},
			{Tool: "assert_text", Text: "欢迎", Locator: &browser.Locator{By: "text", Text: "欢迎"}},
			{Tool: "assert_visible", Locator: &browser.Locator{By: "css", Selector: "#main > div"}},
		},
	}
}

func TestGeneratePythonContainsSteps(t *testing.T) {
	out, err := Generate(scripts.Python, sampleScript())
	if err != nil {
		t.Fatal(err)
	}
	wants := []string{
		"from playwright.sync_api import sync_playwright, expect",
		`page.goto("https://example.com/login")`,
		`page.get_by_role("textbox", name="用户名", exact=True).fill("alice")`,
		`page.get_by_role("button", name="登录", exact=True).nth(0).click()`,
		`expect(page.get_by_text("欢迎").first).to_contain_text("欢迎")`,
		`expect(page.locator("#main > div")).to_be_visible()`,
		"# 输入用户名",
	}
	for _, w := range wants {
		if !strings.Contains(out, w) {
			t.Errorf("python output missing %q\n---\n%s", w, out)
		}
	}
}

func TestGenerateJavaScriptContainsSteps(t *testing.T) {
	out, err := Generate(scripts.JavaScript, sampleScript())
	if err != nil {
		t.Fatal(err)
	}
	wants := []string{
		"import { chromium } from 'playwright'",
		"import { expect } from '@playwright/test'",
		`await page.goto("https://example.com/login")`,
		`await page.getByRole("textbox", { name: "用户名", exact: true }).fill("alice")`,
		`await page.getByRole("button", { name: "登录", exact: true }).nth(0).click()`,
		`await expect(page.getByText("欢迎").first()).toContainText("欢迎")`,
		`await expect(page.locator("#main > div")).toBeVisible()`,
	}
	for _, w := range wants {
		if !strings.Contains(out, w) {
			t.Errorf("javascript output missing %q\n---\n%s", w, out)
		}
	}
}

func TestQuoteKeepsHTMLCharactersReadable(t *testing.T) {
	if got := quote(`a<b>&"c"`); got != `"a<b>&\"c\""` {
		t.Errorf("quote = %s", got)
	}
}

func TestEmptyPythonBodyIsValid(t *testing.T) {
	out, err := Generate(scripts.Python, Script{Name: "empty"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "        pass\n") {
		t.Errorf("expected a pass statement in an empty script:\n%s", out)
	}
}

func TestGenerateRejectsUnknownLanguage(t *testing.T) {
	if _, err := Generate("ruby", sampleScript()); err == nil {
		t.Error("expected an error for an unsupported language")
	}
}
