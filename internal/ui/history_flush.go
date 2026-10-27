// SPDX-FileCopyrightText: Copyright The Miniflux Authors. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package ui // import "miniflux.app/v2/internal/ui"

import (
	"net/http"

	"miniflux.app/v2/internal/http/response"
)

func (h *handler) flushHistory(w http.ResponseWriter, r *http.Request) {
	response.NewBuilder(w, r).
		WithStatus(http.StatusConflict).
		WithHeader("Content-Type", "application/json").
		WithBodyAsString(`{"error_message":"history deletion is disabled: media queue entries are retained"}`).
		Write()
}
