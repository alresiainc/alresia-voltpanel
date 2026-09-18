package service

import "testing"

func indexOf(order []string, id string) int {
	for i, v := range order {
		if v == id {
			return i
		}
	}
	return -1
}

func TestTopoSortOrdersDependenciesFirst(t *testing.T) {
	// web depends on api, api depends on db -- expect db, api, web (in some
	// order consistent with those constraints).
	services := []Service{
		{ID: "web", DependsOn: []string{"api"}},
		{ID: "api", DependsOn: []string{"db"}},
		{ID: "db", DependsOn: nil},
	}
	order, err := TopoSort(services)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(order) != 3 {
		t.Fatalf("expected 3 services in order, got %d: %v", len(order), order)
	}
	if indexOf(order, "db") > indexOf(order, "api") {
		t.Fatalf("db must come before api, got order %v", order)
	}
	if indexOf(order, "api") > indexOf(order, "web") {
		t.Fatalf("api must come before web, got order %v", order)
	}
}

func TestTopoSortIndependentServicesStayIndependent(t *testing.T) {
	// redis and nginx have no relationship -- §15's explicit caution that
	// independent services shouldn't be forced into an artificial order.
	// Both must appear, order between them doesn't matter.
	services := []Service{
		{ID: "redis"},
		{ID: "nginx"},
	}
	order, err := TopoSort(services)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(order) != 2 {
		t.Fatalf("expected both independent services in the order, got %v", order)
	}
}

func TestTopoSortIgnoresDependencyOutsideSet(t *testing.T) {
	// "web" depends on "mysql", which isn't in the autostart set (e.g.
	// already running, or not itself autostart) -- must not be treated as
	// an error or a cycle.
	services := []Service{
		{ID: "web", DependsOn: []string{"mysql"}},
	}
	order, err := TopoSort(services)
	if err != nil {
		t.Fatalf("unexpected error for a dependency outside the set: %v", err)
	}
	if len(order) != 1 || order[0] != "web" {
		t.Fatalf("expected [web], got %v", order)
	}
}

func TestTopoSortDetectsDirectCycle(t *testing.T) {
	services := []Service{
		{ID: "a", DependsOn: []string{"b"}},
		{ID: "b", DependsOn: []string{"a"}},
	}
	_, err := TopoSort(services)
	if err == nil {
		t.Fatal("expected a cycle-detection error, got nil")
	}
}

func TestTopoSortDetectsIndirectCycle(t *testing.T) {
	services := []Service{
		{ID: "a", DependsOn: []string{"b"}},
		{ID: "b", DependsOn: []string{"c"}},
		{ID: "c", DependsOn: []string{"a"}},
	}
	_, err := TopoSort(services)
	if err == nil {
		t.Fatal("expected a cycle-detection error for an indirect cycle, got nil")
	}
}

func TestTopoSortEmptyInput(t *testing.T) {
	order, err := TopoSort(nil)
	if err != nil {
		t.Fatalf("unexpected error for empty input: %v", err)
	}
	if len(order) != 0 {
		t.Fatalf("expected empty order, got %v", order)
	}
}
