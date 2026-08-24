package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"a6core/internal/apps"
	"a6core/internal/config"
	"a6core/internal/events"
	"a6core/internal/icons"
	"a6core/internal/modes"
	"a6core/internal/resources"
	"a6core/internal/retro"
	"a6core/internal/server"
	"a6core/internal/servers"
	"a6core/internal/shortcuts"
	"a6core/internal/state"
	"a6core/internal/system"
	"a6core/internal/telemetry"
	"a6core/internal/version"
)

func main() {
	versionFlag := flag.Bool("version", false, "print version and exit")
	configPath := flag.String("config", "", "path to a JSON config file (optional; defaults are used if omitted)")
	flag.Parse()

	if *versionFlag {
		fmt.Println(version.String)
		return
	}

	log.Printf("A6 Core v%s starting...", version.String)

	cfg, err := config.LoadConfig(*configPath)
	if err != nil {
		log.Fatalf("main: failed to load config: %v", err)
	}
	log.Printf("main: config loaded (env=%s, log_level=%s, data_dir=%s)", cfg.AppEnv, cfg.LogLevel, cfg.DataDir)

	store, err := state.Open(cfg.DataDir)
	if err != nil {
		log.Fatalf("main: failed to open state store: %v", err)
	}

	iconStore := icons.New(store)
	scStore := shortcuts.New(store, iconStore)
	retroStore := retro.New(cfg.DataDir)
	appMgr := apps.NewManager(scStore, retroStore, nil, nil)
	srvMgr := servers.NewManager(servers.Executor{}, 0, 0)

	combinedChecker := func() []resources.BlockingResource {
		var out []resources.BlockingResource
		out = append(out, appMgr.Blocking()...)
		out = append(out, srvMgr.Blocking()...)
		return out
	}

	modeMgr := modes.NewManager(store, combinedChecker, nil)

	sleepSetter := func(force bool) error {
		_, err := modeMgr.Switch(modes.ModeSleep, force)
		return err
	}
	sysMgr := system.NewManager(version.String, combinedChecker, nil, sleepSetter)

	hub := events.NewHub()

	// server.status_changed: internal/servers stays completely
	// unaware events exist (Stage 13 decision 2) — this closure is
	// the only place servers.Status becomes events.Event.
	srvMgr.SetOnChange(func(id string, status servers.Status) {
		hub.Publish(events.Event{
			Event: "server.status_changed",
			Data:  map[string]string{"id": id, "status": string(status)},
		})
	})

	sampler := telemetry.NewSampler(0) // 0 -> real 1s default

	// statusProvider closes over modeMgr/sampler — this is the ONLY
	// place telemetry numbers and the mode state machine's current
	// mode become a StatusTick, kept out of internal/events entirely
	// (same boundary-keeping as the servers callback above).
	statusProvider := func() events.StatusTick {
		mem, err := telemetry.ReadMemInfo()
		if err != nil {
			log.Printf("telemetry: WARNING ReadMemInfo failed: %v", err)
		}
		uptime, err := telemetry.UptimeSeconds()
		if err != nil {
			log.Printf("telemetry: WARNING UptimeSeconds failed: %v", err)
		}
		diskFree, err := telemetry.DiskFreeGB(cfg.DataDir)
		if err != nil {
			log.Printf("telemetry: WARNING DiskFreeGB failed: %v", err)
		}

		tick := events.StatusTick{
			Mode:          string(modeMgr.Current()),
			UptimeSeconds: uptime,
			RAMUsedMB:     mem.RAMUsedMB,
			RAMTotalMB:    mem.RAMTotalMB,
			SwapUsedMB:    mem.SwapUsedMB,
			DiskFreeGB:    diskFree,
			Timestamp:     time.Now().UTC().Format(time.RFC3339),
		}
		if cpuPct, ok := sampler.CPUPercent(); ok {
			tick.CPUPercent = cpuPct
		}
		if tempC, ok := telemetry.Temperature(); ok {
			tick.TemperatureC = tempC
		}
		return tick
	}

	srv := server.New("7887", store, modeMgr, sysMgr, scStore, iconStore, appMgr, retroStore, srvMgr, hub)

	// bgCtx governs every background goroutine started below — all
	// three are cancelled together, at the same moment the HTTP
	// server begins its own graceful shutdown, so nothing outlives
	// the server and nothing gets cut off before it.
	bgCtx, cancelBg := context.WithCancel(context.Background())
	defer cancelBg()

	go sampler.Run(bgCtx)
	go hub.Run(bgCtx)
	go hub.RunStatusTicks(bgCtx, statusProvider, 0) // 0 -> real 5s spec default

	serverErr := make(chan error, 1)
	go func() {
		serverErr <- srv.Start()
	}()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)

	select {
	case err := <-serverErr:
		cancelBg()
		if err != nil {
			log.Fatalf("main: server failed: %v", err)
		}
	case sig := <-sigCh:
		log.Printf("main: received %s, shutting down gracefully...", sig)
		cancelBg()

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		if err := srv.Shutdown(ctx); err != nil {
			log.Printf("main: WARNING graceful shutdown did not complete cleanly: %v", err)
		}
		if err := store.Save(); err != nil {
			log.Fatalf("main: CRITICAL final state save failed: %v", err)
		}
		log.Println("main: final state saved, exiting cleanly")
	}
}