package unlock

import (
	"net/url"
	"testing"

	"hop/internal/model"
)

func TestParseChatGPT(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
		want   model.UnlockState
	}{
		{"served", 200, `{"cookie_consent_required": false}`, model.UnlockAvailable},
		{"refused by country", 403, `{"error":{"code":"unsupported_country"}}`, model.UnlockBlocked},
		{"refused with a bare 403", 403, `blocked`, model.UnlockBlocked},
	}
	for _, c := range cases {
		got, err := ParseChatGPT(c.status, []byte(c.body))
		if err != nil || got.State != c.want {
			t.Errorf("%s: got %+v, %v; want state %v", c.name, got, err, c.want)
		}
	}
	if _, err := ParseChatGPT(502, nil); err == nil {
		t.Error("a 502 told something")
	}
}

func TestParseYouTube(t *testing.T) {
	got, err := ParseYouTube([]byte(`ytcfg.set({"GL":"SG","HL":"en"})`))
	if err != nil || got.State != model.UnlockAvailable || got.Region != "SG" {
		t.Errorf("a page for SG: got %+v, %v", got, err)
	}
	got, err = ParseYouTube([]byte(`<h1>YouTube Premium is not available in your country</h1>`))
	if err != nil || got.State != model.UnlockBlocked {
		t.Errorf("a refusal: got %+v, %v", got, err)
	}
	if _, err := ParseYouTube([]byte(`<html></html>`)); err == nil {
		t.Error("a page with no region and no refusal told something")
	}
}

func TestNetflixRegion(t *testing.T) {
	for path, want := range map[string]string{"/sg/title/81280792": "SG", "/gb/title/1": "GB", "/title/81280792": ""} {
		if got := NetflixRegion(&url.URL{Path: path}); got != want {
			t.Errorf("NetflixRegion(%q) = %q, want %q", path, got, want)
		}
	}
}
