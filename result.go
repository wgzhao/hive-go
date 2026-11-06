package hive

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/wgzhao/hive-go/internal/tcliservice"
)

// Result is an executed statement. For statements with a result set, call
// Next until io.EOF, then Close. For statements without one, Next returns
// io.EOF immediately.
type Result struct {
	client *Client
	op     *tcliservice.TOperationHandle

	columns         []Column
	info            []string
	warnings        []string
	hasResultSet    bool
	rowsAffected    int64
	hasRowsAffected bool

	batch         []Row
	idx           int
	done          bool
	emptyBatches  int
	fetchSize     int
	fetches       int
	lastOffset    int64
	blobBatches   int
	columnBatches int
	rowBatches    int

	closed bool
}

// Columns returns the result-set columns. It is empty when the statement did
// not produce a result set.
func (r *Result) Columns() []Column {
	out := make([]Column, len(r.columns))
	copy(out, r.columns)
	return out
}

// RowsAffected returns the server-reported modified row count, if known.
func (r *Result) RowsAffected() (int64, bool) {
	return r.rowsAffected, r.hasRowsAffected
}

// InfoMessages returns SUCCESS_WITH_INFO messages from the server.
func (r *Result) InfoMessages() []string {
	out := make([]string, len(r.info))
	copy(out, r.info)
	return out
}

// Warnings returns client-side warnings accumulated while fetching.
func (r *Result) Warnings() []string {
	out := make([]string, len(r.warnings))
	copy(out, r.warnings)
	return out
}

// FetchStats reports how many fetched batches used the blob, columnar and
// row-based result encodings.
func (r *Result) FetchStats() (blob, columnar, rowBased int) {
	return r.blobBatches, r.columnBatches, r.rowBatches
}

// Next returns the next row, or io.EOF at the end of the result set.
func (r *Result) Next(ctx context.Context) (Row, error) {
	if r.closed {
		return nil, io.EOF
	}
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrCanceled, err)
	}
	for {
		if r.idx < len(r.batch) {
			row := r.batch[r.idx]
			r.idx++
			return row, nil
		}
		if r.done || !r.hasResultSet {
			r.done = true
			return nil, io.EOF
		}
		if err := r.fetch(ctx); err != nil {
			return nil, err
		}
	}
}

func (r *Result) fetch(ctx context.Context) error {
	if r.client == nil || r.client.tc == nil {
		return ErrClosed
	}
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("%w: %v", ErrCanceled, err)
	}
	r.fetches++
	req := tcliservice.NewTFetchResultsReq()
	req.OperationHandle = r.op
	req.Orientation = tcliservice.TFetchOrientation_FETCH_NEXT
	req.MaxRows = int64(r.fetchSize)

	resp, err := r.client.tc.FetchResults(ctx, req)
	if err != nil {
		if ctx.Err() != nil {
			return fmt.Errorf("%w: %v", ErrCanceled, err)
		}
		return fmt.Errorf("FetchResults: %w", err)
	}
	if err := statusError("FetchResults", resp.Status); err != nil {
		return err
	}
	r.addInfo(statusInfo(resp.Status))

	if resp.Results == nil {
		r.done = true
		return nil
	}
	r.lastOffset = resp.Results.StartRowOffset
	switch {
	case len(resp.Results.BinaryColumns) > 0:
		r.blobBatches++
	case len(resp.Results.Columns) > 0:
		r.columnBatches++
	default:
		r.rowBatches++
	}

	rows, err := decodeRowSet(ctx, resp.Results, r.columns)
	if err != nil {
		return fmt.Errorf("decode results: %w", err)
	}
	r.batch = rows
	r.idx = 0

	if len(rows) == 0 {
		// An empty batch is the only reliable end-of-results signal.
		// Hive 3 has been observed to return hasMoreRows=false together
		// with a non-empty batch while more rows are still available.
		if resp.IsSetHasMoreRows() && resp.GetHasMoreRows() {
			r.emptyBatches++
			if r.emptyBatches >= 3 {
				r.warnings = append(r.warnings,
					"server reported more rows but returned three empty batches; stopping")
				r.done = true
			}
			return nil
		}
		r.done = true
		return nil
	}

	r.emptyBatches = 0
	return nil
}

func (r *Result) addInfo(messages []string) {
	if len(messages) > 0 {
		r.info = append(r.info, messages...)
	}
}

// Close releases the server-side operation. It is safe to call more than once.
func (r *Result) Close() error {
	if r.closed {
		return nil
	}
	r.closed = true
	r.batch = nil
	if r.op == nil || r.client == nil || r.client.tc == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	req := tcliservice.NewTCloseOperationReq()
	req.OperationHandle = r.op
	resp, err := r.client.tc.CloseOperation(ctx, req)
	if err != nil {
		return fmt.Errorf("CloseOperation: %w", err)
	}
	return statusError("CloseOperation", resp.Status)
}
