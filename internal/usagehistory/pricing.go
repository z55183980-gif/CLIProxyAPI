package usagehistory

import (
	_ "embed"
	"encoding/json"
	"regexp"
	"strconv"
	"strings"

	"github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/usage"
)

// This snapshot combines sub2api's price catalog and billing fallbacks; see PRICING.md.
// Explicit management prices take precedence; unknown models remain unpriced.
//
//go:embed model_prices.json
var modelPricesJSON []byte

var modelPrices = func() map[string]map[string]float64 {
	var prices map[string]map[string]float64
	_ = json.Unmarshal(modelPricesJSON, &prices)
	return prices
}()

var priceDateSuffix = regexp.MustCompile(`-(?:\d{8}|\d{4}-\d{2}-\d{2})$`)
var priceContextSuffix = regexp.MustCompile(`^input_cost_per_token_above_(\d+)k_tokens$`)
var claudeVersion = regexp.MustCompile(`(claude-(?:opus|sonnet|haiku|fable)-\d+)\.(\d+)`)

func defaultPrice(r Record) *Price {
	model := strings.ToLower(strings.TrimSpace(r.Model))
	effort := strings.ToLower(strings.TrimSpace(r.ReasoningEffort))
	for _, prefix := range []string{"openai/", "anthropic/", "google/"} {
		model = strings.TrimPrefix(model, prefix)
	}
	if index := strings.IndexByte(model, '('); index >= 0 {
		if effort == "" {
			effort = strings.TrimSuffix(model[index+1:], ")")
		}
		model = model[:index]
	}
	model = claudeVersion.ReplaceAllString(model, "$1-$2")
	// Exact dated entries take precedence over their undated aliases.
	data, ok := modelPrices[model]
	if !ok {
		// Strip only recognized modifiers; future model families remain unpriced.
		for _, suffix := range []string{"-thinking", "-ultra", "-max", "-xhigh", "-high", "-medium", "-low", "-minimal", "-none"} {
			if base, found := strings.CutSuffix(model, suffix); found {
				model = base
				if effort == "" {
					effort = strings.TrimPrefix(suffix, "-")
				}
				break
			}
		}
		data, ok = modelPrices[model]
	}
	if !ok {
		model = priceDateSuffix.ReplaceAllString(model, "")
		data, ok = modelPrices[model]
	}
	if !ok {
		switch model {
		case "gpt-6":
			model = "gpt-6-astra"
		case "gpt-5.6":
			model = "gpt-5.6-sol"
		}
		data, ok = modelPrices[model]
	}
	if !ok {
		return nil
	}
	suffix := ""
	var threshold int64
	input := r.InputTokens + r.CacheReadTokens + r.CacheWriteTokens
	for key := range data {
		match := priceContextSuffix.FindStringSubmatch(key)
		if len(match) == 0 {
			continue
		}
		n, _ := strconv.ParseInt(match[1], 10, 64)
		if n *= 1000; input > n && n > threshold {
			threshold = n
			suffix = strings.TrimPrefix(key, "input_cost_per_token")
		}
	}
	tier := strings.ToLower(strings.TrimSpace(r.ServiceTier))
	if tier == "fast" {
		tier = "priority"
	}
	hasPriority := false
	for _, key := range []string{"input_cost_per_token", "output_cost_per_token", "cache_read_input_token_cost", "cache_creation_input_token_cost"} {
		hasPriority = hasPriority || data[key+"_priority"] > 0
	}
	get := func(key string, tokens int64) (float64, bool) {
		v, exists := data[key+suffix]
		if !exists {
			v, exists = data[key]
		}
		// Apply the long-context multiplier to the selected service tier as sub2api does.
		if tier == "priority" || tier == "flex" {
			if tierPrice, tierExists := data[key+"_"+tier]; tierExists {
				if base := data[key]; suffix != "" && base > 0 {
					tierPrice *= v / base
				}
				v, exists = tierPrice, true
			} else if tier == "flex" {
				v *= 0.5
			} else if !hasPriority {
				v *= 2
			}
		} else if tier == "ultrafast" {
			// sub2api applies its generic 2x multiplier for Ultrafast.
			v *= 2
		}
		// GPT-5.6 and GPT-6 encode their context thresholds separately.
		if limit := data["long_context_input_token_threshold"]; suffix == "" && limit > 0 && float64(input) > limit {
			multiplier := data["long_context_input_cost_multiplier"]
			if key == "output_cost_per_token" {
				multiplier = data["long_context_output_cost_multiplier"]
			}
			if multiplier > 0 {
				v *= multiplier
			}
		}
		if multiplier := data["max_reasoning_effort_multiplier"]; effort == "max" && multiplier > 0 {
			v *= multiplier
		}
		return v * 1e6, exists || tokens == 0
	}
	p := &Price{Model: r.Model}
	var inputOK, outputOK, readOK, writeOK bool
	p.Input, inputOK = get("input_cost_per_token", r.InputTokens)
	p.Output, outputOK = get("output_cost_per_token", r.OutputTokens)
	p.CacheRead, readOK = get("cache_read_input_token_cost", r.CacheReadTokens)
	p.CacheWrite, writeOK = get("cache_creation_input_token_cost", r.CacheWriteTokens)
	if base, hour := data["cache_creation_input_token_cost"], data["cache_creation_input_token_cost_above_1hr"]; base > 0 && hour > 0 {
		// Preserve service-tier, long-context and effort adjustments for both TTLs.
		rate := p.CacheWrite * (hour / base)
		p.CacheWrite1h = &rate
	} else if r.CacheWrite1hTokens > 0 {
		return nil
	}
	if !inputOK || !outputOK || !readOK || !writeOK {
		return nil
	}
	return p
}

func tokenCost(r Record, p *Price) *float64 {
	if r.AccountingQuality != "complete" {
		return nil
	}
	if r.TotalTokens == 0 && r.InputTokens == 0 && r.OutputTokens == 0 && r.CacheReadTokens == 0 && r.CacheWriteTokens == 0 {
		zero := 0.0
		return &zero
	}
	if p == nil {
		return nil
	}
	fiveMinutes, oneHour := usage.NormalizeCacheCreationBreakdown(r.CacheWriteTokens, r.CacheWrite5mTokens, r.CacheWrite1hTokens)
	hourPrice := p.CacheWrite
	if p.CacheWrite1h != nil {
		hourPrice = *p.CacheWrite1h
	}
	cost := (float64(r.InputTokens)*p.Input + float64(r.OutputTokens)*p.Output + float64(r.CacheReadTokens)*p.CacheRead + float64(fiveMinutes)*p.CacheWrite + float64(oneHour)*hourPrice) / 1e6
	return &cost
}
