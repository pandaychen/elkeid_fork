package heartbeat

import (
	"context"
	"log"
	"os"
	"runtime"
	"time"

	"my-security-agent/internal/config"
	"my-security-agent/pkg/guard"
)

func Run(ctx context.Context, cfg *config.Config) {
	log.Println("[HEARTBEAT] started")
	defer log.Println("[HEARTBEAT] stopped")

	collect()

	ticker := time.NewTicker(time.Duration(cfg.HeartbeatIntervalSec) * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			collect()
			guard.NotifyWatchdog()
		}
	}
}

func collect() {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	log.Printf("[HEARTBEAT] pid=%d goroutines=%d alloc=%dMB sys=%dMB",
		os.Getpid(),
		runtime.NumGoroutine(),
		m.Alloc/1024/1024,
		m.Sys/1024/1024,
	)
}
