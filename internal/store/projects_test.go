package store

import (
	"context"
	"testing"
)

func TestListProjectsIncludesTokenAndDatasetWithoutRuns(t *testing.T) {
	s := openTemp(t)
	ctx := context.Background()
	projects, err := s.ListProjects(ctx)
	if err != nil || len(projects) != 1 || projects[0] != "default" {
		t.Fatalf("empty projects: %+v, %v", projects, err)
	}
	if _, err := s.CreateDataset(ctx, "alpha", "cases"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.CreateHubToken(ctx, "beta", "viewer"); err != nil {
		t.Fatal(err)
	}
	projects, err = s.ListProjects(ctx)
	if err != nil || len(projects) != 2 || projects[0] != "alpha" || projects[1] != "beta" {
		t.Fatalf("project list: %+v, %v", projects, err)
	}
}
