package service

import "fmt"

// dependencyNode is the minimal shape TopoSort needs -- kept separate from
// Service so the algorithm is testable without constructing full Service
// values.
type dependencyNode struct {
	ID        string
	DependsOn []string
}

// TopoSort orders services so each one appears after everything in its
// DependsOn[] (Kahn's algorithm, per §15). A dependency that isn't present
// in the given slice is ignored rather than treated as an error -- §15's
// explicit caution that "independent services stay independent" means a
// service depending on something outside the autostart set (already
// running, or not itself autostart) is not a cycle or a missing-dependency
// failure, just an edge this function doesn't need to order.
//
// Returns an error naming the services still stuck in a cycle if the graph
// isn't a DAG, rather than silently picking an order.
func TopoSort(services []Service) ([]string, error) {
	nodes := make([]dependencyNode, len(services))
	for i, s := range services {
		nodes[i] = dependencyNode{ID: s.ID, DependsOn: s.DependsOn}
	}
	return topoSortNodes(nodes)
}

func topoSortNodes(nodes []dependencyNode) ([]string, error) {
	present := make(map[string]bool, len(nodes))
	for _, n := range nodes {
		present[n.ID] = true
	}

	inDegree := make(map[string]int, len(nodes))
	dependents := make(map[string][]string) // dependency ID -> IDs that depend on it
	for _, n := range nodes {
		inDegree[n.ID] = 0
	}
	for _, n := range nodes {
		for _, dep := range n.DependsOn {
			if !present[dep] {
				continue
			}
			inDegree[n.ID]++
			dependents[dep] = append(dependents[dep], n.ID)
		}
	}

	// Seed the queue in input order so ties between independent services
	// resolve deterministically.
	var queue []string
	for _, n := range nodes {
		if inDegree[n.ID] == 0 {
			queue = append(queue, n.ID)
		}
	}

	order := make([]string, 0, len(nodes))
	for len(queue) > 0 {
		id := queue[0]
		queue = queue[1:]
		order = append(order, id)
		for _, dep := range dependents[id] {
			inDegree[dep]--
			if inDegree[dep] == 0 {
				queue = append(queue, dep)
			}
		}
	}

	if len(order) != len(nodes) {
		var stuck []string
		for _, n := range nodes {
			if inDegree[n.ID] > 0 {
				stuck = append(stuck, n.ID)
			}
		}
		return nil, fmt.Errorf("dependency cycle detected among services: %v", stuck)
	}
	return order, nil
}
