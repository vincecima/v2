// SPDX-FileCopyrightText: Copyright The Miniflux Authors. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package api // import "miniflux.app/v2/internal/api"

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"

	"miniflux.app/v2/internal/http/request"
	"miniflux.app/v2/internal/http/response"
	"miniflux.app/v2/internal/model"
	"miniflux.app/v2/internal/validator"
)

func (h *handler) setEntryBucketHandler(w http.ResponseWriter, r *http.Request) {
	entryID := request.RouteInt64Param(r, "entryID")
	if entryID <= 0 {
		response.JSONBadRequest(w, r, errors.New("invalid entry ID"))
		return
	}
	var input model.EntryBucketUpdateRequest
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		response.JSONBadRequest(w, r, err)
		return
	}
	if err := validator.ValidateEntryBucket(input.Bucket); err != nil {
		response.JSONBadRequest(w, r, err)
		return
	}
	bucket, err := h.store.SetEntryBucket(request.UserID(r), entryID, input.Bucket)
	if errors.Is(err, sql.ErrNoRows) {
		response.JSONNotFound(w, r)
		return
	}
	if err != nil {
		response.JSONServerError(w, r, err)
		return
	}
	response.JSON(w, r, struct {
		ID     int64  `json:"id"`
		Bucket string `json:"bucket"`
	}{entryID, bucket})
}

func (h *handler) bucketCountsHandler(w http.ResponseWriter, r *http.Request) {
	counts, err := h.store.BucketCounts(request.UserID(r))
	if err != nil {
		response.JSONServerError(w, r, err)
		return
	}
	response.JSON(w, r, counts)
}
