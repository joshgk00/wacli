package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	appPkg "github.com/steipete/wacli/internal/app"
	"github.com/steipete/wacli/internal/config"
	"github.com/steipete/wacli/internal/out"
)

func newSyncCmd(flags *rootFlags) *cobra.Command {
	var once bool
	var follow bool
	var idleExit time.Duration
	var downloadMedia bool
	var refreshContacts bool
	var refreshGroups bool
	var daemon bool
	var stop bool
	var status bool

	cmd := &cobra.Command{
		Use:   "sync",
		Short: "Sync messages (requires prior auth; never shows QR)",
		RunE: func(cmd *cobra.Command, args []string) error {
			storeDir := flags.storeDir
			if storeDir == "" {
				storeDir = config.DefaultStoreDir()
			}
			storeDir, _ = filepath.Abs(storeDir)

			// Handle --stop flag
			if stop {
				return stopDaemon(storeDir)
			}

			// Handle --status flag
			if status {
				return statusDaemon(storeDir, flags.asJSON)
			}

			// Handle --daemon flag
			if daemon {
				return startDaemon(storeDir, flags)
			}

			// Normal sync execution
			return runSync(flags, once, follow, idleExit, downloadMedia, refreshContacts, refreshGroups)
		},
	}

	cmd.Flags().BoolVar(&once, "once", false, "sync until idle and exit")
	cmd.Flags().BoolVar(&follow, "follow", true, "keep syncing until Ctrl+C")
	cmd.Flags().DurationVar(&idleExit, "idle-exit", 30*time.Second, "exit after being idle (once mode)")
	cmd.Flags().BoolVar(&downloadMedia, "download-media", false, "download media in the background during sync")
	cmd.Flags().BoolVar(&refreshContacts, "refresh-contacts", false, "refresh contacts from session store into local DB")
	cmd.Flags().BoolVar(&refreshGroups, "refresh-groups", false, "refresh joined groups (live) into local DB")
	cmd.Flags().BoolVar(&daemon, "daemon", false, "run sync in the background")
	cmd.Flags().BoolVar(&stop, "stop", false, "stop the daemon")
	cmd.Flags().BoolVar(&status, "status", false, "check daemon status")
	return cmd
}

func runSync(flags *rootFlags, once, follow bool, idleExit time.Duration, downloadMedia, refreshContacts, refreshGroups bool) error {
	ctx, stopSignal := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stopSignal()

	// Check if we're in daemon mode (child process)
	isDaemon := os.Getenv("WACLI_DAEMON") == "1"
	if isDaemon {
		storeDir := flags.storeDir
		if storeDir == "" {
			storeDir = config.DefaultStoreDir()
		}
		storeDir, _ = filepath.Abs(storeDir)

		// Redirect stdout/stderr to log file
		logPath := filepath.Join(storeDir, "sync.log")
		logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
		if err != nil {
			return fmt.Errorf("open log file: %w", err)
		}
		defer logFile.Close()

		os.Stdout = logFile
		os.Stderr = logFile

		// Write PID file
		pidPath := filepath.Join(storeDir, "sync.pid")
		if err := os.WriteFile(pidPath, []byte(fmt.Sprintf("%d\n", os.Getpid())), 0644); err != nil {
			return fmt.Errorf("write PID file: %w", err)
		}

		// Clean up PID file on exit
		defer os.Remove(pidPath)

		fmt.Fprintf(logFile, "[%s] Daemon started (PID %d)\n", time.Now().Format("2006-01-02 15:04:05"), os.Getpid())
	}

	a, lk, err := newApp(ctx, flags, true, false)
	if err != nil {
		if isDaemon {
			fmt.Fprintf(os.Stderr, "[%s] Failed to init app: %v\n", time.Now().Format("2006-01-02 15:04:05"), err)
		}
		return err
	}
	defer closeApp(a, lk)

	if err := a.EnsureAuthed(); err != nil {
		if isDaemon {
			fmt.Fprintf(os.Stderr, "[%s] Auth failed: %v\n", time.Now().Format("2006-01-02 15:04:05"), err)
		}
		return err
	}

	mode := appPkg.SyncModeFollow
	if once {
		mode = appPkg.SyncModeOnce
	} else if follow {
		mode = appPkg.SyncModeFollow
	} else {
		mode = appPkg.SyncModeOnce
	}

	res, err := a.Sync(ctx, appPkg.SyncOptions{
		Mode:            mode,
		AllowQR:         false,
		DownloadMedia:   downloadMedia,
		RefreshContacts: refreshContacts,
		RefreshGroups:   refreshGroups,
		IdleExit:        idleExit,
	})
	if err != nil {
		return err
	}

	if flags.asJSON {
		return out.WriteJSON(os.Stdout, map[string]any{
			"synced":          true,
			"messages_stored": res.MessagesStored,
		})
	}
	fmt.Fprintf(os.Stdout, "Messages stored: %d\n", res.MessagesStored)
	return nil
}

func startDaemon(storeDir string, flags *rootFlags) error {
	pidPath := filepath.Join(storeDir, "sync.pid")

	// Check if daemon is already running
	if pid, err := readPID(pidPath); err == nil {
		if processExists(pid) {
			return fmt.Errorf("daemon already running (PID %d)", pid)
		}
		// Stale PID file, remove it
		_ = os.Remove(pidPath)
	}

	// Re-exec ourselves with WACLI_DAEMON=1
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("get executable path: %w", err)
	}

	args := []string{"sync", "--follow"}
	if flags.storeDir != "" {
		args = append([]string{"--store", flags.storeDir}, args...)
	}

	cmd := exec.Command(exe, args...)
	cmd.Env = append(os.Environ(), "WACLI_DAEMON=1")
	cmd.Dir, _ = os.Getwd()
	// Detach from parent process group so child survives parent exit
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	// Redirect child stdout/stderr to devnull (child manages its own log file)
	cmd.Stdout = nil
	cmd.Stderr = nil
	cmd.Stdin = nil

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start daemon: %w", err)
	}

	pid := cmd.Process.Pid

	// Release the child process so it doesn't become a zombie when parent exits
	_ = cmd.Process.Release()

	// Wait for child to write PID file (confirms it initialized successfully)
	pidWaitPath := filepath.Join(storeDir, "sync.pid")
	started := false
	for i := 0; i < 30; i++ { // wait up to 3 seconds
		time.Sleep(100 * time.Millisecond)
		if _, err := os.Stat(pidWaitPath); err == nil {
			started = true
			break
		}
		if !processExists(pid) {
			// Child died - check log for error
			logPath := filepath.Join(storeDir, "sync.log")
			if data, err := os.ReadFile(logPath); err == nil {
				return fmt.Errorf("daemon failed to start:\n%s", string(data))
			}
			return fmt.Errorf("daemon failed to start")
		}
	}
	if !started && !processExists(pid) {
		return fmt.Errorf("daemon failed to start (timeout)")
	}

	fmt.Fprintf(os.Stdout, "Daemon started (PID %d)\n", pid)
	fmt.Fprintf(os.Stdout, "Log: %s\n", filepath.Join(storeDir, "sync.log"))

	return nil
}

func stopDaemon(storeDir string) error {
	pidPath := filepath.Join(storeDir, "sync.pid")

	pid, err := readPID(pidPath)
	if err != nil {
		return fmt.Errorf("daemon not running (no PID file)")
	}

	if !processExists(pid) {
		_ = os.Remove(pidPath)
		return fmt.Errorf("daemon not running (stale PID file)")
	}

	process, err := os.FindProcess(pid)
	if err != nil {
		return fmt.Errorf("find process: %w", err)
	}

	if err := process.Signal(syscall.SIGTERM); err != nil {
		return fmt.Errorf("send SIGTERM: %w", err)
	}

	// Wait up to 5 seconds for graceful shutdown
	for i := 0; i < 50; i++ {
		if !processExists(pid) {
			fmt.Fprintf(os.Stdout, "Daemon stopped (PID %d)\n", pid)
			_ = os.Remove(pidPath)
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}

	// Force kill if still running
	_ = process.Kill()
	_ = os.Remove(pidPath)
	return fmt.Errorf("daemon killed (PID %d) - did not exit gracefully", pid)
}

func statusDaemon(storeDir string, asJSON bool) error {
	pidPath := filepath.Join(storeDir, "sync.pid")

	pid, err := readPID(pidPath)
	if err != nil {
		if asJSON {
			return out.WriteJSON(os.Stdout, map[string]any{
				"running": false,
			})
		}
		fmt.Fprintln(os.Stdout, "Daemon not running")
		return nil
	}

	running := processExists(pid)
	if !running {
		_ = os.Remove(pidPath)
	}

	if asJSON {
		return out.WriteJSON(os.Stdout, map[string]any{
			"running": running,
			"pid":     pid,
		})
	}

	if running {
		fmt.Fprintf(os.Stdout, "Daemon running (PID %d)\n", pid)
	} else {
		fmt.Fprintln(os.Stdout, "Daemon not running (stale PID file)")
	}

	return nil
}

func readPID(pidPath string) (int, error) {
	data, err := os.ReadFile(pidPath)
	if err != nil {
		return 0, err
	}
	pidStr := strings.TrimSpace(string(data))
	pid, err := strconv.Atoi(pidStr)
	if err != nil {
		return 0, fmt.Errorf("invalid PID: %w", err)
	}
	return pid, nil
}

func processExists(pid int) bool {
	process, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	// On Unix, FindProcess always succeeds, so we need to actually signal it
	err = process.Signal(syscall.Signal(0))
	return err == nil
}
