package issues

import "testing"

func TestParseIssueSearchCanonical(t *testing.T) {
	filter, err := ParseIssueSearch("api failure is:resolved env:prod release:api@1 level:error tag:region=eu assignee:none last_seen_after:2026-04-01")
	if err != nil {
		t.Fatalf("parse search: %v", err)
	}

	if filter.Status != IssueStatusResolved {
		t.Fatalf("unexpected status: %s", filter.Status)
	}
	if filter.Text != "api failure" {
		t.Fatalf("unexpected text: %s", filter.Text)
	}
	if filter.Environment != "prod" || filter.Release != "api@1" || filter.Level != "error" {
		t.Fatalf("unexpected dimensions: %#v", filter)
	}
	if filter.TagKey != "region" || filter.TagValue != "eu" {
		t.Fatalf("unexpected tag: %#v", filter)
	}
	if filter.Assignee.Kind != AssignmentTargetNone {
		t.Fatalf("unexpected assignee: %#v", filter.Assignee)
	}
	if filter.Canonical() != "is:resolved environment:prod release:api@1 level:error tag:region=eu assignee:none last_seen_after:2026-04-01 text:api failure" {
		t.Fatalf("unexpected canonical query: %s", filter.Canonical())
	}
}

func TestParseIssueSearchRejectsUnsupportedSyntax(t *testing.T) {
	cases := []string{
		"unknown:value",
		"tag:region",
		"status:closed",
		"last_seen_after:not-a-date",
	}

	for _, input := range cases {
		t.Run(input, func(t *testing.T) {
			_, err := ParseIssueSearch(input)
			if err == nil {
				t.Fatal("expected search parse error")
			}
		})
	}
}
