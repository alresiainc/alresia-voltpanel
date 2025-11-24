package main

import (
	"embed"
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"strconv"

	"github.com/alresia/voltpanel/internal/server"
	"github.com/alresia/voltpanel/internal/storage"
)

//go:embed dist/*
var embeddedUI embed.FS

func main() {
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
	cfg, err := storage.LoadOrInitConfig()
	if err != nil {
		log.Fatalf("failed to load config: %v", err)
	}

	port := probePort(*portFlag)
	cfg.Port = port
	_ = storage.SaveConfig(cfg)

	srv, err := server.New(server.Options{
		Port:       port,
		Dev:        *devFlag,
		Bind:       "127.0.0.1",
		Token:      cfg.Token,
		EmbeddedFS: embeddedUI,
		CfgDir:     cfgDir,
	})
	if err != nil {
		log.Fatalf("failed to init server: %v", err)
	}
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
