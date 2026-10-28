package service

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"hop/internal/model"
)

// ParseUsage reads a subscription-userinfo header, such as
// "upload=5845982050; download=27080936224; total=214748364800;
// expire=1796918400". Byte counts are in bytes and expire is a Unix time;
// a missing, empty or zero total or expire means no limit. Unknown fields
// are ignored, as providers add their own.
func ParseUsage(header string) (model.Usage, error) {
	var usage model.Usage
	for _, field := range strings.Split(header, ";") {
		key, value, found := strings.Cut(strings.TrimSpace(field), "=")
		if !found {
			continue
		}
		key, value = strings.ToLower(strings.TrimSpace(key)), strings.TrimSpace(value)
		switch key {
		case "upload", "download", "total", "expire":
		default:
			continue
		}
		// Some providers send "expire=" for an account that never expires.
		if value == "" {
			continue
		}
		// Some providers send decimals; the fraction of a byte is noise.
		number, err := strconv.ParseFloat(value, 64)
		if err != nil {
			return model.Usage{}, fmt.Errorf("subscription-userinfo field %s is not a number (value=%q): %w", key, value, err)
		}
		n := int64(number)
		switch key {
		case "upload":
			usage.Upload = n
		case "download":
			usage.Download = n
		case "total":
			usage.Total = n
		case "expire":
			if n > 0 {
				usage.Expire = time.Unix(n, 0)
			}
		}
	}
	return usage, nil
}
