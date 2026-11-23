package handler

import (
	"context"
	"strconv"
	"strings"

	"github.com/egoist/mygo/ui"

	"hop/internal/model"
	"hop/internal/service"
	"hop/locales"
)

// rulesInspectState holds the core's live routing rules, remote providers,
// offline domain/IP match diagnostic input, and list filter.
type rulesInspectState struct {
	rules       []model.ActiveRule
	providers   []model.ProviderSummary
	loaded      bool
	loading     bool
	updating    map[string]bool
	updatingAll bool
	diagnose    string
	filter      string
	list        ui.ListState
}

func (d *Dashboard) ensureRuleInspectLoaded() {
	if d.ruleInspect.loaded || d.ruleInspect.loading || d.services.RuleInspect == nil || d.opCtx == nil {
		return
	}
	d.refreshRuleInspect()
}

// invalidateRuleInspect marks the cached active rule table stale after a
// profile or custom rule change, refreshing immediately if the Rules view is
// currently shown.
func (d *Dashboard) invalidateRuleInspect() {
	d.ruleInspect.loaded = false
	if d.page == pageActivity && d.activity == activityRules {
		d.refreshRuleInspect()
	}
}

func (d *Dashboard) refreshRuleInspect() {
	if d.ruleInspect.loading || d.services.RuleInspect == nil || d.opCtx == nil {
		return
	}
	d.ruleInspect.loading = true
	ctx, win := d.opCtx, d.opWin
	go d.loadRuleInspect(ctx, win)
}

func (d *Dashboard) loadRuleInspect(ctx context.Context, win Updater) {
	if d.services.RuleInspect == nil {
		return
	}
	rules, rulesErr := d.services.RuleInspect.ListRules(ctx)
	providers, provErr := d.services.RuleInspect.ListProviders(ctx)
	win.Update(func() {
		d.ruleInspect.loading = false
		if rulesErr != nil {
			d.fail(actionLoadRulesInspect, rulesErr)
			return
		}
		d.ruleInspect.rules = rules
		if provErr == nil {
			d.ruleInspect.providers = providers
		}
		d.ruleInspect.loaded = true
	})
}

func (d *Dashboard) updateProvider(kind string, name string) {
	if d.services.RuleInspect == nil || d.opCtx == nil || d.controlsLocked() {
		return
	}
	key := kind + ":" + name
	if d.ruleInspect.updating == nil {
		d.ruleInspect.updating = map[string]bool{}
	}
	if d.ruleInspect.updating[key] || d.ruleInspect.updatingAll {
		return
	}
	d.ruleInspect.updating[key] = true
	ctx, win := d.opCtx, d.opWin
	go func() {
		err := d.services.RuleInspect.UpdateProvider(ctx, kind, name)
		win.Update(func() {
			delete(d.ruleInspect.updating, key)
			if err != nil {
				d.fail(actionUpdateProvider, err)
				return
			}
			gen := d.nextViewGen()
			go d.loadView(ctx, win, gen)
			d.refreshRuleInspect()
		})
	}()
}

func (d *Dashboard) updateAllProviders() {
	if d.services.RuleInspect == nil || d.opCtx == nil || d.controlsLocked() || d.ruleInspect.updatingAll || len(d.ruleInspect.providers) == 0 {
		return
	}
	d.ruleInspect.updatingAll = true
	providers := append([]model.ProviderSummary(nil), d.ruleInspect.providers...)
	ctx, win := d.opCtx, d.opWin
	go func() {
		err := d.services.RuleInspect.UpdateAllProviders(ctx, providers)
		win.Update(func() {
			d.ruleInspect.updatingAll = false
			if err != nil {
				d.fail(actionUpdateProvider, err)
			}
			gen := d.nextViewGen()
			go d.loadView(ctx, win, gen)
			d.refreshRuleInspect()
		})
	}()
}

// rulesView renders the Activity → Rules view: domain/IP routing diagnostic,
// optional remote provider refresh strip, and virtualized active rules list.
func (d *Dashboard) rulesView(c *ui.Context, t *ui.Theme, contentW float32) {
	d.ensureRuleInspectLoaded()
	st := &d.ruleInspect
	ui.Column(c.Key("rulesInspect")).Grow(1).Basis(0).MinHeight(0).Gap(gapMedium).Children(func() {
		d.ruleDiagnoseCard(c, t)
		if len(st.providers) > 0 {
			d.ruleProvidersCard(c, t)
		}
		ui.Row(c.Key("rulesToolbar")).Wrap().Gap(gapSmall).GapY(gapSmall).AlignItems(ui.Center).Children(func() {
			ui.TextInput(c.Key("rulesSearch"), &st.filter).
				Placeholder(locales.RulesFilterPlaceholder()).Width(min(260, max(160, contentW*0.34)))
			visible := filterActiveRules(st.rules, st.filter)
			captionText(c, t, locales.RulesCount(len(visible))).FontFeatures("tnum").NoWrap()
			ui.Box(c).Grow(1)
			button(c, buttonSpec{
				label:    locales.RulesRefresh(),
				icon:     iconRefresh,
				style:    mainSoftButton(t),
				busy:     st.loading,
				disabled: d.controlsLocked(),
			}, d.refreshRuleInspect)
		})
		visible := filterActiveRules(st.rules, st.filter)
		if len(visible) == 0 {
			emptyState(c, t, locales.RulesInspectEmpty(), locales.RulesInspectEmptyHint())
			return
		}
		st.list.Key = func(row int) any { return visible[row].Index }
		panelCard(c, t).Key("rulesListCard").Grow(1).Basis(0).MinHeight(0).
			Padding(6, 16).Radius(radiusPanel+2).Shadow(0, 3, 12, 0, glassCardShadow(t)).Children(func() {
			ui.List(c, &st.list, len(visible), func(i int) {
				activeRuleRow(c, t, visible[i], i == len(visible)-1)
			}).Fill()
		})
	})
}

func (d *Dashboard) ruleDiagnoseCard(c *ui.Context, t *ui.Theme) {
	st := &d.ruleInspect
	panelCard(c, t).Key("ruleDiagCard").Padding(12, 16).Radius(radiusPanel+2).
		Shadow(0, 2, 10, 0, glassCardShadow(t)).Children(func() {
		ui.Column(c).Gap(gapSmall).Children(func() {
			ui.Row(c).Gap(gapSmall).AlignItems(ui.Center).Children(func() {
				ui.Icon(c, iconSearch).Width(iconLead).Height(iconLead).TextColor(t.TextMuted)
				ui.TextInput(c.Key("ruleDiagInput"), &st.diagnose).
					Placeholder(locales.RulesDiagnosePlaceholder()).Grow(1).Basis(0)
				if strings.TrimSpace(st.diagnose) != "" {
					iconButton(c, locales.Close(), iconClose, softButton(t), func() { st.diagnose = "" })
				}
			})
			if strings.TrimSpace(st.diagnose) == "" {
				return
			}
			diag := service.DiagnoseRule(st.diagnose, st.rules, d.view.Groups)
			horizontalHairline(c, t)
			if !diag.Matched {
				mutedText(c, t, locales.RulesNoMatch())
				return
			}
			ui.Column(c).Gap(gapTight).Children(func() {
				ui.Row(c).Wrap().Gap(gapSmall).GapY(gapTight).AlignItems(ui.Center).Children(func() {
					badge(c, t, locales.RulesMatchedBadge(diag.Rule.Index+1))
					ruleText := diag.Rule.Type
					if diag.Rule.Payload != "" {
						ruleText += " · " + diag.Rule.Payload
					}
					ui.Text(c, ruleText).FontSize(fontSecondary).FontWeight(weightMedium).Selectable()
					ui.Box(c).Grow(1)
					if len(diag.Chain) > 0 {
						ui.Row(c).Gap(gapTight).AlignItems(ui.Center).Children(func() {
							for i, hop := range diag.Chain {
								if i > 0 {
									captionText(c, t, "›")
								}
								if i == len(diag.Chain)-1 {
									chip(c, t, ruleTargetLabel(hop), ruleTargetColor(t, hop))
								} else {
									mutedText(c, t, hop).MaxLines(1)
								}
							}
						})
					}
				})
				if diag.SkippedDynamic > 0 {
					captionText(c, t, locales.RulesDynamicNote(diag.SkippedDynamic))
				}
			})
		})
	})
}

func (d *Dashboard) ruleProvidersCard(c *ui.Context, t *ui.Theme) {
	st := &d.ruleInspect
	panelCard(c, t).Key("ruleProvidersCard").Padding(10, 16).Radius(radiusPanel+2).
		Shadow(0, 2, 10, 0, glassCardShadow(t)).Children(func() {
		ui.Column(c).Gap(gapSmall).Children(func() {
			ui.Row(c).Gap(gapSmall).AlignItems(ui.Center).Children(func() {
				ui.Text(c, locales.RulesProvidersTitle()).FontSize(fontSecondary).FontWeight(weightSemibold).Grow(1)
				button(c, buttonSpec{
					label:    locales.RulesProvidersUpdateAll(),
					icon:     iconRefresh,
					style:    softButton(t),
					busy:     st.updatingAll,
					disabled: d.controlsLocked(),
				}, d.updateAllProviders)
			})
			for i, p := range st.providers {
				if i > 0 {
					horizontalHairline(c, t)
				}
				prov := p
				key := prov.Kind + ":" + prov.Name
				busy := st.updating[key] || st.updatingAll
				kindLabel := locales.RulesProviderRule()
				if prov.Kind == model.ProviderKindProxy {
					kindLabel = locales.RulesProviderProxy()
				}
				ui.Row(c.Key("prov:"+key)).Gap(gapSmall).AlignItems(ui.Center).Padding(4, 0).Children(func() {
					badge(c, t, kindLabel)
					ui.Text(c, prov.Name).FontSize(fontSecondary).FontWeight(weightMedium).Grow(1).Basis(0).MaxLines(1)
					if prov.Count > 0 {
						captionText(c, t, locales.RulesProviderCount(prov.Count)).FontFeatures("tnum").NoWrap()
					}
					if !prov.UpdatedAt.IsZero() {
						captionText(c, t, prov.UpdatedAt.Format("15:04")).FontFeatures("tnum").NoWrap()
					}
					ui.Box(c).Disabled(d.controlsLocked() || busy).Children(func() {
						iconButton(c, locales.RulesRefresh(), iconRefresh, softButton(t), func() {
							d.updateProvider(prov.Kind, prov.Name)
						})
					})
				})
			}
		})
	})
}

func activeRuleRow(c *ui.Context, t *ui.Theme, rule model.ActiveRule, last bool) {
	ui.Column(c).Children(func() {
		row := ui.Row(c).Gap(gapMedium).AlignItems(ui.Center).Padding(8, 0)
		if rule.Disabled {
			row.Opacity(disabledOpacity)
		}
		row.Children(func() {
			captionText(c, t, "#"+strconv.Itoa(rule.Index+1)).FontFeatures("tnum").Width(44).NoWrap()
			badge(c, t, rule.Type)
			payload := rule.Payload
			if payload == "" {
				payload = "—"
			}
			ui.Text(c, payload).FontSize(fontSecondary).Grow(1).Basis(0).MaxLines(1).Selectable()
			if rule.HitCount > 0 {
				captionText(c, t, strconv.FormatUint(rule.HitCount, 10)).FontFeatures("tnum").NoWrap()
			}
			chip(c, t, ruleTargetLabel(rule.Proxy), ruleTargetColor(t, rule.Proxy))
		})
		if !last {
			horizontalHairline(c, t)
		}
	})
}

func filterActiveRules(rules []model.ActiveRule, filter string) []model.ActiveRule {
	q := strings.ToLower(strings.TrimSpace(filter))
	if q == "" {
		return rules
	}
	var out []model.ActiveRule
	for _, r := range rules {
		if strings.Contains(strings.ToLower(r.Type), q) ||
			strings.Contains(strings.ToLower(r.Payload), q) ||
			strings.Contains(strings.ToLower(r.Proxy), q) {
			out = append(out, r)
		}
	}
	return out
}
