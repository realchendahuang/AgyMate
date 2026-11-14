package rpc

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"testing"

	"github.com/realchendahuang/AgyMate/internal/instances"
)

func TestFetchInstanceMockRPC(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("x-codeium-csrf-token") != "tok-123" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		switch r.URL.Path {
		case servicePrefix + "GetUserStatus":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"userStatus": map[string]any{
					"name":  "chendahuang",
					"email": "realchendahuang@gmail.com",
					"userTier": map[string]any{
						"id":   "g1-ultra-tier",
						"name": "Google AI Ultra",
					},
					"cascadeModelConfigData": map[string]any{
						"clientModelConfigs": []any{
							map[string]any{
								"label":        "Gemini 4 Hydrogen (Medium)",
								"modelId":      "gemini-4-hydrogen-medium",
								"modelOrAlias": map[string]any{"model": "MODEL_PLACEHOLDER_M667"},
								"quotaInfo":    map[string]any{"remainingFraction": 0.92, "resetTime": "2026-11-14T15:00:00Z"},
							},
						},
					},
				},
			})
		case servicePrefix + "RetrieveUserQuotaSummary":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"response": map[string]any{
					"groups": []any{
						map[string]any{
							"displayName": "Gemini Models",
							"buckets": []any{
								map[string]any{
									"bucketId":          "gemini-5h",
									"displayName":       "Five Hour Limit Remaining",
									"window":            "5h",
									"remainingFraction": 0.98,
									"resetTime":         "2026-11-14T08:00:00Z",
								},
							},
						},
					},
				},
			})
		case servicePrefix + "GetAllCascadeTrajectories":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"trajectorySummaries": map[string]any{
					"cid-1": map[string]any{
						"summary":          "Test Cascade",
						"stepCount":        10,
						"status":           "CASCADE_RUN_STATUS_RUNNING",
						"lastModifiedTime": "2026-11-14T04:00:00Z",
						"trajectoryType":   "CORTEX_TRAJECTORY_TYPE_CASCADE",
					},
				},
			})
		case servicePrefix + "GetCascadeTrajectoryGeneratorMetadata":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"generatorMetadata": []any{
					map[string]any{
						"chatModel": map[string]any{
							"model": "MODEL_PLACEHOLDER_M667",
							"usage": map[string]any{
								"inputTokens":     "3000",
								"cacheReadTokens": "150000",
							},
							"chatStartMetadata": map[string]any{
								"checkpointIndex": -1,
								"contextWindowMetadata": map[string]any{
									"estimatedTokensUsed": 160000,
									"maxContextTokens":    256000,
									"tokenBreakdown": map[string]any{
										"groups": []any{
											map[string]any{"name": "System Prompt", "numTokens": 15000},
										},
									},
								},
							},
						},
					},
				},
			})
		}
	}))
	defer srv.Close()

	u, _ := url.Parse(srv.URL)
	port, _ := strconv.Atoi(u.Port())

	client := NewClient()
	snap := client.FetchInstance(context.Background(), "main", &instances.HubInfo{
		PID:       1,
		HTTPPort:  port,
		CSRFToken: "tok-123",
	}, nil)

	if snap.Email != "realchendahuang@gmail.com" || snap.PlanName != "Google AI Ultra" {
		t.Fatalf("unexpected user info: %+v", snap)
	}
	if len(snap.Buckets) != 1 || snap.Buckets[0].BucketID != "gemini-5h" {
		t.Fatalf("unexpected buckets: %+v", snap.Buckets)
	}
	if snap.Context == nil || snap.Context.EstimatedTokensUsed != 160000 || snap.Context.Model != "Gemini 4 Hydrogen (Medium)" {
		t.Fatalf("unexpected context: %+v", snap.Context)
	}
}
