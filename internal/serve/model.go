package serve

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/openfluke/welvet/tokenizer"
	"github.com/openfluke/welvet/transformer"
)

const defaultQueueSize = 32

type modelHost struct {
	entityPath string
	profile    string
	tileSize   int
	model      *transformer.Model
	tokenizer  *tokenizer.Tokenizer
	jobs       chan modelJob
	closed     chan struct{}
	done       chan struct{}
	closeOnce  sync.Once
	active     atomic.Int32
}

type modelJob struct {
	ctx      context.Context
	request  modelRequest
	response chan modelResponse
}

type modelRequest struct {
	Mode           string             `json:"mode,omitempty"`
	Prompt         string             `json:"prompt,omitempty"`
	System         string             `json:"system,omitempty"`
	Turns          []transformer.Turn `json:"turns,omitempty"`
	MaxTokens      int                `json:"max_tokens,omitempty"`
	EnableThinking bool               `json:"enable_thinking,omitempty"`
}

type modelResponse struct {
	status int
	body   any
}

type generationResponse struct {
	Mode    string          `json:"mode"`
	Text    string          `json:"text"`
	Metrics metricsResponse `json:"metrics"`
}

type metricsResponse struct {
	PrefillTokens    int     `json:"prefill_tokens"`
	GeneratedTokens  int     `json:"generated_tokens"`
	PrefillMS        float64 `json:"prefill_ms"`
	DecodeMS         float64 `json:"decode_ms"`
	PrefillTokPerSec float64 `json:"prefill_tokens_per_second"`
	DecodeTokPerSec  float64 `json:"decode_tokens_per_second"`
}

type logitsResponse struct {
	Mode      string   `json:"mode"`
	TokenIDs  []uint32 `json:"token_ids"`
	VocabSize int      `json:"vocab_size"`
	// Logits is indexed by output token ID.
	Logits []float32 `json:"logits"`
	// LogitBits preserves every float32 exactly, including signed zero and NaNs.
	LogitBits []uint32 `json:"logit_bits"`
}

func newModelHost(entityID string, queueSize int, profileName string, tileSize int) (*modelHost, error) {
	path := strings.TrimSpace(entityID)
	if st, err := os.Stat(path); err != nil || st.IsDir() {
		path, err = resolveEntityFile(entityID)
		if err != nil {
			return nil, fmt.Errorf("resolve entity %q: %w", entityID, err)
		}
	}
	if absolute, err := filepath.Abs(path); err == nil {
		path = absolute
	}
	model, err := transformer.LoadEntity(path)
	if err != nil {
		return nil, fmt.Errorf("load entity: %w", err)
	}
	profiles := transformer.NamedProfiles()
	if strings.TrimSpace(profileName) == "" {
		profileName = "cpu_mc"
	}
	var profile transformer.ExecProfile
	found := false
	for _, candidate := range profiles {
		if candidate.Name == profileName {
			profile = candidate
			found = true
			break
		}
	}
	if !found {
		return nil, fmt.Errorf("unknown execution profile %q", profileName)
	}
	if tileSize > 0 {
		profile.TileSize = tileSize
	}
	if model.FusedPack && profile.Fused {
		profile.PackFormat = model.PackFormat
	}
	if err := model.ApplyExec(profile); err != nil {
		return nil, fmt.Errorf("apply %s profile: %w", profileName, err)
	}
	tok, err := tokenizer.LoadForEntity(path, model.TokenizerPath, model.Snapshot, model.Repo)
	if err != nil {
		return nil, fmt.Errorf("load tokenizer: %w", err)
	}
	if queueSize <= 0 {
		queueSize = defaultQueueSize
	}
	h := &modelHost{
		entityPath: path,
		profile:    profileName,
		tileSize:   profile.TileSize,
		model:      model,
		tokenizer:  tok,
		jobs:       make(chan modelJob, queueSize),
		closed:     make(chan struct{}),
		done:       make(chan struct{}),
	}
	go h.run()
	return h, nil
}

func (h *modelHost) close() {
	if h == nil {
		return
	}
	h.closeOnce.Do(func() {
		close(h.closed)
		<-h.done
		h.model.CloseGPU()
		h.model.CloseHybridGPU()
	})
}

func (h *modelHost) run() {
	defer close(h.done)
	for {
		select {
		case <-h.closed:
			return
		case job := <-h.jobs:
			if err := job.ctx.Err(); err != nil {
				job.response <- errorResponse(http.StatusRequestTimeout, err)
				continue
			}
			h.active.Store(1)
			response := h.execute(job)
			h.active.Store(0)
			job.response <- response
		}
	}
}

func (h *modelHost) execute(job modelJob) modelResponse {
	req := job.request
	switch req.Mode {
	case "", "generate":
		if strings.TrimSpace(req.Prompt) == "" {
			return errorResponse(http.StatusBadRequest, fmt.Errorf("prompt is required"))
		}
		if req.MaxTokens <= 0 {
			req.MaxTokens = 256
		}
		if req.MaxTokens > 4096 {
			return errorResponse(http.StatusBadRequest, fmt.Errorf("max_tokens must be <= 4096"))
		}
		system := strings.TrimSpace(req.System)
		if system == "" {
			system = "You are a helpful assistant."
		}
		text, metrics, err := h.model.Generate(
			h.tokenizer.Encode,
			h.tokenizer.Decode,
			req.Turns,
			system,
			req.Prompt,
			transformer.GenOptions{
				MaxTokens:      req.MaxTokens,
				Context:        job.ctx,
				Silent:         true,
				EnableThinking: req.EnableThinking,
			},
		)
		if err != nil {
			return errorResponse(http.StatusInternalServerError, err)
		}
		return modelResponse{status: http.StatusOK, body: generationResponse{
			Mode: "generate",
			Text: text,
			Metrics: metricsResponse{
				PrefillTokens:    metrics.PrefillTokens,
				GeneratedTokens:  metrics.GeneratedTokens,
				PrefillMS:        durationMS(metrics.PrefillTime),
				DecodeMS:         durationMS(metrics.DecodeTime),
				PrefillTokPerSec: metrics.PrefillTokPerSec,
				DecodeTokPerSec:  metrics.DecodeTokPerSec,
			},
		}}
	case "logits":
		if strings.TrimSpace(req.Prompt) == "" {
			return errorResponse(http.StatusBadRequest, fmt.Errorf("prompt is required"))
		}
		ids := h.tokenizer.Encode(req.Prompt, false)
		if len(ids) == 0 {
			return errorResponse(http.StatusBadRequest, fmt.Errorf("tokenizer produced no tokens"))
		}
		h.model.ResetKV()
		logits, err := h.model.ForwardTokens(ids)
		if err != nil {
			return errorResponse(http.StatusInternalServerError, err)
		}
		bits := make([]uint32, len(logits))
		for i, value := range logits {
			bits[i] = math.Float32bits(value)
		}
		return modelResponse{status: http.StatusOK, body: logitsResponse{
			Mode: "logits", TokenIDs: ids, VocabSize: len(logits), Logits: logits, LogitBits: bits,
		}}
	default:
		return errorResponse(http.StatusBadRequest, fmt.Errorf("mode must be generate or logits"))
	}
}

func (h *modelHost) handleInference(w http.ResponseWriter, r *http.Request, forcedMode string) {
	if r.Method != http.MethodPost {
		http.Error(w, "method", http.StatusMethodNotAllowed)
		return
	}
	var req modelRequest
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, fmt.Errorf("invalid JSON: %w", err))
		return
	}
	if forcedMode != "" {
		req.Mode = forcedMode
	}
	job := modelJob{
		ctx:      r.Context(),
		request:  req,
		response: make(chan modelResponse, 1),
	}
	select {
	case h.jobs <- job:
	default:
		writeError(w, http.StatusTooManyRequests, fmt.Errorf("request queue is full"))
		return
	}
	select {
	case response := <-job.response:
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(response.status)
		writeJSON(w, response.body)
	case <-r.Context().Done():
		writeError(w, http.StatusRequestTimeout, r.Context().Err())
	case <-h.closed:
		writeError(w, http.StatusServiceUnavailable, fmt.Errorf("model host stopped"))
	}
}

func (h *modelHost) queueStatus() map[string]any {
	return map[string]any{
		"entity":         h.entityPath,
		"profile":        h.profile,
		"tile_size":      h.tileSize,
		"queue_depth":    len(h.jobs),
		"queue_capacity": cap(h.jobs),
		"active":         h.active.Load() != 0,
	}
}

func registerModelRoutes(mux *http.ServeMux, h *modelHost) {
	if h == nil {
		return
	}
	mux.HandleFunc("/v1/inference", func(w http.ResponseWriter, r *http.Request) {
		h.handleInference(w, r, "")
	})
	mux.HandleFunc("/v1/generate", func(w http.ResponseWriter, r *http.Request) {
		h.handleInference(w, r, "generate")
	})
	mux.HandleFunc("/v1/logits", func(w http.ResponseWriter, r *http.Request) {
		h.handleInference(w, r, "logits")
	})
	mux.HandleFunc("/v1/queue", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method", http.StatusMethodNotAllowed)
			return
		}
		writeJSON(w, h.queueStatus())
	})
}

func errorResponse(status int, err error) modelResponse {
	return modelResponse{status: status, body: map[string]string{"error": err.Error()}}
}

func writeError(w http.ResponseWriter, status int, err error) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	writeJSON(w, map[string]string{"error": err.Error()})
}

func durationMS(d time.Duration) float64 {
	return float64(d) / float64(time.Millisecond)
}
