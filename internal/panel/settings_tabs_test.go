package panel

import (
	"bytes"
	"strings"
	"testing"
)

const tabsTestPanelPath = "/p-x/"

// TestSettingsTabsNav verifies the shared Settings sub-navigation renders all
// four tabs, marks the active one, and carries the htmx attributes that make
// tab switching swap only the #settings-tabs panel (no full reload) while
// keeping a plain href for the no-JS / deep-link fallback.
func TestSettingsTabsNav(t *testing.T) {
	got := string(settingsTabsNav(tabsTestPanelPath, "system"))

	for _, label := range []string{"Endpoint &amp; Proxy", "Admin password", "System", "Two-factor"} {
		if !strings.Contains(got, label) {
			t.Errorf("settings tabs missing label %q in:\n%s", label, got)
		}
	}
	for _, attr := range []string{
		`hx-target="#settings-tabs"`,
		`hx-select="#settings-tabs"`,
		`hx-swap="outerHTML"`,
		`hx-push-url="true"`,
		`hx-get="/p-x/settings/system"`,
	} {
		if !strings.Contains(got, attr) {
			t.Errorf("settings tabs missing htmx attr %q in:\n%s", attr, got)
		}
	}
	if !strings.Contains(got, `<a class="seg-item active" href="/p-x/settings/system"`) {
		t.Errorf("System tab not marked active in:\n%s", got)
	}
	if strings.Contains(got, `class="seg-item active" href="/p-x/settings/proxy"`) {
		t.Errorf("only the active tab should carry the active class, got:\n%s", got)
	}
}

// TestSettingsPagesUseHtmxTabs ensures every Settings sub-page renders the
// swap container plus the htmx-enabled tab strip.
func TestSettingsPagesUseHtmxTabs(t *testing.T) {
	render := []struct {
		name string
		fn   func(*bytes.Buffer)
	}{
		{"proxy", func(b *bytes.Buffer) { proxySettingsPage(b, proxySettingsData{PanelPath: tabsTestPanelPath}) }},
		{"admin-password", func(b *bytes.Buffer) { adminPasswordPage(b, adminPasswordData{PanelPath: tabsTestPanelPath}) }},
		{"system", func(b *bytes.Buffer) { systemSettingsPage(b, systemSettingsData{PanelPath: tabsTestPanelPath}) }},
		{"totp", func(b *bytes.Buffer) { totpSettingsPage(b, totpSettingsData{PanelPath: tabsTestPanelPath}) }},
	}
	for _, tc := range render {
		var buf bytes.Buffer
		tc.fn(&buf)
		html := buf.String()
		if !strings.Contains(html, `id="settings-tabs"`) {
			t.Errorf("%s page missing #settings-tabs swap container", tc.name)
		}
		if !strings.Contains(html, `hx-select="#settings-tabs"`) {
			t.Errorf("%s page missing htmx tab navigation", tc.name)
		}
	}
}

// TestStubsPagesHaveNoForeignTabs ensures the Stubs section only links to its
// own tabs and no longer leaks into the Certificates / Settings sections.
func TestStubsPagesHaveNoForeignTabs(t *testing.T) {
	render := []struct {
		name string
		fn   func(*bytes.Buffer)
	}{
		{"stub-list", func(b *bytes.Buffer) { settingsStubListPage(b, settingsStubListData{PanelPath: tabsTestPanelPath}) }},
		{"stub-remote", func(b *bytes.Buffer) { settingsStubRemotePage(b, settingsStubRemoteData{PanelPath: tabsTestPanelPath}) }},
	}
	for _, tc := range render {
		var buf bytes.Buffer
		tc.fn(&buf)
		html := buf.String()
		// Scope checks to the seg tab strip; the global topbar always links
		// to every section and must not trip these assertions.
		if strings.Contains(html, `seg-item" href="/p-x/settings/certificates"`) {
			t.Errorf("%s tab strip should not link to certificates", tc.name)
		}
		if strings.Contains(html, `seg-item" href="/p-x/settings/proxy"`) {
			t.Errorf("%s tab strip should not link to proxy settings", tc.name)
		}
		if !strings.Contains(html, "/p-x/settings/stubs/remote") {
			t.Errorf("%s page lost its own Remote templates tab", tc.name)
		}
	}
}

// TestCertPageHasNoSettingsTabs ensures the Certificates page (a single view)
// no longer shows a cross-section tab strip.
func TestCertPageHasNoSettingsTabs(t *testing.T) {
	var buf bytes.Buffer
	settingsCertPage(&buf, settingsCertData{PanelPath: tabsTestPanelPath})
	html := buf.String()

	if strings.Contains(html, `aria-label="Certificate tabs"`) {
		t.Error("certificates page should not render a tab strip")
	}
	// The page body has no segmented control at all now (the topbar nav uses
	// the distinct .nav-item class, so this only catches an in-page tab strip).
	if strings.Contains(html, `class="seg-item`) {
		t.Error("certificates page should not contain any seg tab items")
	}
}
