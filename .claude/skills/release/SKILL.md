---
name: release
description: Create a new wallfacer release — generates release notes, commits, tags, pushes, and creates a GitHub release with pre-built binaries. Usage /release <version> e.g. /release v0.0.7
argument-hint: "<version> e.g. v0.0.7 or v0.0.7-beta"
user-invocable: true
disable-model-invocation: true
---

# Create a Wallfacer Release

You are creating a release for wallfacer version `$ARGUMENTS`.

## Pre-flight checks

1. Verify the version argument is provided and starts with `v` (e.g. `v0.0.7`, `v0.0.7-beta`)
2. Verify the working tree is clean (`git status`)
3. Verify the tag does not already exist (`git tag -l $ARGUMENTS`)
4. Identify the previous release tag (`git describe --tags --abbrev=0`)

## Step 1: Gather release data

Run these in parallel:
- `git log <prev-tag>..HEAD --oneline` — full commit list
- `git diff <prev-tag>..HEAD --stat | tail -1` — diffstat summary
- `git log <prev-tag>..HEAD --oneline | wc -l` — commit count
- Read the previous release notes from `docs/releases/<prev-tag>.md` for style reference

## Step 2: Write release notes

Create `docs/releases/<version>.md` following the established style:

1. Start with `## <emoji> <version> — The "..." Release` (a punchy, memorable subtitle)
2. Include release metadata block (date, previous release, range, delta)
3. Write a 1-2 sentence hook capturing why this release matters
4. Group changes into 3-6 themed sections with emoji headers
5. Each section: catchy title, 3-5 bullet points explaining what changed and why it matters
6. Include an **Upgrading** section with install instructions:
   - One-liner: `curl -fsSL https://raw.githubusercontent.com/changkun/wallfacer/main/install.sh | sh`
   - Direct binary download note (binaries auto-attached by CI)
7. Close with a bold summary line (commit count, lines changed, tagline)

Style guidelines:
- Be exciting, energetic, and inspiring — but grounded in what actually shipped
- Focus on user-facing impact, not internal refactoring details
- Use **bold** for emphasis on key features
- Keep sections scannable with bullet points
- Reference the previous release notes for tone and structure

## Step 3: Show the user the release notes and ask for confirmation

Present the release notes to the user and ask them to review before proceeding. Wait for explicit approval.

## Step 4: Commit, tag, push, and create release

After user approval:

1. Run `make fmt` and `make lint` — fix any issues
2. `git add docs/releases/<version>.md` (and any other changed files like install.sh)
3. Commit with message: `docs: add <version> release notes`
4. `git tag -a "<version>" -m "<version>"`
5. `git push origin main "<version>"`
6. Create GitHub release:
   - For beta/rc versions: `gh release create "<version>" --title "<version>" --notes-file docs/releases/<version>.md --prerelease`
   - For stable versions: `gh release create "<version>" --title "<version>" --notes-file docs/releases/<version>.md`

The CI workflow (`release-binary.yml`) automatically builds and attaches binaries for linux/amd64, linux/arm64, darwin/amd64, darwin/arm64, and windows/amd64.

## Step 5: Verify

- Print the release URL
- Remind the user that CI is building binaries (they can check with `gh run list`)
- Confirm the one-liner install will pick up this version
