package collector

import (
	"context"
	"log"

	"my-security-agent/internal/config"
	"my-security-agent/internal/plugin"
)

// Run loads all configured plugins and blocks until the context is cancelled.
func Run(ctx context.Context, cfg *config.Config) {
	log.Println("[COLLECTOR] started")
	defer log.Println("[COLLECTOR] stopped")

	mgr := plugin.NewManager()

	for _, pcfg := range cfg.Plugins {
		if err := mgr.LoadPlugin(ctx, pcfg); err != nil {
			log.Printf("[COLLECTOR] load plugin %s failed: %v", pcfg.Name, err)
		}
	}

	<-ctx.Done()

	log.Println("[COLLECTOR] shutting down plugins...")
	mgr.ShutdownAll()
}
