package guard

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"text/template"
	"time"
)

// ServiceConfig defines how the service should be managed.
type ServiceConfig struct {
	Name            string // service name, e.g. "my-security-agent"
	Description     string
	ExecPath        string // absolute path to the executable
	WorkingDir      string
	RestartPolicy   string // "always" | "on-failure" | "no"
	RestartDelaySec int
	MemoryMax       string // e.g. "250M"
	CPUQuota        string // e.g. "10%"
	WatchdogSec     int    // 0 = disabled
	EnvFile         string
	DelegateSubTree bool
}

// GuardType selects the init system backend.
type GuardType int

const (
	GuardSystemd  GuardType = iota
	GuardSysvinit
)

// Guard manages the service lifecycle.
type Guard struct {
	Config ServiceConfig
	Type   GuardType

	pidPath   string
	hasLock   bool
	lockOwner *os.Process
}

// New auto-detects systemd vs sysvinit.
func New(cfg ServiceConfig) *Guard {
	g := &Guard{Config: cfg}
	if _, err := exec.LookPath("systemctl"); err == nil {
		g.Type = GuardSystemd
	} else {
		g.Type = GuardSysvinit
	}
	return g
}

// NewWithType forces a specific init backend.
func NewWithType(cfg ServiceConfig, t GuardType) *Guard {
	return &Guard{Config: cfg, Type: t}
}

// ---------- public API ----------

func (g *Guard) Install() error {
	switch g.Type {
	case GuardSystemd:
		return g.installSystemd()
	case GuardSysvinit:
		return g.installSysvinit()
	}
	return fmt.Errorf("unsupported guard type")
}

func (g *Guard) Start() error {
	switch g.Type {
	case GuardSystemd:
		return run("systemctl", "start", g.Config.Name)
	case GuardSysvinit:
		return g.sysvinitStart()
	}
	return nil
}

func (g *Guard) Stop() error {
	switch g.Type {
	case GuardSystemd:
		return run("systemctl", "stop", g.Config.Name)
	case GuardSysvinit:
		return g.sysvinitStop()
	}
	return nil
}

func (g *Guard) Restart() error {
	switch g.Type {
	case GuardSystemd:
		return run("systemctl", "restart", g.Config.Name)
	case GuardSysvinit:
		_ = g.sysvinitStop()
		return g.sysvinitStart()
	}
	return nil
}

func (g *Guard) Uninstall() error {
	_ = g.Stop()
	switch g.Type {
	case GuardSystemd:
		_ = run("systemctl", "disable", g.Config.Name)
		svcPath := filepath.Join(filepath.Dir(g.Config.ExecPath), g.Config.Name+".service")
		os.Remove(svcPath)
		_ = run("systemctl", "daemon-reload")
	case GuardSysvinit:
		g.removeCrontab()
		os.Remove(filepath.Join("/etc/init.d/", g.Config.Name))
	}
	return nil
}

// Check is called periodically by crontab in sysvinit mode.
// It restarts the agent if the pid file is stale.
func (g *Guard) Check(pidFile string) error {
	if g.Type != GuardSysvinit {
		return nil
	}
	if !pidFileAlive(pidFile) {
		return g.sysvinitStart()
	}
	return nil
}

// EnsureSingleInstance prevents duplicate agent processes (sysvinit mode).
// Call this at the very start of the agent main().
func (g *Guard) EnsureSingleInstance(pidFile string) error {
	if g.Type != GuardSysvinit {
		return nil
	}
	if pidFileAlive(pidFile) {
		return fmt.Errorf("another instance is running (pidfile %s)", pidFile)
	}
	g.pidPath = pidFile
	return os.WriteFile(pidFile, []byte(fmt.Sprintf("%d\n", os.Getpid())), 0644)
}

// ReleaseLock removes the pid file on shutdown.
func (g *Guard) ReleaseLock() {
	if g.pidPath != "" {
		os.Remove(g.pidPath)
	}
}

// ---------- systemd ----------

const systemdTmpl = `[Unit]
Description={{.Description}}
Wants=network-online.target
After=network-online.target network.target syslog.target

[Service]
Type=simple
ExecStart={{.ExecPath}}
WorkingDirectory={{.WorkingDir}}
Restart={{.RestartPolicy}}
RestartSec={{.RestartDelaySec}}
KillMode=control-group
{{- if .MemoryMax}}
MemoryMax={{.MemoryMax}}
MemoryLimit={{.MemoryMax}}
{{- end}}
{{- if .CPUQuota}}
CPUQuota={{.CPUQuota}}
{{- end}}
{{- if .DelegateSubTree}}
Delegate=yes
{{- end}}
{{- if gt .WatchdogSec 0}}
WatchdogSec={{.WatchdogSec}}
{{- end}}
{{- if .EnvFile}}
EnvironmentFile=-{{.EnvFile}}
{{- end}}

[Install]
WantedBy=multi-user.target
`

func (g *Guard) installSystemd() error {
	svcPath := filepath.Join(filepath.Dir(g.Config.ExecPath), g.Config.Name+".service")
	tmpl, err := template.New("svc").Parse(systemdTmpl)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(svcPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		return err
	}
	defer f.Close()
	if err := tmpl.Execute(f, g.Config); err != nil {
		return err
	}
	if err := run("systemctl", "enable", svcPath); err != nil {
		return fmt.Errorf("systemctl enable: %w", err)
	}
	_ = run("systemctl", "daemon-reload")
	return nil
}

// ---------- sysvinit ----------

func (g *Guard) installSysvinit() error {
	ctlPath := filepath.Join(g.Config.WorkingDir, g.Config.Name+"ctl")
	script := fmt.Sprintf(`#!/bin/sh
### BEGIN INIT INFO
# Provides:             %s
# Required-Start:       $local_fs $network $syslog
# Required-Stop:        $local_fs $network $syslog
# Default-Start:        2 3 4 5
# Default-Stop:         0 1 6
### END INIT INFO
CTL="%s"
case "$1" in
    start)   "${CTL}" start ;;
    stop)    "${CTL}" stop ;;
    restart) "${CTL}" restart ;;
    *) echo "Usage: $0 {start|stop|restart}" && exit 1 ;;
esac
exit 0
`, g.Config.Name, ctlPath)

	initPath := filepath.Join("/etc/init.d/", g.Config.Name)
	if err := os.WriteFile(initPath, []byte(script), 0755); err != nil {
		return err
	}
	if _, err := exec.LookPath("update-rc.d"); err == nil {
		_ = run("update-rc.d", g.Config.Name, "defaults")
	} else if _, err := exec.LookPath("chkconfig"); err == nil {
		_ = run("chkconfig", "--add", g.Config.Name)
	}
	return nil
}

func (g *Guard) sysvinitStart() error {
	cmd := exec.Command(g.Config.ExecPath)
	cmd.Dir = g.Config.WorkingDir
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Env = append(os.Environ(), "service_type=sysvinit")
	if err := cmd.Start(); err != nil {
		return err
	}
	g.installCrontab()
	return nil
}

func (g *Guard) sysvinitStop() error {
	g.removeCrontab()
	pidFile := fmt.Sprintf("/var/run/%s.pid", g.Config.Name)
	data, err := os.ReadFile(pidFile)
	if err != nil {
		return nil
	}
	var pid int
	if _, err := fmt.Sscanf(string(data), "%d", &pid); err != nil || pid < 2 {
		return nil
	}
	_ = syscall.Kill(-pid, syscall.SIGTERM)
	deadline := time.After(30 * time.Second)
	tick := time.NewTicker(200 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-tick.C:
			if err := syscall.Kill(pid, 0); err != nil {
				return nil
			}
		case <-deadline:
			_ = syscall.Kill(-pid, syscall.SIGKILL)
			return nil
		}
	}
}

func (g *Guard) installCrontab() {
	ctlPath := filepath.Join(g.Config.WorkingDir, g.Config.Name+"ctl")
	content := fmt.Sprintf("* * * * * root %s check\n", ctlPath)
	crontabFile := filepath.Join("/etc/cron.d/", g.Config.Name)
	_ = os.WriteFile(crontabFile, []byte(content), 0600)
	_ = run("service", "cron", "restart")
	_ = run("service", "crond", "restart")
}

func (g *Guard) removeCrontab() {
	_ = os.RemoveAll(filepath.Join("/etc/cron.d/", g.Config.Name))
	_ = run("service", "cron", "restart")
	_ = run("service", "crond", "restart")
}

// ---------- helpers ----------

func run(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func pidFileAlive(path string) bool {
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	var pid int
	if _, err := fmt.Sscanf(string(data), "%d", &pid); err != nil || pid < 2 {
		return false
	}
	return syscall.Kill(pid, 0) == nil
}
