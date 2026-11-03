CREATE TABLE project_tool_schemas (
    project          TEXT NOT NULL DEFAULT 'default',
    tool_name        TEXT NOT NULL,
    version          INTEGER NOT NULL,
    schema_json      TEXT NOT NULL,
    learned_from_run TEXT,
    first_seen       INTEGER,
    last_seen        INTEGER,
    PRIMARY KEY (project, tool_name, version)
);

INSERT INTO project_tool_schemas
SELECT COALESCE((SELECT project FROM runs r WHERE r.id = t.learned_from_run), 'default'),
       tool_name, version, schema_json, learned_from_run, first_seen, last_seen
FROM tool_schemas t;

DROP TABLE tool_schemas;
ALTER TABLE project_tool_schemas RENAME TO tool_schemas;
