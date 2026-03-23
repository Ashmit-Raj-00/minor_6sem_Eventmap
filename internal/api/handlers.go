package api

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"eventmap/internal/auth"
	"eventmap/internal/config"
	"eventmap/internal/store"
)

type handlers struct {
	cfg  config.Config
	st   *store.Memory
	jobs interface {
		EnqueueNotification(kind string, payload map[string]any)
		EnqueueAnalytics(event string, payload map[string]any)
	}
}

func (h *handlers) health(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":      true,
		"now_utc": time.Now().UTC().Format(time.RFC3339),
	})
}

func (h *handlers) register(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		Email    string     `json:"email"`
		Password string     `json:"password"`
		Role     store.Role `json:"role"`
	}
	if err := readJSON(r, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "bad_json"})
		return
	}
	u, err := h.st.CreateUser(req.Email, req.Password, req.Role)
	if err != nil {
		code := http.StatusBadRequest
		if err == store.ErrAlreadyExists {
			code = http.StatusConflict
		}
		writeJSON(w, code, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"id":    u.ID,
		"email": u.Email,
		"role":  u.Role,
	})
}

func (h *handlers) login(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if err := readJSON(r, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "bad_json"})
		return
	}
	u, err := h.st.VerifyPassword(req.Email, req.Password)
	if err != nil {
		writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "invalid_credentials"})
		return
	}
	now := time.Now()
	token, err := auth.SignHS256(h.cfg.JWTSecret, auth.Claims{
		Sub:  u.ID,
		Role: string(u.Role),
		Iat:  now.Unix(),
		Exp:  now.Add(h.cfg.TokenTTL).Unix(),
	})
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "token_sign_failed"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"token": token,
		"user": map[string]any{
			"id":    u.ID,
			"email": u.Email,
			"role":  u.Role,
		},
	})
}

func (h *handlers) me(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	chain(withAuth(h.cfg, h.st))(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u, ok := r.Context().Value(ctxKeyUser).(store.User)
		if !ok || u.ID == "" {
			writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "unauthorized"})
			return
		}
		points, level, nextLevelAt := h.st.UserScore(u.ID)
		writeJSON(w, http.StatusOK, map[string]any{
			"id":    u.ID,
			"email": u.Email,
			"role":  u.Role,
			"score": map[string]any{
				"points":      points,
				"level":       level,
				"nextLevelAt": nextLevelAt,
				"toNextLevel": maxInt(0, nextLevelAt-points),
			},
		})
	})).ServeHTTP(w, r)
}

func (h *handlers) events(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		q := r.URL.Query()
		if strings.EqualFold(q.Get("mode"), "nearby") || r.URL.Path == "/api/events/nearby" {
			h.eventsNearby(w, r)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"events": h.st.ListEvents()})
	case http.MethodPost:
		chain(withAuth(h.cfg, h.st), requireRoles(store.RoleAdmin, store.RoleOrganizer))(http.HandlerFunc(h.createEvent)).ServeHTTP(w, r)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (h *handlers) eventsNearby(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	lat, _ := strconv.ParseFloat(q.Get("lat"), 64)
	lng, _ := strconv.ParseFloat(q.Get("lng"), 64)
	radiusKm, _ := strconv.ParseFloat(q.Get("radius_km"), 64)
	writeJSON(w, http.StatusOK, map[string]any{
		"events": h.st.NearbyEvents(lat, lng, radiusKm),
	})
}

func (h *handlers) createEvent(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Title           string   `json:"title"`
		Description     string   `json:"description"`
		StartsAt        string   `json:"starts_at"`
		EndsAt          string   `json:"ends_at"`
		Lat             float64  `json:"lat"`
		Lng             float64  `json:"lng"`
		Address         string   `json:"address"`
		Tags            []string `json:"tags"`
		CheckinRadiusKm float64  `json:"checkin_radius_km"`
	}
	if err := readJSON(r, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "bad_json"})
		return
	}
	startsAt, err := time.Parse(time.RFC3339, req.StartsAt)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "starts_at must be RFC3339"})
		return
	}
	endsAt, err := time.Parse(time.RFC3339, req.EndsAt)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "ends_at must be RFC3339"})
		return
	}

	u := r.Context().Value(ctxKeyUser).(store.User)
	e, err := h.st.CreateEvent(store.Event{
		Title:           req.Title,
		Description:     req.Description,
		StartsAt:        startsAt,
		EndsAt:          endsAt,
		Lat:             req.Lat,
		Lng:             req.Lng,
		Address:         req.Address,
		Tags:            req.Tags,
		CheckinRadiusKm: req.CheckinRadiusKm,
		CreatedBy:       u.ID,
	})
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}

	h.jobs.EnqueueAnalytics("event_created", map[string]any{"event_id": e.ID, "user_id": u.ID})
	h.jobs.EnqueueNotification("event_created", map[string]any{"event_id": e.ID})

	writeJSON(w, http.StatusCreated, e)
}

func (h *handlers) eventSubroutes(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/events/")
	if path == "" {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	parts := strings.Split(path, "/")
	eventID := parts[0]
	if eventID == "" {
		w.WriteHeader(http.StatusNotFound)
		return
	}

	if len(parts) == 1 {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		e, err := h.st.GetEvent(eventID)
		if err != nil {
			writeJSON(w, http.StatusNotFound, map[string]any{"error": "not_found"})
			return
		}
		writeJSON(w, http.StatusOK, e)
		return
	}

	switch parts[1] {
	case "sessions":
		switch r.Method {
		case http.MethodGet:
			s, err := h.st.ListSessions(eventID)
			if err != nil {
				writeJSON(w, http.StatusNotFound, map[string]any{"error": "not_found"})
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{"sessions": s})
		case http.MethodPost:
			chain(withAuth(h.cfg, h.st), requireRoles(store.RoleAdmin, store.RoleOrganizer))(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var req struct {
					Title    string `json:"title"`
					StartsAt string `json:"starts_at"`
					EndsAt   string `json:"ends_at"`
				}
				if err := readJSON(r, &req); err != nil {
					writeJSON(w, http.StatusBadRequest, map[string]any{"error": "bad_json"})
					return
				}
				startsAt, err := time.Parse(time.RFC3339, req.StartsAt)
				if err != nil {
					writeJSON(w, http.StatusBadRequest, map[string]any{"error": "starts_at must be RFC3339"})
					return
				}
				endsAt, err := time.Parse(time.RFC3339, req.EndsAt)
				if err != nil {
					writeJSON(w, http.StatusBadRequest, map[string]any{"error": "ends_at must be RFC3339"})
					return
				}
				s, err := h.st.CreateSession(store.Session{
					EventID:  eventID,
					Title:    req.Title,
					StartsAt: startsAt,
					EndsAt:   endsAt,
				})
				if err != nil {
					writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
					return
				}
				h.jobs.EnqueueAnalytics("session_created", map[string]any{"event_id": eventID, "session_id": s.ID})
				writeJSON(w, http.StatusCreated, s)
			})).ServeHTTP(w, r)
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	case "join":
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		chain(withAuth(h.cfg, h.st), requireRoles(store.RoleAdmin, store.RoleOrganizer, store.RoleAttendee))(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			u := r.Context().Value(ctxKeyUser).(store.User)
			p, err := h.st.JoinEvent(eventID, u.ID)
			if err != nil {
				code := http.StatusBadRequest
				if err == store.ErrAlreadyExists {
					code = http.StatusConflict
				} else if err == store.ErrNotFound {
					code = http.StatusNotFound
				}
				writeJSON(w, code, map[string]any{"error": err.Error()})
				return
			}
			h.jobs.EnqueueAnalytics("event_joined", map[string]any{"event_id": eventID, "user_id": u.ID})
			h.jobs.EnqueueNotification("event_joined", map[string]any{"event_id": eventID, "user_id": u.ID})
			writeJSON(w, http.StatusCreated, p)
		})).ServeHTTP(w, r)
	case "participants":
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		chain(withAuth(h.cfg, h.st), requireRoles(store.RoleAdmin, store.RoleOrganizer))(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			p, err := h.st.ListParticipants(eventID)
			if err != nil {
				writeJSON(w, http.StatusNotFound, map[string]any{"error": "not_found"})
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{"participants": p})
		})).ServeHTTP(w, r)
	case "tag":
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		chain(withAuth(h.cfg, h.st), requireRoles(store.RoleAdmin, store.RoleOrganizer, store.RoleAttendee))(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			u := r.Context().Value(ctxKeyUser).(store.User)
			var req struct {
				Tag string  `json:"tag"`
				Lat float64 `json:"lat"`
				Lng float64 `json:"lng"`
			}
			if err := readJSON(r, &req); err != nil {
				writeJSON(w, http.StatusBadRequest, map[string]any{"error": "bad_json"})
				return
			}
			e, err := h.st.TagEventNear(eventID, u.ID, req.Tag, req.Lat, req.Lng)
			if err != nil {
				code := http.StatusBadRequest
				if err == store.ErrNotFound {
					code = http.StatusNotFound
				} else if err == store.ErrForbidden {
					code = http.StatusForbidden
				}
				writeJSON(w, code, map[string]any{"error": err.Error()})
				return
			}
			h.jobs.EnqueueAnalytics("event_tagged", map[string]any{"event_id": eventID, "user_id": u.ID, "tag": req.Tag})
			writeJSON(w, http.StatusOK, e)
		})).ServeHTTP(w, r)
	case "checkin":
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		chain(withAuth(h.cfg, h.st), requireRoles(store.RoleAdmin, store.RoleOrganizer, store.RoleAttendee))(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			u := r.Context().Value(ctxKeyUser).(store.User)
			var req struct {
				Lat float64 `json:"lat"`
				Lng float64 `json:"lng"`
			}
			if err := readJSON(r, &req); err != nil {
				writeJSON(w, http.StatusBadRequest, map[string]any{"error": "bad_json"})
				return
			}
			at, err := h.st.CheckIn(eventID, u.ID, req.Lat, req.Lng)
			if err != nil {
				code := http.StatusBadRequest
				if err == store.ErrAlreadyExists {
					code = http.StatusConflict
				} else if err == store.ErrNotFound {
					code = http.StatusNotFound
				} else if err == store.ErrForbidden {
					code = http.StatusForbidden
				}
				writeJSON(w, code, map[string]any{"error": err.Error()})
				return
			}
			h.jobs.EnqueueAnalytics("event_checkin", map[string]any{"event_id": eventID, "user_id": u.ID})
			writeJSON(w, http.StatusCreated, map[string]any{"checkedInAt": at.UTC().Format(time.RFC3339)})
		})).ServeHTTP(w, r)
	case "leaderboard":
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		chain(withAuth(h.cfg, h.st), requireRoles(store.RoleAdmin, store.RoleOrganizer, store.RoleAttendee))(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			limit := queryInt(r, "limit", 20)
			scores, err := h.st.EventLeaderboard(eventID, limit)
			if err != nil {
				writeJSON(w, http.StatusNotFound, map[string]any{"error": "not_found"})
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{"leaderboard": h.hydrateScores(scores)})
		})).ServeHTTP(w, r)
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

func (h *handlers) leaderboard(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	chain(withAuth(h.cfg, h.st), requireRoles(store.RoleAdmin, store.RoleOrganizer, store.RoleAttendee))(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		limit := queryInt(r, "limit", 20)
		scores := h.st.Leaderboard(limit)
		writeJSON(w, http.StatusOK, map[string]any{"leaderboard": h.hydrateScores(scores)})
	})).ServeHTTP(w, r)
}

func (h *handlers) hydrateScores(scores []store.Score) []map[string]any {
	out := make([]map[string]any, 0, len(scores))
	for _, s := range scores {
		u, err := h.st.GetUserByID(s.UserID)
		if err != nil {
			continue
		}
		_, level, nextLevelAt := h.st.UserScore(s.UserID)
		out = append(out, map[string]any{
			"userId":      s.UserID,
			"email":       u.Email,
			"points":      s.Points,
			"level":       level,
			"nextLevelAt": nextLevelAt,
		})
	}
	return out
}

func queryInt(r *http.Request, key string, def int) int {
	v := strings.TrimSpace(r.URL.Query().Get(key))
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return def
	}
	return n
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
