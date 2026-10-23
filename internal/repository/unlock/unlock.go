// Package unlock asks a few services whether they work through the route the
// HTTP client it was given takes, which main points at Hop's probe listener,
// so one node at a time.
package unlock

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

	"hop/internal/model"
)

const (
	// ChatGPTURL answers 200 with a small JSON from regions OpenAI serves,
	// and names "unsupported_country" (or a plain 403) elsewhere.
	ChatGPTURL = "https://api.openai.com/compliance/cookie_requirements"
	// NetflixOriginalURL is a Netflix production, which shows wherever
	// Netflix runs, with or without the licensed catalogue.
	NetflixOriginalURL = "https://www.netflix.com/title/80018499"
	// YouTubePremiumURL is the Premium page, which names the viewer's region
	// or says Premium is not offered there.
	YouTubePremiumURL = "https://www.youtube.com/premium"

	// attemptTimeout bounds each request of a check.
	attemptTimeout = 10 * time.Second
	// maxChatGPTBody bounds what is read of OpenAI's reply.
	maxChatGPTBody = 4 << 10
	// maxYouTubeBody bounds what is read of the Premium page: its region sits
	// near the start, so the rest of a page of nearly a megabyte is left
	// unread.
	maxYouTubeBody = 192 << 10

	// userAgent is a current browser's: these sites answer a bare Go client
	// differently.
	userAgent = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.5 Safari/605.1.15"
)

// netflixLicensedURLs are two titles Netflix licenses; either one shows where
// the full catalogue is offered.
var netflixLicensedURLs = []string{
	"https://www.netflix.com/title/81280792",
	"https://www.netflix.com/title/70143836",
}

// youTubeRegion finds the region the page was served for in YouTube's own
// configuration.
var youTubeRegion = regexp.MustCompile(`"GL":"([A-Z]{2})"`)

// Client checks the services through the HTTP client it was given.
type Client struct {
	http *http.Client
}

// NewClient returns a client sending through httpClient.
func NewClient(httpClient *http.Client) *Client {
	return &Client{http: httpClient}
}

// Check runs the three checks at once. A service whose check failed is
// UnlockUnknown, and the error says which and why; the other results stand.
func (c *Client) Check(ctx context.Context) (model.Unlock, error) {
	var (
		result model.Unlock
		errs   [3]error
		wg     sync.WaitGroup
	)
	wg.Add(3)
	go func() {
		defer wg.Done()
		result.ChatGPT, errs[0] = c.chatGPT(ctx)
	}()
	go func() {
		defer wg.Done()
		result.Netflix, errs[1] = c.netflix(ctx)
	}()
	go func() {
		defer wg.Done()
		result.YouTube, errs[2] = c.youTube(ctx)
	}()
	wg.Wait()
	return result, errors.Join(errs[:]...)
}

func (c *Client) chatGPT(ctx context.Context) (model.UnlockResult, error) {
	status, body, _, err := c.get(ctx, ChatGPTURL, maxChatGPTBody)
	if err != nil {
		return model.UnlockResult{}, fmt.Errorf("check ChatGPT: %w", err)
	}
	result, err := ParseChatGPT(status, body)
	if err != nil {
		return model.UnlockResult{}, fmt.Errorf("check ChatGPT: GET %s: %w", ChatGPTURL, err)
	}
	return result, nil
}

// ParseChatGPT reads OpenAI's reply: 403 or an "unsupported_country" error is
// a refusal, a 200 is service; anything else tells nothing.
func ParseChatGPT(status int, body []byte) (model.UnlockResult, error) {
	switch {
	case strings.Contains(string(body), "unsupported_country"), status == http.StatusForbidden:
		return model.UnlockResult{State: model.UnlockBlocked}, nil
	case status == http.StatusOK:
		return model.UnlockResult{State: model.UnlockAvailable}, nil
	}
	return model.UnlockResult{}, fmt.Errorf("unexpected status %d", status)
}

func (c *Client) netflix(ctx context.Context) (model.UnlockResult, error) {
	for _, title := range netflixLicensedURLs {
		status, _, final, err := c.get(ctx, title, 0)
		if err != nil {
			return model.UnlockResult{}, fmt.Errorf("check Netflix: %w", err)
		}
		switch status {
		case http.StatusOK:
			return model.UnlockResult{State: model.UnlockAvailable, Region: NetflixRegion(final)}, nil
		case http.StatusNotFound:
		default:
			return model.UnlockResult{}, fmt.Errorf("check Netflix: GET %s: unexpected status %d", title, status)
		}
	}
	status, _, final, err := c.get(ctx, NetflixOriginalURL, 0)
	if err != nil {
		return model.UnlockResult{}, fmt.Errorf("check Netflix: %w", err)
	}
	switch status {
	case http.StatusOK:
		return model.UnlockResult{State: model.UnlockPartial, Region: NetflixRegion(final)}, nil
	case http.StatusNotFound:
		return model.UnlockResult{State: model.UnlockBlocked}, nil
	}
	return model.UnlockResult{}, fmt.Errorf("check Netflix: GET %s: unexpected status %d", NetflixOriginalURL, status)
}

// NetflixRegion reads the region out of the address Netflix redirected a
// title to, "/sg/title/…". A title served without a prefix names no region.
func NetflixRegion(final *url.URL) string {
	first, _, _ := strings.Cut(strings.TrimPrefix(final.Path, "/"), "/")
	if len(first) != 2 || first[0] < 'a' || first[0] > 'z' || first[1] < 'a' || first[1] > 'z' {
		return ""
	}
	return strings.ToUpper(first)
}

func (c *Client) youTube(ctx context.Context) (model.UnlockResult, error) {
	_, body, _, err := c.get(ctx, YouTubePremiumURL, maxYouTubeBody)
	if err != nil {
		return model.UnlockResult{}, fmt.Errorf("check YouTube Premium: %w", err)
	}
	result, err := ParseYouTube(body)
	if err != nil {
		return model.UnlockResult{}, fmt.Errorf("check YouTube Premium: GET %s: %w", YouTubePremiumURL, err)
	}
	return result, nil
}

// ParseYouTube reads the start of the Premium page: a refusal says so in
// words, a page for a region names it.
func ParseYouTube(body []byte) (model.UnlockResult, error) {
	page := string(body)
	if strings.Contains(strings.ToLower(page), "is not available in your country") {
		return model.UnlockResult{State: model.UnlockBlocked}, nil
	}
	if match := youTubeRegion.FindStringSubmatch(page); match != nil {
		return model.UnlockResult{State: model.UnlockAvailable, Region: match[1]}, nil
	}
	return model.UnlockResult{}, errors.New("the page names no region and does not refuse")
}

// get fetches address, following redirects, and returns the status, the first
// limit bytes of the body (none when limit is 0) and the address it ended at.
func (c *Client) get(ctx context.Context, address string, limit int64) (int, []byte, *url.URL, error) {
	ctx, cancel := context.WithTimeout(ctx, attemptTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if err != nil {
		return 0, nil, nil, fmt.Errorf("GET %s: %w", address, err)
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept-Language", "en")
	resp, err := c.http.Do(req)
	if err != nil {
		return 0, nil, nil, fmt.Errorf("GET %s: %w", address, err)
	}
	defer resp.Body.Close()
	var body []byte
	if limit > 0 {
		body, err = io.ReadAll(io.LimitReader(resp.Body, limit))
		if err != nil {
			return 0, nil, nil, fmt.Errorf("GET %s: read reply (status %d): %w", address, resp.StatusCode, err)
		}
	}
	return resp.StatusCode, body, resp.Request.URL, nil
}
