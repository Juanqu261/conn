package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"

	"github.com/Juanqu261/conn/internal/account"
	"github.com/Juanqu261/conn/internal/repositorygit"
	"github.com/Juanqu261/conn/internal/speckit"
	"github.com/Juanqu261/conn/internal/speckitext"
)

var installedVersion = regexp.MustCompile(`(?m)^\s+version:\s*"?([0-9][^"\s]*)"?\s*$`)

// specKitCheck reports, read-only, whether the repository uses Spec Kit and
// whether the Conn extension is installed at this conn's version. It never
// runs Spec Kit and never writes .specify/.
func specKitCheck(ctx context.Context) account.Check {
	check := account.Check{Name: "Spec Kit", Optional: true, OK: true}
	root, err := repositorygit.TopLevel(ctx, ".")
	if err != nil {
		check.Detail = "not in a Git worktree"
		return check
	}
	root = filepath.FromSlash(root)
	if info, err := os.Stat(filepath.Join(root, ".specify")); err != nil || !info.IsDir() {
		check.Detail = "no Spec Kit project in this repository"
		return check
	}
	export := fmt.Sprintf("conn speckit extension export <dir>, then %s <dir>", speckitext.InstallCommand)
	manifest, err := speckit.ReadFile(filepath.Join(root, ".specify", "extensions", speckitext.ID, "extension.yml"))
	if err != nil {
		check.OK, check.Detail = false, "Spec Kit project without the Conn extension; plan-time collision checks need it: "+export
		return check
	}
	version := ""
	if match := installedVersion.FindSubmatch(manifest); match != nil {
		version = string(match[1])
	}
	if version != speckitext.Version {
		check.OK, check.Detail = false, fmt.Sprintf("the Conn extension %q is installed; this conn renders %s. Update it: %s", version, speckitext.Version, export)
		return check
	}
	check.Detail = fmt.Sprintf("Conn extension %s installed; after_tasks and before_implement call Conn", version)
	if _, err := os.Stat(filepath.Join(root, ".kiro", "prompts")); err == nil {
		check.Detail += "; with the kiro-cli integration its commands appear in Kiro as /speckit.conn.check and /speckit.conn.start beside /conn-start"
	}
	return check
}
