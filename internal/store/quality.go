package store

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"
)

// Dataset groups captured cases within one project.
type Dataset struct {
	ID        string    `json:"id"`
	Project   string    `json:"project"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"created_at"`
	Frozen    bool      `json:"frozen"`
}

// ErrDatasetFrozen means an experiment has fixed the dataset's case contents.
var ErrDatasetFrozen = errors.New("dataset has experiments and is frozen; clone it to change cases or expected outputs")

// DatasetItem stores the captured input and reviewed expected output for a case.
type DatasetItem struct {
	ID           string    `json:"id"`
	DatasetID    string    `json:"dataset_id"`
	SourceRunID  string    `json:"source_run_id"`
	InputJSON    string    `json:"input_json"`
	ExpectedJSON string    `json:"expected_json"`
	CreatedAt    time.Time `json:"created_at"`
}

// Score is one human or automated observation for a run and optional case.
type Score struct {
	ID        int64     `json:"id"`
	RunID     string    `json:"run_id"`
	ItemID    string    `json:"item_id,omitempty"`
	Name      string    `json:"name"`
	Source    string    `json:"source"`
	Comment   string    `json:"comment,omitempty"`
	Value     float64   `json:"value"`
	CreatedAt time.Time `json:"created_at"`
}

// Experiment names a set of scored runs against a frozen dataset.
type Experiment struct {
	ID        string    `json:"id"`
	DatasetID string    `json:"dataset_id"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"created_at"`
}

// ExperimentComparison reports paired score averages over one dataset.
type ExperimentComparison struct {
	DatasetID string  `json:"dataset_id"`
	ScoreName string  `json:"score_name"`
	Items     int     `json:"items"`
	Baseline  float64 `json:"baseline"`
	Candidate float64 `json:"candidate"`
	Delta     float64 `json:"delta"`
}

// DatasetBelongsToProject checks the project boundary for a dataset.
func (s *Store) DatasetBelongsToProject(ctx context.Context, datasetID, project string) bool {
	var found int
	err := s.db.QueryRowContext(ctx, `SELECT 1 FROM datasets WHERE id = ? AND project = ?`, datasetID, project).Scan(&found)
	return err == nil
}

// GetDataset returns a dataset with its current frozen state.
func (s *Store) GetDataset(ctx context.Context, id string) (Dataset, error) {
	var d Dataset
	var at int64
	err := s.db.QueryRowContext(ctx, `SELECT d.id, d.project, d.name, d.created_at,
		EXISTS(SELECT 1 FROM experiments e WHERE e.dataset_id = d.id) FROM datasets d WHERE d.id = ?`, id).
		Scan(&d.ID, &d.Project, &d.Name, &at, &d.Frozen)
	if err != nil {
		return Dataset{}, err
	}
	d.CreatedAt = time.Unix(0, at).UTC()
	return d, nil
}

// ExperimentBelongsToProject checks the project boundary through its dataset.
func (s *Store) ExperimentBelongsToProject(ctx context.Context, experimentID, project string) bool {
	var found int
	err := s.db.QueryRowContext(ctx, `SELECT 1 FROM experiments e JOIN datasets d ON d.id = e.dataset_id
		WHERE e.id = ? AND d.project = ?`, experimentID, project).Scan(&found)
	return err == nil
}

func newID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}

// CreateDataset adds an editable case collection to one project.
func (s *Store) CreateDataset(ctx context.Context, project, name string) (Dataset, error) {
	if project == "" {
		project = "default"
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return Dataset{}, errors.New("dataset name is required")
	}
	id, err := newID()
	if err != nil {
		return Dataset{}, err
	}
	d := Dataset{ID: id, Project: project, Name: name, CreatedAt: time.Now().UTC()}
	_, err = s.db.ExecContext(ctx, `INSERT INTO datasets (id, project, name, created_at) VALUES (?, ?, ?, ?)`,
		d.ID, d.Project, d.Name, d.CreatedAt.UnixNano())
	if err == nil {
		s.notify()
	}
	return d, err
}

// ListDatasets returns the datasets visible within one project.
func (s *Store) ListDatasets(ctx context.Context, project string) ([]Dataset, error) {
	query := `SELECT d.id, d.project, d.name, d.created_at,
		EXISTS(SELECT 1 FROM experiments e WHERE e.dataset_id = d.id) FROM datasets d`
	args := []any{}
	if project != "" {
		query += ` WHERE d.project = ?`
		args = append(args, project)
	}
	query += ` ORDER BY d.created_at DESC, d.id DESC`
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Dataset{}
	for rows.Next() {
		var d Dataset
		var at int64
		if err := rows.Scan(&d.ID, &d.Project, &d.Name, &at, &d.Frozen); err != nil {
			return nil, err
		}
		d.CreatedAt = time.Unix(0, at).UTC()
		out = append(out, d)
	}
	return out, rows.Err()
}

// AddDatasetItem snapshots captured input and expected output. Missing content
// is an error so a no-content trace cannot silently become an empty test case.
func (s *Store) AddDatasetItem(ctx context.Context, datasetID, runID string) (DatasetItem, error) {
	var project, runProject string
	if err := s.db.QueryRowContext(ctx, `SELECT project FROM datasets WHERE id = ?`, datasetID).Scan(&project); err != nil {
		return DatasetItem{}, err
	}
	if err := s.db.QueryRowContext(ctx, `SELECT project FROM runs WHERE id = ?`, runID).Scan(&runProject); err != nil {
		return DatasetItem{}, err
	}
	if project != runProject {
		return DatasetItem{}, errors.New("run and dataset belong to different projects")
	}
	rows, err := s.db.QueryContext(ctx, `SELECT c.role, c.body FROM contents c JOIN spans sp ON sp.id = c.span_id
		WHERE sp.run_id = ? ORDER BY sp.started_at, c.seq`, runID)
	if err != nil {
		return DatasetItem{}, err
	}
	defer rows.Close()
	var input, expected string
	for rows.Next() {
		var role, body string
		if err := rows.Scan(&role, &body); err != nil {
			return DatasetItem{}, err
		}
		if input == "" && (role == "input" || role == "user") {
			input = body
		}
		if role == "output" || role == "assistant" {
			expected = body
		}
	}
	if err := rows.Err(); err != nil {
		return DatasetItem{}, err
	}
	if input == "" || expected == "" {
		return DatasetItem{}, errors.New("run needs recorded input and output to create a dataset item")
	}
	in, _ := json.Marshal(input)
	out, _ := json.Marshal(expected)
	id, err := newID()
	if err != nil {
		return DatasetItem{}, err
	}
	item := DatasetItem{
		ID: id, DatasetID: datasetID, SourceRunID: runID,
		InputJSON: string(in), ExpectedJSON: string(out), CreatedAt: time.Now().UTC(),
	}
	res, err := s.db.ExecContext(ctx, `INSERT INTO dataset_items
		(id, dataset_id, source_run_id, input_json, expected_json, created_at)
		SELECT ?, ?, ?, ?, ?, ? WHERE NOT EXISTS (SELECT 1 FROM experiments WHERE dataset_id = ?)`,
		item.ID, item.DatasetID, item.SourceRunID, item.InputJSON, item.ExpectedJSON, item.CreatedAt.UnixNano(), datasetID)
	if err != nil {
		return DatasetItem{}, err
	}
	if n, err := res.RowsAffected(); err != nil {
		return DatasetItem{}, err
	} else if n == 0 {
		return DatasetItem{}, ErrDatasetFrozen
	}
	s.notify()
	return item, nil
}

// SetDatasetExpected corrects the captured answer before the first experiment.
// The SQL guard also prevents concurrent edits from changing experiment cases.
func (s *Store) SetDatasetExpected(ctx context.Context, datasetID, itemID, expected string) (DatasetItem, error) {
	if expected == "" {
		return DatasetItem{}, errors.New("expected output must not be empty")
	}
	raw, err := json.Marshal(expected)
	if err != nil {
		return DatasetItem{}, err
	}
	var item DatasetItem
	var at int64
	err = s.db.QueryRowContext(ctx, `UPDATE dataset_items SET expected_json = ?
		WHERE id = ? AND dataset_id = ? AND NOT EXISTS (SELECT 1 FROM experiments WHERE dataset_id = ?)
		RETURNING id, dataset_id, source_run_id, input_json, expected_json, created_at`, string(raw), itemID, datasetID, datasetID).
		Scan(&item.ID, &item.DatasetID, &item.SourceRunID, &item.InputJSON, &item.ExpectedJSON, &at)
	if errors.Is(err, sql.ErrNoRows) {
		if d, e := s.GetDataset(ctx, datasetID); e == nil && d.Frozen {
			return DatasetItem{}, ErrDatasetFrozen
		}
	}
	if err != nil {
		return DatasetItem{}, err
	}
	item.CreatedAt = time.Unix(0, at).UTC()
	s.notify()
	return item, nil
}

// CloneDataset creates an editable snapshot with new case IDs while retaining
// source-trace links. Existing experiment results keep referring to old IDs.
func (s *Store) CloneDataset(ctx context.Context, datasetID, name string) (Dataset, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return Dataset{}, errors.New("dataset name is required")
	}
	id, err := newID()
	if err != nil {
		return Dataset{}, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Dataset{}, err
	}
	defer func() { _ = tx.Rollback() }()
	d := Dataset{ID: id, Name: name, CreatedAt: time.Now().UTC()}
	if err := tx.QueryRowContext(ctx, `SELECT project FROM datasets WHERE id = ?`, datasetID).Scan(&d.Project); err != nil {
		return Dataset{}, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO datasets(id, project, name, created_at) VALUES (?, ?, ?, ?)`, d.ID, d.Project, d.Name, d.CreatedAt.UnixNano()); err != nil {
		return Dataset{}, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO dataset_items(id, dataset_id, source_run_id, input_json, expected_json, created_at)
		SELECT lower(hex(randomblob(16))), ?, source_run_id, input_json, expected_json, created_at
		FROM dataset_items WHERE dataset_id = ?`, d.ID, datasetID); err != nil {
		return Dataset{}, err
	}
	if err := tx.Commit(); err != nil {
		return Dataset{}, err
	}
	s.notify()
	return d, nil
}

// ListDatasetItems returns the recorded cases in a dataset.
func (s *Store) ListDatasetItems(ctx context.Context, datasetID string) ([]DatasetItem, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, dataset_id, source_run_id, input_json, expected_json, created_at
		FROM dataset_items WHERE dataset_id = ? ORDER BY created_at, id`, datasetID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []DatasetItem{}
	for rows.Next() {
		var item DatasetItem
		var at int64
		if err := rows.Scan(&item.ID, &item.DatasetID, &item.SourceRunID, &item.InputJSON, &item.ExpectedJSON, &at); err != nil {
			return nil, err
		}
		item.CreatedAt = time.Unix(0, at).UTC()
		out = append(out, item)
	}
	return out, rows.Err()
}

// AddScore stores one named, finite score for a run and optional dataset item.
func (s *Store) AddScore(ctx context.Context, runID, itemID, name string, value float64, source, comment string) (Score, error) {
	if name == "" || math.IsNaN(value) || math.IsInf(value, 0) || value < 0 || value > 1 {
		return Score{}, errors.New("score needs a name and a finite value between 0 and 1")
	}
	if source == "" {
		source = "human"
	}
	if itemID != "" {
		var itemProject, runProject string
		if err := s.db.QueryRowContext(ctx, `SELECT d.project FROM dataset_items i JOIN datasets d ON d.id = i.dataset_id WHERE i.id = ?`, itemID).Scan(&itemProject); err != nil {
			return Score{}, err
		}
		if err := s.db.QueryRowContext(ctx, `SELECT project FROM runs WHERE id = ?`, runID).Scan(&runProject); err != nil {
			return Score{}, err
		}
		if itemProject != runProject {
			return Score{}, errors.New("run and item belong to different projects")
		}
	}
	at := time.Now().UTC()
	res, err := s.db.ExecContext(ctx, `INSERT INTO scores (run_id, item_id, name, value, source, comment, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)`, runID, nullString(itemID), name, value, source, comment, at.UnixNano())
	if err != nil {
		return Score{}, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return Score{}, err
	}
	s.notify()
	return Score{
		ID: id, RunID: runID, ItemID: itemID, Name: name,
		Value: value, Source: source, Comment: comment, CreatedAt: at,
	}, nil
}

// CreateExperiment freezes its dataset before recording result runs.
func (s *Store) CreateExperiment(ctx context.Context, datasetID, name string) (Experiment, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return Experiment{}, errors.New("experiment name is required")
	}
	id, err := newID()
	if err != nil {
		return Experiment{}, err
	}
	e := Experiment{ID: id, DatasetID: datasetID, Name: name, CreatedAt: time.Now().UTC()}
	res, err := s.db.ExecContext(ctx, `INSERT INTO experiments (id, dataset_id, name, created_at)
		SELECT ?, ?, ?, ? WHERE EXISTS (SELECT 1 FROM dataset_items WHERE dataset_id = ?)`,
		e.ID, e.DatasetID, e.Name, e.CreatedAt.UnixNano(), datasetID)
	if err != nil {
		return Experiment{}, err
	}
	if n, err := res.RowsAffected(); err != nil {
		return Experiment{}, err
	} else if n == 0 {
		return Experiment{}, errors.New("dataset needs at least one case before creating an experiment")
	}
	s.notify()
	return e, nil
}

// ListExperiments returns the experiments attached to one dataset.
func (s *Store) ListExperiments(ctx context.Context, datasetID string) ([]Experiment, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, dataset_id, name, created_at
		FROM experiments WHERE dataset_id = ? ORDER BY created_at DESC, id DESC`, datasetID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Experiment{}
	for rows.Next() {
		var e Experiment
		var at int64
		if err := rows.Scan(&e.ID, &e.DatasetID, &e.Name, &at); err != nil {
			return nil, err
		}
		e.CreatedAt = time.Unix(0, at).UTC()
		out = append(out, e)
	}
	return out, rows.Err()
}

// ListScores returns all recorded scores for a run.
func (s *Store) ListScores(ctx context.Context, runID string) ([]Score, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, run_id, item_id, name, value, source, comment, created_at
		FROM scores WHERE run_id = ? ORDER BY created_at, id`, runID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Score{}
	for rows.Next() {
		var sc Score
		var item sql.NullString
		var at int64
		if err := rows.Scan(&sc.ID, &sc.RunID, &item, &sc.Name, &sc.Value, &sc.Source, &sc.Comment, &at); err != nil {
			return nil, err
		}
		sc.ItemID = item.String
		sc.CreatedAt = time.Unix(0, at).UTC()
		out = append(out, sc)
	}
	return out, rows.Err()
}

// RecordExperimentResult copies the latest named score on a run for a dataset
// item. The experiment records a numeric snapshot for comparisons.
func (s *Store) RecordExperimentResult(ctx context.Context, experimentID, itemID, runID, scoreName string) error {
	var datasetID, itemDataset, project, runProject string
	if err := s.db.QueryRowContext(ctx, `SELECT dataset_id FROM experiments WHERE id = ?`, experimentID).Scan(&datasetID); err != nil {
		return err
	}
	if err := s.db.QueryRowContext(ctx, `SELECT dataset_id FROM dataset_items WHERE id = ?`, itemID).Scan(&itemDataset); err != nil {
		return err
	}
	if datasetID != itemDataset {
		return errors.New("item is outside experiment dataset")
	}
	if err := s.db.QueryRowContext(ctx, `SELECT project FROM datasets WHERE id = ?`, datasetID).Scan(&project); err != nil {
		return err
	}
	if err := s.db.QueryRowContext(ctx, `SELECT project FROM runs WHERE id = ?`, runID).Scan(&runProject); err != nil {
		return err
	}
	if project != runProject {
		return errors.New("run and experiment belong to different projects")
	}
	var score float64
	err := s.db.QueryRowContext(ctx, `SELECT value FROM scores WHERE run_id = ? AND item_id = ? AND name = ?
		ORDER BY created_at DESC, id DESC LIMIT 1`, runID, itemID, scoreName).Scan(&score)
	if err != nil {
		return fmt.Errorf("score for run/item/name: %w", err)
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO experiment_results (experiment_id, item_id, run_id, score_name, score)
		VALUES (?, ?, ?, ?, ?) ON CONFLICT (experiment_id, item_id, score_name)
		DO UPDATE SET run_id = excluded.run_id, score = excluded.score`, experimentID, itemID, runID, scoreName, score)
	if err == nil {
		s.notify()
	}
	return err
}

// CompareExperiments requires identical datasets and complete score coverage.
// Delta is candidate minus baseline; positive values are improvements.
func (s *Store) CompareExperiments(ctx context.Context, baselineID, candidateID, scoreName string) (ExperimentComparison, error) {
	var leftDataset, rightDataset string
	if err := s.db.QueryRowContext(ctx, `SELECT dataset_id FROM experiments WHERE id = ?`, baselineID).Scan(&leftDataset); err != nil {
		return ExperimentComparison{}, err
	}
	if err := s.db.QueryRowContext(ctx, `SELECT dataset_id FROM experiments WHERE id = ?`, candidateID).Scan(&rightDataset); err != nil {
		return ExperimentComparison{}, err
	}
	if leftDataset != rightDataset {
		return ExperimentComparison{}, errors.New("experiments use different datasets")
	}
	var total int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM dataset_items WHERE dataset_id = ?`, leftDataset).Scan(&total); err != nil {
		return ExperimentComparison{}, err
	}
	if total == 0 {
		return ExperimentComparison{}, errors.New("dataset is empty")
	}
	var paired int
	var leftSum, rightSum sql.NullFloat64
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*), SUM(a.score), SUM(b.score)
		FROM experiment_results a JOIN experiment_results b ON b.item_id = a.item_id AND b.score_name = a.score_name
		WHERE a.experiment_id = ? AND b.experiment_id = ? AND a.score_name = ?`,
		baselineID, candidateID, scoreName).Scan(&paired, &leftSum, &rightSum)
	if err != nil {
		return ExperimentComparison{}, err
	}
	if paired != total {
		return ExperimentComparison{}, fmt.Errorf("incomplete comparison: %d of %d items have paired scores", paired, total)
	}
	left, right := leftSum.Float64/float64(total), rightSum.Float64/float64(total)
	return ExperimentComparison{
		DatasetID: leftDataset, ScoreName: scoreName,
		Items: total, Baseline: left, Candidate: right, Delta: right - left,
	}, nil
}
