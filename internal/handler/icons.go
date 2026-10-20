package handler

import "github.com/egoist/mygo/ui"

// Hop's icons: one 24-unit grid, 2-unit round strokes, drawn in the colour
// of the text around them.
var (
	iconRefresh = parseIcon(`<path d="M20 12a8 8 0 1 1-2.34-5.66"/><path d="M20 4v5h-5"/>`)
	iconLink    = parseIcon(`<path d="M10 14a4 4 0 0 0 5.66 0l3-3a4 4 0 0 0-5.66-5.66l-1.2 1.2"/><path d="M14 10a4 4 0 0 0-5.66 0l-3 3a4 4 0 0 0 5.66 5.66l1.2-1.2"/>`)
	iconBolt    = parseIcon(`<path d="M13 3 5 13.5h6.5L11 21l8-10.5h-6.5z"/>`)
	iconClose   = parseIcon(`<path d="M6.5 6.5l11 11M17.5 6.5l-11 11"/>`)
	iconTrash   = parseIcon(`<path d="M4.5 7h15M9.5 7V4.5h5V7M6.5 7l1 12.5h9l1-12.5M10 11v5M14 11v5"/>`)
	iconWindow  = parseIcon(`<rect x="3.5" y="5" width="17" height="14" rx="2.5"/><path d="M3.5 9.5h17"/>`)
	iconPower   = parseIcon(`<path d="M12 3.5V12"/><path d="M7 6.8a7.5 7.5 0 1 0 10 0"/>`)
	iconRoute   = parseIcon(`<path d="M5 19v-6.5A4.5 4.5 0 0 1 9.5 8H19"/><path d="M15.5 4.5 19 8l-3.5 3.5"/><circle cx="5" cy="19" r=".6"/>`)
	iconList    = parseIcon(`<path d="M9 6.5h10.5M9 12h10.5M9 17.5h10.5"/><path d="M4.5 6.5h.01M4.5 12h.01M4.5 17.5h.01"/>`)
	iconSearch  = parseIcon(`<circle cx="11" cy="11" r="6"/><path d="m20 20-4.5-4.5"/>`)
	iconImport  = parseIcon(`<path d="M12 4v11M7.5 10.5 12 15l4.5-4.5M5 19.5h14"/>`)
)

func parseIcon(paths string) *ui.SVG {
	return ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="#000" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">` + paths + `</svg>`))
}
