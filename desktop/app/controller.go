package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"

	"github.com/a448582655/vibe-proxy/internal/config"
	"github.com/a448582655/vibe-proxy/internal/desktopbridge"
)

type desktopController struct {
	host *Host
}

var _ desktopbridge.Controller = (*desktopController)(nil)

func (c *desktopController) Snapshot() desktopbridge.Snapshot {
	c.host.reconcileGatewayDone()

	c.host.closeMu.Lock()
	closeBehavior := c.host.preferences.CloseBehavior
	c.host.closeMu.Unlock()

	c.host.lifecycleMu.RLock()
	gateway := c.host.gateway
	ownsGateway := c.host.ownsGateway
	c.host.lifecycleMu.RUnlock()

	var dataAddress, adminAddress string
	if gateway != nil {
		dataAddress = gateway.Address()
		adminAddress = gateway.AdminAddress()
	}
	return desktopbridge.Snapshot{
		Available:     true,
		Platform:      runtime.GOOS,
		CloseBehavior: closeBehavior,
		ListenAddress: dataAddress,
		DataAddress:   dataAddress,
		AdminAddress:  adminAddress,
		DataDir:       c.host.paths.DataDir,
		LogDir:        c.host.paths.LogDir,
		OwnsGateway:   ownsGateway,
	}
}

func (c *desktopController) SetCloseBehavior(behavior desktopbridge.CloseBehavior) error {
	if !validCloseBehavior(behavior) {
		return fmt.Errorf("unsupported close behavior %q", behavior)
	}
	return c.host.setCloseBehavior(behavior)
}

func (c *desktopController) OpenDataDir() error {
	return c.host.system.OpenDirectory(c.host.paths.DataDir)
}

func (c *desktopController) ImportConfig(ctx context.Context) (desktopbridge.ImportResult, error) {
	sourcePath, selected, err := c.host.dialogs.SelectConfig(ctx)
	if err != nil {
		return desktopbridge.ImportResult{}, fmt.Errorf("select config: %w", err)
	}
	if !selected {
		return desktopbridge.ImportResult{Imported: false}, nil
	}
	if sourcePath == "" {
		return desktopbridge.ImportResult{}, errors.New("selected config path is empty")
	}
	if err := importConfigFile(sourcePath, c.host.paths.ConfigPath); err != nil {
		return desktopbridge.ImportResult{}, err
	}
	return desktopbridge.ImportResult{Imported: true, Path: sourcePath}, nil
}

func importConfigFile(sourcePath, targetPath string) error {
	source, err := os.Open(sourcePath)
	if err != nil {
		return fmt.Errorf("open selected config: %w", err)
	}
	defer source.Close()

	parent := filepath.Dir(targetPath)
	if err := os.MkdirAll(parent, 0o700); err != nil {
		return fmt.Errorf("create config directory: %w", err)
	}
	temporary, err := os.CreateTemp(parent, filepath.Base(targetPath)+".tmp-*")
	if err != nil {
		return fmt.Errorf("create temporary config: %w", err)
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0o600); err != nil {
		temporary.Close()
		return fmt.Errorf("set temporary config permissions: %w", err)
	}
	if _, err := io.Copy(temporary, source); err != nil {
		temporary.Close()
		return fmt.Errorf("copy selected config: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return fmt.Errorf("sync temporary config: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close temporary config: %w", err)
	}

	cfg, err := config.LoadRuntime(temporaryPath)
	if err != nil {
		return errors.New("selected config is invalid")
	}
	if issues := config.ValidateRuntime(cfg); config.HasErrors(issues) {
		return errors.New("selected config is invalid")
	}
	if err := replaceFile(temporaryPath, targetPath); err != nil {
		return fmt.Errorf("replace config: %w", err)
	}
	return nil
}
