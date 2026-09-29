package httpapi

import (
	"net/http"

	"github.com/sky-valley/pagelike/internal/budget"
)

// budgetWriter reports the request budget's consumption in the
// X-Budget-Consumed-* headers when the response head is written.
type budgetWriter struct {
	http.ResponseWriter
	b     *budget.Request
	wrote bool
}

func (w *budgetWriter) WriteHeader(code int) {
	if !w.wrote {
		w.wrote = true
		w.b.SetHeaders(w.Header())
	}
	w.ResponseWriter.WriteHeader(code)
}

func (w *budgetWriter) Write(p []byte) (int, error) {
	if !w.wrote {
		w.WriteHeader(http.StatusOK)
	}
	return w.ResponseWriter.Write(p)
}

// Flush supports streaming responses (Server-Sent Events).
func (w *budgetWriter) Flush() {
	if !w.wrote {
		w.WriteHeader(http.StatusOK)
	}
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// Unwrap lets http.ResponseController reach the underlying writer.
func (w *budgetWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }
