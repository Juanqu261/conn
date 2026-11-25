package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"math"
	"mime"
	"net/http"
	"strconv"
	"uuid"

	"github.com/Juanqu261/conn/internal/contracts"
	"github.com/Juanqu261/conn/internal/protocol"
)

// maxRequestBytes bounds every request body. The largest document, a
// BlueprintProposal with 32 maximal charters, stays well below it in
// practice; raising the limit later is compatible.
const maxRequestBytes = 1 << 20

// readDocument reads a bounded JSON body and decodes it into T after schema
// validation. On failure it writes the error response and reports false.
func readDocument[T protocol.Document](w http.ResponseWriter, r *http.Request, log *slog.Logger) (T, bool) {
	var zero T
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		writeError(w, log, http.StatusUnsupportedMediaType, "unsupported_media_type", "Send the request body as application/json.")
		return zero, false
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxRequestBytes))
	var tooLarge *http.MaxBytesError
	if errors.As(err, &tooLarge) {
		writeError(w, log, http.StatusRequestEntityTooLarge, "request_too_large", "The request body exceeds 1 MiB.")
		return zero, false
	}
	if err != nil {
		writeError(w, log, http.StatusBadRequest, "invalid_json", "The request body could not be read.")
		return zero, false
	}
	doc, err := protocol.Decode[T](body)
	var schemaErr *contracts.ValidationError
	switch {
	case err == nil:
		return doc, true
	case errors.As(err, &schemaErr):
		writeSchemaError(w, log, schemaErr)
	case !json.Valid(body):
		writeError(w, log, http.StatusBadRequest, "invalid_json", "The request body is not one valid JSON document.")
	default:
		writeInvalid(w, log, "The request document is invalid.", "")
	}
	return zero, false
}

// pathID parses a UUID path segment. Malformed IDs cannot name any
// resource, so they answer like a missing one.
func pathID(w http.ResponseWriter, r *http.Request, log *slog.Logger, name string) (uuid.UUID, bool) {
	id, err := uuid.Parse(r.PathValue(name))
	if err != nil {
		writeNotFound(w, log)
		return uuid.UUID{}, false
	}
	return id, true
}

func queryOptionalID(w http.ResponseWriter, r *http.Request, log *slog.Logger, name string) (uuid.UUID, bool) {
	values, present := r.URL.Query()[name]
	if !present || len(values) == 0 || values[0] == "" {
		return uuid.UUID{}, true
	}
	if len(values) != 1 {
		writeInvalid(w, log, "The "+name+" query parameter may be supplied only once.", "")
		return uuid.UUID{}, false
	}
	id, err := uuid.Parse(values[0])
	if err != nil {
		writeInvalid(w, log, "The "+name+" query parameter must be a UUID.", "")
		return uuid.UUID{}, false
	}
	return id, true
}

func allowQueryParameters(w http.ResponseWriter, r *http.Request, log *slog.Logger, allowed ...string) bool {
	set := make(map[string]bool, len(allowed))
	for _, name := range allowed {
		set[name] = true
	}
	for name, values := range r.URL.Query() {
		if !set[name] {
			writeInvalid(w, log, "Unknown query parameter: "+name+".", "")
			return false
		}
		if len(values) > 1 {
			writeInvalid(w, log, "The "+name+" query parameter may be supplied only once.", "")
			return false
		}
	}
	return true
}

// pathRevision parses a positive revision path segment.
func pathRevision(w http.ResponseWriter, r *http.Request, log *slog.Logger, name string) (int, bool) {
	n, err := strconv.Atoi(r.PathValue(name))
	if err != nil || n < 1 || n > math.MaxInt32 {
		writeNotFound(w, log)
		return 0, false
	}
	return n, true
}

// queryInt parses an optional integer query parameter within [minimum,
// maximum]. A missing parameter yields fallback, or an error when
// fallback is nil.
func queryInt(w http.ResponseWriter, r *http.Request, log *slog.Logger, name string, minimum, maximum int64, fallback *int64) (int64, bool) {
	raw := r.URL.Query().Get(name)
	if raw == "" && fallback != nil {
		return *fallback, true
	}
	n, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || n < minimum || n > maximum {
		writeInvalid(w, log, "The "+name+" query parameter must be an integer from "+
			strconv.FormatInt(minimum, 10)+" to "+strconv.FormatInt(maximum, 10)+".", "")
		return 0, false
	}
	return n, true
}

func writeNotFound(w http.ResponseWriter, log *slog.Logger) {
	writeError(w, log, http.StatusNotFound, "not_found", "No such resource exists in a project you belong to.")
}
