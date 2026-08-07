package desktopbridge

import "context"

type CloseBehavior string

const (
	CloseAsk  CloseBehavior = "ask"
	CloseTray CloseBehavior = "tray"
	CloseQuit CloseBehavior = "quit"
)

type Snapshot struct {
	Available     bool          `json:"available"`
	Platform      string        `json:"platform"`
	CloseBehavior CloseBehavior `json:"close_behavior"`
	ListenAddress string        `json:"listen_address"` // Deprecated compatibility alias for DataAddress.
	DataAddress   string        `json:"data_address"`
	AdminAddress  string        `json:"admin_address"`
	DataDir       string        `json:"data_dir"`
	LogDir        string        `json:"log_dir"`
	OwnsGateway   bool          `json:"owns_gateway"`
}

type ImportResult struct {
	Imported bool   `json:"imported"`
	Path     string `json:"path,omitempty"`
}

type Controller interface {
	Snapshot() Snapshot
	SetCloseBehavior(CloseBehavior) error
	OpenDataDir() error
	ImportConfig(context.Context) (ImportResult, error)
}
