package app

import "context"

type Window interface {
	Show()
	Hide()
	Focus()
	Navigate(string) error
}

type DialogChoice string

const (
	ChoiceTray   DialogChoice = "tray"
	ChoiceQuit   DialogChoice = "quit"
	ChoiceCancel DialogChoice = "cancel"
)

type Dialogs interface {
	AskClose(context.Context) (DialogChoice, error)
	ShowStartupError(context.Context, StartupPresentation) (RecoveryChoice, error)
	SelectConfig(context.Context) (string, bool, error)
}

type Tray interface {
	SetStatus(string)
	Destroy()
}

type System interface {
	CopyText(string) error
	OpenBrowser(string) error
	OpenDirectory(string) error
}

type Application interface {
	Quit()
}
