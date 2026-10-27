// SPDX-FileCopyrightText: Copyright The Miniflux Authors. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package database

import (
	"testing"

	"miniflux.app/v2/internal/testutil"
)

func TestQueueMigrationBackfillsAndValidatesBuckets(t *testing.T) {
	db := testutil.Postgres(t)
	if _, err := db.Exec(`CREATE TABLE entries (
		id bigint PRIMARY KEY, user_id bigint, published_at timestamptz, status text
	); INSERT INTO entries VALUES (1, 1, now(), 'read');`); err != nil {
		t.Fatal(err)
	}
	if err := isQueueSchemaUpToDate(db); err == nil {
		t.Fatal("unmigrated queue schema accepted")
	}
	for range 2 {
		if err := migrateQueue(db); err != nil {
			t.Fatal(err)
		}
	}
	if err := isQueueSchemaUpToDate(db); err != nil {
		t.Fatal(err)
	}
	var bucket, status string
	if err := db.QueryRow(`SELECT bucket, status FROM entries WHERE id=1`).Scan(&bucket, &status); err != nil {
		t.Fatal(err)
	}
	if bucket != "inbox" || status != "read" {
		t.Fatalf("backfill changed existing status or missed inbox: %s/%s", bucket, status)
	}
	for _, value := range []any{"invalid", "", nil} {
		if _, err := db.Exec(`UPDATE entries SET bucket=$1 WHERE id=1`, value); err == nil {
			t.Fatalf("database accepted invalid bucket %v", value)
		}
	}
	if _, err := db.Exec(`UPDATE queue_schema_version SET version=999`); err != nil {
		t.Fatal(err)
	}
	if err := isQueueSchemaUpToDate(db); err == nil {
		t.Fatal("newer schema accepted")
	}
	if err := migrateQueue(db); err == nil {
		t.Fatal("migration accepted a newer schema")
	}
}
