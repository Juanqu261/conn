package cli

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"unicode"
	"uuid"

	"github.com/spf13/cobra"

	"github.com/Juanqu261/conn/internal/contracts"
	"github.com/Juanqu261/conn/internal/httpclient"
	"github.com/Juanqu261/conn/internal/protocol"
)

// maxInputBytes matches the server's request limit.
const maxInputBytes = 1 << 20

// remote holds the flags shared by commands that call the backend.
type remote struct {
	deps    Dependencies
	streams Streams
	server  func() string
	json    bool
}

func newRemote(cmd *cobra.Command, deps Dependencies, streams Streams) *remote {
	r := &remote{deps: deps, streams: streams, server: serverFlag(cmd, deps)}
	cmd.Flags().BoolVar(&r.json, "json", false, "print the result as JSON on standard output")
	return r
}

// client returns a backend client authenticated with the resolved
// credential.
func (r *remote) client() (*httpclient.Client, error) {
	service, err := r.deps.Account(r.server())
	if err != nil {
		return nil, usageError{err}
	}
	return service.Client()
}

// readInput reads one JSON document from path, or from standard input when
// path is "-", and validates it against T's schema before anything is sent.
// The server validates it again.
func readInput[T protocol.Document](in io.Reader, path string) (T, error) {
	var zero T
	source := in
	if path != "-" {
		file, err := os.Open(path)
		if err != nil {
			return zero, usageError{err}
		}
		defer file.Close()
		source = file
	}
	data, err := io.ReadAll(io.LimitReader(source, maxInputBytes+1))
	if err != nil {
		return zero, usageError{fmt.Errorf("read %s: %w", path, err)}
	}
	if len(data) > maxInputBytes {
		return zero, usageError{fmt.Errorf("%s is larger than 1 MiB", path)}
	}
	doc, err := protocol.Decode[T](data)
	var schemaErr *contracts.ValidationError
	switch {
	case err == nil:
		return doc, nil
	case errors.As(err, &schemaErr) && len(schemaErr.Violations) > 0:
		first := schemaErr.Violations[0]
		location := first.InstancePath
		if location == "" {
			location = "the document root"
		}
		return zero, usageError{fmt.Errorf("%s is invalid at %s: %s", path, location, first.Message)}
	default:
		return zero, usageError{fmt.Errorf("%s is not a valid document: %w", path, err)}
	}
}

// printResult writes {"data": ..., "as_of_event": n} in JSON mode, the same
// envelope the API returns. Otherwise it calls human.
func printResult[T protocol.Document](r *remote, result httpclient.Result[T], human func(io.Writer, T)) error {
	if !r.json {
		human(r.streams.Out, result.Data)
		return nil
	}
	data, err := protocol.Encode(result.Data)
	if err != nil {
		return err
	}
	writeEnvelope(r.streams.Out, data, result.AsOfEvent)
	return nil
}

func writeEnvelope(out io.Writer, data []byte, asOfEvent int64) {
	var body bytes.Buffer
	body.WriteString(`{"data":`)
	body.Write(data)
	fmt.Fprintf(&body, `,"as_of_event":%d}`+"\n", asOfEvent)
	_, _ = out.Write(body.Bytes())
}

// clean makes untrusted text safe to print on a terminal. It keeps line
// breaks and tabs and replaces other control characters, which could
// otherwise move the cursor, recolor, or retitle the terminal.
func clean(s string) string {
	return strings.Map(func(r rune) rune {
		if r == '\n' || r == '\t' || !unicode.IsControl(r) {
			return r
		}
		return '\uFFFD'
	}, s)
}

// indent cleans text and prefixes every line.
func indent(s, prefix string) string {
	lines := strings.Split(strings.TrimRight(clean(s), "\n"), "\n")
	return prefix + strings.Join(lines, "\n"+prefix)
}

// uuidValue is a pflag.Value, so malformed IDs fail as usage errors before
// any request.
type uuidValue struct{ id *uuid.UUID }

func (v uuidValue) String() string {
	if v.id == nil || *v.id == (uuid.UUID{}) {
		return ""
	}
	return v.id.String()
}

func (v uuidValue) Set(s string) error {
	id, err := uuid.Parse(s)
	if err != nil {
		return errors.New("not a UUID")
	}
	*v.id = id
	return nil
}

func (uuidValue) Type() string { return "uuid" }

// requiredUUID adds a required UUID flag.
func requiredUUID(cmd *cobra.Command, target *uuid.UUID, name, usage string) {
	cmd.Flags().Var(uuidValue{target}, name, usage)
	_ = cmd.MarkFlagRequired(name)
}

func requiredInt(cmd *cobra.Command, target *int, name, usage string) {
	cmd.Flags().IntVar(target, name, 0, usage)
	_ = cmd.MarkFlagRequired(name)
}

func requiredString(cmd *cobra.Command, target *string, name, usage string) {
	cmd.Flags().StringVar(target, name, "", usage)
	_ = cmd.MarkFlagRequired(name)
}

// group returns a command that only holds subcommands. Cobra prints help
// for an unknown nested subcommand and succeeds; this reports it instead.
func group(use, short string) *cobra.Command {
	return &cobra.Command{
		Use:   use,
		Short: short,
		Args:  cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return cmd.Help()
			}
			return usageError{fmt.Errorf("unknown command %q for %q", args[0], cmd.CommandPath())}
		},
	}
}

// positive returns a usage error unless n is at least 1.
func positive(name string, n int) error {
	if n < 1 {
		return usageError{fmt.Errorf("--%s must be at least 1", name)}
	}
	return nil
}
