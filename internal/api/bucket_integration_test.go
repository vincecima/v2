// SPDX-FileCopyrightText: Copyright The Miniflux Authors. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package api

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"miniflux.app/v2/internal/config"
	"miniflux.app/v2/internal/database"
	"miniflux.app/v2/internal/model"
	feedHandler "miniflux.app/v2/internal/reader/handler"
	"miniflux.app/v2/internal/storage"
	"miniflux.app/v2/internal/testutil"
)

type queueFixture struct {
	db         *sql.DB
	store      *storage.Storage
	handler    http.Handler
	userID     int64
	otherID    int64
	token      string
	otherToken string
	feedID     int64
	entries    model.Entries
}

func newQueueFixture(t *testing.T) *queueFixture {
	t.Helper()
	db := testutil.Postgres(t)
	previousOptions := config.Opts
	config.Opts = config.NewConfigOptions()
	t.Cleanup(func() { config.Opts = previousOptions })
	if err := database.Migrate(db); err != nil {
		t.Fatal(err)
	}
	if err := database.IsSchemaUpToDate(db); err != nil {
		t.Fatal(err)
	}
	store := storage.NewStorage(db)
	newUser := func(name string) (int64, string) {
		user, err := store.CreateUser(&model.UserCreationRequest{Username: name})
		if err != nil {
			t.Fatal(err)
		}
		key, err := store.CreateAPIKey(user.ID, "queue test")
		if err != nil {
			t.Fatal(err)
		}
		return user.ID, key.Token
	}
	userID, token := newUser("queue-owner")
	otherID, otherToken := newUser("queue-other")
	category, err := store.FirstCategory(userID)
	if err != nil {
		t.Fatal(err)
	}
	feed := &model.Feed{UserID: userID, Title: "Videos", FeedURL: "https://example.com/feed", SiteURL: "https://example.com/", Category: category}
	if err := store.CreateFeed(feed); err != nil {
		t.Fatal(err)
	}
	entries := make(model.Entries, 5)
	for i := range entries {
		entries[i] = model.NewEntry()
		entries[i].Hash = fmt.Sprintf("video-%d", i)
		entries[i].URL = fmt.Sprintf("https://example.com/video/%d", i)
		entries[i].Title = fmt.Sprintf("Video %d", i)
		entries[i].Date = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	}
	if _, err := store.RefreshFeedEntries(userID, feed.ID, entries, true); err != nil {
		t.Fatal(err)
	}
	return &queueFixture{db, store, NewHandler(store, nil), userID, otherID, token, otherToken, feed.ID, entries}
}

func (f *queueFixture) request(t *testing.T, method, path, body, token string, want int) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	if token != "" {
		r.Header.Set("X-Auth-Token", token)
	}
	w := httptest.NewRecorder()
	f.handler.ServeHTTP(w, r)
	if w.Code != want {
		t.Fatalf("%s %s: got %d, want %d: %s", method, path, w.Code, want, w.Body.String())
	}
	return w
}

func TestQueueAPITransitionsAndOwnership(t *testing.T) {
	f := newQueueFixture(t)
	id := f.entries[0].ID
	path := fmt.Sprintf("/v1/entries/%d/bucket", id)
	f.request(t, "PUT", path, `{"bucket":"queue"}`, "", http.StatusUnauthorized)
	f.request(t, "GET", "/v1/buckets", "", "", http.StatusUnauthorized)
	f.request(t, "PUT", path, `{"bucket":"queue"}`, f.otherToken, http.StatusNotFound)
	f.request(t, "PUT", "/v1/entries/999999/bucket", `{"bucket":"queue"}`, f.token, http.StatusNotFound)
	for _, body := range []string{`{}`, `null`, `{"bucket":null}`, `{"bucket":""}`, `{"bucket":"wrong"}`, `{`} {
		f.request(t, "PUT", path, body, f.token, http.StatusBadRequest)
	}
	for _, bucket := range []string{"queue", "later", "archive", "skipped", "inbox"} {
		f.request(t, "PUT", path, fmt.Sprintf(`{"bucket":%q}`, bucket), f.token, http.StatusOK)
		entry, err := f.store.NewEntryQueryBuilder(f.userID).WithEntryIDs(id).GetEntry()
		if err != nil {
			t.Fatal(err)
		}
		if entry.Bucket != bucket || entry.Status != "unread" {
			t.Fatalf("incorrect state: %+v", entry)
		}
		changed := entry.ChangedAt
		f.request(t, "PUT", path, fmt.Sprintf(`{"bucket":%q}`, bucket), f.token, http.StatusOK)
		entry, err = f.store.NewEntryQueryBuilder(f.userID).WithEntryIDs(id).GetEntry()
		if err != nil {
			t.Fatal(err)
		}
		if !entry.ChangedAt.Equal(changed) {
			t.Fatal("repeated move changed timestamp")
		}
	}
	f.request(t, "PUT", path, `{"bucket":"archive"}`, f.token, http.StatusOK)
	f.request(t, "PUT", "/v1/entries", fmt.Sprintf(`{"entry_ids":[%d],"status":"read"}`, id), f.token, http.StatusNoContent)
	w := f.request(t, "GET", fmt.Sprintf("/v1/entries/%d", id), "", f.token, http.StatusOK)
	var entry model.Entry
	if err := json.Unmarshal(w.Body.Bytes(), &entry); err != nil {
		t.Fatal(err)
	}
	if entry.Bucket != "archive" || entry.Status != "read" {
		t.Fatalf("read and bucket state coupled: %+v", entry)
	}
	f.entries[0].Title = "Updated title from feed"
	if _, err := f.store.RefreshFeedEntries(f.userID, f.feedID, f.entries, true); err != nil {
		t.Fatal(err)
	}
	updated, err := f.store.NewEntryQueryBuilder(f.userID).WithEntryIDs(id).GetEntry()
	if err != nil {
		t.Fatal(err)
	}
	if updated.Bucket != "archive" || updated.Title != f.entries[0].Title {
		t.Fatal("feed refresh lost bucket or content update")
	}
}

func TestQueueAPIFiltersCountsAndStableOrdering(t *testing.T) {
	f := newQueueFixture(t)
	f.request(t, "GET", "/v1/entries/ids?bucket=bad", "", f.token, http.StatusBadRequest)
	f.request(t, "GET", "/v1/entries?bucket=bad", "", f.token, http.StatusBadRequest)
	f.request(t, "GET", "/v1/entries?bucket=", "", f.token, http.StatusBadRequest)
	for i, bucket := range []string{"inbox", "queue", "queue", "later", "skipped"} {
		if _, err := f.store.SetEntryBucket(f.userID, f.entries[i].ID, bucket); err != nil {
			t.Fatal(err)
		}
	}
	w := f.request(t, "GET", "/v1/buckets", "", f.token, http.StatusOK)
	var counts map[string]int64
	if err := json.Unmarshal(w.Body.Bytes(), &counts); err != nil {
		t.Fatal(err)
	}
	if len(counts) != 5 || counts["queue"] != 2 || counts["archive"] != 0 {
		t.Fatalf("incorrect counts: %v", counts)
	}
	w = f.request(t, "GET", "/v1/entries/ids?bucket=queue", "", f.token, http.StatusOK)
	var ids entryIDsResponse
	if err := json.Unmarshal(w.Body.Bytes(), &ids); err != nil {
		t.Fatal(err)
	}
	if ids.Total != 2 || len(ids.EntryIDs) != 2 || ids.EntryIDs[0] != f.entries[2].ID {
		t.Fatalf("incorrect bucket IDs: %+v", ids)
	}
	w = f.request(t, "GET", "/v1/buckets", "", f.otherToken, http.StatusOK)
	if err := json.Unmarshal(w.Body.Bytes(), &counts); err != nil {
		t.Fatal(err)
	}
	for bucket, count := range counts {
		if count != 0 {
			t.Fatalf("other user sees %s: %d", bucket, count)
		}
	}
	for _, base := range []string{"/v1/entries", fmt.Sprintf("/v1/feeds/%d/entries", f.feedID)} {
		for _, direction := range []string{"asc", "desc"} {
			for offset := 0; offset < 2; offset++ {
				w = f.request(t, "GET", fmt.Sprintf("%s?bucket=queue&order=published_at&direction=%s&limit=1&offset=%d", base, direction, offset), "", f.token, http.StatusOK)
				var result entriesResponse
				if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
					t.Fatal(err)
				}
				index := 1 + offset
				if direction == "desc" {
					index = 2 - offset
				}
				if result.Total != 2 || len(result.Entries) != 1 || result.Entries[0].ID != f.entries[index].ID {
					t.Fatalf("unstable page: %+v", result)
				}
			}
		}
	}
	w = f.request(t, "GET", "/v1/entries?bucket=queue", "", f.otherToken, http.StatusOK)
	var result entriesResponse
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Total != 0 {
		t.Fatal("entry filter leaked another user's entries")
	}
}

func TestQueueRetentionAndConcurrentMoves(t *testing.T) {
	f := newQueueFixture(t)
	for i, bucket := range []string{"inbox", "queue", "later", "archive", "skipped"} {
		if _, err := f.store.SetEntryBucket(f.userID, f.entries[i].ID, bucket); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := f.db.Exec(`UPDATE entries SET created_at=now()-interval '10 years', status='read'`); err != nil {
		t.Fatal(err)
	}
	for _, status := range []string{"read", "unread"} {
		count, err := f.store.ArchiveEntries(status, time.Hour, 100)
		if err != nil || count != 0 {
			t.Fatalf("cleanup removed workflow entries: %d %v", count, err)
		}
	}
	if err := f.store.FlushHistory(f.userID); !errors.Is(err, storage.ErrEntryRetention) {
		t.Fatalf("unexpected history flush result: %v", err)
	}
	for _, method := range []string{"PUT", "DELETE"} {
		f.request(t, method, "/v1/flush-history", "", f.token, http.StatusConflict)
	}
	counts, err := f.store.BucketCounts(f.userID)
	if err != nil {
		t.Fatal(err)
	}
	for bucket, count := range counts {
		if count != 1 {
			t.Fatalf("retention changed %s: %d", bucket, count)
		}
	}
	errorsCh := make(chan error, 20)
	for i := 0; i < 20; i++ {
		go func(i int) {
			bucket := "queue"
			if i%2 == 0 {
				bucket = "later"
			}
			_, err := f.store.SetEntryBucket(f.userID, f.entries[0].ID, bucket)
			errorsCh <- err
		}(i)
	}
	for range 20 {
		if err := <-errorsCh; err != nil {
			t.Fatal(err)
		}
	}
	entry, err := f.store.NewEntryQueryBuilder(f.userID).WithEntryIDs(f.entries[0].ID).GetEntry()
	if err != nil {
		t.Fatal(err)
	}
	if entry.Bucket != "queue" && entry.Bucket != "later" {
		t.Fatalf("invalid concurrent result: %q", entry.Bucket)
	}
	if _, err := f.store.SetEntryBucket(f.otherID, entry.ID, "skipped"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("cross-user storage update: %v", err)
	}
}

func TestQueueUnsubscribePreservesEntriesAndSkipsRefresh(t *testing.T) {
	f := newQueueFixture(t)
	path := fmt.Sprintf("/v1/feeds/%d/subscription", f.feedID)
	f.request(t, "PUT", path, `{"subscribed":false}`, "", http.StatusUnauthorized)
	f.request(t, "PUT", path, `{"subscribed":false}`, f.otherToken, http.StatusNotFound)
	f.request(t, "PUT", path, `{}`, f.token, http.StatusBadRequest)
	inflightFeed, err := f.store.FeedByID(f.userID, f.feedID)
	if err != nil || inflightFeed == nil {
		t.Fatalf("load active feed: %v", err)
	}
	if _, err := f.store.SetEntryBucket(f.userID, f.entries[0].ID, "later"); err != nil {
		t.Fatal(err)
	}
	f.request(t, "PUT", path, `{"subscribed":false}`, f.token, http.StatusOK)
	// Finishing a refresh with a stale settings snapshot must not resubscribe.
	inflightFeed.EtagHeader = "new-etag"
	if err := f.store.UpdateFeedAfterRefresh(inflightFeed); err != nil {
		t.Fatal(err)
	}
	savedFeed, err := f.store.FeedByID(f.userID, f.feedID)
	if err != nil || savedFeed == nil || !savedFeed.Disabled || savedFeed.EtagHeader != "new-etag" {
		t.Fatalf("refresh overwrote subscription or lost metadata: %v %v", savedFeed, err)
	}
	// The fake feed URL must never be fetched, even by a previously queued job
	// or a forced direct refresh. No network server is needed for this assertion.
	if err := feedHandler.RefreshFeed(f.store, f.userID, f.feedID, true); err != nil {
		t.Fatal(err)
	}
	jobs, err := f.store.NewBatchBuilder().WithUserID(f.userID).WithoutDisabledFeeds().FetchJobs()
	if err != nil || len(jobs) != 0 {
		t.Fatalf("unsubscribed feed was scheduled: %v %v", jobs, err)
	}
	f.request(t, "PUT", path, `{"subscribed":true}`, f.token, http.StatusOK)
	jobs, err = f.store.NewBatchBuilder().WithUserID(f.userID).WithoutDisabledFeeds().FetchJobs()
	if err != nil || len(jobs) != 1 {
		t.Fatalf("resubscribed feed was not scheduled: %v %v", jobs, err)
	}
	entries, err := f.store.NewEntryQueryBuilder(f.userID).WithFeedID(f.feedID).WithSorting("id", "asc").GetEntries()
	if err != nil || len(entries) != 5 || entries[0].Bucket != "later" {
		t.Fatalf("unsubscribe lost entries: %v %v", entries, err)
	}
}
