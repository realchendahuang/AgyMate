package telemetry

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPricingOverridesHotReload(t *testing.T) {
	dir := t.TempDir()
	pricingPath := filepath.Join(dir, "pricing.json")
	t.Setenv("AGYMATE_PRICING", pricingPath)

	// Without file, fallback defaults work.
	def := PricingForModel("MyCustomFutureModel", "")
	if def.StdIn != 4.00 {
		t.Fatalf("expected default StdIn=4.00, got %.2f", def.StdIn)
	}

	raw := `{
  "enums": {
    "9999": "Future Model X"
  },
  "models": [
    {
      "match": "future model x",
      "stdIn": 1.25,
      "stdCache": 0.12,
      "stdOut": 6.50
    }
  ]
}`
	if err := os.WriteFile(pricingPath, []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}

	if gotName := resolveModelName(9999, "", "", nil); gotName != "Future Model X" {
		t.Fatalf("expected enum override 'Future Model X', got %q", gotName)
	}
	gotPrice := PricingForModel("Future Model X", "")
	if gotPrice.StdIn != 1.25 || gotPrice.StdCache != 0.12 || gotPrice.StdOut != 6.50 {
		t.Fatalf("unexpected overridden pricing: %+v", gotPrice)
	}
	// Promo fields fall back to Std when omitted.
	if gotPrice.PromoIn != 1.25 || gotPrice.PromoOut != 6.50 {
		t.Fatalf("expected Promo fallback to Std, got %+v", gotPrice)
	}
}
