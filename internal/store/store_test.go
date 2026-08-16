package store

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
)

func TestStoreSessionMessagesAndTools(t *testing.T) {
	ctx := context.Background()
	db, err := Open(filepath.Join(t.TempDir(), "qodex.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()

	sessionID, err := db.CreateSession(ctx, "/repo", "title", "model", "llama.cpp")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AddMessage(ctx, sessionID, "user", "hello"); err != nil {
		t.Fatal(err)
	}
	if err := db.AddMessage(ctx, sessionID, "assistant", "hi"); err != nil {
		t.Fatal(err)
	}
	if err := db.AddMessage(ctx, sessionID, "assistant", "", `{"tool_calls":[{"id":"call_1"}]}`); err != nil {
		t.Fatal(err)
	}
	callID, err := db.AddToolCall(ctx, sessionID, "read_file", `{"path":"README.md"}`, "requested", `{"skills":["project"],"tool_mode":"native"}`)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AddToolResult(ctx, callID, `{"ok":true}`, ""); err != nil {
		t.Fatal(err)
	}

	sessions, err := db.ListSessions(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 1 || sessions[0].ID != sessionID {
		t.Fatalf("sessions = %#v", sessions)
	}
	messages, err := db.ListMessages(ctx, sessionID)
	if err != nil {
		t.Fatal(err)
	}
	if len(messages) != 3 || messages[0].Content != "hello" || messages[1].Content != "hi" || !strings.Contains(messages[2].Metadata, "tool_calls") {
		t.Fatalf("messages = %#v", messages)
	}
	calls, err := db.ListToolCalls(ctx, sessionID)
	if err != nil || len(calls) != 1 || !strings.Contains(calls[0].ContextJSON, "native") {
		t.Fatalf("tool calls = %#v, err=%v", calls, err)
	}
}
