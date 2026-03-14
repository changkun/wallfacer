package gitutil

import (
	"bufio"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

// DiffNumstat returns the total number of added and removed lines between two
// commits in the git repository at repoPath. Binary files (which git reports
// as "-") are skipped. Returns 0 and a non-nil error when either hash is
// empty or the git command fails.
func DiffNumstat(repoPath, base, head string) (int64, error) {
	if base == "" || head == "" {
		return 0, fmt.Errorf("base or head hash is empty")
	}
	out, err := exec.Command("git", "-C", repoPath, "diff", "--numstat", base, head).Output()
	if err != nil {
		return 0, fmt.Errorf("git diff --numstat: %w", err)
	}
	var total int64
	scanner := bufio.NewScanner(strings.NewReader(string(out)))
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 2 {
			continue
		}
		// Binary files are reported as "-\t-\t<path>"; skip them.
		added, errA := strconv.ParseInt(fields[0], 10, 64)
		removed, errR := strconv.ParseInt(fields[1], 10, 64)
		if errA != nil || errR != nil {
			continue
		}
		total += added + removed
	}
	return total, nil
}

// DiffFileCount returns the number of files changed between two commits in the
// git repository at repoPath. Used as a fallback when line counts are zero
// (e.g., when only binary files changed). Returns 0 and a non-nil error when
// either hash is empty or the git command fails.
func DiffFileCount(repoPath, base, head string) (int64, error) {
	if base == "" || head == "" {
		return 0, fmt.Errorf("base or head hash is empty")
	}
	out, err := exec.Command("git", "-C", repoPath, "diff", "--name-only", base, head).Output()
	if err != nil {
		return 0, fmt.Errorf("git diff --name-only: %w", err)
	}
	var count int64
	scanner := bufio.NewScanner(strings.NewReader(string(out)))
	for scanner.Scan() {
		if strings.TrimSpace(scanner.Text()) != "" {
			count++
		}
	}
	return count, nil
}

// WorkspaceWeights computes normalized per-repository weights (summing to 1.0)
// from git diff statistics between pairs of commits. The input maps each
// repository path to a [baseHash, headHash] pair.
//
// Weighting strategy (in priority order):
//  1. Changed-line count (added + removed) from git diff --numstat.
//  2. Changed-file count when all line counts are zero (e.g. binary-only changes).
//  3. Equal split when both strategies yield zero or diffs cannot be computed.
//
// Repositories whose base or head hash is empty are treated as contributing
// zero changes unless the equal-split fallback is triggered.
func WorkspaceWeights(repoHashes map[string][2]string) map[string]float64 {
	if len(repoHashes) == 0 {
		return nil
	}
	if len(repoHashes) == 1 {
		for repo := range repoHashes {
			return map[string]float64{repo: 1.0}
		}
	}

	// Phase 1: line-based weights.
	lineCounts := make(map[string]int64, len(repoHashes))
	var totalLines int64
	for repo, hashes := range repoHashes {
		n, err := DiffNumstat(repo, hashes[0], hashes[1])
		if err == nil {
			lineCounts[repo] = n
			totalLines += n
		}
	}
	if totalLines > 0 {
		weights := make(map[string]float64, len(repoHashes))
		for repo := range repoHashes {
			weights[repo] = float64(lineCounts[repo]) / float64(totalLines)
		}
		return weights
	}

	// Phase 2: file-count fallback.
	fileCounts := make(map[string]int64, len(repoHashes))
	var totalFiles int64
	for repo, hashes := range repoHashes {
		n, err := DiffFileCount(repo, hashes[0], hashes[1])
		if err == nil {
			fileCounts[repo] = n
			totalFiles += n
		}
	}
	if totalFiles > 0 {
		weights := make(map[string]float64, len(repoHashes))
		for repo := range repoHashes {
			weights[repo] = float64(fileCounts[repo]) / float64(totalFiles)
		}
		return weights
	}

	// Phase 3: equal split.
	weights := make(map[string]float64, len(repoHashes))
	eq := 1.0 / float64(len(repoHashes))
	for repo := range repoHashes {
		weights[repo] = eq
	}
	return weights
}
