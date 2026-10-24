package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// Logger wraps the ResponseWriter; http.ResponseController must still be able
// to reach the underlying connection to extend deadlines.
func TestLogger_ResponseControllerReachesConnection(t *testing.T) {
	var readErr, writeErr error
	srv := httptest.NewServer(Logger(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rc := http.NewResponseController(w)
		readErr = rc.SetReadDeadline(time.Now().Add(time.Minute))
		writeErr = rc.SetWriteDeadline(time.Now().Add(time.Minute))
	})))
	defer srv.Close()

	resp, err := http.Get(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()

	if readErr != nil {
		t.Errorf("SetReadDeadline: %v", readErr)
	}
	if writeErr != nil {
		t.Errorf("SetWriteDeadline: %v", writeErr)
	}
}
