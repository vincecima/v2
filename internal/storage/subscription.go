// SPDX-FileCopyrightText: Copyright The Miniflux Authors. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package storage // import "miniflux.app/v2/internal/storage"

import "fmt"

// SetFeedSubscription stops or resumes polling without deleting the source or
// any of its entries. sql.ErrNoRows covers both missing and unowned feeds.
func (s *Storage) SetFeedSubscription(userID, feedID int64, subscribed bool) (bool, error) {
	var disabled bool
	err := s.db.QueryRow(`UPDATE feeds SET disabled=$1 WHERE user_id=$2 AND id=$3 RETURNING disabled`,
		!subscribed, userID, feedID).Scan(&disabled)
	if err != nil {
		return false, fmt.Errorf("store: unable to update feed subscription: %w", err)
	}
	return !disabled, nil
}
