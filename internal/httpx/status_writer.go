package httpx

import "net/http"

type StatusWriter struct {
	http.ResponseWriter
	Status int
	Bytes  int
}

func (w *StatusWriter) WriteHeader(code int) {
	w.Status = code
	w.ResponseWriter.WriteHeader(code)
}

func (w *StatusWriter) Write(p []byte) (int, error) {
	if w.Status == 0 {
		w.Status = http.StatusOK
	}
	n, err := w.ResponseWriter.Write(p)
	w.Bytes += n
	return n, err
}
