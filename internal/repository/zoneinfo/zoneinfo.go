// Package zoneinfo reads where the system's time zone is, from the zone
// database the system ships. It is offline and sends nothing.
package zoneinfo

import (
	"bufio"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strconv"
	"strings"

	"hop/internal/model"
)

const (
	localtimePath = "/etc/localtime"
	zoneTabPath   = "/usr/share/zoneinfo/zone.tab"
	zoneMarker    = "zoneinfo/"
)

// Reader finds the system's time zone and the place its name stands for.
type Reader struct {
	localtime string
	zoneTab   string
}

// New returns a reader of the system's own files.
func New() *Reader {
	return &Reader{localtime: localtimePath, zoneTab: zoneTabPath}
}

// Home returns the place of the system's time zone, and false when the system
// names none or the database does not list it. A file that cannot be read is
// an error.
func (r *Reader) Home() (model.GeoPoint, bool, error) {
	target, err := os.Readlink(r.localtime)
	if errors.Is(err, fs.ErrNotExist) {
		return model.GeoPoint{}, false, nil
	}
	if err != nil {
		return model.GeoPoint{}, false, fmt.Errorf("read time zone link (path=%q): %w", r.localtime, err)
	}
	_, zone, found := strings.Cut(target, zoneMarker)
	if !found {
		return model.GeoPoint{}, false, nil
	}
	file, err := os.Open(r.zoneTab)
	if errors.Is(err, fs.ErrNotExist) {
		return model.GeoPoint{}, false, nil
	}
	if err != nil {
		return model.GeoPoint{}, false, fmt.Errorf("open zone table (path=%q): %w", r.zoneTab, err)
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		fields := strings.Split(scanner.Text(), "\t")
		if len(fields) < 3 || strings.HasPrefix(fields[0], "#") || fields[2] != zone {
			continue
		}
		point, err := ParseCoordinates(fields[1])
		if err != nil {
			return model.GeoPoint{}, false, fmt.Errorf("zone table (zone=%q): %w", zone, err)
		}
		return point, true, nil
	}
	if err := scanner.Err(); err != nil {
		return model.GeoPoint{}, false, fmt.Errorf("read zone table (path=%q): %w", r.zoneTab, err)
	}
	return model.GeoPoint{}, false, nil
}

// ParseCoordinates reads an ISO 6709 pair as the zone table writes it,
// "+3114+12128" (degrees and minutes) or "+311400+1212800" (with seconds).
func ParseCoordinates(text string) (model.GeoPoint, error) {
	split := strings.LastIndexAny(text, "+-")
	if split <= 0 {
		return model.GeoPoint{}, fmt.Errorf("coordinates %q: want latitude and longitude", text)
	}
	lat, err := parseAngle(text[:split], 2)
	if err != nil {
		return model.GeoPoint{}, fmt.Errorf("coordinates %q: latitude: %w", text, err)
	}
	lon, err := parseAngle(text[split:], 3)
	if err != nil {
		return model.GeoPoint{}, fmt.Errorf("coordinates %q: longitude: %w", text, err)
	}
	return model.GeoPoint{Lon: lon, Lat: lat}, nil
}

// parseAngle reads a signed angle whose degrees take degreeDigits digits,
// followed by two of minutes and optionally two of seconds.
func parseAngle(text string, degreeDigits int) (float32, error) {
	sign, digits := text[:1], text[1:]
	if len(digits) != degreeDigits+2 && len(digits) != degreeDigits+4 {
		return 0, fmt.Errorf("%q has %d digits", text, len(digits))
	}
	value, err := strconv.ParseFloat(digits[:degreeDigits], 64)
	if err != nil {
		return 0, err
	}
	minutes, err := strconv.ParseFloat(digits[degreeDigits:degreeDigits+2], 64)
	if err != nil {
		return 0, err
	}
	value += minutes / 60
	if len(digits) == degreeDigits+4 {
		seconds, err := strconv.ParseFloat(digits[degreeDigits+2:], 64)
		if err != nil {
			return 0, err
		}
		value += seconds / 3600
	}
	if sign == "-" {
		value = -value
	}
	return float32(value), nil
}
