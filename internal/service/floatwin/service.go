// Package floatwin manages the optional always-on-top floating traffic ball.
//
// The ball lives in its own frameless, transparent, always-on-top WebView window
// that renders the "?float=1" frontend view. The persisted setting decides
// whether it is shown; the window itself is created lazily so machines that
// never enable the feature pay no extra WebView cost.
package floatwin

import (
	"errors"
	"sync"

	"github.com/wailsapp/wails/v3/pkg/application"

	"github.com/MardukPrime/Fast-V/internal/model"
	"github.com/MardukPrime/Fast-V/internal/store"
)

const (
	// ballSize is the logical window size in pixels. The round ball fills it.
	ballSize = 140
	// screenMargin keeps the ball away from the taskbar/screen edge.
	screenMargin = 24
)

// Service is the Wails service exposing the floating ball window controls to
// the frontend (drag, open main window, disable).
type Service struct {
	store *store.Store

	mu     sync.Mutex
	window *application.WebviewWindow
	drag   *dragSession

	// OpenMainHandler is invoked when the ball is clicked; bootstrap wires it to
	// the main window. It is a field instead of a method so Wails does not expose
	// a function-typed binding to the frontend.
	OpenMainHandler func()
}

// dragSession records the pointer/window offset captured when a drag starts.
type dragSession struct {
	grabX int
	grabY int
	winX  int
	winY  int
}

// New creates the floating window service. The Wails application is resolved
// lazily through application.Get so the service can be registered before the
// application exists.
func New(st *store.Store) *Service {
	return &Service{store: st}
}

func (s *Service) ServiceName() string {
	return "FloatWindowService"
}

// Sync applies the persisted setting: show the ball when enabled, close it when
// disabled. It is safe to call before app.Run and on every settings save.
func (s *Service) Sync(enabled bool) {
	if enabled {
		// Window creation failures (for example an unsupported session) must not
		// break saving the setting; the ball simply stays hidden.
		_ = s.ensureWindow()
		return
	}
	s.Close()
}

// Show makes sure the ball is visible.
func (s *Service) Show() error {
	return s.ensureWindow()
}

// Close closes the ball window while leaving the persisted setting untouched.
func (s *Service) Close() {
	s.mu.Lock()
	window := s.window
	s.window = nil
	s.mu.Unlock()
	if window != nil {
		window.Close()
	}
}

// DragStart begins a pointer drag. The frontend supplies the current absolute
// cursor position (screenX/screenY) and subsequent DragMove calls move the ball
// by the same delta. Keeping the drag in the service avoids relying on the
// Win32 non-client move loop, which frameless WebView2 windows do not get.
func (s *Service) DragStart(screenX int, screenY int) {
	s.mu.Lock()
	window := s.window
	s.mu.Unlock()
	if window == nil {
		return
	}
	x, y := window.Position()
	s.mu.Lock()
	s.drag = &dragSession{grabX: screenX, grabY: screenY, winX: x, winY: y}
	s.mu.Unlock()
}

// DragMove repositions the ball following the cursor, clamped to the union of
// all monitor bounds so it can never be dragged completely off-screen.
func (s *Service) DragMove(screenX int, screenY int) {
	s.mu.Lock()
	window := s.window
	drag := s.drag
	s.mu.Unlock()
	if window == nil || drag == nil {
		return
	}
	x, y := dragTarget(drag.winX, drag.winY, drag.grabX, drag.grabY, screenX, screenY)
	bounds := virtualScreenBounds()
	x, y = clampToBounds(x, y, ballSize, ballSize, bounds)
	window.SetPosition(x, y)
}

// DragEnd finishes the drag started by DragStart.
func (s *Service) DragEnd() {
	s.mu.Lock()
	s.drag = nil
	s.mu.Unlock()
}

// virtualScreenBounds returns the union of all monitor bounds (in screen
// coordinates). An empty rect means the platform reported no screens.
func virtualScreenBounds() rect {
	app := application.Get()
	if app == nil {
		return rect{}
	}
	screens := app.Screen.GetAll()
	bounds := make([]rect, 0, len(screens))
	for _, screen := range screens {
		if screen == nil {
			continue
		}
		area := screen.Bounds
		bounds = append(bounds, rect{X: area.X, Y: area.Y, Width: area.Width, Height: area.Height})
	}
	return unionBounds(bounds)
}

// OpenMain brings the main application window to the foreground.
func (s *Service) OpenMain() {
	if fn := s.OpenMainHandler; fn != nil {
		fn()
	}
}

// Disable unchecks the floating window setting, persists it, broadcasts the
// change and closes the ball. It backs the ball's own context menu entry.
func (s *Service) Disable() (model.Settings, error) {
	if s.store == nil {
		return model.Settings{}, errors.New("设置不可用")
	}
	settings := s.store.Settings()
	settings.FloatWindowEnabled = false
	if err := s.store.SaveSettings(settings); err != nil {
		return model.Settings{}, err
	}
	if app := application.Get(); app != nil {
		app.Event.Emit("settings:changed", settings)
	}
	s.Close()
	return settings, nil
}

func (s *Service) ensureWindow() error {
	app := application.Get()
	if app == nil {
		return errors.New("应用未启动")
	}

	s.mu.Lock()
	if s.window != nil {
		window := s.window
		s.mu.Unlock()
		window.Show()
		return nil
	}
	s.mu.Unlock()

	window := app.Window.NewWithOptions(application.WebviewWindowOptions{
		Title:            "FastV 流量悬浮球",
		Width:            ballSize,
		Height:           ballSize,
		MinWidth:         ballSize,
		MinHeight:        ballSize,
		MaxWidth:         ballSize,
		MaxHeight:        ballSize,
		Frameless:        true,
		DisableResize:    true,
		AlwaysOnTop:      true,
		BackgroundType:   application.BackgroundTypeTransparent,
		BackgroundColour: application.NewRGBA(0, 0, 0, 0),
		URL:              "/?float=1",
		Windows: application.WindowsWindow{
			HiddenOnTaskbar:                   true,
			DisableFramelessWindowDecorations: true,
		},
	})

	x, y := 0, 0
	if screen := app.Screen.GetPrimary(); screen != nil {
		area := screen.WorkArea
		x = area.X + area.Width - ballSize - screenMargin
		y = area.Y + area.Height - ballSize - screenMargin
	} else {
		window.Center()
	}
	if x > 0 || y > 0 {
		window.SetPosition(x, y)
	}

	window.Show()

	s.mu.Lock()
	s.window = window
	s.mu.Unlock()
	return nil
}
