//go:build desktop

package cli

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"changkun.de/x/wallfacer/assets/icons"
	"changkun.de/x/wallfacer/internal/logger"

	"fyne.io/systray"
)

// trayPollInterval is the interval between health endpoint polls.
const trayPollInterval = 5 * time.Second

// TrayManager manages the system tray icon and menu for the desktop app.
type TrayManager struct {
	showWindow func()
	quit       func()
	serverURL  string
	apiKey     string
	httpClient *http.Client
	stopLoop   func() // returned by systray.RunWithExternalLoop
	done       chan struct{}

	// Menu items updated by poll.
	mInProgress *systray.MenuItem
	mWaiting    *systray.MenuItem
	mBacklog    *systray.MenuItem
	mUptime     *systray.MenuItem

	// Last known icon state to avoid redundant SetIcon calls.
	lastIconState string
}

// NewTrayManager creates a TrayManager. showWindow is called when the user
// clicks "Open Dashboard"; quit is called when the user clicks "Quit".
// serverURL is the base URL of the running HTTP server (e.g., "http://localhost:8080").
// apiKey is the optional WALLFACER_SERVER_API_KEY for authentication.
func NewTrayManager(showWindow, quit func(), serverURL, apiKey string) *TrayManager {
	return &TrayManager{
		showWindow: showWindow,
		quit:       quit,
		serverURL:  serverURL,
		apiKey:     apiKey,
		httpClient: &http.Client{Timeout: 3 * time.Second},
		done:       make(chan struct{}),
	}
}

// Start initializes the system tray icon and menu, then starts the poll loop.
// It must be called from the main goroutine on macOS.
func (tm *TrayManager) Start() {
	start, end := systray.RunWithExternalLoop(tm.onReady, func() {
		logger.Main.Info("system tray exited")
	})
	tm.stopLoop = end
	start()
}

// Stop tears down the system tray and stops the poll loop.
func (tm *TrayManager) Stop() {
	close(tm.done)
	systray.Quit()
	if tm.stopLoop != nil {
		tm.stopLoop()
	}
}

// onReady is called by systray when the tray is ready to receive menu items.
func (tm *TrayManager) onReady() {
	systray.SetTemplateIcon(icons.Tray, icons.Tray)
	systray.SetTooltip("Wallfacer")

	mOpen := systray.AddMenuItem("Open Dashboard", "Show the Wallfacer window")
	systray.AddSeparator()

	tm.mInProgress = systray.AddMenuItem("● 0 In Progress", "")
	tm.mInProgress.Disable()
	tm.mWaiting = systray.AddMenuItem("  0 Waiting", "")
	tm.mWaiting.Disable()
	tm.mBacklog = systray.AddMenuItem("  0 Backlog", "")
	tm.mBacklog.Disable()
	systray.AddSeparator()

	tm.mUptime = systray.AddMenuItem("Uptime: —", "")
	tm.mUptime.Disable()
	systray.AddSeparator()

	mQuit := systray.AddMenuItem("Quit", "Quit Wallfacer")

	// Click handler goroutine.
	go func() {
		for {
			select {
			case <-mOpen.ClickedCh:
				tm.showWindow()
			case <-mQuit.ClickedCh:
				tm.quit()
				return
			case <-tm.done:
				return
			}
		}
	}()

	// Poll loop goroutine.
	go func() {
		tm.poll() // initial poll
		ticker := time.NewTicker(trayPollInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				tm.poll()
			case <-tm.done:
				return
			}
		}
	}()
}

// healthData is the subset of the /api/debug/health response we care about.
type healthData struct {
	TasksByStatus map[string]int `json:"tasks_by_status"`
	UptimeSeconds float64        `json:"uptime_seconds"`
}

// poll fetches the health endpoint and updates the tray state.
func (tm *TrayManager) poll() {
	data, err := tm.fetchHealth()
	if err != nil {
		logger.Main.Debug("tray health poll failed", "error", err)
		return
	}

	// Update icon.
	state := iconState(data.TasksByStatus)
	if state != tm.lastIconState {
		switch state {
		case "active":
			systray.SetIcon(icons.TrayActive)
		case "attention":
			systray.SetIcon(icons.TrayAttention)
		default:
			systray.SetTemplateIcon(icons.Tray, icons.Tray)
		}
		tm.lastIconState = state
	}

	// Update tooltip.
	systray.SetTooltip(formatTooltip(data.TasksByStatus, data.UptimeSeconds))

	// Update menu labels.
	inProgress := data.TasksByStatus["in_progress"] + data.TasksByStatus["committing"]
	waiting := data.TasksByStatus["waiting"] + data.TasksByStatus["failed"]
	backlog := data.TasksByStatus["backlog"]

	if inProgress > 0 {
		tm.mInProgress.SetTitle(fmt.Sprintf("● %d In Progress", inProgress))
	} else {
		tm.mInProgress.SetTitle("  0 In Progress")
	}
	tm.mWaiting.SetTitle(fmt.Sprintf("  %d Waiting", waiting))
	tm.mBacklog.SetTitle(fmt.Sprintf("  %d Backlog", backlog))
	tm.mUptime.SetTitle(fmt.Sprintf("Uptime: %s", formatDuration(data.UptimeSeconds)))
}

// fetchHealth performs an HTTP GET to the health endpoint.
func (tm *TrayManager) fetchHealth() (*healthData, error) {
	req, err := http.NewRequest("GET", tm.serverURL+"/api/debug/health", nil)
	if err != nil {
		return nil, err
	}
	if tm.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+tm.apiKey)
	}
	resp, err := tm.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("health returned %d", resp.StatusCode)
	}
	var data healthData
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return nil, err
	}
	return &data, nil
}

// iconState returns the icon variant name based on task status counts.
func iconState(tasksByStatus map[string]int) string {
	if tasksByStatus["waiting"] > 0 || tasksByStatus["failed"] > 0 {
		return "attention"
	}
	if tasksByStatus["in_progress"] > 0 || tasksByStatus["committing"] > 0 {
		return "active"
	}
	return "idle"
}

// formatTooltip builds a tooltip string from task status and uptime.
func formatTooltip(tasksByStatus map[string]int, uptimeSeconds float64) string {
	running := tasksByStatus["in_progress"] + tasksByStatus["committing"]
	waiting := tasksByStatus["waiting"]
	if running == 0 && waiting == 0 {
		return "Wallfacer — Idle"
	}
	return fmt.Sprintf("Wallfacer — %d running · %d waiting · uptime %s",
		running, waiting, formatDuration(uptimeSeconds))
}

// formatDuration formats seconds into a human-readable duration like "2h 15m".
func formatDuration(seconds float64) string {
	d := time.Duration(seconds * float64(time.Second))
	h := int(d.Hours())
	m := int(d.Minutes()) % 60
	if h > 0 {
		return fmt.Sprintf("%dh %dm", h, m)
	}
	return fmt.Sprintf("%dm", m)
}
