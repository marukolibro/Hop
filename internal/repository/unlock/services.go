package unlock

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strings"

	"golang.org/x/text/language"

	"hop/internal/model"
)

// The addresses the checks ask, each checked by hand against the live
// service from a region it serves; what a refusing region gets is what the
// services are known to answer.
const (
	// GoogleURL answers 204 and nothing else wherever Google is reachable.
	GoogleURL = "https://www.google.com/generate_204"
	// GitHubURL is GitHub's home page.
	GitHubURL = "https://github.com/"
	// YouTubePremiumURL is the Premium page, which names the viewer's region
	// or says Premium is not offered there.
	YouTubePremiumURL = "https://www.youtube.com/premium"
	// ChatGPTURL answers 200 with a small JSON from regions OpenAI serves,
	// and names "unsupported_country" (or a plain 403) elsewhere.
	ChatGPTURL = "https://api.openai.com/compliance/cookie_requirements"
	// ChatGPTTraceURL is Cloudflare's trace on ChatGPT's site, whose loc=
	// line is the region the site sees.
	ChatGPTTraceURL = "https://chatgpt.com/cdn-cgi/trace"
	// ClaudeTraceURL is the same trace on Claude's site. Claude's pages put a
	// bot check in front of any client that is not a browser, so the region
	// the trace names is what tells whether Claude is offered.
	ClaudeTraceURL = "https://claude.ai/cdn-cgi/trace"
	// GeminiURL is Gemini's page, which names the viewer's region as an
	// ISO 3166 alpha-3 code far down its config.
	GeminiURL = "https://gemini.google.com/"
	// NetflixOriginalURL is a Netflix production, which shows wherever
	// Netflix runs, with or without the licensed catalogue.
	NetflixOriginalURL = "https://www.netflix.com/title/80018499"
	// DisneyURL is Disney+'s home page, which sends a region it does not
	// serve to a page saying so.
	DisneyURL = "https://www.disneyplus.com/"
	// PrimeVideoURL is Prime Video's home page, which names the territory it
	// was served for, or says the service is restricted.
	PrimeVideoURL = "https://www.primevideo.com/"
	// SpotifyPremiumURL redirects to the Premium page of the viewer's region,
	// "/sg-en/premium/".
	SpotifyPremiumURL = "https://www.spotify.com/premium/"
	// TikTokURL is TikTok's explore page, which names the region it was
	// served for.
	TikTokURL = "https://www.tiktok.com/explore"
	// DiscordURL is a small public API reply.
	DiscordURL = "https://discord.com/api/v9/experiments"
)

// Bounds on what is read of each reply: the markers sit at about these
// offsets, which leaves the rest of each page unread.
const (
	maxTraceBody   = 4 << 10
	maxChatGPTBody = 4 << 10
	maxYouTubeBody = 192 << 10
	maxGeminiBody  = 1 << 20
	maxPrimeBody   = 384 << 10
	maxTikTokBody  = 256 << 10
	maxDiscordBody = 4 << 10
	noBody         = 0
)

// netflixLicensedURLs are two titles Netflix licenses; either one shows where
// the full catalogue is offered.
var netflixLicensedURLs = []string{
	"https://www.netflix.com/title/81280792",
	"https://www.netflix.com/title/70143836",
}

var (
	// youTubeRegion finds the region the page was served for in YouTube's
	// own configuration.
	youTubeRegion = regexp.MustCompile(`"GL":"([A-Z]{2})"`)
	// geminiRegion finds the alpha-3 region in Gemini's page config.
	geminiRegion = regexp.MustCompile(`,2,1,200,"([A-Z]{3})"`)
	// primeTerritory finds the territory Prime Video served the page for.
	primeTerritory = regexp.MustCompile(`"currentTerritory":"([A-Z]{2})"`)
	// tikTokRegion finds the region TikTok served the page for.
	tikTokRegion = regexp.MustCompile(`"region":"([A-Z]{2})"`)
)

// aiRefusedRegions are the regions where neither Anthropic nor Google offers
// its assistant, as both publish: China, Hong Kong, Macau, Russia, Belarus,
// Iran, North Korea, Cuba and Syria.
var aiRefusedRegions = []string{"CN", "HK", "MO", "RU", "BY", "IR", "KP", "CU", "SY"}

// checkFunc is one service's check. A check that fails returns an error and
// a zero result, UnlockUnknown, or the result it has with an error saying
// what is missing from it.
type checkFunc func(ctx context.Context, c *Client) (model.UnlockResult, error)

// checks are the services' checks, one for each of model.UnlockServices.
var checks = map[model.UnlockService]checkFunc{
	model.UnlockGoogle:  checkGoogle,
	model.UnlockGitHub:  checkGitHub,
	model.UnlockYouTube: checkYouTube,
	model.UnlockChatGPT: checkChatGPT,
	model.UnlockClaude:  checkClaude,
	model.UnlockGemini:  checkGemini,
	model.UnlockNetflix: checkNetflix,
	model.UnlockDisney:  checkDisney,
	model.UnlockPrime:   checkPrime,
	model.UnlockSpotify: checkSpotify,
	model.UnlockTikTok:  checkTikTok,
	model.UnlockDiscord: checkDiscord,
}

// measured is a result with the first request's latency and route.
func measured(r reply, state model.UnlockState, region string) model.UnlockResult {
	return model.UnlockResult{State: state, Region: region, Latency: r.latency, Route: r.route}
}

// reachable is the check of a service that only has to answer: want is
// the status it answers with.
func reachable(ctx context.Context, c *Client, address string, want int, limit int64) (model.UnlockResult, error) {
	r, err := c.probe(ctx, address, limit)
	if err != nil {
		return model.UnlockResult{}, err
	}
	if r.status != want {
		return model.UnlockResult{}, fmt.Errorf("GET %s: unexpected status %d (want %d)", address, r.status, want)
	}
	return measured(r, model.UnlockAvailable, ""), nil
}

func checkGoogle(ctx context.Context, c *Client) (model.UnlockResult, error) {
	return reachable(ctx, c, GoogleURL, http.StatusNoContent, noBody)
}

func checkGitHub(ctx context.Context, c *Client) (model.UnlockResult, error) {
	return reachable(ctx, c, GitHubURL, http.StatusOK, noBody)
}

func checkDiscord(ctx context.Context, c *Client) (model.UnlockResult, error) {
	return reachable(ctx, c, DiscordURL, http.StatusOK, maxDiscordBody)
}

func checkYouTube(ctx context.Context, c *Client) (model.UnlockResult, error) {
	r, err := c.probe(ctx, YouTubePremiumURL, maxYouTubeBody)
	if err != nil {
		return model.UnlockResult{}, err
	}
	state, region, err := ParseYouTube(r.body)
	if err != nil {
		return model.UnlockResult{}, fmt.Errorf("GET %s: %w", YouTubePremiumURL, err)
	}
	return measured(r, state, region), nil
}

// ParseYouTube reads the start of the Premium page: a refusal says so in
// words, a page for a region names it.
func ParseYouTube(body []byte) (model.UnlockState, string, error) {
	if bytes.Contains(bytes.ToLower(body), []byte("is not available in your country")) {
		return model.UnlockBlocked, "", nil
	}
	if match := youTubeRegion.FindSubmatch(body); match != nil {
		return model.UnlockAvailable, string(match[1]), nil
	}
	return model.UnlockUnknown, "", errors.New("the page names no region and does not refuse")
}

// checkChatGPT asks OpenAI's API whether it serves the route, then the site's
// trace for the region. A trace that fails leaves the result without one.
func checkChatGPT(ctx context.Context, c *Client) (model.UnlockResult, error) {
	r, err := c.probe(ctx, ChatGPTURL, maxChatGPTBody)
	if err != nil {
		return model.UnlockResult{}, err
	}
	state, err := ParseChatGPT(r.status, r.body)
	if err != nil {
		return model.UnlockResult{}, fmt.Errorf("GET %s: %w", ChatGPTURL, err)
	}
	result := measured(r, state, "")
	trace, err := c.get(ctx, ChatGPTTraceURL, maxTraceBody)
	if err != nil {
		return result, fmt.Errorf("read the region: %w", err)
	}
	region, err := TraceRegion(trace.body)
	if err != nil {
		return result, fmt.Errorf("read the region: GET %s: %w", ChatGPTTraceURL, err)
	}
	result.Region = region
	return result, nil
}

// ParseChatGPT reads OpenAI's reply: 403 or an "unsupported_country" error is
// a refusal, a 200 is service; anything else tells nothing.
func ParseChatGPT(status int, body []byte) (model.UnlockState, error) {
	switch {
	case bytes.Contains(body, []byte("unsupported_country")), status == http.StatusForbidden:
		return model.UnlockBlocked, nil
	case status == http.StatusOK:
		return model.UnlockAvailable, nil
	}
	return model.UnlockUnknown, fmt.Errorf("unexpected status %d", status)
}

func checkClaude(ctx context.Context, c *Client) (model.UnlockResult, error) {
	r, err := c.probe(ctx, ClaudeTraceURL, maxTraceBody)
	if err != nil {
		return model.UnlockResult{}, err
	}
	if r.status != http.StatusOK {
		return model.UnlockResult{}, fmt.Errorf("GET %s: unexpected status %d", ClaudeTraceURL, r.status)
	}
	region, err := TraceRegion(r.body)
	if err != nil {
		return model.UnlockResult{}, fmt.Errorf("GET %s: %w", ClaudeTraceURL, err)
	}
	return measured(r, regionState(region), region), nil
}

// TraceRegion reads the loc= line of a Cloudflare trace.
func TraceRegion(body []byte) (string, error) {
	for line := range strings.Lines(string(body)) {
		value, found := strings.CutPrefix(strings.TrimSpace(line), "loc=")
		if !found {
			continue
		}
		region, err := language.ParseRegion(value)
		if err != nil || !region.IsCountry() {
			return "", fmt.Errorf("the trace names no country (loc=%q)", value)
		}
		return region.String(), nil
	}
	return "", errors.New("the trace has no loc= line")
}

// regionState is whether Claude or Gemini is offered in region.
func regionState(region string) model.UnlockState {
	if slices.Contains(aiRefusedRegions, region) {
		return model.UnlockBlocked
	}
	return model.UnlockAvailable
}

func checkGemini(ctx context.Context, c *Client) (model.UnlockResult, error) {
	r, err := c.probe(ctx, GeminiURL, maxGeminiBody)
	if err != nil {
		return model.UnlockResult{}, err
	}
	if r.status != http.StatusOK {
		return model.UnlockResult{}, fmt.Errorf("GET %s: unexpected status %d", GeminiURL, r.status)
	}
	region, err := ParseGeminiRegion(r.body)
	if err != nil {
		return model.UnlockResult{}, fmt.Errorf("GET %s: %w", GeminiURL, err)
	}
	return measured(r, regionState(region), region), nil
}

// ParseGeminiRegion reads the region Gemini's page was served for, as an
// alpha-2 code.
func ParseGeminiRegion(body []byte) (string, error) {
	match := geminiRegion.FindSubmatch(body)
	if match == nil {
		return "", errors.New("the page names no region")
	}
	region, err := language.ParseRegion(string(match[1]))
	if err != nil || !region.IsCountry() {
		return "", fmt.Errorf("the page names no country (code=%q)", match[1])
	}
	return region.String(), nil
}

// checkNetflix asks for two licensed titles, then a production of Netflix's
// own: the licensed ones show only where the full catalogue is offered.
func checkNetflix(ctx context.Context, c *Client) (model.UnlockResult, error) {
	first, err := c.probe(ctx, netflixLicensedURLs[0], noBody)
	if err != nil {
		return model.UnlockResult{}, err
	}
	r := first
	for i, title := range netflixLicensedURLs {
		if i > 0 {
			if r, err = c.get(ctx, title, noBody); err != nil {
				return model.UnlockResult{}, err
			}
		}
		switch r.status {
		case http.StatusOK:
			return measured(first, model.UnlockAvailable, pathRegion(r.final)), nil
		case http.StatusNotFound:
		default:
			return model.UnlockResult{}, fmt.Errorf("GET %s: unexpected status %d", title, r.status)
		}
	}
	own, err := c.get(ctx, NetflixOriginalURL, noBody)
	if err != nil {
		return model.UnlockResult{}, err
	}
	switch own.status {
	case http.StatusOK:
		return measured(first, model.UnlockPartial, pathRegion(own.final)), nil
	case http.StatusNotFound:
		return measured(first, model.UnlockBlocked, ""), nil
	}
	return model.UnlockResult{}, fmt.Errorf("GET %s: unexpected status %d", NetflixOriginalURL, own.status)
}

func checkDisney(ctx context.Context, c *Client) (model.UnlockResult, error) {
	r, err := c.probe(ctx, DisneyURL, noBody)
	if err != nil {
		return model.UnlockResult{}, err
	}
	switch {
	case r.status == http.StatusForbidden, strings.Contains(r.final.Path, "unavailable"):
		return measured(r, model.UnlockBlocked, ""), nil
	case r.status == http.StatusOK:
		return measured(r, model.UnlockAvailable, ""), nil
	}
	return model.UnlockResult{}, fmt.Errorf("GET %s: unexpected status %d (ended at %s)", DisneyURL, r.status, r.final.Redacted())
}

func checkPrime(ctx context.Context, c *Client) (model.UnlockResult, error) {
	r, err := c.probe(ctx, PrimeVideoURL, maxPrimeBody)
	if err != nil {
		return model.UnlockResult{}, err
	}
	state, region, err := ParsePrime(r.body)
	if err != nil {
		return model.UnlockResult{}, fmt.Errorf("GET %s (status %d): %w", PrimeVideoURL, r.status, err)
	}
	return measured(r, state, region), nil
}

// ParsePrime reads Prime Video's page: a restricted region says so, a served
// one names its territory.
func ParsePrime(body []byte) (model.UnlockState, string, error) {
	if bytes.Contains(body, []byte("isServiceRestricted")) {
		return model.UnlockBlocked, "", nil
	}
	if match := primeTerritory.FindSubmatch(body); match != nil {
		return model.UnlockAvailable, string(match[1]), nil
	}
	return model.UnlockUnknown, "", errors.New("the page names no territory and is not restricted")
}

func checkSpotify(ctx context.Context, c *Client) (model.UnlockResult, error) {
	r, err := c.probe(ctx, SpotifyPremiumURL, noBody)
	if err != nil {
		return model.UnlockResult{}, err
	}
	region := pathRegion(r.final)
	if r.status != http.StatusOK || region == "" {
		return model.UnlockResult{}, fmt.Errorf("GET %s: status %d at %s names no region", SpotifyPremiumURL, r.status, r.final.Redacted())
	}
	return measured(r, model.UnlockAvailable, region), nil
}

func checkTikTok(ctx context.Context, c *Client) (model.UnlockResult, error) {
	r, err := c.probe(ctx, TikTokURL, maxTikTokBody)
	if err != nil {
		return model.UnlockResult{}, err
	}
	match := tikTokRegion.FindSubmatch(r.body)
	if r.status != http.StatusOK || match == nil {
		return model.UnlockResult{}, fmt.Errorf("GET %s: status %d names no region", TikTokURL, r.status)
	}
	return measured(r, model.UnlockAvailable, string(match[1])), nil
}

// pathRegion reads a region from the first segment of the address a site
// redirected to, Netflix's "/sg/title/…" or Spotify's "/sg-en/premium/"; a
// path without one names none.
func pathRegion(final *url.URL) string {
	first, _, _ := strings.Cut(strings.TrimPrefix(final.Path, "/"), "/")
	code, _, _ := strings.Cut(first, "-")
	if len(code) != 2 || code[0] < 'a' || code[0] > 'z' || code[1] < 'a' || code[1] > 'z' {
		return ""
	}
	return strings.ToUpper(code)
}
