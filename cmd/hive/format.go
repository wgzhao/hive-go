package main

import (
	"bufio"
	"encoding/base64"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
	"unicode/utf8"

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
		return &verticalWriter{out: out, nullString: opts.nullString, nameWidth: 1}
	case "csv":
		return &csvResultWriter{csv: csv.NewWriter(out), nullString: opts.nullString, noHeader: opts.noHeader}
	case "tsv":
		return &tsvWriter{out: bufio.NewWriter(out), nullString: opts.nullString, noHeader: opts.noHeader}
	case "json":
		return &jsonWriter{out: out}
	default:
		return &tableWriter{out: out, nullString: opts.nullString, noHeader: opts.noHeader}
	}
}

type tableWriter struct {
	out        io.Writer
	nullString string
	noHeader   bool
	columns    []hive.Column
	rows       [][]string
}

func (w *tableWriter) WriteHeader(columns []hive.Column) error {
	w.columns = columns
	return nil
}

func (w *tableWriter) WriteRow(row hive.Row) error {
	cells := make([]string, len(row))
	for i, value := range row {
		cells[i] = escapeText(cellText(value, w.nullString))
	}
	w.rows = append(w.rows, cells)
	return nil
}

func (w *tableWriter) Close() error {
	if len(w.columns) == 0 {
		return nil
	}
	widths := make([]int, len(w.columns))
	for i, col := range w.columns {
		widths[i] = utf8.RuneCountInString(escapeText(col.Name))
	}
	for _, row := range w.rows {
		for i, cell := range row {
			if i < len(widths) {
				if n := utf8.RuneCountInString(cell); n > widths[i] {
					widths[i] = n
				}
			}
		}
	}

	var b strings.Builder
	writeBorder(&b, widths)
	if !w.noHeader {
		writeTableRow(&b, headerCells(w.columns), widths, nil)
		writeBorder(&b, widths)
	}
	numeric := make([]bool, len(w.columns))
	for i, col := range w.columns {
		numeric[i] = isNumericType(col.TypeID)
	}
	for _, row := range w.rows {
		writeTableRow(&b, row, widths, numeric)
	}
	writeBorder(&b, widths)
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
	n := utf8.RuneCountInString(s)
	if n >= width {
		return s
	}
	spaces := strings.Repeat(" ", width-n)
	if right {
		return spaces + s
	}
	return s + spaces
}

type verticalWriter struct {
	out        io.Writer
	nullString string
	nameWidth  int
	row        int
	columns    []hive.Column
}

func (w *verticalWriter) WriteHeader(columns []hive.Column) error {
	w.columns = columns
	for _, col := range columns {
		if n := utf8.RuneCountInString(escapeText(col.Name)); n > w.nameWidth {
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
		if i < len(w.columns) {
			name = escapeText(w.columns[i].Name)
		}
		if _, err := fmt.Fprintf(w.out, "%*s: %s\n", w.nameWidth, name, escapeText(cellText(value, w.nullString))); err != nil {
			return err
		}
	}
	return nil
}

func (w *verticalWriter) Close() error { return nil }

type csvResultWriter struct {
	csv        *csv.Writer
	nullString string
	noHeader   bool
}

func (w *csvResultWriter) WriteHeader(columns []hive.Column) error {
	if w.noHeader {
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
		if value.Null {
			record[i] = w.nullString
		} else {
			record[i] = value.Text
		}
	}
	return w.csv.Write(record)
}

func (w *csvResultWriter) Close() error {
	w.csv.Flush()
	return w.csv.Error()
}

type tsvWriter struct {
	out        *bufio.Writer
	nullString string
	noHeader   bool
}

func (w *tsvWriter) WriteHeader(columns []hive.Column) error {
	if w.noHeader {
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
		cells[i] = escapeText(cellText(value, w.nullString))
	}
	_, err := w.out.WriteString(strings.Join(cells, "\t") + "\n")
	return err
}

func (w *tsvWriter) Close() error { return w.out.Flush() }

type jsonWriter struct {
	out     io.Writer
	columns []hive.Column
	rows    []hive.Row
}

func (w *jsonWriter) WriteHeader(columns []hive.Column) error {
	w.columns = columns
	return nil
}

func (w *jsonWriter) WriteRow(row hive.Row) error {
	w.rows = append(w.rows, row)
	return nil
}

func (w *jsonWriter) Close() error {
	if len(w.columns) == 0 {
		_, err := io.WriteString(w.out, "[]\n")
		return err
	}
	var b strings.Builder
	b.WriteString("[\n")
	for i, row := range w.rows {
		b.WriteString("  {")
		for j, col := range w.columns {
			if j > 0 {
				b.WriteString(", ")
			}
			key, _ := json.Marshal(col.Name)
			b.Write(key)
			b.WriteString(": ")
			if j < len(row) {
				b.WriteString(jsonValue(col, row[j]))
			} else {
				b.WriteString("null")
			}
		}
		b.WriteString("}")
		if i < len(w.rows)-1 {
			b.WriteString(",")
		}
		b.WriteString("\n")
	}
	b.WriteString("]\n")
	_, err := io.WriteString(w.out, b.String())
	return err
}

func jsonValue(col hive.Column, value hive.Value) string {
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
	if value.Kind == hive.KindBinary || col.TypeID == "BINARY_TYPE" {
		encoded := base64.StdEncoding.EncodeToString([]byte(value.Text))
		quoted, _ := json.Marshal(encoded)
		return string(quoted)
	}
	quoted, _ := json.Marshal(value.Text)
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

func cellText(value hive.Value, nullString string) string {
	if value.Null {
		return nullString
	}
	return value.Text
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
