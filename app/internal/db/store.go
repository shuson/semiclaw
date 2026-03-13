package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

type Store struct {
	db *sql.DB
}

type Owner struct {
	OwnerID          string
	PasswordHash     string
	SessionTokenHash string
	ClaimedAt        int64
}

type SecretRecord struct {
	Ciphertext []byte
	Nonce      []byte
}

type MessageRecord struct {
	Role      string
	Content   string
	CreatedAt int64
}

type AgentRecord struct {
	Name         string
	SystemPrompt string
	Model        string
	BaseURL      string
	CreatedAt    int64
	UpdatedAt    int64
}

func NewStore(db *sql.DB) *Store {
	return &Store{db: db}
}

func (s *Store) DB() *sql.DB {
	return s.db
}

func (s *Store) IsOwnerClaimed(ctx context.Context) (bool, error) {
	const query = `SELECT COUNT(1) FROM owners LIMIT 1`
	var count int
	if err := s.db.QueryRowContext(ctx, query).Scan(&count); err != nil {
		return false, fmt.Errorf("query owner count: %w", err)
	}
	return count > 0, nil
}

func (s *Store) GetOwner(ctx context.Context) (*Owner, error) {
	const query = `
		SELECT owner_id, password_hash, COALESCE(session_token_hash, ''), claimed_at
		FROM owners
		LIMIT 1
	`

	owner := &Owner{}
	err := s.db.QueryRowContext(ctx, query).Scan(
		&owner.OwnerID,
		&owner.PasswordHash,
		&owner.SessionTokenHash,
		&owner.ClaimedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("query owner: %w", err)
	}

	return owner, nil
}

func (s *Store) CreateOwner(ctx context.Context, owner Owner) error {
	const query = `
		INSERT INTO owners (owner_id, password_hash, session_token_hash, claimed_at)
		VALUES (?, ?, ?, ?)
	`

	_, err := s.db.ExecContext(ctx, query,
		owner.OwnerID,
		owner.PasswordHash,
		owner.SessionTokenHash,
		owner.ClaimedAt,
	)
	if err != nil {
		return fmt.Errorf("insert owner: %w", err)
	}
	return nil
}

func (s *Store) UpdateOwnerSession(ctx context.Context, ownerID string, sessionTokenHash string) error {
	const query = `UPDATE owners SET session_token_hash = ? WHERE owner_id = ?`
	_, err := s.db.ExecContext(ctx, query, sessionTokenHash, ownerID)
	if err != nil {
		return fmt.Errorf("update owner session token hash: %w", err)
	}
	return nil
}

func (s *Store) UpsertConfig(ctx context.Context, key string, value string) error {
	const query = `
		INSERT INTO config (key, value, updated_at)
		VALUES (?, ?, ?)
		ON CONFLICT(key) DO UPDATE SET
			value = excluded.value,
			updated_at = excluded.updated_at
	`

	_, err := s.db.ExecContext(ctx, query, key, value, time.Now().UnixMilli())
	if err != nil {
		return fmt.Errorf("upsert config key %q: %w", key, err)
	}
	return nil
}

func (s *Store) GetConfig(ctx context.Context, key string) (string, bool, error) {
	const query = `SELECT value FROM config WHERE key = ? LIMIT 1`

	var value string
	err := s.db.QueryRowContext(ctx, query, key).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("query config key %q: %w", key, err)
	}

	return value, true, nil
}

func (s *Store) DeleteConfig(ctx context.Context, key string) error {
	const query = `DELETE FROM config WHERE key = ?`
	if _, err := s.db.ExecContext(ctx, query, key); err != nil {
		return fmt.Errorf("delete config key %q: %w", key, err)
	}
	return nil
}

func (s *Store) UpsertSecret(ctx context.Context, key string, ciphertext []byte, nonce []byte) error {
	const query = `
		INSERT INTO secrets (key, ciphertext, nonce, updated_at)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(key) DO UPDATE SET
			ciphertext = excluded.ciphertext,
			nonce = excluded.nonce,
			updated_at = excluded.updated_at
	`

	_, err := s.db.ExecContext(ctx, query, key, ciphertext, nonce, time.Now().UnixMilli())
	if err != nil {
		return fmt.Errorf("upsert secret key %q: %w", key, err)
	}
	return nil
}

func (s *Store) GetSecret(ctx context.Context, key string) (*SecretRecord, error) {
	const query = `SELECT ciphertext, nonce FROM secrets WHERE key = ? LIMIT 1`

	secret := &SecretRecord{}
	err := s.db.QueryRowContext(ctx, query, key).Scan(&secret.Ciphertext, &secret.Nonce)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("query secret key %q: %w", key, err)
	}

	return secret, nil
}

func (s *Store) SaveMessage(ctx context.Context, userID string, role string, content string) error {
	const query = `
		INSERT INTO messages (user_id, role, content, created_at)
		VALUES (?, ?, ?, ?)
	`
	_, err := s.db.ExecContext(ctx, query, userID, role, content, time.Now().UnixMilli())
	if err != nil {
		return fmt.Errorf("insert message: %w", err)
	}
	return nil
}

func (s *Store) GetRecentMessages(ctx context.Context, userID string, limit int) ([]MessageRecord, error) {
	const query = `
		SELECT role, content, created_at
		FROM messages
		WHERE user_id = ?
		ORDER BY created_at DESC
		LIMIT ?
	`

	rows, err := s.db.QueryContext(ctx, query, userID, limit)
	if err != nil {
		return nil, fmt.Errorf("query recent messages: %w", err)
	}
	defer rows.Close()

	var out []MessageRecord
	for rows.Next() {
		var record MessageRecord
		if err := rows.Scan(&record.Role, &record.Content, &record.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan recent message row: %w", err)
		}
		out = append(out, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate recent messages: %w", err)
	}

	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}

	return out, nil
}

func (s *Store) UpsertAgent(ctx context.Context, record AgentRecord) error {
	const query = `
		INSERT INTO agents (name, system_prompt, model, base_url, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(name) DO UPDATE SET
			system_prompt = excluded.system_prompt,
			model = excluded.model,
			base_url = excluded.base_url,
			updated_at = excluded.updated_at
	`

	now := time.Now().UnixMilli()
	createdAt := record.CreatedAt
	if createdAt <= 0 {
		createdAt = now
	}
	updatedAt := record.UpdatedAt
	if updatedAt <= 0 {
		updatedAt = now
	}

	_, err := s.db.ExecContext(
		ctx,
		query,
		record.Name,
		record.SystemPrompt,
		record.Model,
		record.BaseURL,
		createdAt,
		updatedAt,
	)
	if err != nil {
		return fmt.Errorf("upsert agent %q: %w", record.Name, err)
	}
	return nil
}

func (s *Store) GetAgent(ctx context.Context, name string) (*AgentRecord, error) {
	const query = `
		SELECT name, system_prompt, model, base_url, created_at, updated_at
		FROM agents
		WHERE name = ?
		LIMIT 1
	`

	record := &AgentRecord{}
	err := s.db.QueryRowContext(ctx, query, name).Scan(
		&record.Name,
		&record.SystemPrompt,
		&record.Model,
		&record.BaseURL,
		&record.CreatedAt,
		&record.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("query agent %q: %w", name, err)
	}
	return record, nil
}

func (s *Store) ListAgents(ctx context.Context) ([]AgentRecord, error) {
	const query = `
		SELECT name, system_prompt, model, base_url, created_at, updated_at
		FROM agents
		ORDER BY name ASC
	`

	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("query agents: %w", err)
	}
	defer rows.Close()

	var out []AgentRecord
	for rows.Next() {
		var record AgentRecord
		if err := rows.Scan(
			&record.Name,
			&record.SystemPrompt,
			&record.Model,
			&record.BaseURL,
			&record.CreatedAt,
			&record.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan agent row: %w", err)
		}
		out = append(out, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate agents: %w", err)
	}
	return out, nil
}

func (s *Store) DeleteAgent(ctx context.Context, name string) error {
	const query = `DELETE FROM agents WHERE name = ?`
	if _, err := s.db.ExecContext(ctx, query, name); err != nil {
		return fmt.Errorf("delete agent %q: %w", name, err)
	}
	return nil
}

func (s *Store) DeleteSecret(ctx context.Context, key string) error {
	const query = `DELETE FROM secrets WHERE key = ?`
	if _, err := s.db.ExecContext(ctx, query, key); err != nil {
		return fmt.Errorf("delete secret key %q: %w", key, err)
	}
	return nil
}

func (s *Store) DeleteMessagesByUserID(ctx context.Context, userID string) error {
	const query = `DELETE FROM messages WHERE user_id = ?`
	if _, err := s.db.ExecContext(ctx, query, userID); err != nil {
		return fmt.Errorf("delete messages by user_id %q: %w", userID, err)
	}
	return nil
}

func (s *Store) DeleteMessagesByUserIDPrefix(ctx context.Context, userIDPrefix string) error {
	const query = `DELETE FROM messages WHERE user_id LIKE ?`
	if _, err := s.db.ExecContext(ctx, query, userIDPrefix+"%"); err != nil {
		return fmt.Errorf("delete messages by user_id prefix %q: %w", userIDPrefix, err)
	}
	return nil
}
