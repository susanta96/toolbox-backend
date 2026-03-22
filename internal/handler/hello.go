package handler

import (
	"net/http"
	"runtime"
	"sort"
	"strings"

	"github.com/susanta96/toolbox-backend/pkg/response"
)

type routeMeta struct {
	Method string `json:"method"`
	Path   string `json:"path"`
}

var routeProvider = func() []routeMeta { return nil }

func setRouteProvider(provider func() []routeMeta) {
	if provider == nil {
		return
	}
	routeProvider = provider
}

func getExposedRoutes() []routeMeta {
	routes := routeProvider()
	if len(routes) == 0 {
		return []routeMeta{{Method: http.MethodGet, Path: "/hello"}}
	}

	sorted := make([]routeMeta, len(routes))
	copy(sorted, routes)
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].Path == sorted[j].Path {
			return sorted[i].Method < sorted[j].Method
		}
		return sorted[i].Path < sorted[j].Path
	})

	return sorted
}

// Hello is a health/demo endpoint.
func Hello(w http.ResponseWriter, r *http.Request) {
	exposedRoutes := getExposedRoutes()
	query := r.URL.Query()
	routesFlag := strings.EqualFold(query.Get("routes"), "true") || query.Get("routes") == "1"
	routeParam := strings.TrimSpace(query.Get("route"))
	methodParam := strings.ToUpper(strings.TrimSpace(query.Get("method")))

	if routeParam != "" {
		route := normalizeRoute(routeParam)
		matches := make([]routeMeta, 0)
		availableMethods := make([]string, 0)
		exposed := false

		for _, rt := range exposedRoutes {
			if rt.Path != route {
				continue
			}
			availableMethods = append(availableMethods, rt.Method)
			if methodParam == "" || methodParam == rt.Method {
				exposed = true
				matches = append(matches, rt)
			}
		}

		response.Success(w, http.StatusOK, "Route lookup", map[string]any{
			"version":           "1.0.0",
			"go_version":        runtime.Version(),
			"status":            "healthy",
			"route":             route,
			"method":            methodParam,
			"exposed":           exposed,
			"available_methods": availableMethods,
			"matches":           matches,
		})
		return
	}

	if routesFlag {
		response.Success(w, http.StatusOK, "Exposed routes", map[string]any{
			"version":      "1.0.0",
			"go_version":   runtime.Version(),
			"status":       "healthy",
			"total_routes": len(exposedRoutes),
			"routes":       exposedRoutes,
		})
		return
	}

	response.Success(w, http.StatusOK, "Welcome to Toolbox Backend API! 🧰", map[string]any{
		"version":    "1.0.0",
		"go_version": runtime.Version(),
		"status":     "healthy",
		"usage": map[string]string{
			"all_routes":  "/hello?routes=true",
			"check_route": "/hello?route=/api/v1/currency/convert",
			"check_method": "/hello?route=/api/v1/currency/convert&method=GET",
		},
	})
}

func normalizeRoute(route string) string {
	if route == "" {
		return route
	}
	if strings.HasPrefix(route, "/") {
		return route
	}
	return "/" + route
}
