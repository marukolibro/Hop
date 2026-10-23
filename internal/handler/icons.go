package handler

import "github.com/egoist/mygo/ui"

// Hop's icons: one 24-unit grid, 2-unit round strokes, drawn in the colour
// of the text around them.
var (
	iconRefresh     = parseIcon(`<path d="M20 12a8 8 0 1 1-2.34-5.66"/><path d="M20 4v5h-5"/>`)
	iconLink        = parseIcon(`<path d="M10 14a4 4 0 0 0 5.66 0l3-3a4 4 0 0 0-5.66-5.66l-1.2 1.2"/><path d="M14 10a4 4 0 0 0-5.66 0l-3 3a4 4 0 0 0 5.66 5.66l1.2-1.2"/>`)
	iconBolt        = parseIcon(`<path d="M13 3 5 13.5h6.5L11 21l8-10.5h-6.5z"/>`)
	iconClose       = parseIcon(`<path d="M6.5 6.5l11 11M17.5 6.5l-11 11"/>`)
	iconTrash       = parseIcon(`<path d="M4.5 7h15M9.5 7V4.5h5V7M6.5 7l1 12.5h9l1-12.5M10 11v5M14 11v5"/>`)
	iconTerminal    = parseIcon(`<rect x="3.5" y="4.5" width="17" height="15" rx="2.5"/><path d="m7.5 9.5 3 2.5-3 2.5"/><path d="M12.5 15h4"/>`)
	iconWindow      = parseIcon(`<rect x="3.5" y="5" width="17" height="14" rx="2.5"/><path d="M3.5 9.5h17"/>`)
	iconPower       = parseIcon(`<path d="M12 3.5V12"/><path d="M7 6.8a7.5 7.5 0 1 0 10 0"/>`)
	iconRoute       = parseIcon(`<path d="M5 19v-6.5A4.5 4.5 0 0 1 9.5 8H19"/><path d="M15.5 4.5 19 8l-3.5 3.5"/><circle cx="5" cy="19" r=".6"/>`)
	iconList        = parseIcon(`<path d="M9 6.5h10.5M9 12h10.5M9 17.5h10.5"/><path d="M4.5 6.5h.01M4.5 12h.01M4.5 17.5h.01"/>`)
	iconSearch      = parseIcon(`<circle cx="11" cy="11" r="6"/><path d="m20 20-4.5-4.5"/>`)
	iconPlus        = parseIcon(`<path d="M12 5v14M5 12h14"/>`)
	iconBack        = parseIcon(`<path d="M14 6l-6 6 6 6"/><path d="M8 12h12"/>`)
	iconMore        = parseIcon(`<circle cx="5" cy="12" r=".6"/><circle cx="12" cy="12" r=".6"/><circle cx="19" cy="12" r=".6"/>`)
	iconGlobe       = parseIcon(`<circle cx="12" cy="12" r="9"/><path d="M3 12h18M12 3a18 18 0 0 1 0 18 18 18 0 0 1 0-18Z"/>`)
	iconCheck       = parseIcon(`<path d="m5 12 4 4L19 6"/>`)
	iconWarn        = parseIcon(`<path d="m10.3 4-8 14a2 2 0 0 0 1.7 3h16a2 2 0 0 0 1.7-3l-8-14a2 2 0 0 0-3.4 0Z"/><path d="M12 9v5M12 17v.1"/>`)
	iconCore        = parseIcon(`<rect x="7" y="7" width="10" height="10" rx="2"/><path d="M9.5 3v4M14.5 3v4M9.5 17v4M14.5 17v4M3 9.5h4M3 14.5h4M17 9.5h4M17 14.5h4"/>`)
	iconUpgrade     = parseIcon(`<path d="M12 19V5M6 11l6-6 6 6"/>`)
	iconVideo       = parseIcon(`<rect x="3" y="5" width="18" height="14" rx="3"/><path d="M10.5 9.5v5l4-2.5z"/>`)
	iconMusic       = parseIcon(`<path d="M9 18V6l10-2v12"/><circle cx="6.5" cy="18" r="2.5"/><circle cx="16.5" cy="16" r="2.5"/>`)
	iconSparkle     = parseIcon(`<path d="M11 3.5l1.9 4.9 4.9 1.9-4.9 1.9L11 17.1l-1.9-4.9-4.9-1.9 4.9-1.9z"/><path d="M18.5 15.5l.7 1.8 1.8.7-1.8.7-.7 1.8-.7-1.8-1.8-.7 1.8-.7z"/>`)
	iconPhone       = parseIcon(`<rect x="7" y="3" width="10" height="18" rx="2.5"/><path d="M11 18h2"/>`)
	iconGrid        = parseIcon(`<rect x="4" y="4" width="7" height="7" rx="1.4"/><rect x="13" y="4" width="7" height="7" rx="1.4"/><rect x="4" y="13" width="7" height="7" rx="1.4"/><rect x="13" y="13" width="7" height="7" rx="1.4"/>`)
	iconGamepad     = parseIcon(`<path d="M7 8h10a4 4 0 0 1 3.9 4.9l-.7 3.4a2.2 2.2 0 0 1-3.8.9L14.5 15h-5l-1.9 2.2a2.2 2.2 0 0 1-3.8-.9l-.7-3.4A4 4 0 0 1 7 8z"/><path d="M8 10.5v3M6.5 12h3"/><path d="M15.5 11.2h.01M17.5 12.8h.01"/>`)
	iconChat        = parseIcon(`<path d="M5 5h14a2 2 0 0 1 2 2v8a2 2 0 0 1-2 2h-7l-4.5 3.5V17H5a2 2 0 0 1-2-2V7a2 2 0 0 1 2-2z"/>`)
	iconPlane       = parseIcon(`<path d="M20.5 4 3.5 11l6 2.2L11.7 20l2.8-4.2 4.7 3.5z"/><path d="m9.5 13.2 11-9.2"/>`)
	iconUsers       = parseIcon(`<circle cx="9" cy="8.5" r="3.2"/><path d="M3.5 19a5.5 5.5 0 0 1 11 0"/><path d="M15.5 5.6a3.2 3.2 0 0 1 0 5.8M17.5 14a5.5 5.5 0 0 1 3 5"/>`)
	iconCloud       = parseIcon(`<path d="M7.5 18.5a4 4 0 0 1-.6-7.95A5.5 5.5 0 0 1 17.6 9.4 4.6 4.6 0 0 1 17 18.5z"/>`)
	iconCode        = parseIcon(`<path d="m8 8-4.5 4L8 16M16 8l4.5 4L16 16M13.5 5.5l-3 13"/>`)
	iconBag         = parseIcon(`<path d="M5 8h14l-1 12H6z"/><path d="M9 8V7a3 3 0 0 1 6 0v1"/>`)
	iconDownload    = parseIcon(`<path d="M12 4v11M7.5 10.5 12 15l4.5-4.5M5 19.5h14"/>`)
	iconShield      = parseIcon(`<path d="M12 3.5 5 6v5.5c0 4.2 2.8 7.4 7 9 4.2-1.6 7-4.8 7-9V6z"/><path d="M9.5 9.5l5 5M14.5 9.5l-5 5"/>`)
	iconShieldCheck = parseIcon(`<path d="M12 3.5 5 6v5.5c0 4.2 2.8 7.4 7 9 4.2-1.6 7-4.8 7-9V6z"/><path d="m9 11.5 2.2 2.2 4-4.5"/>`)
	iconHome        = parseIcon(`<path d="M4 11.5 12 5l8 6.5"/><path d="M6.5 10v9h11v-9"/>`)
	iconTarget      = parseIcon(`<circle cx="12" cy="12" r="8"/><circle cx="12" cy="12" r="3.5"/>`)
	iconChevron     = parseIcon(`<path d="m6 9 6 6 6-6"/>`)
	iconUnlock      = parseIcon(`<rect x="5" y="11" width="14" height="9" rx="2.5"/><path d="M8.5 11V8a3.5 3.5 0 0 1 6.8-1.2"/>`)
	iconFile        = parseIcon(`<path d="M13.5 3.5H7a2 2 0 0 0-2 2v13a2 2 0 0 0 2 2h10a2 2 0 0 0 2-2V9z"/><path d="M13.5 3.5V9H19"/>`)
)

func parseIcon(paths string) *ui.SVG {
	return ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="#000" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">` + paths + `</svg>`))
}
