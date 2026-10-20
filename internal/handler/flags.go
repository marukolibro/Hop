package handler

import "strings"

// flagReplacements map flag emoji macOS draws as a crossed-out box on a Mac
// set to the China mainland region to the flag shown in their place, the
// way providers name those nodes.
var flagReplacements = map[string]string{"🇹🇼": "🇨🇳"}

// shownFlag is flag as Hop shows it.
func shownFlag(flag string) string {
	if replacement, ok := flagReplacements[flag]; ok {
		return replacement
	}
	return flag
}

// shownName is a group's or node's name with every flag Hop shows
// differently replaced.
func shownName(name string) string {
	for flag, replacement := range flagReplacements {
		name = strings.ReplaceAll(name, flag, replacement)
	}
	return name
}
