// Copyright 2026 Kurok1
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package httpserver

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

type errorBody struct {
	Error errorDetail `json:"error"`
}

type errorDetail struct {
	Message string `json:"message"`
	Type    string `json:"type"`
}

func ioReadAllLimit(r *http.Request, maxBytes int64) ([]byte, error) {
	limited := http.MaxBytesReader(nil, r.Body, maxBytes)
	body, err := io.ReadAll(limited)
	if err != nil {
		return nil, fmt.Errorf("read request body: %w", err)
	}
	if len(body) == 0 {
		return nil, fmt.Errorf("request body cannot be empty")
	}
	return body, nil
}

func writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(data); err != nil {
		http.Error(w, `{"error":{"message":"encode response failed","type":"server_error"}}`, http.StatusInternalServerError)
	}
}

func writeError(w http.ResponseWriter, status int, message, errType string) {
	writeJSON(w, status, errorBody{Error: errorDetail{Message: message, Type: errType}})
}

func writeUpstreamError(w http.ResponseWriter, status int, upstreamBody []byte) {
	var parsed errorBody
	if err := json.Unmarshal(upstreamBody, &parsed); err == nil && parsed.Error.Message != "" {
		writeJSON(w, status, parsed)
		return
	}
	writeError(w, status, string(upstreamBody), "upstream_error")
}
