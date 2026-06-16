package cli

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"latere.ai/x/wallfacer/internal/pkg/sanitize"
)

// taskUsage mirrors the JSON shape of store.TaskUsage (cost field only).
type taskUsage struct {
	CostUSD float64 `json:"cost_usd"`
}

// taskSummary mirrors a minimal subset of the store.Task JSON representation.
type taskSummary struct {
	ID     string    `json:"id"`
	Title  string    `json:"title"`
	Prompt string    `json:"prompt"`
	Status string    `json:"status"`
	Turns  int       `json:"turns"`
	Usage  taskUsage `json:"usage"`
	Tags   []string  `json:"tags"`
}

// ANSI escape sequences for terminal formatting.
const (
	ansiReset = "\033[0m"
	ansiBold  = "\033[1m"
)

// statusColors maps status names to ANSI foreground color codes.
var statusColors = map[string]string{
	"backlog":     "\033[37m", // white
	"in_progress": "\033[34m", // blue
	"waiting":     "\033[33m", // yellow
	"committing":  "\033[36m", // cyan
	"done":        "\033[32m", // green
	"failed":      "\033[31m", // red
	"cancelled":   "\033[90m", // dark gray
}

// statusOrder controls the top-to-bottom display order of sections.
var statusOrder = []string{
	"in_progress",
	"waiting",
	"committing",
	"backlog",
	"failed",
	"done",
	"cancelled",
}

// statusLabel returns a human-readable heading for a status value.
func statusLabel(s string) string {
	labels := map[string]string{
		"backlog":     "Backlog",
		"in_progress": "In Progress",
		"waiting":     "Waiting",
		"committing":  "Committing",
		"done":        "Done",
		"failed":      "Failed",
		"cancelled":   "Cancelled",
	}
	if l, ok := labels[s]; ok {
		return l
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

// groupByStatus groups tasks by their Status field.
func groupByStatus(tasks []taskSummary) map[string][]taskSummary {
	groups := make(map[string][]taskSummary)
	for _, t := range tasks {
		groups[t.Status] = append(groups[t.Status], t)
	}
	return groups
}

// formatCost formats a USD cost as a dollar string with 4 decimal places.
func formatCost(usd float64) string {
	return fmt.Sprintf("$%.4f", usd)
}

// truncate is a package-level alias for sanitize.Truncate.
func truncate(s string, n int) string {
	return sanitize.Truncate(s, n)
}

// fetchTasks calls GET /api/tasks and returns the decoded slice.
func fetchTasks(addr string) ([]taskSummary, error) {
	resp, err := http.Get(addr + "/api/tasks?include_archived=false")
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = resp.Body.Close()
	}()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	var tasks []taskSummary
	if err := json.Unmarshal(body, &tasks); err != nil {
		return nil, err
	}
	return tasks, nil
}

// printBoard renders the formatted board to stdout.
func printBoard(addr string, tasks []taskSummary) {
	fmt.Printf("%sWallfacer%s  %s   %s\n\n",
		ansiBold, ansiReset,
		addr,
		time.Now().Format("2006-01-02 15:04:05"),
	)

	groups := groupByStatus(tasks)

	for _, status := range statusOrder {
		group, ok := groups[status]
		if !ok || len(group) == 0 {
			continue
		}
		color := statusColors[status]
		fmt.Printf("%s%s%s%s (%d)\n", ansiBold, color, statusLabel(status), ansiReset, len(group))

		for _, t := range group {
			display := t.Title
			if display == "" {
				display = t.Prompt
			}
			display = truncate(display, 55)

			idShort := t.ID
			if len(idShort) > 8 {
				idShort = idShort[:8]
			}

			fmt.Printf("  %s  %-56s  turns=%-3d  %s\n",
				idShort,
				display,
				t.Turns,
				formatCost(t.Usage.CostUSD),
			)
		}
		fmt.Println()
	}

	var totalCost float64
	for _, t := range tasks {
		totalCost += t.Usage.CostUSD
	}
	fmt.Printf("Total: %d tasks   Aggregate cost: %s\n", len(tasks), formatCost(totalCost))
}

// RunStatus implements the `wallfacer status` subcommand.
func RunStatus(_ string, args []string) {
	fs := flag.NewFlagSet("status", flag.ExitOnError)
	defaultAddr := envOrDefault("ADDR", "http://localhost:8080")
	addr := fs.String("addr", defaultAddr, "wallfacer server address (or ADDR env var)")
	watch := fs.Bool("watch", false, "re-render every 2 seconds until Ctrl-C")
	jsonOut := fs.Bool("json", false, "emit raw JSON from /api/tasks for scripting")
	_ = fs.Parse(args)

	serverAddr := strings.TrimRight(*addr, "/")

	if *jsonOut {
		resp, err := http.Get(serverAddr + "/api/tasks?include_archived=false")
		if err != nil {
			fmt.Fprintf(os.Stderr, "wallfacer: server not reachable at %s\n", serverAddr)
			os.Exit(1)
		}
		body, err := io.ReadAll(resp.Body)
		_ = resp.Body.Close()

		if err != nil {
			fmt.Fprintf(os.Stderr, "wallfacer: read response: %v\n", err)
			os.Exit(1)
		}
		fmt.Println(string(body))
		return
	}

	render := func() bool {
		tasks, err := fetchTasks(serverAddr)
		if err != nil {
			fmt.Fprintf(os.Stderr, "wallfacer: server not reachable at %s\n", serverAddr)
			return false
		}
		printBoard(serverAddr, tasks)
		return true
	}

	if !*watch {
		if !render() {
			os.Exit(1)
		}
		return
	}

	// Watch mode: clear screen and redraw every 2 seconds until Ctrl-C.
	// Uses ANSI escape codes rather than ncurses for zero-dependency output.
	for {
		fmt.Print("\033[H\033[2J") // ANSI: move cursor to home + clear entire screen
		render()
		time.Sleep(2 * time.Second)
	}
}
