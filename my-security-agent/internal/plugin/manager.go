package plugin

import (
	"bufio"
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"my-security-agent/internal/config"
	"my-security-agent/pkg/integrity"
)

// Plugin represents a running plugin subprocess.
type Plugin struct {
	Name   string
	cmd    *exec.Cmd
	rxPipe *os.File
	txPipe *os.File
	reader *bufio.Reader
	done   chan struct{}
	mu     sync.Mutex
}

// Manager owns all loaded plugins.
type Manager struct {
	plugins map[string]*Plugin
	mu      sync.RWMutex
}

func NewManager() *Manager {
	return &Manager{plugins: make(map[string]*Plugin)}
}

// LoadPlugin verifies the binary, launches the subprocess, and wires up pipe I/O.
func (m *Manager) LoadPlugin(ctx context.Context, cfg config.PluginConfig) error {
	// integrity check
	if cfg.Sha256 != "" {
		if err := integrity.VerifyFile(cfg.ExecPath, cfg.Sha256); err != nil {
			log.Printf("[PLUGIN:%s] local integrity failed: %v", cfg.Name, err)
			if len(cfg.DownloadURLs) > 0 {
				log.Printf("[PLUGIN:%s] downloading...", cfg.Name)
				if err := integrity.DownloadVerified(ctx, cfg.ExecPath, integrity.DownloadConfig{
					URLs:         cfg.DownloadURLs,
					ExpectedHash: cfg.Sha256,
				}); err != nil {
					return fmt.Errorf("download plugin %s: %w", cfg.Name, err)
				}
			} else {
				return fmt.Errorf("plugin %s integrity failed, no download URLs", cfg.Name)
			}
		}
		log.Printf("[PLUGIN:%s] integrity OK", cfg.Name)
	}

	// create pipes: agent reads from agentRxR, plugin writes to agentRxW (fd4)
	//               agent writes to agentTxW, plugin reads from agentTxR (fd3)
	agentRxR, agentRxW, err := os.Pipe()
	if err != nil {
		return fmt.Errorf("rx pipe: %w", err)
	}
	agentTxR, agentTxW, err := os.Pipe()
	if err != nil {
		return fmt.Errorf("tx pipe: %w", err)
	}

	cmd := exec.CommandContext(ctx, cfg.ExecPath)
	cmd.Dir = filepath.Dir(cfg.ExecPath)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.ExtraFiles = []*os.File{agentTxR, agentRxW} // child fd3=read, fd4=write

	stderrFile, _ := os.OpenFile(cfg.ExecPath+".stderr",
		os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
	cmd.Stderr = stderrFile

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start plugin %s: %w", cfg.Name, err)
	}
	agentTxR.Close()
	agentRxW.Close()

	plg := &Plugin{
		Name:   cfg.Name,
		cmd:    cmd,
		rxPipe: agentRxR,
		txPipe: agentTxW,
		reader: bufio.NewReaderSize(agentRxR, 128*1024),
		done:   make(chan struct{}),
	}

	go func() {
		waitErr := cmd.Wait()
		if stderrFile != nil {
			stderrFile.Close()
		}
		if waitErr != nil {
			log.Printf("[PLUGIN:%s] exited: %v (code=%d)", plg.Name, waitErr, cmd.ProcessState.ExitCode())
		} else {
			log.Printf("[PLUGIN:%s] exited normally", plg.Name)
		}
		close(plg.done)
	}()

	go func() {
		for {
			data, err := plg.receiveData()
			if err != nil {
				if err != io.EOF && err != io.ErrClosedPipe {
					log.Printf("[PLUGIN:%s] read error: %v", plg.Name, err)
				}
				return
			}
			log.Printf("[PLUGIN:%s] received %d bytes", plg.Name, len(data))
		}
	}()

	m.mu.Lock()
	m.plugins[cfg.Name] = plg
	m.mu.Unlock()

	log.Printf("[PLUGIN:%s] loaded pid=%d", cfg.Name, cmd.Process.Pid)
	return nil
}

// ShutdownPlugin closes pipes, waits 10s, then SIGKILLs.
func (m *Manager) ShutdownPlugin(name string) {
	m.mu.Lock()
	plg, ok := m.plugins[name]
	if !ok {
		m.mu.Unlock()
		return
	}
	delete(m.plugins, name)
	m.mu.Unlock()

	plg.txPipe.Close()
	plg.rxPipe.Close()

	select {
	case <-time.After(10 * time.Second):
		log.Printf("[PLUGIN:%s] timeout, killing", name)
		syscall.Kill(-plg.cmd.Process.Pid, syscall.SIGKILL)
		<-plg.done
	case <-plg.done:
		log.Printf("[PLUGIN:%s] stopped gracefully", name)
	}
}

// ShutdownAll stops every plugin concurrently.
func (m *Manager) ShutdownAll() {
	m.mu.RLock()
	names := make([]string, 0, len(m.plugins))
	for n := range m.plugins {
		names = append(names, n)
	}
	m.mu.RUnlock()

	wg := &sync.WaitGroup{}
	for _, n := range names {
		wg.Add(1)
		go func(name string) {
			defer wg.Done()
			m.ShutdownPlugin(name)
		}(n)
	}
	wg.Wait()
}

func (plg *Plugin) receiveData() ([]byte, error) {
	var length uint32
	if err := binary.Read(plg.reader, binary.LittleEndian, &length); err != nil {
		return nil, err
	}
	buf := make([]byte, length)
	if _, err := io.ReadFull(plg.reader, buf); err != nil {
		return nil, err
	}
	return buf, nil
}

// SendTask writes a length-prefixed message to the plugin.
func (plg *Plugin) SendTask(data []byte) error {
	plg.mu.Lock()
	defer plg.mu.Unlock()
	header := make([]byte, 4)
	binary.LittleEndian.PutUint32(header, uint32(len(data)))
	if _, err := plg.txPipe.Write(header); err != nil {
		return err
	}
	_, err := plg.txPipe.Write(data)
	return err
}
