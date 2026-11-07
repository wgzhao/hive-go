package main

import (
	"bufio"
	"encoding/base64"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/mattn/go-runewidth"
	hive "github.com/wgzhao/hive-go"
)

type resultWriter interface {
	WriteHeader(columns []hive.Column) error
	WriteRow(row hive.Row) error
	Close() error
}

func newResultWriter(opts *resolvedOptions, out io.Writer) resultWriter {
	switch opts.format {
	case "vertical":
		return &verticalWriter{out: out, opts: opts, nameWidth: 1}
	case "csv":
		return &csvResultWriter{csv: csv.NewWriter(out), opts: opts}
	case "tsv":
		return &tsvWriter{out: bufio.NewWriter(out), opts: opts}
	case "json":
		return &jsonWriter{out: out, binaryFormat: opts.binaryFormat}
	case "jsonl":
		return &jsonlWriter{out: out, binaryFormat: opts.binaryFormat}
	default:
		return newTableWriter(opts, out)
	}
}

type tableWriter struct {
	out     io.Writer
	opts    *resolvedOptions
	columns []hive.Column
	rows    [][]string

	stream  bool
	started bool
	widths  []int
	numeric []bool
}

// tableStreamBuffer is the number of rows buffered before a streaming table
// fixes its column widths. It is deliberately independent of --fetch-size:
// a tiny fetch size should not produce narrow columns.
const tableStreamBuffer = 1000

func newTableWriter(opts *resolvedOptions, out io.Writer) *tableWriter {
	return &tableWriter{
		out:    out,
		opts:   opts,
		stream: opts.stream,
	}
}

func (w *tableWriter) WriteHeader(columns []hive.Column) error {
	w.columns = columns
	w.numeric = make([]bool, len(columns))
	for i, col := range columns {
		w.numeric[i] = isNumericType(col.TypeID)
	}
	return nil
}

func (w *tableWriter) WriteRow(row hive.Row) error {
	cells := make([]string, len(row))
	for i, value := range row {
		col := hive.Column{}
		if i < len(w.columns) {
			col = w.columns[i]
		}
		cells[i] = escapeText(formatCell(col, value, w.opts))
	}
	if w.stream && w.started {
		return w.writeRow(cells)
	}
	w.rows = append(w.rows, cells)
	if w.stream && len(w.rows) >= tableStreamBuffer {
		return w.start()
	}
	return nil
}

func (w *tableWriter) start() error {
	if w.started {
		return nil
	}
	w.widths = w.measure()
	var b strings.Builder
	writeBorder(&b, w.widths)
	if !w.opts.noHeader {
		writeTableRow(&b, headerCells(w.columns), w.widths, nil)
		writeBorder(&b, w.widths)
	}
	for _, row := range w.rows {
		writeTableRow(&b, row, w.widths, w.numeric)
	}
	if _, err := io.WriteString(w.out, b.String()); err != nil {
		return err
	}
	w.rows = nil
	w.started = true
	return nil
}

func (w *tableWriter) measure() []int {
	widths := make([]int, len(w.columns))
	for i, col := range w.columns {
		widths[i] = displayWidth(escapeText(col.Name))
	}
	for _, row := range w.rows {
		for i, cell := range row {
			if i < len(widths) {
				if n := displayWidth(cell); n > widths[i] {
					widths[i] = n
				}
			}
		}
	}
	if w.opts.maxColumnWidth > 0 {
		for i, width := range widths {
			if width > w.opts.maxColumnWidth {
				widths[i] = w.opts.maxColumnWidth
			}
		}
	}
	return widths
}

func (w *tableWriter) writeRow(cells []string) error {
	if w.needsWiden(cells) {
		if err := w.reblock(cells); err != nil {
			return err
		}
	}
	var b strings.Builder
	writeTableRow(&b, cells, w.widths, w.numeric)
	_, err := io.WriteString(w.out, b.String())
	return err
}

func (w *tableWriter) needsWiden(cells []string) bool {
	if w.opts.maxColumnWidth > 0 {
		// Widths are capped on purpose; wider cells are truncated.
		return false
	}
	for i, cell := range cells {
		if i < len(w.widths) && displayWidth(cell) > w.widths[i] {
			return true
		}
	}
	return false
}

// reblock closes the current table block and starts a new one with wider
// columns. Streaming output cannot guess the final widths up front, so this
// keeps values intact instead of silently truncating them.
func (w *tableWriter) reblock(cells []string) error {
	var b strings.Builder
	writeBorder(&b, w.widths)
	for i, cell := range cells {
		if i < len(w.widths) {
			if n := displayWidth(cell); n > w.widths[i] {
				w.widths[i] = n
			}
		}
	}
	b.WriteByte('\n')
	writeBorder(&b, w.widths)
	if !w.opts.noHeader {
		writeTableRow(&b, headerCells(w.columns), w.widths, nil)
		writeBorder(&b, w.widths)
	}
	_, err := io.WriteString(w.out, b.String())
	return err
}

func (w *tableWriter) Close() error {
	if len(w.columns) == 0 {
		return nil
	}
	if !w.started {
		if err := w.start(); err != nil {
			return err
		}
	}
	var b strings.Builder
	writeBorder(&b, w.widths)
	_, err := io.WriteString(w.out, b.String())
	return err
}

func headerCells(columns []hive.Column) []string {
	cells := make([]string, len(columns))
	for i, col := range columns {
		cells[i] = escapeText(col.Name)
	}
	return cells
}

func writeBorder(b *strings.Builder, widths []int) {
	b.WriteByte('+')
	for _, width := range widths {
		b.WriteString(strings.Repeat("-", width+2))
		b.WriteByte('+')
	}
	b.WriteByte('\n')
}

func writeTableRow(b *strings.Builder, cells []string, widths []int, numeric []bool) {
	b.WriteByte('|')
	for i, width := range widths {
		cell := ""
		if i < len(cells) {
			cell = cells[i]
		}
		right := numeric != nil && i < len(numeric) && numeric[i]
		b.WriteByte(' ')
		b.WriteString(pad(cell, width, right))
		b.WriteString(" |")
	}
	b.WriteByte('\n')
}

func pad(s string, width int, right bool) string {
	if displayWidth(s) > width {
		s = truncateDisplay(s, width)
	}
	n := displayWidth(s)
	if n >= width {
		return s
	}
	spaces := strings.Repeat(" ", width-n)
	if right {
		return spaces + s
	}
	return s + spaces
}

// widthCondition fixes ambiguous characters at width 1. The package-level
// runewidth helpers read the process locale, which would make table output
// differ between machines; column layout must be reproducible.
var widthCondition = runewidth.Condition{EastAsianWidth: false}

func displayWidth(s string) int {
	return widthCondition.StringWidth(s)
}

// truncateDisplay cuts s to at most width terminal cells, appending an
// ellipsis when anything was removed.
func truncateDisplay(s string, width int) string {
	if width <= 0 {
		return ""
	}
	if displayWidth(s) <= width {
		return s
	}
	var b strings.Builder
	used := 0
	for _, r := range s {
		w := widthCondition.RuneWidth(r)
		if used+w > width-1 {
			break
		}
		b.WriteRune(r)
		used += w
	}
	b.WriteString("…")
	return b.String()
}

type verticalWriter struct {
	out       io.Writer
	opts      *resolvedOptions
	nameWidth int
	row       int
	columns   []hive.Column
}

func (w *verticalWriter) WriteHeader(columns []hive.Column) error {
	w.columns = columns
	for _, col := range columns {
		if n := displayWidth(escapeText(col.Name)); n > w.nameWidth {
			w.nameWidth = n
		}
	}
	return nil
}

func (w *verticalWriter) WriteRow(row hive.Row) error {
	w.row++
	if _, err := fmt.Fprintf(w.out, "*************************** %d. row ***************************\n", w.row); err != nil {
		return err
	}
	for i, value := range row {
		name := strconv.Itoa(i + 1)
		col := hive.Column{}
		if i < len(w.columns) {
			name = escapeText(w.columns[i].Name)
			col = w.columns[i]
		}
		text := escapeText(formatCell(col, value, w.opts))
		if w.opts.maxColumnWidth > 0 {
			text = truncateDisplay(text, w.opts.maxColumnWidth)
		}
		padding := strings.Repeat(" ", max(0, w.nameWidth-displayWidth(name)))
		if _, err := fmt.Fprintf(w.out, "%s%s: %s\n", padding, name, text); err != nil {
			return err
		}
	}
	return nil
}

func (w *verticalWriter) Close() error { return nil }

type csvResultWriter struct {
	csv     *csv.Writer
	opts    *resolvedOptions
	columns []hive.Column
}

func (w *csvResultWriter) WriteHeader(columns []hive.Column) error {
	w.columns = columns
	if w.opts.noHeader {
		return nil
	}
	header := make([]string, len(columns))
	for i, col := range columns {
		header[i] = col.Name
	}
	return w.csv.Write(header)
}

func (w *csvResultWriter) WriteRow(row hive.Row) error {
	record := make([]string, len(row))
	for i, value := range row {
		col := hive.Column{}
		if i < len(w.columns) {
			col = w.columns[i]
		}
		record[i] = formatCell(col, value, w.opts)
	}
	return w.csv.Write(record)
}

func (w *csvResultWriter) Close() error {
	w.csv.Flush()
	return w.csv.Error()
}

type tsvWriter struct {
	out     *bufio.Writer
	opts    *resolvedOptions
	columns []hive.Column
}

func (w *tsvWriter) WriteHeader(columns []hive.Column) error {
	w.columns = columns
	if w.opts.noHeader {
		return nil
	}
	cells := make([]string, len(columns))
	for i, col := range columns {
		cells[i] = escapeText(col.Name)
	}
	_, err := w.out.WriteString(strings.Join(cells, "\t") + "\n")
	return err
}

func (w *tsvWriter) WriteRow(row hive.Row) error {
	cells := make([]string, len(row))
	for i, value := range row {
		col := hive.Column{}
		if i < len(w.columns) {
			col = w.columns[i]
		}
		cells[i] = escapeText(formatCell(col, value, w.opts))
	}
	_, err := w.out.WriteString(strings.Join(cells, "\t") + "\n")
	return err
}

func (w *tsvWriter) Close() error { return w.out.Flush() }

type jsonWriter struct {
	out          io.Writer
	binaryFormat string
	columns      []hive.Column
	started      bool
}

func (w *jsonWriter) WriteHeader(columns []hive.Column) error {
	w.columns = columns
	return nil
}

func (w *jsonWriter) WriteRow(row hive.Row) error {
	var b strings.Builder
	if !w.started {
		b.WriteString("[\n")
		w.started = true
	} else {
		b.WriteString(",\n")
	}
	b.WriteString("  ")
	b.WriteString(jsonObject(w.columns, row, w.binaryFormat))
	_, err := io.WriteString(w.out, b.String())
	return err
}

func (w *jsonWriter) Close() error {
	if !w.started {
		_, err := io.WriteString(w.out, "[]\n")
		return err
	}
	_, err := io.WriteString(w.out, "\n]\n")
	return err
}

type jsonlWriter struct {
	out          io.Writer
	binaryFormat string
	columns      []hive.Column
}

func (w *jsonlWriter) WriteHeader(columns []hive.Column) error {
	w.columns = columns
	return nil
}

func (w *jsonlWriter) WriteRow(row hive.Row) error {
	_, err := io.WriteString(w.out, jsonObject(w.columns, row, w.binaryFormat)+"\n")
	return err
}

func (w *jsonlWriter) Close() error { return nil }

func jsonObject(columns []hive.Column, row hive.Row, binaryFormat string) string {
	var b strings.Builder
	b.WriteByte('{')
	for j, col := range columns {
		if j > 0 {
			b.WriteString(", ")
		}
		key, _ := json.Marshal(col.Name)
		b.Write(key)
		b.WriteString(": ")
		if j < len(row) {
			b.WriteString(jsonValue(col, row[j], binaryFormat))
		} else {
			b.WriteString("null")
		}
	}
	b.WriteByte('}')
	return b.String()
}

func jsonValue(col hive.Column, value hive.Value, binaryFormat string) string {
	if value.Null {
		return "null"
	}
	if value.Kind == hive.KindBool {
		if value.Text == "true" || value.Text == "false" {
			return value.Text
		}
	}
	if value.Kind == hive.KindNumber && isJSONNumber(value.Text) {
		return value.Text
	}
	text := value.Text
	if isBinary(col, value) {
		text = formatBinary(text, binaryFormat)
	}
	quoted, _ := json.Marshal(text)
	return string(quoted)
}

func isJSONNumber(s string) bool {
	if s == "" {
		return false
	}
	switch s[0] {
	case '-', '0', '1', '2', '3', '4', '5', '6', '7', '8', '9':
	default:
		return false
	}
	if _, err := strconv.ParseFloat(s, 64); err != nil {
		return false
	}
	return json.Valid([]byte(s))
}

func isNumericType(typeID string) bool {
	switch typeID {
	case "TINYINT_TYPE", "SMALLINT_TYPE", "INT_TYPE", "BIGINT_TYPE",
		"FLOAT_TYPE", "DOUBLE_TYPE", "DECIMAL_TYPE":
		return true
	}
	return false
}

func formatCell(col hive.Column, value hive.Value, opts *resolvedOptions) string {
	if value.Null {
		return opts.nullString
	}
	if isBinary(col, value) {
		return formatBinary(value.Text, opts.binaryFormat)
	}
	return value.Text
}

func isBinary(col hive.Column, value hive.Value) bool {
	return value.Kind == hive.KindBinary || col.TypeID == "BINARY_TYPE"
}

func formatBinary(text, format string) string {
	switch format {
	case "hex":
		return hex.EncodeToString([]byte(text))
	case "text":
		return text
	default:
		return base64.StdEncoding.EncodeToString([]byte(text))
	}
}

func escapeText(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch r {
		case '\\':
			b.WriteString(`\\`)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		default:
			if r < 0x20 {
				fmt.Fprintf(&b, `\x%02x`, r)
			} else {
				b.WriteRune(r)
			}
		}
	}
	return b.String()
}
