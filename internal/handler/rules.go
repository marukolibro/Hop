package handler

import (
	"context"
	"slices"
	"strconv"
	"strings"

	"github.com/egoist/mygo/ui"

	"hop/internal/model"
	"hop/internal/service"
)

// ruleTypeLabels name service.CustomRuleTypes, index for index; ruleTypeHints
// say what each matches, in the same order.
var (
	ruleTypeLabels = []string{"域名后缀", "完整域名", "域名关键词", "IP 段", "应用"}
	ruleTypeHints  = []string{
		"匹配这个域名及其子域名，例如 example.com",
		"只匹配这个域名本身，例如 api.example.com",
		"域名里含有这段文字就匹配，例如 example",
		"匹配这个地址段，连接不会为此解析域名，例如 192.0.2.0/24",
		"按本机进程名匹配，例如 Telegram",
	}
	ruleValuePlaceholders = []string{"example.com", "api.example.com", "example", "192.0.2.0/24", "Telegram"}
)

// ruleTargetLabels name service.CustomRuleTargets, index for index.
var ruleTargetLabels = []string{"直连", "代理", "拒绝"}

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

// ruleEditor is the add dialog's fields.
type ruleEditor struct {
	typeIndex   int
	targetIndex int
	value       string
	err         string
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
		e.err = "请输入要匹配的内容。"
		return
	}
	if e.typeIndex < 0 || e.typeIndex >= len(service.CustomRuleTypes) ||
		e.targetIndex < 0 || e.targetIndex >= len(service.CustomRuleTargets) {
		e.err = "请选择规则类型和去向。"
		return
	}
	e.err = ""
	rule := model.CustomRule{
		Type:   service.CustomRuleTypes[e.typeIndex],
		Value:  value,
		Target: service.CustomRuleTargets[e.targetIndex],
	}
	d.beginRuleSave(append(slices.Clone(r.rules), rule), true)
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
		ui.Row(c).Key("rules-add").Gap(gapLarge).AlignItems(ui.Center).MinHeight(switchHeight).Children(func() {
			note := "规则按顺序匹配，排在订阅规则之前。"
			switch {
			case r.failed:
				note = "读取自定义规则失败。"
			case !r.loaded:
				note = "正在读取自定义规则…"
			case len(r.rules) == 0:
				note = "还没有自定义规则，流量按当前的规则分流。"
			}
			mutedText(c, t, note).Grow(1)
			button(c, buttonSpec{label: "添加规则", icon: iconPlus, style: primaryButton(t),
				disabled: d.controlsLocked() || !r.loaded, busy: r.busy}, d.openRuleEditor)
		})
	})
	return rows
}

// customRuleRow is one stored rule: its type, the value it matches, where it
// sends traffic, and its delete button.
func (d *Dashboard) customRuleRow(c *ui.Context, t *ui.Theme, index int, rule model.CustomRule) func() {
	return func() {
		ui.Row(c).Key("rule:" + strconv.Itoa(index)).Gap(gapLarge).AlignItems(ui.Center).MinHeight(switchHeight).Children(func() {
			ui.Column(c).Grow(1).Basis(0).Gap(gapLine).Children(func() {
				captionText(c, t, ruleTypeLabel(rule.Type))
				ui.Text(c, rule.Value).FontSize(fontBody).FontWeight(weightMedium).MaxLines(1)
			})
			chip(c, t, ruleTargetLabel(rule.Target), ruleTargetColor(t, rule.Target))
			ui.Box(c).Disabled(d.controlsLocked()).Children(func() {
				iconButton(c, "删除规则 "+rule.Value, iconTrash, dangerButton(t), func() { d.deleteRule(index) })
			})
		})
	}
}

// customRuleDialog is the add form over a dimmed window.
func (d *Dashboard) customRuleDialog(c *ui.Context, t *ui.Theme) {
	r := &d.rules
	ui.DialogBase(c, &r.editorOpen, func(backdrop, panel ui.Element) {
		backdrop.Background(dialogScrim)
		panel.Width(dialogWidth).Padding(pagePadding).Gap(gapLarge).Radius(radiusPanel).
			Background(overlaySurface(t)).Border(1, t.Border).Shadow(0, 10, 30, 0, dialogShadow)
		panel.Children(func() { d.customRuleForm(c, t) })
	})
}

func (d *Dashboard) customRuleForm(c *ui.Context, t *ui.Theme) {
	r := &d.rules
	e := &r.editor
	typeIndex := max(0, min(e.typeIndex, len(ruleTypeLabels)-1))
	hint := ruleTypeHints[typeIndex]
	placeholder := ruleValuePlaceholders[typeIndex]
	ui.Column(c).Gap(gapSection).Children(func() {
		ui.Column(c).Gap(gapSmall).Children(func() {
			ui.Row(c).Gap(gapMedium).AlignItems(ui.Center).Children(func() {
				ui.Text(c, "添加规则").FontSize(fontHeadline).FontWeight(weightSemibold).Grow(1)
				iconButton(c, "关闭", iconClose, softButton(t), func() { r.editorOpen = false })
			})
			mutedText(c, t, "规则排在订阅规则之前，先于它们匹配；局域网直连规则仍然最先。")
		})
		ui.Column(c).Gap(gapSmall).Children(func() {
			d.ruleSuggestionRow(c, t)
			fieldLabel(c, t, "类型", "")
			ui.Box(c).Disabled(r.busy).Children(func() {
				slidingSwitch(c, "rule-type", &e.typeIndex, ruleTypeLabels, dialogSwitchStyle(t, 68), func(int) { e.err = "" })
			})
			captionText(c, t, hint)
			fieldLabel(c, t, "内容", "")
			ui.TextInput(c, &e.value).Label("规则内容").Placeholder(placeholder).
				Background(dialogField(t)).Disabled(r.busy).OnSubmit(d.submitRuleEditor)
			fieldLabel(c, t, "去向", "")
			ui.Box(c).Disabled(r.busy).Children(func() {
				slidingSwitch(c, "rule-target", &e.targetIndex, ruleTargetLabels, dialogSwitchStyle(t, 64), func(int) { e.err = "" })
			})
			if e.err != "" {
				ui.Text(c, e.err).FontSize(fontSecondary).TextColor(t.Danger)
			}
		})
		ui.Row(c).Gap(gapSmall).Justify(ui.End).Children(func() {
			button(c, buttonSpec{label: "取消", style: softButton(t), disabled: r.busy}, func() { r.editorOpen = false })
			button(c, buttonSpec{label: "添加", style: primaryButton(t), busy: r.busy,
				disabled: r.busy || strings.TrimSpace(e.value) == ""}, d.submitRuleEditor)
		})
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
	fieldLabel(c, t, "按这条连接", "")
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
		return ruleTypeLabels[i]
	}
	return typ
}

// ruleTargetLabel names where a rule sends traffic.
func ruleTargetLabel(target string) string {
	if i := slices.Index(service.CustomRuleTargets, target); i >= 0 {
		return ruleTargetLabels[i]
	}
	return target
}

// ruleTargetColor tints a target chip: direct quiet, proxy in the accent,
// reject in the danger colour.
func ruleTargetColor(t *ui.Theme, target string) ui.Color {
	switch target {
	case model.CustomRuleProxy:
		return accentInk(t)
	case model.CustomRuleReject:
		return t.Danger
	default:
		return t.TextMuted
	}
}
