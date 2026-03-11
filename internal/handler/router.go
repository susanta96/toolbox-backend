package handler

import (
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/cors"
	"github.com/susanta96/toolbox-backend/internal/middleware"
)

// NewRouter sets up all HTTP routes and middleware.
func NewRouter(pdfHandler *PDFHandler, maxBodyBytes int64) *chi.Mux {
	r := chi.NewRouter()

	// Global middleware
	r.Use(middleware.Recoverer)
	r.Use(middleware.Logger)
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   []string{"*"},
		AllowedMethods:   []string{"GET", "POST", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Content-Type", "Authorization"},
		ExposedHeaders:   []string{"Content-Disposition"},
		AllowCredentials: false,
		MaxAge:           300,
	}))
	r.Use(middleware.MaxBodySize(maxBodyBytes))

	// Routes
	r.Get("/hello", Hello)

	// API v1
	r.Route("/api/v1", func(api chi.Router) {
		api.Route("/pdf", func(pdf chi.Router) {
			pdf.Post("/lock", pdfHandler.LockPDF)
			pdf.Post("/unlock", pdfHandler.UnlockPDF)
			pdf.Post("/compress", pdfHandler.CompressPDF)
			pdf.Post("/merge", pdfHandler.MergePDF)
			pdf.Post("/split", pdfHandler.SplitPDF)
			pdf.Get("/download/{id}", pdfHandler.Download)
		})
	})

	return r
}
