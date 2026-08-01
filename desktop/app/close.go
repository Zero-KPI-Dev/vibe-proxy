package app

import (
	"context"
	"errors"
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
	h.lifecycleMu.Lock()
	h.reconcileGatewayDoneLocked()
	if h.shutdownComplete {
		h.lifecycleMu.Unlock()
		return nil
	}
	if !h.shutdownInProgress {
		attempt := &shutdownAttempt{done: make(chan struct{})}
		h.shutdownRequested = true
		h.shutdownInProgress = true
		h.shutdownAttempt = attempt
		h.shutdownDone = attempt.done
		h.shutdownErr = nil
		go h.executeShutdown(attempt)
	}
	attempt := h.shutdownAttempt
	h.lifecycleMu.Unlock()

	select {
	case <-attempt.done:
		return attempt.err
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (h *Host) executeShutdown(attempt *shutdownAttempt) {
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

	h.lifecycleMu.RLock()
	gateway := h.gateway
	sessions := h.sessions
	startupCleanupErr := h.startupCleanupErr
	startupCleanupGeneration := attempt.startupCleanupGeneration
	if attempt.startupCleanupErr != nil {
		if startupCleanupErr == nil {
			startupCleanupErr = attempt.startupCleanupErr
		} else {
			startupCleanupErr = errors.Join(startupCleanupErr, attempt.startupCleanupErr)
		}
	}
	h.lifecycleMu.RUnlock()

	if startupCleanupErr != nil {
		if shutdownErr == nil {
			shutdownErr = startupCleanupErr
		} else {
			shutdownErr = errors.Join(shutdownErr, startupCleanupErr)
		}
	}
	if shutdownErr == nil {
		if gateway != nil {
			shutdownErr = gateway.Shutdown(ctx)
		}
	}
	if sessions != nil {
		sessions.RevokeAll()
	}
	if shutdownErr == nil {
		h.lifecycleMu.Lock()
		h.clearGatewayLocked(gateway)
		h.lifecycleMu.Unlock()
		h.tray.Destroy()
	} else {
		h.reconcileGatewayDone()
		h.tray.SetStatus("Error: " + shutdownErr.Error())
	}

	h.lifecycleMu.Lock()
	if attempt.startupCleanupGeneration != startupCleanupGeneration {
		if shutdownErr == nil {
			shutdownErr = attempt.startupCleanupErr
		} else {
			shutdownErr = errors.Join(shutdownErr, attempt.startupCleanupErr)
		}
		h.tray.SetStatus("Error: " + shutdownErr.Error())
	}
	attempt.err = shutdownErr
	if h.shutdownAttempt == attempt {
		h.shutdownErr = shutdownErr
		h.shutdownInProgress = false
		if shutdownErr == nil {
			h.shutdownComplete = true
		}
	}
	close(attempt.done)
	h.lifecycleMu.Unlock()
}

func (h *Host) quitAfterShutdown() {
	go func() {
		if err := h.Shutdown(context.Background()); err == nil {
			h.quitOnce.Do(func() {
				h.application.Quit()
			})
		}
	}()
}
