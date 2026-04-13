package api

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"mime"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
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
	chat    *chatHub
	persist func() error
}

func (h *handlers) persistBestEffort() {
	if h.persist == nil {
		return
	}
	if err := h.persist(); err != nil {
		log.Printf("persist failed: %v", err)
	}
}

func (h *handlers) health(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":      true,
		"now_utc": time.Now().UTC().Format(time.RFC3339),
	})
}

func (h *handlers) configJS(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	body, _ := json.Marshal(map[string]any{
		"apiBase":       "",
		"devAuthEnabled": h.cfg.DevAuthEnabled,
		"googleClientId": h.cfg.GoogleClientID,
	})
	w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = fmt.Fprintf(w, "window.__EVENTMAP_CONFIG__=%s;\n", body)
}

// --- Auth ---

func (h *handlers) authDev(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if !h.cfg.DevAuthEnabled {
		writeJSON(w, http.StatusForbidden, map[string]any{"error": "dev_auth_disabled"})
		return
	}
	var req struct {
		Email    string `json:"email"`
		Name     string `json:"name"`
		PhotoURL string `json:"photoUrl"`
		Role     string `json:"role"`
	}
	if err := readJSON(r, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "bad_json"})
		return
	}
	u, created, err := h.st.UpsertUserFromOAuth(req.Name, req.Email, req.PhotoURL)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	if req.Role != "" {
		role := store.Role(strings.TrimSpace(req.Role))
		u, err = h.st.UpdateUserRole(u.ID, role)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
			return
		}
	}
	if created && h.persist != nil {
		h.persistBestEffort()
	}
	token, err := h.issueToken(u)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "token_sign_failed"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"token": token, "user": u})
}

func (h *handlers) authGoogle(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		IDToken string `json:"idToken"`
	}
	if err := readJSON(r, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "bad_json"})
		return
	}
	req.IDToken = strings.TrimSpace(req.IDToken)
	if req.IDToken == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "id_token_required"})
		return
	}

	profile, err := parseGoogleIDTokenUnverified(req.IDToken)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid_id_token"})
		return
	}

	if !h.cfg.UnsafeSkipGoogleVerify {
		writeJSON(w, http.StatusForbidden, map[string]any{
			"error": "google_verify_not_configured",
			"hint":  "Set UNSAFE_SKIP_GOOGLE_VERIFY=true for local dev, or implement full signature verification in production.",
		})
		return
	}
	if h.cfg.GoogleClientID != "" && profile.Audience != h.cfg.GoogleClientID {
		writeJSON(w, http.StatusForbidden, map[string]any{"error": "aud_mismatch"})
		return
	}

	u, created, err := h.st.UpsertUserFromOAuth(profile.Name, profile.Email, profile.Picture)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	if created && h.persist != nil {
		h.persistBestEffort()
	}
	token, err := h.issueToken(u)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "token_sign_failed"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"token": token, "user": u})
}

type googleProfile struct {
	Email    string `json:"email"`
	Name     string `json:"name"`
	Picture  string `json:"picture"`
	Audience string `json:"aud"`
	Issuer   string `json:"iss"`
}

func parseGoogleIDTokenUnverified(jwt string) (googleProfile, error) {
	parts := strings.Split(jwt, ".")
	if len(parts) < 2 {
		return googleProfile{}, errors.New("malformed jwt")
	}
	payloadB, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return googleProfile{}, err
	}
	var p googleProfile
	if err := json.Unmarshal(payloadB, &p); err != nil {
		return googleProfile{}, err
	}
	p.Email = strings.TrimSpace(strings.ToLower(p.Email))
	p.Name = strings.TrimSpace(p.Name)
	p.Picture = strings.TrimSpace(p.Picture)
	if p.Email == "" {
		return googleProfile{}, errors.New("email missing")
	}
	return p, nil
}

func (h *handlers) issueToken(u store.User) (string, error) {
	now := time.Now()
	return auth.SignHS256(h.cfg.JWTSecret, auth.Claims{
		Sub:  u.ID,
		Role: string(u.Role),
		Iat:  now.Unix(),
		Exp:  now.Add(h.cfg.TokenTTL).Unix(),
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
		xp := h.st.UserXP(u.ID)
		level := (xp / 100) + 1
		next := level * 100
		u.XP = xp
		approved, rejected, ratio := h.st.UserReliability(u.ID)
		writeJSON(w, http.StatusOK, map[string]any{
			"user": u,
			"stats": map[string]any{
				"level":       level,
				"nextLevelAt": next,
				"toNextLevel": maxInt(0, next-xp),
				"approvedSubmissions": approved,
				"rejectedSubmissions": rejected,
				"approvalRatio": ratio,
			},
		})
	})).ServeHTTP(w, r)
}

func (h *handlers) meActivity(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	chain(withAuth(h.cfg, h.st), requireRoles(store.RoleOperator, store.RoleCommander))(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u := r.Context().Value(ctxKeyUser).(store.User)
		limit := queryInt(r, "limit", 50)
		writeJSON(w, http.StatusOK, map[string]any{
			"xpLogs": h.st.ListXPLogs(u.ID, limit),
		})
	})).ServeHTTP(w, r)
}

func (h *handlers) meRole(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	chain(withAuth(h.cfg, h.st))(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u, ok := r.Context().Value(ctxKeyUser).(store.User)
		if !ok || u.ID == "" {
			writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "unauthorized"})
			return
		}
		var req struct {
			Role string `json:"role"`
		}
		if err := readJSON(r, &req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "bad_json"})
			return
		}
		role := store.Role(strings.TrimSpace(req.Role))
		u2, err := h.st.UpdateUserRole(u.ID, role)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
			return
		}
		if h.persist != nil {
			h.persistBestEffort()
		}
		token, err := h.issueToken(u2)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "token_sign_failed"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"token": token, "user": u2})
	})).ServeHTTP(w, r)
}

// --- Events ---

func (h *handlers) events(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		chain(withAuth(h.cfg, h.st))(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var vid string
			if u, ok := r.Context().Value(ctxKeyUser).(store.User); ok {
				vid = u.ID
			}
			writeJSON(w, http.StatusOK, map[string]any{"events": h.st.ListEvents(vid)})
		})).ServeHTTP(w, r)
	case http.MethodPost:
		chain(withAuth(h.cfg, h.st), requireRoles(store.RoleCommander))(http.HandlerFunc(h.createEvent)).ServeHTTP(w, r)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (h *handlers) createEvent(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Title        string   `json:"title"`
		Description  string   `json:"description"`
		Goal         string   `json:"goal"`
		GoalTarget   int      `json:"goalTarget"`
		GoalUnit     string   `json:"goalUnit"`
		Instructions string   `json:"instructions"`
		Visibility   string   `json:"visibility"`
		StartsAt     string   `json:"startsAt"`
		EndsAt       string   `json:"endsAt"`
		Lat          float64  `json:"lat"`
		Lng          float64  `json:"lng"`
		Address      string   `json:"address"`
		Tags         []string `json:"tags"`
	}
	if err := readJSON(r, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "bad_json"})
		return
	}
	startsAt, err := time.Parse(time.RFC3339, req.StartsAt)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "startsAt must be RFC3339"})
		return
	}
	endsAt, err := time.Parse(time.RFC3339, req.EndsAt)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "endsAt must be RFC3339"})
		return
	}
	u := r.Context().Value(ctxKeyUser).(store.User)
	e, err := h.st.CreateEvent(store.Event{
		Title:        req.Title,
		Description:  req.Description,
		Goal:         req.Goal,
		GoalTarget:   req.GoalTarget,
		GoalUnit:     req.GoalUnit,
		Instructions: req.Instructions,
		Visibility:   store.EventVisibility(strings.TrimSpace(req.Visibility)),
		Status:       store.EventActive,
		StartsAt:     startsAt,
		EndsAt:       endsAt,
		Lat:          req.Lat,
		Lng:          req.Lng,
		Address:      req.Address,
		Tags:         req.Tags,
		CreatedBy:    u.ID,
	})
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	if h.persist != nil {
		h.persistBestEffort()
	}
	h.jobs.EnqueueAnalytics("event_created", map[string]any{"event_id": e.ID, "user_id": u.ID})
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
		chain(withAuth(h.cfg, h.st))(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var viewer store.User
			if u, ok := r.Context().Value(ctxKeyUser).(store.User); ok {
				viewer = u
			}
			e, err := h.st.GetEvent(eventID)
			if err != nil {
				writeJSON(w, http.StatusNotFound, map[string]any{"error": "not_found"})
				return
			}
			if e.Visibility == store.EventPrivate && viewer.ID == "" {
				writeJSON(w, http.StatusForbidden, map[string]any{"error": "forbidden"})
				return
			}
			if e.Visibility == store.EventPrivate && viewer.ID != "" && e.CreatedBy != viewer.ID && !h.st.IsParticipant(eventID, viewer.ID) {
				writeJSON(w, http.StatusForbidden, map[string]any{"error": "forbidden"})
				return
			}
			writeJSON(w, http.StatusOK, e)
		})).ServeHTTP(w, r)
		return
	}

	switch parts[1] {
	case "join":
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		chain(withAuth(h.cfg, h.st), requireRoles(store.RoleOperator, store.RoleCommander))(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
			if h.persist != nil {
				h.persistBestEffort()
			}
			// Notify commander
			ev, _ := h.st.GetEvent(eventID)
			if ev.CreatedBy != "" {
				h.st.AddNotification(ev.CreatedBy, "event_joined", map[string]any{"eventId": eventID, "userId": u.ID})
			}
			writeJSON(w, http.StatusCreated, p)
		})).ServeHTTP(w, r)
	case "participants":
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		chain(withAuth(h.cfg, h.st), requireRoles(store.RoleCommander))(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			u := r.Context().Value(ctxKeyUser).(store.User)
			ev, err := h.st.GetEvent(eventID)
			if err != nil {
				writeJSON(w, http.StatusNotFound, map[string]any{"error": "not_found"})
				return
			}
			if ev.CreatedBy != u.ID {
				writeJSON(w, http.StatusForbidden, map[string]any{"error": "forbidden"})
				return
			}
			p, err := h.st.ListParticipants(eventID)
			if err != nil {
				writeJSON(w, http.StatusNotFound, map[string]any{"error": "not_found"})
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{"participants": p})
		})).ServeHTTP(w, r)
	case "tasks":
		switch r.Method {
			case http.MethodGet:
				chain(withAuth(h.cfg, h.st), requireRoles(store.RoleOperator, store.RoleCommander))(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					u := r.Context().Value(ctxKeyUser).(store.User)
					tasks, err := h.st.ListTasks(eventID, u.ID)
					if err != nil {
						status := http.StatusBadRequest
						if err == store.ErrForbidden {
						status = http.StatusForbidden
					} else if err == store.ErrNotFound {
						status = http.StatusNotFound
					}
					writeJSON(w, status, map[string]any{"error": err.Error()})
					return
				}
				writeJSON(w, http.StatusOK, map[string]any{"tasks": tasks})
			})).ServeHTTP(w, r)
		case http.MethodPost:
			chain(withAuth(h.cfg, h.st), requireRoles(store.RoleCommander))(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				u := r.Context().Value(ctxKeyUser).(store.User)
				ev, err := h.st.GetEvent(eventID)
				if err != nil {
					writeJSON(w, http.StatusNotFound, map[string]any{"error": "not_found"})
					return
				}
				if ev.CreatedBy != u.ID {
					writeJSON(w, http.StatusForbidden, map[string]any{"error": "forbidden"})
					return
				}
				var req struct {
					Title       string `json:"title"`
					Description string `json:"description"`
					Type        string `json:"type"`
					Priority    string `json:"priority"`
					Difficulty  int    `json:"difficulty"`
					Deadline    string `json:"deadline"`
					AssignedTo  string `json:"assignedTo"`
					Lat         float64 `json:"lat"`
					Lng         float64 `json:"lng"`
					HasLocation bool    `json:"hasLocation"`
				}
				if err := readJSON(r, &req); err != nil {
					writeJSON(w, http.StatusBadRequest, map[string]any{"error": "bad_json"})
					return
				}
				var deadline time.Time
				if strings.TrimSpace(req.Deadline) != "" {
					deadline, err = time.Parse(time.RFC3339, req.Deadline)
					if err != nil {
						writeJSON(w, http.StatusBadRequest, map[string]any{"error": "deadline must be RFC3339"})
						return
					}
				}
				t, err := h.st.CreateTask(store.Task{
					EventID:     eventID,
					Title:       req.Title,
					Description: req.Description,
					Type:        store.TaskType(strings.TrimSpace(req.Type)),
					Priority:    store.TaskPriority(strings.TrimSpace(req.Priority)),
					Difficulty:  req.Difficulty,
					Deadline:    deadline,
					AssignedTo:  strings.TrimSpace(req.AssignedTo),
					HasLocation: req.HasLocation,
					Lat:         req.Lat,
					Lng:         req.Lng,
					CreatedBy:   u.ID,
				})
				if err != nil {
					code := http.StatusBadRequest
					if err == store.ErrAlreadyExists {
						code = http.StatusConflict
					}
					writeJSON(w, code, map[string]any{"error": err.Error()})
					return
				}
				if h.persist != nil {
					h.persistBestEffort()
				}
				if t.Type == store.TaskAssigned && t.AssignedTo != "" {
					h.st.AddNotification(t.AssignedTo, "task_assigned", map[string]any{"taskId": t.ID, "eventId": eventID})
				}
				writeJSON(w, http.StatusCreated, t)
			})).ServeHTTP(w, r)
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	case "dashboard":
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		chain(withAuth(h.cfg, h.st), requireRoles(store.RoleCommander))(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			u := r.Context().Value(ctxKeyUser).(store.User)
			d, err := h.st.EventDashboard(eventID, u.ID)
			if err != nil {
				status := http.StatusBadRequest
				if err == store.ErrForbidden {
					status = http.StatusForbidden
				} else if err == store.ErrNotFound {
					status = http.StatusNotFound
				}
				writeJSON(w, status, map[string]any{"error": err.Error()})
				return
			}
			writeJSON(w, http.StatusOK, d)
		})).ServeHTTP(w, r)
		case "chat":
			// /api/events/{id}/chat, /chat/stream
			if len(parts) >= 3 && parts[2] == "stream" {
				if r.Method != http.MethodGet {
					w.WriteHeader(http.StatusMethodNotAllowed)
					return
				}
				chain(withAuth(h.cfg, h.st), requireRoles(store.RoleOperator, store.RoleCommander))(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					u := r.Context().Value(ctxKeyUser).(store.User)
					h.handleChatStream(w, r, eventID, u.ID)
				})).ServeHTTP(w, r)
				return
			}
			switch r.Method {
			case http.MethodGet:
				chain(withAuth(h.cfg, h.st), requireRoles(store.RoleOperator, store.RoleCommander))(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					u := r.Context().Value(ctxKeyUser).(store.User)
					limit := queryInt(r, "limit", 50)
					msgs, err := h.st.ListChatMessages(eventID, u.ID, limit)
					if err != nil {
					status := http.StatusBadRequest
					if err == store.ErrForbidden {
						status = http.StatusForbidden
					} else if err == store.ErrNotFound {
						status = http.StatusNotFound
					}
					writeJSON(w, status, map[string]any{"error": err.Error()})
					return
				}
					writeJSON(w, http.StatusOK, map[string]any{"messages": msgs})
				})).ServeHTTP(w, r)
			case http.MethodPost:
				chain(withAuth(h.cfg, h.st), requireRoles(store.RoleOperator, store.RoleCommander))(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					u := r.Context().Value(ctxKeyUser).(store.User)
					var req struct {
						Body string `json:"body"`
					}
				if err := readJSON(r, &req); err != nil {
					writeJSON(w, http.StatusBadRequest, map[string]any{"error": "bad_json"})
					return
				}
				msg, err := h.st.AddChatMessage(eventID, u.ID, req.Body)
				if err != nil {
					status := http.StatusBadRequest
					if err == store.ErrForbidden {
						status = http.StatusForbidden
					} else if err == store.ErrNotFound {
						status = http.StatusNotFound
					}
					writeJSON(w, status, map[string]any{"error": err.Error()})
					return
				}
				if h.persist != nil {
					h.persistBestEffort()
				}
				h.chat.Broadcast(msg)
				writeJSON(w, http.StatusCreated, msg)
			})).ServeHTTP(w, r)
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	case "queries":
		// /api/events/{id}/queries or /queries/{qid}/answer
		if len(parts) >= 4 && parts[3] == "answer" {
			if r.Method != http.MethodPost {
				w.WriteHeader(http.StatusMethodNotAllowed)
				return
			}
			qid := parts[2]
			chain(withAuth(h.cfg, h.st), requireRoles(store.RoleCommander))(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				u := r.Context().Value(ctxKeyUser).(store.User)
				var req struct{ Answer string `json:"answer"` }
				if err := readJSON(r, &req); err != nil {
					writeJSON(w, http.StatusBadRequest, map[string]any{"error": "bad_json"})
					return
				}
				q, err := h.st.AnswerQuery(eventID, qid, u.ID, req.Answer)
				if err != nil {
					status := http.StatusBadRequest
					if err == store.ErrForbidden {
						status = http.StatusForbidden
					} else if err == store.ErrNotFound {
						status = http.StatusNotFound
					}
					writeJSON(w, status, map[string]any{"error": err.Error()})
					return
				}
				if h.persist != nil {
					h.persistBestEffort()
				}
				h.st.AddNotification(q.FromUserID, "query_answered", map[string]any{"eventId": eventID, "queryId": q.ID})
				writeJSON(w, http.StatusOK, q)
			})).ServeHTTP(w, r)
			return
		}
			switch r.Method {
			case http.MethodGet:
				chain(withAuth(h.cfg, h.st), requireRoles(store.RoleOperator, store.RoleCommander))(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					u := r.Context().Value(ctxKeyUser).(store.User)
					qs, err := h.st.ListQueries(eventID, u.ID)
					if err != nil {
						status := http.StatusBadRequest
					if err == store.ErrForbidden {
						status = http.StatusForbidden
					} else if err == store.ErrNotFound {
						status = http.StatusNotFound
					}
					writeJSON(w, status, map[string]any{"error": err.Error()})
					return
				}
					writeJSON(w, http.StatusOK, map[string]any{"queries": qs})
				})).ServeHTTP(w, r)
			case http.MethodPost:
				chain(withAuth(h.cfg, h.st), requireRoles(store.RoleOperator, store.RoleCommander))(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					u := r.Context().Value(ctxKeyUser).(store.User)
					var req struct{ Body string `json:"body"` }
					if err := readJSON(r, &req); err != nil {
						writeJSON(w, http.StatusBadRequest, map[string]any{"error": "bad_json"})
					return
				}
				q, err := h.st.CreateQuery(eventID, u.ID, req.Body)
				if err != nil {
					status := http.StatusBadRequest
					if err == store.ErrForbidden {
						status = http.StatusForbidden
					} else if err == store.ErrNotFound {
						status = http.StatusNotFound
					}
					writeJSON(w, status, map[string]any{"error": err.Error()})
					return
				}
				if h.persist != nil {
					h.persistBestEffort()
				}
				ev, _ := h.st.GetEvent(eventID)
				h.st.AddNotification(ev.CreatedBy, "query_raised", map[string]any{"eventId": eventID, "queryId": q.ID, "fromUserId": u.ID})
				writeJSON(w, http.StatusCreated, q)
			})).ServeHTTP(w, r)
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	case "announce":
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		chain(withAuth(h.cfg, h.st), requireRoles(store.RoleCommander))(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			u := r.Context().Value(ctxKeyUser).(store.User)
			ev, err := h.st.GetEvent(eventID)
			if err != nil {
				writeJSON(w, http.StatusNotFound, map[string]any{"error": "not_found"})
				return
			}
			if ev.CreatedBy != u.ID {
				writeJSON(w, http.StatusForbidden, map[string]any{"error": "forbidden"})
				return
			}
			var req struct {
				Body string `json:"body"`
			}
			if err := readJSON(r, &req); err != nil {
				writeJSON(w, http.StatusBadRequest, map[string]any{"error": "bad_json"})
				return
			}
			body := strings.TrimSpace(req.Body)
			if body == "" {
				writeJSON(w, http.StatusBadRequest, map[string]any{"error": "body_required"})
				return
			}
			participants, _ := h.st.ListParticipants(eventID)
			for _, p := range participants {
				h.st.AddNotification(p.UserID, "announcement", map[string]any{"eventId": eventID, "body": body})
			}
			// Also send to the commander.
			h.st.AddNotification(u.ID, "announcement", map[string]any{"eventId": eventID, "body": body})
			msg, _ := h.st.AddChatMessage(eventID, u.ID, "[ANNOUNCEMENT] "+body)
			h.chat.Broadcast(msg)
			if h.persist != nil {
				h.persistBestEffort()
			}
			writeJSON(w, http.StatusCreated, map[string]any{"ok": true})
		})).ServeHTTP(w, r)
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

// --- Tasks ---

func (h *handlers) taskSubroutes(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/tasks/")
	if path == "" {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	parts := strings.Split(path, "/")
	taskID := parts[0]
	if taskID == "" {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	if len(parts) == 1 {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		chain(withAuth(h.cfg, h.st))(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			t, err := h.st.GetTask(taskID)
			if err != nil {
				writeJSON(w, http.StatusNotFound, map[string]any{"error": "not_found"})
				return
			}
			u, ok := r.Context().Value(ctxKeyUser).(store.User)
			if !ok || u.ID == "" {
				writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "unauthorized"})
				return
			}
			ev, err := h.st.GetEvent(t.EventID)
			if err != nil {
				writeJSON(w, http.StatusNotFound, map[string]any{"error": "not_found"})
				return
			}
			if ev.CreatedBy != u.ID && !h.st.IsParticipant(ev.ID, u.ID) {
				writeJSON(w, http.StatusForbidden, map[string]any{"error": "forbidden"})
				return
			}
			writeJSON(w, http.StatusOK, t)
		})).ServeHTTP(w, r)
		return
	}

	switch parts[1] {
	case "latest-submission":
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		chain(withAuth(h.cfg, h.st), requireRoles(store.RoleOperator, store.RoleCommander))(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			u := r.Context().Value(ctxKeyUser).(store.User)
			t, err := h.st.GetTask(taskID)
			if err != nil {
				writeJSON(w, http.StatusNotFound, map[string]any{"error": "not_found"})
				return
			}
			ev, err := h.st.GetEvent(t.EventID)
			if err != nil {
				writeJSON(w, http.StatusNotFound, map[string]any{"error": "not_found"})
				return
			}
			sub, err := h.st.LatestSubmission(taskID)
			if err != nil {
				writeJSON(w, http.StatusNotFound, map[string]any{"error": "not_found"})
				return
			}
			// Only the event owner or the submission's operator can view proof.
			if ev.CreatedBy != u.ID && sub.OperatorID != u.ID {
				writeJSON(w, http.StatusForbidden, map[string]any{"error": "forbidden"})
				return
			}
			writeJSON(w, http.StatusOK, sub)
		})).ServeHTTP(w, r)
	case "start":
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		chain(withAuth(h.cfg, h.st), requireRoles(store.RoleOperator, store.RoleCommander))(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			u := r.Context().Value(ctxKeyUser).(store.User)
			t, err := h.st.StartTask(taskID, u.ID)
			if err != nil {
				status := http.StatusBadRequest
				if err == store.ErrForbidden {
					status = http.StatusForbidden
				} else if err == store.ErrNotFound {
					status = http.StatusNotFound
				}
				writeJSON(w, status, map[string]any{"error": err.Error()})
				return
			}
			if h.persist != nil {
				h.persistBestEffort()
			}
			writeJSON(w, http.StatusOK, t)
		})).ServeHTTP(w, r)
	case "submit":
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		chain(withAuth(h.cfg, h.st), requireRoles(store.RoleOperator, store.RoleCommander))(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			u := r.Context().Value(ctxKeyUser).(store.User)
			imgURL, comment, lat, lng, hasGeo, err := h.handleUploadProof(w, r)
			if err != nil {
				// handleUploadProof already wrote response in some cases.
				if !errors.Is(err, errAlreadyWrote) {
					writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
				}
				return
			}
			t, sub, err := h.st.SubmitTask(taskID, u.ID, imgURL, comment, lat, lng, hasGeo)
			if err != nil {
				status := http.StatusBadRequest
				if err == store.ErrForbidden {
					status = http.StatusForbidden
				} else if err == store.ErrNotFound {
					status = http.StatusNotFound
				}
				writeJSON(w, status, map[string]any{"error": err.Error()})
				return
			}
			if h.persist != nil {
				h.persistBestEffort()
			}
			ev, _ := h.st.GetEvent(t.EventID)
			if ev.CreatedBy != "" {
				h.st.AddNotification(ev.CreatedBy, "task_submitted", map[string]any{"eventId": t.EventID, "taskId": t.ID, "submissionId": sub.ID})
			}
			writeJSON(w, http.StatusCreated, map[string]any{"task": t, "submission": sub})
		})).ServeHTTP(w, r)
	case "review":
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		chain(withAuth(h.cfg, h.st), requireRoles(store.RoleCommander))(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			u := r.Context().Value(ctxKeyUser).(store.User)
			var req struct {
				Action   string `json:"action"` // approve|reject
				Feedback string `json:"feedback"`
				Quality  int    `json:"quality"`
			}
			if err := readJSON(r, &req); err != nil {
				writeJSON(w, http.StatusBadRequest, map[string]any{"error": "bad_json"})
				return
			}
			approve := strings.EqualFold(strings.TrimSpace(req.Action), "approve")
			if !approve && !strings.EqualFold(strings.TrimSpace(req.Action), "reject") {
				writeJSON(w, http.StatusBadRequest, map[string]any{"error": "action must be approve|reject"})
				return
			}
			t, sub, awarded, err := h.st.ReviewLatestSubmission(taskID, u.ID, approve, req.Feedback, req.Quality)
			if err != nil {
				status := http.StatusBadRequest
				if err == store.ErrForbidden {
					status = http.StatusForbidden
				} else if err == store.ErrNotFound {
					status = http.StatusNotFound
				}
				writeJSON(w, status, map[string]any{"error": err.Error()})
				return
			}
			if h.persist != nil {
				h.persistBestEffort()
			}
			kind := "task_rejected"
			if approve {
				kind = "task_approved"
			}
			h.st.AddNotification(sub.OperatorID, kind, map[string]any{"eventId": t.EventID, "taskId": t.ID, "submissionId": sub.ID, "xpAwarded": awarded})
			writeJSON(w, http.StatusOK, map[string]any{"task": t, "submission": sub, "xpAwarded": awarded})
		})).ServeHTTP(w, r)
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

// --- Notifications / Leaderboard ---

func (h *handlers) notifications(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		chain(withAuth(h.cfg, h.st), requireRoles(store.RoleOperator, store.RoleCommander))(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			u := r.Context().Value(ctxKeyUser).(store.User)
			limit := queryInt(r, "limit", 50)
			writeJSON(w, http.StatusOK, map[string]any{"notifications": h.st.ListNotifications(u.ID, limit)})
		})).ServeHTTP(w, r)
	case http.MethodPost:
		chain(withAuth(h.cfg, h.st), requireRoles(store.RoleOperator, store.RoleCommander))(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			u := r.Context().Value(ctxKeyUser).(store.User)
			var req struct{ ID string `json:"id"` }
			if err := readJSON(r, &req); err != nil {
				writeJSON(w, http.StatusBadRequest, map[string]any{"error": "bad_json"})
				return
			}
			if err := h.st.MarkNotificationRead(u.ID, strings.TrimSpace(req.ID)); err != nil {
				status := http.StatusBadRequest
				if err == store.ErrNotFound {
					status = http.StatusNotFound
				}
				writeJSON(w, status, map[string]any{"error": err.Error()})
				return
			}
			if h.persist != nil {
				h.persistBestEffort()
			}
			writeJSON(w, http.StatusOK, map[string]any{"ok": true})
		})).ServeHTTP(w, r)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (h *handlers) leaderboard(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	chain(withAuth(h.cfg, h.st), requireRoles(store.RoleOperator, store.RoleCommander))(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
		level := (s.XP / 100) + 1
		out = append(out, map[string]any{
			"userId":   s.UserID,
			"name":     u.Name,
			"email":    u.Email,
			"photoUrl": u.PhotoURL,
			"xp":       s.XP,
			"level":    level,
		})
	}
	return out
}

// --- Proof upload ---

var errAlreadyWrote = errors.New("already wrote response")

func (h *handlers) handleUploadProof(w http.ResponseWriter, r *http.Request) (imageURL, comment string, lat, lng float64, hasGeo bool, err error) {
	r.Body = http.MaxBytesReader(w, r.Body, h.cfg.MaxUploadBytes+1024)
	if err := r.ParseMultipartForm(h.cfg.MaxUploadBytes); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid_multipart"})
		return "", "", 0, 0, false, errAlreadyWrote
	}
	comment = strings.TrimSpace(r.FormValue("comment"))
	if strings.TrimSpace(r.FormValue("lat")) != "" && strings.TrimSpace(r.FormValue("lng")) != "" {
		lat, _ = strconv.ParseFloat(r.FormValue("lat"), 64)
		lng, _ = strconv.ParseFloat(r.FormValue("lng"), 64)
		hasGeo = true
	}

	f, hdr, err := r.FormFile("image")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "image_required"})
		return "", "", 0, 0, false, errAlreadyWrote
	}
	defer f.Close()

	url, err := h.saveUpload(hdr, f)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return "", "", 0, 0, false, errAlreadyWrote
	}
	return url, comment, lat, lng, hasGeo, nil
}

func (h *handlers) saveUpload(hdr *multipart.FileHeader, f multipart.File) (string, error) {
	if hdr == nil || hdr.Filename == "" {
		return "", errors.New("invalid upload")
	}
	if hdr.Size <= 0 || hdr.Size > h.cfg.MaxUploadBytes {
		return "", errors.New("image_too_large")
	}
	ext := strings.ToLower(filepath.Ext(hdr.Filename))
	switch ext {
	case ".jpg", ".jpeg", ".png", ".webp":
	default:
		return "", errors.New("unsupported_image_type")
	}

	// Best-effort MIME sniff for defense-in-depth.
	buf := make([]byte, 512)
	n, _ := io.ReadFull(f, buf)
	_, _ = f.Seek(0, io.SeekStart)
	mtype := http.DetectContentType(buf[:n])
	if mtype == "application/octet-stream" {
		// let it pass; some browsers omit types
	} else {
		mt, _, _ := mime.ParseMediaType(mtype)
		if !strings.HasPrefix(mt, "image/") {
			return "", errors.New("invalid_image_content")
		}
	}

	name := fmt.Sprintf("%s%s", safeID(), ext)
	path := filepath.Join(h.cfg.UploadsDir, name)
	dst, err := os.Create(path)
	if err != nil {
		return "", errors.New("upload_failed")
	}
	defer dst.Close()
	if _, err := io.Copy(dst, io.LimitReader(f, h.cfg.MaxUploadBytes+1)); err != nil {
		return "", errors.New("upload_failed")
	}
	_ = dst.Sync()
	return "/uploads/" + name, nil
}

func safeID() string {
	b := make([]byte, 12)
	_, _ = rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

// --- helpers ---

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
