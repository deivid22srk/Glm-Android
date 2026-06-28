package captcha

import (
        "context"
        "crypto/rand"
        "encoding/base64"
        "encoding/hex"
        "encoding/json"
        "errors"
        "net/http"
        "sort"
        "strings"
        "sync"
        "time"

        "glm5.2proxy/internal/config"
)

type Request struct {
        ID        string `json:"id"`
        Source    string `json:"source"`
        TimeoutMS int64  `json:"timeoutMs"`
        CreatedAt int64  `json:"createdAt"`
}

type Challenge struct {
        Token  string
        Region string
}

type result struct {
        challenge Challenge
        err       error
}

type clientState struct {
        lastPoll time.Time
        requests chan Request
}

type Bridge struct {
        cfg     config.Config
        mu      sync.RWMutex
        clients map[string]*clientState
        waiters map[string]chan result
}

func NewBridge(cfg config.Config) *Bridge {
        return &Bridge{cfg: cfg, clients: map[string]*clientState{}, waiters: map[string]chan result{}}
}

func (b *Bridge) Fresh(ctx context.Context) (string, error) {
        challenge, err := b.FreshChallenge(ctx)
        return challenge.Token, err
}

func (b *Bridge) FreshChallenge(ctx context.Context) (Challenge, error) {
        if !b.cfg.CaptchaEnabled {
                return Challenge{}, NewError(ErrDisabled, "captcha bridge is disabled")
        }
        client := b.chooseClient()
        if client == nil {
                return Challenge{}, NewError(ErrBrowserUnavailable, "fresh captcha browser is unavailable")
        }
        id := randomID()
        waiter := make(chan result, 1)
        b.mu.Lock()
        b.waiters[id] = waiter
        b.mu.Unlock()
        request := Request{ID: id, Source: "openai_proxy", TimeoutMS: b.cfg.CaptchaTimeout.Milliseconds(), CreatedAt: time.Now().UnixMilli()}
        select {
        case client.requests <- request:
        case <-ctx.Done():
                b.deleteWaiter(id)
                return Challenge{}, ctx.Err()
        }
        timer := time.NewTimer(b.cfg.CaptchaTimeout)
        defer timer.Stop()
        select {
        case value := <-waiter:
                return value.challenge, value.err
        case <-timer.C:
                b.deleteWaiter(id)
                return Challenge{}, NewError(ErrTimeout, "timed out waiting for captcha browser")
        case <-ctx.Done():
                b.deleteWaiter(id)
                return Challenge{}, ctx.Err()
        }
}

func (b *Bridge) Poll(w http.ResponseWriter, r *http.Request) {
        clientName := first(r.URL.Query().Get("client"), "zcode-app")
        b.mu.Lock()
        client := b.clients[clientName]
        if client == nil {
                client = &clientState{requests: make(chan Request, 8)}
                b.clients[clientName] = client
        }
        client.lastPoll = time.Now()
        b.mu.Unlock()
        timer := time.NewTimer(25 * time.Second)
        defer timer.Stop()
        select {
        case request := <-client.requests:
                writeJSON(w, http.StatusOK, request)
        case <-timer.C:
                w.WriteHeader(http.StatusNoContent)
        case <-r.Context().Done():
        }
}

func (b *Bridge) Submit(w http.ResponseWriter, r *http.Request) {
        var body struct {
                ID     string `json:"id"`
                Token  string `json:"token"`
                Region string `json:"region"`
                Error  string `json:"error"`
        }
        if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&body); err != nil {
                writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": err.Error()})
                return
        }
        b.mu.Lock()
        waiter := b.waiters[body.ID]
        delete(b.waiters, body.ID)
        b.mu.Unlock()
        if waiter == nil {
                writeJSON(w, http.StatusNotFound, map[string]any{"ok": false, "error": "captcha request not found"})
                return
        }
        if body.Error != "" {
                waiter <- result{err: classifySubmitError(body.Error)}
        } else if strings.TrimSpace(body.Token) == "" {
                waiter <- result{err: NewError(ErrEmptyToken, "captcha browser returned an empty token")}
        } else {
                waiter <- result{challenge: Challenge{Token: strings.TrimSpace(body.Token), Region: strings.TrimSpace(body.Region)}}
        }
        writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func classifySubmitError(message string) error {
        if classified, ok := Classify(errors.New(message)); ok {
                return classified
        }
        return errors.New(message)
}

// Requeue pushes a previously-consumed Request back into a client's
// request channel. Used by the Android CaptchaBroker to forward requests
// from its own client channel to the browser's client channel.
//
// Flow:
//  1. No browser is open → chooseClient() returns the broker (android-broker)
//  2. Broker receives the Request via long-poll
//  3. Broker opens the system browser at /zcode/captcha/browser?client=standalone-browser
//  4. Browser starts polling with client=standalone-browser
//  5. Broker POSTs the Request to /zcode/captcha/requeue?client=standalone-browser
//  6. This method pushes it into the browser's channel
//  7. Browser's next poll receives the Request, solves the captcha, submits
//     the token via /zcode/captcha/submit
//  8. The original waiter (from FreshChallenge) receives the token and
//     the blocked chat completion resumes
//
// Without this endpoint, the broker would consume the Request and the
// browser would never see it — the browser would sit idle showing
// "Broker pronto. Aguardando request..." forever, and the chat completion
// would time out.
func (b *Bridge) Requeue(w http.ResponseWriter, r *http.Request) {
        var request Request
        if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&request); err != nil {
                writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": err.Error()})
                return
        }
        if request.ID == "" {
                writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "request id is required"})
                return
        }
        clientName := first(r.URL.Query().Get("client"), "standalone-browser")
        b.mu.Lock()
        client := b.clients[clientName]
        if client == nil {
                client = &clientState{requests: make(chan Request, 8)}
                b.clients[clientName] = client
        }
        b.mu.Unlock()
        select {
        case client.requests <- request:
                writeJSON(w, http.StatusOK, map[string]any{"ok": true, "clientId": clientName, "requestId": request.ID})
        default:
                writeJSON(w, http.StatusServiceUnavailable, map[string]any{"ok": false, "error": "client request channel is full", "clientId": clientName})
        }
}

func (b *Bridge) Test(w http.ResponseWriter, r *http.Request) {
        token, err := b.Fresh(r.Context())
        if err != nil {
                writeJSON(w, http.StatusBadGateway, map[string]any{"error": map[string]any{"message": err.Error(), "type": "zcode_captcha_test_failed"}})
                return
        }
        fields := []string{}
        sceneID := ""
        isSign := false
        format := "opaque"
        if decoded, err := base64.StdEncoding.DecodeString(token); err == nil {
                var value map[string]any
                if json.Unmarshal(decoded, &value) == nil {
                        format = "base64-json"
                        sceneID, _ = value["sceneId"].(string)
                        isSign, _ = value["isSign"].(bool)
                        for key := range value {
                                fields = append(fields, key)
                        }
                        sort.Strings(fields)
                }
        }
        writeJSON(w, http.StatusOK, map[string]any{"ok": true, "tokenLength": len(token), "format": format, "sceneId": sceneID, "isSign": isSign, "fields": fields})
}

func (b *Bridge) Snapshot() map[string]any {
        b.mu.RLock()
        defer b.mu.RUnlock()
        now := time.Now()
        clients := map[string]any{}
        available := false
        lastClient := ""
        var latest time.Time
        for name, client := range b.clients {
                online := now.Sub(client.lastPoll) < 45*time.Second
                if online {
                        available = true
                }
                if client.lastPoll.After(latest) {
                        latest = client.lastPoll
                        lastClient = name
                }
                clients[name] = map[string]any{"available": online, "lastPollAt": client.lastPoll.UnixMilli()}
        }
        return map[string]any{"available": available, "waiters": len(b.waiters), "lastPollAt": nullableMillis(latest), "lastClient": lastClient, "preferredClient": b.cfg.CaptchaPreferredClient, "clients": clients}
}

func (b *Bridge) chooseClient() *clientState {
        b.mu.RLock()
        defer b.mu.RUnlock()
        now := time.Now()
        if preferred := b.clients[b.cfg.CaptchaPreferredClient]; preferred != nil && now.Sub(preferred.lastPoll) < 45*time.Second {
                return preferred
        }
        var selected *clientState
        for _, client := range b.clients {
                if now.Sub(client.lastPoll) < 45*time.Second && (selected == nil || client.lastPoll.After(selected.lastPoll)) {
                        selected = client
                }
        }
        return selected
}

func (b *Bridge) deleteWaiter(id string) {
        b.mu.Lock()
        delete(b.waiters, id)
        b.mu.Unlock()
}

func nullableMillis(value time.Time) any {
        if value.IsZero() {
                return nil
        }
        return value.UnixMilli()
}

func first(values ...string) string {
        for _, value := range values {
                if value != "" {
                        return value
                }
        }
        return ""
}

func randomID() string {
        value := make([]byte, 16)
        _, _ = rand.Read(value)
        return hex.EncodeToString(value)
}

func writeJSON(w http.ResponseWriter, status int, value any) {
        w.Header().Set("Content-Type", "application/json; charset=utf-8")
        w.WriteHeader(status)
        _ = json.NewEncoder(w).Encode(value)
}
