package runtime

import (
	"strings"
	"testing"
)

func TestDashboardExportsClickableActions(t *testing.T) {
	html := dashboardHTML()
	for _, required := range []string{
		"window.configureProvider = async function",
		"window.testProvider = async function",
		"window.loadAll = async function",
		"onclick=\"configureProvider()\"",
		"onclick=\"testProvider()\"",
		"parseResponse(resp)",
	} {
		if !strings.Contains(html, required) {
			t.Fatalf("dashboard missing %q", required)
		}
	}
	for _, broken := range []string{"window.token=function(){", "const body=await parse(resp)", "catch(e){print(e)}"} {
		if strings.Contains(html, broken) {
			t.Fatalf("dashboard still contains broken script fragment %q", broken)
		}
	}
}
