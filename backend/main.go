package main

import (
	"log"
	"net/http"
	"os"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
	"github.com/medium-harvester/backend/internal/server"
)

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	outputDir := os.Getenv("OUTPUT_DIR")
	if outputDir == "" {
		outputDir = "./output"
	}

	if err := os.MkdirAll(outputDir+"/main", 0755); err != nil {
		log.Fatalf("failed to create output/main dir: %v", err)
	}
	if err := os.MkdirAll(outputDir+"/pages", 0755); err != nil {
		log.Fatalf("failed to create output/pages dir: %v", err)
	}

	r := chi.NewRouter()
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   []string{"*"},
		AllowedMethods:   []string{"GET", "POST", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Content-Type"},
		AllowCredentials: false,
	}))

	h := server.NewHandler(outputDir)
	r.Post("/api/harvest", h.Harvest)
	r.Post("/api/jobs/{jobID}/stop", h.StopJob)
	r.Get("/api/jobs/{jobID}", h.GetJob)
	r.Get("/api/jobs/{jobID}/stream", h.StreamJob)
	r.Get("/api/files/{jobID}/*", h.ServeFile)
	r.Post("/api/cookie-session", h.CookieSessionStart)
	r.Get("/api/cookie-session/{token}", h.CookieSessionPoll)
	r.Post("/api/cookie-session/{token}/submit", h.CookieSessionSubmit)
	r.Get("/api/cookie-helper", h.CookieHelper)

	log.Printf("🚀 Backend listening on :%s", port)
	log.Fatal(http.ListenAndServe(":"+port, r))
}
