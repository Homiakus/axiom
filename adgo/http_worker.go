package adgo

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const HTTPWorkerProtocolVersion = "adgo-worker-v1"

type HTTPWorkerAuthFunc func(*http.Request) bool

type HTTPWorkerServerOptions struct {
	BearerToken string
	Authorize   HTTPWorkerAuthFunc
	MaxBodyBytes int64
}

// HTTPWorkerServer exposes the durable worker protocol without giving remote
// workers direct access to the execution Store. Put it behind TLS and your normal
// service authentication/authorization boundary in production.
type HTTPWorkerServer struct {
	engine *Engine
	auth   HTTPWorkerAuthFunc
	maxBodyBytes int64
}

func NewHTTPWorkerServer(engine *Engine, options HTTPWorkerServerOptions) (*HTTPWorkerServer, error) {
	if engine == nil {
		return nil, fmt.Errorf("adgo: HTTP worker server engine is required")
	}
	auth := options.Authorize
	if auth == nil && options.BearerToken != "" {
		expected := []byte(options.BearerToken)
		auth = func(request *http.Request) bool {
			header := request.Header.Get("Authorization")
			const prefix = "Bearer "
			if !strings.HasPrefix(header, prefix) {
				return false
			}
			actual := []byte(strings.TrimSpace(strings.TrimPrefix(header, prefix)))
			if len(actual) != len(expected) {
				return false
			}
			return subtle.ConstantTimeCompare(actual, expected) == 1
		}
	}
	if options.MaxBodyBytes <= 0 {
		options.MaxBodyBytes = 8 << 20
	}
	return &HTTPWorkerServer{engine: engine, auth: auth, maxBodyBytes: options.MaxBodyBytes}, nil
}

func (s *HTTPWorkerServer) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	writer.Header().Set("Content-Type", "application/json")
	writer.Header().Set("X-ADGO-Worker-Protocol", HTTPWorkerProtocolVersion)
	if s.auth != nil && !s.auth(request) {
		writeHTTPError(writer, http.StatusUnauthorized, "unauthorized", "worker authentication failed")
		return
	}
	if request.Method != http.MethodPost {
		writeHTTPError(writer, http.StatusMethodNotAllowed, "method_not_allowed", "POST required")
		return
	}
	request.Body = http.MaxBytesReader(writer, request.Body, s.maxBodyBytes)
	switch request.URL.Path {
	case "/v1/poll":
		s.handlePoll(writer, request)
	case "/v1/heartbeat":
		s.handleHeartbeat(writer, request)
	case "/v1/complete":
		s.handleComplete(writer, request)
	case "/v1/fail":
		s.handleFail(writer, request)
	default:
		writeHTTPError(writer, http.StatusNotFound, "not_found", "unknown worker endpoint")
	}
}

type remoteWorkItem struct {
	Token      WorkToken       `json:"token"`
	Node       Node            `json:"node"`
	Activity   string          `json:"activity"`
	Provider   string          `json:"provider,omitempty"`
	Request    ActivityRequest `json:"request"`
	LeaseUntil time.Time       `json:"leaseUntil"`
	EnqueuedAt time.Time       `json:"enqueuedAt,omitempty"`
	Score      float64         `json:"score"`
}

func toRemoteWork(item *WorkItem) remoteWorkItem {
	return remoteWorkItem{Token: item.Token, Node: item.Node, Activity: item.Activity, Provider: item.Provider, Request: item.Request, LeaseUntil: item.LeaseUntil, EnqueuedAt: item.EnqueuedAt, Score: item.Score}
}

func fromRemoteWork(item remoteWorkItem) *WorkItem {
	return &WorkItem{Token: item.Token, Node: item.Node, Activity: item.Activity, Provider: item.Provider, Request: item.Request, LeaseUntil: item.LeaseUntil, EnqueuedAt: item.EnqueuedAt, Score: item.Score}
}

func (s *HTTPWorkerServer) handlePoll(writer http.ResponseWriter, request *http.Request) {
	var spec WorkerSpec
	if err := decodeHTTPJSON(request, &spec); err != nil {
		writeHTTPError(writer, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	item, err := s.engine.Poll(request.Context(), spec)
	if errors.Is(err, ErrNoWork) {
		writer.WriteHeader(http.StatusNoContent)
		return
	}
	if err != nil {
		writeHTTPEngineError(writer, err)
		return
	}
	writeHTTPJSON(writer, http.StatusOK, toRemoteWork(item))
}

type heartbeatHTTPRequest struct {
	Token   WorkToken      `json:"token"`
	Details map[string]any `json:"details,omitempty"`
}

func (s *HTTPWorkerServer) handleHeartbeat(writer http.ResponseWriter, request *http.Request) {
	var body heartbeatHTTPRequest
	if err := decodeHTTPJSON(request, &body); err != nil {
		writeHTTPError(writer, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	if err := s.engine.Heartbeat(request.Context(), body.Token, body.Details); err != nil {
		writeHTTPEngineError(writer, err)
		return
	}
	writeHTTPJSON(writer, http.StatusOK, map[string]any{"ok": true})
}

type completeHTTPRequest struct {
	Token         WorkToken      `json:"token"`
	Result        ActivityResult `json:"result"`
	DurationNanos int64          `json:"durationNanos"`
}

func (s *HTTPWorkerServer) handleComplete(writer http.ResponseWriter, request *http.Request) {
	var body completeHTTPRequest
	if err := decodeHTTPJSON(request, &body); err != nil {
		writeHTTPError(writer, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	execution, err := s.engine.Complete(request.Context(), body.Token, body.Result, time.Duration(body.DurationNanos))
	if err != nil {
		writeHTTPEngineError(writer, err)
		return
	}
	writeHTTPJSON(writer, http.StatusOK, ExecutionSummary{ID: execution.ID, PlanID: execution.PlanID, PlanDigest: execution.PlanDigest, Version: execution.Version, Status: execution.Status, Failure: execution.Failure})
}

type RemoteFailure struct {
	Class           FailureClass `json:"class,omitempty"`
	Message         string       `json:"message"`
	RetryAfterNanos int64        `json:"retryAfterNanos,omitempty"`
}

type failHTTPRequest struct {
	Token         WorkToken    `json:"token"`
	Failure       RemoteFailure `json:"failure"`
	DurationNanos int64        `json:"durationNanos"`
}

func (s *HTTPWorkerServer) handleFail(writer http.ResponseWriter, request *http.Request) {
	var body failHTTPRequest
	if err := decodeHTTPJSON(request, &body); err != nil {
		writeHTTPError(writer, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	failure := remoteFailureError(body.Failure)
	execution, err := s.engine.Fail(request.Context(), body.Token, failure, time.Duration(body.DurationNanos))
	if err != nil {
		writeHTTPEngineError(writer, err)
		return
	}
	writeHTTPJSON(writer, http.StatusOK, ExecutionSummary{ID: execution.ID, PlanID: execution.PlanID, PlanDigest: execution.PlanDigest, Version: execution.Version, Status: execution.Status, Failure: execution.Failure})
}

func remoteFailureError(failure RemoteFailure) error {
	message := strings.TrimSpace(failure.Message)
	if message == "" {
		message = "remote worker failure"
	}
	base := errors.New(message)
	if failure.Class == "" {
		return base
	}
	return FailAfter(failure.Class, base, time.Duration(failure.RetryAfterNanos))
}

type httpProtocolError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func decodeHTTPJSON(request *http.Request, target any) error {
	decoder := json.NewDecoder(request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return errors.New("multiple JSON values are not allowed")
		}
		return err
	}
	return nil
}

func writeHTTPJSON(writer http.ResponseWriter, status int, value any) {
	writer.WriteHeader(status)
	_ = json.NewEncoder(writer).Encode(value)
}

func writeHTTPError(writer http.ResponseWriter, status int, code, message string) {
	writeHTTPJSON(writer, status, httpProtocolError{Code: code, Message: message})
}

func writeHTTPEngineError(writer http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrStaleTask):
		writeHTTPError(writer, http.StatusConflict, "stale_task", err.Error())
	case errors.Is(err, ErrConflict):
		writeHTTPError(writer, http.StatusConflict, "conflict", err.Error())
	case errors.Is(err, ErrExecutionNotFound):
		writeHTTPError(writer, http.StatusNotFound, "execution_not_found", err.Error())
	default:
		writeHTTPError(writer, http.StatusInternalServerError, "engine_error", err.Error())
	}
}

// HTTPWorkerClient implements the worker protocol over HTTP. The server may be
// behind a reverse proxy; BaseURL should not include a trailing slash.
type HTTPWorkerClient struct {
	BaseURL     string
	BearerToken string
	Client      *http.Client
}

func (c *HTTPWorkerClient) httpClient() *http.Client {
	if c.Client != nil {
		return c.Client
	}
	return &http.Client{Timeout: 35 * time.Second}
}

func (c *HTTPWorkerClient) Poll(ctx context.Context, spec WorkerSpec) (*WorkItem, error) {
	var remote remoteWorkItem
	status, err := c.post(ctx, "/v1/poll", spec, &remote)
	if err != nil {
		return nil, err
	}
	if status == http.StatusNoContent {
		return nil, ErrNoWork
	}
	return fromRemoteWork(remote), nil
}

func (c *HTTPWorkerClient) Heartbeat(ctx context.Context, token WorkToken, details map[string]any) error {
	_, err := c.post(ctx, "/v1/heartbeat", heartbeatHTTPRequest{Token: token, Details: details}, nil)
	return err
}

func (c *HTTPWorkerClient) Complete(ctx context.Context, token WorkToken, result ActivityResult, duration time.Duration) error {
	_, err := c.post(ctx, "/v1/complete", completeHTTPRequest{Token: token, Result: result, DurationNanos: int64(duration)}, nil)
	return err
}

func (c *HTTPWorkerClient) Fail(ctx context.Context, token WorkToken, failure RemoteFailure, duration time.Duration) error {
	_, err := c.post(ctx, "/v1/fail", failHTTPRequest{Token: token, Failure: failure, DurationNanos: int64(duration)}, nil)
	return err
}

func (c *HTTPWorkerClient) post(ctx context.Context, path string, body any, output any) (int, error) {
	payload, err := json.Marshal(body)
	if err != nil {
		return 0, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(c.BaseURL, "/")+path, strings.NewReader(string(payload)))
	if err != nil {
		return 0, err
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-ADGO-Worker-Protocol", HTTPWorkerProtocolVersion)
	if c.BearerToken != "" {
		request.Header.Set("Authorization", "Bearer "+c.BearerToken)
	}
	response, err := c.httpClient().Do(request)
	if err != nil {
		return 0, err
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusNoContent {
		return response.StatusCode, nil
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		var protocolErr httpProtocolError
		if decodeErr := json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&protocolErr); decodeErr != nil {
			return response.StatusCode, fmt.Errorf("adgo worker HTTP %s", response.Status)
		}
		switch protocolErr.Code {
		case "stale_task":
			return response.StatusCode, fmt.Errorf("%w: %s", ErrStaleTask, protocolErr.Message)
		case "conflict":
			return response.StatusCode, fmt.Errorf("%w: %s", ErrConflict, protocolErr.Message)
		case "execution_not_found":
			return response.StatusCode, fmt.Errorf("%w: %s", ErrExecutionNotFound, protocolErr.Message)
		default:
			return response.StatusCode, fmt.Errorf("adgo worker HTTP %s: %s", protocolErr.Code, protocolErr.Message)
		}
	}
	if output != nil {
		if err := json.NewDecoder(io.LimitReader(response.Body, 8<<20)).Decode(output); err != nil {
			return response.StatusCode, err
		}
	}
	return response.StatusCode, nil
}

// Run executes activity handlers from a local Registry while all durable task
// state remains on the remote Engine server. This is the standard remote-worker
// topology for hosts that should not receive database credentials.
func (c *HTTPWorkerClient) Run(ctx context.Context, spec WorkerSpec, registry *Registry) error {
	if registry == nil {
		return fmt.Errorf("adgo: remote worker registry is required")
	}
	if spec.ID == "" {
		return fmt.Errorf("adgo: remote worker ID is required")
	}
	if spec.Concurrency <= 0 {
		spec.Concurrency = 1
	}
	if spec.PollInterval <= 0 {
		spec.PollInterval = 100 * time.Millisecond
	}
	if spec.LeaseTTL <= 0 {
		spec.LeaseTTL = 30 * time.Second
	}
	workerCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	errCh := make(chan error, spec.Concurrency)
	for slot := 0; slot < spec.Concurrency; slot++ {
		local := spec
		if spec.Concurrency > 1 {
			local.ID = fmt.Sprintf("%s/%d", spec.ID, slot+1)
		}
		go func() { errCh <- c.remoteWorkerLoop(workerCtx, local, registry) }()
	}
	for range spec.Concurrency {
		err := <-errCh
		if err != nil && !errors.Is(err, context.Canceled) {
			cancel()
			return err
		}
	}
	return ctx.Err()
}

func (c *HTTPWorkerClient) remoteWorkerLoop(ctx context.Context, spec WorkerSpec, registry *Registry) error {
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		item, err := c.Poll(ctx, spec)
		if errors.Is(err, ErrNoWork) {
			if err := sleepContext(ctx, spec.PollInterval); err != nil {
				return err
			}
			continue
		}
		if err != nil {
			return err
		}
		if err := c.executeRemote(ctx, spec, registry, item); err != nil && !errors.Is(err, ErrStaleTask) {
			return err
		}
	}
}

func (c *HTTPWorkerClient) executeRemote(ctx context.Context, spec WorkerSpec, registry *Registry, item *WorkItem) error {
	started := time.Now()
	callCtx := ctx
	cancel := func() {}
	if !item.Request.Deadline.IsZero() {
		callCtx, cancel = context.WithDeadline(ctx, item.Request.Deadline)
	}
	defer cancel()
	callCtx = context.WithValue(callCtx, heartbeatContextKey{}, heartbeatFunc(func(details map[string]any) error {
		return c.Heartbeat(callCtx, item.Token, details)
	}))

	heartbeatEvery := spec.LeaseTTL / 3
	if heartbeatEvery <= 0 {
		heartbeatEvery = time.Second
	}
	heartbeatCtx, stopHeartbeat := context.WithCancel(callCtx)
	heartbeatDone := make(chan struct{})
	go func() {
		defer close(heartbeatDone)
		ticker := time.NewTicker(heartbeatEvery)
		defer ticker.Stop()
		for {
			select {
			case <-heartbeatCtx.Done():
				return
			case <-ticker.C:
				if err := c.Heartbeat(heartbeatCtx, item.Token, nil); err != nil {
					return
				}
			}
		}
	}()

	var result ActivityResult
	var callErr error
	if item.Node.Kind == NodeSubflow {
		handler, ok := registry.subflow(item.Activity)
		if !ok {
			callErr = fmt.Errorf("adgo: remote subflow handler %q is not registered", item.Activity)
		} else {
			result, callErr = handler(callCtx, item.Request)
		}
	} else {
		handler, ok := registry.activity(item.Activity)
		if !ok {
			callErr = fmt.Errorf("adgo: remote activity handler %q is not registered", item.Activity)
		} else {
			result, callErr = handler(callCtx, item.Request)
		}
	}
	stopHeartbeat()
	<-heartbeatDone
	duration := time.Since(started)
	if callErr == nil {
		return c.Complete(ctx, item.Token, result, duration)
	}
	failure := RemoteFailure{Message: callErr.Error()}
	var classified *FailureError
	if errors.As(callErr, &classified) {
		failure.Class = classified.Class
		failure.RetryAfterNanos = int64(classified.RetryAfter)
	}
	return c.Fail(ctx, item.Token, failure, duration)
}
