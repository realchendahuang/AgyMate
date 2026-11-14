// Package telemetry parses Antigravity's local SQLite WAL conversation databases
// in strictly read-only mode to compute token consumption, API valuation, trends,
// cross-instance FTS5 search, and Brain/Conversation cache cleanup.
package telemetry

import (
	"fmt"
	"strings"
)

// ModelEnumNames maps known Antigravity protobuf model enum IDs to display names.
var ModelEnumNames = map[uint64]string{
	1318: "Gemini 3.8 Flash (High)",
	1319: "Gemini 3.8 Flash (Medium)",
	1320: "Gemini 3.8 Flash (Low)",
	1322: "Gemini 3.8 Flash Tiered",
	1401: "Claude Opus 5.5 (Medium)",
	1402: "Claude Opus 5.5 (High)",
	1403: "Claude Opus 5.5 (Low)",
	1404: "Claude Sonnet 5.5 (Medium)",
	1405: "Claude Sonnet 5.5 (High)",
	1417: "Gemini 4 Argon (High)",
	1418: "Gemini 4 Argon (Medium)",
	1419: "Gemini 4 Argon (Low)",
	1427: "Gemini 4 Carbon (High)",
	1428: "Gemini 4 Carbon (Medium)",
	1525: "Gemini 4 Hydrogen (High)",
	1526: "Gemini 4 Hydrogen (Medium)",
	1528: "Gemini 4 Hydrogen (XHigh)",
	1667: "Orchestrator (Carbon)",
	1669: "Gemini Flash",
}

// ModelPricing stores per-1M-token prices in USD.
type ModelPricing struct {
	PromoIn    float64
	PromoCache float64
	PromoOut   float64
	StdIn      float64
	StdCache   float64
	StdOut     float64
}

// PricingForModel returns promo and standard rates per 1M tokens.
func PricingForModel(modelName, modelID string) ModelPricing {
	low := strings.ToLower(modelName + " " + modelID)
	switch {
	case strings.Contains(low, "opus"):
		return ModelPricing{4.00, 0.20, 20.00, 4.00, 0.20, 20.00}
	case strings.Contains(low, "sonnet"):
		return ModelPricing{3.00, 0.30, 15.00, 3.00, 0.30, 15.00}
	case strings.Contains(low, "flash"):
		return ModelPricing{0.75, 0.075, 3.75, 1.50, 0.15, 7.50}
	default:
		// Gemini 4 Argon / Carbon / Hydrogen / Orchestrator
		return ModelPricing{2.00, 0.10, 10.00, 4.00, 0.20, 20.00}
	}
}

// CostComponents holds the 4 USD breakdown parts for one call or aggregate.
type CostComponents struct {
	InputUSD    float64 `json:"inputUsd"`
	CacheUSD    float64 `json:"cacheUsd"`
	OutputUSD   float64 `json:"outputUsd"`
	ThinkingUSD float64 `json:"thinkingUsd"`
	TotalUSD    float64 `json:"totalUsd"`
}

// ComputeCallCost calculates USD cost under mode: "promo", "std", "nc_promo", "nc_std".
func ComputeCallCost(modelName, modelID string, inp, cache, out, think, resp int64, mode string) CostComponents {
	p := PricingForModel(modelName, modelID)
	var rIn, rCache, rOut float64
	switch mode {
	case "promo":
		rIn, rCache, rOut = p.PromoIn, p.PromoCache, p.PromoOut
	case "nc_promo":
		rIn, rCache, rOut = p.PromoIn, p.PromoIn, p.PromoOut
	case "nc_std":
		rIn, rCache, rOut = p.StdIn, p.StdIn, p.StdOut
	default: // "std"
		rIn, rCache, rOut = p.StdIn, p.StdCache, p.StdOut
	}

	cIn := float64(inp) * rIn / 1_000_000.0
	cCache := float64(cache) * rCache / 1_000_000.0

	// If thinking/resp subfields are both zero while out > 0, attribute output to cOut.
	var cThink, cResp float64
	if think > 0 || resp > 0 {
		cThink = float64(think) * rOut / 1_000_000.0
		if resp > 0 {
			cResp = float64(resp) * rOut / 1_000_000.0
		} else if out > think {
			cResp = float64(out-think) * rOut / 1_000_000.0
		}
	} else {
		cResp = float64(out) * rOut / 1_000_000.0
	}

	return CostComponents{
		InputUSD:    cIn,
		CacheUSD:    cCache,
		OutputUSD:   cResp,
		ThinkingUSD: cThink,
		TotalUSD:    cIn + cCache + cResp + cThink,
	}
}

// CallRecord represents one parsed LLM call from conversations/*.db.
type CallRecord struct {
	Timestamp   int64
	InstanceID  string
	CascadeID   string
	ModelEnum   uint64
	ModelID     string
	ModelName   string
	Input       int64
	CacheRead   int64
	Output      int64
	Thinking    int64
	ResponseOut int64
}

// ImageEvent records one generate_image tool invocation ($0.04 unit cost).
type ImageEvent struct {
	Timestamp  int64
	InstanceID string
	CascadeID  string
}

type protoField struct {
	varint uint64
	bytes  []byte
	isVar  bool
}

type protoDict map[uint64][]protoField

func parseVarint(data []byte, i int) (uint64, int, bool) {
	var val uint64
	var shift uint
	n := len(data)
	for i < n {
		b := data[i]
		i++
		val |= uint64(b&0x7F) << shift
		if b&0x80 == 0 {
			return val, i, true
		}
		shift += 7
		if shift >= 64 {
			return 0, i, false
		}
	}
	return val, i, false
}

func parseProtoDict(data []byte) protoDict {
	res := make(protoDict)
	i := 0
	n := len(data)
	for i < n {
		tag, next, ok := parseVarint(data, i)
		if !ok || tag == 0 {
			break
		}
		i = next
		fn := tag >> 3
		wt := tag & 7
		switch wt {
		case 0:
			val, nextV, okV := parseVarint(data, i)
			if !okV {
				return res
			}
			i = nextV
			res[fn] = append(res[fn], protoField{varint: val, isVar: true})
		case 1:
			if i+8 > n {
				return res
			}
			res[fn] = append(res[fn], protoField{bytes: data[i : i+8]})
			i += 8
		case 2:
			ln, nextL, okL := parseVarint(data, i)
			if !okL {
				return res
			}
			i = nextL
			if ln > uint64(n-i) {
				return res
			}
			end := i + int(ln)
			res[fn] = append(res[fn], protoField{bytes: data[i:end]})
			i = end
		case 5:
			if i+4 > n {
				return res
			}
			res[fn] = append(res[fn], protoField{bytes: data[i : i+4]})
			i += 4
		default:
			return res
		}
	}
	return res
}

func (d protoDict) firstVarint(fn uint64) uint64 {
	fs := d[fn]
	if len(fs) == 0 {
		return 0
	}
	return fs[0].varint
}

func (d protoDict) firstBytes(fn uint64) []byte {
	fs := d[fn]
	if len(fs) == 0 {
		return nil
	}
	return fs[0].bytes
}

func (d protoDict) firstString(fn uint64) string {
	b := d.firstBytes(fn)
	if len(b) == 0 {
		return ""
	}
	return string(b)
}

func resolveModelName(me uint64, mid, mn string, labelFn func(string) string) string {
	if mn != "" {
		return mn
	}
	if labelFn != nil && mid != "" {
		if l := labelFn(mid); l != "" {
			return l
		}
	}
	if n, ok := ModelEnumNames[me]; ok {
		return n
	}
	if mid != "" {
		return mid
	}
	return fmt.Sprintf("Model(%d)", me)
}
