package saga

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

type fakeExecutor struct {
	executed, compensated []string
	executeErr            error
	failCompensation      map[int]bool
}

func (f *fakeExecutor) Execute(_ context.Context, a Action, _ string, _ int, _ any) (any, error) {
	f.executed = append(f.executed, a.Name)
	if f.executeErr != nil {
		return nil, f.executeErr
	}
	return map[string]any{"resource_id": a.Name + "-1"}, nil
}
func (f *fakeExecutor) Compensate(_ context.Context, _ Action, _ string, s Step) error {
	f.compensated = append(f.compensated, s.Action)
	if f.failCompensation[s.ID] {
		delete(f.failCompensation, s.ID)
		return errors.New("temporary failure")
	}
	return nil
}
func coordinator(t *testing.T, e *fakeExecutor) *Coordinator {
	t.Helper()
	c, err := New([]Action{{Name: "reserve", ExecuteURL: "http://execute", CompensateURL: "http://undo"}, {Name: "charge", ExecuteURL: "http://execute", CompensateURL: "http://undo"}}, e)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestRollbackCompensatesInReverseOrder(t *testing.T) {
	e := &fakeExecutor{}
	c := coordinator(t, e)
	if _, err := c.Begin("order-1"); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Execute(context.Background(), "order-1", "reserve", map[string]any{"sku": "x"}); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Execute(context.Background(), "order-1", "charge", map[string]any{"amount": 10}); err != nil {
		t.Fatal(err)
	}
	got, err := c.Rollback(context.Background(), "order-1")
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != RolledBack {
		t.Fatalf("status=%s", got.Status)
	}
	if !reflect.DeepEqual(e.compensated, []string{"charge", "reserve"}) {
		t.Fatalf("compensation order=%v", e.compensated)
	}
	if _, err := c.Rollback(context.Background(), "order-1"); err != nil {
		t.Fatalf("idempotent rollback: %v", err)
	}
	if len(e.compensated) != 2 {
		t.Fatal("steps were compensated twice")
	}
}
func TestExecuteFailureAutomaticallyRollsBack(t *testing.T) {
	e := &fakeExecutor{}
	c := coordinator(t, e)
	_, _ = c.Begin("order-2")
	_, _ = c.Execute(context.Background(), "order-2", "reserve", nil)
	e.executeErr = errors.New("card declined")
	got, err := c.Execute(context.Background(), "order-2", "charge", nil)
	if err == nil {
		t.Fatal("expected failure")
	}
	if got.Status != RolledBack || !reflect.DeepEqual(e.compensated, []string{"reserve"}) {
		t.Fatalf("saga=%+v compensated=%v", got, e.compensated)
	}
	if len(got.Steps) != 1 {
		t.Fatalf("failed forward step must not be logged: %+v", got.Steps)
	}
}
func TestFailedCompensationCanBeRetried(t *testing.T) {
	e := &fakeExecutor{failCompensation: map[int]bool{1: true}}
	c := coordinator(t, e)
	_, _ = c.Begin("order-3")
	_, _ = c.Execute(context.Background(), "order-3", "reserve", nil)
	got, err := c.Rollback(context.Background(), "order-3")
	if err == nil || got.Status != RollbackFailed {
		t.Fatalf("first rollback status=%s err=%v", got.Status, err)
	}
	got, err = c.Rollback(context.Background(), "order-3")
	if err != nil || got.Status != RolledBack {
		t.Fatalf("retry status=%s err=%v", got.Status, err)
	}
}
func TestCommittedSagaCannotMutate(t *testing.T) {
	c := coordinator(t, &fakeExecutor{})
	_, _ = c.Begin("done")
	_, _ = c.Commit("done")
	if _, err := c.Rollback(context.Background(), "done"); err == nil {
		t.Fatal("committed saga rolled back")
	}
	if _, err := c.Execute(context.Background(), "done", "reserve", nil); err == nil {
		t.Fatal("committed saga executed")
	}
}
