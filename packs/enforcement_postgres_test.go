package packs

import (
	"testing"

	"github.com/Caseymccallum/tapelog/internal/policy"
)

// postgresCases covers schema destruction and the write confirm.
var postgresCases = []caseSpec{
	{name: "DROP upper", pack: "postgres", tool: "query", args: `{"query":"DROP TABLE users"}`, verdict: policy.VerdictDeny, rule: "no-schema-destruction", rationale: "schema destruction"},
	{name: "DROP lower", pack: "postgres", tool: "query", args: `{"query":"drop table users"}`, verdict: policy.VerdictDeny, rule: "no-schema-destruction", rationale: "SQL keywords are case-insensitive"},
	{name: "DROP title", pack: "postgres", tool: "query", args: `{"query":"Drop table users"}`, verdict: policy.VerdictDeny, rule: "no-schema-destruction", rationale: "same"},
	{name: "DROP after a comment line", pack: "postgres", tool: "query", args: `{"query":"-- tidy up\nDROP TABLE users"}`, verdict: policy.VerdictDeny, rule: "no-schema-destruction", rationale: "the statement still runs"},
	{name: "DROP with tab separator", pack: "postgres", tool: "query", args: `{"query":"DROP\tTABLE users"}`, verdict: policy.VerdictDeny, rule: "no-schema-destruction", rationale: "whitespace other than a space is not a bypass"},
	{name: "TRUNCATE", pack: "postgres", tool: "query", args: `{"query":"TRUNCATE orders"}`, verdict: policy.VerdictDeny, rule: "no-schema-destruction", rationale: "data destruction"},
	{name: "truncate lower", pack: "postgres", tool: "query", args: `{"query":"truncate orders"}`, verdict: policy.VerdictDeny, rule: "no-schema-destruction", rationale: "same, lower case"},
	{name: "ALTER USER", pack: "postgres", tool: "query", args: `{"query":"ALTER USER app WITH PASSWORD 'x'"}`, verdict: policy.VerdictDeny, rule: "no-schema-destruction", rationale: "privilege change"},
	{name: "alter system lower", pack: "postgres", tool: "query", args: `{"query":"alter system set work_mem = '1GB'"}`, verdict: policy.VerdictDeny, rule: "no-schema-destruction", rationale: "server-wide change"},
	{name: "GRANT", pack: "postgres", tool: "query", args: `{"query":"GRANT ALL ON t TO u"}`, verdict: policy.VerdictDeny, rule: "no-schema-destruction", rationale: "privilege change"},
	{name: "grant lower", pack: "postgres", tool: "query", args: `{"query":"grant select on t to u"}`, verdict: policy.VerdictDeny, rule: "no-schema-destruction", rationale: "same, lower case"},
	{name: "DROP through postgres_ tool", pack: "postgres", tool: "postgres_run", args: `{"query":"DROP TABLE users"}`, verdict: policy.VerdictDeny, rule: "no-schema-destruction", rationale: "the same policy for the namespaced tool"},
	{name: "INSERT", pack: "postgres", tool: "query", args: `{"query":"INSERT INTO t VALUES (1)"}`, verdict: policy.VerdictConfirm, rule: "writes-confirm", rationale: "data write"},
	{name: "insert lower via execute", pack: "postgres", tool: "execute", args: `{"query":"insert into t values (1)"}`, verdict: policy.VerdictConfirm, rule: "writes-confirm", rationale: "same, lower case, other tool"},
	{name: "UPDATE", pack: "postgres", tool: "query", args: `{"query":"UPDATE t SET a=1"}`, verdict: policy.VerdictConfirm, rule: "writes-confirm", rationale: "data write"},
	{name: "DELETE", pack: "postgres", tool: "query", args: `{"query":"DELETE FROM t"}`, verdict: policy.VerdictConfirm, rule: "writes-confirm", rationale: "data write"},
	{name: "delete lower", pack: "postgres", tool: "query", args: `{"query":"delete from t"}`, verdict: policy.VerdictConfirm, rule: "writes-confirm", rationale: "same, lower case"},
	// legitimate reads
	{name: "SELECT", pack: "postgres", tool: "query", args: `{"query":"SELECT * FROM t"}`, verdict: policy.VerdictAllow, rule: "default", rationale: "selects are free"},
	{name: "identifier containing a keyword", pack: "postgres", tool: "query", args: `{"query":"SELECT drop_zone, delete_flag FROM t"}`, verdict: policy.VerdictAllow, rule: "default", rationale: "a column name is not a statement keyword"},
	{name: "select with a subquery", pack: "postgres", tool: "query", args: `{"query":"SELECT a FROM t WHERE b IN (SELECT c FROM u)"}`, verdict: policy.VerdictAllow, rule: "default", rationale: "reads stay free"},
}

func TestPostgresDeniesEnforce(t *testing.T) { runCases(t, postgresCases) }
