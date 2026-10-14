package server

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"slices"
	"strconv"
	"time"

	"github.com/dogauzun/gelirgider/backend/internal/auth"
	"github.com/dogauzun/gelirgider/backend/internal/price"
	"github.com/dogauzun/gelirgider/backend/internal/scheduler"
	"github.com/dogauzun/gelirgider/backend/internal/settings"
)

// sourceSettings is one item of GET /api/admin/sources. CanBackfill says
// whether POST /api/admin/backfill takes the source. LastRun and
// LastSuccessAt are null for a source that never ran (or never succeeded).
type sourceSettings struct {
	Name          string               `json:"name"`
	Enabled       bool                 `json:"enabled"`
	At            string               `json:"at"`
	CanBackfill   bool                 `json:"can_backfill"`
	LastRun       *scheduler.SourceRun `json:"last_run"`
	LastSuccessAt *string              `json:"last_success_at"`
}

// scheduleSettings is the body of GET and PUT /api/admin/schedule.
type scheduleSettings struct {
	CatchUpEnabled  *bool  `json:"catchup_enabled"`
	BackfillEnabled *bool  `json:"backfill_enabled"`
	BackfillFrom    string `json:"backfill_from"`
}

// registerScheduleRoutes adds the admin source and schedule settings
// endpoints to mux, which must sit behind RequireAdmin. Reads go through
// scheduler.Load, so they show the effective values the scheduler runs
// with, defaults applied; sources is the scheduler's list, in run order.
// Run status comes from runs. Run now and backfill now (GLG-314) answer 202
// and work in the background: run now through runs, whose running flag the
// scheduler shares, backfill now through backfills.
func registerScheduleRoutes(mux *http.ServeMux, cfg *settings.Repo, runs *scheduler.RunLog, backfills *backfillQueue, sources []scheduler.Source) {
	source := func(name string) (scheduler.Source, bool) {
		i := slices.IndexFunc(sources, func(s scheduler.Source) bool { return s.Name == name })
		if i < 0 {
			return scheduler.Source{}, false
		}
		return sources[i], true
	}

	// Each source's effective settings and run status, in run order.
	//
	//	@Summary	List data sources
	//	@Description	Every scheduler source with its effective settings (defaults applied: every source is enabled except tefas, whose endpoint robots.txt disallows) and latest run status, in run order. last_run and last_success_at are null for a source that never ran or never succeeded.
	//	@Tags		admin
	//	@Produce	json
	//	@Success	200	{array}		server.sourceSettings
	//	@Failure	500	{object}	object{error=string}	"internal error"
	//	@Router		/admin/sources [get]
	mux.HandleFunc("GET /api/admin/sources", func(w http.ResponseWriter, r *http.Request) {
		sc := scheduler.Load(r.Context(), cfg, sources, slog.Default())
		status, err := runs.Latest(r.Context())
		if err != nil {
			auth.WriteInternalError(w, r, err)
			return
		}
		out := make([]sourceSettings, 0, len(sources))
		for _, src := range sources {
			s, st := sc.Sources[src.Name], status[src.Name]
			out = append(out, sourceSettings{Name: src.Name, Enabled: s.Enabled, At: s.At, CanBackfill: src.Backfill != nil, LastRun: st.LastRun, LastSuccessAt: st.LastSuccessAt})
		}
		auth.WriteJSON(w, http.StatusOK, out)
	})

	// Stores one source's enabled flag and daily run time.
	//
	//	@Summary	Update a data source
	//	@Tags		admin
	//	@Accept		json
	//	@Produce	json
	//	@Param		name	path	string							true	"source name"
	//	@Param		body	body	object{enabled=bool,at=string}	true	"enabled is required; at is the daily run time, HH:MM"
	//	@Success	204
	//	@Failure	400	{object}	object{error=string}	"invalid request body; at: must be HH:MM"
	//	@Failure	404	{object}	object{error=string}	"unknown source"
	//	@Failure	500	{object}	object{error=string}	"internal error"
	//	@Router		/admin/sources/{name} [put]
	mux.HandleFunc("PUT /api/admin/sources/{name}", func(w http.ResponseWriter, r *http.Request) {
		name := r.PathValue("name")
		if _, ok := source(name); !ok {
			auth.WriteError(w, http.StatusNotFound, "unknown source")
			return
		}
		var req struct {
			Enabled *bool  `json:"enabled"`
			At      string `json:"at"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Enabled == nil {
			auth.WriteError(w, http.StatusBadRequest, "invalid request body")
			return
		}
		// time.Parse takes a one-digit hour; the length keeps it HH:MM.
		if _, err := time.Parse("15:04", req.At); err != nil || len(req.At) != 5 {
			auth.WriteError(w, http.StatusBadRequest, "at: must be HH:MM")
			return
		}
		setSettings(w, r, cfg,
			[2]string{scheduler.EnabledKey(name), strconv.FormatBool(*req.Enabled)},
			[2]string{scheduler.AtKey(name), req.At})
	})

	// A disabled source still runs: the admin asked for it by name.
	//
	//	@Summary	Run a data source now
	//	@Description	Starts one sync of the source in the background, even if it is disabled, and answers 202 at once.
	//	@Tags		admin
	//	@Produce	json
	//	@Param		name	path	string	true	"source name"
	//	@Success	202
	//	@Failure	404	{object}	object{error=string}	"unknown source"
	//	@Failure	409	{object}	object{error=string}	"already running"
	//	@Router		/admin/sources/{name}/run [post]
	mux.HandleFunc("POST /api/admin/sources/{name}/run", func(w http.ResponseWriter, r *http.Request) {
		src, ok := source(r.PathValue("name"))
		if !ok {
			auth.WriteError(w, http.StatusNotFound, "unknown source")
			return
		}
		ctx := context.WithoutCancel(r.Context())
		today := time.Now().In(price.Istanbul)
		err := runs.Go(ctx, src.Name, scheduler.KindManual, func() (int, error) { return src.Sync(ctx, today) })
		if errors.Is(err, scheduler.ErrAlreadyRunning) {
			auth.WriteError(w, http.StatusConflict, "already running")
			return
		}
		w.WriteHeader(http.StatusAccepted)
	})

	// Every enabled source with a history, or only the one named, even if
	// disabled. The answer lists the sources queued.
	//
	//	@Summary	Backfill price history now
	//	@Description	Queues a history backfill in the background for every enabled source that has one, or only for the named source even if disabled. The body is optional.
	//	@Tags		admin
	//	@Accept		json
	//	@Produce	json
	//	@Param		body	body		object{source=string}	false	"source to backfill; empty means every enabled source"
	//	@Success	202	{object}	map[string][]string	"queued lists the sources queued"
	//	@Failure	400	{object}	object{error=string}	"invalid request body; source has no backfill"
	//	@Failure	404	{object}	object{error=string}	"unknown source"
	//	@Router		/admin/backfill [post]
	mux.HandleFunc("POST /api/admin/backfill", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Source string `json:"source"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
			auth.WriteError(w, http.StatusBadRequest, "invalid request body")
			return
		}
		sc := scheduler.Load(r.Context(), cfg, sources, slog.Default())
		todo := sc.EnabledSources(sources)
		if req.Source != "" {
			src, ok := source(req.Source)
			if !ok {
				auth.WriteError(w, http.StatusNotFound, "unknown source")
				return
			}
			if src.Backfill == nil {
				auth.WriteError(w, http.StatusBadRequest, "source has no backfill")
				return
			}
			todo = []scheduler.Source{src}
		}
		queued := []string{}
		for _, src := range todo {
			if src.Backfill == nil {
				continue
			}
			backfills.addWalk(src.Name, func(ctx context.Context) (int, error) { return src.Backfill(ctx, sc.BackfillFrom) })
			queued = append(queued, src.Name)
		}
		auth.WriteJSON(w, http.StatusAccepted, map[string][]string{"queued": queued})
	})

	// The effective catch-up and backfill settings, defaults applied.
	//
	//	@Summary	Get schedule settings
	//	@Description	backfill_from is empty when the backfill starts at the earliest transaction.
	//	@Tags		admin
	//	@Produce	json
	//	@Success	200	{object}	server.scheduleSettings
	//	@Router		/admin/schedule [get]
	mux.HandleFunc("GET /api/admin/schedule", func(w http.ResponseWriter, r *http.Request) {
		sc := scheduler.Load(r.Context(), cfg, nil, slog.Default())
		auth.WriteJSON(w, http.StatusOK, scheduleSettings{CatchUpEnabled: &sc.CatchUp, BackfillEnabled: &sc.Backfill, BackfillFrom: sc.BackfillFrom})
	})

	// Stores the catch-up and backfill settings.
	//
	//	@Summary	Update schedule settings
	//	@Description	catchup_enabled and backfill_enabled are required. An empty backfill_from means the earliest transaction.
	//	@Tags		admin
	//	@Accept		json
	//	@Produce	json
	//	@Param		body	body	server.scheduleSettings	true	"schedule settings"
	//	@Success	204
	//	@Failure	400	{object}	object{error=string}	"invalid request body; backfill_from: must be YYYY-MM-DD; backfill_from: must not be in the future; backfill_from: must be on or after 1990-01-01"
	//	@Failure	500	{object}	object{error=string}	"internal error"
	//	@Router		/admin/schedule [put]
	mux.HandleFunc("PUT /api/admin/schedule", func(w http.ResponseWriter, r *http.Request) {
		var req scheduleSettings
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.CatchUpEnabled == nil || req.BackfillEnabled == nil {
			auth.WriteError(w, http.StatusBadRequest, "invalid request body")
			return
		}
		// Empty means the earliest transaction (scheduler.KeyBackfillFrom).
		if req.BackfillFrom != "" {
			if err := price.CheckDate(req.BackfillFrom); err != nil {
				auth.WriteError(w, http.StatusBadRequest, "backfill_from: "+err.Error())
				return
			}
		}
		setSettings(w, r, cfg,
			[2]string{scheduler.KeyCatchUp, strconv.FormatBool(*req.CatchUpEnabled)},
			[2]string{scheduler.KeyBackfill, strconv.FormatBool(*req.BackfillEnabled)},
			[2]string{scheduler.KeyBackfillFrom, req.BackfillFrom})
	})
}

// setSettings stores each key/value pair in order and answers 204, or 500
// on the first failed write. Not atomic: the pairs written before a failure
// stay, each valid on its own, and the admin's retry writes the rest.
func setSettings(w http.ResponseWriter, r *http.Request, cfg *settings.Repo, pairs ...[2]string) {
	for _, p := range pairs {
		if err := cfg.Set(r.Context(), p[0], p[1]); err != nil {
			auth.WriteInternalError(w, r, err)
			return
		}
	}
	w.WriteHeader(http.StatusNoContent)
}
