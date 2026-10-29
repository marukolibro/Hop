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
		if err != nil || got != c.want {
			t.Errorf("%s: got %v, %v; want state %v", c.name, got, err, c.want)
		}
	}
	if _, err := ParseChatGPT(502, nil); err == nil {
		t.Error("a 502 told something")
	}
}

func TestParseYouTube(t *testing.T) {
	state, region, err := ParseYouTube([]byte(`ytcfg.set({"GL":"SG","HL":"en"})`))
	if err != nil || state != model.UnlockAvailable || region != "SG" {
		t.Errorf("a page for SG: got %v %q, %v", state, region, err)
	}
	state, _, err = ParseYouTube([]byte(`<h1>YouTube Premium is not available in your country</h1>`))
	if err != nil || state != model.UnlockBlocked {
		t.Errorf("a refusal: got %v, %v", state, err)
	}
	if _, _, err := ParseYouTube([]byte(`<html></html>`)); err == nil {
		t.Error("a page with no region and no refusal told something")
	}
}

func TestParsePrime(t *testing.T) {
	state, region, err := ParsePrime([]byte(`{"currentTerritory":"SG","x":1}`))
	if err != nil || state != model.UnlockAvailable || region != "SG" {
		t.Errorf("a page for SG: got %v %q, %v", state, region, err)
	}
	state, _, err = ParsePrime([]byte(`{"isServiceRestricted":true}`))
	if err != nil || state != model.UnlockBlocked {
		t.Errorf("a restricted page: got %v, %v", state, err)
	}
	if _, _, err := ParsePrime([]byte(`<html></html>`)); err == nil {
		t.Error("a page with no territory told something")
	}
}

func TestTraceRegion(t *testing.T) {
	got, err := TraceRegion([]byte("fl=1\nip=192.0.2.1\nloc=US\ntls=TLSv1.3\n"))
	if err != nil || got != "US" {
		t.Errorf("a trace from US: got %q, %v", got, err)
	}
	for _, body := range []string{"ip=192.0.2.1\n", "loc=XX\n"} {
		if _, err := TraceRegion([]byte(body)); err == nil {
			t.Errorf("trace %q named a country", body)
		}
	}
}

func TestParseGeminiRegion(t *testing.T) {
	got, err := ParseGeminiRegion([]byte(`[null,2,1,200,"HKG"]`))
	if err != nil || got != "HK" {
		t.Errorf("a page for HKG: got %q, %v", got, err)
	}
	if regionState(got) != model.UnlockBlocked || regionState("US") != model.UnlockAvailable {
		t.Error("Hong Kong is offered, or the US is not")
	}
	if _, err := ParseGeminiRegion([]byte(`<html></html>`)); err == nil {
		t.Error("a page with no region told something")
	}
}

func TestPathRegion(t *testing.T) {
	for path, want := range map[string]string{"/sg/title/81280792": "SG", "/gb/title/1": "GB", "/title/81280792": "", "/sg-en/premium/": "SG", "/": ""} {
		if got := pathRegion(&url.URL{Path: path}); got != want {
			t.Errorf("pathRegion(%q) = %q, want %q", path, got, want)
		}
	}
}
