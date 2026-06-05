package cgroupctl

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

// Limits defines resource constraints for a cgroup.
type Limits struct {
	CPUQuotaPercent int   // CPU quota percentage, e.g. 10 means 10%
	MemoryLimitMB   int64 // memory ceiling in megabytes
}

// CGroup represents a cgroup v1 group with named/cpu/memory subsystems.
type CGroup struct {
	name       string
	cpuPath    string
	memoryPath string
	namedPath  string
}

// New creates a new cgroup and applies resource limits.
func New(name string, limits Limits) (*CGroup, error) {
	if name == "" {
		return nil, errors.New("cgroup name is required")
	}
	namedRoot, cpuRoot, memRoot, cpuOK, memOK := findMountPoints()
	cg := &CGroup{name: name}

	if namedRoot == "" {
		namedRoot = "/tmp/cgroup_named"
		_ = os.MkdirAll(namedRoot, 0700)
		_ = exec.Command("mount", "-t", "cgroup", "-o", "none,name=all", "cgroup", namedRoot).Run()
	}
	cg.namedPath = filepath.Join(namedRoot, name)
	_ = os.MkdirAll(cg.namedPath, 0700)

	if cpuOK && limits.CPUQuotaPercent > 0 {
		cg.cpuPath = filepath.Join(cpuRoot, name)
		_ = os.MkdirAll(cg.cpuPath, 0700)
		period := readInt(filepath.Join(cg.cpuPath, "cpu.cfs_period_us"), 100000)
		quota := period * int64(limits.CPUQuotaPercent) / 100
		if quota < 10000 {
			quota = 10000
		}
		writeInt(filepath.Join(cg.cpuPath, "cpu.cfs_quota_us"), quota)
	}

	if memOK && limits.MemoryLimitMB > 0 {
		cg.memoryPath = filepath.Join(memRoot, name)
		_ = os.MkdirAll(cg.memoryPath, 0700)
		writeInt(filepath.Join(cg.memoryPath, "memory.limit_in_bytes"),
			limits.MemoryLimitMB*1024*1024)
	}

	return cg, nil
}

// AddProcess moves a pid into all configured subsystems.
func (cg *CGroup) AddProcess(pid int) error {
	data := []byte(strconv.Itoa(pid))
	if cg.namedPath != "" {
		if err := retryWrite(filepath.Join(cg.namedPath, "cgroup.procs"), data); err != nil {
			return fmt.Errorf("named cgroup: %w", err)
		}
	}
	if cg.cpuPath != "" {
		if err := retryWrite(filepath.Join(cg.cpuPath, "cgroup.procs"), data); err != nil {
			return fmt.Errorf("cpu cgroup: %w", err)
		}
	}
	if cg.memoryPath != "" {
		if err := retryWrite(filepath.Join(cg.memoryPath, "cgroup.procs"), data); err != nil {
			return fmt.Errorf("memory cgroup: %w", err)
		}
	}
	return nil
}

// Destroy removes the cgroup directories.
func (cg *CGroup) Destroy() {
	for _, p := range []string{cg.namedPath, cg.cpuPath, cg.memoryPath} {
		if p != "" {
			os.Remove(p)
		}
	}
}

// ---------- internal ----------

func findMountPoints() (named, cpu, mem string, cpuOK, memOK bool) {
	if f, err := os.Open("/proc/cgroups"); err == nil {
		s := bufio.NewScanner(f)
		for s.Scan() {
			fields := strings.Fields(s.Text())
			if len(fields) >= 4 {
				if fields[0] == "cpu" && fields[3] == "1" {
					cpuOK = true
				}
				if fields[0] == "memory" && fields[3] == "1" {
					memOK = true
				}
			}
		}
		f.Close()
	}

	if f, err := os.Open("/proc/self/mountinfo"); err == nil {
		s := bufio.NewScanner(f)
		for s.Scan() {
			fields := strings.Fields(s.Text())
			if len(fields) < 10 {
				continue
			}
			if fields[len(fields)-3] == "cgroup" {
				for _, sub := range strings.Split(fields[len(fields)-1], ",") {
					switch sub {
					case "cpu":
						cpu = fields[4]
					case "memory":
						mem = fields[4]
					case "name=all":
						named = fields[4]
					}
				}
			}
		}
		f.Close()
	}
	return
}

func retryWrite(path string, data []byte) error {
	for {
		err := os.WriteFile(path, data, 0644)
		if err == nil || !errors.Is(err, syscall.EINTR) {
			return err
		}
	}
}

func readInt(path string, def int64) int64 {
	data, err := os.ReadFile(path)
	if err != nil {
		return def
	}
	v, err := strconv.ParseInt(strings.TrimSpace(string(data)), 10, 64)
	if err != nil {
		return def
	}
	return v
}

func writeInt(path string, val int64) {
	_ = retryWrite(path, []byte(strconv.FormatInt(val, 10)))
}
