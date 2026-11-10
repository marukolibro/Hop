package service

import (
	"strings"

	"hop/internal/model"
)

// countryPoints are where a country is drawn on the home's map: a point near
// its middle, or its capital for the small ones. Only the places proxy nodes
// are commonly in are listed; any other code is not placed, and the map says
// so rather than guess.
var countryPoints = map[string]model.GeoPoint{
	"AE": {Lon: 54.4, Lat: 24.5}, "AM": {Lon: 44.5, Lat: 40.2}, "AR": {Lon: -64.0, Lat: -34.0},
	"AT": {Lon: 14.5, Lat: 47.6}, "AU": {Lon: 134.0, Lat: -25.5}, "AZ": {Lon: 47.6, Lat: 40.3},
	"BD": {Lon: 90.3, Lat: 23.8}, "BE": {Lon: 4.5, Lat: 50.6}, "BG": {Lon: 25.2, Lat: 42.7},
	"BH": {Lon: 50.6, Lat: 26.1}, "BR": {Lon: -52.0, Lat: -10.0}, "CA": {Lon: -100.0, Lat: 58.0},
	"CH": {Lon: 8.2, Lat: 46.8}, "CL": {Lon: -71.0, Lat: -33.5}, "CN": {Lon: 104.0, Lat: 35.0},
	"CO": {Lon: -74.0, Lat: 4.6}, "CY": {Lon: 33.2, Lat: 35.0}, "CZ": {Lon: 15.5, Lat: 49.8},
	"DE": {Lon: 10.4, Lat: 51.0}, "DK": {Lon: 9.5, Lat: 56.0}, "EE": {Lon: 25.0, Lat: 58.7},
	"EG": {Lon: 30.8, Lat: 26.8}, "ES": {Lon: -3.7, Lat: 40.2}, "FI": {Lon: 26.0, Lat: 64.0},
	"FR": {Lon: 2.4, Lat: 46.6}, "GB": {Lon: -1.5, Lat: 52.8}, "GE": {Lon: 43.4, Lat: 42.0},
	"GR": {Lon: 22.0, Lat: 39.0}, "HK": {Lon: 114.2, Lat: 22.3}, "HR": {Lon: 15.5, Lat: 45.1},
	"HU": {Lon: 19.5, Lat: 47.2}, "ID": {Lon: 113.0, Lat: -2.0}, "IE": {Lon: -8.0, Lat: 53.2},
	"IL": {Lon: 34.9, Lat: 31.4}, "IN": {Lon: 79.0, Lat: 22.0}, "IR": {Lon: 53.7, Lat: 32.4},
	"IS": {Lon: -19.0, Lat: 64.9}, "IT": {Lon: 12.6, Lat: 42.8}, "JP": {Lon: 138.0, Lat: 37.0},
	"KE": {Lon: 37.9, Lat: 0.0}, "KG": {Lon: 74.6, Lat: 41.2}, "KH": {Lon: 104.9, Lat: 12.6},
	"KR": {Lon: 127.8, Lat: 36.4}, "KW": {Lon: 47.6, Lat: 29.3}, "KZ": {Lon: 67.0, Lat: 48.0},
	"LA": {Lon: 102.5, Lat: 19.9}, "LK": {Lon: 80.7, Lat: 7.9}, "LT": {Lon: 23.9, Lat: 55.3},
	"LU": {Lon: 6.1, Lat: 49.8}, "LV": {Lon: 24.9, Lat: 56.9}, "MA": {Lon: -6.0, Lat: 32.0},
	"MD": {Lon: 28.5, Lat: 47.2}, "MM": {Lon: 96.0, Lat: 21.0}, "MN": {Lon: 103.8, Lat: 46.9},
	"MO": {Lon: 113.5, Lat: 22.2}, "MX": {Lon: -102.0, Lat: 23.6}, "MY": {Lon: 109.7, Lat: 3.8},
	"NG": {Lon: 8.1, Lat: 9.6}, "NL": {Lon: 5.3, Lat: 52.2}, "NO": {Lon: 9.0, Lat: 61.0},
	"NP": {Lon: 84.1, Lat: 28.3}, "NZ": {Lon: 172.0, Lat: -41.5}, "OM": {Lon: 57.0, Lat: 21.0},
	"PA": {Lon: -80.1, Lat: 8.5}, "PE": {Lon: -75.0, Lat: -9.2}, "PH": {Lon: 122.0, Lat: 12.8},
	"PK": {Lon: 69.3, Lat: 30.0}, "PL": {Lon: 19.4, Lat: 52.0}, "PT": {Lon: -8.0, Lat: 39.6},
	"QA": {Lon: 51.2, Lat: 25.3}, "RO": {Lon: 25.0, Lat: 45.9}, "RS": {Lon: 20.8, Lat: 44.0},
	"RU": {Lon: 80.0, Lat: 60.0}, "SA": {Lon: 45.0, Lat: 24.0}, "SE": {Lon: 16.0, Lat: 62.0},
	"SG": {Lon: 103.8, Lat: 1.35}, "SI": {Lon: 14.8, Lat: 46.1}, "SK": {Lon: 19.5, Lat: 48.7},
	"TH": {Lon: 101.0, Lat: 15.0}, "TR": {Lon: 35.0, Lat: 39.0}, "TW": {Lon: 121.0, Lat: 23.7},
	"UA": {Lon: 31.2, Lat: 49.0}, "US": {Lon: -98.0, Lat: 39.0}, "UY": {Lon: -56.0, Lat: -32.8},
	"UZ": {Lon: 64.5, Lat: 41.4}, "VN": {Lon: 106.0, Lat: 16.0}, "ZA": {Lon: 25.0, Lat: -29.0},
}

// CountryPoint is where the map draws a country, given its ISO 3166 alpha-2
// code in either case, and false for a code it does not place.
func CountryPoint(code string) (model.GeoPoint, bool) {
	point, ok := countryPoints[strings.ToUpper(code)]
	return point, ok
}

// FlagCountry is the ISO 3166 alpha-2 code a flag emoji (two regional
// indicator letters) stands for, and false for anything else.
func FlagCountry(flag string) (string, bool) {
	letters := []rune(strings.TrimSpace(flag))
	if len(letters) != 2 {
		return "", false
	}
	code := make([]rune, 0, 2)
	for _, letter := range letters {
		if !isRegionalIndicator(letter) {
			return "", false
		}
		code = append(code, 'A'+letter-0x1F1E6)
	}
	return string(code), true
}
