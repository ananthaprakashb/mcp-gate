// Package saga coordinates forward actions and their compensating actions.
package saga

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

type Status string

const (
	Active         Status = "active"
	Committed      Status = "committed"
	RollingBack    Status = "rolling_back"
	RolledBack     Status = "rolled_back"
	RollbackFailed Status = "rollback_failed"
)

type Action struct {
	Name          string            `json:"name"`
	ExecuteURL    string            `json:"execute_url"`
	CompensateURL string            `json:"compensate_url"`
	Headers       map[string]string `json:"headers,omitempty"`
}

type Step struct {
	ID                int        `json:"id"`
	Action            string     `json:"action"`
	Arguments         any        `json:"arguments"`
	Result            any        `json:"result,omitempty"`
	ExecutedAt        time.Time  `json:"executed_at"`
	CompensatedAt     *time.Time `json:"compensated_at,omitempty"`
	CompensationError string     `json:"compensation_error,omitempty"`
}

type Saga struct {
	ID        string    `json:"id"`
	Status    Status    `json:"status"`
	Steps     []Step    `json:"steps"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	Failure   string    `json:"failure,omitempty"`
}

type Executor interface {
	Execute(context.Context, Action, string, int, any) (any, error)
	Compensate(context.Context, Action, string, Step) error
}

type Coordinator struct {
	mu       sync.Mutex
	sagas    map[string]*Saga
	actions  map[string]Action
	executor Executor
	now      func() time.Time
}

func New(actions []Action, executor Executor) (*Coordinator, error) {
	if executor == nil {
		return nil, errors.New("executor is required")
	}
	c := &Coordinator{sagas: make(map[string]*Saga), actions: make(map[string]Action), executor: executor, now: time.Now}
	for _, action := range actions {
		if action.Name == "" || action.ExecuteURL == "" || action.CompensateURL == "" {
			return nil, fmt.Errorf("action %q requires name, execute_url, and compensate_url", action.Name)
		}
		if _, exists := c.actions[action.Name]; exists {
			return nil, fmt.Errorf("duplicate action %q", action.Name)
		}
		c.actions[action.Name] = action
	}
	return c, nil
}

func (c *Coordinator) Begin(id string) (Saga, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if id == "" {
		return Saga{}, errors.New("saga_id is required")
	}
	if _, exists := c.sagas[id]; exists {
		return Saga{}, errors.New("saga already exists")
	}
	now := c.now().UTC()
	s := &Saga{ID: id, Status: Active, Steps: []Step{}, CreatedAt: now, UpdatedAt: now}
	c.sagas[id] = s
	return clone(s), nil
}

// Execute serializes mutations per coordinator. This deliberately favors a
// simple, deterministic execution log over throughput: no rollback can race a
// forward action.
func (c *Coordinator) Execute(ctx context.Context, id, actionName string, args any) (Saga, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	s, ok := c.sagas[id]
	if !ok {
		return Saga{}, errors.New("saga not found")
	}
	if s.Status != Active {
		return clone(s), fmt.Errorf("saga is %s", s.Status)
	}
	action, ok := c.actions[actionName]
	if !ok {
		return clone(s), errors.New("unknown action")
	}
	stepID := len(s.Steps) + 1
	result, err := c.executor.Execute(ctx, action, id, stepID, args)
	if err != nil {
		s.Failure = err.Error()
		s.UpdatedAt = c.now().UTC()
		rbErr := c.rollbackLocked(ctx, s)
		if rbErr != nil {
			return clone(s), fmt.Errorf("action failed: %v; rollback failed: %w", err, rbErr)
		}
		return clone(s), fmt.Errorf("action failed and saga rolled back: %w", err)
	}
	s.Steps = append(s.Steps, Step{ID: stepID, Action: actionName, Arguments: args, Result: result, ExecutedAt: c.now().UTC()})
	s.UpdatedAt = c.now().UTC()
	return clone(s), nil
}

func (c *Coordinator) Commit(id string) (Saga, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	s, ok := c.sagas[id]
	if !ok {
		return Saga{}, errors.New("saga not found")
	}
	if s.Status != Active {
		return clone(s), fmt.Errorf("saga is %s", s.Status)
	}
	s.Status = Committed
	s.UpdatedAt = c.now().UTC()
	return clone(s), nil
}
func (c *Coordinator) Rollback(ctx context.Context, id string) (Saga, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	s, ok := c.sagas[id]
	if !ok {
		return Saga{}, errors.New("saga not found")
	}
	if s.Status == RolledBack {
		return clone(s), nil
	}
	if s.Status != Active && s.Status != RollbackFailed {
		return clone(s), fmt.Errorf("saga is %s", s.Status)
	}
	err := c.rollbackLocked(ctx, s)
	return clone(s), err
}
func (c *Coordinator) Get(id string) (Saga, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	s, ok := c.sagas[id]
	if !ok {
		return Saga{}, errors.New("saga not found")
	}
	return clone(s), nil
}

func (c *Coordinator) rollbackLocked(ctx context.Context, s *Saga) error {
	s.Status = RollingBack
	s.UpdatedAt = c.now().UTC()
	var errs []error
	for i := len(s.Steps) - 1; i >= 0; i-- {
		step := &s.Steps[i]
		if step.CompensatedAt != nil {
			continue
		}
		action := c.actions[step.Action]
		if err := c.executor.Compensate(ctx, action, s.ID, *step); err != nil {
			step.CompensationError = err.Error()
			errs = append(errs, fmt.Errorf("step %d: %w", step.ID, err))
			continue
		}
		now := c.now().UTC()
		step.CompensatedAt = &now
		step.CompensationError = ""
	}
	s.UpdatedAt = c.now().UTC()
	if len(errs) > 0 {
		s.Status = RollbackFailed
		return errors.Join(errs...)
	}
	s.Status = RolledBack
	return nil
}
func clone(s *Saga) Saga { out := *s; out.Steps = append([]Step(nil), s.Steps...); return out }
