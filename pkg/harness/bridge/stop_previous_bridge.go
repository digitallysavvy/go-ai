package bridge

import (
	"context"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

const stopPollInterval = 100 * time.Millisecond

// DefaultStopGracePeriod is how long StopPreviousBridge waits after each
// signal (TS `gracePeriodMs`, 5 seconds).
const DefaultStopGracePeriod = 5 * time.Second

// listProcessesCommand prints one line per process: pid, parent pid, state
// and start time. On Linux, shell builtins read /proc, so procps is not
// needed; other systems use `ps` (TS LIST_PROCESSES).
const listProcessesCommand = `if [ -r /proc/self/stat ]; then
  for f in /proc/[0-9]*/stat; do read -r s < "$f" && set -- ${s##*') '} && p=${f#/proc/} && echo "${p%/stat} $2 $1 ${20}"; done
else
  ps -A -o pid= -o ppid= -o stat= -o lstart=
fi 2>/dev/null`

// StopPreviousBridgeOptions configures StopPreviousBridge.
type StopPreviousBridgeOptions struct {
	Sandbox        providerutils.SandboxSession
	BridgeStateDir string
	BridgeType     string
	// GracePeriod is the wait after each signal (default DefaultStopGracePeriod).
	GracePeriod time.Duration
}

// StopPreviousBridge stops the bridge process recorded in bridge-meta.json
// while it still runs, so a replacement bridge can bind the port it holds.
// Call it before MarkBridgeStarting overwrites the record.
//
// The bridge and the processes it started (the runtime running its turn and
// that runtime's tool commands) get SIGTERM. Any that still run after
// GracePeriod get SIGKILL, and the call waits up to GracePeriod again. If a
// process outlives both waits, the call returns anyway and the replacement
// reports the port conflict.
//
// The recorded pid can be stale (a sandbox resumed from a snapshot), so only
// a process whose command line contains BridgeStateDir counts as the bridge.
// Without a recorded pid, or without such a process, nothing is signalled.
// Mirrors TS `stopPreviousBridge` (d84bf245a0).
func StopPreviousBridge(ctx context.Context, opts StopPreviousBridgeOptions) error {
	grace := opts.GracePeriod
	if grace <= 0 {
		grace = DefaultStopGracePeriod
	}
	meta := ReadBridgeMeta(ctx, opts.Sandbox, opts.BridgeStateDir, opts.BridgeType)
	if meta == nil || meta.PID == nil {
		return nil
	}
	// `kill` reads 0 and negative ids as process groups, so only accept a pid.
	f := *meta.PID
	if f != math.Trunc(f) || f <= 0 || f > 1<<53-1 {
		return nil
	}
	pid := int(f)

	cmdline, err := opts.Sandbox.Run(ctx, providerutils.SandboxProcessOptions{
		// Linux exposes the command line in /proc; `ps` covers other systems.
		Command: fmt.Sprintf(`{ tr '\0' ' ' < /proc/%d/cmdline || ps -ww -o args= -p %d; } 2>/dev/null`, pid, pid),
	})
	if err != nil {
		return err
	}
	if !strings.Contains(cmdline.Stdout, opts.BridgeStateDir) {
		return nil
	}

	list := func() (map[int]processEntry, error) {
		res, err := opts.Sandbox.Run(ctx, providerutils.SandboxProcessOptions{Command: listProcessesCommand})
		if err != nil {
			return nil, err
		}
		return parseProcesses(res.Stdout), nil
	}
	started, err := list()
	if err != nil {
		return err
	}
	// A pid counts as running only while it keeps the start time it has now,
	// so a pid that another process has reused is dropped, not signalled.
	stillRunning := func(pids []int) ([]int, error) {
		current, err := list()
		if err != nil {
			return nil, err
		}
		var out []int
		for _, id := range pids {
			c, cok := current[id]
			s, sok := started[id]
			if cok == sok && (!cok || c.start == s.start) {
				out = append(out, id)
			}
		}
		return out, nil
	}

	// The bridge comes first, so it is signalled before its children exit.
	remaining := processTree(pid, started)
	for _, signal := range []string{"TERM", "KILL"} {
		if len(remaining) == 0 {
			return nil
		}
		ids := make([]string, len(remaining))
		for i, id := range remaining {
			ids[i] = strconv.Itoa(id)
		}
		if _, err := opts.Sandbox.Run(ctx, providerutils.SandboxProcessOptions{
			Command: fmt.Sprintf("kill -%s %s", signal, strings.Join(ids, " ")),
		}); err != nil {
			return err
		}
		deadline := time.Now().Add(grace)
		if remaining, err = stillRunning(remaining); err != nil {
			return err
		}
		for len(remaining) > 0 && time.Now().Before(deadline) {
			Sleep(ctx, stopPollInterval)
			if ctx.Err() != nil {
				return abortError(ctx)
			}
			if remaining, err = stillRunning(remaining); err != nil {
				return err
			}
		}
	}
	return nil
}

type processEntry struct {
	parent int
	start  string
}

func parseProcesses(output string) map[int]processEntry {
	processes := map[int]processEntry{}
	for _, line := range strings.Split(output, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		pid, err := strconv.Atoi(fields[0])
		if err != nil || pid <= 0 {
			continue
		}
		// A zombie has exited, so it holds no sockets and cannot be signalled.
		if len(fields) > 2 && strings.HasPrefix(fields[2], "Z") {
			continue
		}
		parent := 0 // TS Number(undefined) is NaN: matches no pid
		if len(fields) > 1 {
			if p, err := strconv.Atoi(fields[1]); err == nil {
				parent = p
			}
		}
		start := ""
		if len(fields) > 3 {
			start = strings.Join(fields[3:], " ")
		}
		processes[pid] = processEntry{parent: parent, start: start}
	}
	return processes
}

// processTree returns the running pid and its descendants, parents before
// children, in ascending pid order among siblings.
func processTree(pid int, processes map[int]processEntry) []int {
	if _, ok := processes[pid]; !ok {
		return nil
	}
	tree := []int{pid}
	seen := map[int]bool{pid: true}
	for i := 0; i < len(tree); i++ {
		var children []int
		for child, e := range processes {
			if e.parent == tree[i] && !seen[child] {
				children = append(children, child)
			}
		}
		sortInts(children)
		for _, c := range children {
			seen[c] = true
			tree = append(tree, c)
		}
	}
	return tree
}

func sortInts(a []int) {
	for i := 1; i < len(a); i++ {
		for j := i; j > 0 && a[j] < a[j-1]; j-- {
			a[j], a[j-1] = a[j-1], a[j]
		}
	}
}
