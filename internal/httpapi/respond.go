package httpapi

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/Juanqu261/conn/internal/application"
	"github.com/Juanqu261/conn/internal/contracts"
	"github.com/Juanqu261/conn/internal/protocol"
)

// maxMessageRunes is the schema limit for error messages.
const maxMessageRunes = 1000

// writeData sends {"data": doc}. protocol.Encode validates doc against its
// schema, so a non-conforming response is never sent.
func writeData[T protocol.Document](w http.ResponseWriter, log *slog.Logger, status int, doc T) {
	data, err := protocol.Encode(doc)
	if err != nil {
		log.Error("response failed schema validation", "error", err)
		writeError(w, log, http.StatusInternalServerError, "internal", "The server could not complete the request.")
		return
	}
	writeEnvelope(w, status, data, nil)
}

// respond sends a project-scoped result as {"data": doc, "as_of_event": n},
// or maps the use-case error.
func respond[T protocol.Document](w http.ResponseWriter, log *slog.Logger, status int, result application.Result[T], err error) {
	if err != nil {
		writeProblem(w, log, err)
		return
	}
	data, err := protocol.Encode(result.Data)
	if err != nil {
		log.Error("response failed schema validation", "error", err)
		writeError(w, log, http.StatusInternalServerError, "internal", "The server could not complete the request.")
		return
	}
	writeEnvelope(w, status, data, &result.AsOfEvent)
}

// respondEvents sends a page of events as a JSON array, validating each.
func respondEvents(w http.ResponseWriter, log *slog.Logger, result application.Result[[]protocol.EventEnvelope], err error) {
	if err != nil {
		writeProblem(w, log, err)
		return
	}
	var data bytes.Buffer
	data.WriteByte('[')
	for i, event := range result.Data {
		encoded, err := protocol.Encode(event)
		if err != nil {
			log.Error("response failed schema validation", "error", err)
			writeError(w, log, http.StatusInternalServerError, "internal", "The server could not complete the request.")
			return
		}
		if i > 0 {
			data.WriteByte(',')
		}
		data.Write(encoded)
	}
	data.WriteByte(']')
	writeEnvelope(w, http.StatusOK, data.Bytes(), &result.AsOfEvent)
}

func writeEnvelope(w http.ResponseWriter, status int, data []byte, asOfEvent *int64) {
	var body bytes.Buffer
	body.WriteString(`{"data":`)
	body.Write(data)
	if asOfEvent != nil {
		body.WriteString(`,"as_of_event":`)
		body.WriteString(strconv.FormatInt(*asOfEvent, 10))
	}
	body.WriteString("}\n")
	writeBody(w, status, body.Bytes())
}

// writeProblem maps a use-case error to its stable status and code. Only
// application-authored messages and the caller's own input reach the
// response; internal errors are logged and replaced.
func writeProblem(w http.ResponseWriter, log *slog.Logger, err error) {
	var invalid *application.InvalidError
	var schemaErr *contracts.ValidationError
	switch {
	case errors.As(err, &invalid):
		writeInvalid(w, log, invalid.Message, invalid.Pointer)
	case errors.As(err, &schemaErr):
		writeSchemaError(w, log, schemaErr)
	case errors.Is(err, application.ErrInvalid):
		writeInvalid(w, log, "The request is invalid.", "")
	case errors.Is(err, application.ErrNotFound):
		writeNotFound(w, log)
	case errors.Is(err, application.ErrForbidden):
		writeError(w, log, http.StatusForbidden, "forbidden", "Your role in this project does not permit this operation.")
	case errors.Is(err, application.ErrConflict):
		writeError(w, log, http.StatusConflict, "id_conflict", "This ID is already used by a different record. Generate a new ID.")
	case errors.Is(err, application.ErrStaleRevision):
		writeError(w, log, http.StatusConflict, "stale_revision", "The revision is no longer current. Read the latest revision and try again.")
	case errors.Is(err, application.ErrCharterDecided):
		writeError(w, log, http.StatusConflict, "charter_decided", "This charter already has a different decision. Changes need a new blueprint revision.")
	default:
		internalError(w, log, "request", err)
	}
}

func writeSchemaError(w http.ResponseWriter, log *slog.Logger, err *contracts.ValidationError) {
	message, pointer := "The request document does not match its schema.", ""
	if len(err.Violations) > 0 {
		first := err.Violations[0]
		pointer = first.InstancePath
		location := pointer
		if location == "" {
			location = "the document root"
		}
		message = fmt.Sprintf("The request document is invalid at %s: %s", location, first.Message)
	}
	writeInvalid(w, log, message, pointer)
}

func writeInvalid(w http.ResponseWriter, log *slog.Logger, message, pointer string) {
	writeErrorDetail(w, log, http.StatusUnprocessableEntity, protocol.ErrorDetail{
		Code:    "invalid_input",
		Message: truncate(message, maxMessageRunes),
		Pointer: pointer,
	})
}

// writeError sends a stable code and a safe message.
func writeError(w http.ResponseWriter, log *slog.Logger, status int, code, message string) {
	writeErrorDetail(w, log, status, protocol.ErrorDetail{Code: code, Message: message})
}

func writeErrorDetail(w http.ResponseWriter, log *slog.Logger, status int, detail protocol.ErrorDetail) {
	data, err := protocol.Encode(protocol.Error{Error: detail})
	if err != nil {
		log.Error("error response failed schema validation", "code", detail.Code, "error", err)
		status = http.StatusInternalServerError
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

func truncate(s string, maxRunes int) string {
	runes := []rune(s)
	if len(runes) <= maxRunes {
		return s
	}
	return string(runes[:maxRunes-1]) + "…"
}
