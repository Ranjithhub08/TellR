package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	_ "time/tzdata"

	"tellr/actions"
	"tellr/ai"
	"tellr/confirm"
	"tellr/handlers"
	"tellr/store"
	"tellr/web"
)

type backend struct {
	*store.Store
}

func (b backend) Tx(ctx context.Context, fn func(actions.Repo) error) error {
	return b.Store.WithTx(ctx, func(r store.Repo) error { return fn(r) })
}

func main() {
	log := slog.New(slog.NewTextHandler(os.Stdout, nil))
	if err := run(log); err != nil {
		log.Error("fatal", "err", err)
		os.Exit(1)
	}
}

func run(log *slog.Logger) error {
	cfg := loadConfig(os.Getenv)

	if dir := filepath.Dir(cfg.DBPath); dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("create data directory: %w", err)
		}
	}
	st, err := store.Open(cfg.DBPath)
	if err != nil {
		return err
	}
	defer st.Close()

	client := ai.NewClient(cfg.BaseURL, cfg.APIKey, cfg.Model)
	speech := ai.NewSpeech(cfg.BaseURL, cfg.APIKey, cfg.TTSModel, cfg.TTSVoice)
	indic := ai.NewIndicSpeech("https://api.sarvam.ai", cfg.SarvamKey, cfg.SarvamModel, cfg.SarvamVoice)
	transcriber := ai.NewTranscriber(cfg.BaseURL, cfg.APIKey, cfg.STTModel).WithIndicModel(cfg.STTIndic)
	transcriber.Log = log
	if cfg.SarvamKey != "" {
		transcriber.Sarvam = ai.NewSarvamSTT("https://api.sarvam.ai", cfg.SarvamKey, cfg.SarvamSTT)
	}
	srv := &handlers.Server{
		DB:         backend{st},
		Planner:    client,
		Speaker:    speech,
		Translator: client,
		Listener:   transcriber,
		Indic:      indic,
		AIEnabled:  client.Enabled(),
		Model:      cfg.Model,
		Confirms:   confirm.New(2 * time.Minute),
		Static:     handlers.NoCache(http.FileServerFS(web.FS())),
		Log:        log,
		VoiceLimit: handlers.NewLimiter(cfg.VoicePerMin, 10),
		IPLimit:    handlers.NewIPLimiter(cfg.IPPerMin, 6),
		TrustProxy: cfg.TrustProxy,
	}

	httpSrv := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           srv.Routes(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	errCh := make(chan error, 1)
	go func() {
		log.Info("listening", "addr", httpSrv.Addr, "ai", client.Enabled(), "model", cfg.Model, "db", cfg.DBPath)
		errCh <- httpSrv.ListenAndServe()
	}()

	select {
	case err := <-errCh:
		if !errors.Is(err, http.ErrServerClosed) {
			return err
		}
	case <-ctx.Done():
		log.Info("shutting down")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := httpSrv.Shutdown(shutdownCtx); err != nil {
			return fmt.Errorf("shutdown: %w", err)
		}
	}
	return nil
}
