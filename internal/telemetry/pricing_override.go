package telemetry

import (
	"encoding/json"
	"os"
	"strconv"
	"strings"
	"sync"

	"github.com/realchendahuang/AgyMate/internal/config"
)

// PricingOverrideRule matches a model name/ID substring (case-insensitive) to custom per-1M USD prices.
type PricingOverrideRule struct {
	Match      string  `json:"match"`
	PromoIn    float64 `json:"promoIn,omitempty"`
	PromoCache float64 `json:"promoCache,omitempty"`
	PromoOut   float64 `json:"promoOut,omitempty"`
	StdIn      float64 `json:"stdIn"`
	StdCache   float64 `json:"stdCache"`
	StdOut     float64 `json:"stdOut"`
}

// PricingOverrides defines optional user overrides loaded from ~/.config/agymate/pricing.json.
type PricingOverrides struct {
	Enums  map[string]string     `json:"enums,omitempty"`
	Models []PricingOverrideRule `json:"models,omitempty"`
}

type pricingCacheState struct {
	mu      sync.Mutex
	path    string
	exists  bool
	mtime   int64
	size    int64
	enums   map[uint64]string
	rules   []compiledPricingRule
	checked bool
}

type compiledPricingRule struct {
	matchLower string
	pricing    ModelPricing
}

var pricingCache pricingCacheState

func loadActivePricingOverrides() (map[uint64]string, []compiledPricingRule) {
	p := config.PricingPath()
	info, err := os.Stat(p)

	pricingCache.mu.Lock()
	defer pricingCache.mu.Unlock()

	if err != nil || info.IsDir() {
		if pricingCache.path == p && !pricingCache.exists && pricingCache.checked {
			return nil, nil
		}
		pricingCache.path = p
		pricingCache.exists = false
		pricingCache.mtime = 0
		pricingCache.size = 0
		pricingCache.enums = nil
		pricingCache.rules = nil
		pricingCache.checked = true
		return nil, nil
	}

	mt := info.ModTime().UnixNano()
	sz := info.Size()
	if pricingCache.path == p && pricingCache.exists && pricingCache.mtime == mt && pricingCache.size == sz {
		return pricingCache.enums, pricingCache.rules
	}

	raw, rerr := os.ReadFile(p)
	if rerr != nil {
		return pricingCache.enums, pricingCache.rules
	}
	var doc PricingOverrides
	if jerr := json.Unmarshal(raw, &doc); jerr != nil {
		return pricingCache.enums, pricingCache.rules
	}

	enums := make(map[uint64]string, len(doc.Enums))
	for k, v := range doc.Enums {
		v = strings.TrimSpace(v)
		if v == "" {
			continue
		}
		if id, perr := strconv.ParseUint(strings.TrimSpace(k), 10, 64); perr == nil {
			enums[id] = v
		}
	}

	rules := make([]compiledPricingRule, 0, len(doc.Models))
	for _, r := range doc.Models {
		m := strings.ToLower(strings.TrimSpace(r.Match))
		if m == "" {
			continue
		}
		mp := ModelPricing{
			StdIn:      r.StdIn,
			StdCache:   r.StdCache,
			StdOut:     r.StdOut,
			PromoIn:    r.PromoIn,
			PromoCache: r.PromoCache,
			PromoOut:   r.PromoOut,
		}
		if mp.PromoIn == 0 && mp.StdIn > 0 {
			mp.PromoIn = mp.StdIn
		}
		if mp.PromoCache == 0 && mp.StdCache > 0 {
			mp.PromoCache = mp.StdCache
		}
		if mp.PromoOut == 0 && mp.StdOut > 0 {
			mp.PromoOut = mp.StdOut
		}
		rules = append(rules, compiledPricingRule{matchLower: m, pricing: mp})
	}

	pricingCache.path = p
	pricingCache.exists = true
	pricingCache.mtime = mt
	pricingCache.size = sz
	pricingCache.enums = enums
	pricingCache.rules = rules
	pricingCache.checked = true
	return enums, rules
}

func lookupPricingOverride(low string) (ModelPricing, bool) {
	_, rules := loadActivePricingOverrides()
	for _, r := range rules {
		if strings.Contains(low, r.matchLower) {
			return r.pricing, true
		}
	}
	return ModelPricing{}, false
}

func lookupEnumOverride(me uint64) (string, bool) {
	enums, _ := loadActivePricingOverrides()
	if len(enums) == 0 {
		return "", false
	}
	name, ok := enums[me]
	return name, ok
}
