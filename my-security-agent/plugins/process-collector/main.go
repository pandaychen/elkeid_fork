package main

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"time"
)

// Pipe fd convention: fd3 = read tasks from agent, fd4 = write data to agent
var (
	writePipe = os.NewFile(4, "pipe-write")
)

type ProcessInfo struct {
	PID     int    `json:"pid"`
	Name    string `json:"name"`
	Cmdline string `json:"cmdline"`
	PPID    int    `json:"ppid"`
}

func main() {
	log.SetPrefix("[process-collector] ")
	log.SetOutput(os.Stderr)
	log.Println("started")

	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	collectAndSend()
	for range ticker.C {
		collectAndSend()
	}
}

func collectAndSend() {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		log.Printf("readdir /proc: %v", err)
		return
	}

	count := 0
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		pid, err := strconv.Atoi(e.Name())
		if err != nil {
			continue
		}

		info := ProcessInfo{PID: pid}

		if data, err := os.ReadFile(fmt.Sprintf("/proc/%d/comm", pid)); err == nil {
			info.Name = strings.TrimSpace(string(data))
		}
		if data, err := os.ReadFile(fmt.Sprintf("/proc/%d/cmdline", pid)); err == nil {
			info.Cmdline = strings.ReplaceAll(string(data), "\x00", " ")
			info.Cmdline = strings.TrimSpace(info.Cmdline)
		}
		if data, err := os.ReadFile(fmt.Sprintf("/proc/%d/status", pid)); err == nil {
			for _, line := range strings.Split(string(data), "\n") {
				if strings.HasPrefix(line, "PPid:") {
					fields := strings.Fields(line)
					if len(fields) >= 2 {
						info.PPID, _ = strconv.Atoi(fields[1])
					}
					break
				}
			}
		}

		payload, err := json.Marshal(info)
		if err != nil {
			continue
		}
		sendData(payload)
		count++
	}
	log.Printf("collected %d processes", count)
}

func sendData(data []byte) {
	header := make([]byte, 4)
	binary.LittleEndian.PutUint32(header, uint32(len(data)))
	writePipe.Write(header)
	writePipe.Write(data)
}
