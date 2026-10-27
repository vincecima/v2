// SPDX-FileCopyrightText: Copyright The Miniflux Authors. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package model // import "miniflux.app/v2/internal/model"

// Buckets describe workflow state independently of read/unread status.
const (
	BucketInbox   = "inbox"
	BucketQueue   = "queue"
	BucketLater   = "later"
	BucketArchive = "archive"
	BucketSkipped = "skipped"
)

// EntryBucketUpdateRequest moves one entry to a workflow bucket.
type EntryBucketUpdateRequest struct {
	Bucket string `json:"bucket"`
}
