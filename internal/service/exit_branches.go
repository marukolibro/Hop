package service

import (
	"cmp"
	"slices"

	"hop/internal/model"
)

// ExitBranches gathers the groups by where each sends traffic now: one branch
// per exit proxy, or per pick for groups that lead to none, the branch with
// the most groups first and, among equals, the one whose first group comes
// first. Each branch keeps its groups in the profile's order.
func ExitBranches(groups []model.GroupView) []model.ExitBranch {
	var branches []model.ExitBranch
	first := map[string]int{}
	for i, g := range groups {
		key := g.Exit.Name
		if key == "" {
			key = g.Pick
		}
		if key == "" {
			continue
		}
		at, seen := first[key]
		if !seen {
			at = len(branches)
			first[key] = at
			branches = append(branches, model.ExitBranch{Key: key, Exit: g.Exit})
		}
		branches[at].Groups = append(branches[at].Groups, groups[i])
	}
	slices.SortStableFunc(branches, func(a model.ExitBranch, b model.ExitBranch) int {
		return cmp.Compare(len(b.Groups), len(a.Groups))
	})
	return branches
}
