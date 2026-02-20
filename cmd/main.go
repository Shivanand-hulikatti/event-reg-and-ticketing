// cmd/main.go is the application entry point.
// It wires together all layers and starts the HTTP server.
package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/Shivanand-hulikatti/event-reg-and-ticketing/internal/database"
	"github.com/Shivanand-hulikatti/event-reg-and-ticketing/internal/handler"
	"github.com/Shivanand-hulikatti/event-reg-and-ticketing/internal/repository"
	"github.com/Shivanand-hulikatti/event-reg-and-ticketing/internal/service"
	"github.com/go-chi/chi/v5"
	chimiddleware "github.com/go-chi/chi/v5/middleware"
)

func main() {
	ctx := context.Background()

	// ── 1. Connect to PostgreSQL ──────────────────────────────────────────
	pool, err := database.NewPool(ctx)
	if err != nil {
		log.Fatalf("database: %v", err)
	}
	defer pool.Close()
	log.Println("✓ Connected to PostgreSQL")

	// ── 2. Wire up layers ────────────────────────────────────────────────
	eventRepo := repository.NewEventRepository(pool)
	regRepo := repository.NewRegistrationRepository(pool)
	eventSvc := service.NewEventService(eventRepo, regRepo)
	eventHandler := handler.NewEventHandler(eventSvc)

	// ── 3. Build the router ───────────────────────────────────────────────
	r := chi.NewRouter()

	// Global middleware stack
	r.Use(chimiddleware.Recoverer) // recover from panics, return 500
	r.Use(chimiddleware.RequestID) // attach request IDs
	r.Use(chimiddleware.RealIP)    // trust X-Forwarded-For
	r.Use(handler.Logger)          // structured access log
	r.Use(handler.CORS)            // permissive CORS for demo

	// Health
	r.Get("/health", handler.HealthCheck)

	// API routes
	r.Route("/events", func(r chi.Router) {
		r.Post("/", eventHandler.CreateEvent)
		r.Get("/", eventHandler.ListEvents)
		r.Get("/{id}", eventHandler.GetEvent)
		r.Post("/{id}/register", eventHandler.Register)
		r.Get("/{id}/registrations", eventHandler.ListRegistrations)
	})

	// Static HTML – serve the web/ directory at the root.
	// index.html, create_event.html, event_details.html, static/styles.css
	webFS := http.Dir("./web")
	r.Handle("/*", http.FileServer(webFS))

	// ── 4. Start server with graceful shutdown ────────────────────────────
	port := getEnv("PORT", "8080")
	srv := &http.Server{
		Addr:         fmt.Sprintf(":%s", port),
		Handler:      r,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// Run in background goroutine so we can listen for shutdown signal.
	go func() {
		log.Printf("✓ Server listening on http://localhost:%s", port)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("server error: %v", err)
		}
	}()

	// Block until SIGINT or SIGTERM.
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Println("shutting down server…")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Fatalf("graceful shutdown failed: %v", err)
	}
	log.Println("server stopped")
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
