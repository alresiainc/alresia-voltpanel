package pipeline

import "testing"

func TestParseValidDefinition(t *testing.T) {
	def, err := Parse(`
steps:
  - name: test
    run: npm test
  - name: deploy
    deploy: target-1
  - name: verify
    healthcheck:
      url: https://myapp.test/health
      expectedStatus: 200
`)
	if err != nil {
		t.Fatal(err)
	}
	if len(def.Steps) != 3 {
		t.Fatalf("expected 3 steps, got %d", len(def.Steps))
	}
	if def.Steps[0].Kind() != "run" || def.Steps[1].Kind() != "deploy" || def.Steps[2].Kind() != "healthcheck" {
		t.Fatalf("unexpected step kinds: %+v", def.Steps)
	}
}

func TestParseRejectsEmptyPipeline(t *testing.T) {
	if _, err := Parse(`steps: []`); err == nil {
		t.Fatal("expected an empty pipeline to be rejected")
	}
}

func TestParseRejectsStepWithNoKind(t *testing.T) {
	if _, err := Parse(`
steps:
  - name: nothing
`); err == nil {
		t.Fatal("expected a step with no run/ssh/deploy/healthcheck to be rejected")
	}
}

func TestParseRejectsStepWithNoName(t *testing.T) {
	if _, err := Parse(`
steps:
  - run: echo hi
`); err == nil {
		t.Fatal("expected a step with no name to be rejected")
	}
}

func TestParseRejectsInvalidYAML(t *testing.T) {
	if _, err := Parse("not: valid: yaml: at: all: :::"); err == nil {
		t.Fatal("expected invalid YAML to be rejected")
	}
}
