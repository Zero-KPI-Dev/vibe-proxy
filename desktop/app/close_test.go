package app

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/a448582655/vibe-proxy/internal/desktopbridge"
)

func TestCloseSavedTrayHidesWithoutDialog(t *testing.T) {
	host, fakes := newCloseTestHost(t, desktopbridge.CloseTray)

	host.HandleWindowClose(context.Background())

	if got := fakes.window.hideCount(); got != 1 {
		t.Fatalf("Hide calls = %d, want 1", got)
	}
	if got := fakes.dialogs.askCount(); got != 0 {
		t.Fatalf("AskClose calls = %d, want 0", got)
	}
}

func TestCloseSavedQuitShutsDownBeforeApplicationQuit(t *testing.T) {
	host, fakes := newCloseTestHost(t, desktopbridge.CloseQuit)
	fakes.application.onQuit = func() {
		if got := fakes.tray.destroyCount(); got != 1 {
			t.Errorf("tray Destroy calls before Quit = %d, want 1", got)
		}
	}

	host.HandleWindowClose(context.Background())
	fakes.application.waitForQuit(t)

	if got := fakes.dialogs.askCount(); got != 0 {
		t.Fatalf("AskClose calls = %d, want 0", got)
	}
}

func TestCloseAskTrayPersistsBeforeHiding(t *testing.T) {
	host, fakes := newCloseTestHost(t, desktopbridge.CloseAsk)
	fakes.dialogs.choice = ChoiceTray
	fakes.window.onHide = func() {
		preferences, err := LoadPreferences(fakes.paths.PreferencesPath)
		if err != nil {
			t.Errorf("LoadPreferences() during Hide error = %v", err)
			return
		}
		if preferences.CloseBehavior != desktopbridge.CloseTray {
			t.Errorf("CloseBehavior during Hide = %q, want %q", preferences.CloseBehavior, desktopbridge.CloseTray)
		}
	}

	host.HandleWindowClose(context.Background())

	if got := fakes.window.hideCount(); got != 1 {
		t.Fatalf("Hide calls = %d, want 1", got)
	}
}

func TestCloseAskQuitPersistsBeforeQuitting(t *testing.T) {
	host, fakes := newCloseTestHost(t, desktopbridge.CloseAsk)
	fakes.dialogs.choice = ChoiceQuit
	fakes.application.onQuit = func() {
		preferences, err := LoadPreferences(fakes.paths.PreferencesPath)
		if err != nil {
			t.Errorf("LoadPreferences() during Quit error = %v", err)
			return
		}
		if preferences.CloseBehavior != desktopbridge.CloseQuit {
			t.Errorf("CloseBehavior during Quit = %q, want %q", preferences.CloseBehavior, desktopbridge.CloseQuit)
		}
	}

	host.HandleWindowClose(context.Background())
	fakes.application.waitForQuit(t)
}

func TestCloseAskCancelLeavesWindowVisibleAndPreferenceUnchanged(t *testing.T) {
	host, fakes := newCloseTestHost(t, desktopbridge.CloseAsk)
	fakes.dialogs.choice = ChoiceCancel

	host.HandleWindowClose(context.Background())

	if got := fakes.window.hideCount(); got != 0 {
		t.Fatalf("Hide calls = %d, want 0", got)
	}
	preferences, err := LoadPreferences(fakes.paths.PreferencesPath)
	if err != nil {
		t.Fatalf("LoadPreferences() error = %v", err)
	}
	if preferences.CloseBehavior != desktopbridge.CloseAsk {
		t.Fatalf("CloseBehavior = %q, want %q", preferences.CloseBehavior, desktopbridge.CloseAsk)
	}
}

func TestCloseOnlyOneDialogCanBeActive(t *testing.T) {
	host, fakes := newCloseTestHost(t, desktopbridge.CloseAsk)
	fakes.dialogs.choice = ChoiceCancel
	fakes.dialogs.entered = make(chan struct{})
	fakes.dialogs.release = make(chan struct{})

	firstDone := make(chan struct{})
	go func() {
		host.HandleWindowClose(context.Background())
		close(firstDone)
	}()
	select {
	case <-fakes.dialogs.entered:
	case <-time.After(time.Second):
		t.Fatal("first close dialog did not open")
	}

	secondDone := make(chan struct{})
	go func() {
		host.HandleWindowClose(context.Background())
		close(secondDone)
	}()
	select {
	case <-secondDone:
	case <-time.After(time.Second):
		t.Fatal("duplicate close handler did not return")
	}
	if got := fakes.dialogs.askCount(); got != 1 {
		t.Fatalf("AskClose calls while first dialog active = %d, want 1", got)
	}

	close(fakes.dialogs.release)
	select {
	case <-firstDone:
	case <-time.After(time.Second):
		t.Fatal("first close handler did not return after dialog release")
	}
}

func TestCloseRequestQuitBypassesPreference(t *testing.T) {
	host, fakes := newCloseTestHost(t, desktopbridge.CloseAsk)

	host.RequestQuit(context.Background())
	fakes.application.waitForQuit(t)

	if got := fakes.dialogs.askCount(); got != 0 {
		t.Fatalf("AskClose calls = %d, want 0", got)
	}
}

func TestCloseResetBehaviorPersistsAsk(t *testing.T) {
	host, fakes := newCloseTestHost(t, desktopbridge.CloseTray)

	if err := host.ResetCloseBehavior(); err != nil {
		t.Fatalf("ResetCloseBehavior() error = %v", err)
	}

	preferences, err := LoadPreferences(fakes.paths.PreferencesPath)
	if err != nil {
		t.Fatalf("LoadPreferences() error = %v", err)
	}
	if preferences.CloseBehavior != desktopbridge.CloseAsk {
		t.Fatalf("CloseBehavior = %q, want %q", preferences.CloseBehavior, desktopbridge.CloseAsk)
	}
}

func TestCloseConcurrentShutdownExecutesOnce(t *testing.T) {
	host, fakes := newCloseTestHost(t, desktopbridge.CloseAsk)
	fakes.tray.entered = make(chan struct{})
	fakes.tray.release = make(chan struct{})

	const callers = 16
	start := make(chan struct{})
	results := make(chan error, callers)
	for range callers {
		go func() {
			<-start
			results <- host.Shutdown(context.Background())
		}()
	}
	close(start)

	select {
	case <-fakes.tray.entered:
	case <-time.After(time.Second):
		t.Fatal("shutdown did not reach tray destruction")
	}
	close(fakes.tray.release)

	for range callers {
		select {
		case err := <-results:
			if err != nil {
				t.Fatalf("Shutdown() error = %v", err)
			}
		case <-time.After(time.Second):
			t.Fatal("concurrent Shutdown call did not return")
		}
	}
	if got := fakes.tray.destroyCount(); got != 1 {
		t.Fatalf("Tray.Destroy calls = %d, want 1", got)
	}
}

type closeTestFakes struct {
	paths       Paths
	window      *fakeWindow
	dialogs     *fakeDialogs
	tray        *fakeTray
	system      *fakeSystem
	application *fakeApplication
}

func newCloseTestHost(t *testing.T, behavior desktopbridge.CloseBehavior) (*Host, *closeTestFakes) {
	t.Helper()
	paths := Paths{
		DataDir:         t.TempDir(),
		PreferencesPath: t.TempDir() + "/desktop.json",
	}
	preferences := DefaultPreferences()
	preferences.CloseBehavior = behavior
	if err := SavePreferences(paths.PreferencesPath, preferences); err != nil {
		t.Fatalf("SavePreferences() error = %v", err)
	}
	fakes := &closeTestFakes{
		paths:       paths,
		window:      &fakeWindow{},
		dialogs:     &fakeDialogs{choice: ChoiceCancel},
		tray:        &fakeTray{},
		system:      &fakeSystem{},
		application: newFakeApplication(),
	}
	host, err := NewHost(HostOptions{
		Paths:       paths,
		Preferences: preferences,
		Window:      fakes.window,
		Dialogs:     fakes.dialogs,
		Tray:        fakes.tray,
		System:      fakes.system,
		Application: fakes.application,
	})
	if err != nil {
		t.Fatalf("NewHost() error = %v", err)
	}
	return host, fakes
}

type fakeWindow struct {
	mu          sync.Mutex
	shows       int
	focuses     int
	hides       int
	onHide      func()
	navigations []string
	navigateErr error
	onNavigate  func(string)
}

func (f *fakeWindow) Show() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.shows++
}
func (f *fakeWindow) Focus() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.focuses++
}
func (f *fakeWindow) Hide() {
	f.mu.Lock()
	f.hides++
	onHide := f.onHide
	f.mu.Unlock()
	if onHide != nil {
		onHide()
	}
}
func (f *fakeWindow) Navigate(target string) error {
	f.mu.Lock()
	f.navigations = append(f.navigations, target)
	err, onNavigate := f.navigateErr, f.onNavigate
	f.mu.Unlock()
	if onNavigate != nil {
		onNavigate(target)
	}
	return err
}
func (f *fakeWindow) hideCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.hides
}
func (f *fakeWindow) navigationTargets() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.navigations...)
}
func (f *fakeWindow) activationCounts() (int, int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.shows, f.focuses
}

type fakeDialogs struct {
	mu                    sync.Mutex
	choice                DialogChoice
	err                   error
	asks                  int
	entered               chan struct{}
	release               chan struct{}
	selectedPath          string
	selected              bool
	selectErr             error
	recoveryChoice        RecoveryChoice
	recoveryErr           error
	recoveryPresentations []StartupPresentation
}

func (f *fakeDialogs) AskClose(context.Context) (DialogChoice, error) {
	f.mu.Lock()
	f.asks++
	choice, err := f.choice, f.err
	entered, release := f.entered, f.release
	f.mu.Unlock()
	if entered != nil {
		close(entered)
	}
	if release != nil {
		<-release
	}
	return choice, err
}
func (f *fakeDialogs) ShowStartupError(_ context.Context, presentation StartupPresentation) (RecoveryChoice, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.recoveryPresentations = append(f.recoveryPresentations, presentation)
	choice := f.recoveryChoice
	if choice == "" {
		choice = RecoveryExit
	}
	return choice, f.recoveryErr
}
func (f *fakeDialogs) SelectConfig(context.Context) (string, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.selectedPath, f.selected, f.selectErr
}
func (f *fakeDialogs) askCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.asks
}

type fakeTray struct {
	mu       sync.Mutex
	destroys int
	entered  chan struct{}
	release  chan struct{}
	statuses []string
}

func (f *fakeTray) SetStatus(status string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.statuses = append(f.statuses, status)
}
func (f *fakeTray) Destroy() {
	f.mu.Lock()
	f.destroys++
	entered, release := f.entered, f.release
	f.mu.Unlock()
	if entered != nil {
		close(entered)
	}
	if release != nil {
		<-release
	}
}
func (f *fakeTray) destroyCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.destroys
}
func (f *fakeTray) statusValues() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.statuses...)
}

type fakeSystem struct {
	mu          sync.Mutex
	copied      []string
	browsed     []string
	directories []string
	err         error
	openEntered chan struct{}
	openRelease chan struct{}
}

func (f *fakeSystem) CopyText(value string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.copied = append(f.copied, value)
	return f.err
}
func (f *fakeSystem) OpenBrowser(target string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.browsed = append(f.browsed, target)
	return f.err
}
func (f *fakeSystem) OpenDirectory(path string) error {
	f.mu.Lock()
	f.directories = append(f.directories, path)
	err, entered, release := f.err, f.openEntered, f.openRelease
	f.mu.Unlock()
	if entered != nil {
		close(entered)
	}
	if release != nil {
		<-release
	}
	return err
}

type fakeApplication struct {
	mu     sync.Mutex
	quits  int
	quit   chan struct{}
	onQuit func()
	once   sync.Once
}

func newFakeApplication() *fakeApplication {
	return &fakeApplication{quit: make(chan struct{})}
}

func (f *fakeApplication) Quit() {
	f.mu.Lock()
	f.quits++
	onQuit := f.onQuit
	f.mu.Unlock()
	if onQuit != nil {
		onQuit()
	}
	f.once.Do(func() { close(f.quit) })
}

func (f *fakeApplication) waitForQuit(t *testing.T) {
	t.Helper()
	select {
	case <-f.quit:
	case <-time.After(time.Second):
		t.Fatal("Application.Quit was not called")
	}
}
