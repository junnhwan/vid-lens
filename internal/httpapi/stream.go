package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"vidlens/internal/domain"
	"vidlens/internal/store"
)

type update struct {
	View     map[string]any
	Position store.Position
	Err      error
}
type group struct {
	session, typ, id, conversation string
	channels                       map[chan update]struct{}
	last                           string
}

// One persistent-state poll per active resource, shared by all of its subscribers.
type Hub struct {
	mu       sync.Mutex
	groups   map[string]*group
	count    int
	sessions map[string]int
	server   *Server
}

func NewHub(ctx context.Context, s *Server) *Hub {
	h := &Hub{groups: map[string]*group{}, sessions: map[string]int{}, server: s}
	go func() {
		t := time.NewTicker(2 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				h.poll(ctx)
			}
		}
	}()
	return h
}
func (h *Hub) subscribe(session, typ, id, conversation string) (chan update, func(), bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.count >= 100 || h.sessions[session] >= 4 {
		return nil, nil, false
	}
	key := session + ":" + typ + ":" + id
	g := h.groups[key]
	if g == nil {
		g = &group{session: session, typ: typ, id: id, conversation: conversation, channels: map[chan update]struct{}{}}
		h.groups[key] = g
	}
	ch := make(chan update, 32)
	g.channels[ch] = struct{}{}
	h.count++
	h.sessions[session]++
	var once sync.Once
	return ch, func() {
		once.Do(func() {
			h.mu.Lock()
			defer h.mu.Unlock()
			if gg := h.groups[key]; gg != nil {
				if _, ok := gg.channels[ch]; ok {
					delete(gg.channels, ch)
					h.count--
					h.sessions[session]--
					if len(gg.channels) == 0 {
						delete(h.groups, key)
					}
				}
			}
		})
	}, true
}
func (h *Hub) poll(ctx context.Context) {
	h.mu.Lock()
	list := []*group{}
	for _, g := range h.groups {
		list = append(list, g)
	}
	h.mu.Unlock()
	for _, g := range list {
		var u update
		if g.typ == "request" {
			u.View, u.Position, u.Err = h.server.Store.RequestView(ctx, g.session, g.id)
		} else {
			u.View, u.Position, u.Err = h.server.Store.TurnView(ctx, g.session, g.conversation, g.id)
		}
		sig := "error"
		if u.Err == nil {
			sig = h.server.Store.Cursor(u.Position)
		}
		h.mu.Lock()
		if g.last != sig || u.Err != nil {
			g.last = sig
			for ch := range g.channels {
				select {
				case ch <- u:
				default:
					close(ch)
					delete(g.channels, ch)
					h.count--
					h.sessions[g.session]--
				}
			}
		}
		h.mu.Unlock()
	}
}
func (a *Server) events(w http.ResponseWriter, r *http.Request) {
	ses, ok := a.auth(w, r)
	if !ok {
		return
	}
	typ, id := "request", r.PathValue("request")
	if r.PathValue("turn") != "" {
		typ, id = "turn", r.PathValue("turn")
	}
	var current update
	if typ == "request" {
		current.View, current.Position, current.Err = a.Store.RequestView(r.Context(), ses.ID, id)
	} else {
		current.View, current.Position, current.Err = a.Store.TurnView(r.Context(), ses.ID, r.PathValue("conversation"), id)
	}
	if current.Err != nil {
		a.problem(w, r, current.Err)
		return
	}
	raw := r.Header.Get("Last-Event-ID")
	if raw == "" {
		raw = r.URL.Query().Get("after")
	}
	var previous *store.Position
	reset := false
	if raw != "" {
		p, e := a.Store.ParseCursor(raw, ses.ID, typ, id)
		if e != nil {
			a.problem(w, r, e)
			return
		}
		if p.Page {
			a.problem(w, r, domain.Err(400, "INVALID_CURSOR", "游标类型无效。"))
			return
		}
		if p.Revision > current.Position.Revision || p.Revision == current.Position.Revision && p.Seq > current.Position.Seq {
			a.problem(w, r, domain.Err(400, "INVALID_CURSOR", "游标超出当前水位。"))
			return
		}
		previous = &p
		valid, e := a.Store.CursorAvailable(r.Context(), p)
		if e != nil {
			a.problem(w, r, e)
			return
		}
		reset = !valid
	}
	ch, unsubscribe, ok := a.hub.subscribe(ses.ID, typ, id, r.PathValue("conversation"))
	if !ok {
		a.problem(w, r, domain.Err(429, "ADMISSION_LIMITED", "实时连接数已达到上限。"))
		return
	}
	defer unsubscribe()
	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("Cache-Control", "private, no-store, no-transform")
	w.Header().Set("X-Accel-Buffering", "no")
	controller := http.NewResponseController(w)
	scope := map[string]string{"type": typ, "id": id}
	snapshotURL := r.URL.Path[:len(r.URL.Path)-len("/events")]
	write := func(event string, payload any, p *store.Position) bool {
		var obj map[string]any
		obj = map[string]any{"schema_version": 1, "scope": scope, "payload": payload}
		cursor := ""
		if p != nil {
			cursor = a.Store.Cursor(*p)
			obj["position"] = map[string]any{"cursor": cursor, "revision": p.Revision, "run_id": p.RunID, "seq": p.Seq}
		}
		b, e := json.Marshal(obj)
		if e != nil || len(b) > 1<<20 {
			return false
		}
		_ = controller.SetWriteDeadline(time.Now().Add(10 * time.Second))
		if cursor != "" {
			if _, e = fmt.Fprintf(w, "id: %s\n", cursor); e != nil {
				return false
			}
		}
		if _, e = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, b); e != nil {
			return false
		}
		return controller.Flush() == nil
	}
	_ = controller.SetWriteDeadline(time.Now().Add(10 * time.Second))
	if _, e := fmt.Fprint(w, "retry: 3000\n\n"); e != nil {
		return
	}
	if reset {
		write("stream.reset", map[string]any{"code": "CURSOR_EXPIRED", "snapshot_url": snapshotURL}, nil)
		return
	}
	terminal := func(v map[string]any) bool {
		obj := v[typ].(map[string]any)
		return domain.Terminal(obj["status"].(string))
	}
	send := func(u update) bool {
		if u.Err != nil {
			return false
		}
		same := previous != nil && previous.Revision == u.Position.Revision && equalRun(previous.RunID, u.Position.RunID)
		if same && previous.Seq == u.Position.Seq && !terminal(u.View) {
			return true
		}
		if !write("view.snapshot", u.View, &u.Position) {
			return false
		}
		p := u.Position
		previous = &p
		return !terminal(u.View)
	}
	if !send(current) {
		return
	}
	heart := time.NewTicker(15 * time.Second)
	defer heart.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case u, open := <-ch:
			if !open {
				return
			}
			if !send(u) {
				return
			}
		case <-heart.C:
			if !ses.ExpiresAt.After(time.Now()) {
				write("stream.auth_required", map[string]any{"code": "SESSION_REQUIRED", "message": "匿名会话已失效。"}, nil)
				return
			}
			_ = controller.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if _, e := fmt.Fprint(w, ": keep-alive\n\n"); e != nil {
				return
			}
			if controller.Flush() != nil {
				return
			}
		}
	}
}
func equalRun(a, b *string) bool { return a == nil && b == nil || a != nil && b != nil && *a == *b }
