package service

import (
	"github.com/egoist/mygo/i18n"
	"golang.org/x/text/language"
	"golang.org/x/text/language/display"
)

// CountryName is the name, in the language Hop shows, of an ISO 3166 alpha-2
// code, or the code itself when CLDR names none. A region CLDR does not know
// has a name of its own, which is not a country Hop can name.
func CountryName(code string) string {
	region, err := language.ParseRegion(code)
	if err != nil {
		return code
	}
	names := display.Regions(language.Make(i18n.Locale()))
	name := names.Name(region)
	if name == "" || name == names.Name(language.MustParseRegion("ZZ")) {
		return code
	}
	return name
}
