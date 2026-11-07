package main

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/wgzhao/hive-go"
	"github.com/wgzhao/hive-go/internal/hiveconf"
	"golang.org/x/term"
)

type resolvedOptions struct {
	cfg hive.Config

	execute  string
	file     string
	initFile string

	format         string
	nullString     string
	noHeader       bool
	verbose        bool
	timing         bool
	silent         bool
	force          bool
	delimiter      string
	stream         bool
	maxColumnWidth int
	binaryFormat   string

	historyFile string
	noHistory   bool

	vars *variables
}

type urlOptions struct {
	host     string
	port     int
	database string
	username string
	password string
	auth     string
	hiveconf []string
	hivevar  []string
}

func resolveOptions(o *cliOptions) (*resolvedOptions, error) {
	if o.passwordFile != "" {
		if o.password != "" {
			return nil, fmt.Errorf("--password and --password-file are mutually exclusive")
		}
		data, err := os.ReadFile(o.passwordFile)
		if err != nil {
			return nil, fmt.Errorf("read password file: %w", err)
		}
		o.password = strings.TrimRight(string(data), "\r\n")
		if o.password == "" {
			return nil, fmt.Errorf("password file %s is empty", o.passwordFile)
		}
		if info, err := os.Stat(o.passwordFile); err == nil && info.Mode().Perm()&0o077 != 0 {
			fmt.Fprintf(os.Stderr, "Warning: password file %s is accessible by other users (mode %04o)\n",
				o.passwordFile, info.Mode().Perm())
		}
	}

	if o.initFile == "-" {
		return nil, fmt.Errorf("--init-file cannot read from stdin; pass a file path")
	}

	delimiter := o.delimiter
	if delimiter == "" {
		delimiter = ";"
	}
	if err := validateDelimiter(delimiter); err != nil {
		return nil, err
	}
	if o.maxColumnWidth < 0 {
		return nil, fmt.Errorf("--max-column-width must be zero or positive")
	}

	binaryFormat := strings.ToLower(strings.TrimSpace(o.binaryFormat))
	if binaryFormat == "" {
		binaryFormat = "base64"
	}
	switch binaryFormat {
	case "text", "base64", "hex":
	default:
		return nil, fmt.Errorf("unsupported binary format %q (use text, base64 or hex)", o.binaryFormat)
	}

	nullString := o.nullString
	if o.nullEmpty {
		if o.nullStringSeen {
			return nil, fmt.Errorf("--null-string and --null-empty are mutually exclusive")
		}
		nullString = ""
	}

	res := &resolvedOptions{
		execute:        o.execute,
		file:           o.file,
		initFile:       o.initFile,
		format:         strings.ToLower(strings.TrimSpace(o.format)),
		nullString:     nullString,
		noHeader:       o.noHeader,
		verbose:        o.verbose,
		timing:         o.timing,
		silent:         o.silent,
		force:          o.force,
		delimiter:      delimiter,
		stream:         o.stream,
		maxColumnWidth: o.maxColumnWidth,
		binaryFormat:   binaryFormat,
		noHistory:      o.noHistory,
		historyFile:    o.historyFile,
		vars:           newVariables(),
	}
	switch res.format {
	case "table", "vertical", "csv", "tsv", "json", "jsonl":
	default:
		return nil, fmt.Errorf("unsupported output format %q (use table, vertical, csv, tsv, json or jsonl)", o.format)
	}

	cfg := hive.Config{
		Host:           "localhost",
		Port:           10000,
		Auth:           hive.AuthNone,
		FetchSize:      1000,
		ConnectTimeout: o.connectTimeout,
		SocketTimeout:  o.socketTimeout,
		QueryTimeout:   o.queryTimeout,
		Protocol:       "auto",
	}

	// hive-site.xml
	site := map[string]string{}
	if dir := hiveconf.DefaultDir(); dir != "" {
		loaded, err := hiveconf.Load(dir)
		if err != nil {
			return nil, err
		}
		site = loaded
	}
	if v := site["hive.server2.transport.mode"]; strings.EqualFold(v, "http") {
		return nil, fmt.Errorf("hive-site.xml uses HTTP transport, which this version does not support")
	}
	if v := site["hive.server2.use.SSL"]; strings.EqualFold(v, "true") {
		return nil, fmt.Errorf("hive-site.xml enables SSL, which this version does not support")
	}
	if v := site["hive.server2.thrift.port"]; v != "" {
		port, err := strconv.Atoi(v)
		if err != nil {
			return nil, fmt.Errorf("invalid hive.server2.thrift.port %q: %w", v, err)
		}
		cfg.Port = port
	}
	if v := site["hive.server2.authentication"]; v != "" {
		auth, err := normalizeAuth(v)
		if err != nil {
			return nil, fmt.Errorf("hive.server2.authentication: %w", err)
		}
		cfg.Auth = auth
	}

	// Environment. URL-level values are applied after these.
	if v := os.Getenv("HIVE_HOST"); v != "" {
		cfg.Host = v
	}
	if v := os.Getenv("HIVE_PORT"); v != "" {
		port, err := strconv.Atoi(v)
		if err != nil {
			return nil, fmt.Errorf("invalid HIVE_PORT %q: %w", v, err)
		}
		cfg.Port = port
	}
	if v := os.Getenv("HIVE_DATABASE"); v != "" {
		cfg.Database = v
	}
	if v := os.Getenv("HIVE_USER"); v != "" {
		cfg.Username = v
	}
	if v := os.Getenv("HIVE_PASSWORD"); v != "" {
		cfg.Password = v
	}
	if v := os.Getenv("HIVE_AUTH"); v != "" {
		auth, err := normalizeAuth(v)
		if err != nil {
			return nil, fmt.Errorf("HIVE_AUTH: %w", err)
		}
		cfg.Auth = auth
	}
	if v := os.Getenv("HIVE_PROTOCOL"); v != "" {
		cfg.Protocol = v
	}
	if v := os.Getenv("HIVE_FETCH_SIZE"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			return nil, fmt.Errorf("invalid HIVE_FETCH_SIZE %q: %w", v, err)
		}
		cfg.FetchSize = n
	}

	// URL (from --url or the environment).
	rawURL := o.url
	if rawURL == "" {
		rawURL = os.Getenv("HIVE_SERVER2_URL")
	}
	if rawURL == "" {
		rawURL = os.Getenv("HIVE_URL")
	}
	if rawURL != "" {
		u, err := parseURL(rawURL)
		if err != nil {
			return nil, err
		}
		if u.host != "" {
			cfg.Host = u.host
		}
		if u.port != 0 {
			cfg.Port = u.port
		}
		if u.database != "" {
			cfg.Database = u.database
		}
		if u.username != "" {
			cfg.Username = u.username
		}
		if u.password != "" {
			cfg.Password = u.password
		}
		if u.auth != "" {
			auth, err := normalizeAuth(u.auth)
			if err != nil {
				return nil, fmt.Errorf("URL auth: %w", err)
			}
			cfg.Auth = auth
		}
		if err := res.vars.add(u.hiveconf, res.vars.hiveconf); err != nil {
			return nil, fmt.Errorf("URL hiveconf: %w", err)
		}
		if err := res.vars.add(u.hivevar, res.vars.hivevar); err != nil {
			return nil, fmt.Errorf("URL hivevar: %w", err)
		}
	}

	// CLI flags have the highest precedence.
	if o.host != "" {
		cfg.Host = o.host
	}
	if o.port != 0 {
		cfg.Port = o.port
	}
	if o.database != "" {
		cfg.Database = o.database
	}
	if o.username != "" {
		cfg.Username = o.username
	}
	if o.password != "" {
		cfg.Password = o.password
	}
	if o.auth != "" {
		auth, err := normalizeAuth(o.auth)
		if err != nil {
			return nil, err
		}
		cfg.Auth = auth
	}
	if o.protocol != "" {
		cfg.Protocol = o.protocol
	}
	if o.fetchSize > 0 {
		cfg.FetchSize = o.fetchSize
	}
	if err := res.vars.add(o.hiveconf, res.vars.hiveconf); err != nil {
		return nil, fmt.Errorf("--hiveconf: %w", err)
	}
	if err := res.vars.add(o.hivevar, res.vars.hivevar); err != nil {
		return nil, fmt.Errorf("--hivevar: %w", err)
	}
	if len(res.vars.hiveconf) > 0 {
		cfg.SessionConfig = res.vars.hiveconf
	}
	if o.verbose {
		cfg.Logf = func(format string, args ...any) {
			fmt.Fprintf(os.Stderr, format+"\n", args...)
		}
	}

	if cfg.Auth == hive.AuthLDAP && cfg.Password == "" {
		if !isTerminal(os.Stdin) {
			return nil, fmt.Errorf("LDAP authentication requires a password (use --password or HIVE_PASSWORD)")
		}
		password, err := promptPassword()
		if err != nil {
			return nil, err
		}
		cfg.Password = password
	}

	res.cfg = cfg
	res.historyFile = resolveHistoryFile(res.historyFile, res.noHistory)
	return res, nil
}

func normalizeAuth(s string) (hive.AuthMode, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", "none", "anonymous":
		return hive.AuthNone, nil
	case "nosasl", "no_sasl", "no-sasl":
		return hive.AuthNosASL, nil
	case "ldap":
		return hive.AuthLDAP, nil
	case "kerberos", "custom", "pam", "digest", "digest-md5", "digest_md5":
		return "", fmt.Errorf("authentication mode %q is not supported by this version (supported: none, nosasl, ldap)", s)
	default:
		return "", fmt.Errorf("unknown authentication mode %q (use none, nosasl or ldap)", s)
	}
}

func parseURL(raw string) (*urlOptions, error) {
	raw = strings.TrimSpace(raw)
	lower := strings.ToLower(raw)
	if strings.HasPrefix(lower, "jdbc:") {
		raw = raw[len("jdbc:"):]
	}
	u, err := url.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("invalid URL %q: %w", raw, err)
	}
	scheme := strings.ToLower(u.Scheme)
	switch scheme {
	case "hive2", "hive":
	case "http", "https":
		return nil, fmt.Errorf("HTTP transport is not supported (URL scheme %q)", u.Scheme)
	default:
		return nil, fmt.Errorf("unsupported URL scheme %q (use hive2:// or hive://)", u.Scheme)
	}
	if strings.Contains(u.Host, ",") {
		return nil, fmt.Errorf("multiple hosts in URL are not supported; use a single host:port")
	}
	if u.Hostname() == "" {
		return nil, fmt.Errorf("URL %q has no host", raw)
	}

	out := &urlOptions{host: u.Hostname()}
	if p := u.Port(); p != "" {
		port, err := strconv.Atoi(p)
		if err != nil {
			return nil, fmt.Errorf("invalid port in URL: %w", err)
		}
		out.port = port
	}
	if u.User != nil {
		out.username = u.User.Username()
		if pass, ok := u.User.Password(); ok {
			out.password = pass
		}
	}

	path := strings.TrimPrefix(u.Path, "/")
	parts := strings.Split(path, ";")
	if len(parts) > 0 {
		db, err := url.PathUnescape(parts[0])
		if err != nil {
			return nil, fmt.Errorf("invalid database in URL: %w", err)
		}
		out.database = db
	}
	for _, p := range parts[1:] {
		key, value, ok := strings.Cut(p, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		lowerKey := strings.ToLower(key)
		switch {
		case lowerKey == "auth":
			out.auth = value
		case lowerKey == "user" || lowerKey == "username":
			out.username = value
		case lowerKey == "password":
			out.password = value
		case lowerKey == "transportmode" || lowerKey == "transport.mode":
			if value != "" && !strings.EqualFold(value, "binary") {
				return nil, fmt.Errorf("transport mode %q is not supported (only binary is implemented)", value)
			}
		case lowerKey == "ssl":
			if strings.EqualFold(value, "true") {
				return nil, fmt.Errorf("SSL is not supported by this version")
			}
		case lowerKey == "servicediscoverymode":
			if value != "" && !strings.EqualFold(value, "none") {
				return nil, fmt.Errorf("ZooKeeper service discovery is not supported; connect to a single HiveServer2 host")
			}
		case lowerKey == "zookeepernamespace":
			return nil, fmt.Errorf("ZooKeeper service discovery is not supported; connect to a single HiveServer2 host")
		case lowerKey == "principal":
			return nil, fmt.Errorf("Kerberos authentication is not supported by this version")
		case lowerKey == "httppath":
			return nil, fmt.Errorf("HTTP transport is not supported by this version")
		case strings.HasPrefix(lowerKey, "hiveconf:"):
			out.hiveconf = append(out.hiveconf, key[len("hiveconf:"):]+"="+value)
		case strings.HasPrefix(lowerKey, "hivevar:"):
			out.hivevar = append(out.hivevar, key[len("hivevar:"):]+"="+value)
		}
	}

	q := u.Query()
	for key, values := range q {
		lowerKey := strings.ToLower(key)
		value := ""
		if len(values) > 0 {
			value = values[len(values)-1]
		}
		switch {
		case lowerKey == "auth":
			out.auth = value
		case lowerKey == "user" || lowerKey == "username":
			out.username = value
		case lowerKey == "password":
			out.password = value
		case lowerKey == "transportmode" || lowerKey == "transport.mode":
			if value != "" && !strings.EqualFold(value, "binary") {
				return nil, fmt.Errorf("transport mode %q is not supported (only binary is implemented)", value)
			}
		case lowerKey == "ssl":
			if strings.EqualFold(value, "true") {
				return nil, fmt.Errorf("SSL is not supported by this version")
			}
		case lowerKey == "servicediscoverymode":
			if value != "" && !strings.EqualFold(value, "none") {
				return nil, fmt.Errorf("ZooKeeper service discovery is not supported; connect to a single HiveServer2 host")
			}
		case lowerKey == "zookeepernamespace":
			return nil, fmt.Errorf("ZooKeeper service discovery is not supported; connect to a single HiveServer2 host")
		case lowerKey == "principal":
			return nil, fmt.Errorf("Kerberos authentication is not supported by this version")
		case lowerKey == "httppath":
			return nil, fmt.Errorf("HTTP transport is not supported by this version")
		case strings.HasPrefix(lowerKey, "hiveconf:"):
			out.hiveconf = append(out.hiveconf, key[len("hiveconf:"):]+"="+value)
		case strings.HasPrefix(lowerKey, "hivevar:"):
			out.hivevar = append(out.hivevar, key[len("hivevar:"):]+"="+value)
		}
	}
	return out, nil
}

func validateDelimiter(delimiter string) error {
	if strings.TrimSpace(delimiter) == "" {
		return fmt.Errorf("delimiter must not be empty")
	}
	if strings.ContainsAny(delimiter, " \t\r\n") {
		return fmt.Errorf("delimiter must not contain whitespace")
	}
	switch delimiter[0] {
	case '\'', '"', '`':
		return fmt.Errorf("delimiter must not start with a quote character")
	}
	if delimiter == "--" || strings.HasPrefix(delimiter, "/*") {
		return fmt.Errorf("delimiter %q conflicts with SQL comments", delimiter)
	}
	return nil
}

func promptPassword() (string, error) {
	fmt.Fprint(os.Stderr, "Password: ")
	data, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Fprintln(os.Stderr)
	if err != nil {
		return "", fmt.Errorf("read password: %w", err)
	}
	return string(data), nil
}

func resolveHistoryFile(path string, disabled bool) string {
	if disabled {
		return ""
	}
	if path == "" {
		path = "~/.hive-go_history"
	}
	if strings.HasPrefix(path, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			path = filepath.Join(home, path[2:])
		}
	}
	return path
}
