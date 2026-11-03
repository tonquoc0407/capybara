package store

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"testing"
)

func TestUpgradeFromVersionSevenKeepsTraceEvidence(t *testing.T) {
	path := filepath.Join(t.TempDir(), "old.db")
	db, err := sql.Open("sqlite", "file:"+path+"?_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatal(err)
	}
	for i := 1; i <= 7; i++ {
		matches, err := migrations.ReadDir("migrations")
		if err != nil {
			t.Fatal(err)
		}
		name := matches[i-1].Name()
		raw, err := migrations.ReadFile("migrations/" + name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(string(raw)); err != nil {
			t.Fatalf("old migration %s: %v", name, err)
		}
	}
	if _, err := db.Exec("PRAGMA user_version = 7"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO runs(id,source,label) VALUES('r','old','old run')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO spans(id,run_id,kind,name,status,attrs_json) VALUES('s','r','llm','old span','ok','{}')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO contents(span_id,role,seq,body,media_type) VALUES('s','user',0,'old body','text/plain')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO tool_schemas(tool_name,version,schema_json,learned_from_run) VALUES('fetch',3,'{"type":"object"}','r')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO findings(run_id,type,severity,detail_json) VALUES('r','parse_error','warning','{}')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO findings(run_id,span_id,type,severity,detail_json) VALUES('r','s','empty_payload','warning','{}')`); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	st, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	runs, err := st.ListRuns(context.Background())
	if err != nil || len(runs) != 1 || runs[0].Label != "old run" || runs[0].Project != "default" {
		t.Fatalf("old run lost during upgrade: %+v, %v", runs, err)
	}
	contents, err := st.Contents(context.Background(), "s")
	if err != nil || len(contents) != 1 || contents[0].Body != "old body" {
		t.Fatalf("old content lost: %+v, %v", contents, err)
	}
	if schema, err := st.LatestToolSchema(context.Background(), "fetch"); err != nil || schema == nil || schema.Project != "default" || schema.Version != 3 || schema.Schema != `{"type":"object"}` {
		t.Fatalf("old schema lost: %+v, %v", schema, err)
	}
	findings, err := st.Findings(context.Background(), "r")
	if err != nil || len(findings) != 2 || findings[0].Origin != "import" || findings[1].Origin != "analyzer_span" {
		t.Fatalf("finding origin after upgrade = %+v, %v", findings, err)
	}
	var version int
	if err := st.db.QueryRow("PRAGMA user_version").Scan(&version); err != nil || version != 17 {
		t.Fatalf("version after upgrade = %d, %v", version, err)
	}
	if err := migrate(st.db); err != nil {
		t.Fatal(fmt.Errorf("second migration: %w", err))
	}
}
