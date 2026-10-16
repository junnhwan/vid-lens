package httpapi

import (
	"net/http"
	"vidlens/internal/store"
)

func (a *Server) networkSettings(w http.ResponseWriter, r *http.Request) {
	if !a.admin(w, r) {
		return
	}
	var v store.NetworkSettings
	var e error
	if r.Method == "GET" {
		v, e = a.Store.NetworkSettings(r.Context(), a.Config)
	} else {
		var in store.NetworkInput
		if !a.body(w, r, &in) {
			return
		}
		v, e = a.Store.SaveNetworkSettings(r.Context(), a.Config, in)
	}
	if e != nil {
		a.problem(w, r, e)
		return
	}
	var used struct {
		store.NetworkSettings
		DailyUsed      int  `json:"daily_used"`
		MonthlyUsed    int  `json:"monthly_used"`
		MonthExhausted bool `json:"month_exhausted"`
	}
	used.NetworkSettings = v
	e = a.Store.Pool.QueryRow(r.Context(), `SELECT
        count(*) FILTER (WHERE created_at >= (date_trunc('day',now() AT TIME ZONE 'Asia/Shanghai') AT TIME ZONE 'Asia/Shanghai')),
        count(*) FILTER (WHERE month=to_char(now() AT TIME ZONE 'UTC','YYYY-MM')),
        COALESCE((SELECT exhausted FROM external_search_months WHERE month=to_char(now() AT TIME ZONE 'UTC','YYYY-MM')),false)
        FROM external_search_credits`).Scan(&used.DailyUsed, &used.MonthlyUsed, &used.MonthExhausted)
	if e != nil {
		a.problem(w, r, e)
		return
	}
	a.json(w, 200, used)
}
