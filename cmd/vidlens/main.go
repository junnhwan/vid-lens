package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"golang.org/x/term"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"runtime"
	"runtime/debug"
	"syscall"
	"time"

	"vidlens/internal/config"
	"vidlens/internal/httpapi"
	"vidlens/internal/investigator"
	"vidlens/internal/jobs"
	"vidlens/internal/media"
	"vidlens/internal/resource"
	"vidlens/internal/store"
)

func main() {
	path := flag.String("config", "", "YAML configuration file; credentials must remain outside Git")
	migrate := flag.Bool("migrate-only", false, "apply PostgreSQL migrations and exit")
	password := flag.Bool("set-owner-password", false, "set/reset the owner password interactively and revoke owner sessions")
	flag.Parse()
	if *password {
		if e := setPassword(*path); e != nil {
			slog.Error("password reset failed", "reason", e.Error())
			os.Exit(1)
		}
		return
	}
	if e := run(*path, *migrate); e != nil {
		slog.Error("backend stopped", "reason", e.Error())
		os.Exit(1)
	}
}
func setPassword(path string) error {
	c, e := config.LoadFile(path)
	if e != nil {
		return e
	}
	s, e := store.Open(context.Background(), c.DatabaseURL, c.SecretKey)
	if e != nil {
		return e
	}
	defer s.Pool.Close()
	if e = s.Migrate(context.Background()); e != nil {
		return e
	}
	fmt.Fprint(os.Stderr, "New owner password (at least 12 bytes): ")
	first, e := term.ReadPassword(int(os.Stdin.Fd()))
	if e != nil {
		return e
	}
	fmt.Fprint(os.Stderr, "\nRepeat password: ")
	second, e := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Fprintln(os.Stderr)
	if e != nil {
		return e
	}
	if string(first) != string(second) {
		return errors.New("passwords do not match")
	}
	if e = s.SetOwnerPassword(context.Background(), string(first)); e != nil {
		return e
	}
	fmt.Fprintln(os.Stderr, "Owner password saved; previous management sessions revoked.")
	return nil
}
func run(path string, migrate bool) error {
	runtime.GOMAXPROCS(2)
	debug.SetMemoryLimit(768 << 20)
	debug.SetGCPercent(100)
	cfg, e := config.LoadFile(path)
	if e != nil {
		return e
	}
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()
	s, e := store.Open(ctx, cfg.DatabaseURL, cfg.SecretKey)
	if e != nil {
		return e
	}
	defer s.Pool.Close()
	if e = s.Migrate(ctx); e != nil {
		return e
	}
	if e = s.Bootstrap(ctx, cfg.Profile, cfg.ModelKey, cfg.ASRKey); e != nil {
		return e
	}
	if migrate {
		return nil
	}
	rm, e := resource.New(s, cfg)
	if e != nil {
		return e
	}
	defer rm.Redis.Close()
	ms, e := media.New(s, rm, cfg)
	if e != nil {
		return e
	}
	agent := &investigator.Engine{Store: s, Media: ms, Resources: rm, Config: cfg}
	worker, e := jobs.New(s, ms, agent, cfg)
	if e != nil {
		return e
	}
	if e = worker.Start(ctx); e != nil {
		return e
	}
	api := httpapi.New(ctx, s, cfg)
	api.CheckModel = agent.CheckModel
	server := &http.Server{Addr: cfg.Listen, Handler: api.Handler(), ReadHeaderTimeout: 10 * time.Second, ReadTimeout: 30 * time.Second, IdleTimeout: 90 * time.Second, MaxHeaderBytes: 16384}
	errs := make(chan error, 1)
	go func() { slog.Info("VidLens backend listening", "address", cfg.Listen); errs <- server.ListenAndServe() }()
	select {
	case e = <-errs:
		cancel()
	case <-ctx.Done():
	}
	stop, cc := context.WithTimeout(context.Background(), 30*time.Second)
	defer cc()
	_ = server.Shutdown(stop)
	worker.Shutdown()
	if e == http.ErrServerClosed {
		return nil
	}
	return e
}
