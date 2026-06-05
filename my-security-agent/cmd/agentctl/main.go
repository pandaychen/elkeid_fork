package main

import (
	"fmt"
	"os"

	"my-security-agent/pkg/guard"
)

const (
	serviceName = "my-security-agent"
	workDir     = "/opt/my-security-agent"
	execPath    = "/opt/my-security-agent/my-security-agent"
	pidFile     = "/var/run/my-security-agent.pid"
)

func newGuard() *guard.Guard {
	return guard.New(guard.ServiceConfig{
		Name:            serviceName,
		Description:     "My Security Agent - Host Intrusion Detection",
		ExecPath:        execPath,
		WorkingDir:      workDir,
		RestartPolicy:   "always",
		RestartDelaySec: 30,
		MemoryMax:       "250M",
		CPUQuota:        "10%",
		WatchdogSec:     120,
		EnvFile:         workDir + "/env",
	})
}

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(1)
	}

	g := newGuard()
	var err error

	switch os.Args[1] {
	case "install":
		err = g.Install()
		if err == nil {
			fmt.Println("[OK] service installed")
		}
	case "start":
		err = g.Start()
		if err == nil {
			fmt.Println("[OK] service started")
		}
	case "stop":
		err = g.Stop()
		if err == nil {
			fmt.Println("[OK] service stopped")
		}
	case "restart":
		err = g.Restart()
		if err == nil {
			fmt.Println("[OK] service restarted")
		}
	case "uninstall":
		err = g.Uninstall()
		if err == nil {
			fmt.Println("[OK] service uninstalled")
		}
	case "check":
		err = g.Check(pidFile)
	case "status":
		t := "systemd"
		if g.Type == guard.GuardSysvinit {
			t = "sysvinit"
		}
		fmt.Printf("Guard type: %s\n", t)
	default:
		usage()
		os.Exit(1)
	}

	if err != nil {
		fmt.Fprintf(os.Stderr, "[ERROR] %v\n", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Println("Usage: my-security-agentctl <command>")
	fmt.Println()
	fmt.Println("Commands:")
	fmt.Println("  install    Install service (systemd or sysvinit)")
	fmt.Println("  start      Start the agent")
	fmt.Println("  stop       Stop the agent")
	fmt.Println("  restart    Restart the agent")
	fmt.Println("  uninstall  Remove service")
	fmt.Println("  check      Health check (called by crontab)")
	fmt.Println("  status     Show guard type")
}
