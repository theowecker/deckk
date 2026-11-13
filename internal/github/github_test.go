package github

import "testing"

func TestParse(t *testing.T) {
	pr, ok := parse([]byte(`{"state":"OPEN","isDraft":false,"reviewDecision":"CHANGES_REQUESTED","statusCheckRollup":[
		{"__typename":"CheckRun","name":"lint","status":"COMPLETED","conclusion":"SUCCESS"},
		{"__typename":"CheckRun","name":"test","status":"COMPLETED","conclusion":"FAILURE"},
		{"__typename":"StatusContext","context":"deploy","state":"PENDING"}]}`))
	if !ok || pr.State != "open" || pr.Checks != "failing" || len(pr.Failing) != 1 || pr.Failing[0] != "test" || pr.Review != "changes requested" {
		t.Fatalf("%+v", pr)
	}
	pr, _ = parse([]byte(`{"state":"OPEN","isDraft":true,"statusCheckRollup":[{"name":"ci","status":"IN_PROGRESS"}]}`))
	if pr.State != "draft" || pr.Checks != "pending" {
		t.Fatalf("%+v", pr)
	}
	pr, _ = parse([]byte(`{"state":"MERGED","reviewDecision":"APPROVED","statusCheckRollup":[{"name":"ci","status":"COMPLETED","conclusion":"SUCCESS"}]}`))
	if !pr.Done() || pr.Checks != "passing" || pr.Review != "approved" {
		t.Fatalf("%+v", pr)
	}
}
