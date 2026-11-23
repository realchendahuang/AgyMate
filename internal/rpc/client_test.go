package rpc

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"testing"

	"github.com/realchendahuang/AgyMate/internal/config"
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
					"name":  "Dev User",
					"email": "dev@example.com",
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

	if snap.Email != "dev@example.com" || snap.PlanName != "Google AI Ultra" {
		t.Fatalf("unexpected user info: %+v", snap)
	}
	if len(snap.Buckets) != 1 || snap.Buckets[0].BucketID != "gemini-5h" {
		t.Fatalf("unexpected buckets: %+v", snap.Buckets)
	}
	if snap.Buckets[0].DisplayName != "Gemini · 5小时限额" {
		t.Fatalf("expected localized bucket DisplayName 'Gemini · 5小时限额', got %q", snap.Buckets[0].DisplayName)
	}
	if snap.Context == nil || snap.Context.EstimatedTokensUsed != 160000 || snap.Context.Model != "Gemini 4 Hydrogen (Medium)" {
		t.Fatalf("unexpected context: %+v", snap.Context)
	}
	if len(snap.Context.TokenBreakdown) != 1 || snap.Context.TokenBreakdown[0].Name != "系统提示词" {
		t.Fatalf("expected localized context breakdown '系统提示词', got %+v", snap.Context.TokenBreakdown)
	}
}

func TestFetchInstanceDisabledBucketSkipsStateRecording(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case servicePrefix + "GetUserStatus":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"userStatus": map[string]any{
					"email": "pro@example.com",
					"planStatus": map[string]any{
						"planInfo": map[string]any{"planName": "Pro"},
					},
				},
			})
		case servicePrefix + "RetrieveUserQuotaSummary":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"response": map[string]any{
					"groups": []any{
						map[string]any{
							"displayName": "Claude and GPT models",
							"buckets": []any{
								map[string]any{
									"bucketId":          "3p-weekly",
									"displayName":       "Weekly Limit Remaining",
									"window":            "weekly",
									"remainingFraction": 0,
									"resetTime":         "2026-11-25T03:20:28Z",
								},
								map[string]any{
									"bucketId":          "3p-5h",
									"displayName":       "Five Hour Limit Remaining",
									"description":       "You have hit your weekly limit, the 5-hour limit does not currently apply.",
									"window":            "5h",
									"disabled":          true,
									"remainingFraction": 1,
									"resetTime":         "2026-11-22T13:33:29Z",
								},
							},
						},
					},
				},
			})
		default:
			_ = json.NewEncoder(w).Encode(map[string]any{})
		}
	}))
	defer srv.Close()

	u, _ := url.Parse(srv.URL)
	port, _ := strconv.Atoi(u.Port())

	t.Setenv("AGYMATE_STATE", t.TempDir()+"/state.json")
	st := config.LoadState("")
	client := NewClient()
	snap := client.FetchInstance(context.Background(), "main", &instances.HubInfo{
		PID:      1,
		HTTPPort: port,
	}, st)

	if len(snap.Buckets) != 2 {
		t.Fatalf("expected 2 buckets, got %+v", snap.Buckets)
	}
	if !snap.Buckets[0].Disabled || snap.Buckets[0].BucketID != "3p-5h" {
		t.Fatalf("expected first sorted bucket to be disabled 3p-5h, got %+v", snap.Buckets[0])
	}
	if series := st.QuotaSeries("main:3p-5h", 10); len(series["main:3p-5h"]) != 0 {
		t.Fatalf("disabled bucket must not record history samples, got %+v", series)
	}
	if series := st.QuotaSeries("main:3p-weekly", 10); len(series["main:3p-weekly"]) != 1 {
		t.Fatalf("active bucket should record history sample, got %+v", series)
	}
}
