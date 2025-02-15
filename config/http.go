package config

import (
	"context"
	"net/http"
	"time"

	. "github.com/logrusorgru/aurora"
	"golang.org/x/sync/errgroup"
	"m7s.live/engine/v4/log"
	"m7s.live/engine/v4/util"
)

var _ HTTPConfig = (*HTTP)(nil)

type Middleware func(string, http.Handler) http.Handler
type HTTP struct {
	ListenAddr    string
	ListenAddrTLS string
	CertFile      string
	KeyFile       string
	CORS          bool //是否自动添加CORS头
	UserName      string
	Password      string
	ReadTimeout   float64
	WriteTimeout  float64
	IdleTimeout   float64
	mux           *http.ServeMux
	middlewares   []Middleware
}
type HTTPConfig interface {
	GetHTTPConfig() *HTTP
	Listen(ctx context.Context) error
	Handle(string, http.Handler)
	AddMiddleware(Middleware)
}

func (config *HTTP) AddMiddleware(middleware Middleware) {
	config.middlewares = append(config.middlewares, middleware)
}

func (config *HTTP) Handle(path string, f http.Handler) {
	if config.mux == nil {
		config.mux = http.NewServeMux()
	}
	if config.CORS {
		f = util.CORS(f)
	}
	if config.UserName != "" && config.Password != "" {
		f = util.BasicAuth(config.UserName, config.Password, f)
	}
	for _, middleware := range config.middlewares {
		f = middleware(path, f)
	}
	config.mux.Handle(path, f)
}

func (config *HTTP) GetHTTPConfig() *HTTP {
	return config
}

// ListenAddrs Listen http and https
func (config *HTTP) Listen(ctx context.Context) error {
	if config.mux == nil {
		return nil
	}
	var g errgroup.Group
	if config.ListenAddrTLS != "" && (config == &Global.HTTP || config.ListenAddrTLS != Global.ListenAddrTLS) {
		g.Go(func() error {
			log.Info("🌐 https listen at ", Blink(config.ListenAddrTLS))
			var server = http.Server{
				Addr:         config.ListenAddrTLS,
				ReadTimeout:  time.Duration(config.ReadTimeout) * time.Second,
				WriteTimeout: time.Duration(config.WriteTimeout) * time.Second,
				IdleTimeout:  time.Duration(config.IdleTimeout) * time.Second,
				Handler:      config.mux,
			}
			return server.ListenAndServeTLS(config.CertFile, config.KeyFile)
		})
	}
	if config.ListenAddr != "" && (config == &Global.HTTP || config.ListenAddr != Global.ListenAddr) {
		g.Go(func() error {
			log.Info("🌐 http listen at ", Blink(config.ListenAddr))
			var server = http.Server{
				Addr:         config.ListenAddr,
				ReadTimeout:  time.Duration(config.ReadTimeout) * time.Second,
				WriteTimeout: time.Duration(config.WriteTimeout) * time.Second,
				IdleTimeout:  time.Duration(config.IdleTimeout) * time.Second,
				Handler:      config.mux,
			}
			return server.ListenAndServe()
		})
	}
	g.Go(func() error {
		<-ctx.Done()
		return ctx.Err()
	})
	return g.Wait()
}
