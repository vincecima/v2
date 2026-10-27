// SPDX-FileCopyrightText: Copyright The Miniflux Authors. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package storage // import "miniflux.app/v2/internal/storage"

import (
	"fmt"

	"miniflux.app/v2/internal/model"
)

// SetEntryBucket changes only workflow state. sql.ErrNoRows means the entry
// does not exist or does not belong to this user. The database validates buckets.
func (s *Storage) SetEntryBucket(userID, entryID int64, bucket string) (string, error) {
	var persisted string
	err := s.db.QueryRow(`UPDATE entries SET bucket=$1,
		changed_at=CASE WHEN bucket IS DISTINCT FROM $1 THEN now() ELSE changed_at END
		WHERE user_id=$2 AND id=$3 RETURNING bucket`, bucket, userID, entryID).Scan(&persisted)
	if err != nil {
		return "", fmt.Errorf("store: unable to move entry #%d: %w", entryID, err)
	}
	return persisted, nil
}

// BucketCounts includes zero-count buckets and only the current user's entries.
func (s *Storage) BucketCounts(userID int64) (map[string]int64, error) {
	counts := map[string]int64{
		model.BucketInbox: 0, model.BucketQueue: 0, model.BucketLater: 0,
		model.BucketArchive: 0, model.BucketSkipped: 0,
	}
	rows, err := s.db.Query(`SELECT bucket, count(*) FROM entries WHERE user_id=$1 GROUP BY bucket`, userID)
	if err != nil {
		return nil, fmt.Errorf("store: unable to count buckets: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var bucket string
		var count int64
		if err := rows.Scan(&bucket, &count); err != nil {
			return nil, err
		}
		counts[bucket] = count
	}
	return counts, rows.Err()
}
