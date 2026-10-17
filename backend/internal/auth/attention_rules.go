package auth

import (
	"encoding/json"
	"fmt"
	"math"
	"net/http"
)

// attentionRule is one rule's override; a nil field is not overridden.
type attentionRule struct {
	Enabled *bool    `json:"enabled,omitempty"`
	Value   *float64 `json:"value,omitempty"`
}

// attentionRules holds the overrides only, so an empty object means every
// rule at its default. Unknown keys fail the decode.
type attentionRules struct {
	StaleManualDays *attentionRule `json:"stale_manual_days,omitempty"`
	PositionShare   *attentionRule `json:"position_share,omitempty"`
	CashShare       *attentionRule `json:"cash_share,omitempty"`
	CurrencyShare   *attentionRule `json:"currency_share,omitempty"`
	Movers          *attentionRule `json:"movers,omitempty"`
	TryCashShare    *attentionRule `json:"try_cash_share,omitempty"`
	NoHedge         *attentionRule `json:"no_hedge,omitempty"`
}

// validate returns a 400 message, or "" when every value is in range.
func (a attentionRules) validate() string {
	for _, c := range []struct {
		key      string
		rule     *attentionRule
		min, max float64
		integer  bool
	}{
		{"stale_manual_days", a.StaleManualDays, 1, 365, true},
		{"position_share", a.PositionShare, 0, 1, false},
		{"cash_share", a.CashShare, 0, 1, false},
		{"currency_share", a.CurrencyShare, 0, 1, false},
		{"movers", a.Movers, 0, 10, true},
		{"try_cash_share", a.TryCashShare, 0, 1, false},
	} {
		if c.rule == nil || c.rule.Value == nil {
			continue
		}
		v := *c.rule.Value
		switch {
		case v < c.min || v > c.max:
			return fmt.Sprintf("%s: value must be between %g and %g", c.key, c.min, c.max)
		case c.integer && v != math.Trunc(v):
			return c.key + ": value must be an integer"
		}
	}
	if a.NoHedge != nil && a.NoHedge.Value != nil {
		return "no_hedge: value is not allowed"
	}
	return ""
}

// getAttentionRules handles GET /api/me/attention-rules.
//
//	@Summary	Attention rule overrides
//	@Description	Needs a session. Returns only what the user overrode, `{}` when nothing is.
//	@Tags		auth
//	@Produce	json
//	@Success	200	{object}	auth.attentionRules
//	@Failure	500	{object}	object{error=string}	"internal error"
//	@Router		/me/attention-rules [get]
func (h *handler) getAttentionRules(w http.ResponseWriter, r *http.Request) {
	rules, err := h.users.AttentionRules(r.Context(), MustUserID(r.Context()))
	if err != nil {
		WriteInternalError(w, r, err)
		return
	}
	WriteJSON(w, http.StatusOK, json.RawMessage(rules))
}

// updateAttentionRules handles PUT /api/me/attention-rules.
//
//	@Summary	Replace attention rule overrides
//	@Description	Needs a session. Replaces the whole object; keys are optional, and so are `enabled` and `value` inside a key. `no_hedge` takes `enabled` only. Shares are 0-1, `stale_manual_days` an integer 1-365, `movers` an integer 0-10.
//	@Tags		auth
//	@Accept		json
//	@Produce	json
//	@Param		body	body		auth.attentionRules	true	"overrides only"
//	@Success	200	{object}	auth.attentionRules
//	@Failure	400	{object}	object{error=string}	"invalid request body (unknown key, enabled not a boolean, value not a number); <key>: value must be between <min> and <max>; <key>: value must be an integer; no_hedge: value is not allowed"
//	@Failure	500	{object}	object{error=string}	"internal error"
//	@Router		/me/attention-rules [put]
func (h *handler) updateAttentionRules(w http.ResponseWriter, r *http.Request) {
	userID := MustUserID(r.Context())

	var req attentionRules
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		WriteError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if msg := req.validate(); msg != "" {
		WriteError(w, http.StatusBadRequest, msg)
		return
	}
	// Stored re-encoded, so the column only ever holds known keys.
	b, err := json.Marshal(req)
	if err != nil {
		WriteInternalError(w, r, err)
		return
	}
	if err := h.users.UpdateAttentionRules(r.Context(), userID, string(b)); err != nil {
		WriteInternalError(w, r, err)
		return
	}
	// An admin deleted the user since the session check: the read answers 401.
	h.getAttentionRules(w, r)
}
