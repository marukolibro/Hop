package handler

import (
	"slices"
	"testing"

	"hop/internal/model"
)

func TestSiteOf(t *testing.T) {
	for host, want := range map[string]string{
		"google.com":          "google.com",
		"www.google.com":      "google.com",
		"a.b.example.org":     "example.org",
		"www.bbc.co.uk":       "bbc.co.uk",
		"shop.example.com.au": "example.com.au",
		"localhost":           "localhost",
	} {
		if got := siteOf(host); got != want {
			t.Errorf("siteOf(%q) = %q, want %q", host, got, want)
		}
	}
}

func TestSuggestRules(t *testing.T) {
	got := suggestRules(model.Connection{Host: "api.telegram.org", Process: "Telegram"})
	values := make([]string, len(got))
	for i, s := range got {
		values[i] = s.value
	}
	if want := []string{"telegram.org", "api.telegram.org", "Telegram"}; !slices.Equal(values, want) {
		t.Errorf("domain connection: got %v, want %v", values, want)
	}
	if got := suggestRules(model.Connection{Host: "17.253.144.10"}); len(got) != 1 || got[0].value != "17.253.144.10/32" {
		t.Errorf("address connection: got %v", got)
	}
}
