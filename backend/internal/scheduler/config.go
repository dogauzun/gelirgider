package scheduler

import (
	"context"
	"log/slog"
	"strconv"
	"sync"
	"time"

	"github.com/dogauzun/gelirgider/backend/internal/settings"
)

// Settings keys (ADR 0010). Per source: EnabledKey and AtKey. The defaults
// are DefaultAt (or Source.DefaultAt), enabled (tefas disabled, see
// Enabled), catch-up and backfill on, and an empty backfill start (the
// earliest transaction).
const (
	KeyCatchUp      = "schedule.catchup_enabled"
	KeyBackfill     = "schedule.backfill_enabled"
	KeyBackfillFrom = "schedule.backfill_from"
	DefaultAt       = "19:00"
)

// EnabledKey is the settings key turning source name on or off.
func EnabledKey(name string) string { return "source." + name + ".enabled" }

// AtKey is the settings key holding source name's daily fetch time.
func AtKey(name string) string { return "source." + name + ".at" }

// Config is the schedule as stored in settings, with defaults filled in.
type Config struct {
	Sources      map[string]SourceConfig // by Source.Name
	CatchUp      bool                    // run CatchUp at startup
	Backfill     bool                    // run the history backfill at startup
	BackfillFrom string                  // YYYY-MM-DD, or empty for the earliest transaction
}

// SourceConfig is one source's settings.
type SourceConfig struct {
	Enabled bool
	At      string // HH:MM, Europe/Istanbul
}

// Load reads the schedule for sources from repo. A missing, malformed or
// unreadable value falls back to its default, so a bad setting never stops
// the scheduler.
func Load(ctx context.Context, repo *settings.Repo, sources []Source, logger *slog.Logger) Config {
	cfg := Config{
		Sources:      make(map[string]SourceConfig, len(sources)),
		CatchUp:      readBool(ctx, repo, KeyCatchUp, true, logger),
		Backfill:     readBool(ctx, repo, KeyBackfill, true, logger),
		BackfillFrom: read(ctx, repo, KeyBackfillFrom, "", time.DateOnly, logger),
	}
	for _, src := range sources {
		def := src.DefaultAt
		if def == "" {
			def = DefaultAt
		}
		cfg.Sources[src.Name] = SourceConfig{
			Enabled: Enabled(ctx, repo, src.Name, logger),
			At:      read(ctx, repo, AtKey(src.Name), def, "15:04", logger),
		}
	}
	return cfg
}

// EnabledSources returns the sources enabled in c, in order.
func (c Config) EnabledSources(sources []Source) []Source {
	var on []Source
	for _, src := range sources {
		if c.Sources[src.Name].Enabled {
			on = append(on, src)
		}
	}
	return on
}

// Enabled reports whether source name is enabled. The default is true,
// except for tefas: its endpoint is disallowed by tefas.gov.tr's
// robots.txt, so fetching from it is the operator's opt-in (GLG-1114).
func Enabled(ctx context.Context, repo *settings.Repo, name string, logger *slog.Logger) bool {
	return readBool(ctx, repo, EnabledKey(name), name != NameTEFAS, logger)
}

// readBool reads a true/false setting that defaults to def. A read cut
// off by a cancelled or expired ctx failed nothing: it logs nothing and
// reports false, so it never turns a disabled source on (GLG-466).
func readBool(ctx context.Context, repo *settings.Repo, key string, def bool, logger *slog.Logger) bool {
	v, ok, err := repo.Get(ctx, key)
	if err != nil {
		if ctx.Err() != nil {
			return false
		}
		logger.Error("settings: read failed, using default", "key", key, "error", err)
		return def
	}
	if !ok {
		return def
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		warnMalformed(logger, key, v, strconv.FormatBool(def))
		return def
	}
	return b
}

// read returns key's value if it parses as time layout, def otherwise. An
// empty stored value means def too, and so does a read cut off by ctx,
// which logs nothing.
func read(ctx context.Context, repo *settings.Repo, key, def, layout string, logger *slog.Logger) string {
	v, ok, err := repo.Get(ctx, key)
	if err != nil {
		if ctx.Err() != nil {
			return def
		}
		logger.Error("settings: read failed, using default", "key", key, "error", err)
		return def
	}
	if !ok || v == "" {
		return def
	}
	if _, err := time.Parse(layout, v); err != nil {
		warnMalformed(logger, key, v, def)
		return def
	}
	return v
}

// warned holds the malformed value last logged per key, so a bad setting
// logs once, not on every minute tick.
var warned sync.Map

func warnMalformed(logger *slog.Logger, key, value, def string) {
	if prev, ok := warned.Load(key); ok && prev == value {
		return
	}
	warned.Store(key, value)
	logger.Warn("settings: malformed value, using default", "key", key, "value", value, "default", def)
}
