// Package mobile implements the gRPC mobile API as a thin adapter over the
// existing engine, store, and auth packages. It exposes the same capabilities
// as the REST API (webhook trigger, resume, stream, etc.) through a protobuf
// contract designed for mobile clients.
package mobile

import (
	"context"
	"encoding/json"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/CrossCraftAI/crosscraft-brain/server/internal/auth"
	"github.com/CrossCraftAI/crosscraft-brain/server/internal/engine"
	"github.com/CrossCraftAI/crosscraft-brain/server/internal/schema"
	"github.com/CrossCraftAI/crosscraft-brain/server/internal/store"

	crosscraft "github.com/CrossCraftAI/crosscraft-brain/server/internal/mobile/gen"
)

// Authenticator validates API keys. *auth.Service satisfies this.
type Authenticator interface {
	Validate(ctx context.Context, raw string) (*auth.APIKey, error)
}

// Store is the persistence surface the gRPC handlers need. It combines the
// engine's execution contract with the read queries the mobile API exposes.
// *store.Store satisfies this.
type Store interface {
	// Engine contract (execution lifecycle).
	engine.Store

	// Read queries exposed to mobile clients.
	ListActiveWorkflows(ctx context.Context) ([]schema.Workflow, error)
	ListWorkflows(ctx context.Context) ([]store.WorkflowSummary, error)
	GetExecutionStatus(ctx context.Context, eid string) (store.ExecStatus, error)
	GetExecutionSteps(ctx context.Context, executionID string) ([]schema.StepRecord, error)
}

// Server implements the CrossCraftMobile gRPC service. It is a thin adapter
// over the existing engine, store, and auth packages — no business logic lives
// here.
type Server struct {
	crosscraft.UnimplementedCrossCraftMobileServer
	store   Store
	eng     *engine.Engine
	authSvc Authenticator
	pool    *pgxpool.Pool
}

// NewServer creates a new gRPC server with the given dependencies.
func NewServer(st Store, eng *engine.Engine, authSvc Authenticator, pool *pgxpool.Pool) *Server {
	return &Server{
		store:   st,
		eng:     eng,
		authSvc: authSvc,
		pool:    pool,
	}
}

// authenticate validates the API key from the request. Returns a gRPC status
// error on failure so callers can return it directly.
func (s *Server) authenticate(ctx context.Context, apiKey string) error {
	if apiKey == "" {
		return status.Error(codes.Unauthenticated, "api_key is required")
	}
	if s.authSvc == nil {
		return status.Error(codes.Unavailable, "auth service not configured")
	}
	key, err := s.authSvc.Validate(ctx, apiKey)
	if err != nil {
		return status.Errorf(codes.Internal, "auth error: %v", err)
	}
	if key == nil {
		return status.Error(codes.Unauthenticated, "invalid api_key")
	}
	return nil
}

// findWebhookWorkflow replicates the webhook-lookup logic from api.go's
// webhook handler: scan active workflows for a node whose type and path param
// match the requested path.
func (s *Server) findWebhookWorkflow(ctx context.Context, webhookPath string) (*schema.Workflow, error) {
	wfs, err := s.store.ListActiveWorkflows(ctx)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to list workflows: %v", err)
	}
	for i := range wfs {
		for _, n := range wfs[i].Nodes {
			if n.Type == "core.webhookTrigger" || n.Type == "core.formTrigger" {
				if p, _ := n.Params["path"].(string); p == webhookPath {
					return &wfs[i], nil
				}
			}
		}
	}
	return nil, nil
}

// TriggerWorkflow finds an active workflow with a matching webhook path and
// runs it. Equivalent to POST /api/webhook/{path} in the REST API.
func (s *Server) TriggerWorkflow(ctx context.Context, req *crosscraft.TriggerRequest) (*crosscraft.TriggerResponse, error) {
	if err := s.authenticate(ctx, req.ApiKey); err != nil {
		return nil, err
	}
	if req.WebhookPath == "" {
		return nil, status.Error(codes.InvalidArgument, "webhook_path is required")
	}

	target, err := s.findWebhookWorkflow(ctx, req.WebhookPath)
	if err != nil {
		return nil, err
	}
	if target == nil {
		return nil, status.Errorf(codes.NotFound, "no active workflow for webhook path %q", req.WebhookPath)
	}

	// Build trigger items from request params.
	triggerJSON := map[string]any{}
	for k, v := range req.Params {
		triggerJSON[k] = v
	}
	items := []schema.Item{{JSON: triggerJSON}}

	res, err := s.eng.Run(ctx, target, items)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "workflow execution failed: %v", err)
	}

	resp := &crosscraft.TriggerResponse{
		ExecutionId: res.ExecutionID,
		Status:      res.Status,
	}

	// If the workflow responded via webhookRespond, include the response.
	if res.Respond != nil {
		resp.RespondStatus = int32(res.Respond.Status)
		if bodyBytes, err := json.Marshal(res.Respond.Body); err == nil {
			resp.RespondBody = string(bodyBytes)
		}
	}

	return resp, nil
}

// ResumeExecution resumes a waiting execution with the given payload.
// Equivalent to POST /api/resume/{id} in the REST API.
func (s *Server) ResumeExecution(ctx context.Context, req *crosscraft.ResumeRequest) (*crosscraft.ResumeResponse, error) {
	if err := s.authenticate(ctx, req.ApiKey); err != nil {
		return nil, err
	}
	if req.ExecutionId == "" {
		return nil, status.Error(codes.InvalidArgument, "execution_id is required")
	}

	// Parse payload JSON — tolerate empty/missing payload.
	var payload []schema.Item
	if req.PayloadJson != "" {
		var m map[string]any
		if err := json.Unmarshal([]byte(req.PayloadJson), &m); err != nil {
			return nil, status.Errorf(codes.InvalidArgument, "invalid payload_json: %v", err)
		}
		payload = []schema.Item{{JSON: m}}
	} else {
		payload = []schema.Item{{JSON: map[string]any{}}}
	}

	res, err := s.eng.Resume(ctx, req.ExecutionId, payload)
	if err != nil {
		return nil, status.Errorf(codes.FailedPrecondition, "resume failed: %v", err)
	}

	resp := &crosscraft.ResumeResponse{
		ExecutionId: res.ExecutionID,
		Status:      res.Status,
	}
	if res.Outputs != nil {
		if outBytes, err := json.Marshal(res.Outputs); err == nil {
			resp.OutputsJson = string(outBytes)
		}
	}

	return resp, nil
}

// StreamExecution streams execution events as the workflow runs. Equivalent to
// the SSE stream at GET /api/executions/{id}/stream in the REST API.
func (s *Server) StreamExecution(req *crosscraft.StreamRequest, stream grpc.ServerStreamingServer[crosscraft.ExecutionEvent]) error {
	if err := s.authenticate(stream.Context(), req.ApiKey); err != nil {
		return err
	}
	if req.ExecutionId == "" {
		return status.Error(codes.InvalidArgument, "execution_id is required")
	}

	ctx := stream.Context()
	for i := 0; i < 600; i++ {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		st, err := s.store.GetExecutionStatus(ctx, req.ExecutionId)
		if err != nil || !st.Found {
			return status.Error(codes.NotFound, "execution not found")
		}

		steps, _ := s.store.GetExecutionSteps(ctx, req.ExecutionId)
		stepEvents := make([]*crosscraft.StepEvent, len(steps))
		for j, step := range steps {
			outputJSON := ""
			if outBytes, err := json.Marshal(step.Output); err == nil {
				outputJSON = string(outBytes)
			}
			stepEvents[j] = &crosscraft.StepEvent{
				NodeId:     step.NodeID,
				Status:     step.Status,
				OutputJson: outputJSON,
			}
		}

		event := &crosscraft.ExecutionEvent{
			ExecutionId: req.ExecutionId,
			Status:      st.Status,
			Steps:       stepEvents,
		}
		if err := stream.Send(event); err != nil {
			return err
		}

		if st.Status == "success" || st.Status == "error" {
			return nil
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(700 * time.Millisecond):
		}
	}
	return nil
}

// GetExecution returns the current status and steps of an execution.
func (s *Server) GetExecution(ctx context.Context, req *crosscraft.GetExecutionRequest) (*crosscraft.GetExecutionResponse, error) {
	if err := s.authenticate(ctx, req.ApiKey); err != nil {
		return nil, err
	}
	if req.ExecutionId == "" {
		return nil, status.Error(codes.InvalidArgument, "execution_id is required")
	}

	st, err := s.store.GetExecutionStatus(ctx, req.ExecutionId)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to get execution: %v", err)
	}
	if !st.Found {
		return nil, status.Error(codes.NotFound, "execution not found")
	}

	steps, err := s.store.GetExecutionSteps(ctx, req.ExecutionId)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to get steps: %v", err)
	}

	stepsJSON := "[]"
	if stepsBytes, err := json.Marshal(steps); err == nil {
		stepsJSON = string(stepsBytes)
	}

	return &crosscraft.GetExecutionResponse{
		ExecutionId: req.ExecutionId,
		Status:      st.Status,
		StepsJson:   stepsJSON,
	}, nil
}

// RegisterDevice stores a device token for push notification targeting.
func (s *Server) RegisterDevice(ctx context.Context, req *crosscraft.RegisterDeviceRequest) (*crosscraft.RegisterDeviceResponse, error) {
	if err := s.authenticate(ctx, req.ApiKey); err != nil {
		return nil, err
	}
	if req.DeviceToken == "" {
		return nil, status.Error(codes.InvalidArgument, "device_token is required")
	}
	if req.Platform == "" {
		return nil, status.Error(codes.InvalidArgument, "platform is required (ios or android)")
	}

	// Get the API key ID to link the device to the owning key.
	key, err := s.authSvc.Validate(ctx, req.ApiKey)
	if err != nil || key == nil {
		return nil, status.Error(codes.Unauthenticated, "invalid api_key")
	}

	_, err = s.pool.Exec(ctx, `
		INSERT INTO device_tokens (id, api_key_id, device_token, platform, created_at, updated_at)
		VALUES (gen_random_uuid()::text, $1, $2, $3, now(), now())
		ON CONFLICT (device_token) DO UPDATE SET
			api_key_id = EXCLUDED.api_key_id,
			platform = EXCLUDED.platform,
			updated_at = now()
	`, key.ID, req.DeviceToken, req.Platform)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to register device: %v", err)
	}

	return &crosscraft.RegisterDeviceResponse{Ok: true}, nil
}

// ListWorkflows returns all workflows as a JSON array.
func (s *Server) ListWorkflows(ctx context.Context, req *crosscraft.ListWorkflowsRequest) (*crosscraft.ListWorkflowsResponse, error) {
	if err := s.authenticate(ctx, req.ApiKey); err != nil {
		return nil, err
	}

	wfs, err := s.store.ListWorkflows(ctx)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to list workflows: %v", err)
	}

	wfJSON := "[]"
	if wfBytes, err := json.Marshal(wfs); err == nil {
		wfJSON = string(wfBytes)
	}

	return &crosscraft.ListWorkflowsResponse{WorkflowsJson: wfJSON}, nil
}
