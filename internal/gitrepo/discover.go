package gitrepo

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/snaylaker/lw/internal/domain"
)

// Discover lists the git checkouts directly inside each root, one level deep.
//
// One level is the whole rule. A recursive walk of a home directory is slow,
// surprising, and finds vendored checkouts nobody wants to work in; people keep
// repositories in a handful of flat directories, so that is what this reads. A
// root that does not exist is skipped rather than reported: a stale entry in
// config.json must not stop the picker opening.
//
// Results are deduplicated by path and sorted by name, then path, so the picker
// order does not depend on directory iteration order.
func Discover(ctx context.Context, roots []string, run Runner) []domain.Repo {
	seen := map[string]bool{}
	var found []domain.Repo

	for _, root := range roots {
		root = strings.TrimSpace(root)
		if root == "" {
			continue
		}
		entries, err := os.ReadDir(root)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			path := filepath.Join(root, entry.Name())
			if !isCheckout(path) {
				continue
			}
			// Resolve validates the checkout and maps linked worktrees back to
			// their main checkout. The picker only deals in validated Repo values.
			repo, err := Resolve(ctx, path, run)
			if err != nil || seen[repo.Root] {
				continue
			}
			seen[repo.Root] = true
			found = append(found, repo)
		}
	}

	sort.Slice(found, func(i, j int) bool {
		if found[i].Name != found[j].Name {
			return found[i].Name < found[j].Name
		}
		return found[i].Root < found[j].Root
	})
	return found
}

// isCheckout is true for a directory holding a .git entry. A linked worktree
// carries a .git *file* rather than a directory, and counts: it is still a
// checkout somebody might pick, and Validate resolves it to its main repository
// afterwards.
func isCheckout(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, ".git"))
	return err == nil
}
