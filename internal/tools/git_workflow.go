package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

var (
	gitWorkspaceSummaryParams = json.RawMessage(`{"type":"object","properties":{"recent_commits":{"type":"integer"}},"additionalProperties":false}`)
	gitStageParams            = json.RawMessage(`{"type":"object","properties":{"paths":{"type":"array","items":{"type":"string"}},"all":{"type":"boolean"}},"additionalProperties":false}`)
	gitCommitParams           = json.RawMessage(`{"type":"object","properties":{"message":{"type":"string"},"paths":{"type":"array","items":{"type":"string"}},"all":{"type":"boolean"}},"required":["message"],"additionalProperties":false}`)
	gitBranchParams           = json.RawMessage(`{"type":"object","properties":{"action":{"type":"string","enum":["list","create","switch"]},"name":{"type":"string"},"start_point":{"type":"string"}},"additionalProperties":false}`)
	gitWorktreeParams         = json.RawMessage(`{"type":"object","properties":{"action":{"type":"string","enum":["list","add","remove"]},"path":{"type":"string"},"branch":{"type":"string"},"force":{"type":"boolean"}},"additionalProperties":false}`)
	gitUndoParams             = json.RawMessage(`{"type":"object","properties":{"commit":{"type":"string"}},"additionalProperties":false}`)
	gitSnapshotParams         = json.RawMessage(`{"type":"object","properties":{"label":{"type":"string"}},"additionalProperties":false}`)
	gitRestoreSnapshotParams  = json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"}},"required":["path"],"additionalProperties":false}`)
)

func (r *Registry) registerGitWorkflowTools() {
	r.add("git_workspace_summary", "Show branch, ahead/behind state, staged and unstaged files, diff statistics, and recent commits in one view.", "read", gitWorkspaceSummaryParams, r.gitWorkspaceSummary)
	r.add("git_stage", "Stage selected repository paths, or all current changes when all=true.", "write", gitStageParams, r.gitStage)
	r.add("git_commit", "Create a focused commit from staged changes or explicitly selected paths.", "write", gitCommitParams, r.gitCommit)
	r.add("git_branch", "List, create, or switch branches without invoking a shell.", "write", gitBranchParams, r.gitBranch)
	r.add("git_worktree", "List, create, or remove Git worktrees for isolated parallel work.", "write", gitWorktreeParams, r.gitWorktree)
	r.add("git_undo", "Create a new revert commit for a previous commit, preserving history instead of resetting it.", "write", gitUndoParams, r.gitUndo)
	r.add("git_snapshot", "Save the current tracked worktree diff as a reversible Qodex checkpoint.", "write", gitSnapshotParams, r.gitSnapshot)
	r.add("git_restore_snapshot", "Apply a previously saved Qodex tracked-worktree checkpoint.", "write", gitRestoreSnapshotParams, r.gitRestoreSnapshot)
}

func (r *Registry) gitWorkspaceSummary(ctx context.Context, raw json.RawMessage) (Result, error) {
	var args struct {
		RecentCommits int `json:"recent_commits"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return Result{}, err
	}
	if args.RecentCommits <= 0 || args.RecentCommits > 20 {
		args.RecentCommits = 8
	}
	branch, err := r.runGit(ctx, "branch", "--show-current")
	if err != nil {
		return Result{}, err
	}
	status, err := r.runGit(ctx, "status", "--short", "--branch")
	if err != nil {
		return Result{}, err
	}
	stat, err := r.runGit(ctx, "diff", "--stat")
	if err != nil {
		return Result{}, err
	}
	stagedStat, err := r.runGit(ctx, "diff", "--cached", "--stat")
	if err != nil {
		return Result{}, err
	}
	log, err := r.runGit(ctx, "log", "--oneline", fmt.Sprintf("-%d", args.RecentCommits))
	if err != nil {
		return Result{}, err
	}
	content := fmt.Sprintf("Branch: %s\n\nStatus:\n%s\nUnstaged diff stat:\n%s\nStaged diff stat:\n%s\nRecent commits:\n%s", strings.TrimSpace(branch), status, stat, stagedStat, log)
	return Result{OK: true, Summary: "Collected Git workspace summary.", Content: truncate(content, 20000)}, nil
}

func (r *Registry) gitStage(ctx context.Context, raw json.RawMessage) (Result, error) {
	var args struct {
		Paths []string `json:"paths"`
		All   bool     `json:"all"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return Result{}, err
	}
	if !args.All && len(args.Paths) == 0 {
		return Result{}, fmt.Errorf("paths or all=true is required")
	}
	parts := []string{"add"}
	if args.All {
		parts = append(parts, "--all")
	} else {
		paths, err := r.gitPaths(args.Paths)
		if err != nil {
			return Result{}, err
		}
		parts = append(parts, "--")
		parts = append(parts, paths...)
	}
	return r.gitCommand(ctx, parts, "Staged Git changes.")
}

func (r *Registry) gitCommit(ctx context.Context, raw json.RawMessage) (Result, error) {
	var args struct {
		Message string   `json:"message"`
		Paths   []string `json:"paths"`
		All     bool     `json:"all"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return Result{}, err
	}
	args.Message = strings.TrimSpace(args.Message)
	if args.Message == "" {
		return Result{}, fmt.Errorf("commit message is required")
	}
	if strings.ContainsAny(args.Message, "\r\n") {
		return Result{}, fmt.Errorf("commit message must be a single line")
	}
	if args.All || len(args.Paths) > 0 {
		stageRaw, _ := json.Marshal(map[string]interface{}{"paths": args.Paths, "all": args.All})
		if _, err := r.gitStage(ctx, stageRaw); err != nil {
			return Result{}, err
		}
	}
	commitArgs := []string{"commit", "-m", args.Message}
	if len(args.Paths) > 0 && !args.All {
		paths, err := r.gitPaths(args.Paths)
		if err != nil {
			return Result{}, err
		}
		commitArgs = append(commitArgs, "--")
		commitArgs = append(commitArgs, paths...)
	}
	return r.gitCommand(ctx, commitArgs, "Created Git commit.")
}

func (r *Registry) gitBranch(ctx context.Context, raw json.RawMessage) (Result, error) {
	var args struct {
		Action     string `json:"action"`
		Name       string `json:"name"`
		StartPoint string `json:"start_point"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return Result{}, err
	}
	switch args.Action {
	case "", "list":
		return r.gitCommand(ctx, []string{"branch", "--list"}, "Listed Git branches.")
	case "create":
		if err := validateGitName(args.Name); err != nil {
			return Result{}, err
		}
		parts := []string{"switch", "-c", args.Name}
		if args.StartPoint != "" {
			if err := validateGitName(args.StartPoint); err != nil {
				return Result{}, err
			}
			parts = append(parts, args.StartPoint)
		}
		return r.gitCommand(ctx, parts, "Created and switched Git branch.")
	case "switch":
		if err := validateGitName(args.Name); err != nil {
			return Result{}, err
		}
		return r.gitCommand(ctx, []string{"switch", args.Name}, "Switched Git branch.")
	default:
		return Result{}, fmt.Errorf("unsupported branch action: %s", args.Action)
	}
}

func (r *Registry) gitWorktree(ctx context.Context, raw json.RawMessage) (Result, error) {
	var args struct {
		Action string `json:"action"`
		Path   string `json:"path"`
		Branch string `json:"branch"`
		Force  bool   `json:"force"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return Result{}, err
	}
	switch args.Action {
	case "", "list":
		return r.gitCommand(ctx, []string{"worktree", "list"}, "Listed Git worktrees.")
	case "add":
		if args.Path == "" || args.Branch == "" {
			return Result{}, fmt.Errorf("path and branch are required")
		}
		path, err := filepath.Abs(filepath.Join(r.root, args.Path))
		if err != nil {
			return Result{}, err
		}
		if err := validateGitName(args.Branch); err != nil {
			return Result{}, err
		}
		parts := []string{"worktree", "add"}
		if args.Force {
			parts = append(parts, "--force")
		}
		parts = append(parts, path, args.Branch)
		return r.gitCommand(ctx, parts, "Created Git worktree.")
	case "remove":
		if args.Path == "" {
			return Result{}, fmt.Errorf("path is required")
		}
		parts := []string{"worktree", "remove"}
		if args.Force {
			parts = append(parts, "--force")
		}
		parts = append(parts, args.Path)
		return r.gitCommand(ctx, parts, "Removed Git worktree.")
	default:
		return Result{}, fmt.Errorf("unsupported worktree action: %s", args.Action)
	}
}

func (r *Registry) gitUndo(ctx context.Context, raw json.RawMessage) (Result, error) {
	var args struct {
		Commit string `json:"commit"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return Result{}, err
	}
	if args.Commit == "" {
		args.Commit = "HEAD"
	}
	if err := validateGitName(args.Commit); err != nil {
		return Result{}, err
	}
	return r.gitCommand(ctx, []string{"revert", "--no-edit", args.Commit}, "Created Git revert commit.")
}

func (r *Registry) gitSnapshot(ctx context.Context, raw json.RawMessage) (Result, error) {
	var args struct {
		Label string `json:"label"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return Result{}, err
	}
	label := sanitizeSnapshotLabel(args.Label)
	stamp := time.Now().UTC().Format("20060102T150405.000000000Z")
	dir := filepath.Join(r.root, ".qodex", "git-snapshots")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return Result{}, err
	}
	path := filepath.Join(dir, stamp+"-"+label+".patch")
	content, err := r.runGit(ctx, "diff", "--binary", "HEAD")
	if err != nil {
		return Result{}, err
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		return Result{}, err
	}
	rel, _ := filepath.Rel(r.root, path)
	return Result{OK: true, Summary: "Saved Git worktree snapshot.", Content: rel, Metadata: map[string]interface{}{"path": rel, "tracked_only": true}}, nil
}

func (r *Registry) gitRestoreSnapshot(ctx context.Context, raw json.RawMessage) (Result, error) {
	var args struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return Result{}, err
	}
	path, err := r.safePath(args.Path)
	if err != nil {
		return Result{}, err
	}
	allowedDir := filepath.Join(r.root, ".qodex", "git-snapshots")
	rel, err := filepath.Rel(allowedDir, path)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return Result{}, fmt.Errorf("snapshot must be inside .qodex/git-snapshots")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return Result{}, err
	}
	cctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(cctx, "git", "apply", "--binary", "-")
	cmd.Dir = r.root
	cmd.Stdin = strings.NewReader(string(data))
	out, err := runWithKillStdin(cctx, cmd)
	result := Result{OK: err == nil, Summary: "Restored Git worktree snapshot.", Content: truncate(string(out), 12000), Metadata: map[string]interface{}{"path": args.Path, "tracked_only": true}}
	if err != nil {
		return result, err
	}
	return result, nil
}

func sanitizeSnapshotLabel(label string) string {
	label = strings.TrimSpace(label)
	if label == "" {
		return "checkpoint"
	}
	var b strings.Builder
	for _, r := range label {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			b.WriteRune(r)
		} else {
			b.WriteByte('-')
		}
	}
	return strings.Trim(b.String(), "-")
}

func (r *Registry) gitPaths(paths []string) ([]string, error) {
	out := make([]string, 0, len(paths))
	for _, path := range paths {
		if path == "" || filepath.IsAbs(path) || strings.HasPrefix(filepath.Clean(path), "..") {
			return nil, fmt.Errorf("invalid repository path: %s", path)
		}
		out = append(out, path)
	}
	return out, nil
}

func validateGitName(name string) error {
	if name == "" || strings.HasPrefix(name, "-") || strings.ContainsAny(name, "\r\n") {
		return fmt.Errorf("invalid Git name: %q", name)
	}
	return nil
}

func (r *Registry) runGit(ctx context.Context, args ...string) (string, error) {
	result, err := r.gitCommand(ctx, args, "")
	return result.Content, err
}

func (r *Registry) gitCommand(ctx context.Context, args []string, summary string) (Result, error) {
	cctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(cctx, "git", args...)
	cmd.Dir = r.root
	out, err := runWithKill(cctx, cmd)
	if summary == "" {
		summary = "Ran git " + strings.Join(args, " ")
	}
	result := Result{OK: err == nil, Summary: summary, Content: truncate(string(out), 20000)}
	if err != nil {
		return result, err
	}
	return result, nil
}
