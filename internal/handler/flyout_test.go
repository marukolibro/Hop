package handler

import (
	"errors"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"hop/internal/model"
	"hop/internal/service"
	"hop/locales"
)

func TestFlyoutAddPreservesOpenEditor(t *testing.T) {
	d := &Dashboard{settings: settingsState{settings: service.DefaultSettings()}}
	d.openEditor = func(intent editorIntent, id string) { d.openEditorDialog(intent, id) }
	d.openEditorDialog(editorAdd, "")
	main := ui.NewTester(func(c *ui.Context) {
		d.editorDialog(c, c.Theme())
	}, 860, 700)
	main.Advance(time.Second)
	for i := 0; i < 6 && !main.Focused(locales.SubFieldName()); i++ {
		main.Key(0, ui.KeyTab)
	}
	if !main.Focused(locales.SubFieldName()) {
		t.Fatal("could not focus the subscription name input")
	}
	main.Type("Unsaved profile")
	if d.subscription.editor.name != "Unsaved profile" {
		t.Fatal("the name input did not update the draft")
	}
	flyout := ui.NewTester(func(c *ui.Context) {
		ui.Column(c).Fill().Children(func() {
			d.quickRoutes(c, c.Theme(), func() {})
		})
	}, flyoutWidth, flyoutHeight)
	if err := flyout.Click(locales.SubAdd()); err != nil {
		t.Fatal(err)
	}
	main.Frame()
	if !d.subscription.editorOpen || d.subscription.editor.name != "Unsaved profile" {
		t.Fatal("the flyout discarded the open subscription draft")
	}
}

func TestFlyoutShowsAndDismissesFailure(t *testing.T) {
	d := &Dashboard{
		status:   statusConnected,
		settings: settingsState{settings: service.DefaultSettings()},
		subscription: subscriptionsState{loaded: true, view: model.SubscriptionsView{
			ActiveID: "current", Items: []model.SubscriptionSummary{{ID: "current", Name: "Current profile"}},
		}},
	}
	cause := errors.New("the core rejected the subscription reload")
	d.fail(actionUseSubscription, cause)
	flyout := ui.NewTester(func(c *ui.Context) {
		d.flyoutView(c, func() {}, func() {}, func(int) {})
	}, flyoutWidth, flyoutHeight)
	flyout.Advance(time.Second)
	if !flyout.HasText(cause.Error()) {
		t.Fatal("the subscription switch error is missing from the flyout")
	}
	if err := flyout.Click(locales.DismissNotice()); err != nil {
		t.Fatal(err)
	}
	if flyout.HasText(cause.Error()) {
		t.Fatal("the dismissed error remains in the flyout")
	}
}
