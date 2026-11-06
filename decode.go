package hive

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"

	"github.com/apache/thrift/lib/go/thrift"
	"github.com/wgzhao/hive-go/internal/tcliservice"
)

// decodeRowSet converts one FetchResults batch into rows.
func decodeRowSet(ctx context.Context, rs *tcliservice.TRowSet, cols []Column) ([]Row, error) {
	if rs == nil {
		return nil, nil
	}
	if len(rs.BinaryColumns) > 0 {
		return decodeBinaryColumns(ctx, rs, len(cols))
	}
	if len(rs.Columns) > 0 {
		return decodeColumns(rs.Columns)
	}
	return decodeRows(rs.Rows, len(cols))
}

// decodeBinaryColumns decodes Hive 4's blob rowset. The payload is a sequence
// of TColumn structs serialized with TCompactProtocol; the number of columns
// is carried separately in columnCount.
func decodeBinaryColumns(ctx context.Context, rs *tcliservice.TRowSet, schemaCols int) ([]Row, error) {
	count := int(rs.GetColumnCount())
	if count <= 0 {
		count = schemaCols
	}
	if count <= 0 {
		return nil, fmt.Errorf("blob rowset has no column count")
	}
	mem := thrift.NewTMemoryBufferLen(len(rs.BinaryColumns))
	if _, err := mem.Write(rs.BinaryColumns); err != nil {
		return nil, fmt.Errorf("blob rowset: %w", err)
	}
	proto := thrift.NewTCompactProtocol(mem)
	columns := make([]*tcliservice.TColumn, 0, count)
	for i := 0; i < count; i++ {
		col := tcliservice.NewTColumn()
		if err := col.Read(ctx, proto); err != nil {
			return nil, fmt.Errorf("blob rowset: read column %d: %w", i, err)
		}
		columns = append(columns, col)
	}
	return decodeColumns(columns)
}

func decodeColumns(columns []*tcliservice.TColumn) ([]Row, error) {
	rowCount := 0
	for _, col := range columns {
		if n := columnValueCount(col); n > rowCount {
			rowCount = n
		}
	}
	if rowCount == 0 {
		return nil, nil
	}
	rows := make([]Row, rowCount)
	for i := 0; i < rowCount; i++ {
		row := make(Row, len(columns))
		for j, col := range columns {
			value, err := columnValue(col, i)
			if err != nil {
				return nil, fmt.Errorf("column %d row %d: %w", j, i, err)
			}
			row[j] = value
		}
		rows[i] = row
	}
	return rows, nil
}

func columnValueCount(col *tcliservice.TColumn) int {
	if col == nil {
		return 0
	}
	switch {
	case col.BoolVal != nil:
		return len(col.BoolVal.Values)
	case col.ByteVal != nil:
		return len(col.ByteVal.Values)
	case col.I16Val != nil:
		return len(col.I16Val.Values)
	case col.I32Val != nil:
		return len(col.I32Val.Values)
	case col.I64Val != nil:
		return len(col.I64Val.Values)
	case col.DoubleVal != nil:
		return len(col.DoubleVal.Values)
	case col.StringVal != nil:
		return len(col.StringVal.Values)
	case col.BinaryVal != nil:
		return len(col.BinaryVal.Values)
	}
	return 0
}

func columnValue(col *tcliservice.TColumn, i int) (Value, error) {
	switch {
	case col.BoolVal != nil:
		if i >= len(col.BoolVal.Values) || isNull(col.BoolVal.Nulls, i) {
			return Value{Null: true}, nil
		}
		return Value{Text: strconv.FormatBool(col.BoolVal.Values[i]), Kind: KindBool}, nil
	case col.ByteVal != nil:
		if i >= len(col.ByteVal.Values) || isNull(col.ByteVal.Nulls, i) {
			return Value{Null: true}, nil
		}
		return Value{Text: strconv.FormatInt(int64(col.ByteVal.Values[i]), 10), Kind: KindNumber}, nil
	case col.I16Val != nil:
		if i >= len(col.I16Val.Values) || isNull(col.I16Val.Nulls, i) {
			return Value{Null: true}, nil
		}
		return Value{Text: strconv.FormatInt(int64(col.I16Val.Values[i]), 10), Kind: KindNumber}, nil
	case col.I32Val != nil:
		if i >= len(col.I32Val.Values) || isNull(col.I32Val.Nulls, i) {
			return Value{Null: true}, nil
		}
		return Value{Text: strconv.FormatInt(int64(col.I32Val.Values[i]), 10), Kind: KindNumber}, nil
	case col.I64Val != nil:
		if i >= len(col.I64Val.Values) || isNull(col.I64Val.Nulls, i) {
			return Value{Null: true}, nil
		}
		return Value{Text: strconv.FormatInt(col.I64Val.Values[i], 10), Kind: KindNumber}, nil
	case col.DoubleVal != nil:
		if i >= len(col.DoubleVal.Values) || isNull(col.DoubleVal.Nulls, i) {
			return Value{Null: true}, nil
		}
		return Value{Text: formatDouble(col.DoubleVal.Values[i]), Kind: KindNumber}, nil
	case col.StringVal != nil:
		if i >= len(col.StringVal.Values) || isNull(col.StringVal.Nulls, i) {
			return Value{Null: true}, nil
		}
		return Value{Text: col.StringVal.Values[i], Kind: KindString}, nil
	case col.BinaryVal != nil:
		if i >= len(col.BinaryVal.Values) || isNull(col.BinaryVal.Nulls, i) {
			return Value{Null: true}, nil
		}
		return Value{Text: string(col.BinaryVal.Values[i]), Kind: KindBinary}, nil
	}
	return Value{}, fmt.Errorf("column has no value union")
}

func decodeRows(raw []*tcliservice.TRow, schemaCols int) ([]Row, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	rows := make([]Row, 0, len(raw))
	for _, rawRow := range raw {
		if rawRow == nil {
			rows = append(rows, nil)
			continue
		}
		n := len(rawRow.ColVals)
		if schemaCols > n {
			n = schemaCols
		}
		row := make(Row, n)
		for i := 0; i < n; i++ {
			if i >= len(rawRow.ColVals) || rawRow.ColVals[i] == nil {
				row[i] = Value{Null: true}
				continue
			}
			value, err := columnValueFromRow(rawRow.ColVals[i])
			if err != nil {
				return nil, fmt.Errorf("row column %d: %w", i, err)
			}
			row[i] = value
		}
		rows = append(rows, row)
	}
	return rows, nil
}

func columnValueFromRow(col *tcliservice.TColumnValue) (Value, error) {
	switch {
	case col.BoolVal != nil:
		if col.BoolVal.Value == nil {
			return Value{Null: true}, nil
		}
		return Value{Text: strconv.FormatBool(col.BoolVal.GetValue()), Kind: KindBool}, nil
	case col.ByteVal != nil:
		if col.ByteVal.Value == nil {
			return Value{Null: true}, nil
		}
		return Value{Text: strconv.FormatInt(int64(col.ByteVal.GetValue()), 10), Kind: KindNumber}, nil
	case col.I16Val != nil:
		if col.I16Val.Value == nil {
			return Value{Null: true}, nil
		}
		return Value{Text: strconv.FormatInt(int64(col.I16Val.GetValue()), 10), Kind: KindNumber}, nil
	case col.I32Val != nil:
		if col.I32Val.Value == nil {
			return Value{Null: true}, nil
		}
		return Value{Text: strconv.FormatInt(int64(col.I32Val.GetValue()), 10), Kind: KindNumber}, nil
	case col.I64Val != nil:
		if col.I64Val.Value == nil {
			return Value{Null: true}, nil
		}
		return Value{Text: strconv.FormatInt(col.I64Val.GetValue(), 10), Kind: KindNumber}, nil
	case col.DoubleVal != nil:
		if col.DoubleVal.Value == nil {
			return Value{Null: true}, nil
		}
		return Value{Text: formatDouble(col.DoubleVal.GetValue()), Kind: KindNumber}, nil
	case col.StringVal != nil:
		if col.StringVal.Value == nil {
			return Value{Null: true}, nil
		}
		return Value{Text: col.StringVal.GetValue(), Kind: KindString}, nil
	}
	return Value{}, fmt.Errorf("row value has no union field")
}

func isNull(nulls []byte, i int) bool {
	if i < 0 {
		return true
	}
	idx := i >> 3
	if idx >= len(nulls) {
		return false
	}
	return nulls[idx]&(1<<uint(i&7)) != 0
}

func formatDouble(v float64) string {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return strconv.FormatFloat(v, 'g', -1, 64)
	}
	s := strconv.FormatFloat(v, 'g', -1, 64)
	if !strings.ContainsAny(s, ".eE") {
		s += ".0"
	}
	return s
}

func columnFromDesc(desc *tcliservice.TColumnDesc) Column {
	col := Column{Name: desc.ColumnName, Comment: desc.GetComment()}
	col.Type, col.TypeID = describeType(desc.TypeDesc)
	return col
}

func describeType(desc *tcliservice.TTypeDesc) (string, string) {
	if desc == nil || len(desc.Types) == 0 {
		return "UNKNOWN", ""
	}
	return describeTypeEntry(desc.Types, 0, 0)
}

func describeTypeEntry(types []*tcliservice.TTypeEntry, idx int32, depth int) (string, string) {
	if depth > 8 || idx < 0 || int(idx) >= len(types) || types[idx] == nil {
		return "UNKNOWN", ""
	}
	entry := types[idx]
	if entry.PrimitiveEntry != nil {
		name := primitiveTypeName(entry.PrimitiveEntry.Type)
		return withQualifiers(name, entry.PrimitiveEntry.TypeQualifiers), entry.PrimitiveEntry.Type.String()
	}
	if entry.ArrayEntry != nil {
		inner, _ := describeTypeEntry(types, int32(entry.ArrayEntry.ObjectTypePtr), depth+1)
		return "ARRAY<" + inner + ">", ""
	}
	if entry.MapEntry != nil {
		key, _ := describeTypeEntry(types, int32(entry.MapEntry.KeyTypePtr), depth+1)
		value, _ := describeTypeEntry(types, int32(entry.MapEntry.ValueTypePtr), depth+1)
		return "MAP<" + key + "," + value + ">", ""
	}
	if entry.StructEntry != nil {
		return "STRUCT<" + describeFields(types, entry.StructEntry.NameToTypePtr, depth) + ">", ""
	}
	if entry.UnionEntry != nil {
		return "UNION<" + describeFields(types, entry.UnionEntry.NameToTypePtr, depth) + ">", ""
	}
	if entry.UserDefinedTypeEntry != nil {
		return entry.UserDefinedTypeEntry.GetTypeClassName(), "USER_DEFINED_TYPE"
	}
	return "UNKNOWN", ""
}

func describeFields(types []*tcliservice.TTypeEntry, fields map[string]tcliservice.TTypeEntryPtr, depth int) string {
	names := make([]string, 0, len(fields))
	for name := range fields {
		names = append(names, name)
	}
	sort.Strings(names)
	parts := make([]string, 0, len(names))
	for _, name := range names {
		typ, _ := describeTypeEntry(types, int32(fields[name]), depth+1)
		parts = append(parts, name+":"+typ)
	}
	return strings.Join(parts, ",")
}

func withQualifiers(name string, q *tcliservice.TTypeQualifiers) string {
	if q == nil {
		return name
	}
	getInt := func(key string) (int32, bool) {
		v, ok := q.Qualifiers[key]
		if !ok || v == nil || v.I32Value == nil {
			return 0, false
		}
		return v.GetI32Value(), true
	}
	switch name {
	case "DECIMAL":
		precision, okP := getInt("precision")
		scale, okS := getInt("scale")
		if okP && okS {
			return fmt.Sprintf("DECIMAL(%d,%d)", precision, scale)
		}
	case "CHAR", "VARCHAR":
		if length, ok := getInt("characterMaximumLength"); ok {
			return fmt.Sprintf("%s(%d)", name, length)
		}
	}
	return name
}

func primitiveTypeName(id tcliservice.TTypeId) string {
	switch id {
	case tcliservice.TTypeId_BOOLEAN_TYPE:
		return "BOOLEAN"
	case tcliservice.TTypeId_TINYINT_TYPE:
		return "TINYINT"
	case tcliservice.TTypeId_SMALLINT_TYPE:
		return "SMALLINT"
	case tcliservice.TTypeId_INT_TYPE:
		return "INT"
	case tcliservice.TTypeId_BIGINT_TYPE:
		return "BIGINT"
	case tcliservice.TTypeId_FLOAT_TYPE:
		return "FLOAT"
	case tcliservice.TTypeId_DOUBLE_TYPE:
		return "DOUBLE"
	case tcliservice.TTypeId_STRING_TYPE:
		return "STRING"
	case tcliservice.TTypeId_TIMESTAMP_TYPE:
		return "TIMESTAMP"
	case tcliservice.TTypeId_BINARY_TYPE:
		return "BINARY"
	case tcliservice.TTypeId_ARRAY_TYPE:
		return "ARRAY"
	case tcliservice.TTypeId_MAP_TYPE:
		return "MAP"
	case tcliservice.TTypeId_STRUCT_TYPE:
		return "STRUCT"
	case tcliservice.TTypeId_UNION_TYPE:
		return "UNION"
	case tcliservice.TTypeId_USER_DEFINED_TYPE:
		return "USER_DEFINED"
	case tcliservice.TTypeId_DECIMAL_TYPE:
		return "DECIMAL"
	case tcliservice.TTypeId_NULL_TYPE:
		return "NULL"
	case tcliservice.TTypeId_DATE_TYPE:
		return "DATE"
	case tcliservice.TTypeId_VARCHAR_TYPE:
		return "VARCHAR"
	case tcliservice.TTypeId_CHAR_TYPE:
		return "CHAR"
	case tcliservice.TTypeId_INTERVAL_YEAR_MONTH_TYPE:
		return "INTERVAL_YEAR_MONTH"
	case tcliservice.TTypeId_INTERVAL_DAY_TIME_TYPE:
		return "INTERVAL_DAY_TIME"
	case tcliservice.TTypeId_TIMESTAMPLOCALTZ_TYPE:
		return "TIMESTAMPLOCALTZ"
	}
	return id.String()
}
