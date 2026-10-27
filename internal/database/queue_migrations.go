// SPDX-FileCopyrightText: Copyright The Miniflux Authors. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package database // import "miniflux.app/v2/internal/database"

import (
	"database/sql"
	"fmt"
)

// Fork migrations have their own ledger so future upstream migration numbers
// cannot collide with migrations already applied by this fork.
var queueMigrations = []string{
	`ALTER TABLE entries ADD COLUMN bucket text NOT NULL DEFAULT 'inbox'
		CONSTRAINT entries_bucket_check CHECK (bucket IN ('inbox', 'queue', 'later', 'archive', 'skipped'));
	 CREATE INDEX entries_user_bucket_published_id_idx ON entries (user_id, bucket, published_at, id);`,
}

func migrateQueue(db *sql.DB) error {
	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("queue migrations: %w", err)
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`CREATE TABLE IF NOT EXISTS queue_schema_version (
		id integer PRIMARY KEY CHECK (id = 1), version integer NOT NULL
	);
	INSERT INTO queue_schema_version (id, version) VALUES (1, 0) ON CONFLICT DO NOTHING;`); err != nil {
		return fmt.Errorf("queue migration ledger: %w", err)
	}
	var current int
	if err := tx.QueryRow(`SELECT version FROM queue_schema_version WHERE id=1 FOR UPDATE`).Scan(&current); err != nil {
		return fmt.Errorf("queue migration version: %w", err)
	}
	if current > len(queueMigrations) {
		return fmt.Errorf("queue schema is newer than this binary: current=%d expected=%d", current, len(queueMigrations))
	}
	for i := current; i < len(queueMigrations); i++ {
		if _, err := tx.Exec(queueMigrations[i]); err != nil {
			return fmt.Errorf("queue migration %d: %w", i+1, err)
		}
		if _, err := tx.Exec(`UPDATE queue_schema_version SET version=$1 WHERE id=1`, i+1); err != nil {
			return fmt.Errorf("queue migration version: %w", err)
		}
	}
	return tx.Commit()
}

func isQueueSchemaUpToDate(db *sql.DB) error {
	var current int
	if err := db.QueryRow(`SELECT version FROM queue_schema_version WHERE id=1`).Scan(&current); err != nil {
		return fmt.Errorf("queue schema is unavailable; run migrations: %w", err)
	}
	if current != len(queueMigrations) {
		return fmt.Errorf("queue schema version mismatch: current=%d expected=%d", current, len(queueMigrations))
	}
	return nil
}
