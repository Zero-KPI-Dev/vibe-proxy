//go:build windows || darwin

package wailsapp

import (
	"context"
	"runtime"

	"github.com/a448582655/vibe-proxy/desktop/app"
	"github.com/wailsapp/wails/v3/pkg/application"
)

const (
	minimiseToTrayButton = "Minimise to tray and keep proxy running"
	exitVibeProxyButton  = "Exit vibe-proxy"
	cancelButton         = "Cancel"
)

type nativeDialogs struct {
	application *application.App
	window      *application.WebviewWindow
}

var _ app.Dialogs = (*nativeDialogs)(nil)

func (d *nativeDialogs) AskClose(ctx context.Context) (app.DialogChoice, error) {
	choice := make(chan app.DialogChoice, 1)
	dialog := d.application.Dialog.Question().
		SetTitle("Close vibe-proxy").
		SetMessage("Choose whether vibe-proxy should keep running in the tray.").
		AttachToWindow(d.window)
	dialog.AddButton(minimiseToTrayButton).OnClick(func() { choice <- app.ChoiceTray })
	dialog.AddButton(exitVibeProxyButton).OnClick(func() { choice <- app.ChoiceQuit })
	dialog.AddButton(cancelButton).SetAsCancel().OnClick(func() { choice <- app.ChoiceCancel })
	// Wails alpha maps Windows question dialogs to the native Yes/No API.
	// Keep the requested labels in the dialog definition and supply the
	// callbacks that its Windows backend can actually receive.
	if runtime.GOOS == "windows" {
		dialog.AddButton("Yes").OnClick(func() { choice <- app.ChoiceTray })
		dialog.AddButton("No").OnClick(func() { choice <- app.ChoiceQuit })
	}
	dialog.Show()
	if runtime.GOOS == "windows" {
		select {
		case result := <-choice:
			return result, nil
		default:
			return app.ChoiceCancel, nil
		}
	}

	select {
	case result := <-choice:
		return result, nil
	case <-ctx.Done():
		return app.ChoiceCancel, ctx.Err()
	}
}

func (d *nativeDialogs) ShowStartupError(ctx context.Context, presentation app.StartupPresentation) (app.RecoveryChoice, error) {
	finished := make(chan struct{}, 1)
	dialog := d.application.Dialog.Error().
		SetTitle(presentation.Title).
		SetMessage(presentation.Summary).
		AttachToWindow(d.window)
	dialog.AddButton(exitVibeProxyButton).SetAsDefault().OnClick(func() { finished <- struct{}{} })
	dialog.Show()
	if runtime.GOOS == "windows" {
		return app.RecoveryExit, nil
	}
	select {
	case <-finished:
		return app.RecoveryExit, nil
	case <-ctx.Done():
		return app.RecoveryExit, ctx.Err()
	}
}

func (d *nativeDialogs) SelectConfig(context.Context) (string, bool, error) {
	path, err := d.application.Dialog.OpenFile().
		SetTitle("Import vibe-proxy config.yaml").
		AddFilter("YAML configuration", "*.yaml;*.yml").
		PromptForSingleSelection()
	if err != nil {
		return "", false, err
	}
	return path, path != "", nil
}
