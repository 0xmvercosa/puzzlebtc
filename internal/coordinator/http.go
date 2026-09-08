package coordinator

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/0xmvercosa/puzzlebtc/internal/keyspace"
	"github.com/0xmvercosa/puzzlebtc/internal/proof"
	"github.com/0xmvercosa/puzzlebtc/internal/store"
)

// maxSubmissionBytes caps a request body. A legitimate submission for a 2^40
// block carries ~16k witnesses; 8 MB leaves generous headroom while keeping a
// hostile client from exhausting memory.
const maxSubmissionBytes = 8 << 20

// Server exposes a Coordinator over HTTP.
type Server struct {
	co  *Coordinator
	log *slog.Logger
}

// NewServer wires a coordinator to an HTTP handler.
func NewServer(co *Coordinator, log *slog.Logger) *Server {
	if log == nil {
		log = slog.Default()
	}
	return &Server{co: co, log: log}
}

// Routes returns the mux serving the worker protocol.
func (s *Server) Routes() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/lease", s.handleLease)
	mux.HandleFunc("POST /v1/submit", s.handleSubmit)
	mux.HandleFunc("GET /v1/progress", s.handleProgress)
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok\n"))
	})
	return mux
}

type leaseRequest struct {
	WorkerID string `json:"worker_id"`
	// Count asks for several blocks in one call. Zero or one behaves as before.
	Count int `json:"count,omitempty"`
}

// leaseBatchResponse is returned when Count > 1, so a single-block client keeps
// seeing the flat object it already parses.
type leaseBatchResponse struct {
	Leases []*Lease `json:"leases"`
}

func (s *Server) handleLease(w http.ResponseWriter, r *http.Request) {
	var req leaseRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if strings.TrimSpace(req.WorkerID) == "" {
		writeError(w, http.StatusBadRequest, "missing_worker_id", "worker_id is required")
		return
	}

	if req.Count > 1 {
		leases, err := s.co.LeaseBatch(r.Context(), req.WorkerID, req.Count)
		switch {
		case errors.Is(err, store.ErrNoBlockAvailable):
			writeError(w, http.StatusServiceUnavailable, "no_block_available", err.Error())
		case err != nil:
			s.log.Error("lease batch failed", "worker", req.WorkerID, "count", req.Count, "err", err)
			writeError(w, http.StatusInternalServerError, "internal", "could not allocate blocks")
		default:
			writeJSON(w, http.StatusOK, leaseBatchResponse{Leases: leases})
		}
		return
	}

	lease, err := s.co.LeaseBlock(r.Context(), req.WorkerID)
	switch {
	case errors.Is(err, store.ErrNoBlockAvailable):
		// 503 rather than 404: the campaign exists, there is just nothing left
		// to hand out. A worker should back off and retry, not give up.
		writeError(w, http.StatusServiceUnavailable, "no_block_available", err.Error())
		return
	case err != nil:
		s.log.Error("lease failed", "worker", req.WorkerID, "err", err)
		writeError(w, http.StatusInternalServerError, "internal", "could not allocate a block")
		return
	}
	writeJSON(w, http.StatusOK, lease)
}

type submitRequest struct {
	WorkerID string `json:"worker_id"`
	proof.Submission
}

func (s *Server) handleSubmit(w http.ResponseWriter, r *http.Request) {
	var req submitRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if strings.TrimSpace(req.WorkerID) == "" {
		writeError(w, http.StatusBadRequest, "missing_worker_id", "worker_id is required")
		return
	}

	receipt, err := s.co.Submit(r.Context(), req.WorkerID, req.Submission)
	if err != nil {
		var rej *proof.Rejection
		switch {
		case errors.As(err, &rej):
			// 422, not 400: the request was well-formed, the proof was not.
			writeError(w, http.StatusUnprocessableEntity, rej.Code, rej.Detail)
		case errors.Is(err, store.ErrLeaseInvalid):
			// 409: the lease expired, belongs to another worker, or the block is
			// already settled. The work is real but it cannot be paid for.
			writeError(w, http.StatusConflict, "lease_invalid", err.Error())
		case errors.Is(err, keyspace.ErrBlockOutOfRange):
			writeError(w, http.StatusBadRequest, "block_out_of_range", err.Error())
		default:
			s.log.Error("submit failed", "worker", req.WorkerID, "block", req.BlockIndex, "err", err)
			writeError(w, http.StatusInternalServerError, "internal", "could not record the submission")
		}
		return
	}

	if receipt.Solved {
		// Loud on purpose: this line is the only thing standing between a found
		// key and an operator who never noticed.
		s.log.Warn("SOLUTION FOUND", "worker", req.WorkerID, "campaign", s.co.Campaign().ID, "block", receipt.BlockIndex)
	}
	writeJSON(w, http.StatusOK, receipt)
}

func (s *Server) handleProgress(w http.ResponseWriter, r *http.Request) {
	prog, err := s.co.Progress(r.Context())
	if err != nil {
		s.log.Error("progress failed", "err", err)
		writeError(w, http.StatusInternalServerError, "internal", "could not read progress")
		return
	}
	writeJSON(w, http.StatusOK, prog)
}

func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxSubmissionBytes))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error())
		return false
	}
	return true
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

type errorBody struct {
	Code   string `json:"code"`
	Detail string `json:"detail"`
}

func writeError(w http.ResponseWriter, status int, code, detail string) {
	writeJSON(w, status, errorBody{Code: code, Detail: detail})
}
