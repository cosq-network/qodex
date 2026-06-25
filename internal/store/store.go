package store

import (
	"context"
	"database/sql"
	"time"

	_ "modernc.org/sqlite"
)

type Store struct {
	db *sql.DB
}

type Session struct {
	ID        int64
	Title     string
	UpdatedAt time.Time
}

type Message struct {
	Role      string
	Content   string
	CreatedAt time.Time
}

func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	s := &Store{db: db}
	if err := s.migrate(context.Background()); err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) Close() error {
	return s.db.Close()
}

func (s *Store) migrate(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `
create table if not exists sessions (
  id integer primary key autoincrement,
  project_root text not null,
  title text not null,
  model text not null,
  backend text not null,
  created_at text not null,
  updated_at text not null
);
create table if not exists messages (
  id integer primary key autoincrement,
  session_id integer not null,
  role text not null,
  content text not null,
  created_at text not null
);
create table if not exists tool_calls (
  id integer primary key autoincrement,
  session_id integer not null,
  name text not null,
  arguments_json text not null,
  status text not null,
  created_at text not null
);
create table if not exists tool_results (
  id integer primary key autoincrement,
  tool_call_id integer not null,
  output text not null,
  error text not null,
  created_at text not null
);`)
	return err
}

func (s *Store) CreateSession(ctx context.Context, projectRoot, title, model, backend string) (int64, error) {
	now := time.Now().UTC().Format(time.RFC3339)
	res, err := s.db.ExecContext(ctx, `insert into sessions(project_root,title,model,backend,created_at,updated_at) values(?,?,?,?,?,?)`,
		projectRoot, title, model, backend, now, now)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (s *Store) AddMessage(ctx context.Context, sessionID int64, role, content string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := s.db.ExecContext(ctx, `insert into messages(session_id,role,content,created_at) values(?,?,?,?)`,
		sessionID, role, content, now)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `update sessions set updated_at=? where id=?`, now, sessionID)
	return err
}

func (s *Store) AddToolCall(ctx context.Context, sessionID int64, name, args, status string) (int64, error) {
	now := time.Now().UTC().Format(time.RFC3339)
	res, err := s.db.ExecContext(ctx, `insert into tool_calls(session_id,name,arguments_json,status,created_at) values(?,?,?,?,?)`,
		sessionID, name, args, status, now)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (s *Store) AddToolResult(ctx context.Context, toolCallID int64, output, errText string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := s.db.ExecContext(ctx, `insert into tool_results(tool_call_id,output,error,created_at) values(?,?,?,?)`,
		toolCallID, output, errText, now)
	return err
}

func (s *Store) ListSessions(ctx context.Context) ([]Session, error) {
	rows, err := s.db.QueryContext(ctx, `select id,title,updated_at from sessions order by updated_at desc limit 50`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Session
	for rows.Next() {
		var id int64
		var title, updated string
		if err := rows.Scan(&id, &title, &updated); err != nil {
			return nil, err
		}
		t, _ := time.Parse(time.RFC3339, updated)
		out = append(out, Session{ID: id, Title: title, UpdatedAt: t})
	}
	return out, rows.Err()
}

func (s *Store) GetSession(ctx context.Context, id int64) (Session, error) {
	row := s.db.QueryRowContext(ctx, `select id,title,updated_at from sessions where id=?`, id)
	var sessionID int64
	var title, updated string
	if err := row.Scan(&sessionID, &title, &updated); err != nil {
		return Session{}, err
	}
	t, _ := time.Parse(time.RFC3339, updated)
	return Session{ID: sessionID, Title: title, UpdatedAt: t}, nil
}

func (s *Store) ListMessages(ctx context.Context, sessionID int64) ([]Message, error) {
	rows, err := s.db.QueryContext(ctx, `select role,content,created_at from messages where session_id=? order by id asc`, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Message
	for rows.Next() {
		var role, content, created string
		if err := rows.Scan(&role, &content, &created); err != nil {
			return nil, err
		}
		t, _ := time.Parse(time.RFC3339, created)
		out = append(out, Message{Role: role, Content: content, CreatedAt: t})
	}
	return out, rows.Err()
}
