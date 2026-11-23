package handler

import (
	"context"
	"slices"
	"strconv"
	"strings"

	"github.com/egoist/mygo/ui"

	"hop/internal/model"
	"hop/internal/service"
	"hop/locales"
)

// ruleTypeLabels name service.CustomRuleTypes, index for index; ruleTypeHints
// say what each matches, in the same order.
func ruleTypeLabels() []string {
	return []string{locales.RuleTypeSuffix(), locales.RuleTypeDomain(), locales.RuleTypeKeyword(), locales.RuleTypeCidr(), locales.RuleTypeProcess()}
}

func ruleTypeHints() []string {
	return []string{locales.RuleHintSuffix(), locales.RuleHintDomain(), locales.RuleHintKeyword(), locales.RuleHintCidr(), locales.RuleHintProcess()}
}

var ruleValuePlaceholders = []string{"example.com", "api.example.com", "example", "192.0.2.0/24", "Telegram"}

// ruleTargetLabels name service.CustomRuleTargets, index for index.
func ruleTargetLabels() []string {
	return []string{locales.RouteDirect(), locales.RouteProxy(), locales.RouteReject()}
}

// customRulesState is the stored rules and the add dialog.
type customRulesState struct {
	rules  []model.CustomRule
	loaded bool
	failed bool
	busy   bool
	// editorOpen and editor hold the add dialog between frames.
	editorOpen bool
	editor     ruleEditor
}

// ruleEditor is the add dialog's fields. When customTarget is non-empty, it
// names a specific policy group from the active profile instead of one of the
// built-in targets indexed by targetIndex.
type ruleEditor struct {
	typeIndex    int
	targetIndex  int
	customTarget string
	value        string
	err          string
	// suggestions are the rules offered for the connection the dialog was
	// opened from; none when opened from the settings.
	suggestions []ruleSuggestion
}

// loadRules reads the custom rules.
func (d *Dashboard) loadRules(ctx context.Context, win Updater) {
	rules, err := d.services.Rules.List()
	win.Update(func() {
		if err != nil {
			d.rules.failed = true
			d.fail(actionReadRules, err)
			return
		}
		d.rules.rules, d.rules.loaded, d.rules.failed = rules, true, false
	})
}

// openRuleEditor resets the add dialog and shows it. A rule aimed at the
// proxy is the one most people add, so that is the default.
func (d *Dashboard) openRuleEditor() {
	r := &d.rules
	if r.busy || d.controlsLocked() {
		return
	}
	r.editor = ruleEditor{targetIndex: slices.Index(service.CustomRuleTargets, model.CustomRuleProxy)}
	r.editorOpen = true
}

// openRuleEditorFor opens the add dialog for conn with the likeliest rule
// filled in; the others are one click away.
func (d *Dashboard) openRuleEditorFor(conn model.Connection) {
	r := &d.rules
	if !r.loaded {
		return
	}
	d.openRuleEditor()
	if !r.editorOpen {
		return
	}
	r.editor.suggestions = suggestRules(conn)
	if len(r.editor.suggestions) > 0 {
		r.editor.typeIndex, r.editor.value = r.editor.suggestions[0].typeIndex, r.editor.suggestions[0].value
	}
}

// submitRuleEditor begins saving the list with the dialog's rule added.
func (d *Dashboard) submitRuleEditor() {
	r := &d.rules
	e := &r.editor
	if r.busy || d.controlsLocked() {
		return
	}
	value := strings.TrimSpace(e.value)
	if value == "" {
		e.err = locales.RuleEnterContent()
		return
	}
	if e.typeIndex < 0 || e.typeIndex >= len(service.CustomRuleTypes) {
		e.err = locales.RuleChooseTypeTarget()
		return
	}
	var target string
	if e.targetIndex == len(service.CustomRuleTargets) {
		target = strings.TrimSpace(e.customTarget)
		if target == "" {
			e.err = locales.RuleChooseTypeTarget()
			return
		}
	} else if e.targetIndex >= 0 && e.targetIndex < len(service.CustomRuleTargets) {
		target = service.CustomRuleTargets[e.targetIndex]
	} else {
		e.err = locales.RuleChooseTypeTarget()
		return
	}
	e.err = ""
	rule := model.CustomRule{
		Type:   service.CustomRuleTypes[e.typeIndex],
		Value:  value,
		Target: target,
	}
	d.beginRuleSave(append(slices.Clone(r.rules), rule), true)
}

// toggleRule enables or disables the rule at index and reapplies the config.
func (d *Dashboard) toggleRule(index int) {
	r := &d.rules
	if index < 0 || index >= len(r.rules) {
		return
	}
	next := slices.Clone(r.rules)
	next[index].Disabled = !next[index].Disabled
	d.beginRuleSave(next, false)
}

// moveRule shifts the rule at index by delta (-1 up, +1 down) to adjust match
// priority, then reapplies the config.
func (d *Dashboard) moveRule(index int, delta int) {
	r := &d.rules
	dest := index + delta
	if index < 0 || index >= len(r.rules) || dest < 0 || dest >= len(r.rules) {
		return
	}
	next := slices.Clone(r.rules)
	next[index], next[dest] = next[dest], next[index]
	d.beginRuleSave(next, false)
}

// deleteRule begins saving the list without the rule at index.
func (d *Dashboard) deleteRule(index int) {
	r := &d.rules
	if index < 0 || index >= len(r.rules) {
		return
	}
	d.beginRuleSave(slices.Delete(slices.Clone(r.rules), index, index+1), false)
}

// beginRuleSave validates and applies next in the background, then reads the
// stored list back. The service keeps the previous list when the core rejects
// the rebuilt config; the dialog shows the error when it started the change,
// and the window's notice does otherwise.
func (d *Dashboard) beginRuleSave(next []model.CustomRule, fromEditor bool) {
	r := &d.rules
	if r.busy || d.controlsLocked() {
		return
	}
	r.busy = true
	ctx, win := d.opCtx, d.opWin
	go func() {
		err := d.services.Rules.Save(ctx, next)
		rules, listErr := d.services.Rules.List()
		win.Update(func() {
			r.busy = false
			if listErr != nil {
				r.failed = true
				d.fail(actionReadRules, listErr)
			} else {
				r.rules, r.loaded, r.failed = rules, true, false
			}
			if err != nil {
				if fromEditor && r.editorOpen {
					r.editor.err = err.Error()
					return
				}
				d.fail(actionSaveRules, err)
				return
			}
			if fromEditor {
				r.editorOpen = false
			}
			d.invalidateRuleInspect()
			gen := d.nextViewGen()
			go d.loadView(ctx, win, gen)
		})
	}()
}

// customRulesRows are the settings rows of the custom rules: one per rule,
// then the add row, which carries the empty state when there are none.
func (d *Dashboard) customRulesRows(c *ui.Context, t *ui.Theme) []func() {
	r := &d.rules
	rows := make([]func(), 0, len(r.rules)+1)
	for i, rule := range r.rules {
		rows = append(rows, d.customRuleRow(c, t, i, rule))
	}
	rows = append(rows, func() {
		ui.Row(c.Key("rules-add")).Gap(gapLarge).AlignItems(ui.Center).MinHeight(switchHeight).Children(func() {
			note := locales.RulesOrderNote()
			switch {
			case r.failed:
				note = locales.RulesReadFailed()
			case !r.loaded:
				note = locales.RulesReading()
			case len(r.rules) == 0:
				note = locales.RulesEmpty()
			}
			mutedText(c, t, note).Grow(1)
			button(c, buttonSpec{label: locales.RuleAdd(), icon: iconPlus, style: primaryButton(t),
				disabled: d.controlsLocked() || !r.loaded, busy: r.busy}, d.openRuleEditor)
		})
	})
	return rows
}

// customRuleRow is one stored rule: its enable switch, type and value, target
// chip, priority reorder buttons, and delete button.
func (d *Dashboard) customRuleRow(c *ui.Context, t *ui.Theme, index int, rule model.CustomRule) func() {
	return func() {
		locked := d.controlsLocked() || d.rules.busy
		total := len(d.rules.rules)
		ui.Row(c.Key("rule:" + strconv.Itoa(index))).Gap(gapMedium).AlignItems(ui.Center).MinHeight(switchHeight).Children(func() {
			enabled := !rule.Disabled
			ui.Box(c).Disabled(locked).Children(func() {
				ui.Switch(c.Key("rule-on:"+strconv.Itoa(index)), &enabled).
					Label(locales.RuleToggleLabel(rule.Value)).
					OnChange(func() { d.toggleRule(index) })
			})
			ui.Column(c).Grow(1).Basis(0).Gap(gapLine).Children(func() {
				meta := ruleTypeLabel(rule.Type)
				if rule.Disabled {
					meta += " · " + locales.RuleDisabledTag()
				}
				captionText(c, t, meta)
				valText := ui.Text(c, rule.Value).FontSize(fontBody).FontWeight(weightMedium).MaxLines(1)
				if rule.Disabled {
					valText.TextColor(t.TextMuted)
				}
			})
			targetColor := ruleTargetColor(t, rule.Target)
			if rule.Disabled {
				targetColor = t.TextMuted
			}
			chip(c, t, ruleTargetLabel(rule.Target), targetColor)
			ui.Row(c).Gap(gapTight).AlignItems(ui.Center).Children(func() {
				if total > 1 {
					ui.Box(c).Disabled(locked || index == 0).Children(func() {
						iconButton(c, locales.RuleMoveUp(), iconArrowUp, softButton(t), func() { d.moveRule(index, -1) })
					})
					ui.Box(c).Disabled(locked || index == total-1).Children(func() {
						iconButton(c, locales.RuleMoveDown(), iconArrowDown, softButton(t), func() { d.moveRule(index, 1) })
					})
				}
				ui.Box(c).Disabled(locked).Children(func() {
					iconButton(c, locales.RuleDelete(rule.Value), iconTrash, dangerButton(t), func() { d.deleteRule(index) })
				})
			})
		})
	}
}

// customRuleDialog is the add form over a dimmed window.
func (d *Dashboard) customRuleDialog(c *ui.Context, t *ui.Theme) {
	r := &d.rules
	animatedDialog(c, "rule", &r.editorOpen, func(backdrop, panel ui.Element) {
		backdrop.Background(dialogScrim)
		panel.Width(dialogWidth).Padding(pagePadding).Gap(gapLarge).Radius(radiusPanel).
			Background(overlaySurface(t)).Border(1, t.Border).Shadow(0, 10, 30, 0, dialogShadow)
		panel.Children(func() { d.customRuleForm(c, t) })
	})
}

func (d *Dashboard) customRuleForm(c *ui.Context, t *ui.Theme) {
	r := &d.rules
	e := &r.editor
	typeIndex := max(0, min(e.typeIndex, len(ruleTypeLabels())-1))
	hint := ruleTypeHints()[typeIndex]
	placeholder := ruleValuePlaceholders[typeIndex]
	hasGroups := len(d.view.Groups) > 0
	targetOptions := ruleTargetLabels()
	targetSegWidth := float32(64)
	if hasGroups {
		targetOptions = append(targetOptions, locales.RuleTargetGroup())
		targetSegWidth = 84
	} else if e.targetIndex >= len(service.CustomRuleTargets) {
		e.targetIndex = slices.Index(service.CustomRuleTargets, model.CustomRuleProxy)
		e.customTarget = ""
	}
	ui.Column(c).Gap(gapSection).Children(func() {
		ui.Column(c).Gap(gapSmall).Children(func() {
			ui.Row(c).Gap(gapMedium).AlignItems(ui.Center).Children(func() {
				ui.Text(c, locales.RuleAdd()).FontSize(fontHeadline).FontWeight(weightSemibold).Grow(1)
				iconButton(c, locales.Close(), iconClose, softButton(t), func() { r.editorOpen = false })
			})
			mutedText(c, t, locales.RuleDialogNote())
		})
		ui.Column(c).Gap(gapSmall).Children(func() {
			d.ruleSuggestionRow(c, t)
			fieldLabel(c, t, locales.RuleFieldType(), "")
			ui.Box(c).Disabled(r.busy).Children(func() {
				slidingSwitch(c, "rule-type", &e.typeIndex, ruleTypeLabels(), dialogSwitchStyle(t, 68), func(int) { e.err = "" })
			})
			captionText(c, t, hint)
			fieldLabel(c, t, locales.RuleFieldContent(), "")
			ui.TextInput(c, &e.value).Label(locales.RuleContentLabel()).Placeholder(placeholder).
				Background(dialogField(t)).Disabled(r.busy).OnSubmit(d.submitRuleEditor)
			fieldLabel(c, t, locales.RuleFieldTarget(), "")
			ui.Box(c).Disabled(r.busy).Children(func() {
				slidingSwitch(c, "rule-target", &e.targetIndex, targetOptions, dialogSwitchStyle(t, targetSegWidth), func(i int) {
					e.err = ""
					if i == len(service.CustomRuleTargets) && e.customTarget == "" && len(d.view.Groups) > 0 {
						e.customTarget = d.view.Groups[0].Name
					}
				})
			})
			if hasGroups && e.targetIndex == len(service.CustomRuleTargets) {
				if e.customTarget == "" {
					e.customTarget = d.view.Groups[0].Name
				}
				d.ruleGroupPicker(c, t)
			}
			if e.err != "" {
				ui.Text(c, e.err).FontSize(fontSecondary).TextColor(t.Danger)
			}
		})
		ui.Row(c).Gap(gapSmall).Justify(ui.End).Children(func() {
			button(c, buttonSpec{label: locales.Cancel(), style: softButton(t), disabled: r.busy}, func() { r.editorOpen = false })
			button(c, buttonSpec{label: locales.Add(), style: primaryButton(t), busy: r.busy,
				disabled: r.busy || strings.TrimSpace(e.value) == ""}, d.submitRuleEditor)
		})
	})
}

// ruleGroupPicker renders an inline scrollable 2-column grid of the active
// profile's policy groups directly inside the add-rule dialog, matching Hop's
// native group list instead of overflowing with a system popup menu.
func (d *Dashboard) ruleGroupPicker(c *ui.Context, t *ui.Theme) {
	groups := d.view.Groups
	ui.Box(c.Key("rule-group-tray")).Radius(radiusControl).Background(dialogField(t)).
		Border(hairlineWidth, t.Border).Padding(ruleGroupTrayPad).Children(func() {
		ui.Scroll(c.Key("rule-group-scroll")).MaxHeight(ruleGroupPickerHeight).Children(func() {
			ui.Column(c).Gap(gapTight).Disabled(d.rules.busy).Children(func() {
				for i := 0; i < len(groups); i += 2 {
					ui.Row(c.Key(i)).Gap(gapTight).Children(func() {
						d.ruleGroupTile(c, t, groups[i])
						if i+1 < len(groups) {
							d.ruleGroupTile(c, t, groups[i+1])
						} else {
							ui.Box(c).Grow(1).Basis(0)
						}
					})
				}
			})
		})
	})
}

// ruleGroupTile is one selectable policy group card inside ruleGroupPicker.
func (d *Dashboard) ruleGroupTile(c *ui.Context, t *ui.Theme, group model.GroupView) {
	e := &d.rules.editor
	name := group.Name
	selected := name == e.customTarget
	row := ui.Row(c.Key("rg:"+name)).Grow(1).Basis(0).MinWidth(0).
		Padding(ruleGroupTilePadY, ruleGroupTilePadX).Gap(gapSmall).
		Radius(radiusControl).AlignItems(ui.Center).Cursor(ui.CursorPointer)
	fill := ui.Transparent
	border := ui.Transparent
	switch {
	case selected:
		fill = t.Accent.Alpha(0.13)
		border = t.Accent.Alpha(0.28)
	case row.Hovered():
		fill = t.Text.Alpha(0.05)
	}
	row.Background(row.AnimateColor("fill", fill, ui.Smooth)).Border(hairlineWidth, border)
	row.OnClick(func() {
		e.customTarget = name
		e.err = ""
	})
	row.Children(func() {
		groupIcon(c, t, group, ruleGroupIconSize)
		ui.Column(c).Grow(1).Basis(0).MinWidth(0).Gap(gapLine).Children(func() {
			ink, weight := t.Text, weightMedium
			if selected {
				ink, weight = accentInk(t), weightSemibold
			}
			ui.Text(c, groupTitle(group)).FontSize(fontSecondary).FontWeight(weight).TextColor(ink).MaxLines(1)
			if dest := strings.TrimPrefix(groupDestination(group), "→ "); dest != "" {
				captionText(c, t, dest).MaxLines(1)
			}
		})
		if selected {
			ui.Icon(c.Key("check"), iconCheck).Width(iconInline).Height(iconInline).TextColor(accentInk(t))
		}
	})
}

// ruleSuggestionRow lets the user switch among the rules offered for the
// connection; the one in the fields shows as chosen.
func (d *Dashboard) ruleSuggestionRow(c *ui.Context, t *ui.Theme) {
	r := &d.rules
	e := &r.editor
	if len(e.suggestions) == 0 {
		return
	}
	fieldLabel(c, t, locales.RuleFromConnection(), "")
	ui.Row(c).Gap(gapSmall).Wrap().Children(func() {
		for _, suggestion := range e.suggestions {
			style := softButton(t)
			if suggestion.typeIndex == e.typeIndex && suggestion.value == strings.TrimSpace(e.value) {
				style = primaryButton(t)
			}
			button(c, buttonSpec{label: suggestion.label, style: style, disabled: r.busy}, func() {
				e.typeIndex, e.value, e.err = suggestion.typeIndex, suggestion.value, ""
			})
		}
	})
}

// ruleTypeLabel names a stored rule's type.
func ruleTypeLabel(typ string) string {
	if i := slices.Index(service.CustomRuleTypes, typ); i >= 0 {
		return ruleTypeLabels()[i]
	}
	return typ
}

// ruleTargetLabel names where a rule sends traffic.
func ruleTargetLabel(target string) string {
	if i := slices.Index(service.CustomRuleTargets, target); i >= 0 {
		return ruleTargetLabels()[i]
	}
	return target
}

// ruleTargetColor tints a target chip: direct quiet, reject in the danger
// colour, and proxy or any named policy group in the accent colour.
func ruleTargetColor(t *ui.Theme, target string) ui.Color {
	switch target {
	case model.CustomRuleDirect:
		return t.TextMuted
	case model.CustomRuleReject:
		return t.Danger
	default:
		return accentInk(t)
	}
}
