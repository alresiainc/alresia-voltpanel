package main

import (
	"context"
	"embed"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/alresiainc/alresia-voltpanel/internal/server"
	"github.com/alresiainc/alresia-voltpanel/internal/storage"
)

//go:embed dist/*
var embeddedUI embed.FS

func main() {
	if dispatch(os.Args[1:]) {
		return
	}

	dev := os.Getenv("DEV") == "1"
	defaultPort := 7788
	if p := os.Getenv("PORT"); p != "" {
		if v, err := strconv.Atoi(p); err == nil {
			defaultPort = v
		}
	}
	portFlag := flag.Int("port", defaultPort, "port to listen on")
	devFlag := flag.Bool("dev", dev, "development mode")
	flag.Parse()

	cfgDir, err := storage.EnsureDirs()
	if err != nil {
		log.Fatalf("failed to ensure config dirs: %v", err)
	}
	setupDaemonLog(cfgDir)

	cfg, err := storage.LoadOrInitConfig()
	if err != nil {
		log.Fatalf("failed to load config: %v", err)
	}

	port := probePort(*portFlag)
	cfg.Port = port
	_ = storage.SaveConfig(cfg)

	if err := writePIDFile(cfgDir); err != nil {
		log.Printf("volt: warning: failed to write pid file: %v", err)
	}
	defer removePIDFile(cfgDir)

	srv, err := server.New(server.Options{
		Port:          port,
		Dev:           *devFlag,
		Bind:          "127.0.0.1",
		Token:         cfg.Token,
		SessionSecret: cfg.SessionSecret,
		EmbeddedFS:    embeddedUI,
		CfgDir:        cfgDir,
	})
	if err != nil {
		log.Fatalf("failed to init server: %v", err)
	}

	// Dependency-ordered autostart (§15): bring up every persisted
	// autostart=true service, in topological order, before the daemon
	// starts serving. Failures here are logged, not fatal -- a bad
	// dependency graph or a single stuck service shouldn't prevent the
	// control plane itself (and the UI to fix the problem) from coming up.
	if err := srv.StartAutostartServices(context.Background()); err != nil {
		log.Printf("volt: autostart failed: %v", err)
	}

	// SIGTERM/SIGINT trigger a graceful shutdown (§12: this is what makes
	// `volt stop` work, rather than the daemon running forever with no way
	// to ask it to exit cleanly).
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGTERM, os.Interrupt)
	go func() {
		<-stop
		log.Printf("volt: received stop signal, shutting down")
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := srv.Shutdown(ctx); err != nil {
			log.Printf("volt: shutdown error: %v", err)
		}
		srv.Close()
	}()

	log.Printf("VoltPanel listening on http://127.0.0.1:%d", port)
	if err := srv.Run(); err != nil {
		log.Fatalf("server error: %v", err)
	}
}

func probePort(start int) int {
	p := start
	for i := 0; i < 20; i++ {
		ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", p))
		if err == nil {
			_ = ln.Close()
			return p
		}
		p++
	}
	return start
}

// setupDaemonLog tees the standard logger to <cfgDir>/logs/daemon.log (in
// addition to stdout/stderr) so `volt logs` (§12) has a real file to read
// -- previously nothing but stdout ever captured the daemon's own runtime
// log lines.
func setupDaemonLog(cfgDir string) {
	f, err := os.OpenFile(cfgDir+"/logs/daemon.log", os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		log.Printf("volt: warning: failed to open daemon log file: %v", err)
		return
	}
	log.SetOutput(io.MultiWriter(os.Stderr, f))
}

func writePIDFile(cfgDir string) error {
	return os.WriteFile(cfgDir+"/daemon.pid", []byte(strconv.Itoa(os.Getpid())), 0o644)
}

func removePIDFile(cfgDir string) {
	_ = os.Remove(cfgDir + "/daemon.pid")
}
