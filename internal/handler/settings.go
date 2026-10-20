package handler

import (
	"slices"

	"github.com/egoist/mygo/ui"

	"hop/internal/model"
	"hop/internal/service"
)

// appearanceLabels name service.Appearances, index for index.
var appearanceLabels = []string{"跟随系统", "浅色", "深色"}

// timeoutLabels name service.DelayTimeouts, index for index.
var timeoutLabels = []string{"2 秒", "3 秒", "5 秒"}

// settingsState is what the settings page shows and edits. The indexes are
// what its switches hold, kept in step with settings.
type settingsState struct {
	settings        model.Settings
	appearanceIndex int
	timeoutIndex    int
}

// newSettingsState is the state showing settings.
func newSettingsState(settings model.Settings) settingsState {
	return settingsState{
		settings:        settings,
		appearanceIndex: slices.Index(service.Appearances, settings.Appearance),
		timeoutIndex:    slices.Index(service.DelayTimeouts, settings.DelayTimeoutSeconds),
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

// theme is Hop's theme for this frame: the frame starts on the system's
// look, which the user's choice overrides.
func (d *Dashboard) theme(c *ui.Context) *ui.Theme {
	return hopTheme(isDark(d.settings.settings.Appearance, c.Theme().Dark))
}

// saveSettings keeps next and shows it; a save that fails leaves the page on
// what is saved. It writes on the UI thread: the file is tiny, and saves
// then land in the order the user made them.
func (d *Dashboard) saveSettings(next model.Settings) {
	if err := d.services.Settings.Save(next); err != nil {
		d.fail(actionSaveSettings, err)
		d.settings = newSettingsState(d.settings.settings)
		return
	}
	d.settings = newSettingsState(next)
}

// settingsWidth is the column the settings page centres in the window.
const settingsWidth float32 = 600

// settingsPage holds what the user sets once and then leaves: the look, the
// delay test and the core, in grouped rows the way macOS lays out its own
// settings, centred in the window.
func (d *Dashboard) settingsPage(c *ui.Context, t *ui.Theme) {
	s := &d.settings
	ui.Scroll(c).Grow(1).Basis(0).MinHeight(0).Children(func() {
		ui.Row(c).Justify(ui.Center).Padding(scrollTrim, pagePadding, scrollEnd, pagePadding).Children(func() {
			ui.Column(c).Grow(1).MaxWidth(settingsWidth).Gap(gapSection).Children(func() {
				settingsGroup(c, t, "通用",
					func() {
						settingRow(c, t, "开机启动", "登录后在菜单栏里运行,不打开窗口", func() {
							on := d.openAtLogin
							ui.Switch(c.Key("open-at-login"), &on).Label("开机启动").OnChange(func() {
								d.setOpenAtLogin(!d.openAtLogin)
							})
						})
					},
					func() {
						settingRow(c, t, "外观", "", func() {
							slidingSwitch(c, "appearance", &s.appearanceIndex, appearanceLabels, pageSwitchStyle(t, 64), func(i int) {
								next := s.settings
								next.Appearance = service.Appearances[i]
								d.saveSettings(next)
							})
						})
					},
					func() {
						settingRow(c, t, "测速超时", "越短测得越快,较慢的线路可能被记为超时", func() {
							slidingSwitch(c, "timeout", &s.timeoutIndex, timeoutLabels, pageSwitchStyle(t, 52), func(i int) {
								next := s.settings
								next.DelayTimeoutSeconds = service.DelayTimeouts[i]
								d.saveSettings(next)
							})
						})
					},
				)
				if d.core.loaded {
					settingsGroup(c, t, "内核", append(d.coreRows(c, t), d.helperRows(c, t)...)...)
				}
				mutedText(c, t, "Hop "+d.appVersion+" 以 GPL-3.0 许可发布,内置的 mihomo 内核同为 GPL-3.0。").AlignSelf(ui.Center)
			})
		})
	})
}

// settingsGroup is a titled card of rows, with a hairline between them.
func settingsGroup(c *ui.Context, t *ui.Theme, title string, rows ...func()) {
	ui.Column(c).Key(title).Gap(gapSmall).Children(func() {
		ui.Text(c, title).FontSize(fontSecondary).FontWeight(weightSemibold).TextColor(t.TextMuted).Padding(0, gapLarge)
		ui.Column(c).Radius(radiusPanel).Background(t.Surface).Border(1, t.Border).Children(func() {
			for i, row := range rows {
				if i > 0 {
					ui.Box(c).Height(1).Margin(0, gapLarge).Background(t.Border.Alpha(cardDividerAlpha))
				}
				ui.Box(c).Key(i).Padding(gapMedium, gapLarge).Children(row)
			}
		})
	})
}

// settingRow names a setting, with a short note under it when it needs one,
// and its control on the right.
func settingRow(c *ui.Context, t *ui.Theme, label string, note string, control func()) {
	ui.Row(c).Key(label).Gap(gapLarge).AlignItems(ui.Center).MinHeight(switchHeight).Children(func() {
		ui.Column(c).Grow(1).Basis(0).Gap(gapLine).Children(func() {
			ui.Text(c, label).FontSize(fontBody).FontWeight(weightMedium)
			if note != "" {
				mutedText(c, t, note)
			}
		})
		control()
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
