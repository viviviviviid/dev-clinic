//go:build clinicdesktop && darwin

package main

import (
	"context"
	"fmt"
	"io/fs"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/coding-tutor/frontend"
	"github.com/coding-tutor/internal/ai"
	"github.com/coding-tutor/internal/config"
	"github.com/coding-tutor/internal/desktop"
	"github.com/coding-tutor/internal/watcher"
	"github.com/gin-gonic/gin"
	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/menu"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/mac"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

type DesktopBoot struct {
	SupabaseURL     string `json:"supabaseUrl"`
	SupabaseAnonKey string `json:"supabaseAnonKey"`
	LocalURL        string `json:"localUrl"`
	BaseDir         string `json:"baseDir"`
}

// Desktop is the entire native bridge. File/code operations still require the
// authenticated clinic API; no arbitrary filesystem or shell binding is exposed.
type Desktop struct {
	mu                sync.Mutex
	ctx               context.Context
	cancel            context.CancelFunc
	server            *http.Server
	boot              DesktopBoot
	auth              desktop.Auth
	support           string
	setupOnce         sync.Once
	handlers          sync.WaitGroup
	closeHandlerReady bool
	closing           bool
	closeAllowed      bool
}

func main() {
	app := &Desktop{}
	assets, err := fs.Sub(frontend.Assets, "dist-desktop")
	if err != nil {
		log.Fatal(err)
	}
	appMenu := menu.NewMenu()
	appMenu.Append(menu.AppMenu())
	appMenu.Append(menu.EditMenu())
	if err := wails.Run(&options.App{
		Title: "코딩 재활센터", Width: 1440, Height: 940, MinWidth: 900, MinHeight: 640,
		BackgroundColour: options.NewRGB(245, 243, 238),
		AssetServer:      &assetserver.Options{Assets: assets},
		Menu:             appMenu,
		OnStartup:        func(ctx context.Context) { app.ctx, app.cancel = context.WithCancel(ctx) },
		OnShutdown:       app.shutdown,
		OnBeforeClose:    app.beforeClose,
		Bind:             []interface{}{app},
		SingleInstanceLock: &options.SingleInstanceLock{
			UniqueId: "f3d0495f-8d0a-48a0-88f0-6eb48bfd831c",
			OnSecondInstanceLaunch: func(options.SecondInstanceData) {
				runtime.WindowShow(app.ctx)
				runtime.WindowUnminimise(app.ctx)
			},
		},
		Mac: &mac.Options{
			TitleBar: mac.TitleBarDefault(),
			About:    &mac.AboutInfo{Title: "코딩 재활센터", Message: "개인용 AI 코딩 튜터"},
			OnUrlOpen: func(raw string) {
				if app.auth.HandleURL(raw) {
					runtime.WindowShow(app.ctx)
					runtime.WindowUnminimise(app.ctx)
				}
			},
		},
	}); err != nil {
		log.Fatal(err)
	}
}

func (d *Desktop) Boot() (result DesktopBoot, err error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.server != nil {
		return d.boot, nil
	}
	// Existing AI initialization reports missing local tools with a panic. Keep
	// the native window alive so the user sees the error and can retry.
	defer func() {
		if failure := recover(); failure != nil {
			err = fmt.Errorf("시작 준비를 확인해 주세요: %v", failure)
		}
	}()
	d.setupOnce.Do(func() { desktop.RestorePath(d.ctx) })
	home, err := os.UserHomeDir()
	if err != nil {
		return result, err
	}
	d.support = filepath.Join(home, "Library", "Application Support", "Coding Tutor")
	if err := os.MkdirAll(d.support, 0700); err != nil {
		return result, err
	}
	config.Load(filepath.Join(d.support, "config.toml"))
	if err := config.LoadPublic(d.ctx); err != nil {
		return result, err
	}
	if config.Global.BaseDir == "" {
		config.Global.BaseDir = filepath.Join(home, "Coding Tutor")
	}
	if strings.HasPrefix(config.Global.BaseDir, "~/") {
		config.Global.BaseDir = filepath.Join(home, config.Global.BaseDir[2:])
	}
	base, err := filepath.Abs(config.Global.BaseDir)
	if err != nil {
		return result, err
	}
	if err := os.MkdirAll(base, 0755); err != nil {
		return result, err
	}
	config.Global.BaseDir = base
	ai.Init()
	// Reserve a private port, so a separately running web clinic cannot collide.
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return result, err
	}
	_, config.Global.Server.Port, _ = net.SplitHostPort(listener.Addr().String())
	origins := os.Getenv("ALLOWED_ORIGINS")
	_ = os.Setenv("ALLOWED_ORIGINS", strings.Trim(origins+",wails://wails", ","))
	gin.SetMode(gin.ReleaseMode)
	server := newClinicServer()
	handler := server.Handler
	server.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		d.handlers.Add(1)
		defer d.handlers.Done()
		handler.ServeHTTP(w, r)
	})
	server.BaseContext = func(net.Listener) context.Context { return d.ctx }
	d.server = server
	d.boot = DesktopBoot{config.Global.Supabase.URL, config.Global.Supabase.AnonKey,
		"http://" + listener.Addr().String(), base}
	go func() {
		if err := server.Serve(listener); err != nil && err != http.ErrServerClosed {
			log.Printf("desktop clinic: %v", err)
		}
	}()
	log.Printf("desktop clinic ready at %s (base_dir=%s)", d.boot.LocalURL, base)
	return d.boot, nil
}

func (d *Desktop) Login(authorizationURL string) (string, error) {
	d.mu.Lock()
	supabaseURL, ready := d.boot.SupabaseURL, d.server != nil
	d.mu.Unlock()
	if !ready {
		return "", fmt.Errorf("앱이 아직 준비되지 않았습니다")
	}
	return d.auth.Login(d.ctx, authorizationURL, supabaseURL, func(raw string) error {
		ctx, cancel := context.WithTimeout(d.ctx, 10*time.Second)
		defer cancel()
		return exec.CommandContext(ctx, "/usr/bin/open", raw).Run()
	})
}

func (d *Desktop) CancelLogin() { d.auth.Cancel() }

func (d *Desktop) EnableCloseHandler() {
	d.mu.Lock()
	d.closeHandlerReady = true
	d.mu.Unlock()
}

func (d *Desktop) FinishClose(saved bool) {
	d.mu.Lock()
	d.closing = false
	d.closeAllowed = saved
	d.mu.Unlock()
	if saved {
		runtime.Quit(d.ctx)
	}
}

func (d *Desktop) beforeClose(ctx context.Context) bool {
	d.mu.Lock()
	if !d.closeHandlerReady || d.closeAllowed {
		d.mu.Unlock()
		return false
	}
	if d.closing {
		d.mu.Unlock()
		return true
	}
	d.closing = true
	d.mu.Unlock()
	runtime.EventsEmit(ctx, "desktop:before-close")
	return true
}

func (d *Desktop) shutdown(context.Context) {
	d.auth.Cancel()
	if d.cancel != nil {
		d.cancel()
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.server != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = d.server.Shutdown(ctx)
		_ = d.server.Close()
		done := make(chan struct{})
		go func() { d.handlers.Wait(); close(done) }()
		select {
		case <-done:
		case <-ctx.Done():
		}
	}
	if watcher.Global != nil {
		watcher.Global.Stop()
	}
}
