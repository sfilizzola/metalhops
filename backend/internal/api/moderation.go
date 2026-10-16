package api

import (
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/metalhops/metalhops/backend/internal/profile"
)

type moderationReportView struct {
	ID        string          `json:"id"`
	Reason    string          `json:"reason"`
	Note      *string         `json:"note"`
	Status    string          `json:"status"`
	CreatedAt time.Time       `json:"created_at"`
	Reporter  profile.Profile `json:"reporter"`
	Target    profile.Profile `json:"target"`
}

func listProfileReports(w http.ResponseWriter, r *http.Request, profiles Profiles, logger *slog.Logger) {
	limit, err := parseReportLimit(r.URL.Query().Get("limit"))
	if err != nil {
		writeProfileError(logger, w, r, err)
		return
	}
	page, err := profiles.ListReports(r.Context(), limit, r.URL.Query().Get("cursor"))
	if err != nil {
		writeProfileError(logger, w, r, err)
		return
	}
	items := make([]moderationReportView, 0, len(page.Items))
	for _, item := range page.Items {
		items = append(items, moderationReportView{
			ID: item.ID, Reason: item.Reason, Note: item.Note, Status: item.Status, CreatedAt: item.CreatedAt.UTC(),
			Reporter: moderationProfile(item.Reporter), Target: moderationProfile(item.Target),
		})
	}
	WriteList(w, http.StatusOK, items, page.NextCursor)
}

func getModerationProfile(w http.ResponseWriter, r *http.Request, profiles Profiles, logger *slog.Logger) {
	got, err := profiles.GetModerationProfile(r.Context(), r.PathValue("userID"))
	if err != nil {
		writeProfileError(logger, w, r, err)
		return
	}
	WriteData(w, http.StatusOK, moderationProfile(got))
}

func parseReportLimit(raw string) (int, error) {
	if raw == "" {
		return 20, nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		return 0, &profile.ValidationError{Fields: map[string]string{"limit": "must be from 1 to 100"}}
	}
	return n, nil
}

func moderationProfile(got profile.Profile) profile.Profile {
	got.CreatedAt = got.CreatedAt.UTC()
	got.UpdatedAt = got.UpdatedAt.UTC()
	if got.SuspendedUntil != nil {
		until := got.SuspendedUntil.UTC()
		got.SuspendedUntil = &until
	}
	return got
}
