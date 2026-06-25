package store

import (
	"context"
	"fmt"
)

type migration struct {
	version int
	up      string
}

var migrations = []migration{
	{
		version: 1,
		up: `create table if not exists sessions (
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
);`,
	},
	{
		version: 2,
		up: `create table if not exists approvals (
  id integer primary key autoincrement,
  session_id integer not null,
  tool_call_id integer not null,
  tool_name text not null,
  kind text not null,
  summary text not null,
  approved integer not null,
  created_at text not null
);`,
	},
	{
		version: 3,
		up: `create table if not exists output_artifacts (
  id integer primary key autoincrement,
  session_id integer not null,
  tool_call_id integer not null,
  tool_name text not null,
  summary text not null default '',
  content text not null,
  content_type text not null default 'text',
  size integer not null default 0,
  created_at text not null
);
create index if not exists idx_output_artifacts_session on output_artifacts(session_id);`,
	},
}

func (s *Store) migrate(ctx context.Context) error {
	if _, err := s.db.ExecContext(ctx, `create table if not exists schema_version (version integer primary key, applied_at text not null)`); err != nil {
		return err
	}
	for _, m := range migrations {
		var count int
		if err := s.db.QueryRowContext(ctx, `select count(*) from schema_version where version=?`, m.version).Scan(&count); err != nil {
			return err
		}
		if count > 0 {
			continue
		}
		if _, err := s.db.ExecContext(ctx, m.up); err != nil {
			return fmt.Errorf("migration %d: %w", m.version, err)
		}
		if _, err := s.db.ExecContext(ctx, `insert into schema_version(version,applied_at) values(?,datetime('now'))`, m.version); err != nil {
			return fmt.Errorf("record migration %d: %w", m.version, err)
		}
	}
	return nil
}
