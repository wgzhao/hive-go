package hive

import (
	"context"
	"fmt"
	"time"

	"github.com/wgzhao/hive-go/internal/tcliservice"
)

// Execute runs one SQL statement asynchronously and waits for it to finish.
// Statements without a result set return a Result whose Next yields io.EOF.
func (c *Client) Execute(ctx context.Context, sql string) (*Result, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if c.session == nil || c.tc == nil {
		return nil, ErrClosed
	}

	req := tcliservice.NewTExecuteStatementReq()
	req.SessionHandle = c.session
	req.Statement = sql
	req.RunAsync = true
	req.QueryTimeout = int64(c.cfg.QueryTimeout)
	if len(c.cfg.ConfOverlay) > 0 {
		req.ConfOverlay = c.cfg.ConfOverlay
	}

	resp, err := c.tc.ExecuteStatement(ctx, req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, fmt.Errorf("%w: %v", ErrCanceled, err)
		}
		return nil, fmt.Errorf("ExecuteStatement: %w", err)
	}
	if err := statusError("ExecuteStatement", resp.Status); err != nil {
		return nil, err
	}
	if resp.OperationHandle == nil {
		return nil, fmt.Errorf("ExecuteStatement: server returned no operation handle")
	}

	r := &Result{
		client:    c,
		op:        resp.OperationHandle,
		fetchSize: c.cfg.FetchSize,
	}
	r.addInfo(statusInfo(resp.Status))
	c.logQueryID(ctx, r.op)

	if err := r.wait(ctx); err != nil {
		_ = r.Close()
		return nil, err
	}
	return r, nil
}

func (r *Result) wait(ctx context.Context) error {
	verbose := r.client.cfg.Logf != nil
	var lastState tcliservice.TOperationState
	stateLogged := false
	var lastPct float64
	pctLogged := false

	backoff := 50 * time.Millisecond
	for {
		req := tcliservice.NewTGetOperationStatusReq()
		req.OperationHandle = r.op
		if verbose {
			getProgress := true
			req.GetProgressUpdate = &getProgress
		}
		resp, err := r.client.tc.GetOperationStatus(ctx, req)
		if err != nil {
			if ctx.Err() != nil {
				return r.cancel()
			}
			return fmt.Errorf("GetOperationStatus: %w", err)
		}
		if err := statusError("GetOperationStatus", resp.Status); err != nil {
			if ctx.Err() != nil {
				return r.cancel()
			}
			return err
		}
		r.addInfo(statusInfo(resp.Status))

		if verbose {
			state := resp.GetOperationState()
			if !stateLogged || state != lastState {
				r.client.logf("Operation state: %s", state.String())
				lastState = state
				stateLogged = true
			}
			if progress := resp.ProgressUpdateResponse; progress != nil {
				pct := progress.GetProgressedPercentage()
				if !pctLogged || pct != lastPct {
					r.client.logf("Progress: %.1f%% %s", pct, progress.GetFooterSummary())
					lastPct = pct
					pctLogged = true
				}
			}
		}

		switch resp.GetOperationState() {
		case tcliservice.TOperationState_FINISHED_STATE:
			r.hasResultSet = r.op.GetHasResultSet()
			if resp.IsSetHasResultSet() {
				r.hasResultSet = resp.GetHasResultSet()
			}
			if resp.IsSetNumModifiedRows() {
				r.rowsAffected = resp.GetNumModifiedRows()
				r.hasRowsAffected = r.rowsAffected >= 0
			}
			if !r.hasRowsAffected && r.op.IsSetModifiedRowCount() {
				r.rowsAffected = int64(r.op.GetModifiedRowCount())
				r.hasRowsAffected = r.rowsAffected >= 0
			}
			if !r.hasResultSet {
				r.done = true
				return nil
			}
			return r.loadMetadata(ctx)

		case tcliservice.TOperationState_ERROR_STATE:
			return &ServerError{
				Stage:    "query",
				SQLState: resp.GetSqlState(),
				Code:     resp.GetErrorCode(),
				Message:  resp.GetErrorMessage(),
			}
		case tcliservice.TOperationState_CANCELED_STATE:
			return ErrCanceled
		case tcliservice.TOperationState_CLOSED_STATE:
			return fmt.Errorf("query: operation was closed")
		case tcliservice.TOperationState_TIMEDOUT_STATE:
			return fmt.Errorf("query: operation timed out")
		}

		// PENDING, RUNNING, INITIALIZED, UKNOWN and any future state are
		// treated as "keep waiting".
		select {
		case <-ctx.Done():
			return r.cancel()
		case <-time.After(backoff):
		}
		if backoff < time.Second {
			backoff *= 2
			if backoff > time.Second {
				backoff = time.Second
			}
		}
	}
}

func (r *Result) loadMetadata(ctx context.Context) error {
	req := tcliservice.NewTGetResultSetMetadataReq()
	req.OperationHandle = r.op
	resp, err := r.client.tc.GetResultSetMetadata(ctx, req)
	if err != nil {
		if ctx.Err() != nil {
			return r.cancel()
		}
		return fmt.Errorf("GetResultSetMetadata: %w", err)
	}
	if err := statusError("GetResultSetMetadata", resp.Status); err != nil {
		return err
	}
	r.addInfo(statusInfo(resp.Status))

	if resp.Schema == nil {
		r.hasResultSet = false
		r.done = true
		return nil
	}
	r.columns = make([]Column, 0, len(resp.Schema.Columns))
	for _, desc := range resp.Schema.Columns {
		if desc == nil {
			r.columns = append(r.columns, Column{Name: "?", Type: "UNKNOWN"})
			continue
		}
		r.columns = append(r.columns, columnFromDesc(desc))
	}
	return nil
}

func (r *Result) cancel() error {
	if r.client == nil || r.client.tc == nil {
		return ErrCanceled
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	req := tcliservice.NewTCancelOperationReq()
	req.OperationHandle = r.op
	resp, err := r.client.tc.CancelOperation(ctx, req)
	if err != nil {
		return fmt.Errorf("%w (CancelOperation failed: %v)", ErrCanceled, err)
	}
	if err := statusError("CancelOperation", resp.Status); err != nil {
		return fmt.Errorf("%w (%v)", ErrCanceled, err)
	}
	return ErrCanceled
}
