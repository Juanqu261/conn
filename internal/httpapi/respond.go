package httpapi

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/Juanqu261/conn/internal/protocol"
)

// writeData sends {"data": doc}. protocol.Encode validates doc against its
// schema, so a non-conforming response is never sent.
func writeData[T protocol.Document](w http.ResponseWriter, log *slog.Logger, status int, doc T) {
	data, err := protocol.Encode(doc)
	if err != nil {
		log.Error("response failed schema validation", "error", err)
		writeError(w, log, http.StatusInternalServerError, "internal", "The server could not complete the request.")
		return
	}
	var body bytes.Buffer
	body.WriteString(`{"data":`)
	body.Write(data)
	body.WriteString("}\n")
	writeBody(w, status, body.Bytes())
}

// writeError sends a stable code and a safe message.
func writeError(w http.ResponseWriter, log *slog.Logger, status int, code, message string) {
	data, err := protocol.Encode(protocol.Error{Error: protocol.ErrorDetail{Code: code, Message: message}})
	if err != nil {
		log.Error("error response failed schema validation", "code", code, "error", err)
		data, _ = json.Marshal(protocol.Error{Error: protocol.ErrorDetail{Code: "internal", Message: "The server could not complete the request."}})
	}
	writeBody(w, status, append(data, '\n'))
}

func writeBody(w http.ResponseWriter, status int, body []byte) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_, _ = w.Write(body)
}
