package main

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/amcchord/ringring/internal/openaiadmin"
)

type fakeRetentionVerifier struct {
	organization      openaiadmin.OrganizationDataRetention
	organizationErr   error
	organizationCalls int
	projectErrors     map[string]error
	projectCalls      []string
}

func (f *fakeRetentionVerifier) VerifyOrganizationZeroDataRetention(context.Context) (openaiadmin.OrganizationDataRetention, error) {
	f.organizationCalls++
	return f.organization, f.organizationErr
}

func (f *fakeRetentionVerifier) VerifyProjectZeroDataRetention(_ context.Context, projectID string) (openaiadmin.ProjectDataRetention, error) {
	f.projectCalls = append(f.projectCalls, projectID)
	if err := f.projectErrors[projectID]; err != nil {
		return openaiadmin.ProjectDataRetention{}, err
	}
	return openaiadmin.ProjectDataRetention{Type: "organization_default"}, nil
}

type fakeProjectSource struct {
	projectIDs []string
	err        error
	calls      int
}

func (f *fakeProjectSource) ListOpenAIProjectIDs(context.Context) ([]string, error) {
	f.calls++
	return append([]string(nil), f.projectIDs...), f.err
}

func TestRequireOpenAIZeroDataRetention(t *testing.T) {
	closedVerifier := &fakeRetentionVerifier{organizationErr: errors.New("must not be called")}
	closedProjects := &fakeProjectSource{err: errors.New("must not be called")}
	retention, err := requireOpenAIZeroDataRetention(t.Context(), false, closedVerifier, closedProjects)
	if err != nil || retention != (openAIRetentionReport{}) || closedVerifier.organizationCalls != 0 || len(closedVerifier.projectCalls) != 0 || closedProjects.calls != 0 {
		t.Fatalf("closed gate consulted retention sources: report=%#v verifier=%#v projects=%#v error=%v", retention, closedVerifier, closedProjects, err)
	}

	verified := &fakeRetentionVerifier{organization: openaiadmin.OrganizationDataRetention{Type: "zero_data_retention"}}
	projects := &fakeProjectSource{projectIDs: []string{"proj_alpha", "proj_bravo"}}
	retention, err = requireOpenAIZeroDataRetention(t.Context(), true, verified, projects)
	if err != nil || retention.OrganizationType != "zero_data_retention" || retention.ProjectsVerified != 2 || verified.organizationCalls != 1 || !reflect.DeepEqual(verified.projectCalls, projects.projectIDs) || projects.calls != 1 {
		t.Fatalf("verified ZDR was rejected: report=%#v verifier=%#v projects=%#v error=%v", retention, verified, projects, err)
	}

	denied := &fakeRetentionVerifier{organizationErr: errors.New("not eligible")}
	unusedProjects := &fakeProjectSource{projectIDs: []string{"proj_unused"}}
	if _, err := requireOpenAIZeroDataRetention(t.Context(), true, denied, unusedProjects); err == nil || denied.organizationCalls != 1 || unusedProjects.calls != 0 {
		t.Fatalf("unverified organization was accepted or project state was consulted: verifier=%#v projects=%#v error=%v", denied, unusedProjects, err)
	}

	sourceFailure := &fakeProjectSource{err: errors.New("database unavailable")}
	if _, err := requireOpenAIZeroDataRetention(t.Context(), true, verified, sourceFailure); err == nil || sourceFailure.calls != 1 {
		t.Fatalf("project source failure was accepted: source=%#v error=%v", sourceFailure, err)
	}

	privateProjectID := "proj_private_identifier"
	projectDenied := &fakeRetentionVerifier{
		organization:  openaiadmin.OrganizationDataRetention{Type: "enhanced_zero_data_retention"},
		projectErrors: map[string]error{privateProjectID: errors.New("modified abuse monitoring")},
	}
	if _, err := requireOpenAIZeroDataRetention(t.Context(), true, projectDenied, &fakeProjectSource{projectIDs: []string{privateProjectID}}); err == nil || strings.Contains(err.Error(), privateProjectID) {
		t.Fatalf("unsafe project was accepted or its identifier leaked: %v", err)
	}
}
