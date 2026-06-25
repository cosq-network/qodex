package store

import (
	"context"
	"path/filepath"
	"testing"
)

func TestStoreSessionMessagesAndTools(t *testing.T) {
	ctx := context.Background()
	db, err := Open(filepath.Join(t.TempDir(), "locha.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

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
	callID, err := db.AddToolCall(ctx, sessionID, "read_file", `{"path":"README.md"}`, "requested")
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
	if len(messages) != 2 || messages[0].Content != "hello" || messages[1].Content != "hi" {
		t.Fatalf("messages = %#v", messages)
	}
}
