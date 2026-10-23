package handler

import (
	"fmt"
	"math"
	"runtime"
	"slices"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"hop/internal/model"
	"hop/internal/service"
)

// appearanceLabels name service.Appearances, index for index.
var appearanceLabels = []string{"跟随系统", "浅色", "深色"}

// colorThemeLabels name service.ColorThemes, index for index.
var colorThemeLabels = []string{"海蓝", "暮紫", "纸张"}

// timeoutLabels name service.DelayTimeouts, index for index.
var timeoutLabels = []string{"2 秒", "3 秒", "5 秒"}

// updateLabels name service.SubscriptionUpdateIntervals, index for index.
var updateLabels = []string{"关闭", "6 小时", "12 小时", "24 小时"}

// settingsState is what the settings page shows and edits. The indexes are
// what its switches hold, kept in step with settings.
type settingsState struct {
	settings        model.Settings
	appearanceIndex int
	themeIndex      int
	timeoutIndex    int
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
	return settingsState{
		settings:        settings,
		appearanceIndex: slices.Index(service.Appearances, settings.Appearance),
		themeIndex:      slices.Index(service.ColorThemes, settings.Theme),
		timeoutIndex:    slices.Index(service.DelayTimeouts, settings.DelayTimeoutSeconds),
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
func AppearanceSource(appearance model.Appearance) mygo.ThemeSource {
	switch appearance {
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
	if err := d.services.Settings.Save(next); err != nil {
		d.fail(actionSaveSettings, err)
		d.settings = newSettingsState(d.settings.settings)
		return
	}
	d.settings = newSettingsState(next)
	mygo.Theme.SetSource(AppearanceSource(next.Appearance))
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
				pageHeading(c, t, "设置", "按你的习惯,让 Hop 安静运行")
				general := []func(){
					func() {
						settingRow(c, t, "开机启动", "登录后在菜单栏里运行,不打开窗口", func() {
							on := d.openAtLogin
							ui.Switch(c.Key("open-at-login"), &on).Label("开机启动").OnChange(func() {
								d.setOpenAtLogin(!d.openAtLogin)
							})
						})
					},
					func() {
						settingRow(c, t, "菜单栏网速", "在菜单栏图标旁显示实时上传和下载速度", func() {
							on := s.settings.MenuBarSpeed
							ui.Switch(c.Key("menu-bar-speed"), &on).Label("菜单栏网速").OnChange(func() {
								next := s.settings
								next.MenuBarSpeed = !next.MenuBarSpeed
								d.saveSettings(next)
							})
						})
					},
					func() {
						settingRow(c, t, "终端代理", "终端不走系统代理,粘贴这条命令后当前窗口就经过 Hop;增强模式下不需要", func() {
							button(c, buttonSpec{label: "复制命令", icon: iconTerminal, style: mainSoftButton(t)}, func() {
								c.WriteClipboard(service.TerminalProxyCommand(runtime.GOOS, d.proxyAddress))
								c.Toast("已复制终端代理命令,粘贴到终端即可")
							})
						})
					},
					func() {
						settingRow(c, t, "主题", "玻璃背景配色,切换后立即生效", func() {
							slidingSwitch(c, "color-theme", &s.themeIndex, colorThemeLabels, pageSwitchStyle(t, themeSegmentWidth), func(i int) {
								next := s.settings
								next.Theme = service.ColorThemes[i]
								d.saveSettings(next)
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
				}
				if runtime.GOOS == "darwin" && c.Vibrancy() {
					general = append(general, func() {
						settingRow(c, t, "窗口通透度", "数值越高,玻璃越通透", func() {
							ui.Row(c).Gap(gapSmall).AlignItems(ui.Center).Children(func() {
								ui.Slider(c, &s.clarity, model.WindowClarityMin, model.WindowClarityMax).
									Label("窗口通透度").Width(150).OnChange(func() {
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
					})
				}
				general = append(general,
					func() {
						settingRow(c, t, "测速超时", "越短测得越快,较慢的线路可能被记为超时", func() {
							slidingSwitch(c, "timeout", &s.timeoutIndex, timeoutLabels, pageSwitchStyle(t, 52), func(i int) {
								next := s.settings
								next.DelayTimeoutSeconds = service.DelayTimeouts[i]
								d.saveSettings(next)
							})
						})
					},
					d.appVersionRow(c, t),
				)
				settingsGroup(c, t, "通用", general...)
				settingsGroup(c, t, "订阅",
					func() {
						settingRow(c, t, "自动更新", "按间隔刷新已保存的订阅，本地配置不刷新；失败后等下一个间隔再试", func() {
							slidingSwitch(c, "subscription-update", &s.updateIndex, updateLabels, pageSwitchStyle(t, 58), func(i int) {
								next := s.settings
								next.SubscriptionUpdateHours = service.SubscriptionUpdateIntervals[i]
								d.saveSettings(next)
							})
						})
					},
				)
				settingsGroup(c, t, "规则", d.geodataRow(c, t))
				settingsGroup(c, t, "自定义规则", d.customRulesRows(c, t)...)
				if d.core.loaded {
					settingsGroup(c, t, "内核", append(d.coreRows(c, t), d.helperRows(c, t)...)...)
				}
				mutedText(c, t, "以 GPL-3.0 许可发布,内置的 mihomo 内核同为 GPL-3.0。").AlignSelf(ui.Center)
			})
		})
	})
}

// settingsGroup is a titled card of rows, as the design's setting-list.
func settingsGroup(c *ui.Context, t *ui.Theme, title string, rows ...func()) {
	ui.Column(c).Key(title).Gap(10).Children(func() {
		mutedText(c, t, title).FontWeight(weightMedium).Padding(0, 14)
		panelCard(c, t).Padding(0, 16).Children(func() {
			for i, row := range rows {
				if i > 0 {
					horizontalHairline(c, t)
				}
				ui.Box(c).Key(i).Padding(16, 0).Children(row)
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
