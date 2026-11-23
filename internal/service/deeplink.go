package service

import (
	"errors"
	"fmt"
	"net/url"
	"strings"

	"hop/internal/model"
)

// ParseInstallConfigURL parses a clash://, mihomo://, or hop:// install-config
// deep link into a pre-filled remote subscription. It returns (sub, true, nil)
// for a valid import link, (zero, false, nil) for unrelated URLs, and a
// redacted error (never containing the subscription link itself) when an
// install-config link carries an invalid target URL.
func ParseInstallConfigURL(raw string) (model.DeepLinkSubscription, bool, error) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return model.DeepLinkSubscription{}, false, nil
	}
	switch strings.ToLower(parsed.Scheme) {
	case "clash", "mihomo", "hop":
	default:
		return model.DeepLinkSubscription{}, false, nil
	}
	action := strings.ToLower(strings.Trim(parsed.Host, "/"))
	if action == "" {
		action = strings.ToLower(strings.Trim(parsed.Path, "/"))
	}
	if action != "install-config" {
		return model.DeepLinkSubscription{}, false, nil
	}
	query := parsed.Query()
	subURL := strings.TrimSpace(query.Get("url"))
	if subURL == "" {
		return model.DeepLinkSubscription{}, true, errors.New("the install-config link has no url parameter")
	}
	// Some panels double-encode the subscription URL in clash:// links.
	if !strings.HasPrefix(strings.ToLower(subURL), "http://") && !strings.HasPrefix(strings.ToLower(subURL), "https://") {
		if decoded, decErr := url.QueryUnescape(subURL); decErr == nil {
			subURL = strings.TrimSpace(decoded)
		}
	}
	target, err := url.Parse(subURL)
	if err != nil || (target.Scheme != "http" && target.Scheme != "https") || target.Host == "" {
		return model.DeepLinkSubscription{}, true, errors.New("the install-config url parameter must be an http:// or https:// address")
	}
	name := strings.TrimSpace(query.Get("name"))
	return model.DeepLinkSubscription{
		Name: name,
		URL:  subURL,
	}, true, nil
}

// RedactDeepLinkHost returns just the host of a subscription URL for safe
// logging, or "" when unavailable.
func RedactDeepLinkHost(subURL string) string {
	parsed, err := url.Parse(subURL)
	if err != nil {
		return ""
	}
	if parsed.Host == "" {
		return ""
	}
	return fmt.Sprintf("%s://%s", parsed.Scheme, parsed.Host)
}
