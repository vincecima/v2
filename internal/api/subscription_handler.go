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
)

func (h *handler) setFeedSubscriptionHandler(w http.ResponseWriter, r *http.Request) {
	feedID := request.RouteInt64Param(r, "feedID")
	if feedID <= 0 {
		response.JSONBadRequest(w, r, errors.New("invalid feed ID"))
		return
	}
	var input struct {
		Subscribed *bool `json:"subscribed"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		response.JSONBadRequest(w, r, err)
		return
	}
	if input.Subscribed == nil {
		response.JSONBadRequest(w, r, errors.New("subscribed is required"))
		return
	}
	subscribed, err := h.store.SetFeedSubscription(request.UserID(r), feedID, *input.Subscribed)
	if errors.Is(err, sql.ErrNoRows) {
		response.JSONNotFound(w, r)
		return
	}
	if err != nil {
		response.JSONServerError(w, r, err)
		return
	}
	response.JSON(w, r, struct {
		ID         int64 `json:"id"`
		Subscribed bool  `json:"subscribed"`
	}{feedID, subscribed})
}
