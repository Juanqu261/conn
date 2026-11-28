package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Juanqu261/conn/internal/account"
	"github.com/Juanqu261/conn/internal/githook"
	"github.com/Juanqu261/conn/internal/kirointegration"
	"github.com/Juanqu261/conn/internal/kiropower"
)

// kiroChecks report, read-only, what the Conn Kiro power needs around it: a
// conn version the power supports, manual MCP registrations that would start
// a second server beside the power's, the always-on workspace bundle, and
// the commit-trailer Git hook. All are optional.
func kiroChecks(ctx context.Context, deps Dependencies) []account.Check {
	checks := []account.Check{powerVersionCheck(deps.Version)}
	checks = append(checks, registrationChecks()...)
	return append(checks, integrationCheck(), gitHookCheck(ctx))
}

func powerVersionCheck(version string) account.Check {
	check := account.Check{Name: "Kiro power", Optional: true, OK: true}
	minimum := kiropower.MinimumConnVersion
	switch kiropower.CheckVersion(version) {
	case kiropower.VersionSupported:
		check.Detail = fmt.Sprintf("conn %s meets the power's minimum %s", version, minimum)
	case kiropower.VersionSourceDev:
		check.Detail = fmt.Sprintf("source build without a version stamp; the power requires %s or later", minimum)
	case kiropower.VersionTooOld:
		check.OK, check.Detail = false, fmt.Sprintf("conn %s is older than the power's minimum %s; rebuild conn from source", version, minimum)
	default:
		check.OK, check.Detail = false, fmt.Sprintf("conn version %q is not a semantic version; the power requires %s or later", version, minimum)
	}
	return check
}

func registrationChecks() []account.Check {
	working, _ := os.Getwd()
	home, _ := os.UserHomeDir()
	found, err := kiropower.Registrations(working, home)
	checks := make([]account.Check, 0, len(found)+1)
	for _, registration := range found {
		state := ""
		if registration.Disabled {
			state = " (disabled)"
		}
		checks = append(checks, account.Check{Name: "MCP registration", Optional: true, OK: true, Detail: fmt.Sprintf(
			"%s entry %q in %s runs conn mcp%s. With the Conn power installed it starts a second server; keep one, removing this one with kiro-cli mcp remove",
			registration.Scope, registration.Name, filepath.ToSlash(registration.Path), state)})
	}
	if err != nil {
		checks = append(checks, account.Check{Name: "MCP registration", Optional: true, Detail: "could not read Kiro MCP configuration: " + err.Error()})
	} else if len(found) == 0 {
		checks = append(checks, account.Check{Name: "MCP registration", Optional: true, OK: true, Detail: "none in workspace or user Kiro MCP configuration"})
	}
	return checks
}

// integrationCheck reports the always-on steering and command hooks, which
// keep pre-edit warnings on while the power is inactive.
func integrationCheck() account.Check {
	check := account.Check{Name: "Kiro integration", Optional: true}
	for _, scope := range []kirointegration.Scope{kirointegration.ScopeWorkspace, kirointegration.ScopeUser} {
		plan, err := kirointegration.Preview(scope, "", kirointegration.ModeRemove, "")
		if err != nil {
			continue
		}
		installed, modified := 0, []string{}
		for _, change := range plan.Changes {
			switch change.Action {
			case kirointegration.ActionRemove:
				if filepath.Clean(change.RelativePath) != filepath.Clean(kirointegration.LegacyHooksTarget) {
					installed++
				}
			case kirointegration.ActionConflict:
				modified = append(modified, filepath.ToSlash(change.RelativePath))
			}
		}
		switch {
		case len(modified) > 0:
			check.Detail = fmt.Sprintf("%s files differ from Conn's: %s; inspect them before reinstalling", scope, strings.Join(modified, ", "))
			return check
		case installed == len(plan.Artifacts):
			check.OK, check.Detail = true, fmt.Sprintf("steering and warn-only hooks installed for the %s", scope)
			return check
		}
	}
	check.Detail = "not installed; pre-edit warnings need it while the Conn power is inactive: conn kiro integration preview --scope workspace --root . --command-on-path"
	return check
}

func gitHookCheck(ctx context.Context) account.Check {
	check := account.Check{Name: "Git hook", Optional: true}
	plan, err := githook.Preview(ctx, ".", githook.ModeRemove, githook.PathCommand)
	switch {
	case err != nil:
		check.OK, check.Detail = true, "not in a Git worktree"
	case plan.Action == githook.ActionRemove:
		check.OK, check.Detail = true, "commit-msg hook adds Conn-Intent trailers"
	case plan.Action == githook.ActionConflict:
		check.Detail = "a different commit-msg hook is installed; add the trailer line from conn git-hook preview by hand"
	default:
		check.Detail = "not installed; commits need --trailer, or run conn git-hook preview"
	}
	return check
}
