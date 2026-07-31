package runtime

import "testing"

func TestModelCatalogProxyViewRedactsCredentials(t *testing.T) {
	view := modelCatalogProxyView("http://alice:secret@proxy.example:8080")
	if configured, _ := view["configured"].(bool); !configured {
		t.Fatalf("proxy should be configured: %#v", view)
	}
	if got := view["display_url"]; got != "http://proxy.example:8080" {
		t.Fatalf("display URL leaked or changed unexpectedly: %#v", got)
	}
}

func TestModelCatalogProxyViewEmpty(t *testing.T) {
	view := modelCatalogProxyView("")
	if configured, _ := view["configured"].(bool); configured {
		t.Fatalf("empty proxy should not be configured: %#v", view)
	}
}
