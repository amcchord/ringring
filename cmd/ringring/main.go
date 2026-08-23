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
	"github.com/amcchord/ringring/internal/openaiadmin"
	"github.com/amcchord/ringring/internal/openairuntime"
	"github.com/amcchord/ringring/internal/radio"
	"github.com/amcchord/ringring/internal/secure"
	"github.com/amcchord/ringring/internal/store"
	"github.com/amcchord/ringring/internal/telephony"
	"github.com/amcchord/ringring/internal/voice"
	"github.com/amcchord/ringring/internal/weather"
	"github.com/amcchord/ringring/internal/webapp"
)

const (
	openAIRetentionRequestTimeout = 10 * time.Second
	openAIRetentionAuditTimeout   = 30 * time.Second
)

type openAIRetentionVerifier interface {
	VerifyOrganizationZeroDataRetention(context.Context) (openaiadmin.OrganizationDataRetention, error)
	VerifyProjectZeroDataRetention(context.Context, string) (openaiadmin.ProjectDataRetention, error)
}

type openAIProjectSource interface {
	ListOpenAIProjectIDs(context.Context) ([]string, error)
}

type openAIRetentionReport struct {
	OrganizationType string `json:"organization_type"`
	ProjectsVerified int    `json:"projects_verified"`
}

func requireOpenAIZeroDataRetention(ctx context.Context, required bool, verifier openAIRetentionVerifier, projects openAIProjectSource) (openAIRetentionReport, error) {
	if !required {
		return openAIRetentionReport{}, nil
	}
	organization, err := verifier.VerifyOrganizationZeroDataRetention(ctx)
	if err != nil {
		return openAIRetentionReport{}, err
	}
	projectIDs, err := projects.ListOpenAIProjectIDs(ctx)
	if err != nil {
		return openAIRetentionReport{}, err
	}
	for index, projectID := range projectIDs {
		if _, err := verifier.VerifyProjectZeroDataRetention(ctx, projectID); err != nil {
			return openAIRetentionReport{}, fmt.Errorf("verify OpenAI project Zero Data Retention (%d of %d): %w", index+1, len(projectIDs), err)
		}
	}
	return openAIRetentionReport{OrganizationType: organization.Type, ProjectsVerified: len(projectIDs)}, nil
}

func openAIAdminClient(cfg config.Config) *openaiadmin.Client {
	return openaiadmin.New(cfg.OpenAIAdminKey, cfg.OpenAIPartySpendLimitCents, &http.Client{Timeout: openAIRetentionRequestTimeout})
}

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
		case "verify-openai-retention":
			database, err := store.Open(cfg.DatabasePath)
			if err != nil {
				logger.Error("open database for OpenAI retention verification", "error", err)
				os.Exit(1)
			}
			defer database.Close()
			ctx, cancel := context.WithTimeout(context.Background(), openAIRetentionAuditTimeout)
			defer cancel()
			retention, err := requireOpenAIZeroDataRetention(ctx, true, openAIAdminClient(cfg), database)
			if err != nil {
				logger.Error("verify OpenAI Zero Data Retention", "error", err)
				os.Exit(1)
			}
			if err := json.NewEncoder(os.Stdout).Encode(struct {
				Status string `json:"status"`
				openAIRetentionReport
			}{Status: "ok", openAIRetentionReport: retention}); err != nil {
				logger.Error("write OpenAI retention verification report", "error", err)
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
	if err := database.EnforceAIAdultOnlyGate(context.Background(), cfg.AIAdultOnlyEnabled, time.Now()); err != nil {
		logger.Error("enforce AI conversation adult-only gate", "error", err)
		os.Exit(1)
	}
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
	metricsListener, err := net.Listen("tcp", cfg.MetricsAddr)
	if err != nil {
		logger.Error("listen for internal metrics", "address", cfg.MetricsAddr, "error", err)
		os.Exit(1)
	}
	defer metricsListener.Close()
	metricsServer := &http.Server{
		Handler: app.MetricsHandler(), ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout: 5 * time.Second, WriteTimeout: 5 * time.Second, IdleTimeout: 30 * time.Second,
	}
	go func() {
		if err := metricsServer.Serve(metricsListener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("internal metrics server stopped", "error", err)
		}
	}()
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
		Source: database, AIAdultAccess: database, Extensions: database, Reconcile: app.ReconcileTelephony,
		Cipher: cipher, Weather: weather.New(nil), Speech: openairuntime.New(nil),
		AudioDir: cfg.VoiceAudioDir, PlaybackDir: cfg.VoicePlaybackDir, Logger: logger,
		AIModel: cfg.AIRealtimeModel, AICallMaxDuration: cfg.AICallMaxDuration, AIMaxConcurrent: cfg.AIMaxConcurrent,
		AIAdultOnlyEnabled: cfg.AIAdultOnlyEnabled,
		Metrics:            app.Metrics(),
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
		if err := metricsServer.Shutdown(ctx); err != nil {
			logger.Error("internal metrics shutdown", "error", err)
		}
		if err := server.Shutdown(ctx); err != nil {
			logger.Error("graceful shutdown", "error", err)
		}
	}()

	logger.Info("RingRing listening", "address", cfg.HTTPAddr, "metrics_address", cfg.MetricsAddr, "fastagi_address", cfg.FastAGIAddr, "ai_audio_address", cfg.AIAudioAddr, "environment", cfg.Environment)
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		logger.Error("HTTP server stopped", "error", err)
		os.Exit(1)
	}
}
