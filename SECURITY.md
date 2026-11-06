# Security Policy

## Supported versions

Security fixes target the latest tagged release and the `master` branch. The
project is pre-1.0; fixes are typically released on `master` first.

## Reporting a vulnerability

**Do not open a public issue for a security problem.**

Use GitHub's private vulnerability reporting form:

https://github.com/wgzhao/hive-go/security/advisories/new

If the form is unavailable, open a minimal issue that says you need a private
channel, without exploit details or sensitive data, and the maintainer will
follow up.

Please include:

- Affected version (`hive --version`) and platform.
- HiveServer2 version, auth mode, and protocol version.
- A minimal reproduction (command, SQL, input file, or packet sequence).
- The impact you observed, and any suggested fix.

Never include real passwords, LDAP bind credentials, hostnames, or customer
data. Redact them before sending logs.

## What to expect

- Acknowledgement within 7 days.
- Triage and a severity assessment as soon as the report is reproducible.
- A fix on `master` and, when applicable, a patched release.
- Coordinated disclosure. We will credit reporters who want credit and will
  agree on a disclosure date before publishing details.

## Scope

In scope:

- Credential handling, including accidental exposure in stdout, stderr,
  history files, or E2E artifacts.
- Protocol parsing, SASL framing, Thrift transport handling, and result
  decoding on untrusted server data.
- Crashes, unbounded memory use, or denial of service caused by a malformed
  result batch, null bitmap, or blob rowset.
- Terminal injection through query results in the supported output formats.
- Bugs that let a connection or operation act outside the requested database,
  session, or server.

Out of scope (documented limitations or third-party behavior):

- The absence of TLS, Kerberos/GSSAPI, or HTTP transport. These are explicit
  v1 limitations; LDAP without TLS sends credentials in cleartext, as warned in
  the README.
- Vulnerabilities in Apache Hive, the Thrift runtime, the JDBC/HiveServer2
  server, or a compromised endpoint.
- SQL authorization or privileges enforced by the server. `hive-go` is a client
  and does not add its own authorization layer.
- Denial of service achieved only by a malicious server the user deliberately
  connected to, unless it causes a client-side crash or resource leak.

## Safe harbor

We support good-faith security research on the client. Do not access data you do
not own, disrupt a server, or test against systems without permission. Reports
made under this policy will be handled cooperatively.
