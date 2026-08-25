package saga

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

type HTTPExecutor struct {
	Client           *http.Client
	MaxResponseBytes int64
}

func (e HTTPExecutor) Execute(ctx context.Context, a Action, sagaID string, stepID int, args any) (any, error) {
	return e.call(ctx, a, a.ExecuteURL, map[string]any{"saga_id": sagaID, "step_id": stepID, "arguments": args})
}
func (e HTTPExecutor) Compensate(ctx context.Context, a Action, sagaID string, step Step) error {
	_, err := e.call(ctx, a, a.CompensateURL, map[string]any{"saga_id": sagaID, "step_id": step.ID, "arguments": step.Arguments, "result": step.Result})
	return err
}
func (e HTTPExecutor) call(ctx context.Context, a Action, url string, payload any) (any, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range a.Headers {
		req.Header.Set(k, v)
	}
	client := e.Client
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	limit := e.MaxResponseBytes
	if limit <= 0 {
		limit = 1 << 20
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(raw)) > limit {
		return nil, fmt.Errorf("upstream response exceeds %d bytes", limit)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("upstream returned status %d", resp.StatusCode)
	}
	if len(raw) == 0 {
		return map[string]any{}, nil
	}
	var result any
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, fmt.Errorf("upstream returned invalid JSON")
	}
	return result, nil
}
