package app

type RecoveryKind string

const (
	RecoveryInvalidConfig RecoveryKind = "invalid_config"
	RecoveryPortConflict  RecoveryKind = "port_conflict"
	RecoveryDatabase      RecoveryKind = "database"
	RecoveryWebView       RecoveryKind = "webview"
)

type RecoveryChoice string

const (
	RecoveryOpenExisting RecoveryChoice = "open_existing"
	RecoveryOpenData     RecoveryChoice = "open_data"
	RecoveryExit         RecoveryChoice = "exit"
)

type StartupPresentation struct {
	Kind    RecoveryKind
	Title   string
	Summary string
	Address string
}
