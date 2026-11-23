package handler

import (
	"fmt"
	"math"
	"runtime"
	"slices"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/i18n"
	"github.com/egoist/mygo/ui"

	"hop/internal/model"
	"hop/internal/service"
	"hop/locales"
)

// appearanceLabels name service.Appearances, index for index.
func appearanceLabels() []string {
	return []string{locales.FollowSystem(), locales.AppearanceLight(), locales.AppearanceDark()}
}

// languageLabels name service.Languages, index for index; a language is named
// in itself, whichever language Hop shows.
func languageLabels() []string {
	return []string{locales.FollowSystem(), i18n.Autonym(string(model.LanguageChinese)), i18n.Autonym(string(model.LanguageEnglish))}
}

// colorThemeLabel returns the localized name of one color theme.
func colorThemeLabel(theme model.ColorTheme) string {
	switch theme {
	case model.ThemeDusk:
		return locales.ThemeDusk()
	case model.ThemeForest:
		return locales.ThemeForest()
	case model.ThemeAmber:
		return locales.ThemeAmber()
	case model.ThemeRose:
		return locales.ThemeRose()
	case model.ThemeSlate:
		return locales.ThemeSlate()
	case model.ThemePaper:
		return locales.ThemePaper()
	case model.ThemeHack:
		return locales.ThemeHack()
	default:
		return locales.ThemeOcean()
	}
}

// timeoutLabels name service.DelayTimeouts, index for index.
func timeoutLabels() []string {
	return []string{locales.SecondsShort(2), locales.SecondsShort(3), locales.SecondsShort(5)}
}

// delayTestURLLabels name service.DelayTestURLs, index for index.
var delayTestURLLabels = []string{"Google", "Cloudflare", "Apple"}

// updateLabels name service.SubscriptionUpdateIntervals, index for index.
func updateLabels() []string {
	return []string{locales.Off(), locales.HoursShort(6), locales.HoursShort(12), locales.HoursShort(24)}
}

// settingsState is what the settings page shows and edits. The indexes are
// what its switches hold, kept in step with settings.
type settingsState struct {
	settings        model.Settings
	appearanceIndex int
	languageIndex   int
	timeoutIndex    int
	delayURLIndex   int
	updateIndex     int
	// clarity is the slider's value; settings.WindowClarity is the saved int.
	clarity float64
}

// newSettingsState is the state showing settings. A zero value from a caller
// that did not load settings reads as the default, not as a fully opaque
// window.
func newSettingsState(settings model.Settings) settingsState {
	if settings.Theme == "" {
		settings.Theme = model.ThemeOcean
	}
	if settings.WindowClarity < model.WindowClarityMin || settings.WindowClarity > model.WindowClarityMax {
		settings.WindowClarity = model.DefaultWindowClarity
	}
	delayURLIdx := slices.Index(service.DelayTestURLs, service.DelayTestURL(settings))
	if delayURLIdx < 0 {
		delayURLIdx = 0
	}
	return settingsState{
		settings:        settings,
		appearanceIndex: slices.Index(service.Appearances, settings.Appearance),
		languageIndex:   slices.Index(service.Languages, settings.Language),
		timeoutIndex:    slices.Index(service.DelayTimeouts, settings.DelayTimeoutSeconds),
		delayURLIndex:   delayURLIdx,
		updateIndex:     slices.Index(service.SubscriptionUpdateIntervals, settings.SubscriptionUpdateHours),
		clarity:         float64(settings.WindowClarity),
	}
}

// isDark says whether Hop draws dark under appearance, when the system is
// dark or not.
func isDark(appearance model.Appearance, systemDark bool) bool {
	switch appearance {
	case model.AppearanceDark:
		return true
	case model.AppearanceLight:
		return false
	default:
		return systemDark
	}
}

// AppearanceSource is the system appearance Hop's windows take under
// appearance. Applying it makes the native materials, the main window's and
// the menu bar panel's, follow the user's choice with the palette drawn over
// them, rather than the system's.
func AppearanceSource(settings model.Settings) mygo.ThemeSource {
	if settings.Theme == model.ThemeHack {
		return mygo.ThemeDark
	}
	switch settings.Appearance {
	case model.AppearanceDark:
		return mygo.ThemeDark
	case model.AppearanceLight:
		return mygo.ThemeLight
	default:
		return mygo.ThemeSystem
	}
}

// theme is Hop's theme for this frame: the frame starts on the system's
// look, which the user's choice overrides.
func (d *Dashboard) theme(c *ui.Context) *ui.Theme {
	return hopColorTheme(isDark(d.settings.settings.Appearance, c.Theme().Dark), d.settings.settings.Theme)
}

// saveSettings keeps next and shows it; a save that fails leaves the page on
// what is saved. It writes on the UI thread: the file is tiny, and saves
// then land in the order the user made them.
func (d *Dashboard) saveSettings(next model.Settings) {
	previous := d.settings.settings
	if err := d.services.Settings.Save(next); err != nil {
		d.fail(actionSaveSettings, err)
		d.settings = newSettingsState(d.settings.settings)
		return
	}
	d.settings = newSettingsState(next)
	mygo.Theme.SetSource(AppearanceSource(next))
	if next.Language != previous.Language {
		mygo.App.SetLocale(string(next.Language))
		// The view carries names the service words in the language shown.
		if d.opCtx != nil {
			d.reloadViewAfterCore(d.opCtx, d.opWin)
		}
	}
}

// settingsWidth is the column the settings page centres in the window.
const settingsWidth float32 = 690

// settingsPage holds what the user sets once and then leaves: the look, the
// delay test and the core, in grouped rows the way macOS lays out its own
// settings, centred in the window.
func (d *Dashboard) settingsPage(c *ui.Context, t *ui.Theme) {
	s := &d.settings
	ui.Scroll(c).Grow(1).Basis(0).MinHeight(0).Children(func() {
		ui.Row(c).Justify(ui.Center).Padding(28, pageGutter(c), 40, pageGutter(c)).Children(func() {
			ui.Column(c).Grow(1).MaxWidth(settingsWidth).Gap(26).Children(func() {
				pageHeading(c, t, locales.PageSettings(), locales.SettingsSubtitle())
				launchRow := func() {
					settingRow(c, t, locales.SettingsLaunchAtLogin(), locales.SettingsLaunchAtLoginNote(), func() {
						on := d.openAtLogin
						ui.Switch(c.Key("open-at-login"), &on).Label(locales.SettingsLaunchAtLogin()).OnChange(func() {
							d.setOpenAtLogin(!d.openAtLogin)
						})
					})
				}
				menuBarSpeedRow := func() {
					settingRow(c, t, locales.SettingsMenuBarSpeed(), locales.SettingsMenuBarSpeedNote(), func() {
						on := s.settings.MenuBarSpeed
						ui.Switch(c.Key("menu-bar-speed"), &on).Label(locales.SettingsMenuBarSpeed()).OnChange(func() {
							next := s.settings
							next.MenuBarSpeed = !next.MenuBarSpeed
							d.saveSettings(next)
						})
					})
				}
				globalShortcutsRow := func() {
					show, proxy, mode := shortcutDisplayLabels()
					settingRow(c, t, locales.SettingsGlobalShortcuts(), locales.SettingsGlobalShortcutsNote(show, proxy, mode), func() {
						on := s.settings.GlobalShortcuts
						ui.Switch(c.Key("global-shortcuts"), &on).Label(locales.SettingsGlobalShortcuts()).OnChange(func() {
							d.setGlobalShortcuts(!s.settings.GlobalShortcuts)
						})
					})
				}
				terminalProxyRow := func() {
					settingRow(c, t, locales.SettingsTerminalProxy(), locales.SettingsTerminalProxyNote(), func() {
						button(c, buttonSpec{label: locales.SettingsCopyCommand(), icon: iconTerminal, style: mainSoftButton(t)}, func() {
							c.WriteClipboard(service.TerminalProxyCommand(runtime.GOOS, d.proxyAddress()))
							c.Toast(locales.SettingsCommandCopied())
						})
					})
				}
				timeoutRow := func() {
					settingRow(c, t, locales.SettingsTimeout(), locales.SettingsTimeoutNote(), func() {
						slidingSwitch(c, "timeout", &s.timeoutIndex, timeoutLabels(), pageSwitchStyle(t, 52), func(i int) {
							next := s.settings
							next.DelayTimeoutSeconds = service.DelayTimeouts[i]
							d.saveSettings(next)
						})
					})
				}
				delayURLRow := func() {
					settingRow(c, t, locales.SettingsDelayUrl(), locales.SettingsDelayUrlNote(), func() {
						slidingSwitch(c, "delay-url", &s.delayURLIndex, delayTestURLLabels, pageSwitchStyle(t, 74), func(i int) {
							next := s.settings
							next.DelayTestURL = service.DelayTestURLs[i]
							d.saveSettings(next)
						})
					})
				}
				themeRow := func() {
					ui.Column(c.Key("color-theme")).Gap(gapMedium).Children(func() {
						ui.Column(c).Gap(gapLine).Children(func() {
							ui.Text(c, locales.SettingsTheme()).FontSize(fontBody).FontWeight(weightMedium)
							mutedText(c, t, locales.SettingsThemeNote())
						})
						d.themePicker(c, t)
					})
				}
				appearanceRow := func() {
					settingRow(c, t, locales.SettingsAppearance(), "", func() {
						slidingSwitch(c, "appearance", &s.appearanceIndex, appearanceLabels(), pageSwitchStyle(t, 64), func(i int) {
							next := s.settings
							next.Appearance = service.Appearances[i]
							d.saveSettings(next)
						})
					})
				}
				languageRow := func() {
					settingRow(c, t, locales.SettingsLanguage(), locales.SettingsLanguageNote(), func() {
						slidingSwitch(c, "language", &s.languageIndex, languageLabels(), pageSwitchStyle(t, languageSegmentWidth), func(i int) {
							next := s.settings
							next.Language = service.Languages[i]
							d.saveSettings(next)
						})
					})
				}
				clarityRow := func() {
					settingRow(c, t, locales.SettingsClarity(), locales.SettingsClarityNote(), func() {
						ui.Row(c).Gap(gapSmall).AlignItems(ui.Center).Children(func() {
							ui.Slider(c, &s.clarity, model.WindowClarityMin, model.WindowClarityMax).
								Label(locales.SettingsClarity()).Width(150).OnChange(func() {
								next := s.settings
								next.WindowClarity = int(math.Round(s.clarity))
								if next.WindowClarity != s.settings.WindowClarity {
									d.saveSettings(next)
								}
							})
							ui.Text(c, fmt.Sprintf("%d%%", int(math.Round(s.clarity)))).
								FontSize(fontSecondary).FontFeatures("tnum").Width(34).TextAlign(ui.End)
						})
					})
				}
				subscriptionUpdateRow := func() {
					settingRow(c, t, locales.SettingsAutoUpdate(), locales.SettingsAutoUpdateNote(), func() {
						slidingSwitch(c, "subscription-update", &s.updateIndex, updateLabels(), pageSwitchStyle(t, 58), func(i int) {
							next := s.settings
							next.SubscriptionUpdateHours = service.SubscriptionUpdateIntervals[i]
							d.saveSettings(next)
						})
					})
				}

				// The groups go by what the user is doing, in the order a
				// first run goes: how Hop looks, how it starts, how it
				// reaches the network, then what its rules and its core are.
				interfaceRows := []func(){themeRow}
				// The Hack theme is one fixed look: no light or dark, no glass.
				if s.settings.Theme != model.ThemeHack {
					interfaceRows = append(interfaceRows, appearanceRow)
					if runtime.GOOS == "darwin" && c.Vibrancy() {
						interfaceRows = append(interfaceRows, clarityRow)
					}
				}
				interfaceRows = append(interfaceRows, languageRow)
				settingsGroup(c, t, locales.SettingsInterface(), interfaceRows...)
				settingsGroup(c, t, locales.SettingsStartup(), launchRow, menuBarSpeedRow, globalShortcutsRow)
				settingsGroup(c, t, locales.SettingsNetwork(), d.portRow(c, t), d.proxyBypassRow(c, t), terminalProxyRow, timeoutRow, delayURLRow)
				settingsGroup(c, t, locales.SettingsLan(), d.lanRows(c, t)...)
				settingsGroup(c, t, locales.SettingsSubscriptionsRules(), subscriptionUpdateRow, d.geodataRow(c, t))
				settingsGroup(c, t, locales.SettingsCustomRules(), d.customRulesRows(c, t)...)
				settingsGroup(c, t, locales.SettingsTun(), d.fakeIPRows(c, t)...)
				aboutRows := []func(){}
				if d.core.loaded {
					aboutRows = append(append(append(aboutRows, d.coreRows(c, t)...), d.helperRows(c, t)...), d.memoryRow(c, t))
				}
				settingsGroup(c, t, locales.SettingsCoreAbout(), append(aboutRows, d.appVersionRow(c, t))...)
				mutedText(c, t, locales.SettingsLicense()).AlignSelf(ui.Center)
			})
		})
	})
}

// settingsGroup is a titled card of rows, as the design's setting-list.
func settingsGroup(c *ui.Context, t *ui.Theme, title string, rows ...func()) ui.Element {
	return ui.Column(c.Key(title)).Gap(10).Children(func() {
		mutedText(c, t, title).FontWeight(weightMedium).Padding(0, 14)
		panelCard(c, t).Padding(0, 16).Children(func() {
			for i, row := range rows {
				if i > 0 {
					horizontalHairline(c, t)
				}
				ui.Box(c.Key(i)).Padding(16, 0).Children(row)
			}
		})
	})
}

// settingRow names a setting, with a short note under it when it needs one,
// and its control on the right.
func settingRow(c *ui.Context, t *ui.Theme, label string, note string, control func()) {
	ui.Row(c.Key(label)).Gap(gapLarge).AlignItems(ui.Center).MinHeight(switchHeight).Children(func() {
		ui.Column(c).Grow(1).Basis(0).Gap(gapLine).Children(func() {
			ui.Text(c, label).FontSize(fontBody).FontWeight(weightMedium)
			if note != "" {
				mutedText(c, t, note)
			}
		})
		control()
	})
}

// themePicker lays out all offered themes in balanced rows of swatch cards so
// each option previews its palette colors without wrapping into awkward orphans.
func (d *Dashboard) themePicker(c *ui.Context, t *ui.Theme) {
	themes := service.ColorThemes
	ui.Column(c.Key("theme-picker")).Gap(gapSmall).Children(func() {
		for start := 0; start < len(themes); start += themePickerCols {
			end := min(start+themePickerCols, len(themes))
			ui.Row(c.Key(start)).Gap(gapSmall).Children(func() {
				for _, theme := range themes[start:end] {
					d.themeSwatchButton(c, t, theme)
				}
			})
		}
	})
}

// themeSwatchButton is one interactive theme tile showing a two-stop gradient
// preview orb, the theme name, and an active check mark when selected.
func (d *Dashboard) themeSwatchButton(c *ui.Context, t *ui.Theme, theme model.ColorTheme) {
	selected := d.settings.settings.Theme == theme
	label := colorThemeLabel(theme)
	b := ui.ButtonBase(c.Key(string(theme))).Label(label).
		Grow(1).Basis(0).Height(themeChipHeight).Padding(0, themeChipPadX).Gap(gapSmall).
		Justify(ui.Start).Radius(themeChipRadius).Cursor(ui.CursorPointer)

	var fill, border ui.Color
	switch {
	case selected && onGlass(t):
		fill = glassButton(t).Mix(t.Accent, 0.16)
		border = glassSelectedBorder(t)
	case selected:
		fill = t.Accent.Alpha(0.12)
		border = t.Accent
	case b.Hovered() && onGlass(t):
		fill = glassButton(t)
		border = glassStrongLine(t)
	case b.Hovered():
		fill = t.Text.Alpha(softHoverAlpha)
		border = t.Border
	case onGlass(t):
		fill = glassFill(t, glassTrackAlpha)
		border = t.Border
	default:
		fill = t.Text.Alpha(softFillAlpha)
		border = t.Border
	}

	ink := t.Text.Alpha(0.82)
	weight := weightMedium
	if selected {
		ink = t.Text
		weight = weightSemibold
	} else if b.Hovered() {
		ink = t.Text
	}

	b.Background(b.AnimateColor("fill", fill, ui.Smooth)).
		Border(hairlineWidth, b.AnimateColor("border", border, ui.Smooth))

	scale := float32(1)
	if b.Pressed() {
		scale = pressedScale
	}
	b.AnimatePose(ui.Pose{Scale: scale}, ui.Snappy)

	b.OnClick(func() {
		if d.settings.settings.Theme != theme {
			next := d.settings.settings
			next.Theme = theme
			d.saveSettings(next)
		}
	})

	from, to := themeSwatchColors(theme)
	orbRing := ui.RGBA(0, 0, 0, 0.14)
	if t.Dark {
		orbRing = ui.RGBA(255, 255, 255, 0.24)
	}
	b.Children(func() {
		ui.Box(c).Width(themeOrbSize).Height(themeOrbSize).Shrink(0).Radius(themeOrbSize/2).
			LinearGradient(ui.LinearGradient{Angle: themeSwatchAngle, From: from, To: to}).
			Border(hairlineWidth, orbRing)
		ui.Text(c, label).FontSize(fontSecondary).FontWeight(weight).
			TextColor(b.AnimateColor("ink", ink, ui.Smooth)).
			Grow(1).NoWrap().Translate(0, -opticalLift)
		if selected {
			ui.Icon(c, iconCheck).Width(iconInline).Height(iconInline).Shrink(0).TextColor(accentInk(t))
		}
	})
}

// setOpenAtLogin asks the system to start Hop at login, or not, and shows
// what the system holds afterwards.
func (d *Dashboard) setOpenAtLogin(open bool) {
	if err := d.services.LoginItem.SetOpenAtLogin(open); err != nil {
		d.fail(actionOpenAtLogin, err)
	}
	d.openAtLogin = d.services.LoginItem.OpenAtLogin()
}
