package app

import (
	"context"
	"time"

	"github.com/a448582655/vibe-proxy/internal/desktopbridge"
)

const shutdownTimeout = 10 * time.Second

func (h *Host) HandleWindowClose(ctx context.Context) {
	h.closeMu.Lock()
	if h.quitting {
		h.closeMu.Unlock()
		return
	}

	switch h.preferences.CloseBehavior {
	case desktopbridge.CloseTray:
		h.closeMu.Unlock()
		h.window.Hide()
		return
	case desktopbridge.CloseQuit:
		h.quitting = true
		h.closeMu.Unlock()
		h.quitAfterShutdown()
		return
	case desktopbridge.CloseAsk:
		if h.dialogOpen {
			h.closeMu.Unlock()
			return
		}
		h.dialogOpen = true
	default:
		h.closeMu.Unlock()
		return
	}
	h.closeMu.Unlock()

	choice, err := h.dialogs.AskClose(ctx)

	h.closeMu.Lock()
	h.dialogOpen = false
	if err != nil || h.quitting {
		h.closeMu.Unlock()
		return
	}

	switch choice {
	case ChoiceTray:
		if err := h.saveCloseBehaviorLocked(desktopbridge.CloseTray); err != nil {
			h.closeMu.Unlock()
			return
		}
		h.closeMu.Unlock()
		h.window.Hide()
	case ChoiceQuit:
		if err := h.saveCloseBehaviorLocked(desktopbridge.CloseQuit); err != nil {
			h.closeMu.Unlock()
			return
		}
		h.quitting = true
		h.closeMu.Unlock()
		h.quitAfterShutdown()
	default:
		h.closeMu.Unlock()
	}
}

func (h *Host) RequestQuit(context.Context) {
	h.closeMu.Lock()
	if h.quitting {
		h.closeMu.Unlock()
		return
	}
	h.quitting = true
	h.closeMu.Unlock()
	h.quitAfterShutdown()
}

func (h *Host) ResetCloseBehavior() error {
	return h.setCloseBehavior(desktopbridge.CloseAsk)
}

func (h *Host) setCloseBehavior(behavior desktopbridge.CloseBehavior) error {
	h.closeMu.Lock()
	defer h.closeMu.Unlock()
	return h.saveCloseBehaviorLocked(behavior)
}

func (h *Host) saveCloseBehaviorLocked(behavior desktopbridge.CloseBehavior) error {
	next := h.preferences
	next.CloseBehavior = behavior
	if err := SavePreferences(h.paths.PreferencesPath, next); err != nil {
		return err
	}
	h.preferences = normalizeSavedPreferences(next)
	return nil
}

func (h *Host) Shutdown(ctx context.Context) error {
	h.shutdownOnce.Do(func() {
		h.lifecycleMu.Lock()
		h.shutdownRequested = true
		h.lifecycleMu.Unlock()
		go h.executeShutdown()
	})

	select {
	case <-h.shutdownDone:
		return h.shutdownErr
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (h *Host) executeShutdown() {
	ctx, cancel := h.newShutdownContext()
	defer cancel()
	var shutdownErr error

	h.lifecycleMu.RLock()
	startupDone := h.startupDone
	h.lifecycleMu.RUnlock()
	if startupDone != nil {
		select {
		case <-startupDone:
		case <-ctx.Done():
			shutdownErr = ctx.Err()
		}
	}

	if shutdownErr == nil {
		h.lifecycleMu.RLock()
		gateway := h.gateway
		sessions := h.sessions
		h.lifecycleMu.RUnlock()

		if gateway != nil {
			shutdownErr = gateway.Shutdown(ctx)
		}
		if sessions != nil {
			sessions.RevokeAll()
		}
		if shutdownErr == nil {
			h.lifecycleMu.Lock()
			if h.gateway == gateway {
				h.gateway = nil
				h.sessions = nil
				h.ownsGateway = false
			}
			h.lifecycleMu.Unlock()
			h.tray.Destroy()
		}
	}

	h.lifecycleMu.Lock()
	h.shutdownErr = shutdownErr
	h.lifecycleMu.Unlock()
	close(h.shutdownDone)
}

func (h *Host) quitAfterShutdown() {
	h.quitOnce.Do(func() {
		go func() {
			if err := h.Shutdown(context.Background()); err == nil {
				h.application.Quit()
			}
		}()
	})
}
