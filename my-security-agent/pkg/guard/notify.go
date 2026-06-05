package guard

import (
	"net"
	"os"
)

// NotifyWatchdog sends WATCHDOG=1 to systemd.
// Safe to call even when not running under systemd.
func NotifyWatchdog() {
	sdNotify("WATCHDOG=1")
}

// NotifyReady sends READY=1 to systemd (Type=notify).
func NotifyReady() {
	sdNotify("READY=1")
}

func sdNotify(state string) {
	addr := os.Getenv("NOTIFY_SOCKET")
	if addr == "" {
		return
	}
	conn, err := net.DialUnix("unixgram", nil, &net.UnixAddr{Name: addr, Net: "unixgram"})
	if err != nil {
		return
	}
	defer conn.Close()
	_, _ = conn.Write([]byte(state))
}
