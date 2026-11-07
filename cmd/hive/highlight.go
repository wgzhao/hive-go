package main

import (
	"strings"
	"unicode"
)

// ANSI SGR styles for the REPL painter. Only basic 16-color codes are used so
// the result stays readable on both light and dark terminals.
const (
	ansiReset = "\x1b[0m"

	styleKeyword  = "\x1b[1;34m" // bold blue
	styleConstant = "\x1b[1;35m" // bold magenta
	styleString   = "\x1b[32m"   // green
	styleNumber   = "\x1b[35m"   // magenta
	styleComment  = "\x1b[90m"   // bright black
	styleVariable = "\x1b[33m"   // yellow
	styleQuotedID = "\x1b[36m"   // cyan
	styleMeta     = "\x1b[36m"   // cyan
)

type tokenKind uint8

const (
	tokenPlain tokenKind = iota
	tokenKeyword
	tokenConstant
	tokenString
	tokenNumber
	tokenComment
	tokenVariable
	tokenQuotedIdentifier
)

func styleFor(kind tokenKind) string {
	switch kind {
	case tokenKeyword:
		return styleKeyword
	case tokenConstant:
		return styleConstant
	case tokenString:
		return styleString
	case tokenNumber:
		return styleNumber
	case tokenComment:
		return styleComment
	case tokenVariable:
		return styleVariable
	case tokenQuotedIdentifier:
		return styleQuotedID
	default:
		return ""
	}
}

// sqlPainter colors the line being edited in the REPL. pending reports the text
// of earlier lines in the current statement, so string and block-comment state
// carries across continuation prompts.
type sqlPainter struct {
	pending func() string
}

func (p *sqlPainter) Paint(line []rune, _ int) []rune {
	if len(line) == 0 {
		return line
	}

	prefix := ""
	if p.pending != nil {
		prefix = p.pending()
	}

	// A line beginning with "!" is a Hive CLI meta command, but only when no
	// statement is being continued (matching the REPL's dispatch rule).
	if strings.TrimSpace(prefix) == "" && isMetaLine(line) {
		return wrapStyle(line, styleMeta)
	}

	prefixRunes := []rune(prefix)
	combined := make([]rune, 0, len(prefixRunes)+len(line))
	combined = append(combined, prefixRunes...)
	combined = append(combined, line...)

	kinds := classifySQL(combined)
	return renderStyles(combined, kinds, len(prefixRunes))
}

func wrapStyle(line []rune, style string) []rune {
	out := make([]rune, 0, len(line)+len(style)+len(ansiReset))
	out = append(out, []rune(style)...)
	out = append(out, line...)
	out = append(out, []rune(ansiReset)...)
	return out
}

func isMetaLine(line []rune) bool {
	for _, r := range line {
		switch r {
		case ' ', '\t':
			continue
		default:
			return r == '!'
		}
	}
	return false
}

// renderStyles inserts SGR escapes for the part of src at or after start,
// which is the current input line. The prefix is tokenized only to carry
// lexical state, not to be displayed.
func renderStyles(src []rune, kinds []tokenKind, start int) []rune {
	if start >= len(src) {
		return nil
	}

	out := make([]rune, 0, len(src)-start+64)
	current := tokenPlain
	for i := start; i < len(src); i++ {
		kind := kinds[i]
		if kind != current {
			if current != tokenPlain {
				out = append(out, []rune(ansiReset)...)
			}
			if style := styleFor(kind); style != "" {
				out = append(out, []rune(style)...)
			}
			current = kind
		}
		out = append(out, src[i])
	}
	if current != tokenPlain {
		out = append(out, []rune(ansiReset)...)
	}
	return out
}

func classifySQL(src []rune) []tokenKind {
	kinds := make([]tokenKind, len(src))

	mark := func(start, end int, kind tokenKind) {
		for i := start; i < end && i < len(kinds); i++ {
			kinds[i] = kind
		}
	}

	for i := 0; i < len(src); {
		c := src[i]
		switch {
		case c == '\'' || c == '"':
			end := scanQuoted(src, i, c)
			mark(i, end, tokenString)
			i = end

		case c == '`':
			end := scanQuotedIdentifier(src, i)
			mark(i, end, tokenQuotedIdentifier)
			i = end

		case c == '-' && i+1 < len(src) && src[i+1] == '-':
			end := i
			for end < len(src) && src[end] != '\n' && src[end] != '\r' {
				end++
			}
			mark(i, end, tokenComment)
			i = end

		case c == '/' && i+1 < len(src) && src[i+1] == '*':
			end := i + 2
			for end+1 < len(src) && !(src[end] == '*' && src[end+1] == '/') {
				end++
			}
			if end+1 < len(src) {
				end += 2
			} else {
				end = len(src)
			}
			mark(i, end, tokenComment)
			i = end

		case c == '$' && i+1 < len(src) && src[i+1] == '{':
			end := i + 2
			for end < len(src) && src[end] != '}' && src[end] != '\n' && src[end] != '\r' {
				end++
			}
			if end < len(src) && src[end] == '}' {
				end++
			}
			mark(i, end, tokenVariable)
			i = end

		case isDigit(c) || (c == '.' && i+1 < len(src) && isDigit(src[i+1])):
			end := scanNumber(src, i)
			mark(i, end, tokenNumber)
			i = end

		case isIdentifierStart(c):
			end := scanIdentifier(src, i)

			// Hive 4 has hyphenated keywords such as FAST-FORWARD.
			if end < len(src) && src[end] == '-' && end+1 < len(src) && isIdentifierStart(src[end+1]) {
				compoundEnd := scanIdentifier(src, end+1)
				if isKeyword(strings.ToUpper(string(src[i:compoundEnd]))) {
					mark(i, compoundEnd, tokenKeyword)
					i = compoundEnd
					continue
				}
			}

			word := strings.ToUpper(string(src[i:end]))
			switch {
			case isConstant(word):
				mark(i, end, tokenConstant)
			case isKeyword(word):
				mark(i, end, tokenKeyword)
			}
			i = end

		default:
			i++
		}
	}

	return kinds
}

func scanQuoted(src []rune, i int, quote rune) int {
	for j := i + 1; j < len(src); j++ {
		switch src[j] {
		case '\\':
			j++
		case quote:
			return j + 1
		}
	}
	return len(src)
}

func scanQuotedIdentifier(src []rune, i int) int {
	for j := i + 1; j < len(src); j++ {
		if src[j] != '`' {
			continue
		}
		if j+1 < len(src) && src[j+1] == '`' {
			j++
			continue
		}
		return j + 1
	}
	return len(src)
}

func scanNumber(src []rune, i int) int {
	j := i
	if src[j] == '.' {
		j++
		for j < len(src) && isDigit(src[j]) {
			j++
		}
		return scanNumberSuffix(src, j)
	}

	if src[j] == '0' && j+1 < len(src) && (src[j+1] == 'x' || src[j+1] == 'X') {
		j += 2
		for j < len(src) && isHexDigit(src[j]) {
			j++
		}
		return j
	}

	for j < len(src) && isDigit(src[j]) {
		j++
	}
	if j < len(src) && src[j] == '.' {
		j++
		for j < len(src) && isDigit(src[j]) {
			j++
		}
	}
	if j < len(src) && (src[j] == 'e' || src[j] == 'E') {
		k := j + 1
		if k < len(src) && (src[k] == '+' || src[k] == '-') {
			k++
		}
		if k < len(src) && isDigit(src[k]) {
			for k < len(src) && isDigit(src[k]) {
				k++
			}
			j = k
		}
	}
	return scanNumberSuffix(src, j)
}

// scanNumberSuffix consumes Hive numeric literal suffixes (L, S, Y, D, BD) and
// byte-length units (B, K, M, G).
func scanNumberSuffix(src []rune, j int) int {
	if j >= len(src) {
		return j
	}
	switch unicode.ToUpper(src[j]) {
	case 'L', 'S', 'Y', 'D', 'K', 'M', 'G':
		return j + 1
	case 'B':
		if j+1 < len(src) && unicode.ToUpper(src[j+1]) == 'D' {
			return j + 2
		}
		return j + 1
	}
	return j
}

func scanIdentifier(src []rune, i int) int {
	j := i + 1
	for j < len(src) && isIdentifierPart(src[j]) {
		j++
	}
	return j
}

func isDigit(r rune) bool {
	return '0' <= r && r <= '9'
}

func isHexDigit(r rune) bool {
	return isDigit(r) || ('a' <= r && r <= 'f') || ('A' <= r && r <= 'F')
}

func isIdentifierStart(r rune) bool {
	return unicode.IsLetter(r) || r == '_'
}

func isIdentifierPart(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_'
}

func isKeyword(word string) bool {
	_, ok := hiveKeywords[word]
	return ok
}

func isConstant(word string) bool {
	_, ok := hiveConstants[word]
	return ok
}

var hiveConstants = map[string]struct{}{
	"TRUE":  {},
	"FALSE": {},
	"NULL":  {},
}

// hiveKeywords covers the keyword tokens from the Hive 3.1.3 and 4.0.0
// HiveLexer grammars (reserved and non-reserved), so highlighting stays in
// sync with the Hive server rather than a generic SQL dialect.
var hiveKeywords = func() map[string]struct{} {
	const words = `
ABORT ACTIVATE ACTIVE ADD ADMIN AFTER ALL ALLOC_FRACTION ALTER ANALYZE AND ANY
APPLICATION ARCHIVE ARRAY AS ASC AST AT AUTHORIZATION AUTOCOMMIT BEFORE BETWEEN BIGINT
BINARY BOOLEAN BOTH BRANCH BUCKET BUCKETS BY CACHE CASCADE CASE CAST CBO
CHANGE CHAR CHECK CHERRY-PICK CLUSTER CLUSTERED CLUSTERSTATUS COLLECTION COLUMN COLUMNS COMMENT COMMIT
COMPACT COMPACTIONID COMPACTIONS COMPUTE CONCATENATE CONF CONNECTOR CONNECTORS CONSTRAINT CONTINUE CONVERT COST
CREATE CRON CROSS CUBE CURRENT CURRENT_DATE CURRENT_TIMESTAMP CURSOR DATA DATABASE DATABASES DATE
DATETIME DAY DAYOFWEEK DAYS DBPROPERTIES DCPROPERTIES DDL DEBUG DEC DECIMAL DEFAULT DEFERRED
DEFINED DELETE DELIMITED DEPENDENCY DESC DESCRIBE DETAIL DIRECTORIES DIRECTORY DISABLE DISABLED DISTINCT
DISTRIBUTE DISTRIBUTED DO DOUBLE DROP DUMP ELSE ENABLE ENABLED END ENFORCED ESCAPED
EVERY EXCEPT EXCHANGE EXCLUSIVE EXECUTE EXECUTED EXISTS EXPIRE_SNAPSHOTS EXPLAIN EXPORT EXPRESSION EXTENDED
EXTERNAL EXTRACT FALSE FAST-FORWARD FETCH FIELDS FILE FILEFORMAT FIRST FLOAT FLOOR FOLLOWING
FOR FORCE FOREIGN FORMAT FORMATTED FROM FULL FUNCTION FUNCTIONS GRANT GROUP GROUPING
HAVING HOUR HOURS IDXPROPERTIES IF IGNORE IMPORT IN INDEX INDEXES INNER INPATH
INPUTDRIVER INPUTFORMAT INSERT INT INTEGER INTERSECT INTERVAL INTO IS ISOLATION ITEMS JAR
JOIN JOINCOST KEY KEYS KILL LAST LATERAL LEADING LEFT LESS LEVEL LIKE
LIMIT LINES LOAD LOCAL LOCATION LOCK LOCKS LOGICAL LONG MACRO MANAGED MANAGEDLOCATION
MANAGEMENT MAP MAPJOIN MAPPING MATCHED MATERIALIZED MERGE METADATA MINUS MINUTE MINUTES MONTH
MONTHS MORE MOVE MSCK NONE NORELY NOSCAN NOT NOVALIDATE NULL NULLS NUMERIC
OF OFFSET OLDER ON ONLY OPERATOR OPTIMIZE OPTION OR ORDER ORPHAN-FILES OUT
OUTER OUTPUTDRIVER OUTPUTFORMAT OVER OVERWRITE OWNER PARTITION PARTITIONED PARTITIONS PATH PERCENT PKFK_JOIN
PLAN PLANS PLUS POOL PRECEDING PRECISION PREPARE PRESERVE PRIMARY PRINCIPALS PROCEDURE PURGE
QUALIFY QUARTER QUERY QUERY_PARALLELISM RANGE READ READS REAL REBUILD RECORDREADER RECORDWRITER REDUCE
REFERENCES REGEXP RELOAD RELY REMOTE RENAME REOPTIMIZATION REPAIR REPL REPLACE REPLICATION RESOURCE
RESPECT RESTRICT RETAIN RETENTION REVOKE REWRITE RIGHT RLIKE ROLE ROLES ROLLBACK ROLLUP
ROW ROWS SCHEDULED SCHEDULING_POLICY SCHEMA SCHEMAS SECOND SECONDS SELECT SEMI SERDE SERDEPROPERTIES
SERVER SET SET_CURRENT_SNAPSHOT SETS SHARED SHOW SHOW_DATABASE SKEWED SMALLINT SNAPSHOT SNAPSHOTS SOME
SORT SORTED SPEC SSL START STATISTICS STATUS STORED STREAMTABLE STRING STRUCT SUMMARY
SYNC SYSTEM_TIME SYSTEM_VERSION TABLE TABLES TABLESAMPLE TAG TBLPROPERTIES TEMPORARY TERMINATED THAN THEN
TIME TIMESTAMP TIMESTAMPLOCALTZ TINYINT TO TOUCH TRAILING TRANSACTION TRANSACTIONAL TRANSACTIONS TRANSFORM TRIGGER
TRIM TRUE TRUNCATE TYPE UNARCHIVE UNBOUNDED UNDO UNION UNIONTYPE UNIQUE UNIQUEJOIN UNKNOWN
UNLOCK UNMANAGED UNSET UNSIGNED UPDATE URI URL USE USER USING UTC UTC_TMESTAMP
VALIDATE VALUES VARCHAR VECTORIZATION VIEW VIEWS WAIT WEEK WEEKS WHEN WHERE WHILE
WINDOW WITH WITHIN WORK WORKLOAD WRITE YEAR YEARS ZONE
`
	fields := strings.Fields(words)
	set := make(map[string]struct{}, len(fields))
	for _, word := range fields {
		set[word] = struct{}{}
	}
	return set
}()
