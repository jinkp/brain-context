package api

import (
	"net/http"
	"time"

	"github.com/labstack/echo/v4"
)

// GetMetrics returns usage analytics for the authenticated tenant.
// GET /api/admin/metrics?from=2026-01-01&to=2026-12-31
func (h *Handler) GetMetrics(c echo.Context) error {
	tenantID, ok := tenantIDFromContext(c)
	if !ok {
		return writeError(c, http.StatusUnauthorized, "UNAUTHORIZED", "missing tenant context")
	}

	// Parse date range — default to last 30 days
	from := time.Now().UTC().AddDate(0, 0, -30)
	to := time.Now().UTC()

	if f := c.QueryParam("from"); f != "" {
		if t, err := time.Parse("2006-01-02", f); err == nil {
			from = t
		}
	}
	if t := c.QueryParam("to"); t != "" {
		if parsed, err := time.Parse("2006-01-02", t); err == nil {
			to = parsed.Add(24*time.Hour - time.Second) // end of day
		}
	}

	metrics, err := h.store.GetMetrics(c.Request().Context(), tenantID, from, to)
	if err != nil {
		return writeError(c, http.StatusInternalServerError, "METRICS_ERROR", "failed to load metrics")
	}

	return c.JSON(http.StatusOK, map[string]any{
		"period": map[string]string{
			"from": from.Format("2006-01-02"),
			"to":   to.Format("2006-01-02"),
		},
		"summary": map[string]any{
			"total_queries":           metrics.TotalQueries,
			"queries_last_7_days":     metrics.QueriesLast7Days,
			"queries_last_30_days":    metrics.QueriesLast30Days,
			"avg_relevance_score":     round2(metrics.AvgScore),
			"avg_chunks_returned":     round2(metrics.AvgChunksReturned),
			"estimated_tokens_served": metrics.EstimatedTokensServed,
			"estimated_tokens_saved":  metrics.EstimatedTokensSaved,
			"savings_percent":         round2(metrics.SavingsPercent),
		},
		"queries_by_day":     metrics.QueriesByDay,
		"queries_by_project": metrics.QueriesByProject,
		"interpretation": map[string]string{
			"avg_relevance_score": "0.0-1.0. Above 0.6 = good relevance. Below 0.4 = consider re-indexing.",
			"savings_percent":     "Estimated % of LLM context tokens saved vs sending the full repository.",
			"tokens_saved":        "Tokens your LLM did NOT receive because brain-context filtered them out.",
		},
	})
}

func round2(f float64) float64 {
	return float64(int(f*100)) / 100
}
