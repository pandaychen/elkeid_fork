package sighandler

import (
	"context"
	"net"
	"net/http"
	_ "net/http/pprof"
	"os"
	"os/signal"
	"runtime/debug"
	"sync"
	"syscall"
	"time"
)

// Config controls which signals are handled and how.
type Config struct {
	ShutdownDelay      time.Duration // sleep before cancelling context on SIGTERM
	OnShutdown         func()        // called immediately when SIGTERM arrives
	EnableDynamicPprof bool          // SIGUSR1 toggles a pprof HTTP server
	EnableMemRelease   bool          // SIGUSR2 calls debug.FreeOSMemory()
}

// Handler holds the running state of the signal processor.
type Handler struct {
	cfg      Config
	cancel   context.CancelFunc
	listener net.Listener
	mu       sync.Mutex
}

// Setup registers signal handlers and returns a context that is cancelled
// on SIGTERM (after the configured delay).
func Setup(cfg Config) (context.Context, *Handler) {
	ctx, cancel := context.WithCancel(context.Background())
	h := &Handler{cfg: cfg, cancel: cancel}

	sigs := make(chan os.Signal, 1)
	sigList := []os.Signal{syscall.SIGTERM}
	if cfg.EnableDynamicPprof {
		sigList = append(sigList, syscall.SIGUSR1)
	}
	if cfg.EnableMemRelease {
		sigList = append(sigList, syscall.SIGUSR2)
	}
	signal.Notify(sigs, sigList...)

	go func() {
		for sig := range sigs {
			switch sig {
			case syscall.SIGTERM:
				if cfg.OnShutdown != nil {
					cfg.OnShutdown()
				}
				if cfg.ShutdownDelay > 0 {
					time.Sleep(cfg.ShutdownDelay)
				}
				cancel()
				return
			case syscall.SIGUSR1:
				h.togglePprof()
			case syscall.SIGUSR2:
				debug.FreeOSMemory()
			}
		}
	}()

	return ctx, h
}

func (h *Handler) togglePprof() {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.listener == nil {
		l, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			return
		}
		h.listener = l
		go http.Serve(l, nil)
	} else {
		h.listener.Close()
		h.listener = nil
	}
}

// PprofAddr returns the pprof listen address, or "" if not active.
func (h *Handler) PprofAddr() string {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.listener != nil {
		return h.listener.Addr().String()
	}
	return ""
}
