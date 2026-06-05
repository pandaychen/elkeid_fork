package main

import (
	"log"
	"os"
	"runtime"
	"sync"
	"time"

	"my-security-agent/internal/collector"
	"my-security-agent/internal/config"
	"my-security-agent/internal/heartbeat"
	"my-security-agent/pkg/cgroupctl"
	"my-security-agent/pkg/guard"
	"my-security-agent/pkg/sighandler"
)

var Version = "dev"

func init() {
	runtime.GOMAXPROCS(4)
}

func main() {
	cfg := config.Load()

	// single instance lock (sysvinit only)
	g := guard.New(guard.ServiceConfig{
		Name:       cfg.ServiceName,
		ExecPath:   cfg.ExecPath,
		WorkingDir: cfg.WorkDir,
	})
	if err := g.EnsureSingleInstance(cfg.PidFile); err != nil {
		log.Fatalf("[AGENT] single instance check: %v", err)
	}
	defer g.ReleaseLock()

	// signal handler
	ctx, _ := sighandler.Setup(sighandler.Config{
		ShutdownDelay:      5 * time.Second,
		OnShutdown:         func() { log.Println("[AGENT] SIGTERM received, shutting down...") },
		EnableDynamicPprof: true,
		EnableMemRelease:   true,
	})

	log.Printf("[AGENT] starting version=%s pid=%d", Version, os.Getpid())

	// cgroup resource limits (sysvinit mode)
	if os.Getenv("service_type") == "sysvinit" {
		cg, err := cgroupctl.New(cfg.ServiceName, cgroupctl.Limits{
			CPUQuotaPercent: 10,
			MemoryLimitMB:   250,
		})
		if err != nil {
			log.Printf("[AGENT] cgroup init warning: %v", err)
		} else {
			if err := cg.AddProcess(os.Getpid()); err != nil {
				log.Printf("[AGENT] cgroup add pid warning: %v", err)
			}
			defer cg.Destroy()
		}
	}

	wg := &sync.WaitGroup{}

	wg.Add(1)
	go func() {
		defer wg.Done()
		heartbeat.Run(ctx, cfg)
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		collector.Run(ctx, cfg)
	}()

	wg.Wait()
	log.Println("[AGENT] exited")
}
