package server

import (
	"log/slog"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

type serviceNotifier struct {
	conn   *net.UnixConn
	logger *slog.Logger
	stop   chan struct{}
	once   sync.Once
	health func() bool
}

func newServiceNotifier(logger *slog.Logger, health func() bool) *serviceNotifier {
	n := &serviceNotifier{logger: logger, stop: make(chan struct{}), health: health}
	name := os.Getenv("NOTIFY_SOCKET")
	if name == "" {
		return n
	}
	if strings.HasPrefix(name, "@") {
		name = "\x00" + name[1:]
	}
	conn, err := net.DialUnix("unixgram", nil, &net.UnixAddr{Name: name, Net: "unixgram"})
	if err != nil {
		logger.Warn("systemd notify unavailable", "error", err)
		return n
	}
	n.conn = conn
	if usec, err := strconv.ParseInt(os.Getenv("WATCHDOG_USEC"), 10, 64); err == nil && usec > 0 {
		interval := time.Duration(usec) * time.Microsecond / 2
		go n.watchdog(interval)
	}
	return n
}

func (n *serviceNotifier) send(state string) {
	if n.conn == nil {
		return
	}
	if _, err := n.conn.Write([]byte(state)); err != nil {
		n.logger.Warn("systemd notify failed", "state", state, "error", err)
	}
}

func (n *serviceNotifier) ready() { n.send("READY=1\nSTATUS=Ready") }

func (n *serviceNotifier) watchdog(interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			if n.health == nil || n.health() {
				n.send("WATCHDOG=1")
			}
		case <-n.stop:
			return
		}
	}
}

func (n *serviceNotifier) close() {
	n.once.Do(func() {
		close(n.stop)
		n.send("STOPPING=1\nSTATUS=Stopping")
		if n.conn != nil {
			_ = n.conn.Close()
		}
	})
}
