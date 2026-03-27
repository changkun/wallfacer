# Commit and Push

Commit changes and push to the remote repository. Focus on making **one commit per logical change** — if there are multiple independent changes, create separate commits for each. Push once at the end.

## Instructions

1. Run `git status` and `git diff` to see all current changes
2. Run `git log --oneline -5` to see recent commit style
3. Group changes by logical unit (e.g., a bug fix, a feature, a doc update, a refactor). Each group becomes one commit.
4. For each logical group:
   - Stage only the files belonging to that group (use specific file paths, not `git add -A`)
   - Create a commit with a concise message describing that single change
5. After all commits are created, push once to the remote repository
6. Report all commit hashes when done

## Rules

- One commit = one logical change. Do NOT bundle unrelated changes into a single commit.
- Never end the commit message with a Co-Authored-By trailer. Keep only the human author.
- Exclude untracked files that look unrelated to the current work.
