package store

import (
	"context"
	"database/sql"
	"fmt"
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

type ToolCallRecord struct {
	ID        int64
	SessionID int64
	Name      string
	Arguments string
	Status    string
	CreatedAt time.Time
	Result    *ToolResultRecord
}

type ToolResultRecord struct {
	ID        int64
	ToolCallID int64
	Output    string
	Error     string
	CreatedAt time.Time
}

type ExportData struct {
	Session   Session
	Messages  []Message
	ToolCalls []ToolCallRecord
}

func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(`pragma journal_mode=wal`); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("enable wal: %w", err)
	}
	if _, err := db.Exec(`pragma busy_timeout=5000`); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("set busy timeout: %w", err)
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

func (s *Store) AddApproval(ctx context.Context, sessionID, toolCallID int64, toolName, kind, summary string, approved bool) error {
	now := time.Now().UTC().Format(time.RFC3339)
	v := 0
	if approved {
		v = 1
	}
	_, err := s.db.ExecContext(ctx, `insert into approvals(session_id,tool_call_id,tool_name,kind,summary,approved,created_at) values(?,?,?,?,?,?,?)`,
		sessionID, toolCallID, toolName, kind, summary, v, now)
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

type OutputArtifact struct {
	ID          int64
	SessionID   int64
	ToolCallID  int64
	ToolName    string
	Summary     string
	Content     string
	ContentType string
	Size        int64
	CreatedAt   time.Time
}

const ArtifactThreshold = 4000

func (s *Store) SaveArtifact(ctx context.Context, sessionID, toolCallID int64, toolName, summary, content, contentType string) (int64, error) {
	now := time.Now().UTC().Format(time.RFC3339)
	res, err := s.db.ExecContext(ctx, `insert into output_artifacts(session_id,tool_call_id,tool_name,summary,content,content_type,size,created_at) values(?,?,?,?,?,?,?,?)`,
		sessionID, toolCallID, toolName, summary, content, contentType, len(content), now)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (s *Store) GetArtifact(ctx context.Context, id int64) (*OutputArtifact, error) {
	row := s.db.QueryRowContext(ctx, `select id,session_id,tool_call_id,tool_name,summary,content,content_type,size,created_at from output_artifacts where id=?`, id)
	var a OutputArtifact
	var created string
	if err := row.Scan(&a.ID, &a.SessionID, &a.ToolCallID, &a.ToolName, &a.Summary, &a.Content, &a.ContentType, &a.Size, &created); err != nil {
		return nil, err
	}
	a.CreatedAt, _ = time.Parse(time.RFC3339, created)
	return &a, nil
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

func (s *Store) ListToolCalls(ctx context.Context, sessionID int64) ([]ToolCallRecord, error) {
	query := `select tc.id,tc.session_id,tc.name,tc.arguments_json,tc.status,tc.created_at,
		tr.id,tr.tool_call_id,tr.output,tr.error,tr.created_at
		from tool_calls tc
		left join tool_results tr on tr.tool_call_id = tc.id
			and tr.id = (select max(id) from tool_results where tool_call_id = tc.id)
		where tc.session_id=? order by tc.id asc`
	rows, err := s.db.QueryContext(ctx, query, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ToolCallRecord
	for rows.Next() {
		var r ToolCallRecord
		var created string
		var resultID, resultToolCallID sql.NullInt64
		var resultOutput, resultError, resultCreated sql.NullString
		if err := rows.Scan(&r.ID, &r.SessionID, &r.Name, &r.Arguments, &r.Status, &created,
			&resultID, &resultToolCallID, &resultOutput, &resultError, &resultCreated); err != nil {
			return nil, err
		}
		r.CreatedAt, _ = time.Parse(time.RFC3339, created)
		if resultID.Valid {
			r.Result = &ToolResultRecord{
				ID:        resultID.Int64,
				ToolCallID: resultToolCallID.Int64,
				Output:    resultOutput.String,
				Error:     resultError.String,
			}
			r.Result.CreatedAt, _ = time.Parse(time.RFC3339, resultCreated.String)
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *Store) ExportSession(ctx context.Context, sessionID int64) (ExportData, error) {
	session, err := s.GetSession(ctx, sessionID)
	if err != nil {
		return ExportData{}, err
	}
	messages, err := s.ListMessages(ctx, sessionID)
	if err != nil {
		return ExportData{}, err
	}
	toolCalls, err := s.ListToolCalls(ctx, sessionID)
	if err != nil {
		return ExportData{}, err
	}
	return ExportData{Session: session, Messages: messages, ToolCalls: toolCalls}, nil
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
