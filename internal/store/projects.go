package store

import "context"

func (s *Store) ListProjects(ctx context.Context) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT project FROM runs
		UNION SELECT project FROM datasets
		UNION SELECT project FROM prompt_versions
		UNION SELECT project FROM hub_tokens
		ORDER BY project`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	projects := []string{}
	for rows.Next() {
		var project string
		if err := rows.Scan(&project); err != nil {
			return nil, err
		}
		projects = append(projects, project)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(projects) == 0 {
		projects = append(projects, "default")
	}
	return projects, nil
}
