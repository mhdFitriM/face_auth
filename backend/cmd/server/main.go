package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"face_auth/internal"
)

func main() {
	cfg := internal.LoadConfig()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	store, err := internal.NewStore(ctx, cfg)
	if err != nil {
		log.Fatalf("store init: %v", err)
	}
	defer store.Close()

	if err := internal.Bootstrap(ctx, store); err != nil {
		log.Printf("WARN: bootstrap: %v", err)
	}

	if dp := os.Getenv("DEBUG_PORT"); dp != "" {
		internal.StartDebugListener(dp)
	}

	hub := internal.NewAgentHub()

	pushApp := internal.NewPushServer(store, cfg)
	apiApp := internal.NewAPIServer(store, cfg, hub)

	stopPolicy := internal.StartPolicyRunner(store, hub)
	defer stopPolicy()

	go func() {
		log.Printf("device push listener on :%s (plain HTTP)", cfg.PushPort)
		if err := pushApp.Listen(":" + cfg.PushPort); err != nil {
			log.Fatalf("push listener: %v", err)
		}
	}()

	if cfg.TLSPort != "" {
		certFile, keyFile, err := internal.EnsureSelfSignedCert(cfg.CertDir, cfg.PublicPushHost)
		if err != nil {
			log.Printf("WARN: TLS cert generation failed, skipping TLS listener: %v", err)
		} else {
			go func() {
				log.Printf("device push listener on :%s (TLS, cert=%s)", cfg.TLSPort, certFile)
				if err := pushApp.ListenTLS(":"+cfg.TLSPort, certFile, keyFile); err != nil {
					log.Printf("TLS listener: %v", err)
				}
			}()
		}
	}

	go func() {
		log.Printf("admin api on :%s", cfg.APIPort)
		if err := apiApp.Listen(":" + cfg.APIPort); err != nil {
			log.Fatalf("api listener: %v", err)
		}
	}()

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	<-sig
	log.Println("shutting down...")
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()
	_ = pushApp.ShutdownWithContext(shutdownCtx)
	_ = apiApp.ShutdownWithContext(shutdownCtx)
}
