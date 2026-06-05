package config

import (
	"os"
	"path/filepath"
)

type Config struct {
	ServiceName string
	WorkDir     string
	ExecPath    string
	PidFile     string

	HeartbeatIntervalSec int
	ServerAddr           string

	Plugins []PluginConfig
}

type PluginConfig struct {
	Name         string
	Version      string
	ExecPath     string
	Sha256       string
	DownloadURLs []string
}

func Load() *Config {
	workDir, _ := os.Getwd()
	execPath, _ := os.Executable()

	return &Config{
		ServiceName:          "my-security-agent",
		WorkDir:              workDir,
		ExecPath:             execPath,
		PidFile:              "/var/run/my-security-agent.pid",
		HeartbeatIntervalSec: 60,
		ServerAddr:           "127.0.0.1:8443",
		Plugins: []PluginConfig{
			{
				Name:    "process-collector",
				Version: "1.0.0",
				ExecPath: filepath.Join(workDir, "plugins", "process-collector",
					"process-collector"),
			},
		},
	}
}
