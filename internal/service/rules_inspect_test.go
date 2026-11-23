package service

import (
	"slices"
	"testing"

	"hop/internal/model"
)

func TestDiagnoseRule(t *testing.T) {
	rules := []model.ActiveRule{
		{Index: 0, Type: "Domain", Payload: "exact.example.com", Proxy: "DIRECT"},
		{Index: 1, Type: "DomainSuffix", Payload: "openai.com", Proxy: "AI"},
		{Index: 2, Type: "IPCIDR", Payload: "192.168.0.0/16", Proxy: "DIRECT"},
		{Index: 3, Type: "GeoSite", Payload: "cn", Proxy: "DIRECT"},
		{Index: 4, Type: "Match", Payload: "", Proxy: "节点选择"},
	}
	groups := []model.GroupView{
		{Name: "AI", Selected: "🇺🇸 美国"},
		{Name: "🇺🇸 美国", Selected: "🇺🇸 US 01"},
		{Name: "节点选择", Selected: "🇭🇰 HK 01"},
	}

	t.Run("strips url scheme path and port", func(t *testing.T) {
		got := DiagnoseRule("https://api.openai.com:443/v1/chat", rules, groups)
		if !got.Matched || got.Rule.Index != 1 {
			t.Fatalf("matched = %v index = %d, want index 1", got.Matched, got.Rule.Index)
		}
		if want := []string{"AI", "🇺🇸 美国", "🇺🇸 US 01"}; !slices.Equal(got.Chain, want) {
			t.Fatalf("chain = %v, want %v", got.Chain, want)
		}
		if got.SkippedDynamic != 0 {
			t.Fatalf("skippedDynamic = %d, want 0", got.SkippedDynamic)
		}
	})

	t.Run("matches ip cidr", func(t *testing.T) {
		got := DiagnoseRule("192.168.1.50:8080", rules, groups)
		if !got.Matched || got.Rule.Index != 2 || got.Exit != "DIRECT" {
			t.Fatalf("got = %+v, want index 2 DIRECT", got)
		}
	})

	t.Run("notes skipped dynamic rules before match", func(t *testing.T) {
		got := DiagnoseRule("unknown.example.org", rules, groups)
		if !got.Matched || got.Rule.Index != 4 || got.Exit != "🇭🇰 HK 01" {
			t.Fatalf("got = %+v, want Match -> HK 01", got)
		}
		if got.SkippedDynamic != 1 {
			t.Fatalf("skippedDynamic = %d, want 1", got.SkippedDynamic)
		}
	})
}
