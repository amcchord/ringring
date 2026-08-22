package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/amcchord/ringring/internal/config"
	"github.com/amcchord/ringring/internal/maintenance"
	"github.com/amcchord/ringring/internal/openairuntime"
	"github.com/amcchord/ringring/internal/radio"
	"github.com/amcchord/ringring/internal/secure"
	"github.com/amcchord/ringring/internal/store"
	"github.com/amcchord/ringring/internal/telephony"
	"github.com/amcchord/ringring/internal/voice"
	"github.com/amcchord/ringring/internal/weather"
	"github.com/amcchord/ringring/internal/webapp"
)

func main() {
	if len(os.Args) == 2 && os.Args[1] == "radio-catalog" {
		for _, station := range radio.All() {
			fmt.Printf("%s\t%s\n", station.ID, station.StreamURL)
		}
		return
	}
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	cfg, err := config.Load()
	if err != nil {
		logger.Error("load configuration", "error", err)
		os.Exit(1)
	}
	if len(os.Args) > 1 {
		if len(os.Args) != 2 {
			logger.Error("unknown command")
			os.Exit(2)
		}
		switch os.Args[1] {
		case "verify-state":
			report, err := maintenance.VerifyState(context.Background(), cfg.DatabasePath, cfg.MasterKey)
			if err != nil {
				logger.Error("verify restored state", "error", err)
				os.Exit(1)
			}
			if err := json.NewEncoder(os.Stdout).Encode(report); err != nil {
				logger.Error("write verification report", "error", err)
				os.Exit(1)
			}
		case "verify-ami":
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			statuses, err := (telephony.AMI{
				Address: cfg.AsteriskAMIAddr, Username: cfg.AsteriskAMIUser, Secret: cfg.AsteriskAMISecret,
			}).ContactStatuses(ctx)
			if err != nil {
				logger.Error("verify AMI contact access", "error", err)
				os.Exit(1)
			}
			if err := json.NewEncoder(os.Stdout).Encode(struct {
				Status       string `json:"status"`
				ContactCount int    `json:"contact_count"`
			}{Status: "ok", ContactCount: len(statuses)}); err != nil {
				logger.Error("write AMI verification report", "error", err)
				os.Exit(1)
			}
		default:
			logger.Error("unknown command")
			os.Exit(2)
		}
		return
	}
	database, err := store.Open(cfg.DatabasePath)
	if err != nil {
		logger.Error("open database", "error", err)
		os.Exit(1)
	}
	defer database.Close()
	cipher, err := secure.NewCipher(cfg.MasterKey)
	if err != nil {
		logger.Error("create credential cipher", "error", err)
		os.Exit(1)
	}
	app, err := webapp.New(cfg, database, cipher, logger)
	if err != nil {
		logger.Error("create application", "error", err)
		os.Exit(1)
	}
	if err := app.ReconcileTelephony(context.Background()); err != nil {
		logger.Warn("initial telephony reconcile", "error", err)
	}
	voiceListener, err := net.Listen("tcp", cfg.FastAGIAddr)
	if err != nil {
		logger.Error("listen for FastAGI", "address", cfg.FastAGIAddr, "error", err)
		os.Exit(1)
	}
	defer voiceListener.Close()
	aiListener, err := net.Listen("tcp", cfg.AIAudioAddr)
	if err != nil {
		logger.Error("listen for AI AudioSocket", "address", cfg.AIAudioAddr, "error", err)
		os.Exit(1)
	}
	defer aiListener.Close()
	voiceServer := &voice.Server{
		Source: database, Extensions: database, Reconcile: app.ReconcileTelephony,
		Cipher: cipher, Weather: weather.New(nil), Speech: openairuntime.New(nil),
		AudioDir: cfg.VoiceAudioDir, PlaybackDir: cfg.VoicePlaybackDir, Logger: logger,
		AIModel: cfg.AIRealtimeModel, AICallMaxDuration: cfg.AICallMaxDuration, AIMaxConcurrent: cfg.AIMaxConcurrent,
	}
	go func() {
		if err := voiceServer.Serve(voiceListener); err != nil {
			logger.Error("FastAGI server stopped", "error", err)
		}
	}()
	go func() {
		if err := voiceServer.ServeAudioSocket(aiListener); err != nil {
			logger.Error("AI AudioSocket server stopped", "error", err)
		}
	}()

	server := &http.Server{
		Addr: cfg.HTTPAddr, Handler: app,
		ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second,
		WriteTimeout: 30 * time.Second, IdleTimeout: 60 * time.Second,
	}

	shutdownSignals, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		<-shutdownSignals.Done()
		_ = voiceListener.Close()
		_ = aiListener.Close()
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := server.Shutdown(ctx); err != nil {
			logger.Error("graceful shutdown", "error", err)
		}
	}()

	logger.Info("RingRing listening", "address", cfg.HTTPAddr, "fastagi_address", cfg.FastAGIAddr, "ai_audio_address", cfg.AIAudioAddr, "environment", cfg.Environment)
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		logger.Error("HTTP server stopped", "error", err)
		os.Exit(1)
	}
}
