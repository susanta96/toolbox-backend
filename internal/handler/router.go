package handler

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/cors"
	"github.com/go-chi/httprate"
	"github.com/susanta96/toolbox-backend/internal/middleware"
)

// NewRouter sets up all HTTP routes and middleware.
func NewRouter(pdfHandler *PDFHandler, currencyHandler *CurrencyHandler, maxBodyBytes int64, rateLimitRPM int, rateLimitWindow time.Duration) *chi.Mux {
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

	// Rate limiting — per-IP sliding window
	r.Use(httprate.LimitByIP(rateLimitRPM, rateLimitWindow))

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

		api.Route("/currency", func(curr chi.Router) {
			curr.Get("/supported", currencyHandler.Supported)
			curr.Get("/convert", currencyHandler.Convert)
			curr.Get("/historical", currencyHandler.Historical)
		})
	})

	setRouteProvider(func() []routeMeta {
		return collectRouteMeta(r)
	})

	return r
}

func collectRouteMeta(r chi.Routes) []routeMeta {
	if r == nil {
		return nil
	}

	routes := make([]routeMeta, 0)
	seen := make(map[string]struct{})

	_ = chi.Walk(r, func(method string, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		key := method + " " + route
		if _, exists := seen[key]; exists {
			return nil
		}
		seen[key] = struct{}{}
		routes = append(routes, routeMeta{Method: method, Path: route})
		return nil
	})

	return routes
}
